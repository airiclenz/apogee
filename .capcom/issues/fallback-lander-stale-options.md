---
title: TUI fallback lander reads options snapshotted before a /settings edit
status: open
type: bug
priority: 3
plan: 2026-11-02 - 01 - fallback-followups-plan
created: 2026-11-02T14:12:35Z
---

The TUI `sessionFallback` lander snapshots `live.options()` when the resolver is first built (cmd/apogee/fallback.go:513; sessionFallback at cmd/apogee/fallback.go:392-437), so a fallback walk after a `/settings` edit reads stale options. Landed in item 10 (b5511769) of plan 2026-11-02 - 00 - midrun-model-fallback-plan.

## Why

A setting changed live (a window pin, a key) is ignored when the session later falls over.

## Acceptance

- A fallback walk after a /settings edit lands with the edited options, pinned by a test
