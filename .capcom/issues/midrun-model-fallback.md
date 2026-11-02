---
title: Mid-session fallback to a second model when the primary keeps failing
status: done
type: feature
priority: 3
labels: [feature]
plan: 2026-11-02 - 00 - midrun-model-fallback-plan
created: 2026-11-01T19:26:29Z
closed: 2026-11-02T14:28:02Z
reason: "Resolved by plan 2026-11-02 - 00 - midrun-model-fallback-plan (archived at 206e9e61): ADR for per-entry server fallback settles trigger, scope, one-way switch and per-Driver reporting; implemented across items 1-11. Known gaps filed separately: anthropic-api-error-fallback, background-workflow-no-fallback, fallback-lander-stale-options, routed-rehome-root-pins."
---

Model choice is fixed before a session starts: if the configured server dies or keeps erroring partway through, the session retries it or fails; nothing hands the session to a second configured server/model. Reported by Seqcom (request apogee-midrun-fallback, run 20261031T1223Z, apogee v0.29.1) for unattended headless runs on a local model with a cloud fallback.

Needs design before work: switching model mid-transcript changes context window, tokenizer and wire (openai vs anthropic) under a live session, interacts with context-fill accounting, sub-agents and prompt caching, and must not regress the bare loop. Questions: trigger (N consecutive failures vs M minutes of errors), whether it is per session or per turn, whether it ever switches back, and how the TUI/headless/daemon Drivers each surface the switch.

## Why

An unattended run on a flaky local server loses most of its time window when the server fails mid-run.

## Acceptance

- A design (ADR or plan) settles trigger, scope, switch-back and how each Driver reports the switch, before any code
