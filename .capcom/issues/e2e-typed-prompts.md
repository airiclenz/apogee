---
title: e2e tests type their prompts key by key where the claim is not about typing
status: open
type: task
priority: 4
plan: 2026-10-12 - 02 - test-suite-stability-plan
created: 2026-10-12T12:28:48Z
---

Driven e2e tests enter their prompts one key at a time even when the test's claim has nothing to do with typing, which costs wall time in `make test`.

**Policy call first:** pasting prompts (bracketed paste) in those tests weakens coverage of the typed-key path. Do it only if profiling shows typing dominates the driven tests' time, and keep typed input in every test whose claim is about keys.

**Acceptance:** a profile justifies the change; tests whose claim is typing still type; suite wall time drops.

Source: option F of the archived handoff `docs/handoffs/archived/2026-10-11 - 01 - make-check-slowness-and-oom.md` (local only).
