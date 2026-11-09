---
title: "Navigation mode: the modal keyboard cursor over the transcript and the side panel"
refs: [docs/layout/side-panel-layout.md, docs/layout/tool-layout.md]
---

**Navigation mode**:
The TUI's modal keyboard cursor over two regions, the transcript and the **Side panel**: the
keyboard's reach to everything a click there acts on. `⌥↑`/`⌥↓` start it — in the transcript, or in
the panel when the transcript has nothing to stand on — `→` and `←` move the cursor between the two
regions, each remembering where it was, and `↑`/`↓`, `⏎`, `esc` and `^x` mean the same in both: walk
the region's stops, act on the one the cursor stands on, leave the mode (as does typing, which still
lands in the prompt), and stop the sub-agent run whose row it stands on (**Stop (a delegation)**). A
region's stops are its click targets — the transcript's foldable blocks at their deepest visible
level, the panel's section headers and rows, each header two stops (its pin glyph, then its title)
— and `⏎` goes through the click's own path, so key and pointer cannot drift: on a framed delegation
it opens the **Run view**, on a panel header's pin it toggles the section between `pinned` and
`auto` and on its title it folds or opens the section, the mode staying on the stop it acted on
either way (`adr-0111-amend-1109b`), and a panel stop that opens a detail ends the mode. The
cursor's row is marked by a `▌` as well as by the selection tone, so a terminal that paints no
colour still shows it, and only the region it stands in is painted. A pending approval or ask
suspends it, keys and mark together. It is **Driver state** on the TUI's `Model`, never written to a
**Session record**. The transcript half shipped as the block cursor (design call 7 of
`docs/layout/tool-layout.md`); ratified as navigation mode 2026-11-08 (ADR 0111), shipped 2026-11-09.
_Avoid_: "focus" (nothing takes focus: the prompt keeps every key the mode does not claim), "panel
focus", "keyboard mode"; "block cursor" names only its transcript half.
