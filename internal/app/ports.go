// Package app holds permwatch's use cases and the ports they need.
package app

import (
	"context"
	"time"

	"github.com/Amadeus-22/permwatch/internal/domain"
)

// AccountSource reads an account's current permissions from the chain.
type AccountSource interface {
	Account(ctx context.Context, addr domain.Address) (domain.Account, error)
}

// SnapshotStore keeps the last permissions seen for each account.
type SnapshotStore interface {
	// Load returns the stored snapshot; found is false on the first run.
	Load(ctx context.Context, addr domain.Address) (snapshot domain.Account, found bool, err error)
	Save(ctx context.Context, snapshot domain.Account) error
}

// Notifier delivers an alert to whoever should hear about it.
type Notifier interface {
	Notify(ctx context.Context, alert Alert) error
}

// Report is the result of auditing one account.
type Report struct {
	Account   domain.Account   `json:"account"`
	Findings  []domain.Finding `json:"findings"`
	CheckedAt time.Time        `json:"checked_at"`
}

// Alert says an account's permissions changed, and what the new state allows.
type Alert struct {
	Address  domain.Address   `json:"address"`
	Changes  []domain.Change  `json:"changes"`
	Findings []domain.Finding `json:"findings"`
	At       time.Time        `json:"at"`
}

// Audit reads one account and judges its permissions.
func Audit(ctx context.Context, src AccountSource, addr domain.Address, now time.Time) (Report, error) {
	account, err := src.Account(ctx, addr)
	if err != nil {
		return Report{}, err
	}
	account = account.Normalized()
	return Report{Account: account, Findings: domain.Assess(account), CheckedAt: now}, nil
}
