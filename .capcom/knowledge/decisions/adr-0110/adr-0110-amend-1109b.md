---
title: "ADR 0110 amendment of 2026-11-09 (b): a 44-column default, a 36-column gate and the chrome's columns"
refs: [docs/layout/side-panel-layout.md, internal/tui/sidepanel.go]
---

**Why now.** The side-panel visual alignment (plan
[`2026-11-09 - 04 - side-panel-visual-alignment-plan.md`](../../../../docs/plans/2026-11-09%20-%2004%20-%20side-panel-visual-alignment-plan.md),
issue `panel-spacing-and-margins`) gave the column the chrome's columns: a left gutter as wide as
the transcript's body indent and a right margin that lines the column's right-edge cells up with
the status line's last cell. That chrome costs the content four columns the old one-cell gutter
did not, so the owner raised the default width on 2026-11-09 ("Default width" and "Dock gate" in
the plan's ratified design calls) to give the body text its room back, and kept the gate where it
was.

**What this supersedes.** `adr-0110-amend-1108` (a)'s "36 by default" and its "the show/hide gate
… is still computed with the default 36", (b)'s "the gutter column to its right" and "resets the
width to 36", and (c)'s "default 36; zero means unset and resolves to 36" and "resets it to 36".
Decision 7's 36-column width was already superseded by that amendment. Each is to be read as this
amendment restates it.

**(a) The default width is 44.** `ui.side-panel.width` defaults to 44 columns; zero means unset and
resolves to 44, and a double-click on the separator or `/panel width reset` puts back 44. A width
the user set is unchanged. The limits of `adr-0110-amend-1108` (a) hold: at least 24, at most the
window less the transcript floor less the scroll-bar column.

**(b) The gate keeps its 36.** The show/hide gate is computed with a gate width of its own, 36
columns, not with the default width: the column still docks from 137 columns in `auto` and 117 in
`on`, whatever width the user set. Between 137 and 145 columns in `auto` the default-width column
docks clamped narrower than 44.

**(c) The column's chrome.** Right of the separator stands a blank gutter as wide as the
transcript's body indent (two cells), then the content, then a right margin of the same two cells,
so a header's fold indicator and a row's right-aligned facts sit in the status line's last cell's
column. Where the content would be narrower than a full sub-agent row, the right margin yields
first and then the gutter's second cell, a cell at a time, before any fact is cut; the gutter's
first cell, where the cursor's `▌` stands, never yields.

**(d) The grip.** The grip of `adr-0110-amend-1108` (b) is the separator column and the one column
right of it, whatever the gutter yields, so a press on a header's pin glyph never starts a drag.
