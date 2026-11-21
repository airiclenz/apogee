---
title: "Reader fold gestures live in the fold module (architecture review 2026-11-21 #6)"
status: parked
type: task
priority: 3
labels: [tui]
created: 2026-11-21T12:27:13Z
---

Found by docs/reviews/architecture-review-2026-11-21.html, verified 2026-11-21 at 171d210f.

Problem: each gesture caller must know the `blockFoldRule` enum and the right mix of setters per arm — `openBlockAlone`, `toggleToolsFold` + `markBlockTouched`, `setBlockFolded` + `markBlockTouched` (switch at internal/tui/mouse.go:933-945; 7 `markBlockTouched` calls in mouse.go). Fold state is spread over head flags (`folded`, `foldTouched`, `foldOwn`, `turnClosed`, `typeExpanded`) and transcript fields (`toolsOpen`, `toolsFoldOver`, `toolsAutoCollapse`, `turnLive`). Other gesture sites: sidepanel.go:1382, :1392, settingsapply.go:809-824. Testing a gesture needs a full Model, `refreshViewport`, `lineTargets` and `clickCell` or cursor steps; there are 23 raw-flag reads across 5 test files (autocollapse_touch_test alone: 11 drives, 13 reads; 4353151a rewrote 100 lines of it).

Solution: the fold module takes the reader's act (header fold, opening a type row or member, panel reveal, entering a run) and returns the resulting fold, plus whether the shared `ui.tools-open` preference must flip (only the Model writes config). Touch, group-of-one and Sub-agent fold tests run on a bare transcript and assert observable fold, not flags. Best done after #1 (tool-block-shape-twice).

Must keep: ADR 0035 D2 (auto-collapse writes nothing; only the large-umbrella shared toggle writes), amendment 1118 (b)/(c) (touched is session-only and never serialised; opening an auto-collapsed block opens that one alone), amendment 1120c (c) (the touch belongs to the head and carries as the group grows).

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
