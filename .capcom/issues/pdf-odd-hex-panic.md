---
title: A closed odd-digit hex string still panics in the PDF lexer's readHexString
status: done
type: bug
priority: 3
created: 2026-11-11T09:55:47Z
closed: 2026-11-11T14:27:07Z
reason: "Resolved by plan '2026-11-11 - 01 - fence-escape-and-open-defects-plan' item 2 (archived at d2bc5fcb; fix 134822b5): readHexString reads a closed odd-digit hex string with a final 0, pinned by TestBufferReadObject_OddDigitHexString_ReadsFinalZero."
---

A closed hex string with an odd number of digits (`<414>`) still panics in `buffer.readHexString` (`internal/doctext/pdfparse/lex.go:217`): the second read of a pair takes the closing `>` as a digit, `unhex` returns -1, and `b.errorf("malformed hex string ...")` fires (`internal/doctext/pdfparse/lex.go:238`). Upstream `ledongthuc/pdf` behaved the same. Observed by the verifier of item 3 of docs/plans/archived/2026-11-11 - 00 - pdf-parser-fork-and-defects-plan.md (which made end of input close an unterminated hex string); pre-existing, not introduced by that run.

## Why

The PDF reference treats a final odd digit as followed by 0, so a valid-by-spec string fails the parse instead of reading.

## Acceptance

- A closed odd-digit hex string (`<414>`) reads as if a final 0 followed the last digit, without a panic, pinned by a test in internal/doctext/pdfparse/
