---
title: ShellCommandTool marker on the real Terminal/ConsoleOpen reaches no journey test
status: done
type: task
priority: 3
labels: [plan-2026-09-14-02, run-residual]
aliases: [apogee-t74]
created: 2026-09-15T11:49:41Z
closed: 2026-09-16T21:15:17Z
reason: "journey pinned: real Terminal shell-write view reaches the guard (615a42f6)"
---

Deferred from plan 2026-09-14 - 02 item 10. No e2e/agent test drives a read-only command such as `ls -la .git/hooks` through the registry's real `*tools.Terminal` (internal/tools/terminal.go:91 ShellCommandKeys) or `*tools.ConsoleOpen` (internal/tools/console_open.go:101) to a TierNone verdict from the write-git-control-plane rule; the security-package cases (internal/security/dangerous_test.go:628) use a stub shaped like the marker. Missing test on delivered work.
