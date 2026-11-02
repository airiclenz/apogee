---
title: Engine's post-cancel landing consult can open a fresh grace window and walk
status: open
type: bug
priority: 3
created: 2026-11-02T22:07:31Z
---

When every routed caller is cancelled during the grace window, `Agent.awaitLanding` (internal/agent/rebind.go:581) re-consults the seat's resolver on `context.WithoutCancel` (internal/agent/rebind.go:585) to join the run still in flight. If that run ends without a switch between the caller's cancel and this second consult, `fallbackResolver` has already dropped the run, so the consult starts a new grace window and walk instead of joining. Its answer is used only to re-latch the Delegation target. (plan `2026-11-02 - 04 - seat-and-resolved-entry-plan.md`, item 7)

## Why

The engine's observation of a landing should never start a walk of its own; an extra grace wait and fallback walk runs detached with no caller.

## Acceptance

- The post-cancel consult joins an in-flight run or answers without starting a new grace window and walk; a test drives the run ending between the cancel and the consult.
