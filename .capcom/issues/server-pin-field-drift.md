---
title: No test keeps every MCP server field in its adoption pin
status: open
type: task
priority: 3
created: 2026-10-18T18:44:05Z
---

ADR 0104 pins a repo MCP server whole, but `mcpServerConfig.execPin` (`internal/config/execpin.go:72-89`) lists the fields it pins by hand. A field later added to `mcpServerConfig` (`internal/config/config.go:2732`) and not added there would change what the server runs without re-proposing it. No drift test guards this.

Deferred out of plan `2026-10-18 - 00 - project-config-exec-keys-plan`.

## Acceptance

- A test in `internal/config/` fails when `mcpServerConfig` gains a field that `execPin` does not pin (for example, by reflecting over the struct's yaml tags and comparing with the pinned field names, with an explicit exemption list).
