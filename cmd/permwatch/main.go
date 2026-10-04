// Command permwatch audits and watches KleverChain account permissions.
//
//	permwatch audit [-api URL] [-json] <address>...
//	permwatch vault [-node URL] [-warn N] [-json] <contract>...
//	permwatch watch            (configured by PERMWATCH_* environment variables)
//	permwatch version
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Amadeus-22/permwatch/internal/adapter/filestore"
	"github.com/Amadeus-22/permwatch/internal/adapter/httpapi"
	"github.com/Amadeus-22/permwatch/internal/adapter/kleverapi"
	"github.com/Amadeus-22/permwatch/internal/adapter/klevernode"
	"github.com/Amadeus-22/permwatch/internal/adapter/notify"
	"github.com/Amadeus-22/permwatch/internal/app"
	"github.com/Amadeus-22/permwatch/internal/domain"
	"github.com/Amadeus-22/permwatch/internal/platform/config"
	"github.com/Amadeus-22/permwatch/internal/platform/logging"
	"github.com/Amadeus-22/permwatch/internal/platform/metrics"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// Exit codes of `permwatch audit` and `permwatch vault`.
const (
	exitOK       = 0
	exitError    = 1
	exitFindings = 2 // audit: a critical or high finding; vault: the warning line was reached
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return exitError
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch args[0] {
	case "audit":
		return audit(ctx, args[1:], stdout, stderr)
	case "vault":
		return vault(ctx, args[1:], stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, "permwatch", version)
		return exitOK
	case "watch":
		if err := watch(ctx, stderr); err != nil {
			fmt.Fprintln(stderr, "permwatch:", err)
			return exitError
		}
		return exitOK
	default:
		usage(stderr)
		return exitError
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `usage:
  permwatch audit [-api URL] [-json] <address>...   judge the permissions of accounts now
  permwatch vault [-node URL] [-warn N] [-json] <contract>...   how much of a limit vault's allowance is used
  permwatch watch                                   poll accounts and alert on changes (PERMWATCH_* env)
  permwatch version                                 print the version
`)
}

func audit(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("audit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	apiURL := flags.String("api", config.DefaultAPIURL, "KleverChain API base URL")
	asJSON := flags.Bool("json", false, "print the reports as JSON")
	timeout := flags.Duration("timeout", 10*time.Second, "timeout of each API request")
	if err := flags.Parse(args); err != nil {
		return exitError
	}
	if flags.NArg() == 0 {
		usage(stderr)
		return exitError
	}

	source := kleverapi.New(*apiURL, *timeout)
	var reports []app.Report
	for _, raw := range flags.Args() {
		addr, err := domain.ParseAddress(raw)
		if err != nil {
			fmt.Fprintln(stderr, "permwatch:", err)
			return exitError
		}
		report, err := app.Audit(ctx, source, addr, time.Now().UTC())
		if err != nil {
			fmt.Fprintln(stderr, "permwatch:", err)
			return exitError
		}
		reports = append(reports, report)
	}

	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(map[string]any{"accounts": reports}); err != nil {
			fmt.Fprintln(stderr, "permwatch:", err)
			return exitError
		}
	} else {
		for _, r := range reports {
			printReport(stdout, r)
		}
	}

	for _, r := range reports {
		for _, f := range r.Findings {
			if f.Severity.Rank() >= domain.High.Rank() {
				return exitFindings
			}
		}
	}
	return exitOK
}

func vault(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("vault", flag.ContinueOnError)
	flags.SetOutput(stderr)
	nodeURL := flags.String("node", config.DefaultNodeURL, "KleverChain node base URL")
	warn := flags.Int("warn", 80, "warn when this percentage of the period's limit is used")
	asJSON := flags.Bool("json", false, "print the reports as JSON")
	timeout := flags.Duration("timeout", 10*time.Second, "timeout of each node request")
	if err := flags.Parse(args); err != nil {
		return exitError
	}
	if flags.NArg() == 0 || *warn < 1 || *warn > 100 {
		usage(stderr)
		return exitError
	}

	source := klevernode.New(*nodeURL, *timeout)
	var reports []app.VaultReport
	for _, raw := range flags.Args() {
		contract, err := domain.ParseAddress(raw)
		if err != nil {
			fmt.Fprintln(stderr, "permwatch:", err)
			return exitError
		}
		report, err := app.CheckVault(ctx, source, contract, *warn, time.Now().UTC())
		if err != nil {
			fmt.Fprintln(stderr, "permwatch:", err)
			return exitError
		}
		reports = append(reports, report)
	}

	code := exitOK
	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(map[string]any{"vaults": reports}); err != nil {
			fmt.Fprintln(stderr, "permwatch:", err)
			return exitError
		}
	}
	for _, r := range reports {
		if !*asJSON {
			fmt.Fprintf(stdout, "%s\n  limit %s  spent %s  remaining %s  used %d%%\n",
				r.Status.Contract, r.Status.Limit, r.Status.Spent, r.Status.Remaining, r.UsedPercent)
			for _, w := range r.Warnings {
				fmt.Fprintf(stdout, "  [%s] %s: %s\n", w.Severity, w.Rule, w.Message)
			}
		}
		if len(r.Warnings) > 0 {
			code = exitFindings
		}
	}
	return code
}

func printReport(w io.Writer, r app.Report) {
	fmt.Fprintf(w, "%s\n", r.Account.Address)
	if len(r.Account.Permissions) == 0 {
		fmt.Fprintln(w, "  no permissions set: only the account's own key can sign")
	}
	for _, p := range r.Account.Permissions {
		allowed := "any transaction"
		if p.Type != domain.Owner {
			allowed = "nothing"
			if types := p.Operations.Types(); len(types) > 0 {
				allowed = fmt.Sprint(types)
			}
		}
		fmt.Fprintf(w, "  permission %d (%s) %q  threshold %d  allows %s\n", p.ID, p.Type, p.Name, p.Threshold, allowed)
		for _, s := range p.Signers {
			fmt.Fprintf(w, "    signer %s  weight %d\n", s.Address, s.Weight)
		}
	}
	if len(r.Findings) == 0 {
		fmt.Fprintln(w, "  findings: none")
	}
	for _, f := range r.Findings {
		fmt.Fprintf(w, "  [%s] %s: %s\n", f.Severity, f.Rule, f.Message)
	}
	fmt.Fprintln(w)
}

func watch(ctx context.Context, stderr io.Writer) error {
	cfg, err := config.Load(os.Getenv, os.ReadFile)
	if err != nil {
		return fmt.Errorf("configuration:\n%w", err)
	}
	logger := logging.New(stderr, cfg.LogLevel)

	store, err := filestore.New(cfg.StateDir)
	if err != nil {
		return err
	}
	notifiers := notify.Multi{notify.Log{Logger: logger}}
	if cfg.WebhookURL != "" {
		notifiers = append(notifiers, notify.NewWebhook(cfg.WebhookURL, cfg.APITimeout))
	}
	if cfg.TelegramBotToken != "" {
		notifiers = append(notifiers, notify.NewTelegram(notify.TelegramAPI, cfg.TelegramBotToken, cfg.TelegramChatID, cfg.APITimeout))
	}

	registry := prometheus.NewRegistry()
	watcher := &app.Watcher{
		Source:      kleverapi.New(cfg.APIURL, cfg.APITimeout),
		VaultSource: klevernode.New(cfg.NodeURL, cfg.APITimeout),
		Store:       store,
		Notifier:    notifiers,
		Observer:    metrics.New(registry),
		Log:         logger,
		Accounts:    cfg.Accounts,
		Vaults:      cfg.Vaults,
		Interval:    cfg.PollInterval,
		Now:         func() time.Time { return time.Now().UTC() },
	}
	server := httpapi.NewServer(cfg.HTTPAddr, watcher, registry)

	logger.Info("permwatch starting", "accounts", len(cfg.Accounts), "vaults", len(cfg.Vaults),
		"api_url", cfg.APIURL, "node_url", cfg.NodeURL, "poll_interval", cfg.PollInterval.String(),
		"http_addr", cfg.HTTPAddr, "webhook", cfg.WebhookURL != "", "telegram", cfg.TelegramBotToken != "")

	// Both goroutines are owned here: watch waits for them before returning.
	ctx, cancelWatcher := context.WithCancel(ctx)
	defer cancelWatcher()
	var wg sync.WaitGroup
	serverErr := make(chan error, 1)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = watcher.Run(ctx)
	}()
	go func() {
		defer wg.Done()
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	var runErr error
	select {
	case <-ctx.Done():
		logger.Info("shutdown requested")
	case runErr = <-serverErr:
		runErr = fmt.Errorf("http server: %w", runErr)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown", "error", err.Error())
	}
	cancelWatcher()
	wg.Wait()
	logger.Info("permwatch stopped")
	return runErr
}
