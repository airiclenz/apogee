---
title: console_open skips the env scrub Terminal.Execute applies to rule-answered calls
status: open
type: task
priority: 3
labels: [security]
plan: 2026-11-28 - 00 - audit-bounds-guards-deadlines-plan.md
created: 2026-11-27T21:27:15Z
---

`internal/tools/console_open.go:182`: `Terminal.Execute` removes `CDPATH`, `BASH_ENV`, `ENV` and
`BASH_FUNC_*` when an Allow rule answered the call. `console_open` runs the same shell line
(`ArgRoleShellCommand`) and dispatch marks it rule-answered, but it never calls
`scrubRuleAnsweredEnv`.

An Allow rule on `console_open` (default-off tool) runs with whatever hostile shell-startup
environment the workspace left in apogee's own environment. A sibling-case gap
(`guard-sibling-cases-missed`).

Fix: apply `scrubRuleAnsweredEnv` in `ConsoleOpen.Execute` under the same `domain.RuleAnswered(ctx)`
condition.

Source: `docs/reviews/code-audit-2026-11-27.md` (Medium, independently verified).

## Acceptance

- A rule-answered `console_open` starts without the scrubbed variables; a test pins it, and one test covers every tool with a shell-command argument role.
