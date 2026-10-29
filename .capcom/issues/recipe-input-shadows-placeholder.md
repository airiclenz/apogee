---
title: A recipe input can shadow a script stage's run-time placeholder
status: done
type: bug
priority: 2
plan: 2026-10-29 - 00 - p2-fixes-plan
created: 2026-10-29T08:29:25Z
closed: 2026-10-29T11:56:23Z
reason: "Resolved by plan 2026-10-29 - 00 - p2-fixes-plan (items 1-2), archived at 979a6d7d: ValidateInputs refuses every reserved placeholder name from one workflow list; the agent binder and renderer read it."
---

A recipe input named `workflow_dir`, `part_bytes` or `SKILL_DIR` passes `workflow.ValidateInputs`, which reserves only `apogee` (`ApogeeInputName`). `bindPlanInputs` (`internal/agent/recipe.go`) substitutes `{<input>}` into a script stage's `run:` before `recipeScripts.render` fills the run-time placeholders, so the input's quoted value silently replaces `{workflow_dir}` or `{part_bytes}`, and turns `{{SKILL_DIR}}` into `{<value>}`, which render no longer recognises — the stage runs against the wrong folder, budget or script with no error. The brief placeholders `item` and `out` are kept unbound by a second, silent list (`reservedInputNames`). Found 2026-10-10 (docs/plans/archived/2026-10-10 - 01 - audit-split-windows-plan.md:163), confirmed against the code 2026-10-29.

## Acceptance
- A recipe declaring an input named `workflow_dir`, `part_bytes` or `SKILL_DIR` is refused by `ValidateInputs` with a Problem naming the placeholder it would shadow, as `apogee` is today; a test pins each name.
- The reserved names have one home that both `ValidateInputs` and the render placeholders read, so a new script-stage placeholder cannot be added without reserving its name.
- `docs/manual/workflows.md` §Inputs lists every reserved name.
