package httpapi

import (
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Amadeus-22/permwatch/internal/app"
	"github.com/Amadeus-22/permwatch/internal/domain"
	"github.com/Amadeus-22/permwatch/internal/platform/metrics"
)

type fakeReporter struct {
	ready   bool
	reports []app.Report
	vaults  []app.VaultReport
}

func (f fakeReporter) Reports() []app.Report           { return f.reports }
func (f fakeReporter) VaultReports() []app.VaultReport { return f.vaults }
func (f fakeReporter) Ready() bool                     { return f.ready }

const addr domain.Address = "klv1uah7ye2sq6vdnlksf6v3q5mtp2yf87352ghy8hve2khxjjcu3vqqaycqgs"

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealthAndReadiness(t *testing.T) {
	reg := prometheus.NewRegistry()
	notReady := NewHandler(fakeReporter{}, reg)
	if rec := get(t, notReady, "/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("/healthz = %d", rec.Code)
	}
	rec := get(t, notReady, "/readyz")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"code":"not_ready"`) {
		t.Fatalf("/readyz before first cycle = %d %s", rec.Code, rec.Body)
	}
	if rec := get(t, NewHandler(fakeReporter{ready: true}, reg), "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("/readyz after first cycle = %d", rec.Code)
	}
}

func TestAccountsReturnsReports(t *testing.T) {
	account := domain.Account{Address: addr, Permissions: []domain.Permission{{
		ID: 0, Type: domain.User, Name: "ops", Threshold: 1, Operations: domain.Operations{0x01},
		Signers: []domain.Signer{{Address: "klv1x4lhywvzqt4dhz92tps2a2jfrsn2qljz7c89njver2e6nmamkjhq62e787", Weight: 1}},
	}}}
	reporter := fakeReporter{ready: true, reports: []app.Report{{Account: account, Findings: domain.Assess(account)}}}

	rec := get(t, NewHandler(reporter, prometheus.NewRegistry()), "/v1/accounts")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, content type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var body struct {
		Accounts []struct {
			Account  struct{ Address string }
			Findings []struct{ Rule, Severity string }
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Accounts) != 1 || body.Accounts[0].Account.Address != string(addr) {
		t.Fatalf("body = %s", rec.Body)
	}
	if f := body.Accounts[0].Findings; len(f) != 1 || f[0].Rule != domain.RuleSingleSignerFunds || f[0].Severity != "high" {
		t.Fatalf("findings = %+v", f)
	}
}

func TestVaultsReturnsReportsWithExactAmounts(t *testing.T) {
	status := domain.VaultStatus{Contract: addr, Limit: big.NewInt(10_000_000), Spent: big.NewInt(9_000_000), Remaining: big.NewInt(1_000_000)}
	reporter := fakeReporter{vaults: []app.VaultReport{{
		Status: status, Label: "ops vault", UsedPercent: status.UsedPercent(), Warnings: status.Warnings(80),
	}}}

	rec := get(t, NewHandler(reporter, prometheus.NewRegistry()), "/v1/vaults")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{`"limit":"10000000"`, `"spent":"9000000"`, `"used_percent":90`, `"label":"ops vault"`, `"rule":"vault_near_limit"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body is missing %s: %s", want, rec.Body)
		}
	}
}

func TestMetricsExposeFindings(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)
	m.Polled(addr, nil)
	m.Assessed(addr, []domain.Finding{{Rule: domain.RuleSingleSignerFunds, Severity: domain.High}})
	m.Changed(addr, []domain.Change{{Kind: domain.ChangeSignerAdded}})
	m.VaultChecked(addr, 90, nil)

	body := get(t, NewHandler(fakeReporter{}, reg), "/metrics").Body.String()
	for _, want := range []string{
		`permwatch_polls_total{result="ok"} 1`,
		`permwatch_findings{address="` + string(addr) + `",severity="high"} 1`,
		`permwatch_findings{address="` + string(addr) + `",severity="critical"} 0`,
		`permwatch_changes_total{kind="signer_added"} 1`,
		`permwatch_vault_checks_total{result="ok"} 1`,
		`permwatch_vault_used_percent{contract="` + string(addr) + `"} 90`,
		`permwatch_last_success_timestamp_seconds{address="` + string(addr) + `"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output is missing %q", want)
		}
	}
}
