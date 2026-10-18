---
title: The adoption pane hides reject behind its scrollbar on a short window
status: done
type: bug
priority: 3
created: 2026-10-18T18:44:06Z
closed: 2026-10-18T20:56:55Z
reason: resolved by plan 2026-10-18 - 02 - exec-pin-followups-plan (archived at 86afb7c1), item 9
---

At 50x21-22 the adoption / Allow-rule pane's wrapped answer glosses push the **reject** answer below the pane's fold, behind its scrollbar (still reachable with ↓). Before the run's item 9 (`e2d1ad58`) the pane showed all three answers with the glosses cut. `docs/layout/user-questions-layout.md` is silent on a window too short for the answers alone; the pane code is `internal/tui/adoption.go` and `internal/tui/adoptionbody.go`.

Deferred out of plan `2026-10-18 - 00 - project-config-exec-keys-plan`.

## Acceptance

- `docs/layout/user-questions-layout.md` states what the pane does when the answers with their wrapped glosses do not fit (for example: glosses give up lines before an answer does).
- At 50x21 every answer label is visible without scrolling, and a frame test in `internal/tui/` pins it.
