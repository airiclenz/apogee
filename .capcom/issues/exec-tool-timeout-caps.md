---
title: "Execution-tool timeouts are unreachable on slow hardware: run_tests is a hard 5m, subprocess caps at 10m"
status: done
type: bug
priority: 2
labels: [slow-box]
aliases: [apogee-v0e8]
created: 2026-09-22T16:03:45Z
closed: 2026-09-23T06:02:03Z
reason: Closed
---

internal/tools/run_tests.go:46 runTestsTimeout = 300 * time.Second is hard-coded (used at :276, quoted at :427) with no way for the model or the user to ask for more — apogee's own suite on a Pi-class machine exceeds five minutes, so the model just gets "— timed out after 5m0s".

internal/subprocess/subprocess.go:28 MaxSubprocessTimeout = 600 * time.Second is a hard CEILING on a caller-named timeout (default 120s at :26), so a cold `go build ./...`, a `pip install` or a container build on a throttled laptop is unreachable through the execution tools at any requested timeout.

Found by the slow-box deadline sweep, 2026-09-22.

## Acceptance

run_tests takes a timeout (or reads one from config); the subprocess ceiling is raised or made configurable. A slow machine makes a build slower, not impossible.

## Log

- 2026-09-23 Airic Lenz: Verified fixed in the tree at 533ce9af (slow-box-deadlines plan, items 5 and 6, commits e262100a and 218c266c). Both halves.

  - internal/tools/run_tests.go: the hard 5m is gone. runTestsTimeout = 900 * time.Second is now only the DEFAULT when the model names no budget, and the tool takes a timeout_seconds argument. runTestsBudget() falls back to the tool's own default on absent/zero (never to the 120s subprocess default a zero Timeout would otherwise select) and hands a positive value through untouched, because internal/subprocess is the single place the ceiling is applied. The schema description is RENDERED from the default and the ceiling, so the model is told both numbers and told to raise the budget rather than re-run a timed-out suite.
  - internal/subprocess/subprocess.go: MaxSubprocessTimeout 600s -> 3600s, so a cold 'go build ./...', a pip install or a container build is reachable at a requested timeout. python_exec.go and terminal.go quote the new ceiling; docs/manual/reactions.md dropped the 600s clamp sentence.

  Acceptance met: a slow machine makes a build slower, not impossible.
