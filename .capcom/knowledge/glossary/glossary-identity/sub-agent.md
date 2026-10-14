---
title: "Sub-agent: a nested agent loop spawned for one delegated sub-task"
---

**Sub-agent**:
A nested, focused agent loop the top-level agent spawns for one delegated sub-task, with its
own Session. It is itself an instance of the **Embeddable agent**, spawned in-process; its
events nest into the parent's event stream at **`Depth = parent+1`**. An **unrouted** child's
**context window is not reduced**: it inherits the parent's `Config` verbatim
(`internal/agent/subagent.go`), so it works against a window — and a Budget over it — of the same
size the parent has. A child **routed** to the **Sub-agent server** works against the **Delegation
target's** window instead (that entry's `context-window:` pin, else the per-slot window the target
server advertises), which is a different model's window and may be smaller or larger than the
parent's (ADR 0045,
ADR 0066).
When that entry names **neither** — no pin, and nothing observed — the routed child keeps the
**parent's** window rather than running windowless: no window at all would leave its Budget
inactive, its folds on the conservative unknown-window ceiling and its readings unmeasurable.
How full that window got is **visible per run**: the TUI paints the run's own reading on its
collapsed call block (`N tool calls · 12k/32k · <gist>`) and `apogee headless` prints one
`sub-agent: <used>/<limit> · <the delegation's name, else the task>` line on stderr per run.
Each reading belongs to the agent that filled it — it never moves the parent's gauge, never
accrues to an enclosing run, and is spelled against **that agent's own window** (a routed child's
`<limit>` is the Delegation target's, not the session's). Its privileges are always **≤ the parent's** (mode, guardrails, Confinement,
tool set) — see ADR 0005.
The *shape* is ADR 0013:
the model reaches it through a **`sub_agent` tool** that dispatch treats as a **recursion
point** (not a leaf — never confined/gated as a unit; each *child* call gets the per-call
disposition one level down), the orchestrator threads mode/approver/confiner/tool-subset
verbatim-or-stricter, the sub-agent's **live guard state is isolated** (a fresh
circuit-breaker — `Guards.ForSubAgent`; its audit events reach the parent's sink by depth and
spawn ids) over a **shared, read-only
dangerous-action floor** (unloosenable one level down), and recursion is depth-bounded by the
`delegate-max-depth` key (`Config.Delegation.MaxDepth`), default **1**: the top-level agent
delegates and its delegates are never offered `sub_agent`; `2` lets a sub-agent delegate in turn
(ADR 0013 decision 4, superseded 2026-09-15). A delegation blocks its parent's Turn by default; where
`fan_out`'s background gate is open, a call may carry `background: true` and run as a one-item
**Background workflow** instead — the same child, its report delivered in the finish note; a call
carrying both `continue` and `background: true` is refused, and headless and daemon runs never offer
the switch (ADR 0094,
2026-10-05). A `max_steps` ask above `delegate-max-steps` is
applied as the cap and the delegation's result says so in one appended line. A delegation the
engine capped at a **Step cap** (or its token or time sibling), whose Exchange **faulted**
(ADR 0082), that the human **stopped** (**Stop (a delegation)**), or that completed under a name its
call gave, can be picked up again rather than re-spawned: the call's `continue: "<name>"`
argument names a **Retained delegation** — kept for the rest of the **Session**, saved with it and
restored on resume — and spawns a fresh child from the original task and that delegation's rounds
under a cap of its own, with `task` as the continuation instructions; the retained
name, `tools` and `output_path` are inherited wherever the call leaves them unset.
The parent also reads the **delegate ledger** (2026-09-19, ADR 0076 addendum, `apogee-clb`): the
engine's own row per delegation of the current Exchange — spawn order (the model's call order,
reserved per pooled group), the delegation's name, its outcome (`completed | capped | faulted |
cancelled | stopped | refused | skipped` — `stopped` the human's stop of that one delegation, ADR
0086 D4; `refused` a call no child was built or started for; `skipped` a delegation a queued
message pre-empted before it started), the head line of a fault
or refusal, and the RESOLVED `output_path` target — rendered onto every request tail as the engine
note `[engine — delegations]` … `[end engine — delegations]` (`#<n> <name> — <outcome>[: <cause>] —
output <present|missing|none>`, presence read from disk at render time) once the Exchange holds
two or more delegations or any that did not complete — a `skipped` row counting toward neither, so
a pre-emption adds no note of its own but is listed when one fires — ahead of the wrap-up
directive, with the same system-prompt fallback for a tail that is not a tool result. Facts, no imperatives; structural
(no key, on under Bypass); in memory only, cleared as the next Exchange opens — the retained
delegations outlive it, the ledger does not — never in the conversation or the snapshot
(`internal/agent/children.go`).
The tool subset a child inherits is the parent's menu **minus the human's seat**: `ask_user`
and `present_document` are withheld from every sub-agent at every depth (owner call,
2026-09-14 — a delegation has no seat at the human's prompt; a child reports the question or
the deliverable's path in its result and the parent asks or presents), which supersedes ADR 0039
§6's queued child questions. The `sub_agent` call's **`tools`** argument narrows further —
`"read-only"` for every inherited tool Plan mode admits on every target (`planAdmits`, the class
rather than the bare `ReadOnly()` declaration) plus `sub_agent` where the depth rule offers it,
or an array of tool names — always intersected with the parent's roster, never widened
(ADR 0005); an unknown name is refused with a result naming it.
When one reply carries several `sub_agent` calls, the **top-level** agent runs them
**concurrently** up to the server's **Parallel agents** cap (depth-0 only — a sub-agent's
own delegations run serially inline; a reply split across **Delegation seats** takes the
smaller of the two caps), and only up to the **Fan-out ceiling** — the `sub_agent` calls past it
are refused, not queued; every event a sub-agent emits carries the engine-minted **run id** of its
delegation and the **call-ID** of the `sub_agent` call that spawned it, so interleaved streams stay
attributable — by the run id, because a call-ID may repeat across one fan-out and the run id never
does; the parent's call and result carry the same run id, which is how a run is paired with the
block it answers
(ADR 0039,
amended 2026-09-24).
A delegation the group has **not started** when an **Interjection** is staged **now** for its
parent — sent with `ctrl+g` while the turn runs, in the main prompt or a Run view — is
**skipped**, not run; a message sent with ⏎ instead waits for the whole wave, queued members
included, and lands once every member has finished (ADR 0025, amended 2026-10-05). A skipped
delegation commits, in call order, the tool result `sub-agent not started: the user sent a
message while this group was running; delegate again if the task is still needed` with a
finished phase and no started one, so the model is told and may delegate again; the TUI paints that
row with the neutral `not started · your message` verdict in the marker tone, no ✓ (never `scheduled`,
never the red `error`: the skip is the human's act, not a failure), and opens it onto the skip's own
words, like a refusal at the depth bound. A delegation a turn cancel caught queued reads `stopped by
you` the same way. The children already running finish untouched — a message never
cancels anything — and a child holding a message sent now in its own mailbox skips its unstarted
grandchildren the same way, while an ordinary message, or the model's own `workflow message`,
skips nothing (ADR 0039, amended 2026-09-14 and 2026-10-05). Headless runs and Firings have no
queue, so nothing is ever skipped there.
What a delegation is CALLED is its **Delegation name**, and the rule is three-deep: the name its
call gave — an optional `name` argument on the `sub_agent` call, normalised to a trimmed first
line — else a **generated** one that lands once the run is under way, else the delegated task's
first line. It is display identity — and, for a retained delegation, the handle
`continue` spells back — never privilege.
A **running** child is **addressable**, and by the handle it already has: its engine-minted **run
id**, which never repeats where a call-ID may (ADR 0086 D5, `apogee-interject-by-run-id`).
`Agent.InterjectChild(runID, in)` appends a message to that child's engine-side **mailbox** — its
sibling `Agent.InterjectChildNow(runID, in)` appends one sent *now*, the only kind that makes the
child skip its unstarted grandchildren —
recursing into registered children so a grandchild is reachable from the top-level agent and
answering `domain.ErrNoSuchChild` when no such child is running; the goroutine driving that child
pops the mailbox before every Step after the first and delivers each message through
`Agent.Interject`, so it commits at the child's own between-Steps boundary as an ordinary
**Interjection** — the one place a `Run` drains for an embedder, because per ADR 0013 D5 nobody
else can drive a child's Steps. Every queued message earns one
`domain.ChildInterjectionEvent{Input, Landed, Reason}` on the shared sink, so a Driver never has to
guess whether it arrived — nor, for one that did not, why: the `Reason` says the child `completed`,
was `capped`, `faulted`, `stopped` by the human or was `cancelled` before the boundary the message
waited for, or `refused` it there. `Agent.StopChild(runID)`, keyed and recursing the same way,
is the other call a Driver makes on a running child — see **Stop (a delegation)**. A child that received such messages returns its result with the trailer
`(the user sent N message(s) to this sub-agent while it ran)` on every outcome but a cancelled
dispatch — a parent reading a result shaped by instructions it never issued must be able to see
that from the result alone. Every `sub_agent` result — report or fault — is **capped at 64 KiB**
(`delegateResultMaxBytes`, a constant): a larger body keeps its first 48 KiB and its tail around
the one elision marker every reducer renders, and the body notes and that trailer are appended
after the cut, so they always follow the capped tail intact; and a finished child's closing text
whose most frequent content line — a trimmed line of 4 or more runes carrying a letter or digit —
occurs 50 times or more is **degenerate narration**, answered as an error
result naming the repeat count with the first 20 lines only, never handed over as a report.
Addressing a child buys no privilege (ADR 0005 stands), and all of
it exists at **depth > 0 only**
(ADR 0063); the TUI's surface for it
is the **Run view**.

One shape to know when reading events: `domain.AuditEvent` carries a `CallID` of its own — the
**audited** call, the tool call that record is about — which **shadows** the promoted
`EventBase.CallID`, so an observer reading `ev.CallID` on an audit record gets the audited call;
the spawning one still travels, reached as `ev.EventBase.CallID`.
Bare "agent" means the **top-level** agent unless qualified as "sub-agent".
_Avoid_: "child agent" (says nothing about the privilege bound), "worker".
