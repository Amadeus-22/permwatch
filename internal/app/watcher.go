package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Amadeus-22/permwatch/internal/domain"
)

// Observer receives what the watcher measures. The metrics adapter implements it.
type Observer interface {
	Polled(addr domain.Address, err error)
	Changed(addr domain.Address, changes []domain.Change)
	Assessed(addr domain.Address, findings []domain.Finding)
}

// Watcher polls a fixed set of accounts and alerts when their permissions change.
type Watcher struct {
	Source    AccountSource
	Store     SnapshotStore
	Notifier  Notifier
	Observer  Observer
	Log       *slog.Logger
	Addresses []domain.Address
	Interval  time.Duration
	Now       func() time.Time

	mu      sync.RWMutex
	reports map[domain.Address]Report
	cycled  bool
}

// Run polls until ctx is cancelled. The first cycle starts immediately.
func (w *Watcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()
	for {
		w.Cycle(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Cycle checks every address once. A failure on one address is logged and
// counted; it does not stop the others.
func (w *Watcher) Cycle(ctx context.Context) {
	for _, addr := range w.Addresses {
		if ctx.Err() != nil {
			return
		}
		err := w.check(ctx, addr)
		w.Observer.Polled(addr, err)
		if err != nil {
			w.Log.Error("check account", "address", string(addr), "error", err.Error())
		}
	}
	w.mu.Lock()
	w.cycled = true
	w.mu.Unlock()
}

func (w *Watcher) check(ctx context.Context, addr domain.Address) error {
	report, err := Audit(ctx, w.Source, addr, w.Now())
	if err != nil {
		return fmt.Errorf("audit %s: %w", addr, err)
	}
	w.Observer.Assessed(addr, report.Findings)

	previous, found, err := w.Store.Load(ctx, addr)
	if err != nil {
		return fmt.Errorf("load snapshot %s: %w", addr, err)
	}

	w.mu.Lock()
	if w.reports == nil {
		w.reports = make(map[domain.Address]Report)
	}
	w.reports[addr] = report
	w.mu.Unlock()

	if !found {
		// First sight of the account: record the baseline, alert on nothing.
		// Its current findings are visible through the API, the log and metrics.
		w.Log.Info("baseline recorded", "address", string(addr),
			"permissions", len(report.Account.Permissions), "findings", len(report.Findings))
		return w.save(ctx, report.Account)
	}

	changes := domain.Diff(previous, report.Account)
	if len(changes) == 0 {
		return nil
	}
	w.Observer.Changed(addr, changes)

	alert := Alert{Address: addr, Changes: changes, Findings: report.Findings, At: report.CheckedAt}
	if err := w.Notifier.Notify(ctx, alert); err != nil {
		// Keep the old snapshot so the next cycle sees the change again and retries.
		return fmt.Errorf("notify %s: %w", addr, err)
	}
	return w.save(ctx, report.Account)
}

func (w *Watcher) save(ctx context.Context, snapshot domain.Account) error {
	if err := w.Store.Save(ctx, snapshot); err != nil {
		return fmt.Errorf("save snapshot %s: %w", snapshot.Address, err)
	}
	return nil
}

// Reports returns the latest report of every account checked so far, in the
// configured order.
func (w *Watcher) Reports() []Report {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]Report, 0, len(w.reports))
	for _, addr := range w.Addresses {
		if r, ok := w.reports[addr]; ok {
			out = append(out, r)
		}
	}
	return out
}

// Ready reports whether a full cycle has completed.
func (w *Watcher) Ready() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cycled
}
