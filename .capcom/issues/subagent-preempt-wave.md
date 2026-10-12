---
title: Interjection skips queued sub-agents as an empty, red 'error' row
status: done
type: bug
priority: 2
plan: 2026-10-05 - 00 - subagent-wave-and-background-plan
aliases: [apogee-subagent-preempt-wave]
created: 2026-10-05T12:30:20Z
closed: 2026-10-05T16:11:30Z
reason: "Resolved by plan 'subagent-wave-and-background-plan' (items 1-5; archived at 959f9dd9): skipped rows expand in place pre-burst, neutral verdicts, ADR 0025 amended — the wave finishes by default, ctrl+g sends now."
---

## Observed (live session 2026-10-05, ~/.apogee/sessions/20261005T094536Z-618cbaa4.json, entries ~1009/1010, spawnRunID 3a6ca038.26)

The main agent ran a code-audit wave of `sub_agent` calls. While the wave ran, the user typed a message that meant "stop after this wave". The two members that had not started yet were dropped. The TUI showed:

```
 ┝ lens-conc cmd-apogee-1 ⋯⋯⋯ error ▶
 ┕ lens-intent cmd-apogee-2 ⋯⋯ error ▶
```

Expanding a row showed nothing. The session file has the reason in `tool.details`: "sub-agent not started: the user sent a message while this group was running; delegate again if the task is still needed".

The user reports three problems:
1. Expanding the row shows no detail.
2. The row says "error", but nothing failed.
3. The user wanted "finish the wave, then stop". They did not want to abort part of the wave.

## Cause 1: expanding opens an empty run view
- Mouse `targetHeader` and ⏎ call `openRunAt` first (internal/tui/mouse.go:677).
- `openRunAt` (internal/tui/runview.go:258) refuses a run view only when `subAgentSpan(...) == 0 && head.done`.
- `done` is set only when the group's results burst in. Here siblings call_2/call_3 (lens-correct, lens-security) were still running, so the skipped heads had a finished phase but `done=false`. The session entries confirm it: the skipped heads have no `done` field, while earlier-wave heads have `done: true`.
- So a run view is pushed for a child with no entries. The result is a blank view whose legend reads "… has finished · esc back" (childPhaseOf → childOver, runview.go:150).
- The doc comment of `openRunAt` says it should refuse "one that has not reported yet". The code does not match the doc. `subAgentReported` (subagentblock.go:671, done || phase==Finished) or `neverStarted()` (transcript.go:2481) is the right check.
- Why tests miss it: `TestSubAgentSkippedRowReadsItsResult` (internal/tui/subagentblock_test.go:2414) checks the pre-burst state only collapsed. Its expanded subtests use `tr.setExpanded` after the burst, which bypasses `openRunAt`. cmd/apogee/e2e_subagent_preempt_test.go never expands a row.

## Cause 2: shown as "error"
- `skippedDelegationResult` (internal/agent/dispatch.go:762, content const at :758) is built with `errorToolResult` (IsError: true). `toolView.absorbFailure` (internal/tui/toolview.go:1282) then sets `erroredSummary`, shown in red.
- There is a precedent for a non-error outcome. A queued delegation the user stopped (`stoppedQueuedDelegationContent`, dispatch.go:744) is matched by `delegationStoppedByUser` (internal/tui/toolregistry.go:984) and shown as "stopped by you" in the ordinary marker tone (toolregistry.go:888, stoppedSummary toolleader.go:388).
- Three outcomes have no such branch and fall through to "error":
  - the interjection preempt (this case);
  - the user's turn cancel (`cancelledQueuedDelegationContent`, dispatch.go:751);
  - possibly the fan-out-ceiling refusal (decide whether that one is a real error).
- The preempt should get its own neutral verdict, for example "not started · your message".

## Cause 3: the wave is cut short, with no "finish the wave" option
- `preemptDelegation` (internal/agent/dispatch.go:433) runs at each `sub_agent` slot dequeue (serial :393, pool :684). If `interjectionPending()` (:799) is true, the slot is skipped (`skipDelegation` :819).
- At depth 0 the predicate is `Bridge.InterjectionPending` (internal/tui/bridge.go:120). It is true whenever the mailbox (interject.go:69) holds any staged row. It never reads the message.
- Running members finish. The message is delivered at the next between-Steps boundary (worker.go ~233-243, deliverInterjections → Agent.Interject). The group is a single tool round, so "react now" mid-group is not possible (ADR 0025 D1). The model gets the skip text as each skipped member's tool result and must re-delegate.
- Rationale: ADR 0025, the "Amended 2026-09-14" block (docs/adr/0025-*.md:67-85), and CONTEXT.md:1048-1053. The aim is that a message should not wait behind members queued past the `parallel-agents` cap.
- The gap: every staged message preempts. The user cannot say "let the wave finish, then deliver". The only off-switch is a nil host seam.
- Options to design (needs an ADR 0025 amendment):
  - (a) a way to stage a message as "after the wave" (a separate key, or a modifier on submit) that `Bridge.InterjectionPending` ignores;
  - (b) a config knob for the preempt;
  - (c) flip the default to finish the wave, and keep preempt only for an explicit "now" send.
- Also make the UX clear: when the user stages a message while a group runs, say what will happen to queued members (for example a hint "queued sub-agents will be skipped").

## Acceptance

- Expanding (⏎ or click) a skipped/never-started sub-agent row before the group's results burst shows its result text in place, not an empty run view. A driven test covers the pre-burst expand path through openRunAt.
- A delegation skipped by an interjection preempt (and a user turn-cancel of a queued delegation) does not render as "error" or in the error tone; it gets a neutral verdict like "stopped by you".
- A decision is recorded (ADR 0025 amendment) on how a user lets the running wave finish, queued members included, before their message is delivered, and that path is implemented and tested.

## Log

- 2026-10-05 Airic Lenz: Related: apogee-background-sub-agent (option A, owner 2026-10-05) lets an approved model hear the user while sub-agents run. This bead stays needed for the blocking path (unapproved classes, headless, daemon).
