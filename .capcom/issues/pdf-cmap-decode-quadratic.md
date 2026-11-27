---
title: CMap decode is quadratic and cannot be interrupted
status: open
type: task
priority: 0
labels: [security]
created: 2026-11-27T21:27:15Z
---

`internal/doctext/pdfparse/page.go:324-383`: `cmap.Decode` scans every `bfchar` and `bfrange`
entry linearly for each decoded character, and `Page.GetPlainText` never checks ctx inside a page.

A small deflated ToUnicode CMap with 10^5–10^6 entries (the 64 MiB inflate budget allows it) plus
long `Tj` strings makes `read_file` hang for minutes; the read budget, page cap and cancellation
cannot interrupt it.

Fix: index `bfchar` by code in a map, cap the entry count in `readCmap`, and poll ctx in the decode
loop.

Source: `docs/reviews/code-audit-2026-11-27.md` (High, independently verified).

## Acceptance

- Decoding cost is independent of the CMap's entry count per character; cancelling ctx ends a decode promptly; a test pins both.
