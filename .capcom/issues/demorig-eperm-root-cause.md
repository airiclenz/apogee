---
title: demorig second-take EPERM was closed with a retry; root cause unknown
status: done
type: bug
priority: 3
plan: 2026-10-17 - 02 - film-hero-retire-demorig-plan.md
created: 2026-10-12T21:51:16Z
closed: 2026-10-17T15:44:19Z
reason: "Moot: cmd/demorig retired by film-hero-retire-demorig-plan item 1 (archived e9b14d71); photon carries the same EPERM retry and probe and records one take per process (owner-ratified 2026-10-17)."
---

`demorig-take-start-eperm` was closed on 2026-09-25 with a workaround, not a cause: `StartTerminal` (`cmd/demorig/term.go`) retries an EPERM launch up to 3× on a fresh pty. About 1 in 12 `APOGEE_DEMO_E2E=1` e2e runs saw the second take fail with `fork/exec /usr/bin/bash: operation not permitted` from the Setsid+Setctty launch, and 20 e2e runs plus 2000 pty starts did not reproduce it afterwards. The retry hides whatever makes the second Setctty launch refuse.

**Acceptance:** the EPERM's cause is named (pty/session ownership, a leftover controlling terminal, the environment) and either fixed at the source — after which the retry can go — or documented as an environment property the retry is the right answer to.

Source: `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md` §4, "These were left open and never filed or done"; lever 11 of §8.


## Progress (2026-10-13)

Advanced by `docs/plans/archived/2026-10-13 - 00 - open-bugs-sweep-plan.md` item 7; left open until a real occurrence confirms the cause.

- On an EPERM launch, `startRetryingEPERM` (`cmd/demorig/term.go`) now probes the same pty with three `true` launches — no attrs, `Setsid`, `Setsid`+`Setctty` — and scans `/proc/*/stat` for the pty's controlling-terminal owner (linux only; a stub elsewhere). The report rides the returned error, which still `errors.Is(syscall.EPERM)`. The retry stays.
- **Reading the report:** a failing no-attrs probe means the environment refuses clone/execve; only `Setctty` failing means pty ownership.
- **Likely cause:** a throwaway check reproduced the exact `operation not permitted` text on a pty another session already holds as its controlling terminal (`true` and `true+setsid` ok, `+setctty` refused, holder named).

**Next:** on the next EPERM in an `APOGEE_DEMO_E2E=1` run, read the probe report; if it names a foreign owner, fix the first take's teardown at the source and drop the retry.

## Progress (2026-10-13, later)

- **No reproduction in 60 more runs.** `TestE2ERecordCheckRender` looped 60× on the Pi (Go 1.26.9, kernel 6.18.53) with a temporary, uncommitted hook that logged the probe on *every* EPERM attempt, not only the last: 60/60 green, zero EPERM on any attempt — so the retry is not silently absorbing refusals either. With the earlier 20 runs and 2000 pty starts, that is 80 clean e2e runs since the 1-in-12 rate of 2026-09-25.
- **What the kernel allows, read from the source.** Go's child (`syscall/exec_linux.go`) runs setsid, the fd dup, then `ioctl(0, TIOCSCTTY, 1)`; after a successful setsid the only EPERM `tiocsctty` can return is "this tty is already another session's controlling terminal" (arg 1 steals only with `CAP_SYS_ADMIN`). A fresh pty's slave is opened `O_NOCTTY` (creack/pty) and ptmx masters never become a ctty, so the only way in is a *third-party* session leader without a controlling terminal opening `/dev/pts/N` without `O_NOCTTY` in the window between `pty.Open` and the child's ioctl. Nothing in apogee or demorig does that. An agent harness's shell is one such session leader (the Claude Code Bash tool here runs as one), so a sibling command it ran at that moment is the class of process that could; which one, if any, is unknown.
- **Reading:** an environment property of that day's box, not a demorig defect. The probe and the retry stay; this issue stays parked at P3 until a real occurrence prints a probe report naming the holder.
