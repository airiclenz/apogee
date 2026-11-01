---
title: "apogee headless: user-set wall-time and turn budgets with a distinct exit code"
status: open
type: feature
priority: 2
labels: [feature]
plan: 2026-11-01 - 00 - viewdiff-orientation-and-headless-budgets-plan
created: 2026-11-01T19:26:29Z
---

apogee headless has no run budget: an outer supervisor enforces its cap with SIGTERM, and a SIGKILL after the grace window loses in-flight work and the record. Exit codes today are 0-3. Wanted: optional --max-wall-time and --max-turns (and config keys under the same resolution as other headless settings: flag over APOGEE_* over config.yaml) that, when reached, stop the run at the next safe point, save the record, print a stderr line naming the budget, and exit with a new distinct code (e.g. 4). Reported by Seqcom (request apogee-visible-budgets, run 20261031T1223Z, apogee v0.29.1).

Out of scope here: telling the model its remaining budget. That is model-facing behaviour above the Floor guards, so it ships off and waits for bench evidence (AGENTS.md invariant; ADR 0076); file it separately if wanted.

No default budget: a slow machine makes apogee slower, never broken, so a budget only exists when the user sets one.

## Why

A supervisor-imposed SIGTERM destroys work an in-process stop would keep, and the caller cannot tell a budget stop from a failure.

## Acceptance

- --max-wall-time <duration> and --max-turns <n> exist on apogee headless and resolve flag > env > config like the other headless settings; unset means no budget
- Reaching either budget ends the run cleanly with the record saved and a stderr line naming which budget and its value
- That stop exits with a code of its own, documented in apogee headless --help and docs/manual/headless.md alongside 0-3
- --format json emits the stop reason in run_finished
