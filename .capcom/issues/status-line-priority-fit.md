---
title: "Status line as one priority fit like the footer (architecture review 2026-11-02 #10)"
status: parked
type: task
priority: 4
created: 2026-11-02T20:02:20Z
---

Found by docs/reviews/architecture-review-2026-11-02.html, verified 2026-11-02 at 00d5bf9f. Verified: the render-measure-re-render shape is real (fd39159d added a third re-render; statusText has 33 test uses) but no defect. The status line mixes styles, so footerFit does not drop in; any fit must work over styled forms behind a byte-identical golden matrix (ADR 0030).

## Acceptance

- Decided: planned behind a golden matrix, or closed.
