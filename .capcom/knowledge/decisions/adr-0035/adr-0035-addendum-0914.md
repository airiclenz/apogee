---
title: "ADR 0035 addendum of 2026-09-14: the transcript's task-list fold gesture writes `ui.task-list-open`"
---

The set gains its third program-written key — a key apogee records off a gesture in a surface other
than the settings pane, beside `server:` (ADR 0036 decision 2, above) and `launch-profile:` under
`remember-model:` (ADR 0048): **`ui.task-list-open`**,
written by the transcript's task-list fold. A click on a `task_list` card, or `enter` at the block
cursor on one, folds — or opens — every task-list card in the session at once, and the same
keystroke splice-writes `ui.task-list-open: true|false` through this record's writer (decision 2),
so the next session starts the way this one was left. The fence is decision 2's: a keystroke in a
surface the user is looking at, aimed at the card they toggled, and the key is a registry-declared
editable bool that shows in `/settings` and live-applies from a hand-edit like any other. What differs
from the two writers above is the announcement: this one records **silently** — no `saved` line,
because the fold is a glance-frequency gesture and a transcript note per flip would be the wall the
fold exists to avoid — and only a write that fails says so, where the gesture happened, without
unwinding the fold. A session without a settings host (the bench, a Driver that mounts none)
toggles in-session and writes nothing. Plan `docs/plans/2026-09-14 - 00` carries the ratified calls.
