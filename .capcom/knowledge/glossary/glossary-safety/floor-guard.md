---
title: "Floor guard: engine behaviour that only answers the model's own failure or shapes a request without steering it"
---

**Floor guard**:
Plain **engine behaviour** that changes only what the model sees **after its own failure**, or
shapes the request **without steering it** — so it needs no per-model proof and cannot regress
Bypass (ADR 0071).
It is the **engine**-origin **shape (view)** column of the
**Reaction surface** matrix: a builtin **Reaction**, armed by nothing
and throttled by nothing — the per-Turn `maxPostResponseRetries` bound
is the only limiter the post-response guards share. **Seven ship, on in every arm** — Bypass
included — each switched off by exactly one top-level, **file-only** boolean (no flag, no env;
editable live in `/settings`) whose key names it:
- **tool-call repair** (`tool-call-repair`) — an unknown tool, malformed arguments or a missing
  required parameter answered with the correction the Turn re-streams with. A call naming a tool the
  engine *has* but this request's menu **withdrew** (Plan's filtered menu, a delegate's wrap-up Turn)
  is left alone: that call belongs to the mode, and the mode's own refusal is the answer that reaches
  the model.
- **tool-call salvage** (`tool-call-salvage`) — a reply from a **native-profile** model that carries
  no wire call but wrote one out as JSON in its own text — fenced, wrapped in `<tool_call>` tags, or
  as the whole trimmed content — read back as the call the model meant, and the text handed on
  without the block it salvaged. A block that **reproduces** JSON from a prior tool result (the same
  decoded call, or its whitespace-free text inside the result's) is quoted content, not a call, and
  stays as text. Alone among the seven it **completes** a response rather than
  correcting one, so it runs **first** and does not short-circuit: the four repair guards below it
  judge the response the model meant rather than a Turn that only looked empty.
- **tool-loop breaker** (`tool-loop-breaker`) — a response repeating the previous Turn's exact
  calls, or closing an exact A-B-A-B alternation over the Exchange's last four tool Turns whose
  repeated pair drew byte-identical tool results (a poll whose output moves is progress, not a
  loop — ADR 0059), answered with a directive that names the repeat and steers at the remaining
  work. Byte-identical keys only, no fuzzy match.
  **Exchange-scoped**, scan and recap both: a human re-asking for the same
  thing opens a new Exchange, and its first call is the work just asked for rather than a loop.
- **empty-response recovery** (`empty-response-recovery`) — a reply carrying neither text nor a
  tool call answered with the completion-check nudge.
- **tool-use enforcer** (`tool-use-enforcer`) — a second narration where an action was asked for
  answered with the tool menu and the instruction to call one of it.
- **read cache** (`read-cache`) — a redundant re-read of a file already read successfully, with no
  write to it and no call to a non-read-only tool since (a shell command or an MCP tool may rewrite
  a file without naming it), capped to a header slice so the copy already in the conversation
  stands.
- **tool-result cap** (`tool-result-cap`) — every older tool result that outgrew its fraction of
  the Budget trimmed in the **request projection**, the conversation itself untouched (see
  Tool-result capping).
The decision logic is **pure policy** in `internal/floor`; the seams that call it, the live on/off
gate and the events a firing emits are `internal/agent`'s. `domain.FloorConfig` spells the seven as
`Disable…` bools, so an embedder handing `New` a bare `Config` gets the **whole floor**. A firing
reaches every Driver as one `ReactionFiredEvent` keyed by the guard's config key
(ADR 0076).
_Avoid_: "Mechanism" for a guard (a retired term — a guard is engine behaviour, never gated or
self-regulating), "off-ramp" (the Capability that named two of them — see
Retired terms), "always-on Mechanism".
