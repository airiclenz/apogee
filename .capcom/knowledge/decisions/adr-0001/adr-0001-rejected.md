---
title: ADR 0001 considered options
---

- **Harness inside the binary** (a `sim`/`bench` subcommand) — rejected: bloats the
  binary against its single-static-binary promise; the bench is a development-time
  instrument end users never run.
- **Drive the real binary over a serialized stdio protocol** (step/snapshot/intervene as
  JSON) — rejected: highest fidelity and language-agnostic, but neither is needed (both
  repos are Go), and it imposes a permanent versioned control surface on the agent —
  exactly the weight the small-binary goal resists.
