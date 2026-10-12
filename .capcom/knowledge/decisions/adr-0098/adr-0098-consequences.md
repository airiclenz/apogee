---
title: ADR 0098 consequences
---

- ADR 0097 §6 said
  the personal `/cut-release` calls the Scoop and winget make targets. That caller is now
  `cmd/release publish`.
- `.claude/.gitignore` gains an exception for `skills/`, so the pointer files can be committed.
- The release runbook in `docs/manual/building.md` changes in the same plan as the tooling.
