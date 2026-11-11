---
title: A PDF whose content stream ends inside an unclosed '[' hangs ExtractPDF until the process is killed
status: open
type: bug
priority: 1
labels: [security]
created: 2026-11-11T05:56:06Z
---

A page content stream or CMap that ends inside an unclosed array (`[ 1 2` with no `]`) hangs `ExtractPDF` (`internal/doctext/pdf.go:220`). The third-party parser's `readArray` (`github.com/ledongthuc/pdf@v0.0.0-20250511090121-5959a4027728` `lex.go:465-476`) loops until it reads `]` or a nil token; under `allowEOF` (the `Interpret` path for content streams and CMaps) the token at end of input is `io.EOF`, not nil, so the loop never ends and appends to the array on every pass. Memory grows until the process is killed. Reached by `read_file`, an `@file` reference and the path-read route on any such PDF. Observed by the audit-defects run (docs/plans/archived/2026-11-10 - 02 - audit-defects-plan.md, item 1); pre-existing, not introduced by it.

## Why

One truncated or hostile PDF in a workspace kills the whole session; ctx cancellation does not reach a loop that makes no reads.

## Acceptance

- A content stream and a CMap ending inside an unclosed '[' each make ExtractPDF return the unreadable-PDF failure (or a per-page placeholder) within the read budget, pinned by tests in internal/doctext/pdf_test.go
