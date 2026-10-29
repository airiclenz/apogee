---
title: "ADR 0104 amendment of 2026-10-18: an adopted repo run: always takes the hook door"
---

Shipped in the exec-keys run (docs/plans/archived/2026-10-18 - 00 - project-config-exec-keys-plan.md). It corrects the last sentence of decision 8. An adopted repo `run:` spawns through the execution tools' hook door (`(*Agent).spawnReactionArgv`) whether or not `confine-to-workspace` is on. With the fence off it runs unfenced, as a global `run:` does, but it still gets the door's credential scrub, its 256 KiB output and 3600 s timeout ceilings, the funnel's failure wording (`apogee: <program> exited 3: …`), and the Project root as its working directory. A global `run:` goes through `userexec.Run` and inherits apogee's environment whole. docs/manual/reactions.md states this posture.
