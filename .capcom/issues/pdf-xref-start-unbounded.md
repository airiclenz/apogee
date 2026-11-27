---
title: A PDF's xref start offset sizes an unbounded allocation
status: open
type: task
priority: 0
labels: [security]
created: 2026-11-27T21:27:15Z
---

`internal/doctext/pdfparse/read.go:437-443` (also `:340-343`): `readXrefTableData` and
`readXrefStreamData` grow the xref table up to a subsection start (or `/Index` start) read
straight from the bytes, never compared with the file size or `/Size`; the trim to `/Size` comes
after the growth. With `/W [0 0 0]` and a huge `/Index` count the stream path grows with no
budget charge.

A ~100-byte PDF allocates tens of GiB. Go's out-of-memory abort is not recoverable by
`ExtractPDF`'s recover, so `read_file` or `@file` of a hostile document kills the agent or TUI —
against the invariant stated at `internal/doctext/pdf.go:28-31`.

Fix: in both readers refuse a subsection whose `start + n` exceeds the file end (or `/Size`), and
add `/Index` and xref-table subsection headers to the preflight beside `Size`. Record the patch in
`pdfparse/doc.go`. A natural fuzz target (`untrusted-parsers-unfuzzed`).

Source: `docs/reviews/code-audit-2026-11-27.md` (High, independently verified).

## Acceptance

- A ~100-byte PDF with a huge subsection start or `/Index` count is refused without a large allocation; a regression test pins it.
- The patch is listed in `pdfparse/doc.go`.
