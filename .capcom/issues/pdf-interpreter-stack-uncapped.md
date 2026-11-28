---
title: The PDF content-stream interpreter stack is uncapped
status: open
type: task
priority: 1
labels: [security]
plan: 2026-11-27 - 00 - pdf-parser-hardening-plan.md
created: 2026-11-27T21:27:15Z
---

`internal/doctext/pdfparse/ps.go:56-140`: `Interpret` pushes one 32-byte Value per token with no
stack cap, and page callbacks copy the whole stack at the next operator.

A deflated 64 MiB content stream of `1 ` tokens (about 64 KB on disk) forces roughly 1.3 GB or more
of memory — dangerous on an 8 GB box.

Fix: cap `stk.Len()` in `Interpret` (for example `1<<16`) and fail the page past it.

Source: `docs/reviews/code-audit-2026-11-27.md` (Medium, independently verified).

## Acceptance

- A content stream past the cap fails that page with a clear cause and bounded memory; a regression test pins it.
