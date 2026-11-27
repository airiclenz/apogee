---
title: Per-page re-inflation of a shared content stream breaks the single PDF budget
status: open
type: task
priority: 1
created: 2026-11-27T21:27:15Z
---

`internal/doctext/pdf.go:1852-1881`: `pageContentNesting` calls `readPDFSyntax` with a fresh 64 MiB
inflate limit per page, and `GetPlainText` inflates the same stream again, while the preflight
charges a shared `/Contents` stream once.

2000 pages sharing one large content stream cost minutes of CPU for one `read_file`. Cancellation
still ends it.

Fix: cache the nesting verdict per content-stream object number, or pass a shared remaining-inflate
budget.

Source: `docs/reviews/code-audit-2026-11-27.md` (Medium, independently verified).

## Acceptance

- A shared content stream is inflated for the nesting check at most once per document; a test pins it.
