---
title: "The start-up chrome routes every note (architecture review 2026-11-21 #2)"
status: done
type: task
priority: 3
labels: [tui]
plan: 2026-11-21 - 02 - startup-notes-plan.md
created: 2026-11-21T12:27:13Z
closed: 2026-11-21T20:58:53Z
reason: Acceptance met by plan 2026-11-21 - 02 - startup-notes-plan.md (43bf2196); the two leftover heartbeat notes are tracked in server-switch-notes-unrouted.
---

Found by docs/reviews/architecture-review-2026-11-21.html, verified 2026-11-21 at 171d210f.

Problem: where a note lands (session log or standalone card) and whether it is persisted or ephemeral is decided per call site. Five routers: `noteContextFiles`, `noteRoutine`, `noteStartupNotices` (internal/tui/model.go:1045-1110), `noteRebindRoutine` and the loop in `applyRebind` (heartbeat.go:404-478). `addSessionLogNote` (transcript.go:1070) returns a bool and each caller picks its own fallback, and they disagree: a routine start-up note left over after the log closes becomes ephemeral, a routine first-beat note becomes persisted; loud start-up notices are ephemeral, loud rebind notices, `unknownWindowNote` and `rebindFailNote` are persisted. The heartbeat carries two first-contact flags, `quietSeed` and `firstBeat` (heartbeat.go:122-133); `rebindNote` suppresses notes the restated start-up box already shows (heartbeat.go:585-610). Three boundary sites (model.go:879, commandrun.go:371, sessions.go:960) repeat reset → addStartup(newStartupView) → openSessionLog. Three first-contact special cases landed in two days: 40328c44, 5b008393, 04ea2cc5.

Solution: one module owns the start-up box restated at a boundary, the session log's open and its close at the first Exchange launch, each note's destination and persistence, first-contact absorption and repeat-of-box suppression. The transcript only places what it is told. The 9 tests in sessionlog_test.go and the 5 `TestSessionLog*` tests in heartbeat_test.go stop building a full Model and become a table over class × origin × first beat × log state → destination.

Must keep: ADR 0112 amend-1112 (the residual stays routine; a residual inside the collapsed log counts as disclosed), ADR 0024 amend-1111 (`applyRebind` alone sets `lastRebindFailed`; a refusal is noted once per target), ADR 0011 (notes are copy-on-write, transcript.go:1083). Pairs with #8 (startup-notice-class-scattered).

## Acceptance

- Decided: pursued as a plan or closed as not worth it.

## Log

- 2026-11-21 Airic Lenz: Triage 2026-11-21 at 7869cca7 (read-only sub-agent check, not re-verified): VALID — five routers and disagreeing fallbacks unchanged (internal/tui/model.go:1045-1110, heartbeat.go:473-477); do after startup-notice-class-scattered
- 2026-11-21 Airic Lenz: Plan 2026-11-21 - 02 - startup-notes-plan.md (archived at 43bf2196) landed the startupChrome module: launch-side and heartbeat notes, session boundaries and box restatement route through it. Left open: serverSwitchNote and the 'server fallback: could not follow' note still call transcript.addNote directly (deferred; tracked as server-switch-notes-unrouted).
