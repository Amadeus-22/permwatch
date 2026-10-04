# 0003 — Decode the operations bitmask locally, without the chain's protobuf types

- Status: accepted
- Date: 2026-10-03

## Context

A user permission lists what it may send as a hex bitmask (`"operations": "01"`).
To explain it, permwatch needs the bit layout and the contract type names. They
live in the node repository (`klever-io/klever-go`):
`data/transaction/utils.go` (`CheckPermissionGrantedForContract`) and the
`TXContract_ContractType` enum.

## Decision

Reimplement the layout in `internal/domain` (contract type `n` is bit `n % 8` of
byte `n / 8`) and keep a table of the contract type names, instead of importing
the node module.

## Reasons

- **The node module is large** and pulls in the whole chain's dependency tree for
  one function and one enum. `ENGINEERING.md` limits dependencies to a short list.
- **The layout is consensus-relevant and stable**: changing it would break every
  existing permission on chain.
- **Unknown types degrade safely.** A bit with no name is reported by the
  `unknown_operations` rule rather than ignored, so a new contract type shows up
  as a finding until the table is updated.

## Consequences

- The name table must be updated when the chain adds a contract type. Until then
  the type appears as `Unknown(n)` and raises a low-severity finding.
- The layout is covered by table-driven tests in `internal/domain`, and by an
  integration test that reads a real testnet account.
