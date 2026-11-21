---
title: "Status line as one priority fit like the footer (architecture review 2026-11-02 #10)"
status: dropped
type: task
priority: 4
created: 2026-11-02T20:02:20Z
closed: 2026-11-21T16:14:32Z
reason: "Not worth the churn: no defect; fit must stay byte-identical behind a golden matrix. Triage 2026-11-21."
---

Found by docs/reviews/archived/architecture-review-2026-11-02.html, verified 2026-11-02 at 00d5bf9f. Verified: the render-measure-re-render shape is real (fd39159d added a third re-render; statusText has 33 test uses) but no defect. The status line mixes styles, so footerFit does not drop in; any fit must work over styled forms behind a byte-identical golden matrix (ADR 0030).

## Acceptance

- Decided: planned behind a golden matrix, or closed.

## Log

- 2026-11-21 Airic Lenz: Triage 2026-11-21 at 7869cca7 (read-only sub-agent check, not re-verified): DOUBTFUL — no defect; render-measure-re-render must stay byte-identical behind a golden matrix
