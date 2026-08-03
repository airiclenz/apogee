---
Status: accepted
---

# The local-platform north star binds every future layer to the embeddable engine

## Context

Unified-memory scaling (Apple M-class; M7 Ultra rumoured up to ~1.5 TB) is on track to
make on-prem inference viable for small and medium companies around ~2027. The owner's
strategic bet (grilled 2026-08-03) is that Apogee should be able to grow into a **local
AI automation platform** — scheduled and webhook-triggered runs, a thin workflow layer,
enterprise data access, governance; a local-first analogue of cloud automation
platforms. The bet is the *trend*, not the exact specs; it survives the rumours being
off by 2×.

Nothing needs building yet. The risk this ADR addresses is different: doors close
through small local decisions — a capability reachable only by keypress, an approval
welded to a live prompt, a "harmless" wire endpoint on the engine — not through the
absence of a grand design. Meanwhile the load-bearing prerequisite already exists:
[ADR 0001](0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md)
makes the loop an embeddable library with injected state roots, and two **Drivers**
(TUI, bench) already prove the pattern a future daemon would join.

## Decision

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
   ([ADR 0002](0002-tools-are-an-open-extension-point-mechanisms-are-curated.md)) for
   third-party embedders composing their own tools at their own risk.
4. **Benchable all the way up.** Any layer that shapes model-visible behaviour —
   workflow scaffolding, trigger-injected prompts, step chaining — must itself be an
   embeddable library the bench can drive in-process; no model-facing behaviour may
   exist only inside a binary's `main()`. This generalizes ADR 0001 upward and extends
   the Bypass floor ([ADR 0006](0006-bypass-mode-is-the-mechanisms-off-floor.md),
   [ADR 0009](0009-the-ab-decision-rule.md)) to every future layer by construction: a
   daemon is a thin main over a workflow library over the engine, exactly as the TUI is
   a thin renderer ([ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md)).

**Doors already held** (recorded, no new decision): the engine's `domain.Session`
envelope is Driver-neutral and resumable
([ADR 0022](0022-sessions-persist-per-turn-as-dual-representation-records.md)), and
every state root is Config-injected (ADR 0001) — a daemon brings its own roots;
`~/.apogee` layout is a Driver concern.

**Non-goals now** (consciously unbuilt until the thesis firms): daemon, triggers,
workflow schema, RBAC/multi-user, visual builder.

**Open questions recorded, not decided:** durable-across-restart approvals (a restart
mid-wait today loses only that Step; the session resumes at the quiescent boundary);
the retry unit for nondeterministic steps (Step, Turn, or workflow node); whether a
workflow node is a fresh session or a resumed one; daemon packaging; the shape of an
unattended eval harness.

## Considered options

- **Candidate direction (no force)** — rejected: door-keeping without tiebreaker force
  is advisory, and silent door-closing is the exact failure mode.
- **Product-centric subject** (the binary becomes the platform) — rejected: front-loads
  an identity change and strains the single-binary promise before any platform code
  exists.
- **Engine grows a versioned wire API** — rejected: re-opens what ADR 0001 closed for
  reasons that still hold; Drivers can expose protocols without taxing the engine.
- **Curated first-party connector set** (SQL, S3, IMAP) — rejected: the first step onto
  the maintenance treadmill, and naming the exception invites it.
- **Engine-only bench discipline** — rejected: recreates the proxy-era unmeasurable-
  scaffolding problem one layer up, precisely where unattended operation makes it most
  dangerous.

## Consequences

- Future design sessions cite this ADR as a tiebreaker; door-closing proposals must
  supersede it explicitly.
- The first mechanical consequence of invariants 1–4 is the deferred **`apogee
  headless`** runner: once it exists, any capability that only works through the TUI
  visibly breaks the headless path instead of silently accreting — the architectural
  analogue of the Builder guard test. No schedule is implied; sequencing lives in plans.
- `CONTEXT.md` gains **Driver** as a canonical term.
