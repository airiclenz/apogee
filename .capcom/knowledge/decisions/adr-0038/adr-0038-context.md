---
title: ADR 0038 context
---

For as long as apogee has had a TUI it has ghosted on Windows: under Windows Terminal and VS Code's
integrated terminal, fragments of an earlier frame survived a repaint. Streamed text arrived
corrupted, the activity spinner left a trail of dead glyphs behind it, scrolling smeared, and
nothing short of resizing the window put the screen back. A classic conhost window never showed it.
macOS and Linux never showed it. The bug was old, reproducible by eye, and unexplained — a previous
session had already spent itself on a dependency bump made on a guess.

`docs/plans/2026-08-06 - 04 - windows-tui-ghosting-plan.md` is the investigation that ended it, and
its 49 numbered findings are the evidence behind everything below. Three things about that record
matter here, because a reader who only sees the fix will otherwise rediscover them.

**The first four hypotheses were all wrong, and three of them were wrong in an interesting way.**
The plan ranked four: H1, a last-column / pending-wrap disagreement between what ultraviolet emits
and what ConPTY does; H2, `noCaps` — the renderer's capability set collapsing to nothing because
`TERM` is empty on Windows; H3, hard tabs; H4, a width disagreement. H3 and H4 fell to
single-variable A/B runs. H1 was **confirmed on a trace replay and then falsified by direct
measurement**: `apogee probe terminal` writes a glyph into the final column and reads the cursor
back, and the wrap is *deferred* in conhost, in Windows Terminal and in VS Code alike — including in
both terminals that ghost. The replay had reproduced the symptom under a wrap model that the
terminals turned out not to have, which is exactly the confident-sounding wrong answer the plan was
built to prevent. H2 survived only as the **amplifier**: a renderer with no CHA and no HPA has no
absolute column addressing with which to re-anchor, so one bad column becomes permanent corruption
rather than one bad frame. Mode 2026 (synchronized output) was investigated as a fifth candidate on
the way past and also falsified as a cause.

**The real cause is apogee's own start-up ordering, and it is one line of sequencing.** A Windows
console screen buffer that does not carry `DISABLE_NEWLINE_AUTO_RETURN` rewrites every bare `LF` an
application writes as `CR LF`. ultraviolet depends on the untranslated meaning: it emits `\r\n` when
it wants the next row at column 1 and a bare `\n` when it wants the next row at the **same column**,
and it then addresses cells relative to that preserved column. Collapse the two and every such row
is painted 1-based instead of column-relative, so the cells the renderer believes it has just
overwritten are never touched and survive on screen. bubbletea *does* set that flag — but the mode
word is **per screen buffer**, and `internal/tui`'s `Run` claimed the alternate screen *before*
bubbletea initialised the terminal. The flag therefore landed on the primary buffer while every
frame was written to the alternate one, which kept the translating default. Under a pseudoconsole VT
processing is on from the start, so the early switch is honoured and the bug fires; in a classic
conhost window VT processing is off at that moment, the early switch is ignored, the alternate
buffer is entered later by the renderer, and the mode is already correct by then. That is the whole
of the conhost/Windows-Terminal split that had looked like an emulator difference for months.

**Nothing was owed upstream.** The plan had drafted an issue against `charmbracelet/ultraviolet` and
considered one against `microsoft/terminal`; both were withdrawn once the wrap measurement came
back, and the console-mode finding removed the last reason for either. The byte stream apogee emits
is correct for the terminals it is emitted to. No issue has been filed and none should be.
