---
title: ADR 0016 context
---

Phase 4's bench evidence forced the question this ADR answers. On gemma-4-e4b-it-qat the
full 17-Mechanism stack failed the non-inferiority gate twice (the 20260706 aggregate,
replicated in-bundle by the 20260708 Screen); the Screen convicted no single Mechanism
("underpowered for diffuse harm"); the 20260714 Probe then showed the stack **minus
`truncate_history` is non-inferior** to Bypass (W+ = 102.0, p = 0.0003, N = 14) — but the
Probe is exploratory by construction (its hypothesis was data-suggested), so under L9 it
licensed a ledger claim only. No convicted set exists, so the Screen → Confirmation path is
closed, and the question "what evidence licenses which curation action?" was left open.

Meanwhile the evidence says models differ: the stack demonstrably hurts gemma, and the qwen
campaigns (once the engagement post-mortem landed) say nothing either way. New models arrive
constantly. Any curation rule that requires proven *benefit*, or that makes global claims
from single-model evidence, either throttles curation into a full-time job or claims more
than the data supports.

There is also a statistical trap worth recording: "the harm sits in `truncate_history`" was
never tested directly. Stack-with-TH failing the gate while stack-without-TH passes it is a
comparison of two verdicts, not a test of the difference (the Screen's direct removal
contrast gave p = 0.1104 — not significant). Any rule that needed the *attribution* claim
confirmed would need a new, adequately-powered head-to-head design; the suite's N = 14 tasks
already failed to power it once.
