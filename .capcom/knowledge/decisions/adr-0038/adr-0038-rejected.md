---
title: ADR 0038 considered options
---

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
