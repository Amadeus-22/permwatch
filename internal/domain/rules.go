package domain

import (
	"fmt"
	"sort"
	"strings"
)

// Severity orders findings by how much damage the situation allows.
type Severity string

const (
	Critical Severity = "critical"
	High     Severity = "high"
	Low      Severity = "low"
)

// Rank gives a sortable weight: higher is worse.
func (s Severity) Rank() int {
	switch s {
	case Critical:
		return 3
	case High:
		return 2
	default:
		return 1
	}
}

// Rule identifiers, stable across versions: alerts and metrics key on them.
const (
	RuleForeignOwner        = "foreign_owner"
	RuleCanChangePermission = "can_change_permissions"
	RuleSingleSignerFunds   = "single_signer_moves_funds"
	RuleUnknownOperations   = "unknown_operations"
)

// Finding is one risk a rule found in one permission.
type Finding struct {
	Rule         string    `json:"rule"`
	Severity     Severity  `json:"severity"`
	PermissionID int32     `json:"permission_id"`
	Signers      []Address `json:"signers,omitempty"`
	Message      string    `json:"message"`
}

// fundOperations are the contract types that can take value out of an account.
var fundOperations = []ContractType{Transfer, Withdraw, Claim, AssetTrigger, Buy, Sell, SmartContract}

// Assess runs every rule over the account and returns the findings, worst first.
func Assess(a Account) []Finding {
	var out []Finding
	for _, p := range a.Normalized().Permissions {
		out = append(out, assessPermission(a.Address, p)...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Severity.Rank() > out[j].Severity.Rank() })
	return out
}

func assessPermission(owner Address, p Permission) []Finding {
	var out []Finding
	others := foreignSigners(owner, p)

	if p.Type == Owner {
		if weightOf(others) >= p.Threshold && len(others) > 0 {
			out = append(out, Finding{
				Rule: RuleForeignOwner, Severity: Critical, PermissionID: p.ID, Signers: addresses(others),
				Message: fmt.Sprintf("%s can take full control of the account without the account's own key (owner permission, threshold %d)",
					describe(others), p.Threshold),
			})
		}
		return out
	}

	if p.Allows(UpdateAccountPermission) && len(others) > 0 {
		out = append(out, Finding{
			Rule: RuleCanChangePermission, Severity: Critical, PermissionID: p.ID, Signers: addresses(others),
			Message: fmt.Sprintf("permission %q lets %s rewrite the account's permissions, which is a path to full control",
				p.Name, describe(others)),
		})
	}

	if funds := allowedFundOperations(p); len(funds) > 0 {
		if solo := soloSigners(others, p.Threshold); len(solo) > 0 {
			out = append(out, Finding{
				Rule: RuleSingleSignerFunds, Severity: High, PermissionID: p.ID, Signers: addresses(solo),
				Message: fmt.Sprintf("permission %q lets %s move value alone, with no second signature (%s)",
					p.Name, describe(solo), joinTypes(funds)),
			})
		}
	}

	if unknown := unknownTypes(p.Operations); len(unknown) > 0 {
		out = append(out, Finding{
			Rule: RuleUnknownOperations, Severity: Low, PermissionID: p.ID,
			Message: fmt.Sprintf("permission %q grants contract types this version does not know: %s",
				p.Name, joinTypes(unknown)),
		})
	}
	return out
}

func foreignSigners(owner Address, p Permission) []Signer {
	var out []Signer
	for _, s := range p.Signers {
		if s.Address != owner {
			out = append(out, s)
		}
	}
	return out
}

// soloSigners returns the signers whose own weight reaches the threshold.
func soloSigners(signers []Signer, threshold int64) []Signer {
	var out []Signer
	for _, s := range signers {
		if s.Weight >= threshold {
			out = append(out, s)
		}
	}
	return out
}

func weightOf(signers []Signer) int64 {
	var sum int64
	for _, s := range signers {
		sum += s.Weight
	}
	return sum
}

func allowedFundOperations(p Permission) []ContractType {
	var out []ContractType
	for _, c := range fundOperations {
		if p.Operations.Allows(c) {
			out = append(out, c)
		}
	}
	return out
}

func unknownTypes(o Operations) []ContractType {
	var out []ContractType
	for _, c := range o.Types() {
		if !c.Known() {
			out = append(out, c)
		}
	}
	return out
}

func addresses(signers []Signer) []Address {
	out := make([]Address, len(signers))
	for i, s := range signers {
		out[i] = s.Address
	}
	return out
}

func describe(signers []Signer) string {
	parts := make([]string, len(signers))
	for i, s := range signers {
		parts[i] = string(s.Address)
	}
	return strings.Join(parts, ", ")
}

func joinTypes(types []ContractType) string {
	parts := make([]string, len(types))
	for i, c := range types {
		parts[i] = c.String()
	}
	return strings.Join(parts, ", ")
}
