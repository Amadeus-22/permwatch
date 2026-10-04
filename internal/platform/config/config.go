// Package config reads permwatch's configuration once, at startup, from
// environment variables. Every variable is documented in the README.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/Amadeus-22/permwatch/internal/domain"
)

// Config is the validated process configuration.
type Config struct {
	Addresses       []domain.Address
	APIURL          string
	APITimeout      time.Duration
	PollInterval    time.Duration
	HTTPAddr        string
	StateDir        string
	WebhookURL      string
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
}

// DefaultAPIURL is KleverChain mainnet.
const DefaultAPIURL = "https://api.mainnet.klever.org"

// Load parses and validates the configuration. getenv is os.Getenv in
// production and a map lookup in tests. Every problem is reported at once.
func Load(getenv func(string) string) (Config, error) {
	var errs []error
	cfg := Config{
		APIURL:     value(getenv, "PERMWATCH_API_URL", DefaultAPIURL),
		HTTPAddr:   value(getenv, "PERMWATCH_HTTP_ADDR", ":8080"),
		StateDir:   value(getenv, "PERMWATCH_STATE_DIR", "./data"),
		WebhookURL: getenv("PERMWATCH_WEBHOOK_URL"),
	}

	addresses, err := ParseAddresses(getenv("PERMWATCH_ADDRESSES"))
	if err != nil {
		errs = append(errs, fmt.Errorf("PERMWATCH_ADDRESSES: %w", err))
	}
	cfg.Addresses = addresses

	if err := checkURL(cfg.APIURL); err != nil {
		errs = append(errs, fmt.Errorf("PERMWATCH_API_URL: %w", err))
	}
	if cfg.WebhookURL != "" {
		if err := checkURL(cfg.WebhookURL); err != nil {
			errs = append(errs, fmt.Errorf("PERMWATCH_WEBHOOK_URL: %w", err))
		}
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
