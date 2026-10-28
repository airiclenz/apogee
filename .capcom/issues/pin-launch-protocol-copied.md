---
title: Each Project pin caller copies the verify-before-launch protocol
status: done
type: task
priority: 2
plan: 2026-10-27 - 01 - project-pin-admit-and-adoption-snapshot-plan
created: 2026-10-27T20:58:58Z
closed: 2026-10-28T09:27:59Z
reason: "Resolved by plan '2026-10-27 - 01 - project-pin-admit-and-adoption-snapshot-plan' (c5e89d6d): ProjectPin is Admit-only with one *domain.PinMovedError refusal used by MCP and reactions; an unreadable pin is refused as moved (re-read drops it, per ratified design call)"
---

domain.ProjectPin exposes Root/Changed/Programs, and the MCP stdio launcher (internal/mcp/transport.go resolveStdioProgram) and the reaction spawner (internal/agent/syncexec.go spawnReactionArgv, checkProjectPin) each run "nil? refuse → Changed() → moved-refusal → Dir = Root() → admit Programs()" by hand, with diverging handling of an unreadable pin (MCP aborts the connect set; reactions refuse and cue a re-ask). Fixes came in pairs (d45e436a; 82cceef4, cb46e44e). Architecture review 2026-10-27 item 3.

## Why

The verify-before-admit invariant of ADR 0104 §4 lives at every call site; a third launch kind would copy it again.

## Acceptance

- ProjectPin has one operation that verifies the pinned files and yields the launch dir and admitted programs, or one typed pin-moved refusal; the getters are not part of the interface
- An unreadable pin is refused as moved on both sides: an MCP repo server is skipped and re-proposed, not a connect failure
- The MCP skip and the reaction re-ask cue both key off the one refusal type; refusal wording is unchanged
