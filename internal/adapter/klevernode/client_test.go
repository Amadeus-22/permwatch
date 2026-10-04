package klevernode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Amadeus-22/permwatch/internal/domain"
)

const contract domain.Address = "klv1qqqqqqqqqqqqqpgqprrwnlul05prr753xm278kq6jexa0a523vqq5gvgsc"

func newClient(url string) *Client {
	c := New(url, time.Second)
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	return c
}

// node answers each view with the given body, as node.testnet.klever.org does.
func node(t *testing.T, answers map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ ScAddress, FuncName string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if r.URL.Path != "/vm/int" || req.ScAddress != string(contract) {
			t.Errorf("unexpected request %s for %q", r.URL.Path, req.ScAddress)
		}
		_, _ = w.Write([]byte(answers[req.FuncName]))
	}))
}

func TestVaultStatus(t *testing.T) {
	srv := node(t, map[string]string{
		"getLimit":     `{"data":{"data":"10000000"},"error":"","code":"successful"}`,
		"getSpent":     `{"data":{"data":"4000000"},"error":"","code":"successful"}`,
		"getRemaining": `{"data":{"data":"6000000"},"error":"","code":"successful"}`,
	})
	defer srv.Close()

	status, err := newClient(srv.URL).VaultStatus(context.Background(), contract)
	if err != nil {
		t.Fatal(err)
	}
	if status.Limit.String() != "10000000" || status.Spent.String() != "4000000" || status.Remaining.String() != "6000000" {
		t.Fatalf("status = %+v", status)
	}
	if status.UsedPercent() != 40 {
		t.Fatalf("UsedPercent() = %d", status.UsedPercent())
	}
}

func TestVaultStatusReportsMissingView(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"data":{"data":null},"error":"VMFunctionNotFound:invalid function (not found) no return data","code":"successful"}`))
	}))
	defer srv.Close()

	_, err := newClient(srv.URL).VaultStatus(context.Background(), contract)
	if err == nil || !strings.Contains(err.Error(), "getLimit") || !strings.Contains(err.Error(), "VMFunctionNotFound") {
		t.Fatalf("err = %v", err)
	}
	if calls != 1 {
		t.Fatalf("a contract error was retried: %d calls", calls)
	}
}

func TestVaultStatusReportsNonContractAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"data":null,"error":"doGetVMValue: element does not exist in container","code":"bad_request"}`))
	}))
	defer srv.Close()

	if _, err := newClient(srv.URL).VaultStatus(context.Background(), contract); err == nil {
		t.Fatal("expected an error")
	}
}

func TestVaultStatusRetriesServerErrors(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"data":"1"},"error":"","code":"successful"}`))
	}))
	defer srv.Close()

	if _, err := newClient(srv.URL).VaultStatus(context.Background(), contract); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatalf("calls = %d, want 1 failure + 3 successful views", calls)
	}
}
