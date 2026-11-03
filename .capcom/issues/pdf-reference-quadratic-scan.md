---
title: Hostile PDF stalls a Turn with quadratic reference scanning
status: open
type: bug
priority: 2
labels: [security]
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 9 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. `decodedReferences` (internal/doctext/pdf.go:1294-1307) rescans a referenced array object in full for every `/Contents` or `/ToUnicode` site naming it, with no de-duplication and no `ctx` check. One object of K references plus M sites costs M*K in `preflightPDF`, before any read budget.

## Why

A hostile repo file or downloaded document hangs the Turn and ignores cancellation (bounded by file size).

## Acceptance

- Each referenced array is expanded once and the site loop checks `ctx.Err()`.
- `ExtractPDF` on a crafted ~300k-site file finishes fast and honours cancellation.
