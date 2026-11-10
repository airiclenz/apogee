---
title: "The side panel's frame is one computed value (architecture review 2026-11-10 #1)"
status: parked
type: task
priority: 3
labels: [tui]
created: 2026-11-10T19:36:32Z
---

Found by docs/reviews/architecture-review-2026-11-10.html, verified 2026-11-10 at a5c4f2db. Verified PARTLY: the structural claims hold, but the churn is overstated — 41 commits since 11-02, 30 of which touch only one of sidepanel.go, panelstops.go and blockcursor.go, not 47 co-edits; the "15 methods" are 13 Model methods plus 2 free functions.

Problem: the panel's horizontal geometry (width, paint width, chrome, overlay gap, clamp, pin x, grip, gate floors) is spread over ~15 shallow helpers in internal/tui/sidepanel.go. Geometry depends on navigation state: `panelOverlaid` (sidepanel.go:216) reads `m.cursor.inPanel()`. `enterPanelRegion` (blockcursor.go:376) and `openPanelOverlay` (sidepanel.go:1569) mutate a copy's cursor to ask what the stops would be. `panelStops` re-parses and re-paints the sections on each call (click, cursor row, nav). Click (mouse.go:455), wheel (:2164) and hover (pointershape.go:52) each hit-test the column on their own.

Solution: build one `panelFrame` per frame from window width, show mode, width setting, overlay state, sections and rows; carry it on `pre` as `withFrameSpans` carries vertical geometry (`footerFit → footerLayout` is the precedent). It answers `at(x, y)` → cell/grip/pin/target, plus `rows()` and `stops()`. Must keep ADR 0110 D6 and ADR 0111 D4. Foundation for #6 (nav-verbs-per-region).

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
