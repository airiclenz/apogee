---
title: "ADR 0111 amendment of 2026-11-09 (b): a side-panel header is a pin stop and a title stop"
refs: [docs/layout/side-panel-layout.md, internal/tui/panelstops.go]
---

**Why now.** Decision 4 made a section header one stop whose `⏎` toggles its pin, because the
header was then one click target: the pin glyph. The side-panel visual alignment (plan
[`2026-11-09 - 04 - side-panel-visual-alignment-plan.md`](../../../../docs/plans/2026-11-09%20-%2004%20-%20side-panel-visual-alignment-plan.md),
issue `panel-section-user-fold`) split the header row into two click targets: the lead pin glyph
toggles `pinned` ↔ `auto`, and a click anywhere else on the header folds the section to its header
row, or opens a folded one, writing `ui.side-panel.folded`. A header that stayed one stop would
have left the fold with no way in from the keyboard, against Decision 4's own rule that the
panel's stops are its click targets. The owner ratified the shape on 2026-11-09 ("Pin keyboard
path" in the plan's ratified design calls).

**What this supersedes.** Decision 4's sentence "A section header is a stop whose `⏎` toggles its
pin, as the pin glyph's click does". The rest of Decision 4 — stops are the targets
`panelTargetAt` resolves, and `⏎` calls `followPanelTarget` — holds, and is why the change below
is two stops rather than a new key. Decisions 5 and 6 hold, as (b) and (c) apply them.

**(a) A header is two stops, pin first.** Each section header is two stops on its one row, in
this order: its **pin stop**, on the lead pin glyph's cell, whose `⏎` toggles `pinned` ↔ `auto`
exactly as the pin glyph's click does; then its **title stop**, the rest of the row, whose `⏎`
folds or opens the section exactly as a click there does, writing `ui.side-panel.folded` (and so
toggling and writing while watching, as the click does). Both keep navigation mode on, the cursor
on the stop it acted on; only a stop that opens a detail ends the mode. `panelTargetAt` picks
between a header's two stops by the click's column, so every stop is still the click at its own
cell.

**(b) Identity (Decision 5).** The two stops have their own identities — the section plus which
part of the header it is — so a fold, which re-lays out the column under the title stop that made
it, leaves the cursor on that title, and a fold elsewhere that moves the header's row moves the
cursor with it.

**(c) The mark (Decision 6).** A header's two stops share a row, so the selection tone tells them
apart: on the pin stop it covers the pin glyph's cell alone, on the title stop the rest of the row
past the pin. The `▌` in the gutter cell marks the row either way; a terminal painting no colour
shows the row, not which of its two stops the cursor is on.
