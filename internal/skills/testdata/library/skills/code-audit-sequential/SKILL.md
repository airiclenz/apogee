---
name: code-audit-sequential
description: >
  Sequential single-dispatch variant of /code-audit for small or
  context-constrained host models (roughly 4B-35B parameters, little context
  or compute). Same high-signal audit and report, but the coordinator
  dispatches one sub-agent at a time with strict anti-stall scaffolding. Use
  ONLY when the host model is small or context-starved; on a frontier model
  use /code-audit instead.
argument-hint: "Optional: scope (files, directory, branch/PR, or 'everything'); add 'fresh' to discard a resumable run"
---

Fixture body — see testdata/library/README.md.
