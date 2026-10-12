---
title: hookMarkers is a whitelist, so a new hook-text leak passes the 'nothing reaches the screen' assertion
status: done
type: task
priority: 3
aliases: [apogee-apz.4]
created: 2026-09-06T19:16:52Z
closed: 2026-09-06T20:08:58Z
reason: delivered by docs/plans/archived/2026-09-06 - 03 - hooks-deferred-findings-plan.md
---

cmd/apogee/e2e_hooks_test.go: the absence check enumerates six spellings (`hook sink`, `hook bell`, `APOGEE_HOOK` and the three event names). Hook text under a different wording — a queue-drop line, say — would reach the final frame unnoticed. Widen the marker set, or assert on a narrower positive-control string. Cite: cmd/apogee/e2e_hooks_test.go.
