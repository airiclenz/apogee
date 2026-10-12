---
title: ADR 0046 considered and rejected
---

- **A fixed constant default** (say 8192 everywhere): simpler to read, but it disagrees with the
  reserve on every window that is not the one it was tuned for — the same two-numbers-for-one-thing
  defect this record closes.
- **A top-level `max-output-tokens:` key**: the ceiling would then describe the session rather than
  the slot, and a delegation routed to a different server would carry the wrong one.
- **Leaving it to a Mechanism** (or to `SamplingParams` and a hook): it would be absent in Bypass,
  which is where an unbudgeted session most needs the floor.
- ~~**Retrying or continuing a reply that hit the cap**: burns the same tokens again for the same
  outcome; the honest failure is the cheaper answer and the one the operator can act on.~~ *Struck
  2026-09-19 for the retry half (decision 4's second note): a capped reasoning-only reply is
  retried once at twice the cap. Continuing a reply stays rejected.*
- **Salvaging the visible text of a cut-off reply**: a reasoning-only reply has none, and half an
  answer committed as a Turn is the blank-assistant-message failure wearing better clothes.
- **Capping the auxiliaries too**: `internal/title/title.go` and `internal/agent/compact.go`
  already bound themselves at 4096 and are not turns; nothing about them is unbudgeted.
