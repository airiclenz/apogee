---
title: "model-profiles: tool-call-format: and tool-call-pattern: are undocumented"
status: done
type: task
priority: 3
labels: [docs, residue]
plan: 2026-09-03 - 02 - deferred-items-sweep-plan
aliases: [apogee-t57.5]
created: 2026-09-03T18:41:51Z
closed: 2026-09-04T21:28:22Z
reason: Delivered by docs/plans/2026-09-03 - 02 - deferred-items-sweep-plan.md items 9-17 (manual gaps, plus the registry-drift gate)
---

- [ ] **`model-profiles:` `tool-call-format:` and `tool-call-pattern:` are undocumented**
  (`internal/config/config.go:2297-2298`), and the `thinking:` sub-keys `style:` / `start:` /
  `end:` are alluded to at `docs/manual/configuration.md:563` without being specified — so a
  profile for a non-native-tool-call model cannot be written from the manual alone.
