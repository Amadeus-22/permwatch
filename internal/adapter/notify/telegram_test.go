package notify

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Amadeus-22/permwatch/internal/app"
	"github.com/Amadeus-22/permwatch/internal/domain"
)

const secretToken = "123456:SECRET-TOKEN"

func TestTelegramSendsReadableMessage(t *testing.T) {
	var path string
	var message struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	in := alert
	in.Kind = app.AlertPermissionsChanged
	in.Label = "treasury"
	in.Owner = "finance team"
	in.Findings = []domain.Finding{{Rule: domain.RuleSingleSignerFunds, Severity: domain.High, Message: "one key moves value alone"}}

	if err := NewTelegram(srv.URL, secretToken, "-100123", time.Second).Notify(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if path != "/bot"+secretToken+"/sendMessage" || message.ChatID != "-100123" {
		t.Fatalf("path %q, chat %q", path, message.ChatID)
	}
	want := "Permissions changed: treasury (" + string(alert.Address) + ")\n" +
		"- signer added\n" +
		"What the account allows now:\n" +
		"[HIGH] one key moves value alone\n" +
		"Owner: finance team"
	if message.Text != want {
		t.Fatalf("text =\n%s\nwant\n%s", message.Text, want)
	}
}

func TestTextForVaultAlert(t *testing.T) {
	status := domain.VaultStatus{Contract: alert.Address, Limit: big.NewInt(100), Spent: big.NewInt(100), Remaining: big.NewInt(0)}
	text := Text(app.Alert{
		Kind: app.AlertVaultNearLimit, Address: alert.Address,
		Vault: &app.VaultReport{Status: status, UsedPercent: 100, Warnings: status.Warnings(80)},
	})
	if !strings.HasPrefix(text, "Vault near its limit: "+string(alert.Address)+"\n[HIGH] vault ") || !strings.Contains(text, "100% of its limit") {
		t.Fatalf("text = %q", text)
	}
}

func TestTelegramErrorsNeverContainTheToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	notifier := NewTelegram(srv.URL, secretToken, "1", time.Second)

	err := notifier.Notify(context.Background(), alert)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("status error = %v", err)
	}

	srv.Close() // connection refused: the transport error quotes the URL
	err = notifier.Notify(context.Background(), alert)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("transport error leaks the token: %v", err)
	}
	if !strings.Contains(err.Error(), "<telegram-api>") {
		t.Fatalf("transport error was not scrubbed: %v", err)
	}
}
