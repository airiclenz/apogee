---
title: Show the ongoing turn's total duration beside the elapsed clock
status: open
type: task
priority: 2
plan: 2026-11-02 - 02 - turn-total-and-open-bugs-plan
created: 2026-11-02T15:11:18Z
---

**What**

While the agent is working, the status line already carries a per-activity elapsed
clock (`thinking · 12s`, `reading · 3s` — the clock whose origin `moveActivity` moves
with the work in flight, `internal/tui/activity.go`). Show beside it — **to the right
of the existing time** — the total duration of the ongoing Turn, bracketed as `[total …]` in the same
value-filled shape the elapsed clock reads. One shared formatter serves both
readouts (`formatElapsed`, `internal/tui/activity.go`, reworked): it fills only the
spots that carry a value — 15s reads `15s`, 3m 15s reads `03:15s`, 1h 2m 15s reads
`01:02:15s` — with the trailing `s` on every shape. This replaces the current
`#s` / `#m ##s` form and supersedes the deliberate no-hour form `formatElapsed`
documents today.

**Why**

The per-activity clock restarts whenever one activity ends and the next begins, so on
a long Turn it never says how long the whole round of work has been going. A total
Turn counter gives the user that at a glance, next to the time they already see.

**Acceptance**

- While the status line shows a live activity, it also shows `[total …]` immediately
  to the right of the activity's elapsed time, counting from the start of the Turn —
  `[total 15s]`, `[total 03:15s]`, `[total 01:02:15s]` as the total grows (the
  per-phrase clock counts the work in flight; the total counts the Turn).
- The total keeps counting across activity changes within the Turn — the per-activity
  clock restarts; the total does not.
- Both readouts come from one shared formatter — the elapsed clock and the
  `[total …]` read the same shape: `15s` at 15 seconds, `03:15s` at 3m 15s,
  `01:02:15s` at 1h 2m 15s — no minutes or hours while they are zero, the trailing
  `s` always there, and the two never diverging in shape.
- Nothing extra is painted when the agent is not working: idle, asking and every other
  state's status line is byte-for-byte as before. The total is only displayed while the
  agent is working.
- The status line already repaints about once a second while running (the
  elapsed-clock repaint), so the total can ride that tick; it must not add a new timer
  of its own.
- Placement and styling match the existing running-line text (the total is part of the
  status line, not a separate row).
