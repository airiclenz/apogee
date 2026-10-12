---
title: ADR 0059 consequences
---

- ADR 0008 gains a dated amendment pointer; its `Tool` interface clause reads "stateless across
  Turns, except a Console, which is live host state and never survives a snapshot".
- CONTEXT.md gains **Console** under *Safety and autonomy*.
- `docs/design/tool-surface-findings.md` closes its "persistent terminal / PTY session" grill
  line against this record.
- A saved implementation plan follows (house format, `docs/plans/`); nothing ships from this
  record alone.
