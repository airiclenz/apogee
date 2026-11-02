---
title: Background workflow host holds no fallback resolver
status: open
type: bug
priority: 3
created: 2026-11-02T14:12:35Z
---

A background workflow's host holds no fallback resolver (backgroundHost, internal/agent/background.go:666-681 copies `appliedGen` but no resolver), so a session-seat fault inside a background workflow child is abandoned as before. The child still follows a move another agent made. Deferred from plan 2026-11-02 - 00 - midrun-model-fallback-plan (item 7).

## Why

A session-seat server failure during a background workflow kills that workflow item even when the entry names a fallback.

## Acceptance

- A session-seat fault in a background workflow child consults the seat's fallback resolver, pinned by a test
