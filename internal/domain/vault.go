package domain

import (
	"encoding/json"
	"fmt"
	"math/big"
)

// RuleVaultNearLimit fires when a spending vault has used most of its allowance.
const RuleVaultNearLimit = "vault_near_limit"

// VaultStatus is the spending state of a limit vault contract in its current
// period. Amounts are in the token's smallest unit and exact.
type VaultStatus struct {
	Contract  Address
	Limit     *big.Int
	Spent     *big.Int
	Remaining *big.Int
}

// MarshalJSON writes the amounts as decimal strings: they can exceed what a JSON
// number holds exactly in most clients.
func (v VaultStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Contract  Address `json:"contract"`
		Limit     string  `json:"limit"`
		Spent     string  `json:"spent"`
		Remaining string  `json:"remaining"`
	}{v.Contract, v.Limit.String(), v.Spent.String(), v.Remaining.String()})
}

// VaultWarning says a vault's spending reached the warning line.
type VaultWarning struct {
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// UsedPercent is the share of the period's limit already withdrawn, rounded
// down. A vault with a zero limit counts as fully used: nothing can be withdrawn.
func (v VaultStatus) UsedPercent() int {
	if v.Limit.Sign() <= 0 {
		return 100
	}
	used := new(big.Int).Mul(v.Spent, big.NewInt(100))
	used.Quo(used, v.Limit)
	if !used.IsInt64() || used.Int64() > 100 {
		return 100
	}
	return int(used.Int64())
}

// Warnings returns a warning when at least warnPercent of the limit is used:
// high once the limit is reached, low before that.
func (v VaultStatus) Warnings(warnPercent int) []VaultWarning {
	used := v.UsedPercent()
	if used < warnPercent {
		return nil
	}
	severity := Low
	if used >= 100 {
		severity = High
	}
	return []VaultWarning{{
		Rule:     RuleVaultNearLimit,
		Severity: severity,
		Message: fmt.Sprintf("vault %s has used %d%% of its limit this period (%s of %s, %s left)",
			v.Contract, used, v.Spent, v.Limit, v.Remaining),
	}}
}
