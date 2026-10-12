---
title: "Per-servers:-entry bypass: and mechanisms: are omitted from the entry inventory"
status: done
type: task
priority: 3
labels: [docs, residue]
plan: 2026-09-03 - 02 - deferred-items-sweep-plan
aliases: [apogee-t57.4]
created: 2026-09-03T18:41:50Z
closed: 2026-09-04T21:28:22Z
reason: Delivered by docs/plans/2026-09-03 - 02 - deferred-items-sweep-plan.md items 9-17 (manual gaps, plus the registry-drift gate)
---

- [ ] **Per-`servers:`-entry `bypass:` and `mechanisms:` are omitted from the entry inventory.**
  Both parse (`internal/config/config.go:1608-1609`) and the starter template names them, but the
  entry-key list at `docs/manual/configuration.md:684-692` does not.
