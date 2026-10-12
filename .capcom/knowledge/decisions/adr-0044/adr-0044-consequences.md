---
title: ADR 0044 consequences
---

- **`RebindSpec`'s exclusion note is reversed and must be rewritten.** The clause "the model
  profile (`model-profile` is global, not per-model — see Config.Profile; SetProfile is its
  separate, explicit door)" no longer describes the system: the profile joins Model,
  SystemPrompt, MaxContextTokens and EnableMechanisms as a per-model binding. `SetProfile`'s
  "leaves it alone when the Upstream's loaded model changes" goes with it.
- **Two clauses of older records are superseded — clauses, not whole ADRs.**
  ADR 0024 §4's
  "what stands across a rebind" list, where the model profile stands with "`model-profile` is
  global, not per-model", and
  ADR 0028's
  Consequences note that the switchable profile abstraction "stays deliberately global". Both are
  struck in place and point here; everything else in those two records stands.
- **`SetProfile` narrows to one job:** applying a profile to the CURRENT model when the config
  changed under a stable model. It keeps its mid-Exchange refusal; Rebind stays atomic and
  idle-only.
- **A wrong profile is safe to auto-apply, which is why no confidence gate is owed.** It is
  visible (the notice names the pattern that fired), reversible (one user entry), and bounded to
  request/parse shaping — no bench-honesty claim rides on it, unlike a Validated set, where a
  wrongly applied Mechanism stack would contaminate an A/B arm (ADR 0016). The
  ADR 0006 floor is untouched: the profile is
  wire dialect, not a Mechanism, and it is on under Bypass exactly as it is today.
- **New package `internal/profiles`** (entries, pattern match, shipped set, notice posture),
  shaped like `internal/validated` and importing `internal/domain` only.
- **Config grows `model-profiles:` as a pattern→profile map** reusing the existing block schema
  unchanged, layered like `mechanisms:` (a present map in a nearer layer replaces the farther one
  whole), and loses the global key.
- **ADR 0045's Delegation
  target consumes this**: the profile latched for a Sub-agent server is the one resolved for the
  model that server is OBSERVED to serve, through the same `Resolve`.
