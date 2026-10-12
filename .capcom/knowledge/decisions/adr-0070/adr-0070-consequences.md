---
title: ADR 0070 consequences
---

- A stock install now arms two Mechanisms. The `/settings` mechanisms list shows them `on` with no
  block present, so the pane reads honestly; switching one off writes `<id>: false`.
- **A non-empty `mechanisms:` block still means manual control** (ADR 0016), and a block that names
  only `tool_use_enforcer: false` is one — it suppresses auto-application of a Validated set. That
  is a real cost of turning an off-ramp off and is the user's own choice.
- A Validated set that never named an off-ramp now runs with them anyway, since `withOffRampFloor`
  folds the floor into the applied set. That is the intended reading of ADR 0016's "a model with no
  Validated set runs the D1 floor (structure + off-ramps)", now true of a model *with* one too.
- Documents stating the old blanket default were amended in the same commit: `README.md`,
  `CONTEXT.md` (Bypass, Mechanism, Off-ramp), `docs/manual/configuration.md`,
  `docs/manual/commands.md`, the catalogue's D1 line and Table B rows, the shipped `config.yaml`
  template, and the code comments at the enable seams.
- The catalogue's D1 stays the rule for the other nineteen rows. A future Capability wanting the
  same exemption needs its own record; "recovery guarantee, fires only after a failure, survives
  Bypass" is the test this one passed.
