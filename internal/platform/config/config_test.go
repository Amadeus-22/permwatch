package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

const (
	a1 = "klv1uah7ye2sq6vdnlksf6v3q5mtp2yf87352ghy8hve2khxjjcu3vqqaycqgs"
	a2 = "klv1x4lhywvzqt4dhz92tps2a2jfrsn2qljz7c89njver2e6nmamkjhq62e787"
)

func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"PERMWATCH_ADDRESSES": a1 + ", " + a2}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Addresses) != 2 || cfg.APIURL != DefaultAPIURL || cfg.HTTPAddr != ":8080" || cfg.StateDir != "./data" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.PollInterval != time.Minute || cfg.APITimeout != 10*time.Second || cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("durations = %s %s %s", cfg.PollInterval, cfg.APITimeout, cfg.ShutdownTimeout)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("log level = %s", cfg.LogLevel)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"PERMWATCH_ADDRESSES":     a1,
		"PERMWATCH_API_URL":       "https://api.testnet.klever.org",
		"PERMWATCH_POLL_INTERVAL": "15s",
		"PERMWATCH_WEBHOOK_URL":   "https://example.org/hook",
		"PERMWATCH_LOG_LEVEL":     "debug",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIURL != "https://api.testnet.klever.org" || cfg.PollInterval != 15*time.Second ||
		cfg.WebhookURL != "https://example.org/hook" || cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadReportsEveryProblem(t *testing.T) {
	_, err := Load(env(map[string]string{
		"PERMWATCH_ADDRESSES":     "",
		"PERMWATCH_API_URL":       "ftp://nope",
		"PERMWATCH_POLL_INTERVAL": "-5s",
		"PERMWATCH_LOG_LEVEL":     "loud",
	}))
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"PERMWATCH_ADDRESSES", "PERMWATCH_API_URL", "PERMWATCH_POLL_INTERVAL", "PERMWATCH_LOG_LEVEL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}

func TestParseAddressesRejectsDuplicatesAndGarbage(t *testing.T) {
	if _, err := ParseAddresses(a1 + "," + a1); err == nil {
		t.Error("duplicate accepted")
	}
	if _, err := ParseAddresses(a1 + ",not-an-address"); err == nil {
		t.Error("invalid address accepted")
	}
}
