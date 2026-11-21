---
title: "A delegation request is parsed once (architecture review 2026-10-27 #4)"
status: done
type: task
priority: 3
plan: 2026-11-21 - 00 - delegation-one-value-plan.md
created: 2026-11-05T19:56:21Z
closed: 2026-11-21T18:30:53Z
reason: "Pursued and delivered by plan 2026-11-21 - 00 - delegation-one-value-plan (archived at ed287516), items 11-12: one delegationRequest parser, parsed once per dispatch slot after the pre-tool-exec Moment."
---

Found by docs/reviews/archived/architecture-review-2026-10-27.html (#4), re-verified 2026-11-05 at f7ec72b6. The sub_agent arguments (tools.SubAgentArgs) are still decoded independently at six sites in internal/agent: askedSeat and stepCapFor (dispatch.go), runDelegate and delegationLabel (subagent.go), and asksBackground and backgroundSubAgentResult (workflowcall.go). askedSeat, stepCapFor and delegationLabel swallow a parse error and fall back to a default, and backgroundSubAgentResult re-runs the blocking path's validation (task, continue, seat) in its own copy. The review proposes one module that turns (call, what this Agent offers) into a validated delegation request or a refusal, built once in prepareCall and read from the dispatch slot by width sizing, the ledger label, the blocking run and the background launch, so the next argument edits one place. No ADR conflict; the refusal strings are model-facing and must stay byte-identical, background prefixes included. Composes with #10 (the offer value it reads).

## Acceptance

- Decided: pursued as a plan or closed as not worth it.

## Log

- 2026-11-21 Airic Lenz: Triage 2026-11-21 at 7869cca7 (read-only sub-agent check, not re-verified): VALID — six SubAgentArgs decodes remain (dispatch.go:173,938; subagent.go:1004,1418; workflowcall.go:331); one fix with delegation-offer-type-probes
