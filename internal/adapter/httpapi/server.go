// Package httpapi serves permwatch's health checks, metrics and the latest
// audit reports.
package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Amadeus-22/permwatch/internal/app"
)

// Reporter is what the API needs from the watcher.
type Reporter interface {
	Reports() []app.Report
	VaultReports() []app.VaultReport
	Ready() bool
}

// NewServer builds the HTTP server with explicit timeouts.
func NewServer(addr string, reporter Reporter, gatherer prometheus.Gatherer) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           NewHandler(reporter, gatherer),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// NewHandler wires the routes.
func NewHandler(reporter Reporter, gatherer prometheus.Gatherer) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !reporter.Ready() {
			writeError(w, http.StatusServiceUnavailable, "not_ready", "the first check of the watched accounts has not finished")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /v1/accounts", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"accounts": reporter.Reports()})
	})
	mux.HandleFunc("GET /v1/vaults", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"vaults": reporter.VaultReports()})
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}))
	return mux
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
