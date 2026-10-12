---
title: No test drives an empty int value through applySettingFor to the row Default
status: done
type: task
priority: 3
aliases: [apogee-1ah]
created: 2026-09-16T11:15:12Z
closed: 2026-09-16T21:07:01Z
reason: "test(cmd/apogee): an empty int value lands the registry row's Default through applySettingFor"
---

Deferred out of plan 2026-09-16 - 00 (item 14). landSetting (cmd/apogee/wire_settings.go:2175-2189) resolves an empty value to row.Default, so live applies of context-window (wire_settings.go:1328), working-window (:1344) and delegate-max-* (:1292 and neighbours) now accept "" as the row's Default where settingInt once refused it — consistent with the dispatcher's empty-value rule, but no test drives the empty-int case through applySettingFor (wire_settings.go:935) end to end. Add one.
