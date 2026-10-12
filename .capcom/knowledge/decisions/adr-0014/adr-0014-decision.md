---
title: ADR 0014 decision
---

**Guided decomposition (`guided_decomposition`) is a Mechanism that steers the model's own
primary call to enumerate subtasks, intercepts the enumeration into `sub_agent` calls, and
delegates them one per Turn — it never calls the Upstream itself and never spawns a child
outside dispatch.** Concretely:

**1 — It is a Mechanism, not a structural reducer.** Budget and Compaction earn structural
status because without them the request literally does not fit; a non-decomposed task still
fits — degraded but coherent — so decomposition is a *quality* hypothesis, not a feasibility
requirement. As a Mechanism it is off under Bypass, giving the
ADR 0009 gate an honest control arm. Capability is
`proactive-nudge` — no fourth Capability value; the catalogue already reads the capability
broadly (`cached_content_intercept` mutates pending tool calls under the same label), and a
one-member class would touch ADR 0006 for
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
