---
title: demorig second-take EPERM was closed with a retry; root cause unknown
status: open
type: bug
priority: 3
created: 2026-10-12T21:51:16Z
---

`demorig-take-start-eperm` was closed on 2026-09-25 with a workaround, not a cause: `StartTerminal` (`cmd/demorig/term.go`) retries an EPERM launch up to 3× on a fresh pty. About 1 in 12 `APOGEE_DEMO_E2E=1` e2e runs saw the second take fail with `fork/exec /usr/bin/bash: operation not permitted` from the Setsid+Setctty launch, and 20 e2e runs plus 2000 pty starts did not reproduce it afterwards. The retry hides whatever makes the second Setctty launch refuse.

**Acceptance:** the EPERM's cause is named (pty/session ownership, a leftover controlling terminal, the environment) and either fixed at the source — after which the retry can go — or documented as an environment property the retry is the right answer to.

Source: `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md` §4, "These were left open and never filed or done"; lever 11 of §8.
