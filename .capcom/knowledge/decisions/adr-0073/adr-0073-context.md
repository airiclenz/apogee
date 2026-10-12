---
title: ADR 0073 context
---

A 2026-09-06 scout asked whether apogee should grow event hooks and/or plugins. It found that the
engine already has the observation door: `domain.EventSink` (`internal/domain/events.go`) is a
typed, sealed, one-way stream every Driver reads, and an observer can attach to it with no engine
edit. It also found the real gaps: nothing today runs a user command or posts a webhook when a
run finishes, a Turn ends, a file changes, an Approval waits or an error lands. The grill the same
day settled the shape, and it settled three refusals just as firmly, because each is the door
through which this surface would otherwise drift into a second Mechanism catalogue.

Two moments users care about most were not observable: a Turn's end is a `StepResult` return
value, and an Approval is announced only after its decision. Both become additive
observation-only event variants, so the signal is the engine's and every Driver sees it — nothing
model-visible or safety-relevant may exist in one Driver's surface alone
(ADR 0031).
