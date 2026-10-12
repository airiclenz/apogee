---
title: Session system follow-ons — deliberately deferred from the 2026-07-24 session-system plan
status: done
type: task
priority: 3
plan: 2026-09-19 - 01 - ten-urgent-beads-plan
aliases: [apogee-3b3]
created: 2026-09-03T18:40:19Z
closed: 2026-09-20T06:57:55Z
reason: implemented by docs/plans/archived/2026-09-19 - 01 - ten-urgent-beads-plan.md (merged to main, closeout 5a415e13)
---

**Status:** recorded 2026-07-24 at the session-system close-out
([ADR 0022](docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md)). Named
out of scope in the plan and left for later — it is not a live gap.

- **[P2] Cross-instance file lock** — concurrent apogee instances are **last-write-wins per
  file**. Ids are per-instance unique, so a clobber requires the *same* session resumed into two
  instances at once; documented and accepted, not locked. A cross-instance lock (or a
  resumed-elsewhere detection) is the follow-on if that narrow case ever bites.

Explicitly **not** deferred (deliberate non-goals, so they are not re-opened as gaps):
session search, session export, sub-agent session persistence (tool-call arguments ARE
recorded per card, bounded, since plan `2026-08-29 - 02`), and serializing
mode/approvals/confinement/MCP into the record (the last per ADR 0008). The bench keeps composing
`Snapshot`/`Encode` directly (ADR 0001) — no bench-side session-store TODO.
