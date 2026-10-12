---
title: apogee.go public Event alias list lacks RefClippedEvent
status: done
type: bug
priority: 3
labels: [plan-2026-09-14-02, run-residual]
aliases: [apogee-5wb]
created: 2026-09-15T11:49:41Z
closed: 2026-09-16T17:42:27Z
reason: "fix(apogee): alias RefClippedEvent on the public Event list and pin the list complete"
---

Deferred from plan 2026-09-14 - 02 item 7. The `domain.RefClippedEvent` variant added by item 7 (internal/agent/loop.go:1114) has no alias in apogee.go's public Event variant list (apogee.go:242-260, 'additively extensible'), so an embedder (ADR 0031 Driver) cannot type-switch on the new variant or reach Notice(). Add `RefClippedEvent = domain.RefClippedEvent` to the type block. Headless --format text narrating a prune line but no clip note is consistent with ErrorEvent, not a defect.
