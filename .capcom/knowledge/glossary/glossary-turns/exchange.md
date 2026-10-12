---
title: Exchange
---

**Exchange**:
One user input through to the final no-tool response — usually several Turns. The
user-facing unit of a conversation. In code the Exchange is derived from the conversation —
the messages strictly after the last user message — as a domain working value
(`internal/domain`'s `ExchangeView`) consumed by the loop and by Reactions. One engine
exception: the abort-rollback boundary stays a cached field read through
`Agent.exchangeBoundary()`, because a mid-Exchange truncation can drop the opening user
message the derivation would need
(ADR 0017 §2's recorded
fallback). An Exchange the human stopped mid-run closes **without an answer**
(`Agent.SettleExchange`): the Turns that finished before the stop stay — the opening user
message, the tool calls and their results, and any interjection delivered after the last
result — and the cut is marked as an ephemeral `[engine — cancelled]` engine note on that last
tool result, so the model's next request reads that the results above stand and the reply was
not given. The marker is **live-session only**: a saved record and a resumed conversation hold
the kept results — a closed Exchange with its finished Turns — and no marker
(ADR 0076 D6;
ADR 0022's
2026-09-19 note). An Exchange with no finished Turn has no tool result to carry the note and is
scrapped instead (`Agent.AbortExchange`, the explicit throw-away `/clear` also takes) — except a
**Recipe** launch whose Workflow ran: its opening user message already carries the user's line and
the Workflow's result lines, so a cancel keeps that opening, the `[engine — cancelled]` note rides on
it, and no model request follows (amended 2026-09-29,
ADR 0088 D1) — and a kept
interjection leaves the next Submit opening user→user — the tail a faulted Exchange already
leaves (ADR 0025's
2026-09-19 note). The TUI's cancel and loop-error folds and a fresh message on a restored
interrupted session settle. A cancel never rewinds finished work: the Turn in flight is settled
too — a finished call keeps its real result, a running one ends with a cancelled result, one that
never started gets a not-run result — and a cancelled delegation pool keeps its finished children
and stops each running one as `^x` would (**Stop (a delegation)**); only a Turn cancelled before
its reply finished streaming is dropped (ADR 0088 D1–D2, superseding
ADR 0013
§5(b)). An Exchange usually opens on a user message, but may also open on the finish note
of a **Background workflow** (one line, or a background `sub_agent`'s lead line and report) that ended while the agent was idle — the **wake** (ADR 0089 D3).
