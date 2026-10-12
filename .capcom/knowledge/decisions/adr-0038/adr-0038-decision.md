---
title: ADR 0038 decision
---

**1. Apogee sets the Windows console mode itself, before it claims the alternate screen, and gives
the shell's own mode back when it stops.** `prepareAltScreenConsole` (`internal/tui/altscreen_windows.go`,
with a no-op `altscreen_other.go` twin — the `internal/platform/prewarm_*.go` pattern) reads the
current mode off stdout's console handle and ORs `ENABLE_VIRTUAL_TERMINAL_PROCESSING` and
`DISABLE_NEWLINE_AUTO_RETURN` onto it. `Run` calls it immediately before `claimAltScreen`, so the
alternate buffer **inherits** a mode that already agrees with what the renderer assumes, and
bubbletea's own later `SetConsoleMode` lands on a buffer that is already correct.

Three properties are load-bearing rather than incidental. It is **additive** — the existing mode
word is preserved and nothing it does not name is changed. It **cannot fail the run**: a console
that refuses the mode leaves apogee exactly where it was before the function existed, which is a bug
we know how to live with and not a reason to refuse to start. And it **returns a restore closure**
that `Run` defers unconditionally (never nil, a no-op where nothing was changed), because bubbletea
samples the output mode for its own restore *after* this call has already changed it: left to
bubbletea, every apogee session would hand the shell back a console with newline-auto-return
disabled, and the next program to write a bare `LF` would staircase down the screen.

The early claim itself stays. It is load-bearing on macOS — Terminal.app leaves the scroll bar lit
for the whole run without it — so the ordering is kept and the console mode is moved ahead of it.

**2. Apogee names the terminal it is talking to on Windows, and only there.** Windows shells do not
follow the Unix `TERM` convention: `TERM` is empty in conhost, Windows Terminal and VS Code alike
(measured, not assumed). ultraviolet's `xtermCaps` switches on the first `-`-separated field of
`TERM`, `""` matches no case, and the renderer is handed `noCaps` — no VPA, no HPA, no CHA, no ECH,
no ICH, no REP, no SD/SU. `programEnviron` (`internal/tui/environ_windows.go`) therefore injects
`TERM=xterm-256color` when `TERM` is unset or empty, plus `COLORTERM=truecolor` when `COLORTERM` is
unset, and only when stdout is a terminal.

The bounds are the decision as much as the values are. The injected slice is **bubbletea's alone**,
passed through `tea.WithEnvironment`; the process environment is never mutated, so no child process,
no tool call and no shell command inherits a `TERM` apogee invented. A `TERM` the user or a
WSL/Cygwin/MSYS shell already set is **never** overridden. `xterm-256color` is chosen because it
yields exactly the capability set macOS already runs with (`allCaps &^ capHPA &^ capCHT &^ capREP`)
— the proven-good configuration rather than a new one. And `COLORTERM=truecolor` is not a nicety:
`colorprofile.Detect` reads the same environment, so naming `xterm-256color` without it would
silently flatten the palette and the spinner gradient to 256 colours.

This is the amplifier removal, and it stands on its own merits. It does not fix the ghost — decision
1 does — but a renderer that believes the terminal cannot address a column is wrong on every modern
Windows terminal, and it is what made every other error unrecoverable.

**3. Apogee declines synchronized output on Windows, on a real terminal.** bubbletea asks every
terminal whether it supports mode 2026 and wraps each frame in BSU/ESU when the answer is yes.
Measured out of a real pseudoconsole, on both arms of an A/B: ConPTY forwards apogee's `CSI ?2026h`
and `CSI ?2026l` back to back as an **empty pair** and re-serializes the frame's cells after that
window has already closed, and the two arms' captures are byte-identical once the empty pairs are
deleted. So the atomicity never arrives — while paying for it costs a mitigation that does, since
bubbletea treats synchronized output and cursor hiding as alternatives and writes cells with the
cursor live when mode 2026 is on. apogee filters the question out of the program's output
(`internal/tui/syncoutput*.go`), so the answer never comes and the renderer runs the
cursor-hide configuration conhost — the one Windows path that never ghosted — already ran.

The asymmetry off Windows is the measurement, not caution: no re-serializing layer stands between
apogee and the terminal elsewhere, the frame really is presented atomically there, and declining it
would buy the tearing BSU/ESU exists to prevent. `programDeclinesSyncOutput` returns false in
`syncoutput_other.go` for exactly that reason.

**4. The instruments stay in the repo, supported, rather than being deleted with the bug.** Four of
them: `--tui-trace` and `--tui-diag` (hidden flags, each a file path, both no-ops when unset — the
exact bytes the renderer wrote, and what the terminal told the program about itself), `apogee probe
terminal` (a measuring command: modes 2026 and 2027 as reported and as they behave, glyph advance
with grapheme clustering off and on, the tab stops and whether a tab erases what it passes, the
last-column wrap, and the terminal's real capabilities beside the ones `xtermCaps(TERM)` claims),
`ctrl+l` (the readline
redraw, which forces a full repaint and is the way back from any smeared frame), and a headless
ConPTY regression test that asserts on the **rendered buffer** rather than the byte stream.

They exist because the alternative was measured too: this investigation had to build apogee against
a locally patched bubbletea to see either artifact, which is both unshippable and a measurement of a
program nobody runs. The seams are portable — the same on every OS — because a rendering bug is
argued from the same two artifacts everywhere.

**5. The falsified hypotheses stay in the record, and nothing is filed upstream.** The plan keeps H1
struck through rather than deleted, keeps the replay that supported it, and keeps the measurement
that killed it. A record that shows only the answer teaches the next reader nothing about how a
plausible mechanism survived a trace replay and died to a direct measurement — and H1 is exactly the
theory a future reader would re-derive from the symptom.
