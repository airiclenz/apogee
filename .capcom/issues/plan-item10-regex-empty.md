---
title: "Plan 2026-09-16 - 01 item 10: acceptance regex matches no test in internal/run"
status: done
type: task
priority: 3
aliases: [apogee-a4i]
created: 2026-09-16T13:50:20Z
closed: 2026-09-19T17:41:34Z
reason: Already fixed by 8ce2b778 (item 10's Acceptance regex gained Tap; plan line 370 NOTES D10). Stale.
---

Deferred from the implement-plan run of docs/plans/2026-09-16 - 01 - engine-tools-and-provider-deepening-plan.md (item 10, commit c0fded2b). The item's acceptance regex 'Usage|EventLines|Encode' matches no test in internal/run ("no tests to run"), so the package contributes no evidence to the acceptance line. TestEventTap* covers the changed eventTap.noteUsage path and ran green under -run Tap separately. Fix: add Tap to the acceptance regex in the plan, or name a run test so the existing regex catches it. Non-regression; recorded at verification, not litigated.
