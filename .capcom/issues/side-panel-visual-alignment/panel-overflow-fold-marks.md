---
title: A section folded by overflow looks open and unfolds by itself
status: done
type: feature
priority: 2
labels: [tui]
deps: [panel-section-user-fold]
plan: 2026-11-09 - 04 - side-panel-visual-alignment-plan.md
created: 2026-11-09T18:56:44Z
closed: 2026-11-09T22:59:19Z
reason: resolved by plan '2026-11-09 - 04 - side-panel-visual-alignment-plan' (archived at 21eb9fbf), item(s) 7
---

**Today**
- Under overflow, `panelAllocation` (`internal/tui/sidepanel.go:298-314`) folds sections to their
  header and unfolds them as rows come and go.
- Nothing marks the folded state: `◆ Changed files 3 ⊙` looks the same open or folded (frame
  `06-overflow-h20`).
- The `+N more` row is faint (`sidepanel.go:253`).

**The rest of the UI**
- The transcript's rule is "nothing ever expands or collapses by itself; only a click changes a
  block's state" (`layout.md`).
- Its own `+N more lines` is painted in the `toolMarker` tone.

**Wanted (owner decision 2026-11-09: "squeeze shows ▶")**
- A section squeezed by overflow wears `▶`, like a user fold.
- Opening a squeezed section keeps that promise: the opened section gets rows ahead of the other
  auto sections, and the last auto section in list order is squeezed in its place.
- Pinned sections still take rows first.
- A user fold (`panel-section-user-fold`) is never undone by allocation.
- `+N more` stays as the fallback when not every header fits, painted in `toolMarker`.
- Optionally, a folded header can carry its hidden-row count in the same tone.

## Acceptance

- On a short window every header folded by overflow shows `▶`; an open one shows `▼`.
- Opening a squeezed section shows its body and squeezes the last auto section instead; pinned sections keep their rows.
- `+N more` paints in the `toolMarker` tone.
- The spec's Overflow section states the new rules.
