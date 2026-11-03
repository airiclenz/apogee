---
title: Shell tokenizer keeps backslash-newline as a word, blinding the git-control-plane write rules
status: open
type: bug
priority: 2
labels: [security]
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 4 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. `scan` and `scanDoubleQuoted` (internal/security/shellwrites.go:506-510, :578-581) keep a backslash-newline as a literal newline word instead of removing it as `sh` does. In `a && \<NL> cat .git/HEAD` the newline becomes the leader and the real leader an operand: `cd .git && \<NL> ls hooks` is falsely refused, and `rm -rf .git/\<NL>hooks` hides a real write.

## Why

Models emit multi-line `cmd && \` constantly: false refusals in common use, a missed control-plane write in rarer forms.

## Acceptance

- Backslash-newline is consumed and emits nothing; `writeTargetsOf("cd .git && \\\n ls hooks")` reports no write.
- `rm -rf .git/\<NL>hooks` reports the `.git/hooks` target.
