---
title: "One bracket for every delegation ending (architecture review 2026-10-27 #5)"
status: parked
type: task
priority: 3
created: 2026-11-05T19:56:21Z
---

Found by docs/reviews/archived/architecture-review-2026-10-27.html (#5), re-verified 2026-11-05 at f7ec72b6; it re-raises and extends docs/reviews/archived/architecture-review-2026-10-14.html (#1, "a child run's bracket and ending are one module"), which this issue also tracks. The rules "every delegation books exactly one ledger row" and "every started phase gets one finished phase" are still kept by hand at about nine sites: preemptDelegation, refusePastCeiling, settleCancelledDelegation, stopQueuedDelegation, skipDelegation and runDelegation in internal/agent/dispatch.go, runDelegate's deferred recover block in subagent.go, and Spawn (its own recover) and spawnSubAgent (a copied phase pair) in workflowspawn.go; runChild in childrun.go covers started runs only. The ending is also still classified by several separate switches (classifyDelegation, delegationResult, subAgentEnding, workflowOutcome, undeliveredWorkflowReason, undeliveredReason), which the 2026-10-14 review found already disagreeing. Four fix commits were a site that forgot one rule (2635ab57, 630d59b8, 4dd18a08, 27654d54). Both reviews propose one bracket module around runChild that owns the run id, the started/finished pair, the recover boundary and the single ledger row, and classifies the end once into one ending value that the ledger word, workflow.Ending, UndeliveredReason and DelegationOutcome project from; each entry point supplies only what to run, or why it never started. Keeps ADR 0039 D4 (the bracket becomes each child's recover boundary); ADR 0083 §3 denied the delegate's runtime state as one value, so a plan must stay inside that denial.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
