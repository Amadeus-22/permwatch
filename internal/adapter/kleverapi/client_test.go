package kleverapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Amadeus-22/permwatch/internal/domain"
)

const addr domain.Address = "klv1uah7ye2sq6vdnlksf6v3q5mtp2yf87352ghy8hve2khxjjcu3vqqaycqgs"

// Captured from api.testnet.klever.org on 2026-10-03 and trimmed to the fields used.
const accountJSON = `{"data":{"account":{"address":"klv1uah7ye2sq6vdnlksf6v3q5mtp2yf87352ghy8hve2khxjjcu3vqqaycqgs","nonce":3,
"permissions":[
 {"id":0,"type":1,"permissionName":"ops","Threshold":1,"operations":"01","signers":[{"address":"klv1x4lhywvzqt4dhz92tps2a2jfrsn2qljz7c89njver2e6nmamkjhq62e787","weight":1}]},
 {"id":1,"type":0,"permissionName":"","Threshold":1,"operations":"","signers":[{"address":"klv1uah7ye2sq6vdnlksf6v3q5mtp2yf87352ghy8hve2khxjjcu3vqqaycqgs","weight":1}]}
]}},"error":"","code":"successful"}`

func newClient(url string) *Client {
	c := New(url, time.Second)
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	return c
}

func TestAccountDecodesPermissions(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(accountJSON))
	}))
	defer srv.Close()

	account, err := newClient(srv.URL+"/").Account(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1.0/address/"+string(addr) {
		t.Fatalf("requested %q", path)
	}
	if len(account.Permissions) != 2 {
		t.Fatalf("permissions = %d, want 2", len(account.Permissions))
	}
	user := account.Permissions[0]
	if user.Type != domain.User || user.Name != "ops" || user.Threshold != 1 || !user.Operations.Allows(domain.Transfer) {
		t.Fatalf("user permission = %+v", user)
	}
	if owner := account.Permissions[1]; owner.Type != domain.Owner || owner.Signers[0].Address != addr {
		t.Fatalf("owner permission = %+v", owner)
	}
}

func TestAccountNotFound(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"data":null,"error":"cannot find account in database","code":"not_found"}`))
	}))
	defer srv.Close()

	_, err := newClient(srv.URL).Account(context.Background(), addr)
	if !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("got %v, want ErrAccountNotFound", err)
	}
	if calls != 1 {
		t.Fatalf("a 404 was retried: %d calls", calls)
	}
}

func TestAccountRetriesServerErrorsThenSucceeds(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(accountJSON))
	}))
	defer srv.Close()

	if _, err := newClient(srv.URL).Account(context.Background(), addr); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestAccountGivesUpAfterBoundedRetries(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	c.Retries = 2
	if _, err := c.Account(context.Background(), addr); err == nil {
		t.Fatal("expected an error")
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 1 attempt + 2 retries", calls)
	}
}

func TestAccountDoesNotRetryBadRequest(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"data":null,"error":"could not create address","code":"bad_request"}`))
	}))
	defer srv.Close()

	_, err := newClient(srv.URL).Account(context.Background(), addr)
	if err == nil || calls != 1 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
}

func TestAccountRejectsMalformedOperations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"account":{"permissions":[{"id":0,"type":1,"operations":"zz"}]}}}`))
	}))
	defer srv.Close()

	if _, err := newClient(srv.URL).Account(context.Background(), addr); err == nil {
		t.Fatal("expected an error for a non-hex operations mask")
	}
}
