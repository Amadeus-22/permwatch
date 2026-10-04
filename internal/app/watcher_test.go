package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/Amadeus-22/permwatch/internal/domain"
)

const (
	acct   domain.Address = "klv1uah7ye2sq6vdnlksf6v3q5mtp2yf87352ghy8hve2khxjjcu3vqqaycqgs"
	signer domain.Address = "klv1x4lhywvzqt4dhz92tps2a2jfrsn2qljz7c89njver2e6nmamkjhq62e787"
)

type fakeSource struct {
	account domain.Account
	err     error
}

func (f *fakeSource) Account(context.Context, domain.Address) (domain.Account, error) {
	return f.account, f.err
}

type fakeStore struct {
	saved map[domain.Address]domain.Account
	saves int
}

func (f *fakeStore) Load(_ context.Context, addr domain.Address) (domain.Account, bool, error) {
	a, ok := f.saved[addr]
	return a, ok, nil
}

func (f *fakeStore) Save(_ context.Context, a domain.Account) error {
	if f.saved == nil {
		f.saved = map[domain.Address]domain.Account{}
	}
	f.saved[a.Address] = a
	f.saves++
	return nil
}

type fakeNotifier struct {
	alerts []Alert
	err    error
}

func (f *fakeNotifier) Notify(_ context.Context, a Alert) error {
	if f.err != nil {
		return f.err
	}
	f.alerts = append(f.alerts, a)
	return nil
}

type fakeObserver struct {
	failures  int
	changes   []string
	vaultUsed []int
}

func (f *fakeObserver) Polled(_ domain.Address, err error) {
	if err != nil {
		f.failures++
	}
}

func (f *fakeObserver) Changed(_ domain.Address, changes []domain.Change) {
	for _, c := range changes {
		f.changes = append(f.changes, c.Kind)
	}
}

func (f *fakeObserver) Assessed(domain.Address, []domain.Finding) {}

func (f *fakeObserver) VaultChecked(_ domain.Address, used int, err error) {
	if err != nil {
		f.failures++
		return
	}
	f.vaultUsed = append(f.vaultUsed, used)
}

type fakeVaultSource struct {
	spent int64
	err   error
}

func (f *fakeVaultSource) VaultStatus(_ context.Context, contract domain.Address) (domain.VaultStatus, error) {
	if f.err != nil {
		return domain.VaultStatus{}, f.err
	}
	return domain.VaultStatus{Contract: contract, Limit: big.NewInt(100), Spent: big.NewInt(f.spent), Remaining: big.NewInt(100 - f.spent)}, nil
}

func transferPermission(threshold int64) domain.Permission {
	return domain.Permission{ID: 0, Type: domain.User, Name: "ops", Threshold: threshold,
		Operations: domain.Operations{0x01}, Signers: []domain.Signer{{Address: signer, Weight: 1}}}
}

func newWatcher(src *fakeSource, store *fakeStore, notifier *fakeNotifier, obs *fakeObserver) *Watcher {
	return &Watcher{
		Source: src, Store: store, Notifier: notifier, Observer: obs,
		Log:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Accounts: []Watched{{Address: acct, Label: "treasury", Owner: "finance"}},
		Interval: time.Hour,
		Now:      func() time.Time { return time.Unix(1_790_000_000, 0).UTC() },
	}
}

func TestWatcherFirstCycleRecordsBaselineWithoutAlerting(t *testing.T) {
	src := &fakeSource{account: domain.Account{Address: acct, Permissions: []domain.Permission{transferPermission(1)}}}
	store, notifier, obs := &fakeStore{}, &fakeNotifier{}, &fakeObserver{}
	w := newWatcher(src, store, notifier, obs)

	if w.Ready() {
		t.Fatal("ready before the first cycle")
	}
	w.Cycle(context.Background())

	if len(notifier.alerts) != 0 {
		t.Fatalf("alerted on the baseline: %+v", notifier.alerts)
	}
	if store.saves != 1 {
		t.Fatalf("saves = %d, want 1", store.saves)
	}
	if !w.Ready() {
		t.Fatal("not ready after a full cycle")
	}
	reports := w.Reports()
	if len(reports) != 1 || len(reports[0].Findings) != 1 || reports[0].Findings[0].Rule != domain.RuleSingleSignerFunds {
		t.Fatalf("reports = %+v", reports)
	}
}

func TestWatcherAlertsOnceWhenPermissionsChange(t *testing.T) {
	src := &fakeSource{account: domain.Account{Address: acct}}
	store, notifier, obs := &fakeStore{}, &fakeNotifier{}, &fakeObserver{}
	w := newWatcher(src, store, notifier, obs)
	w.Cycle(context.Background())

	src.account.Permissions = []domain.Permission{transferPermission(1)}
	w.Cycle(context.Background())
	w.Cycle(context.Background())

	if len(notifier.alerts) != 1 {
		t.Fatalf("alerts = %d, want 1", len(notifier.alerts))
	}
	alert := notifier.alerts[0]
	if alert.Address != acct || len(alert.Changes) != 1 || alert.Changes[0].Kind != domain.ChangePermissionAdded {
		t.Fatalf("alert = %+v", alert)
	}
	if alert.Kind != AlertPermissionsChanged || alert.Label != "treasury" || alert.Owner != "finance" {
		t.Fatalf("alert kind/label/owner = %q %q %q", alert.Kind, alert.Label, alert.Owner)
	}
	if len(alert.Findings) != 1 {
		t.Fatalf("alert carries %d findings, want 1", len(alert.Findings))
	}
	if !reflect.DeepEqual(obs.changes, []string{domain.ChangePermissionAdded}) {
		t.Fatalf("observed changes = %v", obs.changes)
	}
}

func TestWatcherRetriesAlertWhenNotifierFails(t *testing.T) {
	src := &fakeSource{account: domain.Account{Address: acct}}
	store, notifier, obs := &fakeStore{}, &fakeNotifier{}, &fakeObserver{}
	w := newWatcher(src, store, notifier, obs)
	w.Cycle(context.Background())

	src.account.Permissions = []domain.Permission{transferPermission(1)}
	notifier.err = errors.New("webhook down")
	w.Cycle(context.Background())
	if obs.failures != 1 {
		t.Fatalf("failures = %d, want 1", obs.failures)
	}
	if got := len(store.saved[acct].Permissions); got != 0 {
		t.Fatalf("snapshot advanced past an undelivered alert: %d permissions", got)
	}

	notifier.err = nil
	w.Cycle(context.Background())
	if len(notifier.alerts) != 1 {
		t.Fatalf("alerts after recovery = %d, want 1", len(notifier.alerts))
	}
}

func TestWatcherSourceErrorIsCountedAndDoesNotSave(t *testing.T) {
	src := &fakeSource{err: errors.New("api unreachable")}
	store, notifier, obs := &fakeStore{}, &fakeNotifier{}, &fakeObserver{}
	w := newWatcher(src, store, notifier, obs)
	w.Cycle(context.Background())

	if obs.failures != 1 || store.saves != 0 || len(notifier.alerts) != 0 {
		t.Fatalf("failures=%d saves=%d alerts=%d", obs.failures, store.saves, len(notifier.alerts))
	}
}

func TestRunStopsWhenContextIsCancelled(t *testing.T) {
	src := &fakeSource{account: domain.Account{Address: acct}}
	w := newWatcher(src, &fakeStore{}, &fakeNotifier{}, &fakeObserver{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
}

const vaultAddr domain.Address = "klv1qqqqqqqqqqqqqpgqprrwnlul05prr753xm278kq6jexa0a523vqq5gvgsc"

func newVaultWatcher(src *fakeVaultSource, notifier *fakeNotifier, obs *fakeObserver) *Watcher {
	w := newWatcher(&fakeSource{}, &fakeStore{}, notifier, obs)
	w.Accounts = nil
	w.VaultSource = src
	w.Vaults = []WatchedVault{{Contract: vaultAddr, Label: "ops vault", Owner: "ops", WarnPercent: 80}}
	return w
}

func TestWatcherAlertsOncePerVaultLevel(t *testing.T) {
	src := &fakeVaultSource{spent: 40}
	notifier, obs := &fakeNotifier{}, &fakeObserver{}
	w := newVaultWatcher(src, notifier, obs)
	ctx := context.Background()

	w.Cycle(ctx) // 40%: below the line
	if len(notifier.alerts) != 0 {
		t.Fatalf("alerted at 40%%: %+v", notifier.alerts)
	}

	src.spent = 85
	w.Cycle(ctx) // crosses the warning line
	w.Cycle(ctx) // same level: no repeat
	if len(notifier.alerts) != 1 {
		t.Fatalf("alerts after crossing the line = %d, want 1", len(notifier.alerts))
	}
	first := notifier.alerts[0]
	if first.Kind != AlertVaultNearLimit || first.Address != vaultAddr || first.Label != "ops vault" || first.Owner != "ops" {
		t.Fatalf("alert = %+v", first)
	}
	if first.Vault == nil || first.Vault.UsedPercent != 85 || first.Vault.Warnings[0].Severity != domain.Low {
		t.Fatalf("alert vault = %+v", first.Vault)
	}

	src.spent = 100
	w.Cycle(ctx) // limit reached: escalation
	if len(notifier.alerts) != 2 || notifier.alerts[1].Vault.Warnings[0].Severity != domain.High {
		t.Fatalf("alerts after reaching the limit = %+v", notifier.alerts)
	}

	src.spent = 0
	w.Cycle(ctx) // new period: clears silently
	src.spent = 90
	w.Cycle(ctx) // crosses the line again in the new period
	if len(notifier.alerts) != 3 {
		t.Fatalf("alerts in the new period = %d, want 3", len(notifier.alerts))
	}

	if !reflect.DeepEqual(obs.vaultUsed, []int{40, 85, 85, 100, 0, 90}) {
		t.Fatalf("observed usage = %v", obs.vaultUsed)
	}
	if reports := w.VaultReports(); len(reports) != 1 || reports[0].UsedPercent != 90 || reports[0].Label != "ops vault" {
		t.Fatalf("vault reports = %+v", reports)
	}
}

func TestWatcherRetriesVaultAlertWhenNotifierFails(t *testing.T) {
	src := &fakeVaultSource{spent: 95}
	notifier, obs := &fakeNotifier{err: errors.New("telegram down")}, &fakeObserver{}
	w := newVaultWatcher(src, notifier, obs)

	w.Cycle(context.Background())
	if obs.failures != 1 {
		t.Fatalf("failures = %d, want 1", obs.failures)
	}
	notifier.err = nil
	w.Cycle(context.Background())
	w.Cycle(context.Background())
	if len(notifier.alerts) != 1 {
		t.Fatalf("alerts after recovery = %d, want 1", len(notifier.alerts))
	}
}

func TestWatcherVaultSourceErrorIsCounted(t *testing.T) {
	src := &fakeVaultSource{err: errors.New("node unreachable")}
	notifier, obs := &fakeNotifier{}, &fakeObserver{}
	w := newVaultWatcher(src, notifier, obs)

	w.Cycle(context.Background())
	if obs.failures != 1 || len(notifier.alerts) != 0 || len(w.VaultReports()) != 0 {
		t.Fatalf("failures=%d alerts=%d reports=%d", obs.failures, len(notifier.alerts), len(w.VaultReports()))
	}
	if !w.Ready() {
		t.Fatal("a failing vault must not keep the watcher from becoming ready")
	}
}
