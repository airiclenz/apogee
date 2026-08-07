---
Status: accepted
---

# The Windows console is prepared before the alternate screen

## Context

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

## Decision

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

## Considered options

- **Drop the early `claimAltScreen` and let bubbletea own the switch** — rejected: the early claim is
  load-bearing on macOS (Terminal.app's lit scroll bar), and it is not what is wrong. The console
  mode is. Moving the mode is a two-call addition; moving the claim would trade a Windows bug for a
  macOS one.
- **Let bubbletea's own save/restore put the console mode back** — rejected: it samples the mode
  *after* `prepareAltScreenConsole` has changed it, so it would faithfully restore apogee's mode and
  hand the shell a console that staircases on the next bare `LF`. The closure is the only thing that
  still holds the mode word as it was before apogee touched anything.
- **Set the mode and never restore it** — rejected for the same reason, plus the general one: a TUI
  that quits owes the shell the terminal it borrowed.
- **Fix it in the renderer** — emit `\r` plus a cursor-forward instead of a bare `LF`, or take the
  one-column-short mitigation the H1 branch had drafted — rejected: the stream apogee emits was
  measured *correct* for the terminals it is emitted to. Rewriting a correct stream to survive a
  misconfigured console would have hidden the real defect and cost a column of every frame forever.
- **File issues against `charmbracelet/ultraviolet` and `microsoft/terminal`** — rejected, and
  withdrawn after being drafted: neither has a measured defect to point at. The wrap is deferred
  everywhere it was suspected of being immediate, and the mode word being per-buffer is documented
  Win32 behaviour that apogee was using wrongly.
- **Bump or patch the Charm dependencies and re-test** — rejected: this is what the previous session
  spent itself on. Nothing in the stack had changed; the ordering had always been ours.
- **Leave `TERM` empty and teach the renderer its capabilities some other way** — rejected: `TERM`
  is the single input ultraviolet's `xtermCaps` reads, so anything else is a fork of the capability
  table. Naming the terminal is the supported mechanism.
- **Set `TERM` in the process environment instead of the injected slice** — rejected: every tool
  call, shell command and child process would inherit a terminal type apogee made up, which is a
  much larger claim than the one being made.
- **Keep synchronized output on Windows anyway** ("it costs nothing") — rejected per the measurement
  in decision 3: it costs the cursor-hide flicker mitigation, and buys an empty window.
- **Delete the trace/diag seams and the probe once the bug was fixed** — rejected per decision 4.
  The next rendering bug will need exactly these artifacts, and a patched-dependency build is not a
  measurement of the shipped program.

## Consequences

- **Windows Terminal and VS Code now paint the same frames macOS and Linux always did.** Confirmed
  by the owner on a build of this tree: no corruption in streamed text, no ghost behind the activity
  spinner, scrolling correct.
- **The console-mode restore rides `Run`'s defer path.** A process killed outright (`SIGKILL`, a
  closed window, a crash inside the renderer) leaves the mode as apogee set it, exactly as it leaves
  the alternate screen claimed — inherent to the mechanism, and the same exposure every terminal
  program on the platform has.
- **`apogee probe terminal`'s capability section still compares against `noCaps` on Windows, and
  that is a known limitation of the plan's item-5 acceptance row rather than a defect in the fix.**
  The probe reads the *process* `TERM` (`cmd/apogee/probeterminal.go`), which decision 2
  deliberately never sets, so it reports MISMATCH for capabilities the painter now genuinely has.
  What the injection is actually pinned by is `internal/tui`'s environment-builder tests and
  `TestInjectedEnvironmentResolvesTheSameColorProfile`, both of which have been watched fail. A
  later change could give the probe the same naming rule, or an explicit "assume this `TERM`"
  argument; until then the row is not answerable as written and the plan records it as such.
- **Three Windows-only rules now live as `_windows.go` / `_other.go` twins** — `altscreen_*`,
  `environ_*`, `syncoutput_*` — and the six-target cross-build is the gate that keeps their
  non-Windows halves honest. `internal/tui/doc.go`'s file map names them all.
- **The regression is pinned by a headless pseudoconsole test** that was seen red against a
  pre-fix build and green after, asserting on the rendered buffer rather than the byte stream — a
  byte-stream assertion would re-encode the renderer's own model and pass while the screen is wrong.
  It skips cleanly where a pseudoconsole cannot be created, and off Windows.
- **ADR 0011 and ADR 0030 are untouched.** Nothing here changes the Model, the value-copy invariant,
  or the width authority; the whole record is about the console apogee hands the painter and the
  order in which it does so.
- This is a user-visible fix that arrives beside a new subcommand (`apogee probe terminal`) and a new
  key (`⌃l`), so it **warrants a bump** when the next version is cut. That call is the owner's, and
  no item of the implementing plan touches a version identifier.
