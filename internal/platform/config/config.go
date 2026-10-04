// Package config reads permwatch's configuration once, at startup: process
// settings from environment variables and, optionally, the watch list from a
// YAML file decoded strictly. Every variable is documented in the README.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Amadeus-22/permwatch/internal/app"
	"github.com/Amadeus-22/permwatch/internal/domain"
)

// Config is the validated process configuration.
type Config struct {
	Accounts         []app.Watched
	Vaults           []app.WatchedVault
	APIURL           string
	NodeURL          string
	APITimeout       time.Duration
	PollInterval     time.Duration
	HTTPAddr         string
	StateDir         string
	WebhookURL       string
	TelegramBotToken string
	TelegramChatID   string
	LogLevel         slog.Level
	ShutdownTimeout  time.Duration
}

// Defaults point at KleverChain mainnet.
const (
	DefaultAPIURL      = "https://api.mainnet.klever.org"
	DefaultNodeURL     = "https://node.mainnet.klever.org"
	DefaultWarnPercent = 80
)

// Load parses and validates the configuration. getenv is os.Getenv and readFile
// is os.ReadFile in production; tests pass fakes. Every problem is reported at once.
func Load(getenv func(string) string, readFile func(string) ([]byte, error)) (Config, error) {
	var errs []error
	cfg := Config{
		APIURL:           value(getenv, "PERMWATCH_API_URL", DefaultAPIURL),
		NodeURL:          value(getenv, "PERMWATCH_NODE_URL", DefaultNodeURL),
		HTTPAddr:         value(getenv, "PERMWATCH_HTTP_ADDR", ":8080"),
		StateDir:         value(getenv, "PERMWATCH_STATE_DIR", "./data"),
		WebhookURL:       strings.TrimSpace(getenv("PERMWATCH_WEBHOOK_URL")),
		TelegramBotToken: strings.TrimSpace(getenv("PERMWATCH_TELEGRAM_BOT_TOKEN")),
		TelegramChatID:   strings.TrimSpace(getenv("PERMWATCH_TELEGRAM_CHAT_ID")),
	}

	list := strings.TrimSpace(getenv("PERMWATCH_ADDRESSES"))
	file := strings.TrimSpace(getenv("PERMWATCH_CONFIG_FILE"))
	switch {
	case list != "" && file != "":
		errs = append(errs, errors.New("set PERMWATCH_ADDRESSES or PERMWATCH_CONFIG_FILE, not both"))
	case file != "":
		raw, err := readFile(file)
		if err != nil {
			errs = append(errs, fmt.Errorf("PERMWATCH_CONFIG_FILE: %w", err))
			break
		}
		accounts, vaults, err := ParseWatchList(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("PERMWATCH_CONFIG_FILE %s: %w", file, err))
		}
		cfg.Accounts, cfg.Vaults = accounts, vaults
	default:
		addresses, err := ParseAddresses(list)
		if err != nil {
			errs = append(errs, fmt.Errorf("PERMWATCH_ADDRESSES: %w", err))
		}
		for _, addr := range addresses {
			cfg.Accounts = append(cfg.Accounts, app.Watched{Address: addr})
		}
	}

	for name, raw := range map[string]string{"PERMWATCH_API_URL": cfg.APIURL, "PERMWATCH_NODE_URL": cfg.NodeURL} {
		if err := checkURL(raw); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	if cfg.WebhookURL != "" {
		if err := checkURL(cfg.WebhookURL); err != nil {
			errs = append(errs, fmt.Errorf("PERMWATCH_WEBHOOK_URL: %w", err))
		}
	}
	if (cfg.TelegramBotToken == "") != (cfg.TelegramChatID == "") {
		errs = append(errs, errors.New("PERMWATCH_TELEGRAM_BOT_TOKEN and PERMWATCH_TELEGRAM_CHAT_ID must be set together"))
	}

	durations := []struct {
		name     string
		fallback time.Duration
		target   *time.Duration
	}{
		{"PERMWATCH_API_TIMEOUT", 10 * time.Second, &cfg.APITimeout},
		{"PERMWATCH_POLL_INTERVAL", time.Minute, &cfg.PollInterval},
		{"PERMWATCH_SHUTDOWN_TIMEOUT", 10 * time.Second, &cfg.ShutdownTimeout},
	}
	for _, d := range durations {
		parsed, err := duration(getenv, d.name, d.fallback)
		if err != nil {
			errs = append(errs, err)
		}
		*d.target = parsed
	}

	if err := cfg.LogLevel.UnmarshalText([]byte(value(getenv, "PERMWATCH_LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("PERMWATCH_LOG_LEVEL: %w", err))
	}

	return cfg, errors.Join(errs...)
}

// ParseAddresses reads a comma-separated address list, rejecting an empty list
// and duplicates.
func ParseAddresses(list string) ([]domain.Address, error) {
	var out []domain.Address
	seen := map[domain.Address]bool{}
	for _, raw := range strings.Split(list, ",") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		addr, err := domain.ParseAddress(raw)
		if err != nil {
			return nil, err
		}
		if seen[addr] {
			return nil, fmt.Errorf("address %s listed twice", addr)
		}
		seen[addr] = true
		out = append(out, addr)
	}
	if len(out) == 0 {
		return nil, errors.New("at least one address is required")
	}
	return out, nil
}

type watchListFile struct {
	Accounts []struct {
		Address string `yaml:"address"`
		Label   string `yaml:"label"`
		Owner   string `yaml:"owner"`
	} `yaml:"accounts"`
	Vaults []struct {
		Contract    string `yaml:"contract"`
		Label       string `yaml:"label"`
		Owner       string `yaml:"owner"`
		WarnPercent *int   `yaml:"warn_percent"`
	} `yaml:"vaults"`
}

// ParseWatchList decodes the YAML watch list. Unknown keys are errors, so a
// typo cannot silently drop a setting. Every problem is reported at once.
func ParseWatchList(raw []byte) ([]app.Watched, []app.WatchedVault, error) {
	var file watchListFile
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		return nil, nil, fmt.Errorf("decode: %w", err)
	}

	var errs []error
	var accounts []app.Watched
	var vaults []app.WatchedVault
	seen := map[domain.Address]bool{}

	for i, a := range file.Accounts {
		addr, err := domain.ParseAddress(a.Address)
		if err != nil {
			errs = append(errs, fmt.Errorf("accounts[%d]: %w", i, err))
			continue
		}
		if seen[addr] {
			errs = append(errs, fmt.Errorf("accounts[%d]: %s listed twice", i, addr))
			continue
		}
		seen[addr] = true
		accounts = append(accounts, app.Watched{Address: addr, Label: strings.TrimSpace(a.Label), Owner: strings.TrimSpace(a.Owner)})
	}

	seen = map[domain.Address]bool{}
	for i, v := range file.Vaults {
		contract, err := domain.ParseAddress(v.Contract)
		if err != nil {
			errs = append(errs, fmt.Errorf("vaults[%d]: %w", i, err))
			continue
		}
		if seen[contract] {
			errs = append(errs, fmt.Errorf("vaults[%d]: %s listed twice", i, contract))
			continue
		}
		seen[contract] = true
		warn := DefaultWarnPercent
		if v.WarnPercent != nil {
			warn = *v.WarnPercent
		}
		if warn < 1 || warn > 100 {
			errs = append(errs, fmt.Errorf("vaults[%d]: warn_percent must be between 1 and 100, got %d", i, warn))
			continue
		}
		vaults = append(vaults, app.WatchedVault{Contract: contract, Label: strings.TrimSpace(v.Label), Owner: strings.TrimSpace(v.Owner), WarnPercent: warn})
	}

	if len(file.Accounts) == 0 && len(file.Vaults) == 0 {
		errs = append(errs, errors.New("the file lists no accounts and no vaults"))
	}
	return accounts, vaults, errors.Join(errs...)
}

func value(getenv func(string) string, name, fallback string) string {
	if v := strings.TrimSpace(getenv(name)); v != "" {
		return v
	}
	return fallback
}

func duration(getenv func(string) string, name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback, fmt.Errorf("%s: %w", name, err)
	}
	if d <= 0 {
		return fallback, fmt.Errorf("%s: must be positive, got %s", name, raw)
	}
	return d, nil
}

func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%q is not an http(s) URL", raw)
	}
	return nil
}
