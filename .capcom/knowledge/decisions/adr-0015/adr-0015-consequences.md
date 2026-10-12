---
title: ADR 0015 consequences
---

- **The bench arms any stack in one Config** — catalogued IDs plus experimental hooks,
  Bypass toggled per arm with identical construction — and computes valid arm sets from
  public descriptors.
- **`benchreadiness_test.go` sheds its `internal/mechanisms` and `internal/library`
  imports**: the ADR 0001 executable contract becomes a true external-surface consumer,
  proving the enable path it previously had to cheat around.
- **`Config.Mechanisms` is repositioned by documentation, not renamed** (v1 semver): its
  external role is the experimental-hook carrier; catalogued enablement is
  `EnableMechanisms`.
- **Descriptor fields become public API**: future descriptor growth (as `Requires` was in
  ADR 0014) is an additive v1 change and must be treated with API discipline.
- **Default-off posture is unchanged (D1)**: an absent or empty `EnableMechanisms` builds
  no catalogued Mechanism; flips to default-ON remain bench-evidence-gated
  (ADR 0009).
- **No CONTEXT.md change**: the grill crystallised no new term — "enable" was already the
  canonical enable-time verb in the Mechanism-descriptor entry.
