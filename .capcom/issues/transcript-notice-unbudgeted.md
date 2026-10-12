---
title: "renderBudgetedTranscript: elision notice is not counted against maxChars"
status: done
type: bug
priority: 4
labels: [audit]
aliases: [apogee-transcript-notice-unbudgeted]
created: 2026-10-01T10:20:28Z
closed: 2026-10-01T17:31:39Z
reason: "Resolved by plan '2026-10-01 - 00 - compaction-notice-and-layout-residue-plan' item 1 (archived in 6c0756a2): elision notice now counted against maxChars, pinned by compact_test.go."
---

From docs/reviews/2026-09-30 - audit-context-all.md, finding C-01/M-01 (the report names a
nonexistent transcript.go; the code is internal/context/compact.go:248).

renderBudgetedTranscript fills the protected prefix plus a backwards tail against maxChars,
then writes the "[... N earlier message(s) omitted to fit the compaction budget ...]\n\n"
notice (~70 bytes) on top. When middle messages are elided, the output can therefore exceed
maxChars by the notice's length.

Impact is negligible: the budget is already soft by design (prefix and most recent message are
kept unconditionally, compact.go:241-247), the summary instruction/tail are appended outside it
(compact.go:134), and the budget (agent.compactTranscriptChars, internal/agent/compact.go:521)
is thousands of chars. Filed for correctness of the stated contract, not a live failure.

Fix: reserve the notice's length out of maxChars before the tail-fill loop (only the
elision case pays it), or check used+notice when deciding to admit each tail message.

## Acceptance

When middle messages are elided, len(output) <= maxChars unless the prefix or most recent message alone exceeds it; a test in internal/context/compact_test.go pins this.
