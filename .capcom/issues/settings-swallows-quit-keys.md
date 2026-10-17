---
title: /settings swallows ctrl+c and ctrl+l while open
status: done
type: bug
priority: 3
plan: 2026-10-17 - 00 - quit-keys-stage-kinds-daemon-layer-plan.md
created: 2026-10-14T06:57:53Z
closed: 2026-10-17T12:41:46Z
reason: "Resolved by plan '2026-10-17 - 00 - quit-keys-stage-kinds-daemon-layer-plan' item 1 (archived at c46ba088): ctrl+c and ctrl+l pass through every modal pane, pinned for all six /settings states by framechords_test.go."
---

While `/settings` is open, every key goes to the pane: its key claim is a `modalClaim` (`internal/tui/panes.go`, `Model.settingsKey` in `internal/tui/settings.go`), and the list state swallows every key it does not bind. That includes `ctrl+c`, so the quit double-tap never arms while the pane is open, and `ctrl+l`. No test pins either behaviour, so it is unclear whether this is intended or an accident of the modal claim.

Why it matters: `ctrl+c` is the universal way out of a terminal program. A pane that silently eats it reads as a hang. Found during the `settings-typing-filter` grilling (2026-10-14).

## Acceptance

- Decide whether `ctrl+c` and `ctrl+l` reach their global handlers while `/settings` is open, consistently with the other modal panes (picker, `/sessions`).
- A test pins the decided behaviour for both keys in the key-list state and in each sub-step (value field, text editor, enum sub-list, armed reset, `Save to:`).
