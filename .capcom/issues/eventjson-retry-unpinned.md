---
title: "eventjson: no test pins the marshal-failure retry"
status: done
type: task
priority: 3
aliases: [apogee-nww]
created: 2026-09-07T14:29:05Z
closed: 2026-09-07T17:22:49Z
reason: Closed by test-only additions; make check green
---

Deferred from plan 'docs/plans/2026-09-07 - 00 - headless-event-lines-plan.md'. When a data value fails to marshal (malformed model arguments), the Writer re-emits the envelope with `data:null` and the stream continues. That retry path in internal/eventjson/writer.go has no test — it was verified only by a throwaway probe.
