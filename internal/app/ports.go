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

// VaultSource reads the spending state of a limit vault contract.
type VaultSource interface {
	VaultStatus(ctx context.Context, contract domain.Address) (domain.VaultStatus, error)
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

// Watched is an account under watch and the names its operators know it by.
type Watched struct {
	Address domain.Address
	Label   string
	Owner   string
}

// WatchedVault is a limit vault contract under watch.
type WatchedVault struct {
	Contract    domain.Address
	Label       string
	Owner       string
	WarnPercent int
}

// Report is the result of auditing one account.
type Report struct {
	Account   domain.Account   `json:"account"`
	Label     string           `json:"label,omitempty"`
	Owner     string           `json:"owner,omitempty"`
	Findings  []domain.Finding `json:"findings"`
	CheckedAt time.Time        `json:"checked_at"`
}

// VaultReport is the result of checking one vault.
type VaultReport struct {
	Status      domain.VaultStatus    `json:"status"`
	Label       string                `json:"label,omitempty"`
	Owner       string                `json:"owner,omitempty"`
	UsedPercent int                   `json:"used_percent"`
	Warnings    []domain.VaultWarning `json:"warnings"`
	CheckedAt   time.Time             `json:"checked_at"`
}

// Alert kinds, stable across versions.
const (
	AlertPermissionsChanged = "permissions_changed"
	AlertVaultNearLimit     = "vault_near_limit"
)

// Alert is what a Notifier delivers. A permissions alert carries Changes and
// the Findings of the new state; a vault alert carries Vault.
type Alert struct {
	Kind     string           `json:"kind"`
	Address  domain.Address   `json:"address"`
	Label    string           `json:"label,omitempty"`
	Owner    string           `json:"owner,omitempty"`
	Changes  []domain.Change  `json:"changes,omitempty"`
	Findings []domain.Finding `json:"findings,omitempty"`
	Vault    *VaultReport     `json:"vault,omitempty"`
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

// CheckVault reads a vault and reports whether its spending is near the limit.
func CheckVault(ctx context.Context, src VaultSource, contract domain.Address, warnPercent int, now time.Time) (VaultReport, error) {
	status, err := src.VaultStatus(ctx, contract)
	if err != nil {
		return VaultReport{}, err
	}
	return VaultReport{Status: status, UsedPercent: status.UsedPercent(), Warnings: status.Warnings(warnPercent), CheckedAt: now}, nil
}
