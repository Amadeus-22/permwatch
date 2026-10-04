// Package klevernode queries smart contract views on a KleverChain node
// (node.mainnet.klever.org / node.testnet.klever.org).
package klevernode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/Amadeus-22/permwatch/internal/domain"
	"github.com/Amadeus-22/permwatch/internal/platform/backoff"
)

const maxBody = 1 << 20

// Client calls the node's read-only VM endpoint.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Retries int
	Backoff backoff.Policy
	Sleep   func(ctx context.Context, d time.Duration) error
}

// New returns a client with a per-request timeout and bounded, jittered retries.
func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: timeout},
		Retries: 3,
		Backoff: backoff.Policy{Base: 500 * time.Millisecond, Max: 5 * time.Second},
		Sleep: func(ctx context.Context, d time.Duration) error {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

type retryable struct{ err error }

func (r retryable) Error() string { return r.err.Error() }
func (r retryable) Unwrap() error { return r.err }

// VaultStatus reads the limit, the amount spent and the amount remaining in the
// current period from a limit vault contract.
func (c *Client) VaultStatus(ctx context.Context, contract domain.Address) (domain.VaultStatus, error) {
	status := domain.VaultStatus{Contract: contract}
	views := []struct {
		name   string
		target **big.Int
	}{
		{"getLimit", &status.Limit},
		{"getSpent", &status.Spent},
		{"getRemaining", &status.Remaining},
	}
	for _, v := range views {
		value, err := c.queryInt(ctx, contract, v.name)
		if err != nil {
			return domain.VaultStatus{}, fmt.Errorf("query %s on %s: %w", v.name, contract, err)
		}
		*v.target = value
	}
	return status, nil
}

func (c *Client) queryInt(ctx context.Context, contract domain.Address, view string) (*big.Int, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			if err := c.Sleep(ctx, c.Backoff.Delay(attempt)); err != nil {
				return nil, err
			}
		}
		value, err := c.queryIntOnce(ctx, contract, view)
		if err == nil {
			return value, nil
		}
		lastErr = err
		if _, again := err.(retryable); !again {
			break
		}
	}
	return nil, lastErr
}

func (c *Client) queryIntOnce(ctx context.Context, contract domain.Address, view string) (*big.Int, error) {
	payload, err := json.Marshal(map[string]any{"scAddress": string(contract), "funcName": view, "args": []string{}})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/vm/int", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, retryable{err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, retryable{fmt.Errorf("read response: %w", err)}
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, retryable{fmt.Errorf("node status %d", resp.StatusCode)}
	}

	var decoded struct {
		Data *struct {
			Data *string `json:"data"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode response (status %d): %w", resp.StatusCode, err)
	}
	// The node answers 200 with an error string when the view does not exist,
	// and 400 when the address is not a contract.
	if decoded.Error != "" {
		return nil, fmt.Errorf("node: %s", decoded.Error)
	}
	if resp.StatusCode != http.StatusOK || decoded.Data == nil || decoded.Data.Data == nil {
		return nil, fmt.Errorf("node returned no value (status %d)", resp.StatusCode)
	}
	value, ok := new(big.Int).SetString(*decoded.Data.Data, 10)
	if !ok {
		return nil, fmt.Errorf("node returned a non-integer value %q", *decoded.Data.Data)
	}
	return value, nil
}
