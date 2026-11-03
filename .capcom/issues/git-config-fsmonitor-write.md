---
title: "`git config core.fsmonitor` is not treated as a git-control-plane write"
status: open
type: bug
priority: 2
labels: [security]
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 6 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. `GitCommandConfigName` (internal/security/shellwrites.go:875-880, also :1270, :1280) puts `core.hookspath` in the write view but not `core.fsmonitor` or the include paths, reasoning that apogee's own git argv neutralises them. That protects only apogee's git tools: `git config core.fsmonitor ./.cache/x` appends no `.git/config` target. `alias.*` stays allowed (owner call, see the plan).

## Why

A planted workspace script registered as fsmonitor runs unconfined the next time the user's shell, editor or `git status` touches the repo.

## Acceptance

- `core.fsmonitor` (non-boolean values), `interactive.difffilter` and `include(if.*)?.path` join the write view; a rule-engine test of `git config core.fsmonitor ./x` refuses.
- ADR 0049 is amended.
