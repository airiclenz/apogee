---
Status: accepted
---

# The sub-agent orchestrator is a dispatch recursion point; its live guard state is isolated, its dangerous floor shared read-only

## Context

[ADR 0005](0005-sub-agent-privileges-are-bounded-by-the-parent.md) fixed the *policy* — a
sub-agent's privileges are always ≤ its parent's. The Phase-3 plan's **D2** sketched the
*shape*; P3.13 is where sub-agents are actually spawned, so the remaining calls have to be
settled in code:

- **Where does a sub-agent live in the dispatch flow?** A sub-agent is the embeddable `Agent`
  ([ADR 0001](0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md)) one
  level down. It is exposed to the model as a tool (`sub_agent`), but it is unlike every other
  tool: it is the **recursion point**, not a leaf — its "execution" is a whole nested loop,
  each of whose child tool calls must itself get the full per-call blast-radius disposition
  ([D5](../plans/archived/phase-3-detail-plan.md) / [ADR 0012](0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md)).
- **What does a sub-agent inherit, and how is "≤ parent" made structural** rather than a thing
  a careless port can drop (apogee-code's `SubAgentOrchestrator` constructed the loop with *no*
  approval manager and *no* mode — the exact hole ADR 0005 closed)?
- **The carried `/code-review` finding:** `security.Guards` bundles the dangerous-action floor,
  the circuit-breaker, and the audit log. Breaker and audit hold **mutable** pointer-backed
  state. A naïve "thread Guards verbatim into the sub-agent" therefore makes the sub-agent and
  the parent **share one breaker and one audit trail** through the aliased pointers — a
  sub-agent's runaway tool-loop would trip the *parent's* breaker, and a fresh delegated task
  would start pre-tripped by the parent's earlier failures. Decide **share-vs-isolate** and
  implement it.
- **What stops an unbounded tower of sub-agents** (each level is a full nested loop)?
- **How does stepping/cancel/snapshot interact** with a loop running inside another loop's
  tool dispatch?

## Decision

**A sub-agent is a nested `Agent`, dispatched at a recognised recursion point, constructed
through a single orchestrator that threads the parent's privileges bounded — and it runs with
ISOLATED live guard state over a SHARED, read-only dangerous-action floor.** Concretely:

**1 — The `sub_agent` tool is the recursion point, not a leaf.** It is a plain `domain.Tool`
carrying the model-facing name/description/schema, registered in the default set
(`internal/tools/sub_agent.go`). It carries **no disposition marker** — not `ReadOnlyTool`, not
`workspaceScopedWriter`, not `ExternalEffectTool`, not `SubprocessTool` — because the sub-agent
is **never confined or gated as a unit**. Dispatch (`resolveAndExecute`) recognises
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
conversation/pending-input/approval-cache (the ADR 0008 statelessness boundary).

**3 — Live guard state is ISOLATED; the dangerous floor is SHARED read-only.** `Guards` gains
`ForSubAgent()`, used to build the child's bundle:

- **Fresh `CircuitBreaker`** (same threshold) and **fresh `AuditLog`** — the sub-agent's
  runaway loop trips *its own* breaker, not the parent's, and its audit trail is its own. The
  two loops cannot interfere through aliased pointers.
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

**5 — Stepping is top-level-only; a sub-agent runs atomically within the parent Turn.** The
driver runs the nested `Agent` to its Exchange boundary in one shot
([broad plan #15](../plans/implementation-plan-apogee-merge.md)). While it runs, the parent is
mid-tool-dispatch — **not** at a quiescent boundary — so (a) **no snapshot lands mid-sub-agent**
(the parent's next boundary is *after* `sub_agent` returns; the snapshot schema's "suspended
sub-agent" slot stays **reserved-but-always-empty in v1**, forward-compat only); (b) **cancel
mid-sub-agent rolls back the whole parent Turn** — the cancel propagates to the nested loop's
next boundary, which returns, and the orchestrator surfaces `dispatchCancelled` so the parent
rolls back to its **pre-`sub_agent`** quiescent boundary with no partial result; (c) resume is
coarse by design — *before* or *after* a sub-agent, never inside it. Nested stepping
(suspend/resume a sub-agent at its own boundary) is a later, snapshot-schema-additive change
behind the same single-shot driver seam.

## Considered options

- **Thread `Guards` verbatim (share breaker + audit + floor)** — *rejected* (the carried
  finding): a sub-agent's runaway trips the parent's breaker and inherits a pre-tripped one;
  audit trails conflate. The only thing that *should* be shared is the read-only floor.
- **Isolate everything, including a fresh dangerous floor per sub-agent** — *rejected*: a
  per-sub-agent floor is a per-sub-agent place to *re-derive* (and therefore loosen) the floor,
  reopening exactly the hostile-input hole the tighten-only merge closes. The floor must be one
  shared, unloosenable instance.
- **Make `sub_agent` a normal leaf tool with a disposition marker** — *rejected*: a sub-agent's
  blast radius is not a single call; confining/gating it "as a unit" is meaningless and would
  either over-block (gate the whole delegation) or under-block (skip the per-child disposition).
  The recursion point gets the per-child disposition right by construction.
- **No depth bound (rely on the model not to recurse)** — *rejected*: a small model can loop;
  an unbounded tower is a resource footgun. A small fixed ceiling with defence-in-depth is cheap
  and total.

## Consequences

- **`security.Guards` gains `ForSubAgent()`** (fresh breaker + fresh audit, shared read-only
  `Dangerous`); the misleading `Guards` doc comment is corrected to describe the live-state
  aliasing honestly. A sub-agent breaker trip provably does **not** trip the parent's, and a
  sub-agent cannot loosen the dangerous floor (both tested).
- **`Agent` gains a `depth` field**; `base()` becomes a method stamping `Depth` so every event
  an `Agent` emits carries its nesting level. Top-level events stay `Depth == 0`; sub-agent
  events are `Depth == 1`. **P3.14** turns this from *tolerate* into *render* (the TUI frames a
  `Depth > 0` block); it needs only the `Depth` on events, which this ADR establishes.
- **The default tool set gains `sub_agent`** (`DefaultTools` is now ~19 built-ins; 2026-07-23:
  21 with the host-gated `ask_user`/`present_document` — ADR 0019). It is
  offered in every mode **including Plan** (it is the recursion point, bounded one level down —
  a Plan sub-agent inherits Plan, so its children are read-only), and is **withheld at the depth
  bound**.
- **Acceptance held under `-race`:** a Plan-parent sub-agent cannot write (inherits Plan); a
  subset-narrowed sub-agent cannot call a tool the parent has but the subset omits; an Auto
  sub-agent confines a child subprocess, runs a child Apogee write path-safety-bounded, and
  still Approval-gates child MCP/external (the per-call disposition, one level down); nested
  events arrive at `Depth == 1`; a child tool panic recovers at the parent boundary (ADR 0007)
  without killing the parent Exchange; a cancel during a sub-agent rolls the whole parent Turn
  back; the depth bound holds via both the withheld tool and the defensive refusal.
- **Forward-compat preserved:** the snapshot schema's "suspended sub-agent" slot stays
  reserved-but-empty, so nested stepping drops in later without a schema break.

## Post-v1 realisation (apogee-code track) — the child sees a LIVE mode tightening, tighten-only

D2/§2 say the child inherits the parent's Mode "verbatim (never loosened)." That was written
before the mode became **live**: the apogee-code track made Shift+Tab cycle the autonomy mode
mid-session (`SetMode`, [ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md)'s
realisation), and a sub-agent runs its whole Exchange — many Turns on a small local model — inside
one parent tool dispatch. Freezing the parent's mode at spawn (`childCfg.Mode = a.Mode()`) is
correct for **loosening** (a parent that later loosens must not loosen a running child) but leaves
a **tighten**-direction hole: a mid-delegation Shift+Tab from Auto down to Plan flips the footer
and the TUI promises it "takes effect on the next tool call," yet the child kept auto-approving
writes on its frozen spawn mode until its Exchange ended — the child running Auto while the parent
is now Plan, the exact tighten-direction failure ADR 0005 forbids.

The realisation refines — it does not change — the Decision: privileges are still **≤ parent**,
and loosening mid-flight is still impossible. It closes the tighten gap by making "≤ parent"
track the parent's *live* mode, not just its spawn-time snapshot:

- **`newChildAgent` injects a tighten-only live view.** The child captures the parent's
  `modeMu`-guarded `Mode` accessor as a closure (`Agent.liveMode`), **not** the shared mode field
  or mutex pointer — the child reads the parent's live mode race-free but has no seam to mutate it
  (the same "read-only view, no re-derivation" shape §3 uses for the shared dangerous floor). A
  top-level agent has a nil view and behaves exactly as before.
- **The disposition takes the tighter of the two modes.** `Agent.effectiveMode` returns
  `TighterMode(parentLive, spawnMode)` — a new ladder-index helper in `internal/domain/config.go`
  where Plan < Ask-Before < Allow-Edits < Auto — so a parent tightening **below** the child's
  spawn mode gates/refuses the child's next call, while a parent loosening **above** it is ignored
  (the child never rises above its spawn privilege). The spawn mode remains the ceiling; the live
  parent mode can only lower the effective floor.

**Acceptance:** a child spawned in Auto refuses its next write once the parent tightens to Plan
mid-run; a child spawned in Plan keeps refusing even after the parent cycles up to Auto (loosening
stays impossible); the parent's `modeMu` covers the child's cross-agent read under `-race`
(`internal/agent/setmode_test.go`, `internal/domain/config_test.go`).

## Clarification (2026-07-02) — guard tiers at the recursion point

§1 leaves the dangerous-action tiers at the delegation call implicit; made explicit while
collapsing the dispatch decision into the Resolution verdict:

- **Tier-1 (hard-refuse) applies to the delegation call itself.** A hard-refusal needs no
  human judgment, so it fails fast — no child loop is spawned just to flounder.
- **Tier-2 (force-approval) does NOT gate the delegation as a unit** — consistent with §1's
  "never gated as a unit." Nothing executes at delegation; the shared read-only floor (§3)
  re-fires on the child's *actual* dangerous tool call, which threads the parent's Approver.
  The human gets the speed-bump at the point of execution with the real command in the prompt
  (better judgment context than a prose task description), and is not double-prompted (a
  forced gate ignores the allow-for-session cache, so a delegation-level gate would not have
  pre-cleared the child's anyway).

This is a rule of the Resolution table (leaf verdicts honour force-approval; `Delegate` does
not), pinned by a table-test row, not an accident of check ordering.
