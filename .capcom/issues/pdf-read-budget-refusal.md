---
title: Valid long PDFs exhaust the read budget and are refused with a false cause
status: open
type: task
priority: 1
created: 2026-11-27T21:27:15Z
---

`internal/doctext/pdf.go:141` (with `pdfparse/page.go:25-56` and `:195-200`): `pdfMaxReads =
200_000` is too low because `Reader.Page(n)` re-walks `/Kids` on every call (about N²/2 reads for a
flat page tree), and `Font.Encoder()` has a value receiver, so its cache never persists and each
`Tf` re-parses the whole CMap, charged to the budget.

Valid flat-tree manuals of roughly 400–600+ pages return nothing and fail with the false cause
"the document's object graph does not terminate". A clean refusal, not a crash.

Fix: walk the page tree once into a page list; use a pointer receiver or the `fonts` map for the
encoder cache; or charge the budget per page.

Source: `docs/reviews/code-audit-2026-11-27.md` (Medium, independently verified).

## Acceptance

- A valid 1000-page flat-tree PDF extracts within the budget; the reads charged grow linearly with page count (a test pins it).
- A budget refusal names the real cause.
