---
title: load_skill never rescans; skills added mid-session are invisible to the model
status: done
type: bug
priority: 2
plan: 2026-10-11 - 01 - p1-p2-bugs-plan
aliases: [apogee-load-skill-stale-catalogue]
created: 2026-10-11T18:16:33Z
closed: 2026-10-11T21:53:33Z
reason: "Resolved by plan '2026-10-11 - 01 - p1-p2-bugs-plan' (item 7: load_skill reloads the catalogue before answering a miss), archived at 3fb76cff"
---

`load_skill` never rescans the skill catalogue, so a skill added while apogee runs is invisible to the model.

**Seen:** session `20261011T131113Z-1ae692f2` (2026-10-11): the user created `~/.apogee/skills/handoff/SKILL.md` mid-session and told the model "the handoff skill is now available"; `load_skill` with query `handoff` still returned `no skill matches "handoff"`. The model then found and read the file by hand.

**Cause:** the catalogue is built at startup (`NewProvider`, `internal/skills/provider.go:43-45`) and reloaded only via `Provider.Reload` from the "/" menu (`internal/tui/autocomplete.go:212`), `/skills` (`internal/tui/skills.go:44`), a workflow save (`internal/tui/workflows.go:495`) and a skills setting change (`cmd/apogee/wire_settings.go:1480`). `load_skill` (`internal/tools/load_skill.go:95`) only reads the current snapshot. The human-facing paths rescan; the model-facing one does not.

**Acceptance:** `load_skill` reloads the catalogue on a miss (or per call, if cheap enough) before answering "no skill matches"; a skill written to disk after startup is found by the next `load_skill`. Test pins it.
