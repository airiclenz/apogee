---
title: "A tool block's shape is asked once (architecture review 2026-11-21 #1)"
status: parked
type: task
priority: 3
labels: [tui]
plan: 2026-11-21 - 00 - tool-block-shape-plan.md
created: 2026-11-21T12:27:13Z
---

Found by docs/reviews/architecture-review-2026-11-21.html, verified 2026-11-21 at 171d210f.

Problem: paint and fold each walk the transcript entries to decide what shape a tool block is (Tools umbrella, Sub-agent group, Skill group, group of one, standalone). `resolveBlock` (internal/tui/render.go:897) is the paint walk; `foldableHead` / `foldHeadOf` / `toolGroupAt` (transcript.go:2585-2615) are the fold walk. Only a comment ("in render.go's resolveBlock order", transcript.go:2592) keeps their order in step. The group-of-one rule is restated in paint (toolblock.go:181) and in the reveal (`sup.calls() < 2`, transcript.go:2677). They have drifted: bug `lone-call-no-refold` (fix 0ebd1c61, 22 files: `blockFoldBy` returned `foldByNone` for a lone call render painted as folded), and 4353151a deleted a parallel `resolveFoldedCall` paint path.

Solution: one module answers "what shape is the block at this head" (kind, span, head, group of one); paint, fold (`blockFoldBy`), reveal and mouse hit-testing ask it. `blockFoldBy` (transcript.go:2307) and the paint cache's resolved-fold key are already well-shaped and stay. Agreement tests (toolshape_test, blocktarget_test, groupofone_test) become shape tables. Foundation for #6 (fold-gesture-setters-leak) and #7 (tool-grouping-class-scattered).

Must keep ADR 0035 amendment 1120c (a)/(b): every call is a group of one; a lone delegation or skill fetch is `(1)`; task-list cards, answered ask_user records, targetless calls and workflow blocks stay out.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
