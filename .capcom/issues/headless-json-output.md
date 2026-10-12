---
title: "Headless JSON output: apogee headless --format json emits a JSONL stream of domain events"
status: done
type: feature
priority: 2
plan: 2026-09-07 - 00 - headless-event-lines-plan
aliases: [apogee-tvw]
created: 2026-09-07T10:48:28Z
closed: 2026-09-07T19:28:47Z
reason: "Shipped in v0.21.0: apogee headless --format json emits the ADR 0075 JSONL Event stream with run_started/run_finished frames; plan 'docs/plans/2026-09-07 - 00 - headless-event-lines-plan.md' complete and archived."
---

Gap 3 of the 2026-09-06 contender gap assessment (docs/handoffs/2026-09-06 - 02 - contender-gap-assessment.md), ACCEPTED by the owner 2026-09-07.

Today headless prints plain text: no --format json, no server Driver. Rivals: opencode's TUI is a client of 'opencode serve' (OpenAPI 3.1 + SSE); pi ships --mode json / --mode rpc; qwen-code runs a daemon over HTTP+SSE.

Fit: this is ADR 0031's thesis (engine-centric, Drivers) cashed in. 'apogee headless --format json' emitting a JSONL stream of 'domain' events is the whole of this bead. Structural, engine-side, no bench gate. Effort M.

OWNER CALL 2026-09-07 (handoff Q4): JSONL FIRST, serve later. 'apogee serve' as a fourth Driver is explicitly OUT of this bead — it becomes its own ADR and plan once the event contract has proven itself on a real consumer. Do not design the HTTP/SSE surface here.

Next step: grill-with-docs (may amend ADR 0031's Driver list), then implement-plan Write mode.

## Acceptance

apogee headless --format json emits one JSON object per line, one per domain event; the human-readable default output is unchanged; the event contract is documented in docs/manual/headless.md.

## Log

- 2026-09-07 beads: GRILLED 2026-09-07 (grill-with-docs). Fifteen design calls settled and owner-ratified; six design-review gaps ratified the same day (SIGPIPE, blocking-Emit consequence, run_finished on every exit path, decision-12 rationale, 'Event lines' term, assessment paragraph in the ADR). Written: docs/adr/0075-the-headless-event-stream-is-a-versioned-driver-protocol.md (accepted, with Consequences), CONTEXT.md 'Event' + 'Event lines' entries, ADR 0031 Driver-list amendment. Bead apogee-czf (live text output) filed and blocked on this one. Plan: docs/plans/2026-09-07 - 00 - headless-event-lines-plan.md (implement-plan Write mode, regression-checked). Next: /implement-plan Execute mode on that file.
