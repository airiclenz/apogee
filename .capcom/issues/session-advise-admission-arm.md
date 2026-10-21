---
title: apogee-sim admission arm for the session-start advise slot
status: done
type: task
priority: 3
plan: 2026-10-21 - 00 - user-session-advice-plan
created: 2026-10-19T17:02:11Z
closed: 2026-10-21T12:34:38Z
reason: Retired as a gate by adr-0076-amend-1021; resolved by plan 2026-10-21 - 00 - user-session-advice-plan (archived at 51b3a10d)
---

ADR 0076 D5 shipped the user advise cell only after an admission arm proved its injection path (slot, fence, cap) is not itself a regression. Session-start advice uses a different path: a fenced block inside the session's next user message, ahead of the prompt (ADR 0076 amendment 2026-10-19). That path is unproven, so the user `advise:` key on `session-started` stays refused until it is measured.

Wanted: an apogee-sim arm, pre-registered like the stage-3 arm (`../apogee-sim/docs/plans/2026-09-11 - 02 - advise-admission-arm-pre-registration-v2.md`): an engine-origin Go handler returning a fixed neutral sentence at `session-started`, vs Bypass, under ADR 0009, on the target model class. apogee-sim work, not apogee.

## Acceptance
- A pre-registered arm reports a verdict; a pass is recorded where `session-start-advise` can cite it.

## Log

- 2026-10-21 Airic Lenz: 2026-10-21: adr-0076-amend-1021 retires this as a gate; closed by the session-start-advise plan.
