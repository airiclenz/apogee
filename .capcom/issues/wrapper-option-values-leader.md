---
title: "`stripWrappers` drops wrapper options without their values, so `env -u echo rm .git/hooks/x` hides the write"
status: open
type: bug
priority: 2
labels: [security]
plan: 2026-11-03 - 01 - open-bugs-plan
created: 2026-11-03T14:20:29Z
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 5 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. `stripWrappers` (internal/security/shellwrites.go:1335-1339, :1429-1434, :939-940) drops wrapper option words without knowing which take a value, so in `env -u echo rm -f .git/hooks/pre-commit` it takes `echo` as a read leader and never emits the `rm` target.

## Why

A hostile model can slip a hook write past `write-git-control-plane`. The guard is a footgun guard, not obfuscation-proof, which keeps this Medium.

## Acceptance

- Known value-taking options are consumed per wrapper (`env -u/-C/-S`, `sudo -u/-g/-h/-p/-C/-D/-R/-T`, `nice -n`, `xargs -I/-n/-s/-P/-d/-E/-a/-L`), or the strip fails closed.
- `env -u echo rm -f .git/hooks/pre-commit` reports the hook target.

## Log

- 2026-11-03 Airic Lenz: Plan '2026-11-03 - 00 - code-audit-fixes-plan' item 5 (archived in c3346711) consumes the values of the short value-taking options (env -u/-C/-S, sudo -u/-g/-h/-p/-C/-D/-R/-T/-U, nice -n, xargs -I/-n/-s/-P/-d/-E/-a/-L); long forms (env --unset NAME, sudo --user root, nice --adjustment 5, xargs --max-args 1) still strip bare, so the leader confusion remains through them.
