---
title: reloadServers' row in TestSettingsApplierReloadsRefuseAnUnparseableFile calls t.Error on the parent T
status: done
type: bug
priority: 3
aliases: [apogee-itk]
created: 2026-09-09T06:59:14Z
closed: 2026-09-11T10:28:51Z
reason: fixed in plan 2026-09-11 - 00 items 2 and 3
---

cmd/apogee/wire_settings_test.go:3433 — the reloadServers table row's run closure calls t.Error on the parent *testing.T even though the closure is invoked from inside the t.Run subtest at cmd/apogee/wire_settings_test.go:3439, so a moved-token failure would be attributed to the parent test rather than to the reloadServers subtest. Deferred from plan 'docs/plans/2026-09-09 - 00 - deferred-findings-plan.md' item 8.
