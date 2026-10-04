# 0002 — Snapshots in JSON files, not a database

- Status: accepted
- Date: 2026-10-03

## Context

The watcher needs the last permissions seen per account, to survive a restart
without re-alerting or missing a change. `ENGINEERING.md` allows SQLite
(`modernc.org/sqlite`) and Postgres.

## Decision

One JSON file per account in `PERMWATCH_STATE_DIR`, written to a temporary file
and renamed into place.

## Reasons

- **The data is one small document per account**, read and replaced whole. There
  are no queries, joins or history to justify a schema and migrations.
- **Atomic replace is enough.** `rename` on the same filesystem gives old-or-new
  semantics, which is the only consistency the watcher needs.
- **Operable with `cat`.** An operator can read, back up or delete a snapshot
  (to force a new baseline) with standard tools.
- **No dependency added.**

## Consequences

- Only one permwatch process may use a state directory.
- If alert history or many thousands of accounts become requirements, this moves
  to SQLite behind the same `SnapshotStore` port.
