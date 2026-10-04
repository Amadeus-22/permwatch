// Package domain holds the KleverChain account-permission model and the rules
// that judge it. It does no I/O and imports nothing from the other layers.
package domain

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Sentinel errors.
var (
	ErrInvalidAddress  = errors.New("invalid klever address")
	ErrAccountNotFound = errors.New("account not found")
)

// Address is a bech32 KleverChain address ("klv1…").
type Address string

const (
	addressPrefix  = "klv1"
	addressLength  = 62
	bech32Alphabet = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"
)

// ParseAddress checks the shape of a KleverChain address: prefix, length and
// bech32 alphabet. It does not verify the checksum; the API rejects a wrong one.
func ParseAddress(s string) (Address, error) {
	s = strings.TrimSpace(s)
	if len(s) != addressLength || !strings.HasPrefix(s, addressPrefix) {
		return "", fmt.Errorf("%w: %q", ErrInvalidAddress, s)
	}
	for _, c := range s[len(addressPrefix):] {
		if !strings.ContainsRune(bech32Alphabet, c) {
			return "", fmt.Errorf("%w: %q", ErrInvalidAddress, s)
		}
	}
	return Address(s), nil
}

// PermissionType separates the owner permission, which may send any transaction,
// from user permissions, which are limited to their Operations.
type PermissionType int

const (
	Owner PermissionType = 0
	User  PermissionType = 1
)

func (t PermissionType) String() string {
	if t == Owner {
		return "owner"
	}
	return "user"
}

// ContractType is the KleverChain transaction contract type a permission can allow.
type ContractType int

// Contract types permwatch reasons about. The numbering is the chain's.
const (
	Transfer                ContractType = 0
	Withdraw                ContractType = 8
	Claim                   ContractType = 9
	AssetTrigger            ContractType = 11
	Buy                     ContractType = 17
	Sell                    ContractType = 18
	UpdateAccountPermission ContractType = 22
	SmartContract           ContractType = 63
)

var contractNames = map[ContractType]string{
	0: "Transfer", 1: "CreateAsset", 2: "CreateValidator", 3: "ValidatorConfig",
	4: "Freeze", 5: "Unfreeze", 6: "Delegate", 7: "Undelegate", 8: "Withdraw",
	9: "Claim", 10: "Unjail", 11: "AssetTrigger", 12: "SetAccountName",
	13: "Proposal", 14: "Vote", 15: "ConfigITO", 16: "SetITOPrices", 17: "Buy",
	18: "Sell", 19: "CancelMarketOrder", 20: "CreateMarketplace",
	21: "ConfigMarketplace", 22: "UpdateAccountPermission", 23: "Deposit",
	24: "ITOTrigger", 63: "SmartContract",
}

// Known reports whether permwatch has a name for the contract type.
func (c ContractType) Known() bool {
	_, ok := contractNames[c]
	return ok
}

func (c ContractType) String() string {
	if name, ok := contractNames[c]; ok {
		return name
	}
	return fmt.Sprintf("Unknown(%d)", int(c))
}

// Operations is the bitmask of contract types a user permission allows. Contract
// type n is bit n%8 (least significant first) of byte n/8, the layout the node
// uses when it validates a transaction against a permission.
type Operations []byte

// MaxOperationsSize is the largest mask the chain accepts, in bytes.
const MaxOperationsSize = 8

// ParseOperations decodes the hex mask the API returns ("" means no operation).
func ParseOperations(hexMask string) (Operations, error) {
	raw, err := hex.DecodeString(hexMask)
	if err != nil {
		return nil, fmt.Errorf("decode operations %q: %w", hexMask, err)
	}
	if len(raw) > MaxOperationsSize {
		return nil, fmt.Errorf("decode operations %q: longer than %d bytes", hexMask, MaxOperationsSize)
	}
	return Operations(raw), nil
}

// Allows reports whether the mask grants the contract type.
func (o Operations) Allows(c ContractType) bool {
	if c < 0 {
		return false
	}
	i := int(c) / 8
	if i >= len(o) {
		return false
	}
	return o[i]&(1<<(uint(c)%8)) != 0
}

// Types lists the granted contract types in ascending order.
func (o Operations) Types() []ContractType {
	var out []ContractType
	for i, b := range o {
		for bit := 0; bit < 8; bit++ {
			if b&(1<<bit) != 0 {
				out = append(out, ContractType(i*8+bit))
			}
		}
	}
	return out
}

func (o Operations) String() string { return hex.EncodeToString(o) }

// MarshalJSON writes the mask as the hex string the chain API uses.
func (o Operations) MarshalJSON() ([]byte, error) { return json.Marshal(o.String()) }

// UnmarshalJSON reads the hex form written by MarshalJSON.
func (o *Operations) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("decode operations: %w", err)
	}
	parsed, err := ParseOperations(s)
	if err != nil {
		return err
	}
	*o = parsed
	return nil
}

// Signer is one key of a permission and the weight its signature carries.
type Signer struct {
	Address Address `json:"address"`
	Weight  int64   `json:"weight"`
}

// Permission lets its signers send transactions for the account once the
// weights of the signatures reach Threshold.
type Permission struct {
	ID         int32          `json:"id"`
	Type       PermissionType `json:"type"`
	Name       string         `json:"name"`
	Threshold  int64          `json:"threshold"`
	Operations Operations     `json:"operations"`
	Signers    []Signer       `json:"signers"`
}

// Allows reports whether the permission may send the contract type. The owner
// permission may send anything, whatever its mask says.
func (p Permission) Allows(c ContractType) bool {
	return p.Type == Owner || p.Operations.Allows(c)
}

// Account is an address and its permissions as read from the chain. An account
// that never set permissions has none: only its own key signs for it.
type Account struct {
	Address     Address      `json:"address"`
	Permissions []Permission `json:"permissions"`
}

// Normalized returns a copy with permissions ordered by ID and signers by
// address, so two reads of the same on-chain state compare equal.
func (a Account) Normalized() Account {
	out := Account{Address: a.Address, Permissions: make([]Permission, len(a.Permissions))}
	for i, p := range a.Permissions {
		p.Signers = append([]Signer(nil), p.Signers...)
		sort.Slice(p.Signers, func(x, y int) bool { return p.Signers[x].Address < p.Signers[y].Address })
		p.Operations = append(Operations(nil), p.Operations...)
		out.Permissions[i] = p
	}
	sort.Slice(out.Permissions, func(x, y int) bool { return out.Permissions[x].ID < out.Permissions[y].ID })
	return out
}
