---
title: Read-cache Floor guard caps a needed full re-read to one line when the earlier copy was elided
status: open
type: bug
priority: 2
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 18 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. `CapToolResults` (internal/floor/resultcap.go:41-49, internal/context/toolresult.go:37) elides a large earlier read to 20 head and 20 tail lines in the request, but `priorSuccessfulReadUnchanged` (internal/floor/readcache.go:96-100) checks only for a prune stub on the stored result, so a later bare re-read is capped to `max_lines=1`.

## Why

With a small context window the model asks for the whole file and gets a header line: worse than the bare loop, breaching ADR 0071 D1(c).

## Acceptance

- The cache declines whenever the earlier copy is not whole in the request (elided in store or over the cap's ceiling), via one predicate shared with the result cap.
- A test with an over-cap earlier read lets the re-read through.
