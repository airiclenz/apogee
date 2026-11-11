---
title: A forged xref offset lets the PDF parser recurse through bytes the nesting preflight never scanned
status: open
type: bug
priority: 2
labels: [security]
created: 2026-11-11T05:56:07Z
---

The nesting guard added for pdf-parser-unbounded-nesting holds only for the preflight lexer's tokenisation from the start of each span it scans (`refuseDeepNesting`, `internal/doctext/pdf.go`; the limit is stated in ExtractPDF's doc, `pdf.go:194-197`). A forged xref or startxref offset sends the parser's `resolve` → `readObject` into bytes the preflight read differently or not at all:

- an offset into a string or comment re-tokenises those bytes, bounded only by that span's own depth;
- an offset into an unscanned binary stream body (an untyped stream holding 1M `[`) still overflows the goroutine stack in `resolve` → `readObject` and kills the process.

ExtractPDF's doc (`pdf.go:194-197`) names only strings and comments as the forged-offset limit, so the binary-stream case is both a crash and a doc that understates it. Found by the audit-defects run's verification of item 1 (docs/plans/archived/2026-11-10 - 02 - audit-defects-plan.md).

## Why

The point of the guard is that no PDF can kill the process with a stack overflow; a forged offset into a binary stream still does.

## Acceptance

- A PDF whose xref offset points into an untyped stream body of deeply nested '[' is refused, not parsed, pinned by a test in internal/doctext/pdf_test.go
- ExtractPDF's doc states the forged-offset limit that remains, if any, without understating it
