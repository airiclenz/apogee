---
title: "demorig check: a nonexistent or non-git --stage dir aborts before any beat row prints"
status: done
type: bug
priority: 4
aliases: [apogee-vf1]
created: 2026-09-16T17:08:33Z
closed: 2026-09-16T21:12:57Z
reason: demorig check judges an unreadable stage as a FAIL row (ad62694e)
---

Deferred out of the 2026-09-16 - 02 demo-storyboard-rig run. judgeStage (cmd/demorig/check.go:249-261) treats a nonexistent/non-git --stage dir as a hard error (exit 1, `git … exit status 128`) rather than a FAIL row — correct for record.sh (the stage always exists) but a typo'd --stage by hand aborts the table before any beat row prints.
