package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Amadeus-22/permwatch/internal/app"
	"github.com/Amadeus-22/permwatch/internal/domain"
)

var alert = app.Alert{
	Address: "klv1uah7ye2sq6vdnlksf6v3q5mtp2yf87352ghy8hve2khxjjcu3vqqaycqgs",
	Changes: []domain.Change{{Kind: domain.ChangeSignerAdded, PermissionID: 0, Message: "signer added"}},
	At:      time.Unix(1_790_000_000, 0).UTC(),
}

func TestWebhookPostsAlertAsJSON(t *testing.T) {
	var got app.Alert
	var contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := NewWebhook(srv.URL, time.Second).Notify(context.Background(), alert); err != nil {
		t.Fatal(err)
	}
	if contentType != "application/json" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	if got.Address != alert.Address || len(got.Changes) != 1 || got.Changes[0].Kind != domain.ChangeSignerAdded {
		t.Fatalf("received %+v", got)
	}
}

func TestWebhookTreatsNon2xxAsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if err := NewWebhook(srv.URL, time.Second).Notify(context.Background(), alert); err == nil {
		t.Fatal("expected an error for status 500")
	}
}

type recorder struct {
	calls int
	err   error
}

func (r *recorder) Notify(context.Context, app.Alert) error {
	r.calls++
	return r.err
}

func TestMultiCallsEveryNotifierAndJoinsErrors(t *testing.T) {
	failing := &recorder{err: errors.New("down")}
	working := &recorder{}
	err := Multi{failing, working}.Notify(context.Background(), alert)
	if err == nil {
		t.Fatal("expected the failure to be reported")
	}
	if failing.calls != 1 || working.calls != 1 {
		t.Fatalf("calls: failing=%d working=%d", failing.calls, working.calls)
	}
}
