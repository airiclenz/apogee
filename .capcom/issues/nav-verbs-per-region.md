---
title: "Navigation mode over two region adapters (architecture review 2026-11-10 #6)"
status: parked
type: task
priority: 3
labels: [tui]
created: 2026-11-10T19:37:32Z
---

Found by docs/reviews/architecture-review-2026-11-10.html, verified 2026-11-10 at a5c4f2db. Verified VALID: every navigation verb is written twice, once per region (internal/tui/blockcursor.go: `moveBlockCursor`/`movePanelCursor`, `toggleAtBlockCursor`/`activatePanelCursor`, `cursorRunHead`/`panelCursorRunHead`, `seatBlockCursor`/`reseatPanelCursor`). The panel cursor holds a stale identity until `reseatPanelCursor` runs, so a caller asking before `blockCursorKey` must reseat first (internal/tui/runview.go:467, :472). About 20 region branches (the review said 23).

Problem: an ordering invariant (reseat before asking) leaks to callers, and each verb is maintained in two copies.

Solution: resolve the cursor once per key behind a region seam. Navigation mode maps a key to (model, cmd, claimed) and answers standingOn(); two adapters, transcript and panel, each give stops, nearest, activate and run head. ADR 0111 D12 declines a third region (the footer). Best taken after #1 (panel-geometry-scattered), whose stops the panel adapter would read.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
