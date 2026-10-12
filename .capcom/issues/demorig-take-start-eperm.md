---
title: "demorig: second e2e take sometimes fails to start with EPERM"
status: done
type: bug
priority: 3
aliases: [apogee-demorig-take-start-eperm]
created: 2026-09-25T10:37:48Z
closed: 2026-09-25T12:26:37Z
reason: "StartTerminal retries an EPERM launch up to 3x on a fresh pty (pre-exec, safe). Root cause not pinned: 20 e2e runs + 2000 pty starts did not reproduce."
---

~1 in 12 e2e runs (APOGEE_DEMO_E2E=1 go test -run TestE2E ./cmd/demorig/): the second take fails to start with 'fork/exec /usr/bin/bash: operation not permitted' from StartTerminal's Setsid+Setctty launch (cmd/demorig/term.go:148). Cause unknown, possibly a test-environment flake. Deferred from plan docs/plans/archived/2026-09-24 - 02 - hero-gif-recorder-plan.md closeout.
