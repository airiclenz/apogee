---
title: Panel sections cannot be folded and use no fold marker
status: done
type: feature
priority: 2
labels: [tui]
plan: 2026-11-09 - 04 - side-panel-visual-alignment-plan.md
created: 2026-11-09T18:56:44Z
closed: 2026-11-09T22:59:19Z
reason: resolved by plan '2026-11-09 - 04 - side-panel-visual-alignment-plan' (archived at 21eb9fbf), item(s) 3-6
---

**Today**
- A section header reads `◆ Title summary … ⊙/●`. Its right-edge cell holds the pin glyph
  (`internal/tui/sidepanel.go:424-434`).
- A click on the header title does nothing. Per ADR 0111 D4, `⏎` on a header toggles the pin.
- A user cannot fold a section at all.

**The rest of the UI**
- Every foldable transcript header wears `▶` (folded) or `▼` (open) at its right edge
  (`stateIndicator`, `internal/tui/blockstate.go:78-83`, painted in `toolIndicator`). `▶` becomes `►`
  under `ui.glyphs: safe`.
- A header click or `⏎` toggles the fold (`docs/layout/tool-layout.md` §Fold states;
  `toggleAtBlockCursor`, `internal/tui/blockcursor.go:628`).
- The rule is "the indicator appears exactly where a click toggles" (`layout.md`).

**Wanted (owner decisions 2026-11-09)**
- Headers take the transcript's shape: `<pin> Title (summary) ⋯⋯ ▶/▼`.
  - The fold indicator sits at the right edge, through `stateIndicator`/`toolIndicator`, with the
    leader in `toolLeader`.
  - A header click or `⏎` folds and unfolds the section.
- The pin becomes the header's lead glyph: `●` when pinned, `○` when auto. It replaces `◆`.
  - A click on that cell toggles `pinned` ↔ `auto`, as the right-edge pin does today.
  - The keyboard path to the pin is part of the design: a second stop on the pin cell, or
    `/panel pin` accepted as the keyboard path. ADR 0111's rejected list refuses pinning by
    `/panel pin` alone, so this needs a dated amendment to ADR 0111 D4, D4 being the rule that `⏎`
    on a header pins.
- A user's fold persists in config, written silently like `ui.task-list-open` / `ui.tools-open`.
  - Fold is orthogonal to `pinned | auto | hidden` (`internal/domain/sidepanelprefs.go:173-179`),
    so it is not a fourth mode. A per-entry flag in `ui.side-panel.sections` is one shape.
  - While watching a session, it follows the `ui.task-list-open` precedent.
- A user-folded section costs exactly one row in the overflow allocation, and nothing unfolds it
  by itself.

Spec, `layout.md` §"The side panel", ADR 0111 (amendment) and the glossary Note `side-panel` change
with it. `panel-overflow-fold-marks` then makes overflow folds wear the same marker.

## Acceptance

- A section header renders `● Title (summary) ⋯ ▼` / `○ … ▶` in the transcript's indicator and leader tones; `ui.glyphs: safe` shows `►`.
- A header click and `⏎` in navigation mode fold and unfold the section; a click on the lead pin glyph toggles pinned/auto and persists as today.
- The fold survives a restart (config written once per toggle, failed save says `not saved`).
- ADR 0111 carries a dated amendment for D4, named on its Status line; the spec's Pointer and Keyboard sections and the glossary Note say the new rules.
