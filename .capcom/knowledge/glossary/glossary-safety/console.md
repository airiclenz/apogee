---
title: Console
---

**Console**:
A persistent interactive program — a REPL, a dev server, a shell — the model opens and drives
across Turns through `console_open` / `console_send` / `console_read` /
`console_close`; the `terminal` tool stays one-shot. A Console is **live host state, never
Session state** (like the Undo journal): it lives
until closed, `/new`, a session restore, or engine exit; a snapshot, fork, or resume inherits
**none**; a delegation's end closes the ones it opened. `open` and `send` carry the Subprocess marker and each
`send` takes its own **Resolution**; `read` and `close` sit on the read-only floor. The program runs
under a pseudo-terminal on POSIX and under a **ConPTY** pseudoconsole on Windows (Windows 10 1809 or
later), there via `cmd /c` inside a kill-on-close Job Object, with Enter sent as a carriage return
(`internal/console`, `platform.StartPseudoConsole`); a Console opened confined runs there under
the Confiner's restricted Low token, so Auto fences it on every host (kill-on-denial stays
POSIX-only). On every host a `send` under a confinement box to a Console that was not opened
confined is demoted to Approval, each send on its own. Ships
**default-off**, profile-enabled (ADR 0057) — the first tool to use that state. See
ADR 0059.
_Avoid_: "terminal session" / "PTY session" (the mechanism, and "session" is the saved record),
"persistent terminal" (`terminal` is the one-shot tool), "shell" (a Console can host any program).
