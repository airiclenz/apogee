---
Status: accepted
---

# Guided decomposition steers the primary call and serializes delegation through the recursion point

## Context

Small models do not spontaneously decompose an oversized task — handed "review this codebase"
on an 8k window, a 4B model reads files until the window bursts. Apogee owns the loop and
already has the delegation primitive (the `sub_agent` recursion point,
[ADR 0013](0013-the-sub-agent-orchestrator-is-the-recursion-point-with-isolated-live-guard-state.md)),
but nothing makes the model reach for it when a prompt or skill does not say so. The question
this ADR settles: how Apogee helps a model decompose a too-large task **without** the user
asking — and what shape that help takes without violating settled architecture.

The grill (2026-07-05) surfaced four constraints that forced the shape:

- **No Mechanism calls the Upstream.** Compaction's summarisation call is internal-to-a-Turn
  as a *structural* privilege ([ADR 0007](0007-step-turn-and-the-quiescent-boundary.md));
  granting Mechanisms their own model calls opens unanswerable questions (whose tokens, which
  Turn's effectiveness judgment, what the bench sees).
- **The recursion point is the single spawn path.** ADR 0013 §1 hangs Resolution, guard
  tiers, and event nesting off dispatch recognising `SubAgentToolName`; loop code invoking
  `newChildAgent` directly would be a second, uncovered spawn path — and would fabricate
  history the model never produced.
- **Auto-compaction is Exchange-boundary-only.** A multi-Turn fan-out lives inside *one*
  Exchange, so no generative reducer can fire for its entire life; only `tool_result_cap`
  (default-off) acts mid-Exchange. A fan-out design that ignores this re-creates the very
  window explosion it exists to prevent.
- **Cancel rolls the whole Turn back, and a sub-agent is atomic within its parent Turn**
  (ADR 0007; ADR 0013 §5). Anything that piles N children into one Turn makes a user's cancel
  during child 9 of 10 destroy nine completed child Exchanges.

## Decision

**Guided decomposition (`guided_decomposition`) is a Mechanism that steers the model's own
primary call to enumerate subtasks, intercepts the enumeration into `sub_agent` calls, and
delegates them one per Turn — it never calls the Upstream itself and never spawns a child
outside dispatch.** Concretely:

**1 — It is a Mechanism, not a structural reducer.** Budget and Compaction earn structural
status because without them the request literally does not fit; a non-decomposed task still
fits — degraded but coherent — so decomposition is a *quality* hypothesis, not a feasibility
requirement. As a Mechanism it is off under Bypass, giving the
[ADR 0009](0009-the-ab-decision-rule.md) gate an honest control arm. Capability is
`proactive-nudge` — no fourth Capability value; the catalogue already reads the capability
broadly (`cached_content_intercept` mutates pending tool calls under the same label), and a
one-member class would touch [ADR 0006](0006-bypass-mode-is-the-mechanisms-off-floor.md) for
taxonomy's sake. Suppression is standard strikes-3.

**2 — Shape: steer, don't call.** When the gate trips, the **pre-request** half injects an
instruction so the *primary* call answers "list the remaining independent subtasks, one per
line" — the enumeration costs a visible Turn, not a silent side-call. The **post-response**
half **intercepts** the list into `sub_agent` tool calls (text-becomes-tool-calls is already
how every non-native model profile is parsed), and dispatch executes them through the ADR 0013
recursion point unchanged — per-child Resolution, `Depth+1` events, guard tiers all inherited.
Synthesis needs no machinery: child reports land as ordinary tool results, and the model's
next primary call integrates them as its natural Turn. One Mechanism, two hook attachments
(the glossary already permits multi-point attachment); suppressing it disarms both halves as
a unit. It fails soft: no list to intercept → benign no-op.

**3 — Serialized fan-out: one delegation per Turn.** The intercept emits only the *first*
`sub_agent` call; the remaining items ride as a **Deferred Response Action** (the CONTEXT.md
definition widened from "a correction" to "a decision" to cover carried work), consumed at
pre-request each following Turn. This keeps a quiescent boundary between children: a cancel
loses at most the in-flight child, snapshots land between children, earlier reports leave the
protected most-recent Turn and become cappable, and the model gets a per-child decision point
to rescope, retry, or skip. Because the enumeration is the model's own visible response, the
session-state queue is just a cursor over honest history — recoverable by the model even
after suppression.

**4 — The descriptor gains a `Requires` relation; `guided_decomposition` requires
`tool_result_cap`.** Ten serialized child reports (each naturally bounded by the *child's*
ResponseReserve — a report is one model reply) still sum past a small window, and
auto-compaction cannot fire mid-Exchange; `tool_result_cap` is the only reducer that can keep
the accumulation down. That dependency was previously *inexpressible* — the descriptor
carried only `IncompatibleWith`. `Requires` is its dual: an **enable-time** constraint
(switching a Mechanism on without its requirements is a config error), so dependent
Mechanisms are benched and shipped as a stack. Live-suppression divergence (capping struck
out mid-queue) is accepted, as for any two co-fired Mechanisms. Cheap hygiene rides along:
the delegated task text asks each child for a compact report.

**5 — The gate is measured signals only, top-level only.** Two Budget-derived facts, no
semantic heuristics: (a) at Turn 1, resolved `@file` context exceeding its FileContext
allocation (the one moment size is fact, not prophecy); (b) mid-Exchange,
`HistoryExceedsAllocation()` going true while the model is still mid-work — the auto-compact
signal, which mid-Exchange currently has no consumer with a remedy. Verb-sniffing was
rejected: task size is semantic, and every false positive hijacks a fitting task — the exact
"Mechanism makes the model worse" failure the hard constraint exists to prevent. The gate
also no-ops when `sub_agent` is not in the offered tool set, and fires at **`Depth == 0`
only** for v1: a Depth-1 fan-out would run atomically inside one parent Turn (ADR 0013 §5),
re-creating the jumbo-Turn problem §3 exists to avoid, and an oversized subtask is the
parent's enumeration failure — the serialized decision point is where it gets fixed. Relaxing
the depth gate later is additive, gated on bench evidence.

**Self-regulation is entirely stock.** Both halves count as acts (R4); each act is judged by
the next Turn, which during the fan-out is a `sub_agent` dispatch — a successful child is
productive, a failed child is harmful, so three consecutive failing children suppress the
Mechanism with no custom logic. On suppression (strikes or Turn Budget) the queue is
**silently abandoned**: injections stop, nothing fires a farewell (a suppressed Mechanism
does not get an exit visa), and the model continues from honest history, which contains the
full enumeration and every completed report.

## Considered options

- **Structural reducer (Compaction's peer, on under Bypass)** — *rejected*: the bench would
  be structurally blind to whether decomposition helps or hurts; fragmented fan-outs
  plausibly lose cross-file coherence, and only a control arm can tell.
- **Silent internal Upstream call + direct orchestrator invocation** — *rejected*: creates a
  Mechanisms-may-call-the-model capability class, a second spawn path outside the ADR 0013
  recursion point, and fabricated history explaining where N results came from.
- **All N children in one Turn** — *rejected*: cancel during child 9 destroys nine child
  Exchanges; all N reports pile into the one *protected* most-recent Turn where no reducer
  may touch them (the explosion returns); no adaptation between children.
- **A fourth Capability value (`task-shaping`)** — *rejected*: a one-member class, an
  ADR 0006 touch, and the catalogue already reads `proactive-nudge` as "proactively
  intervenes", not "injects prompt text".
- **Prompt-shape (verb) gating** — *rejected*: "review this function" would hijack a fitting
  task into enumeration; misfires are hard-constraint violations, and both false directions
  are unavoidable when guessing semantics from keywords.
- **Depth-1 firing in v1** — *rejected*: multiplies the acknowledged one-child atomicity
  coarseness by an entire nested fan-out; additive to enable later with evidence.

## Consequences

- **A principle now constrains every future Mechanism:** a Mechanism steers the primary call;
  it never makes its own Upstream call and never spawns sub-agents outside dispatch.
  Compaction's internal call remains a structural privilege, not a precedent.
- **The Mechanism descriptor grows `Requires`** (CONTEXT.md updated): config validation gains
  an enable-time dependency check, and the bench evaluates required stacks as a unit —
  `guided_decomposition` is benched with `tool_result_cap`, never alone.
- **"Deferred Response Action" is widened** (CONTEXT.md updated): the deferred thing is a
  *decision* — a correction, or carried work such as the remaining-items queue.
- **CONTEXT.md gains the fifth context-and-history operation**: capping and truncation *cut*,
  Compaction *summarises*, guided decomposition *avoids* — "decompose" joins the
  must-not-conflate list.
- **Ships default-off, bench-gated** like every Mechanism (D1): it flips on only when the
  ADR 0009 non-inferiority gate passes for the decomposition + capping stack.
- **Parked separately** (TODO.md, 2026-07-05): mid-Exchange auto-compaction at quiescent Turn
  boundaries under pressure — a structural-reducer contract change with its own blast radius;
  if it ever lands, it would loosen (not remove) this Mechanism's `Requires` coupling.
