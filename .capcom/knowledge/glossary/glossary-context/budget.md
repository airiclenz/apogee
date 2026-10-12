---
title: "Budget: the allocation of the context window across a request's parts"
---

**Budget**:
The allocation of the model's context window across the parts of a request — system
prompt, conversation history, file context, and response reserve. The single authority
on how much room each part gets; other reducers consume it. Lives in `internal/context/`.
The split is **measured**, not a set of fixed shares: the **reply reserve** comes off the top
(20% of the window by default, `response-reserve:`), and out of the **working room** that
remains each standing part — the system-prompt part (the rendered System prompt
and every engine-owned standing block) and the Context files part —
reserves what it actually measures plus **10% headroom**, never less than **2%** of the working
room. **History is the remainder**, floored at **50%** of the working room (a standing content
larger than that is the oversize notice's business, never a reason to switch the reducers off)
and capped at the emergency fold's own transcript budget, so the structural floor stays below it
at every window (ADR 0018 §8).
The former fixed **15%** (system prompt) and **25%** (file context) fractions survive only as the
**fallback for an unmeasured part** — test-only in the shipped engine, since `(*Agent).budget()`
always measures. Beside the reservations the Budget carries one **advisory ceiling**
(`StandingAdvisory`, still 15% of the working room): the Context files
oversize notice measures against that, never against the reserved share. The shares, the headroom,
the floors and the prune band are **code constants**, not configuration. See
ADR 0084.
It reads TWO ceilings: the **advertised window** (`Budget.Window`, the wall the server
enforces — read by overflow detection alone, since whether a request *will not fit* is the
server's question) and the **working ceiling** (`Budget.ContextLimit`, the room the session
chose to live in — what the allocation, the reply ceiling, the tool-result cap and the
compaction triggers all read).
They are the same number unless the **`working-window:`** key (top-level or per server
entry) names a smaller room, which is how a model advertising a very large window is made
affordable: bound the room rather than lie about the window.
_Avoid_: "context limit" for the advertised window (the Budget is the *allocation* within a
ceiling, and since the split its `ContextLimit` field names the **working** ceiling — the
advertised window is `Window`).
