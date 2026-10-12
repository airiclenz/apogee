---
title: ADR 0089 considered options
---

- **Background for user-launched recipes only.** It is simpler, but a frontier agent could not park
  a long audit and keep working, which is the case the owner values most. The per-class gate keeps
  small models on the blocking path instead.
- **Wait for the user instead of waking.** It is kept as the `workflow-wake: off` setting. It is not
  the default, because the agent (or the user) started the workflow to use its result.
- **Stop-only control (a `stop` field on `fan_out`).** It is smaller, but the agent could neither see
  progress nor redirect a helper going wrong. The owner chose the full tool, behind the same bench
  gate.
