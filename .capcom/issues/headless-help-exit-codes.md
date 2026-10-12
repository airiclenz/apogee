---
title: headless --help lists exit codes 0/1/2 and omits 3
status: done
type: bug
priority: 3
aliases: [apogee-o4w]
created: 2026-09-16T06:55:44Z
closed: 2026-09-16T17:40:36Z
reason: "fix(headless): --help names exit code 3 and the unanswered-server case"
---

Found by the 2026-09-16 docs fact-check. cmd/apogee/headless.go:470-473 Long text names exit codes 0, 1 and 2 only; the manual (docs/manual/headless.md) and the code both have a fourth, exit 3. Add it to the help text.
