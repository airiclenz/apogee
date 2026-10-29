---
title: "ADR 0104 addendum of 2026-10-18: the spawn-time pin check crosses the import graph as a domain value"
---

Learned in the exec-keys run (docs/plans/archived/2026-10-18 - 00 - project-config-exec-keys-plan.md); it says how decision 4's spawn-time check is wired. `internal/config` imports `internal/mcp`, `internal/reactions`, `internal/tools`, `internal/adoption`, `internal/userexec` and `internal/security`, and `internal/agent` does not depend on config, so no spawn path can call config. The check is passed down as a value instead: `domain.ProjectPin`, whose `Admit()` re-reads the pinned files, implemented by config as the plain `adoptedPin{root, text}` and carried on `domain.Reaction.Pin` and `mcp.ServerConfig.Pin`. Its values must compare by `reflect.DeepEqual`, because arming compares one observe lane with the next to decide whether to swap (`internal/run/run.go`). A plan that says "the spawn calls config.X" needs this seam.
