---
title: Decide whether a Hook may advise the model (tier-C talk-back), or keep Hooks observe-only
status: done
type: task
priority: 2
aliases: [apogee-575]
created: 2026-09-07T17:46:37Z
closed: 2026-09-07T19:48:26Z
reason: "Decided in ADR 0076 (2026-09-07): Hooks become the observe cell of one Reaction core; user advise + gate admitted on day one, admission arm = fixed-text machinery arm non-inferior to Bypass (ADR 0009); ADR 0031 invariant 4 satisfied by construction (every handler on one core, provenance ledger attributes by reaction id); content carries the ADR 0064 §6 obligation. Supersedes ADR 0073 D2/D4/D5/D7 + veto/lint rejections and ADR 0071 D4/D6-B. CONTEXT.md Reaction surface = matrix."
---

Research recorded in docs/design/hook-talkback-findings.md (2026-09-07). The owner asked whether Hooks and Mechanisms should become one user-facing surface with three modes: event to external action; event to external action whose output returns to the model; both. Research finding: the proposal is exactly one tier of a six-tier design space (advisory injection), it does NOT require reopening the veto decision or the no-pre-event rule, and its execution machinery already exists (tools.RunHookSubprocess plus a facade-exported PreRequestHook calling Request.InjectContext). The Event stream cannot carry the talk-back modes, so it is one config shape over two execution paths. Blocking obstacle is ADR 0031 invariant 4 (benchable all the way up), whose worked example list names trigger-injected prompts; the counter-argument is ADR 0064 section 6, which answered the same unbenchability for the system prompt with a standing obligation rather than a gate. Next step is a grill, not a plan.

## Design

Do not model this as a Hook/Mechanism code merge. The runtime split is forced by EventSink.Emit returning nothing and by one tree-wide mutex serialising emission. What is unifiable is the CONFIG surface. Tier C only: adding veto (tier B), mutation (tier D) or continuation control (tier E) each reopens a separately settled decision, and tier E is what wedged every rival that shipped it.

## Acceptance

An owner decision recorded: either Hooks stay observe-only and the findings doc records why, or an ADR supersedes ADR 0073 decision 2 and its third rejection, addresses ADR 0031 invariant 4 explicitly, and names the bench arm.

## Log

- 2026-09-07 beads: greenfield design recorded in docs/design/reaction-core-greenfield.md (one Reaction core, two lanes, origin×class policy matrix, three config layers with adoption pin). Reframes this decision as 'which user cells on day one' rather than 'supersede five ADRs'. Still a thought experiment; grill next.
