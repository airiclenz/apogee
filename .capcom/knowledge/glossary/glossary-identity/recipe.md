---
title: Recipe
---

**Recipe**:
A saved **Workflow** definition a human writes into a **Skill**'s header: a declarative list of
named stages — fan-out, merge, pick (a helper's output becomes the next stage's items), verify,
script, ask (a question to the user) and bounded repeat — with simple conditions on **Receipt**
fields between them. Anything computed (splitting a repo into parts, renumbering claims) is a
script stage, never a language feature. Invoking the skill starts a workflow from its recipe with
only its declared inputs filled in (for `audit`: scope and focus) — the model never writes a multi-stage workflow itself.
Ratified 2026-09-27 (ADR 0087 D6).
A cancelled `/<id>` launch keeps the user's line and the stopped result lines in the conversation
(the **Exchange**'s kept recipe opening), and re-running the same line picks the workflow up where it
stopped (amended 2026-09-29, ADR 0088 D1).
_Avoid_: "workflow" for the definition (the workflow is one execution of a recipe), "plan" (a
`docs/plans/` document), "script" (one stage kind). The one exception to "plan" is the in-memory
Go type `workflow.Plan`, the parsed stage list either source (a recipe or a `fan_out` call) produces;
prose still says recipe (2026-09-27).
