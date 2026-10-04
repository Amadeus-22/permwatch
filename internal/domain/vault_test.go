package domain

import (
	"encoding/json"
	"math/big"
	"testing"
)

func vault(limit, spent int64) VaultStatus {
	remaining := limit - spent
	if remaining < 0 {
		remaining = 0
	}
	return VaultStatus{Contract: self, Limit: big.NewInt(limit), Spent: big.NewInt(spent), Remaining: big.NewInt(remaining)}
}

func TestVaultUsedPercentAndAssess(t *testing.T) {
	tests := []struct {
		name     string
		status   VaultStatus
		percent  int
		severity Severity // "" means no finding at warn=80
	}{
		{"untouched", vault(10_000_000, 0), 0, ""},
		{"under the warning line", vault(10_000_000, 7_999_999), 79, ""},
		{"at the warning line", vault(10_000_000, 8_000_000), 80, Low},
		{"one unit short of the limit", vault(10_000_000, 9_999_999), 99, Low},
		{"limit reached", vault(10_000_000, 10_000_000), 100, High},
		{"limit lowered below what was spent", vault(50, 60), 100, High},
		{"zero limit", vault(0, 0), 100, High},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.status.UsedPercent(); got != tc.percent {
				t.Fatalf("UsedPercent() = %d, want %d", got, tc.percent)
			}
			findings := tc.status.Warnings(80)
			if tc.severity == "" {
				if len(findings) != 0 {
					t.Fatalf("unexpected findings: %+v", findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Rule != RuleVaultNearLimit || findings[0].Severity != tc.severity {
				t.Fatalf("findings = %+v, want one %s %s", findings, tc.severity, RuleVaultNearLimit)
			}
		})
	}
}

func TestVaultUsedPercentWithAmountsBeyondInt64(t *testing.T) {
	limit, _ := new(big.Int).SetString("400000000000000000000000000", 10)
	spent, _ := new(big.Int).SetString("100000000000000000000000000", 10)
	status := VaultStatus{Contract: self, Limit: limit, Spent: spent, Remaining: new(big.Int).Sub(limit, spent)}
	if got := status.UsedPercent(); got != 25 {
		t.Fatalf("UsedPercent() = %d, want 25", got)
	}
}

func TestVaultStatusJSONKeepsAmountsExact(t *testing.T) {
	limit, _ := new(big.Int).SetString("400000000000000000000000001", 10)
	raw, err := json.Marshal(VaultStatus{Contract: self, Limit: limit, Spent: big.NewInt(1), Remaining: big.NewInt(0)})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"contract":"` + string(self) + `","limit":"400000000000000000000000001","spent":"1","remaining":"0"}`
	if string(raw) != want {
		t.Fatalf("got  %s\nwant %s", raw, want)
	}
}
