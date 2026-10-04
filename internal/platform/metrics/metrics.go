// Package metrics defines permwatch's Prometheus metrics in one place, so the
// README table and the code cannot drift apart.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/Amadeus-22/permwatch/internal/domain"
)

// Metrics groups every permwatch collector and implements app.Observer.
type Metrics struct {
	Polls       *prometheus.CounterVec // label: result (ok, error)
	Changes     *prometheus.CounterVec // label: kind
	Findings    *prometheus.GaugeVec   // labels: address, severity
	LastSuccess *prometheus.GaugeVec   // label: address
	VaultChecks *prometheus.CounterVec // label: result (ok, error)
	VaultUsed   *prometheus.GaugeVec   // label: contract
}

var severities = []domain.Severity{domain.Critical, domain.High, domain.Low}

// New creates the collectors and registers them with reg. Tests pass a fresh
// prometheus.NewRegistry() so runs do not share state.
func New(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		Polls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "permwatch_polls_total",
			Help: "Account checks, by result.",
		}, []string{"result"}),
		Changes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "permwatch_changes_total",
			Help: "Permission changes detected, by kind.",
		}, []string{"kind"}),
		Findings: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "permwatch_findings",
			Help: "Open risk findings per account and severity, as of the last check.",
		}, []string{"address", "severity"}),
		LastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "permwatch_last_success_timestamp_seconds",
			Help: "Unix time of the last successful check of the account.",
		}, []string{"address"}),
	}
	m.VaultChecks = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "permwatch_vault_checks_total",
		Help: "Vault checks, by result.",
	}, []string{"result"})
	m.VaultUsed = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "permwatch_vault_used_percent",
		Help: "Share of the vault's limit withdrawn in the current period, as of the last check.",
	}, []string{"contract"})
	reg.MustRegister(m.Polls, m.Changes, m.Findings, m.LastSuccess, m.VaultChecks, m.VaultUsed)
	return m
}

// Polled counts one check and stamps the account on success.
func (m *Metrics) Polled(addr domain.Address, err error) {
	if err != nil {
		m.Polls.WithLabelValues("error").Inc()
		return
	}
	m.Polls.WithLabelValues("ok").Inc()
	m.LastSuccess.WithLabelValues(string(addr)).SetToCurrentTime()
}

// Changed counts each detected change by kind.
func (m *Metrics) Changed(_ domain.Address, changes []domain.Change) {
	for _, c := range changes {
		m.Changes.WithLabelValues(c.Kind).Inc()
	}
}

// Assessed sets the open findings of the account, zeroing severities that cleared.
func (m *Metrics) Assessed(addr domain.Address, findings []domain.Finding) {
	counts := map[domain.Severity]int{}
	for _, f := range findings {
		counts[f.Severity]++
	}
	for _, s := range severities {
		m.Findings.WithLabelValues(string(addr), string(s)).Set(float64(counts[s]))
	}
}

// VaultChecked counts one vault check and records the usage on success.
func (m *Metrics) VaultChecked(contract domain.Address, usedPercent int, err error) {
	if err != nil {
		m.VaultChecks.WithLabelValues("error").Inc()
		return
	}
	m.VaultChecks.WithLabelValues("ok").Inc()
	m.VaultUsed.WithLabelValues(string(contract)).Set(float64(usedPercent))
}
