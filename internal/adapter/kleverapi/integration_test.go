//go:build integration

package kleverapi

import (
	"context"
	"testing"
	"time"

	"github.com/Amadeus-22/permwatch/internal/domain"
)

// The account below was set up on testnet for this project: a user permission
// "treasury" that lets one other key send transfers alone.
func TestLiveTestnetAccount(t *testing.T) {
	const account domain.Address = "klv1x4lhywvzqt4dhz92tps2a2jfrsn2qljz7c89njver2e6nmamkjhq62e787"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	got, err := New("https://api.testnet.klever.org", 10*time.Second).Account(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	var treasury *domain.Permission
	for i := range got.Permissions {
		if got.Permissions[i].Name == "treasury" {
			treasury = &got.Permissions[i]
		}
	}
	if treasury == nil {
		t.Fatalf("no treasury permission in %+v", got.Permissions)
	}
	if treasury.Type != domain.User || !treasury.Operations.Allows(domain.Transfer) || len(treasury.Signers) != 1 {
		t.Fatalf("treasury = %+v", *treasury)
	}
	findings := domain.Assess(got)
	if len(findings) != 1 || findings[0].Rule != domain.RuleSingleSignerFunds {
		t.Fatalf("findings = %+v", findings)
	}
}
