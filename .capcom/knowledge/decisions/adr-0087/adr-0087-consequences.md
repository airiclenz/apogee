---
title: ADR 0087 consequences
---

- New CONTEXT.md entries: **Workflow**, **Recipe**, **Receipt** (and **Background workflow**, ADR
  0089). **Fan-out ceiling** now names `sub_agent`.
- The tool roster grows by `fan_out` (off by default), `finish` (workflow children only), and
  `workflow` (ADR 0089). `internal/tools/registry.go`'s pinned counts and the README move with them.
- `/implement-plan`'s per-item implement → verify → commit loop is not a recipe. It stays the
  model's own work.
- ADR 0088 settles what `esc` does to a workflow and to plain helpers. ADR 0089 settles background
  workflows.
- A workflow's PlanHash covers the bound plan — inputs substituted and shell-quoted into each `run:` — and its items, as JSON. So a change to how recipe values are rendered or quoted makes a re-issued recipe start a fresh folder instead of resuming, and a change to how `workflow.Plan` serialises makes every stored `plan.json` hash differently, so a named-folder re-run is refused (`ErrFolderMoved`). A new Plan field is therefore `omitempty`, keeping the hash of every plan without it (2026-10-10, docs/plans/archived/2026-10-10 - 01 - audit-split-windows-plan.md).
