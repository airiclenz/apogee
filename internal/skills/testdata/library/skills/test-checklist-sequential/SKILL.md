---
name: test-checklist-sequential
description: >
  Sequential single-dispatch variant of /test-checklist for small or
  context-constrained host models (roughly 4B-35B parameters, little context
  or compute). Same release test checklist — what changed since the last cut
  release, automated checks executed, step-by-step manual instructions for
  the rest, saved under docs/test-checklists/ — but the coordinator
  dispatches one sub-agent at a time with strict anti-stall scaffolding. Use
  ONLY when the host model is small or context-starved; on a frontier model
  use /test-checklist instead.
argument-hint: "Optional: 'since TAG'; 'name: SLUG'; 'no-run' (compile only); 'fresh' (discard a resumable run); 'record: T-03 pass, T-05 fail: NOTE' (tick off manual results)"
---

Fixture body — see testdata/library/README.md.
