---
title: "Starter template overrides two documented defaults: remember-model: true, ui.stall-after: 120s"
status: done
type: task
priority: 3
aliases: [apogee-lq4]
created: 2026-09-16T06:55:45Z
closed: 2026-09-16T21:41:26Z
reason: registry defaults follow the starter template (977771c4)
---

Owner call. internal/config/defaults/config.yaml ships remember-model: true (registry default false, and the template's own comment two lines up says 'Default: false') and an active ui.stall-after: 120s (code default 90s, internal/config/config.go:331). The manual (2026-09-16 review) now says the starter file switches these; decide whether the template or the defaults should move so the two agree.
