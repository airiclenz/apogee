---
title: ADR 0104 consequences
---

- The adoption package learns structured entries and file pins; `adoption.Entry` stays
  schema-agnostic, the config layer building the canonical text. For a structured kind the record
  also keeps the adopted text under its kind and handle — no longer fingerprints alone — so a
  re-ask can name what changed (§11).
- `config.KeyClass` rows for `mcp-servers` and `reactions` become granting; `projectWalk`, the
  MCP and reaction loaders, `reconnectMCP` and `reloadReactions` read the layered set.
- The adoption pane gets a body and a re-ask reason, specified in `docs/layout/` before it is built.
- A repo reaction's script is re-hashed at every load and reload, which reads each named file once.
- A repo can still ship a script that does harm once adopted; as with ADR 0096's `go test`, the
  manual states it beside the feature.
- Plan: `2026-10-18 - 00 - project-config-exec-keys-plan`; issue `project-config-exec-keys`.
