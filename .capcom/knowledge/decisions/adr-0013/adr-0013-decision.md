---
title: ADR 0013 decision
---

**A sub-agent is a nested `Agent`, dispatched at a recognised recursion point, constructed
through a single orchestrator that threads the parent's privileges bounded — and it runs with
ISOLATED live guard state over a SHARED, read-only dangerous-action floor.** Concretely:

**1 — The `sub_agent` tool is the recursion point, not a leaf.** It is a plain `domain.Tool`
carrying the model-facing name/description/schema, registered in the default set
(`internal/tools/sub_agent.go`). It carries **no disposition marker** — not `ReadOnlyTool`, not
`workspaceScopedWriter`, not `ExternalEffectTool`, not `SubprocessTool` — because the sub-agent
is **never confined or gated as a unit**. Dispatch (`resolveAndExecute`; since 2026-09-15 `prepareCall`, the one prepare phase of the per-call pipeline) recognises
`SubAgentToolName` *after* the always-on guardrails run but *before* the mode disposition, and
drives a nested `Agent` instead of executing a leaf. Each **child** tool call inside the nested
loop then gets the full per-call disposition one level down (a child subprocess confines, a
child Apogee write is path-safety-bounded, a child MCP/external call still raises Approval),
using the parent's threaded mode / confiner / approver / guardrails. The tool's own `Execute`
returns an error if ever reached, so a mis-wired recursion point fails loudly.

**2 — One orchestrator threads "≤ parent" structurally.** `newChildAgent` builds the nested
`Agent` from the parent's `Config` with the **same** Mode, Approver, Confiner, and
`confine-to-workspace` flag (verbatim — never loosened), a tool set that is a `registry.Subset`
of the parent's (`defaultSubAgentTools` — never an expansion; a privilege leak is structurally
impossible because the subset is built from the parent registry's own names), and Depth =
parent+1. The nested loop emits through the parent's `EventSink`, and because each `Agent`
stamps its own `Depth` in `base()`, the sub-agent's events nest at **`Depth = 1`** with no
per-call threading. The sub-agent starts **fresh** — only the delegated task, no parent
conversation and no parent pending input (the ADR 0008 statelessness boundary).

> **Amended 2026-09-15 (plan 2026-09-14 - 03, item 5).** The subset is no longer "the parent's
> whole set by default": `defaultSubAgentTools` withholds `ask_user` and `present_document` from
> every child unconditionally (the human's seat — owner call 2026-09-14), beside the recursion
> point at the depth bound, and the `sub_agent` call's `tools` argument narrows the child further
> (`"read-only"` = the inherited tools `planAdmits` plus `sub_agent` where offered; or a list of
> names, refused whole when one is unknown). Both are name-lists into the same `Subset`, so the
> structural "never an expansion" property is untouched.

> **Amended 2026-08-10:** the allow-for-session approval cache used to be on that withheld list and
> no longer is. It is now **session-scoped**, shared tree-wide through the approver queueing seam
> (`queuedApprover`, whose idempotent wrapping already gives a parent and all its descendants ONE
> object), so "allow for session" means the whole Session rather than the Agent that happened to
> ask: an allow granted in a sub-agent clears the prompt for its parent and its siblings and
> outlives the child that earned it. Forced gates are unaffected — they carry an empty
> `ApprovalRequest.CacheKey` and so neither read nor seed the memory. Conversation and
> pending-input isolation are unchanged. See plan `docs/plans/2026-08-10 - 03 -
> shared-session-approval-cache-plan.md`.

**3 — Live guard state is ISOLATED; the dangerous floor is SHARED read-only.** `Guards` gains
`ForSubAgent()`, used to build the child's bundle:

- **Fresh `CircuitBreaker`** (same threshold) and **fresh `AuditLog`** — the sub-agent's
  runaway loop trips *its own* breaker, not the parent's, and its audit trail is its own. The
  two loops cannot interfere through aliased pointers.
  *(Amended 2026-10-02 — see the last Amendment below: the `AuditLog` is gone; the fresh breaker
  stands, and a sub-agent's audit trail is told apart by depth and spawn ids on the event stream.)*
- **Shared `*DangerousActionGuard` by pointer.** The floor is read-only after construction
  (the guard exposes only `Inspect`/`Rules`, no mutator), so sharing the pointer is safe and
  intended: the sub-agent inherits the **exact** floor and has **no seam to re-derive, replace,
  or loosen it**. The dangerous-rules floor cannot be lowered one level down — the same
  hostile-repo invariant `MergeDangerousRules` enforces at construction, now preserved across
  the sub-agent boundary at run time.

This is the **isolate** answer to the carried finding. Sharing was rejected: it conflates two
loops' runaway-detection and audit trails, and an inherited *tripped* breaker would wrongly
refuse a fresh delegated task. Isolating the live state while sharing the floor read-only gives
each loop honest, independent runaway-detection without ever weakening the security floor. The
misleading "threads verbatim / no live state" comment on `Guards` is corrected to state the
truth (the pointers alias live state; use `ForSubAgent` to isolate).

**4 — Recursion is bounded (`maxSubAgentDepth = 2`).** Defence in depth: a child constructed
**at** the bound is never offered the `sub_agent` tool (`defaultSubAgentTools` withholds it), so
the model cannot even request a deeper spawn; and the recursion point **also** refuses
defensively if a spawn is somehow requested at the bound. Three levels (depth 0→1→2) is ample
for real delegation while making a runaway tower structurally impossible.

> **Superseded 2026-09-15 (plan 2026-09-14 - 03, item 4).** The bound is no longer the constant
> `maxSubAgentDepth = 2`: it is `Config.Delegation.MaxDepth`, fed by the top-level
> `delegate-max-depth` key, **default 1** — the session delegates, its delegates do not. The
> session-mining review (2026-09-14) found that a middle layer buys nothing on the task pool while
> putting every result one hand-off further from the human, so two levels is now an opt-in
> (`delegate-max-depth: 2`), never the default. The mechanism is unchanged: at depth ≥ MaxDepth the
> tool is withheld from the menu and the recursion point refuses defensively. The engine reads a
> zero field as the default 1, never as "no delegation" — an embedder's untouched Config still
> delegates once. "Three levels is ample" is retired with the constant.

> **Amended 2026-09-27 (ADR 0087 D9).** The bound is unchanged: workflow children do not delegate,
> and a workflow's `verify` and `merge` stages are sibling stages the engine runs, never grandchildren.

**5 — Stepping is top-level-only; a sub-agent runs atomically within the parent Turn.** The
driver runs the nested `Agent` to its Exchange boundary in one shot
([broad plan #15](../../../../docs/plans/archived/implementation-plan-apogee-merge.md)). While it runs, the parent is
mid-tool-dispatch — **not** at a quiescent boundary — so (a) **no snapshot lands mid-sub-agent**
(the parent's next boundary is *after* `sub_agent` returns; the snapshot schema's "suspended
sub-agent" slot stays **reserved-but-always-empty in v1**, forward-compat only); (b) **cancel
mid-sub-agent rolls back the whole parent Turn** — the cancel propagates to the nested loop's
next boundary, which returns, and the orchestrator surfaces `dispatchCancelled` so the parent
rolls back to its **pre-`sub_agent`** quiescent boundary with no partial result; (c) resume is
coarse by design — *before* or *after* a sub-agent, never inside it. Nested stepping
(suspend/resume a sub-agent at its own boundary) is a later, snapshot-schema-additive change
behind the same single-shot driver seam.

> **Amended 2026-09-19 (`apogee-2un`, Stage A).** (b) is unchanged: a cancel mid-sub-agent — or
> mid-pool, per the 2026-08-07 amendment — still rolls the whole parent Turn back to its
> pre-`sub_agent` boundary with no partial result. What Stage A changes is what happens to the
> **Exchange** after that rollback. The host used to close it with `AbortExchange`, which dropped
> every Turn the Exchange held, the opening prompt included (48 sessions saved `messages: null`);
> it now closes it with `Agent.SettleExchange`, which keeps the Turns that finished *before* the
> cancelled one and marks the cut with an ephemeral `[engine — cancelled]` engine note on the last
> tool result (ADR 0076 D6 — the saved record holds the kept results and no marker). So a cancel
> during child 2 of a pool still discards child 1's result with the rest of that Turn, but the
> Turns before the delegating one stand. A finer cut inside the pool — keeping the children that
> finished before the Esc — is Stage B and stays on `apogee-2un`. Plan `2026-09-19 - 01`.

> **Amended 2026-09-26 (ADR 0086 D4).** §5 gains a third ending. A child completes
> inside the parent's Turn, or a cancel rolls that Turn back with no partial result — or the human
> **stops** that one delegation (`Agent.StopChild(runID)`, `^x` in the TUI): the child and every
> delegation under it end with the parent's ctx still live, the engine folds the child's
> conversation at the stop as `finishAtFault` folds it at a fault (no wrap-up Turn), and the parent
> receives a non-error partial result opening `[stopped by the user — engine summary follows]` and
> goes on, nothing rolled back. (a) and (c) are unchanged: no snapshot lands mid-child, and resume
> stays coarse.

> **Amended 2026-09-27 (ADR 0088 D1–D2).** (b) is superseded: a cancel no longer rolls the parent
> Turn back. The Turn in flight is settled — a finished call keeps its real result, a running call
> gets a cancelled result, an unstarted one a not-run result — and in a delegation pool a finished
> child keeps its report while a running child is stopped exactly as `^x` stops it (fold, partial
> result, retention under its name). Every cancel sentence in the amendments below reads the
> same way, the retained set included: it is no longer restored to its Turn-start value. (a) and
> (c) stand: a child is still atomic to the snapshot, no longer to the cancel.
