package domain

import "fmt"

// Change kinds, stable across versions.
const (
	ChangePermissionAdded    = "permission_added"
	ChangePermissionRemoved  = "permission_removed"
	ChangeSignerAdded        = "signer_added"
	ChangeSignerRemoved      = "signer_removed"
	ChangeWeightChanged      = "weight_changed"
	ChangeThresholdLowered   = "threshold_lowered"
	ChangeThresholdRaised    = "threshold_raised"
	ChangeOperationsWidened  = "operations_widened"
	ChangeOperationsNarrowed = "operations_narrowed"
	ChangeTypeChanged        = "type_changed"
)

// Change is one difference between two reads of an account's permissions.
type Change struct {
	Kind         string `json:"kind"`
	PermissionID int32  `json:"permission_id"`
	Message      string `json:"message"`
}

// Diff lists what changed from before to after, matching permissions by ID.
func Diff(before, after Account) []Change {
	var out []Change
	old := indexPermissions(before)
	cur := indexPermissions(after)

	for _, p := range after.Normalized().Permissions {
		prev, existed := old[p.ID]
		if !existed {
			if isDefaultOwner(after.Address, p) && !hasOwner(before) {
				// The chain writes the implicit owner permission down the first
				// time any permission is set. Control did not change.
				continue
			}
			out = append(out, Change{ChangePermissionAdded, p.ID,
				fmt.Sprintf("%s permission %q added with %d signer(s), threshold %d", p.Type, p.Name, len(p.Signers), p.Threshold)})
			continue
		}
		out = append(out, diffPermission(prev, p)...)
	}
	for _, p := range before.Normalized().Permissions {
		if _, still := cur[p.ID]; !still {
			out = append(out, Change{ChangePermissionRemoved, p.ID, fmt.Sprintf("%s permission %q removed", p.Type, p.Name)})
		}
	}
	return out
}

func diffPermission(prev, p Permission) []Change {
	var out []Change
	if prev.Type != p.Type {
		out = append(out, Change{ChangeTypeChanged, p.ID, fmt.Sprintf("permission %q changed from %s to %s", p.Name, prev.Type, p.Type)})
	}
	switch {
	case p.Threshold < prev.Threshold:
		out = append(out, Change{ChangeThresholdLowered, p.ID, fmt.Sprintf("threshold of %q lowered from %d to %d", p.Name, prev.Threshold, p.Threshold)})
	case p.Threshold > prev.Threshold:
		out = append(out, Change{ChangeThresholdRaised, p.ID, fmt.Sprintf("threshold of %q raised from %d to %d", p.Name, prev.Threshold, p.Threshold)})
	}

	was := map[Address]int64{}
	for _, s := range prev.Signers {
		was[s.Address] = s.Weight
	}
	now := map[Address]int64{}
	for _, s := range p.Signers {
		now[s.Address] = s.Weight
		weight, existed := was[s.Address]
		switch {
		case !existed:
			out = append(out, Change{ChangeSignerAdded, p.ID, fmt.Sprintf("signer %s added to %q with weight %d", s.Address, p.Name, s.Weight)})
		case weight != s.Weight:
			out = append(out, Change{ChangeWeightChanged, p.ID, fmt.Sprintf("weight of %s in %q changed from %d to %d", s.Address, p.Name, weight, s.Weight)})
		}
	}
	for _, s := range prev.Signers {
		if _, still := now[s.Address]; !still {
			out = append(out, Change{ChangeSignerRemoved, p.ID, fmt.Sprintf("signer %s removed from %q", s.Address, p.Name)})
		}
	}

	gained, lost := operationsDelta(prev.Operations, p.Operations)
	if len(gained) > 0 {
		out = append(out, Change{ChangeOperationsWidened, p.ID, fmt.Sprintf("%q can now also send: %s", p.Name, joinTypes(gained))})
	}
	if len(lost) > 0 {
		out = append(out, Change{ChangeOperationsNarrowed, p.ID, fmt.Sprintf("%q can no longer send: %s", p.Name, joinTypes(lost))})
	}
	return out
}

func operationsDelta(prev, cur Operations) (gained, lost []ContractType) {
	for _, c := range cur.Types() {
		if !prev.Allows(c) {
			gained = append(gained, c)
		}
	}
	for _, c := range prev.Types() {
		if !cur.Allows(c) {
			lost = append(lost, c)
		}
	}
	return gained, lost
}

func indexPermissions(a Account) map[int32]Permission {
	out := make(map[int32]Permission, len(a.Permissions))
	for _, p := range a.Normalized().Permissions {
		out[p.ID] = p
	}
	return out
}

// isDefaultOwner reports whether p is the owner permission every account starts
// with: the account's own key, alone, reaching the threshold.
func isDefaultOwner(owner Address, p Permission) bool {
	return p.Type == Owner && len(p.Signers) == 1 && p.Signers[0].Address == owner && p.Signers[0].Weight >= p.Threshold
}

func hasOwner(a Account) bool {
	for _, p := range a.Permissions {
		if p.Type == Owner {
			return true
		}
	}
	return false
}
