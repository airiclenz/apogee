---
title: ADR 0031 decision
---

**The north star is engine-centric.** The subject of "Apogee becomes a platform" is the
Embeddable agent — the public module must stay sufficient to power *any* Driver: TUI
today, bench today, a scheduling/workflow daemon tomorrow. Whether that daemon ships in
this repo, this binary, or as a separate embedder is **deferred packaging**, not part of
the north star.

**Force: tiebreaker.** This ADR obligates no building. When two designs are otherwise
equal, the one that keeps the platform reachable wins; a change that closes one of the
doors below must argue against this ADR explicitly (and supersede it if it wins).

**Door-keeping invariants:**

1. **The engine is wire-silent.** ADR 0001's rejection of a serialized control surface
   stands unamended. Any network-facing surface (webhook listener, HTTP API, queue
   consumer) is composed by a Driver, which translates to in-process API calls — exactly
   as the bench composes forking. Non-Go integrations reach a Driver's protocol, never
   the engine's.
2. **The Approver seam is wait-tolerant.** Nothing in the engine may assume an
   interactive human at a keyboard: no internal timeout on `Approver.Approve`, `ctx` as
   the only cancellation. Parking, notification, and asynchronous human response are a
   Driver's composition over the existing synchronous seam.
3. **No first-party connectors, ever.** Neither the engine nor any first-party Driver
   ships service-specific data connectors (no Salesforce/Postgres/Gmail tools). External
   data access is MCP. The injectable tool registry stays an open extension point
   (ADR 0002) for
   third-party embedders composing their own tools at their own risk.
4. **Benchable all the way up.** Any layer that shapes model-visible behaviour —
   workflow scaffolding, trigger-injected prompts, step chaining — must itself be an
   embeddable library the bench can drive in-process; no model-facing behaviour may
   exist only inside a binary's `main()`. This generalizes ADR 0001 upward and extends
   the Bypass floor (ADR 0006,
   ADR 0009) to every future layer by construction: a
   daemon is a thin main over a workflow library over the engine, exactly as the TUI is
   a thin renderer (ADR 0011).

> **Amended 2026-09-27 (ADR 0087 D10).** Invariant 4's thin workflow layer is being built: the
> Workflow engine is a Driver-free library in its own engine package, re-exported through the root
> facade so the bench drives it in-process.

**Doors already held** (recorded, no new decision): the engine's `domain.Session`
envelope is Driver-neutral and resumable
(ADR 0022), and
every state root is Config-injected (ADR 0001) — a daemon brings its own roots;
`~/.apogee` layout is a Driver concern.

**Non-goals now** (consciously unbuilt until the thesis firms): daemon, triggers,
workflow schema, RBAC/multi-user, visual builder.

**Open questions recorded, not decided:** durable-across-restart approvals (a restart
mid-wait today loses only that Step; the session resumes at the quiescent boundary);
the retry unit for nondeterministic steps (Step, Turn, or workflow node); whether a
workflow node is a fresh session or a resumed one; daemon packaging; the shape of an
unattended eval harness.
ADR 0033
answers the fresh-vs-resumed question for the **scheduled-trigger** case only (fresh per
firing); the workflow-node case stays open.
ADR 0034
answers daemon packaging (an in-repo `apogee daemon` subcommand) and records the
cross-process contention settlement (the slot broker splits into llama-launcher's Gateway and
a deferred apogee-side resolver); durable-across-restart approvals stay open — the daemon
launches with ADR 0033's deny-and-record posture.
