---
title: apogee-sim admission arm for the session-start advise slot
status: open
type: task
priority: 3
created: 2026-10-19T17:02:11Z
---

ADR 0076 D5 shipped the user advise cell only after an admission arm proved its injection path (slot, fence, cap) is not itself a regression. Session-start advice uses a different path: a fenced block inside the session's next user message, ahead of the prompt (ADR 0076 amendment 2026-10-19). That path is unproven, so the user `advise:` key on `session-started` stays refused until it is measured.

Wanted: an apogee-sim arm, pre-registered like the stage-3 arm (`../apogee-sim/docs/plans/2026-09-11 - 02 - advise-admission-arm-pre-registration-v2.md`): an engine-origin Go handler returning a fixed neutral sentence at `session-started`, vs Bypass, under ADR 0009, on the target model class. apogee-sim work, not apogee.

## Acceptance
- A pre-registered arm reports a verdict; a pass is recorded where `session-start-advise` can cite it.
