---
title: ADR 0034 consequences
---

- ADR 0031's open-questions list shortens: daemon packaging is answered here (in-repo
  subcommand). **Durable-across-restart approvals stay open** — the daemon launches with the
  deny-and-record posture, and the cross-session approval inbox that would unlock Ask-Before
  schedules remains its own future design.
- The daemon becomes the third Driver, and the sequencing recorded in ADRs 0031/0033 stands:
  `apogee headless` (a thin CLI over `internal/run`) precedes it as the tripwire that keeps
  every capability off the TUI-only path.
- `schedules.yaml` becomes apogee's second declarative config surface, with validate-and-swap
  semantics; its envelope is the extension point through which triggers and workflow actions
  later arrive without migration.
- Daemon Firings appear in the TUI's `/sessions` browser labeled by schedule; short-cycle
  schedules will eventually need `keep: N` and browser filtering, both additive.
- `apogee daemon install` puts three OS-specific supervision surfaces into v1 scope — a
  recorded turnkey-over-lean choice.
- No wire surface exists on the engine or between apogee processes. The first apogee wire
  surface will be the webhook listener of the trigger wave, composed by the daemon Driver
  exactly as ADR 0031 invariant 1 prescribes.
- Cross-repo: the Gateway's behavior (priority queue, stickiness window, memory budgets,
  single-host v1 with federation later) is llama-launcher ADR-0013's to own; apogee consumes
  it as an ordinary OpenAI-compatible endpoint plus a request-level priority hint.
- `CONTEXT.md` gains **Daemon** as a canonical term; **Gateway** is a llama-launcher term,
  referenced from here, not duplicated.
