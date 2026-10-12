---
title: "Write the Reaction stage 3 plan: the user advise and gate cells"
status: done
type: task
priority: 2
deps: [reaction-stage2-plan]
plan: 2026-09-09 - 01 - reaction-core-stage-3-plan
aliases: [apogee-rxj]
created: 2026-09-08T06:10:33Z
closed: 2026-09-13T18:58:26Z
reason: Stage 3 plan written and executed; archived at 4447df47 (16/16 items)
---

ADR 0076 D3/D5/D6/D7/D8: gate as an Approver stage (5s, escalate-to-ask on timeout or crash, deny text engine-authored); advise 10s fail-open, 8 KiB cap, fenced trailer on the closing tool result for tool-shaped Moments and a fenced tail user-role message otherwise, provenance ledger recording {reaction, origin, moment, turn} with ephemeral spans dropped on resume, terminal-tool secret redaction before the fence, workspace exec fence; /settings 'what the model saw this Turn'. The advise cell ships only after decision 5's admission arm passes on apogee-sim: an argv advise reaction returning a fixed neutral sentence at post-tool-result, measured against Bypass under ADR 0009. Stage 2 rejects advise:/gate: at load with 'not yet shipped' (ADR 0076 amendment A7), so this plan lifts that rejection. Gated on the stage 2 plan landing.

## Acceptance

The plan's first item either LANDS the fixed-text admission arm (ADR 0076 decision 5, lives in apogee-sim: an argv advise reaction returning a fixed neutral sentence at post-tool-result, measured against Bypass under ADR 0009) or, where apogee-sim is not reachable from the executing checkout, names that gate explicitly as a verify-first item; every advise-cell item DEPENDS on it. The gate cell is not gated. Stage 2's load-time 'not yet shipped' rejection of advise:/gate: (ADR 0076 amendment A7) is lifted only by the item that ships the cell.

## Log

- 2026-09-09 beads: (owner): stage 3's acceptance is the fixed-text admission arm; the advise cell ships gated on it, so the plan carries an item that lands the arm or names the gate explicitly. Stage 2 plan-writing bead apogee-pjx closed the same day (plan archived at cebd60fd).
