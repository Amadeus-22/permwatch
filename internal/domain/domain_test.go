package domain

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

const (
	self   Address = "klv1uah7ye2sq6vdnlksf6v3q5mtp2yf87352ghy8hve2khxjjcu3vqqaycqgs"
	other  Address = "klv1x4lhywvzqt4dhz92tps2a2jfrsn2qljz7c89njver2e6nmamkjhq62e787"
	second Address = "klv1fpwjz234gy8aaae3gx0e8q9f52vymzzn3z5q0s5h60pvktzx0n0qwvtux5"
)

func ops(t *testing.T, hexMask string) Operations {
	t.Helper()
	o, err := ParseOperations(hexMask)
	if err != nil {
		t.Fatalf("ParseOperations(%q): %v", hexMask, err)
	}
	return o
}

func TestParseAddress(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"valid", string(self), true},
		{"valid with spaces", "  " + string(other) + "\n", true},
		{"wrong prefix", "erd1uah7ye2sq6vdnlksf6v3q5mtp2yf87352ghy8hve2khxjjcu3vqqaycqgs", false},
		{"too short", "klv1uah7", false},
		{"bad character", "klv1uah7ye2sq6vdnlksf6v3q5mtp2yf87352ghy8hve2khxjjcu3vqqaycqgO", false},
		{"empty", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAddress(tc.in)
			if tc.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrInvalidAddress) {
				t.Fatalf("got %v, want ErrInvalidAddress", err)
			}
		})
	}
}

func TestOperations(t *testing.T) {
	tests := []struct {
		name  string
		mask  string
		types []ContractType
	}{
		{"empty", "", nil},
		{"transfer only", "01", []ContractType{Transfer}},
		{"withdraw and claim are in the second byte", "0003", []ContractType{Withdraw, Claim}},
		{"update permission is bit 6 of byte 2", "000040", []ContractType{UpdateAccountPermission}},
		{"smart contract is the top bit of byte 7", "0000000000000080", []ContractType{SmartContract}},
		{"first twelve types", "ff0f", []ContractType{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := ops(t, tc.mask)
			if got := o.Types(); !reflect.DeepEqual(got, tc.types) {
				t.Fatalf("Types() = %v, want %v", got, tc.types)
			}
			for _, c := range tc.types {
				if !o.Allows(c) {
					t.Errorf("Allows(%s) = false", c)
				}
			}
			if o.Allows(ContractType(40)) {
				t.Errorf("Allows(40) = true for mask %q", tc.mask)
			}
		})
	}
}

func TestParseOperationsRejectsBadInput(t *testing.T) {
	for _, mask := range []string{"zz", "0", "000000000000000001"} {
		if _, err := ParseOperations(mask); err == nil {
			t.Errorf("ParseOperations(%q) accepted", mask)
		}
	}
}

func TestOperationsJSONRoundTrip(t *testing.T) {
	in := Permission{ID: 1, Type: User, Name: "ops", Threshold: 2, Operations: ops(t, "0103"), Signers: []Signer{{other, 1}}}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Permission
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip changed the permission:\n in  %+v\n out %+v", in, out)
	}
}

func TestAssess(t *testing.T) {
	tests := []struct {
		name  string
		perms []Permission
		rules []string
	}{
		{"no permissions set", nil, nil},
		{
			"default owner permission held by the account itself",
			[]Permission{{ID: 0, Type: Owner, Threshold: 1, Signers: []Signer{{self, 1}}}},
			nil,
		},
		{
			"another key alone reaches the owner threshold",
			[]Permission{{ID: 0, Type: Owner, Threshold: 1, Signers: []Signer{{self, 1}, {other, 1}}}},
			[]string{RuleForeignOwner},
		},
		{
			"two other keys together reach the owner threshold",
			[]Permission{{ID: 0, Type: Owner, Threshold: 2, Signers: []Signer{{self, 1}, {other, 1}, {second, 1}}}},
			[]string{RuleForeignOwner},
		},
		{
			"owner multisig that still needs the account key",
			[]Permission{{ID: 0, Type: Owner, Threshold: 2, Signers: []Signer{{self, 1}, {other, 1}}}},
			nil,
		},
		{
			"one delegate can transfer alone",
			[]Permission{{ID: 0, Type: User, Name: "ops", Threshold: 1, Operations: ops(t, "01"), Signers: []Signer{{other, 1}}}},
			[]string{RuleSingleSignerFunds},
		},
		{
			"transfers need two delegates",
			[]Permission{{ID: 0, Type: User, Name: "ops", Threshold: 2, Operations: ops(t, "01"), Signers: []Signer{{other, 1}, {second, 1}}}},
			nil,
		},
		{
			"delegate limited to voting",
			[]Permission{{ID: 0, Type: User, Name: "gov", Threshold: 1, Operations: ops(t, "0040"), Signers: []Signer{{other, 1}}}},
			nil,
		},
		{
			"delegate may rewrite permissions",
			[]Permission{{ID: 0, Type: User, Name: "admin", Threshold: 1, Operations: ops(t, "000040"), Signers: []Signer{{other, 1}}}},
			[]string{RuleCanChangePermission},
		},
		{
			"critical finding sorts before high",
			[]Permission{
				{ID: 0, Type: User, Name: "ops", Threshold: 1, Operations: ops(t, "01"), Signers: []Signer{{other, 1}}},
				{ID: 1, Type: User, Name: "admin", Threshold: 1, Operations: ops(t, "000040"), Signers: []Signer{{second, 1}}},
			},
			[]string{RuleCanChangePermission, RuleSingleSignerFunds},
		},
		{
			"mask with a contract type that has no name",
			[]Permission{{ID: 0, Type: User, Name: "x", Threshold: 1, Operations: ops(t, "00000002"), Signers: []Signer{{other, 1}}}},
			[]string{RuleUnknownOperations},
		},
		{
			"user permission whose only signer is the account itself",
			[]Permission{{ID: 0, Type: User, Name: "self", Threshold: 1, Operations: ops(t, "01"), Signers: []Signer{{self, 1}}}},
			nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, f := range Assess(Account{Address: self, Permissions: tc.perms}) {
				got = append(got, f.Rule)
			}
			if !reflect.DeepEqual(got, tc.rules) {
				t.Fatalf("rules = %v, want %v", got, tc.rules)
			}
		})
	}
}

func TestDiff(t *testing.T) {
	base := Permission{ID: 0, Type: User, Name: "ops", Threshold: 2, Operations: ops(t, "01"), Signers: []Signer{{other, 1}, {second, 1}}}
	with := func(change func(*Permission)) []Permission {
		p := base
		p.Signers = append([]Signer(nil), base.Signers...)
		change(&p)
		return []Permission{p}
	}
	tests := []struct {
		name   string
		before []Permission
		after  []Permission
		kinds  []string
	}{
		{"nothing changed", []Permission{base}, []Permission{base}, nil},
		{"signer order alone is not a change", []Permission{base}, with(func(p *Permission) { p.Signers[0], p.Signers[1] = p.Signers[1], p.Signers[0] }), nil},
		{"permission added", nil, []Permission{base}, []string{ChangePermissionAdded}},
		{"permission removed", []Permission{base}, nil, []string{ChangePermissionRemoved}},
		{
			"default owner permission written down by the chain is not a change",
			nil,
			[]Permission{base, {ID: 1, Type: Owner, Threshold: 1, Signers: []Signer{{self, 1}}}},
			[]string{ChangePermissionAdded},
		},
		{
			"owner permission naming another key is reported",
			nil,
			[]Permission{{ID: 0, Type: Owner, Threshold: 1, Signers: []Signer{{other, 1}}}},
			[]string{ChangePermissionAdded},
		},
		{
			"second owner permission is reported even if it names the account",
			[]Permission{{ID: 0, Type: Owner, Threshold: 2, Signers: []Signer{{self, 1}, {other, 1}}}},
			[]Permission{{ID: 0, Type: Owner, Threshold: 2, Signers: []Signer{{self, 1}, {other, 1}}}, {ID: 1, Type: Owner, Threshold: 1, Signers: []Signer{{self, 1}}}},
			[]string{ChangePermissionAdded},
		},
		{"threshold lowered", []Permission{base}, with(func(p *Permission) { p.Threshold = 1 }), []string{ChangeThresholdLowered}},
		{"threshold raised", []Permission{base}, with(func(p *Permission) { p.Threshold = 3 }), []string{ChangeThresholdRaised}},
		{"signer added", []Permission{base}, with(func(p *Permission) { p.Signers = append(p.Signers, Signer{self, 1}) }), []string{ChangeSignerAdded}},
		{"signer removed", []Permission{base}, with(func(p *Permission) { p.Signers = p.Signers[:1] }), []string{ChangeSignerRemoved}},
		{"weight changed", []Permission{base}, with(func(p *Permission) { p.Signers[0].Weight = 2 }), []string{ChangeWeightChanged}},
		{"operations widened", []Permission{base}, with(func(p *Permission) { p.Operations = ops(t, "0103") }), []string{ChangeOperationsWidened}},
		{"operations narrowed", []Permission{base}, with(func(p *Permission) { p.Operations = ops(t, "") }), []string{ChangeOperationsNarrowed}},
		{"user turned into owner", []Permission{base}, with(func(p *Permission) { p.Type = Owner }), []string{ChangeTypeChanged}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, c := range Diff(Account{self, tc.before}, Account{self, tc.after}) {
				got = append(got, c.Kind)
			}
			if !reflect.DeepEqual(got, tc.kinds) {
				t.Fatalf("kinds = %v, want %v", got, tc.kinds)
			}
		})
	}
}
