---
title: Grill the reserved user shape(view) Reaction cell (ADR 0076 D3)
status: open
type: task
priority: 3
aliases: [apogee-8za]
created: 2026-09-07T19:50:52Z
---

ADR 0076 put the user-origin shape(view) cell (edit post-tool-result / pre-request / history-rewrite by a user script: strip ANSI, trim test output, redact) in the matrix but RESERVED it: no motivating case yet, and an edit leaves no span for the provenance ledger to record. Needs its own design + grill before it ships: ledger design for edits, trust posture (inbound, exec-fenced like advise), bench arm, and a case. Do not implement under the ADR 0076 plan.
