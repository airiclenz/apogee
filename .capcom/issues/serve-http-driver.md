---
title: apogee serve — a fourth Driver over HTTP+SSE
status: open
type: feature
priority: 3
deps: [headless-json-output]
aliases: [apogee-afu]
created: 2026-09-07T10:48:54Z
---

The deferred half of gap 3 of the 2026-09-06 contender gap assessment. Owner call 2026-09-07 (handoff Q4): JSONL first, serve later — 'serve' gets its own ADR and plan once the headless JSONL event contract (apogee-tvw) has proven itself on a real consumer.

Rivals: opencode's TUI is a client of 'opencode serve' (OpenAPI 3.1 + SSE); qwen-code runs a daemon over HTTP+SSE. This is the payoff of ADR 0031's Driver model and what unlocks an IDE extension later.

Do not start before apogee-tvw has landed. The grill must cover auth, bind address, lifecycle and the confinement of a long-lived server.

## Acceptance

An ADR admitting 'serve' as a Driver (auth, bind address, lifecycle, confinement of a long-lived server) is ratified before any plan is written. ADR 0031's Driver list was already refreshed on 2026-09-07 by the apogee-tvw grill; that amendment does not satisfy this criterion.
