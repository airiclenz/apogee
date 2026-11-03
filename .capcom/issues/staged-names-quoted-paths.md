---
title: Commit-secrets pre-check misses secret-named files whose path git C-quotes
status: open
type: bug
priority: 2
labels: [security]
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 2 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. internal/agent/secretsguard.go:185-189 lists staged paths with `git diff --cached --name-only` without `-z` or `core.quotepath=false`. Git quotes any path with a non-ASCII byte, control character, `"` or `\`, so `données/.env` becomes `"donn\303\251es/.env"` and the base name `.env"` never matches the secret file-name class (internal/security/secrets.go:106). `addedFilePath`'s diff-header parse has the same blind spot.

## Why

A `git_commit` staging `données/.env` skips the forced commit-secrets approval (ADR 0080) when its contents match no pattern.

## Acceptance

- Staged names are read NUL-delimited; a test stages a non-ASCII-pathed `.env` and the forced look fires.
- `addedFilePath` tolerates a quoted diff header.
