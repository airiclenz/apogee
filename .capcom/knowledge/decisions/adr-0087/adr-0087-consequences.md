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
