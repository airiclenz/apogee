---
title: settingsrows_test.go's external-edit path list omits the hooks row
status: done
type: task
priority: 3
aliases: [apogee-apz.6]
created: 2026-09-06T19:16:53Z
closed: 2026-09-06T20:08:59Z
reason: delivered by docs/plans/archived/2026-09-06 - 03 - hooks-deferred-findings-plan.md
---

cmd/apogee/settingsrows_test.go:552 pins an explicit list of the registry keys whose row opens $EDITOR; `hooks` is not in it, so only the general `ExternalEdit == (pointer == …)` invariant guards the new row, not its membership in that set. Add `hooks` to the list. (The same list also omits `sub-agents-server` and `tools.enabled`, which pre-date this run.) Cite: cmd/apogee/settingsrows_test.go:552.
