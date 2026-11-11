---
title: A command that exited 0 is reported as "could not run" when a child it spawned keeps stdout or stderr open
status: done
type: bug
priority: 2
created: 2026-11-10T19:35:13Z
closed: 2026-11-11T06:36:33Z
reason: resolved by audit-defects plan (2026-11-10 - 02 - audit-defects-plan), archived at e5e5bb54
---

Source: docs/reviews/code-audit-2026-11-10.md ("A command that ran is reported as 'could not run' when a detached child holds stderr"). Verified 2026-11-10 at a5c4f2db.

**Where:** `internal/userexec/userexec.go:157-185`. Callers: `internal/config/keyresolve.go:353` (api-key-cmd), `internal/keystore/run.go:85` (credential-store helper), `internal/reactions/command.go:70` (Reaction `run:` handlers).

**What:** `Run` captures stderr (and stdout when `WantStdout`) through pipes with `cmd.WaitDelay = WaitGrace` (2 s). When the command exits 0 but a child it started still holds a pipe, `Wait` returns `exec.ErrWaitDelay` in place of nil. The child can be a plain `helper &` in the same process group (the group is killed only in `Reap`, after `Wait` returns) or a setsid escapee. `ErrWaitDelay` is not `*exec.ExitError` and the deadline did not fire, so `Run` reports `could not run X: exec: WaitDelay expired before I/O complete`. Non-zero exits are unaffected (Go only replaces a nil error). `internal/subprocess` already treats this as `DrainWedged` (`subprocess.go:403-407`), not a failure to run. No test pins the exit-0 case.

**Why it matters:** an `api-key-cmd:` that starts a background agent or cache helper fails to resolve its key although it printed it and exited 0 — the captured stdout is discarded and the server is unusable. A Reaction `run:` notifier that backgrounds a sound player reads as a failed firing. Each also costs the 2 s grace.

**Suggested fix:** treat `errors.Is(runErr, exec.ErrWaitDelay)` as ran-to-status (exit 0, captured stdout/stderr kept), as `subprocess` does; optionally a `Result` flag (e.g. `DrainCut`) noting output may be incomplete. Update the `Run` doc comment.

## Acceptance

- A test running a script that backgrounds a long sleep inheriting stderr and then exits 0 gets a nil error and ExitCode 0 from Run, within WaitGrace plus a margin.
- With WantStdout set, the stdout printed before exit is returned in that case (an api-key-cmd that backgrounds a helper resolves its key).
- A script that backgrounds a helper and exits non-zero still reports its exit status, and the deadline case still reports TimedOut.
