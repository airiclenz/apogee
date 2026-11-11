---
title: Nothing ties the loading notice's "10m" to the provider's loadingBackstop
status: open
type: task
priority: 3
created: 2026-11-11T05:56:07Z
---

`domain.UpstreamLoadingEvent.Notice()` says `waiting up to 10m` from the string constant `upstreamLoadingBound` (`internal/domain/events.go:905-907`), which restates the provider's unexported `loadingBackstop = 10 * time.Minute` (`internal/provider/client.go:85`). No test ties the two, so a change to the backstop leaves every Driver's notice naming the wrong wait, silently. Found by the audit-defects run, item 8 (docs/plans/archived/2026-11-10 - 02 - audit-defects-plan.md).

## Why

The notice is what the user reads to decide whether to wait; a wrong bound misleads them.

## Acceptance

- A test (or a shared exported value) fails when loadingBackstop and the notice's bound disagree
