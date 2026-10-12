---
title: ADR 0018 context
---

A `/refocus` run against a 32k-window model died mid-task on the llama.cpp 400 —
`request (57546 tokens) exceeds the available context size (32768 tokens)`. The wire
classification was already right (`isContextOverflow` → `DeltaContextOverflow`), but the loop
folded that delta into the same terminal outcome as any stream fault: one `ErrorEvent`, then
`abandonTurn` — the Exchange over, the task lost, the history intact but useless.

Nothing proactive could reach the failure. Automatic Compaction is Exchange-boundary-only (the S2
`inExchange` gate), and a doc-heavy Exchange — assistant → whole-file read → assistant → … —
grows past the window exactly where the boundary trigger cannot fire. The only reducer that could
act mid-Exchange was `tool_result_cap`: a default-off, Bypass-disabled, self-regulation-
withdrawable Mechanism. And no loop-level bound existed on a single tool result entering history
at all — dispatch appended verbatim, `read_file`'s only ceiling being a 10 MiB refusal, so one
`read_file` of a large file could put more text into history than the whole window holds.

That left the guarantee resting on a config opt-in, which
ADR 0006 forbids: the structural reducers
(Budget, Compaction) are load-bearing parts of the agent, must stay on in the Bypass baseline, and
that baseline must be *functional* — a floor that quits at the first stumble passes the hard
constraint trivially. Overflow recovery therefore belongs with Budget and Compaction, not in the
catalogue.
