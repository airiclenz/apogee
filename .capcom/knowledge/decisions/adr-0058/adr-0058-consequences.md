---
title: ADR 0058 consequences
---

- `Entry.spellsThinking` is replaced by `spellsThinkingStyle` and `spellsThinkingEffort`;
  `Resolve` composes `Thinking` from two supplier calls.
- ADR 0057 decision 5 carries a dated pointer here; its "three axes" reads as three axes, one of
  which resolves in two halves.
- The package doc of `internal/profiles` and `CONTEXT.md`'s Model-profile and Thinking-effort
  entries say the thinking axis defers in two halves; the profile doc surfaces in the config
  layer (`internal/config/config.go`, the seeded `config.yaml`) follow suit.
