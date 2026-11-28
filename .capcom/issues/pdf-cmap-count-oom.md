---
title: A ToUnicode CMap's entry count allocates until out of memory
status: open
type: task
priority: 0
labels: [security]
plan: 2026-11-27 - 00 - pdf-parser-hardening-plan.md
created: 2026-11-27T21:27:15Z
---

`internal/doctext/pdfparse/page.go:425-443`: `readCmap` takes the `beginbfchar`/`beginbfrange`
loop count `n` from the document and loops `n` times; on an empty stack each pass pops a zero
Value and appends an entry, charging no read, so neither the ReadAt budget nor ctx sees it.

A deflated stream of about 30 bytes (`4000000000 beginbfchar endbfchar`) allocates tens of GiB and
kills the process.

Fix: cap `n` by the stack depth (`min(n, stk.Len()/2)`, `/3` for bfrange) and reject negative or
larger counts with `ok=false`. Record the patch in `pdfparse/doc.go`.

Source: `docs/reviews/code-audit-2026-11-27.md` (High, independently verified).

## Acceptance

- A CMap whose count exceeds its stack is refused with no growth; a regression test pins it.
