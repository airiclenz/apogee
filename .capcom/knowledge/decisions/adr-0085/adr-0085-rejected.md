---
title: ADR 0085 considered and rejected
---

- **A dynamic cost/speed/reliability scoring router** (the brainstorm's Layer 4) — rejected, for
  three reasons. Scoring by cost needs pricing data, and pricing tables are provider-tuned: the
  per-provider tables are precisely the OpenRouter-tuned trap this ADR exists to avoid. The
  typical user runs one or two servers and has nothing to choose between. And above a provider
  that already scores and routes, apogee would double-route — two schedulers second-guessing each
  other over a sample apogee sees only a sliver of.
- **Typed, provider-specific routing fields** (`provider.sort`, `order`, …) — rejected: it is the
  vocabulary-learning Decision 1 refuses, and every gateway spells it differently. The opaque
  overlay serves them all.
- **`request-extra:` that only adds keys, never overrides** — rejected in favour of the Merge
  Patch overlay: a passthrough that cannot adjust a key apogee already sends (a sampling field, a
  `reasoning` block) forces a typed option for each one. The reserved keys fence off the ones
  whose override would break the protocol.
- **A side-channel observer on the Client** — rejected: it would hand the provider a Driver
  dependency and bypass the event stream every Driver already consumes.
- **Persisting stats per session** — rejected: a session is too short to carry a meaningful
  percentile, and the question a user asks at the picker ("which server is fast?") spans sessions.
- **Estimating tok/s from delta text** when the server reports no usage — rejected: a figure
  apogee cannot stand behind is worse than none.
