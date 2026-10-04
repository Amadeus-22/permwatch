// Package kleverapi reads accounts from the KleverChain public API
// (api.mainnet.klever.org / api.testnet.klever.org).
package kleverapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Amadeus-22/permwatch/internal/domain"
	"github.com/Amadeus-22/permwatch/internal/platform/backoff"
)

// maxBody bounds how much of a response is read: an account document is a few KB.
const maxBody = 4 << 20

// Client is an HTTP client for the account endpoint.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	// Retries is how many times a failed read is tried again (network errors,
	// 429 and 5xx only). 0 disables retrying.
	Retries int
	Backoff backoff.Policy
	// Sleep waits between attempts; tests replace it.
	Sleep func(ctx context.Context, d time.Duration) error
}

// New returns a client with a per-request timeout and bounded, jittered retries.
func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: timeout},
		Retries: 3,
		Backoff: backoff.Policy{Base: 500 * time.Millisecond, Max: 5 * time.Second},
		Sleep:   sleep,
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type accountResponse struct {
	Data *struct {
		Account struct {
			Address     string          `json:"address"`
			Permissions []apiPermission `json:"permissions"`
		} `json:"account"`
	} `json:"data"`
	Error string `json:"error"`
	Code  string `json:"code"`
}

type apiPermission struct {
	ID        int32  `json:"id"`
	Type      int    `json:"type"`
	Name      string `json:"permissionName"`
	Threshold int64  `json:"Threshold"`
	// Operations is the hex bitmask of allowed contract types.
	Operations string `json:"operations"`
	Signers    []struct {
		Address string `json:"address"`
		Weight  int64  `json:"weight"`
	} `json:"signers"`
}

// retryable marks an error worth another attempt.
type retryable struct{ err error }

func (r retryable) Error() string { return r.err.Error() }
func (r retryable) Unwrap() error { return r.err }

// Account fetches the account and its permissions.
func (c *Client) Account(ctx context.Context, addr domain.Address) (domain.Account, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			if err := c.Sleep(ctx, c.Backoff.Delay(attempt)); err != nil {
				return domain.Account{}, fmt.Errorf("fetch account %s: %w", addr, err)
			}
		}
		account, err := c.fetch(ctx, addr)
		if err == nil {
			return account, nil
		}
		lastErr = err
		if _, again := err.(retryable); !again {
			break
		}
	}
	return domain.Account{}, fmt.Errorf("fetch account %s: %w", addr, lastErr)
}

func (c *Client) fetch(ctx context.Context, addr domain.Address) (domain.Account, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1.0/address/"+string(addr), nil)
	if err != nil {
		return domain.Account{}, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return domain.Account{}, ctx.Err()
		}
		return domain.Account{}, retryable{err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return domain.Account{}, retryable{fmt.Errorf("read response: %w", err)}
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return domain.Account{}, domain.ErrAccountNotFound
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return domain.Account{}, retryable{fmt.Errorf("api status %d", resp.StatusCode)}
	case resp.StatusCode != http.StatusOK:
		return domain.Account{}, fmt.Errorf("api status %d: %s", resp.StatusCode, apiError(body))
	}

	var decoded accountResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return domain.Account{}, fmt.Errorf("decode response: %w", err)
	}
	if decoded.Data == nil {
		return domain.Account{}, fmt.Errorf("api returned no account: %s", decoded.Error)
	}
	return toDomain(addr, decoded.Data.Account.Permissions)
}

func apiError(body []byte) string {
	var decoded accountResponse
	if json.Unmarshal(body, &decoded) == nil && decoded.Error != "" {
		return decoded.Error
	}
	return "no error message"
}

func toDomain(addr domain.Address, perms []apiPermission) (domain.Account, error) {
	account := domain.Account{Address: addr}
	for _, p := range perms {
		ops, err := domain.ParseOperations(p.Operations)
		if err != nil {
			return domain.Account{}, fmt.Errorf("permission %d: %w", p.ID, err)
		}
		permission := domain.Permission{
			ID: p.ID, Type: domain.PermissionType(p.Type), Name: p.Name,
			Threshold: p.Threshold, Operations: ops,
		}
		for _, s := range p.Signers {
			signer, err := domain.ParseAddress(s.Address)
			if err != nil {
				return domain.Account{}, fmt.Errorf("permission %d signer: %w", p.ID, err)
			}
			permission.Signers = append(permission.Signers, domain.Signer{Address: signer, Weight: s.Weight})
		}
		account.Permissions = append(account.Permissions, permission)
	}
	return account.Normalized(), nil
}
