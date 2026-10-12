---
title: Sub-agent shown as done before it finishes
status: done
type: bug
priority: 1
plan: 2026-09-24 - 01 - subagent-premature-done-plan
aliases: [apogee-subagent-premature-done]
created: 2026-09-24T13:26:30Z
closed: 2026-09-24T18:17:14Z
reason: "Plan 2026-09-24 - 01 landed all 8 items; the two deferred gaps are fixed in 3e1234af (record closes a tool call in its own run) and a02fb5ad (SubAgentUsage bracketed by run id). Sessions saved before item 6 still replay a no-report delegation as done: no verdict was recorded, so none can be recovered."
---

Sub-agents are sometimes shown as "done" (✓) even though they have not finished and have not submitted their final report. The owner sees it now and then.

## Diagnosis (2026-09-24, read-only; no repro yet)

The engine side is sound. `runDelegation` (internal/agent/dispatch.go:777) emits SubAgentFinished only after runSubAgent returns, and events reach the TUI in order (sink.go:95, bridge.go:205). The UI pairs events by call id alone, and nothing makes call ids unique across the delegation tree.

### Candidate causes, most likely first

1. **Call-id collision in a parallel fan-out (HIGH).** At width > 1, dispatchGroup (dispatch.go:363) emits every head's ToolCallEvent before any child runs. `place` (transcript.go:486) then inserts child A's inner calls inside A's span, ahead of sibling head B. `addToolResult` (internal/tui/transcript.go:1517) pairs a result with the newest open entry whose `callID` matches and checks neither depth nor spawn id. So if an inner call of A has the same id as B's head, A's leaf result closes B's head (`e.done = true`). `delegationVerdict` (toolregistry.go:906) then labels it `done` and B shows ✓ while its child is still running.
   Ids collide in two ways:
   - Salvage ids `text_call_<turnIndex>_<i>` (builtins.go:265). The child's turn index restarts at 0 (construct.go:162), so a child at turn T that salvages two or more calls produces `text_call_T_1`, which is the second head's id.
   - Native per-reply counter ids (`call_0`, `call_1`) from some local servers or chat templates.

   It needs width > 1 and non-unique ids, which is why it is intermittent. It never happens on Anthropic, whose `toolu_` ids are unique.
2. **Duplicate sub_agent ids within one reply (MEDIUM-HIGH).** Nothing removes duplicate ids from a reply (children.go:46 acknowledges this: ADR 0059 §6). The first child's SubAgentFinished reaches `addSubAgentPhase` (transcript.go:1599, also `headsRunFor` :2108), which matches by callID and ticks the last head with that id. The two runs also merge under one head. With delegate-max-depth ≥ 2, a grandchild's spawn id can collide the same way.
3. **The child really ended, but not on a report (MEDIUM).** A child ends on any reply that has text and no tool call. `completedResult` (subagent.go:1846) does not run the narration check (`closingShapeOf`/`endsOnIntent`, subagent.go:342); that check runs only on the step-capped path. So a delegate that stops on "Let me now read X." counts as its report and shows ✓ done. `noReportMarker` "[delegate returned no report]" (subagent.go:268) also shows ✓ done. apogee-2h6 and apogee-1zz closed this gap for the capped path only.
4. **"finished before your message landed" is wrong wording (LOW).** `drainMailbox` → `reportUndelivered` (children.go:480) fires when Interject refuses. transcript.go:1667 always words that as "finished", even though the child is still running.

### Telling them apart
- Look at the parent's sub_agent call ids in the affected session:
  - `text_call_…` or `call_0`/`call_1`-style ids point to #1.
  - The same id on two sub_agent calls point to #2.
  - Unique ids leave #3.
- Look at the row that ticked early:
  - A leaf tool's output (file text, a grep hit) in its body points to #1.
  - A "Let me …" narration or "[delegate returned no report]" points to #3.

### Proposed tests
- #1: a transcript test. Send two depth-0 heads c0 and c1, then a depth-1 ToolCallEvent and ToolResultEvent with spawn c0 and Call.ID c1. Assert that head c1 is not done. This is expected to fail today.
- #2: two heads with id c0 and one SubAgentFinished{c0}.
- #3: a stub-LLM delegate whose last reply is narration with no tool call.

### Fix direction
- UI: pair results and phases by full run identity (depth plus spawn id, checking the entry's spawnCallID), not by callID alone.
- Engine: make ids unique per Agent and per reply. That means a per-agent prefix for synthesised `text_call_` ids, and deduplicating repeated native ids in a way that does not break the dispatchableCalls rule that native ids are never made up.
- #3: extend the narration check to the completed path, or show "stopped without a report" separately from "done".

Related: apogee-2un (open; nearby, not the cause).

## Log

- 2026-09-24 Airic Lenz: Plan 2026-09-24 - 01 - subagent-premature-done-plan (archived e2e4a9a6) landed all 8 items: engine-minted run id on every event, persisted and in the JSON stream; TUI keys runs and pairs results by run id; no-report verdict (no ✓); undelivered-interjection reasons; docs. Left open for deferred gaps: internal/run/transcript.go appendToolResult still closes by CallID (mid-run save can cross-close colliding siblings); internal/run/run.go eventTap still brackets SubAgentUsage by call id (fan-out siblings mix usage; comment near :633 still says 'run identity'); pre-item-6 sessions replay no-report as ✓ (no recoverable verdict).
