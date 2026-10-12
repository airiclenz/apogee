---
title: ADR 0057 bounds (stated, not separately ratified)
---

- Deltas apply to the DEFAULT tool set only; an embedder-injected `Config.Tools` is the host's
  authority verbatim, exactly like `DisabledTools` today (ADR 0031's doors — the engine still
  never reads config; resolution runs in the composition root).
- Built-in tools only: MCP-served tools ride their own surface, untouched.
- The roster is plain config, not a Mechanism — no gating, no Bypass interaction.
- A sub-agent resolves its own model's profile, so a delegation running a different model
  (sub-agent-server work) gets its own roster for free.
