---
title: ADR 0096 consequences
---

- Allowing `go test` or `make lint` runs whatever the repo's code does, and in `allow-edits` the model
  may edit that code. Every agent with an allowlist has this; the manual states it beside the feature.
- **Not in scope, filed separately:** MCP servers and Reactions in the project config (both execution
  keys D10 admits as adoption-gated); the sandbox loosens `NetworkAllow` and `confine-writable-paths`;
  the rest of the `apogee-9d8` tool × mode matrix.
- The AGENTS.md "single `~/.apogee` dotdir" decision stands for the **global** config home; the
  project layer is the stage-2b reopening A2 reserved.
