---
title: "Delegation offer is one value (architecture review 2026-10-27 #10)"
status: parked
type: task
priority: 3
created: 2026-11-05T19:56:21Z
---

Found by docs/reviews/archived/architecture-review-2026-10-27.html (#10), re-verified 2026-11-05 at f7ec72b6. The engine still learns what delegation it offers by looking a tool up by name in the registry and type-asserting it to *tools.SubAgent or *tools.FanOut to read a bool: publishesSeatChoice, publishesSubAgentChoice, publishesFanOutChoice, publishesContext and offersContext in internal/agent/subagent.go, offersBackground and offersBackgroundSubAgent in workflowcall.go, and planOffersDelegation in resolution.go; withoutSeatChoice rebuilds the child roster's descriptors by hand (ee14cf6b nearly lost context there). Each new option grows a bool, an ArgRoles branch, a schema variant and a probe. The review proposes one offer value per Agent, built with the roster, that renders the sub_agent and fan_out schemas, answers which options are live, and derives the child's offer in one place; #4's request parser reads it. It must preserve ADR 0005 (child at most parent), ADR 0069 D3 (run_on at depth 0), ADR 0094 D1 and ADR 0031 (background is the Driver's opt-in), and the schema bytes are model-facing and must stay identical.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
