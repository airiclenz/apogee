---
title: ADR 0057 considered and rejected
---

- **A separate `tool-rosters:` pattern map** — keeps the profile pure wire-shape at the cost of
  a second per-model map with identical matching; two places to key the same model.
- **Per-server rosters** — the roster describes a model, and the launcher swaps models under one
  endpoint.
- **Disabled-only axis** — cannot express default-off/profile-enabled, the promoted use case.
- **Full replacement roster** — churns against every release's roster evolution.
- **Global `tools.disabled` as a hard off** — kills the off-by-default/on-for-one-model
  pattern; the ladder keeps global as the default word, not the last word.
- **Keeping rule-4 whole-profile replacement** (or axis-wise for the tools axis alone) — the
  silent wire-shape wipe on the most common edit; a half-axis-wise map means two resolution
  rules side by side.
