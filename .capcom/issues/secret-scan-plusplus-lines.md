---
title: Staged-secret scan skips any added line that begins with `++ `
status: done
type: bug
priority: 2
labels: [security]
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
closed: 2026-11-03T16:07:40Z
reason: Resolved by plan '2026-11-03 - 00 - code-audit-fixes-plan' (archived in c3346711)
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 3 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. `SecretFindings` (internal/security/secrets.go:113) treats every diff line starting `+++ ` as a file header. An added line whose content begins `++ ` shows as `+++ ...`, so it is never scanned and it resets the current path, mis-attributing later findings.

## Why

A secret on a line like `++ AKIA...` passes the commit-secrets forced look under Auto.

## Acceptance

- `+++ ` is a header only after a `--- ` line; otherwise it is scanned as content.
- A test with an added `++ AKIA...` line reports a finding against the right path.
