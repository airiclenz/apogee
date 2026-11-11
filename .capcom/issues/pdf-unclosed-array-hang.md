---
title: A PDF whose content stream ends inside an unclosed '[' hangs ExtractPDF until the process is killed
status: done
type: bug
priority: 1
labels: [security]
plan: 2026-11-11 - 00 - pdf-parser-fork-and-defects-plan
created: 2026-11-11T05:56:06Z
closed: 2026-11-11T10:11:35Z
reason: "Resolved by item 3: end of input closes an open array or hex string and stops the ObjStm loop, pinned for content streams and CMaps (plan '2026-11-11 - 00 - pdf-parser-fork-and-defects-plan', archived at 87846d40)"
---

A page content stream or CMap that ends inside an unclosed array (`[ 1 2` with no `]`) hangs `ExtractPDF` (`internal/doctext/pdf.go:220`). The third-party parser's `readArray` (`github.com/ledongthuc/pdf@v0.0.0-20250511090121-5959a4027728` `lex.go:465-476`) loops until it reads `]` or a nil token; under `allowEOF` (the `Interpret` path for content streams and CMaps) the token at end of input is `io.EOF`, not nil, so the loop never ends and appends to the array on every pass. Memory grows until the process is killed. Reached by `read_file`, an `@file` reference and the path-read route on any such PDF. Observed by the audit-defects run (docs/plans/archived/2026-11-10 - 02 - audit-defects-plan.md, item 1); pre-existing, not introduced by it.

## Why

One truncated or hostile PDF in a workspace kills the whole session; ctx cancellation does not reach a loop that makes no reads.

## Acceptance

- A content stream and a CMap ending inside an unclosed '[' each make ExtractPDF return the unreadable-PDF failure (or a per-page placeholder) within the read budget, pinned by tests in internal/doctext/pdf_test.go
