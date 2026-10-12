---
title: The /settings enum sub-list's question stays flush on its rows
status: done
type: bug
priority: 2
aliases: [apogee-7ur]
created: 2026-09-06T15:22:45Z
closed: 2026-09-06T15:56:14Z
reason: "Fixed in 5359e90e: renderListPlaced withholds the upper breathing row only where the body pads itself below."
---

Plan 'docs/plans/2026-09-06 - 00 - popup-breathing-rows-and-click-to-select-plan.md' item 4 gave the list pop-ups their breathing rows, but the /settings ENUM sub-list still shows its question flush against the first row. Its body is built with bodyPad false, and the literal 'c.body == ""' gate in internal/tui/listsurface.go withholds the upper blank whenever a body is present, so nothing closes the gap for a body that books no pad of its own. No design frame under cmd/apogee/testdata/frames/ covers that pane, so no golden catches it. Deferred out of the 2026-09-06 - 00 run.
