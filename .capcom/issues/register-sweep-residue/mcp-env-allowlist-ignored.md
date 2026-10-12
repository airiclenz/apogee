---
title: "env-allowlist: on a non-stdio MCP server is silently ignored"
status: done
type: bug
priority: 3
labels: [mcp, residue]
plan: 2026-09-03 - 01 - driver-parity-and-residues-plan
aliases: [apogee-1ov.3]
created: 2026-09-03T18:41:29Z
closed: 2026-09-04T10:53:07Z
reason: "delivered by plan docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md, item 17 (env-allowlist: on a non-stdio MCP server gets a notice)"
---

- [ ] **`env-allowlist:` on a non-stdio MCP server is silently ignored.** The key is meaningful to
  the stdio launch only (`internal/mcp/transport.go:62,172-176`); an `sse` or `streamable-http`
  entry that sets it (`internal/config/config.go:2276`) gets no validation notice, matching the
  existing silent treatment of `command:` / `args:` on http transports. A human who narrows the
  environment of a remote server is told nothing about it having no effect.
