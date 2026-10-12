---
title: ADR 0056 considered options
---

- **`set -e` alone as the incident fix** — rejected by evidence: the POSIX AND-OR exemption
  means the incident's own script survives the preamble; only the kill-on-denial watch stops
  it. The plan originally ratified the preamble as sufficient; the escape-probe work
  falsified that and the owner redirected the item to the watch (the incident report's fix A).
- **Config-gating the preamble or the watch** — rejected: both are safety floors in the
  ADR 0006 sense; a knob to turn them off is a knob to reintroduce the incident.
- **Blocking the denial at Confine time with a prompt** — rejected: ADR 0012 settled that the
  subprocess surface is OS-fenced, not gated; the watch keeps that (no prompt — the call just
  ends, labeled).
- **Scratch under `~/.apogee/sessions/<id>/scratch/`** (the incident report's sketch) —
  rejected: sessions storage is a flat `<id>.json` store; a sibling `scratch/` root keeps the
  session store layout untouched and gives GC one sweepable root.
- **Prompt-level hardening alone** (tell the model to use absolute paths and `set -euo
  pipefail`) — rejected as primary: guidance a model can forget; shipped only as the
  `{{scratch}}` guidance line on top of the structural fixes.
- **Making the mutation warning a Mechanism** — rejected: Mechanisms are gated and benched;
  this is a floor that must hold under Bypass, exactly like the clamp (ADR 0006).
