---
title: In-session Firing scrubs the entry description from orientation; other Drivers do not
status: done
type: task
priority: 3
plan: 2026-11-03 - 01 - open-bugs-plan
created: 2026-11-02T20:02:20Z
closed: 2026-11-03T17:58:59Z
reason: "Resolved by item 4 of plan '2026-11-03 - 01 - open-bugs-plan' (archived in b3ffc752): an in-session Firing carries the entry description like every other Driver; ADR 0083 part 4 amended."
---

Found by docs/reviews/architecture-review-2026-11-02.html, verified 2026-11-02 at 00d5bf9f. wire_settings.go firingBinding clears the server entry's description, changing model-facing orientation bytes for in-session Firings only ('decision not taken' comment). Kept byte-identical by plan '2026-11-02 - 04 - seat-and-resolved-entry-plan.md'; parity is its own decision.

## Acceptance

- Decided and pinned: every Driver orients the same way, or the difference is recorded in an ADR.
