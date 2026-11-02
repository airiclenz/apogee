---
title: Seqcom overflow loop pins miss the fold elision and room-left share
status: open
type: task
priority: 3
plan: 2026-11-02 - 05 - request-room-and-delegate-bounds-plan.md
created: 2026-11-02T18:50:12Z
---

Deferred from plan 'docs/plans/2026-11-02 - 03 - fresh-result-overflow-plan.md' (item 5). The loop-level pins in internal/agent/freshresult_overflow_test.go (TestFreshOversizedResultFitsTheNextRequest at :91, TestSeqcomOverflowBodyFoldsAndRetries at :141) do not bite on item 2's fold-transcript byte elision of an oversized newest message (internal/context/compact.go elideNewestMessage) or on item 4's room-left share (internal/agent/dispatch.go freshResultBound): reverting either change leaves both tests green. Only those items' own unit tests pin them, so the end-to-end Seqcom scenario is not guarded against a regression in either mechanism.

## Why

The loop-level pins were meant to hold the whole Seqcom overflow path end to end; a pin that stays green with a fix reverted does not hold it.

## Acceptance

- Reverting item 2's newest-message elision in the fold transcript fails a loop-level test in internal/agent
- Reverting item 4's room-left share (freshResultBound back to structuralFloor) fails a loop-level test in internal/agent
