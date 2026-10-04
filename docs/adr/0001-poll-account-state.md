# 0001 — Poll the account endpoint instead of indexing transactions

- Status: accepted
- Date: 2026-10-03

## Context

permwatch must notice when an account's permissions change. Two ways to learn it:

1. Follow the chain and look for `UpdateAccountPermission` transactions sent by
   the watched accounts.
2. Read each account's current permissions on an interval and compare them with
   the last read.

## Decision

Poll `GET /v1.0/address/{addr}` on an interval and diff against a stored snapshot.

## Reasons

- **The state is the truth.** What matters is who can sign now. Reading the state
  cannot miss a change because of a transaction type, a contract call or a future
  feature that alters permissions by another path.
- **No backfill.** After downtime the next read shows the current permissions;
  an indexer would have to replay every block it missed.
- **One small request per account.** Watching tens of accounts every minute is far
  below anything the public API would notice.

## Consequences

- A change that is made and undone between two polls is invisible. The interval
  (`PERMWATCH_POLL_INTERVAL`, default 1 minute) bounds that window.
- An alert says what changed, not which transaction did it. The operator looks the
  account up in the explorer.
