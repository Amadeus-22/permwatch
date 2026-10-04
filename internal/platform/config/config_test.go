package config

import (
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	a1 = "klv1uah7ye2sq6vdnlksf6v3q5mtp2yf87352ghy8hve2khxjjcu3vqqaycqgs"
	a2 = "klv1x4lhywvzqt4dhz92tps2a2jfrsn2qljz7c89njver2e6nmamkjhq62e787"
	v1 = "klv1qqqqqqqqqqqqqpgqprrwnlul05prr753xm278kq6jexa0a523vqq5gvgsc"
)

func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func noFile(string) ([]byte, error) { return nil, errors.New("no file expected") }

func file(content string) func(string) ([]byte, error) {
	return func(string) ([]byte, error) { return []byte(content), nil }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"PERMWATCH_ADDRESSES": a1 + ", " + a2}), noFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Accounts) != 2 || string(cfg.Accounts[1].Address) != a2 || len(cfg.Vaults) != 0 {
		t.Fatalf("accounts = %+v, vaults = %+v", cfg.Accounts, cfg.Vaults)
	}
	if cfg.APIURL != DefaultAPIURL || cfg.NodeURL != DefaultNodeURL || cfg.HTTPAddr != ":8080" || cfg.StateDir != "./data" {
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
		"PERMWATCH_ADDRESSES":          a1,
		"PERMWATCH_API_URL":            "https://api.testnet.klever.org",
		"PERMWATCH_NODE_URL":           "https://node.testnet.klever.org",
		"PERMWATCH_POLL_INTERVAL":      "15s",
		"PERMWATCH_WEBHOOK_URL":        "https://example.org/hook",
		"PERMWATCH_TELEGRAM_BOT_TOKEN": "1:token",
		"PERMWATCH_TELEGRAM_CHAT_ID":   "-1001",
		"PERMWATCH_LOG_LEVEL":          "debug",
	}), noFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIURL != "https://api.testnet.klever.org" || cfg.NodeURL != "https://node.testnet.klever.org" ||
		cfg.PollInterval != 15*time.Second || cfg.WebhookURL != "https://example.org/hook" ||
		cfg.TelegramBotToken != "1:token" || cfg.TelegramChatID != "-1001" || cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadReportsEveryProblem(t *testing.T) {
	_, err := Load(env(map[string]string{
		"PERMWATCH_ADDRESSES":          "",
		"PERMWATCH_API_URL":            "ftp://nope",
		"PERMWATCH_POLL_INTERVAL":      "-5s",
		"PERMWATCH_LOG_LEVEL":          "loud",
		"PERMWATCH_TELEGRAM_BOT_TOKEN": "1:token",
	}), noFile)
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"PERMWATCH_ADDRESSES", "PERMWATCH_API_URL", "PERMWATCH_POLL_INTERVAL", "PERMWATCH_LOG_LEVEL", "PERMWATCH_TELEGRAM_CHAT_ID"} {
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

const watchList = `
accounts:
  - address: ` + a1 + `
    label: treasury
    owner: finance team
  - address: ` + a2 + `
vaults:
  - contract: ` + v1 + `
    label: ops vault
    owner: ops
    warn_percent: 70
  - contract: ` + a1 + `
`

func TestLoadWatchListFromFile(t *testing.T) {
	var asked string
	read := func(path string) ([]byte, error) {
		asked = path
		return []byte(watchList), nil
	}
	cfg, err := Load(env(map[string]string{"PERMWATCH_CONFIG_FILE": "/etc/permwatch.yaml"}), read)
	if err != nil {
		t.Fatal(err)
	}
	if asked != "/etc/permwatch.yaml" {
		t.Fatalf("read %q", asked)
	}
	if len(cfg.Accounts) != 2 || cfg.Accounts[0].Label != "treasury" || cfg.Accounts[0].Owner != "finance team" || cfg.Accounts[1].Label != "" {
		t.Fatalf("accounts = %+v", cfg.Accounts)
	}
	if len(cfg.Vaults) != 2 || string(cfg.Vaults[0].Contract) != v1 || cfg.Vaults[0].WarnPercent != 70 || cfg.Vaults[0].Label != "ops vault" {
		t.Fatalf("vaults = %+v", cfg.Vaults)
	}
	if cfg.Vaults[1].WarnPercent != DefaultWarnPercent {
		t.Fatalf("default warn_percent = %d", cfg.Vaults[1].WarnPercent)
	}
}

func TestLoadRejectsListAndFileTogether(t *testing.T) {
	_, err := Load(env(map[string]string{"PERMWATCH_ADDRESSES": a1, "PERMWATCH_CONFIG_FILE": "x.yaml"}), file(watchList))
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadReportsUnreadableFile(t *testing.T) {
	_, err := Load(env(map[string]string{"PERMWATCH_CONFIG_FILE": "missing.yaml"}), noFile)
	if err == nil || !strings.Contains(err.Error(), "PERMWATCH_CONFIG_FILE") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseWatchListRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"unknown key", "accounts:\n  - address: " + a1 + "\n    lable: typo\n", "lable"},
		{"empty file", "accounts: []\n", "no accounts and no vaults"},
		{"bad address", "accounts:\n  - address: nope\n", "accounts[0]"},
		{"duplicate account", "accounts:\n  - address: " + a1 + "\n  - address: " + a1 + "\n", "listed twice"},
		{"duplicate vault", "vaults:\n  - contract: " + v1 + "\n  - contract: " + v1 + "\n", "listed twice"},
		{"warn_percent out of range", "vaults:\n  - contract: " + v1 + "\n    warn_percent: 0\n", "warn_percent"},
		{"not yaml", "accounts: [", "decode"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseWatchList([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestParseWatchListReportsEveryProblem(t *testing.T) {
	_, _, err := ParseWatchList([]byte("accounts:\n  - address: nope\nvaults:\n  - contract: " + v1 + "\n    warn_percent: 500\n"))
	if err == nil || !strings.Contains(err.Error(), "accounts[0]") || !strings.Contains(err.Error(), "vaults[0]") {
		t.Fatalf("err = %v", err)
	}
}

// The example file in the repository root must stay loadable.
func TestExampleWatchListIsValid(t *testing.T) {
	raw, err := os.ReadFile("../../../permwatch.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	accounts, vaults, err := ParseWatchList(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].Label != "demo treasury" || len(vaults) != 1 || vaults[0].WarnPercent != 80 {
		t.Fatalf("accounts = %+v, vaults = %+v", accounts, vaults)
	}
}
