# 0004 — Alert on vault level changes, with the level kept in memory

- Status: accepted
- Date: 2026-10-03

## Context

`watch` checks each vault every cycle. A vault that has used 85% of its limit
stays at 85% for the rest of the period, so alerting on every check would send
the same message every minute. The watcher needs to know what it already said.

## Decision

Give each vault a level: none, warning line crossed (`low`), limit reached
(`high`). Alert only when the level rises, and let it fall silently when a new
period resets the spending. Keep the last delivered level in memory, not in the
snapshot store.

## Reasons

- **Two messages per period at most**, each at a moment an operator can act on:
  "getting close" and "nothing left".
- **A missed delivery is retried.** The level advances only after the notifier
  succeeds.
- **The level is derived state.** It is recomputed from the chain on every check;
  only "did I already say it" is lost on restart, and the cost of losing it is
  one repeated alert, which is better than a silent one.

## Consequences

- After a restart, a vault still over its warning line alerts once more.
- A vault that crosses the line and returns below it between two polls (owner
  raised the limit) produces no alert.
