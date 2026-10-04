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
	VaultChecked(contract domain.Address, usedPercent int, err error)
}

// Watcher polls a fixed set of accounts and vaults. It alerts when an account's
// permissions change and when a vault's spending reaches its warning line.
type Watcher struct {
	Source      AccountSource
	VaultSource VaultSource // may be nil when Vaults is empty
	Store       SnapshotStore
	Notifier    Notifier
	Observer    Observer
	Log         *slog.Logger
	Accounts    []Watched
	Vaults      []WatchedVault
	Interval    time.Duration
	Now         func() time.Time

	mu           sync.RWMutex
	reports      map[domain.Address]Report
	vaultReports map[domain.Address]VaultReport
	// vaultLevel is the severity rank of the last vault alert delivered, 0 for
	// none. It is kept in memory: after a restart a vault still over its warning
	// line alerts once more.
	vaultLevel map[domain.Address]int
	cycled     bool
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

// Cycle checks every account and vault once. A failure on one target is logged
// and counted; it does not stop the others.
func (w *Watcher) Cycle(ctx context.Context) {
	for _, account := range w.Accounts {
		if ctx.Err() != nil {
			return
		}
		err := w.check(ctx, account)
		w.Observer.Polled(account.Address, err)
		if err != nil {
			w.Log.Error("check account", "address", string(account.Address), "label", account.Label, "error", err.Error())
		}
	}
	for _, vault := range w.Vaults {
		if ctx.Err() != nil {
			return
		}
		used, err := w.checkVault(ctx, vault)
		w.Observer.VaultChecked(vault.Contract, used, err)
		if err != nil {
			w.Log.Error("check vault", "contract", string(vault.Contract), "label", vault.Label, "error", err.Error())
		}
	}
	w.mu.Lock()
	w.cycled = true
	w.mu.Unlock()
}

func (w *Watcher) check(ctx context.Context, target Watched) error {
	addr := target.Address
	report, err := Audit(ctx, w.Source, addr, w.Now())
	if err != nil {
		return fmt.Errorf("audit %s: %w", addr, err)
	}
	report.Label, report.Owner = target.Label, target.Owner
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
		w.Log.Info("baseline recorded", "address", string(addr), "label", target.Label,
			"permissions", len(report.Account.Permissions), "findings", len(report.Findings))
		return w.save(ctx, report.Account)
	}

	changes := domain.Diff(previous, report.Account)
	if len(changes) == 0 {
		return nil
	}
	w.Observer.Changed(addr, changes)

	alert := Alert{
		Kind: AlertPermissionsChanged, Address: addr, Label: target.Label, Owner: target.Owner,
		Changes: changes, Findings: report.Findings, At: report.CheckedAt,
	}
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

// checkVault reads one vault and alerts when its warning level rises: once when
// the warning line is crossed and once more when the limit is reached. The level
// falls back silently when a new period starts.
func (w *Watcher) checkVault(ctx context.Context, target WatchedVault) (int, error) {
	report, err := CheckVault(ctx, w.VaultSource, target.Contract, target.WarnPercent, w.Now())
	if err != nil {
		return 0, fmt.Errorf("check vault %s: %w", target.Contract, err)
	}
	report.Label, report.Owner = target.Label, target.Owner

	level := 0
	for _, warning := range report.Warnings {
		if rank := warning.Severity.Rank(); rank > level {
			level = rank
		}
	}

	w.mu.Lock()
	if w.vaultReports == nil {
		w.vaultReports = make(map[domain.Address]VaultReport)
		w.vaultLevel = make(map[domain.Address]int)
	}
	w.vaultReports[target.Contract] = report
	previous := w.vaultLevel[target.Contract]
	w.mu.Unlock()

	if level > previous {
		alert := Alert{
			Kind: AlertVaultNearLimit, Address: target.Contract, Label: target.Label, Owner: target.Owner,
			Vault: &report, At: report.CheckedAt,
		}
		if err := w.Notifier.Notify(ctx, alert); err != nil {
			// The level is not advanced, so the next cycle tries again.
			return report.UsedPercent, fmt.Errorf("notify vault %s: %w", target.Contract, err)
		}
	}

	w.mu.Lock()
	w.vaultLevel[target.Contract] = level
	w.mu.Unlock()
	return report.UsedPercent, nil
}

// Reports returns the latest report of every account checked so far, in the
// configured order.
func (w *Watcher) Reports() []Report {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]Report, 0, len(w.reports))
	for _, account := range w.Accounts {
		if r, ok := w.reports[account.Address]; ok {
			out = append(out, r)
		}
	}
	return out
}

// VaultReports returns the latest report of every vault checked so far, in the
// configured order.
func (w *Watcher) VaultReports() []VaultReport {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]VaultReport, 0, len(w.vaultReports))
	for _, vault := range w.Vaults {
		if r, ok := w.vaultReports[vault.Contract]; ok {
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
