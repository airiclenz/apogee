---
title: headlessHooksAgainst reads the package-level runOnce without binding it
status: done
type: task
priority: 3
aliases: [apogee-apz.5]
created: 2026-09-06T19:16:52Z
closed: 2026-09-06T20:08:58Z
reason: delivered by docs/plans/archived/2026-09-06 - 03 - hooks-deferred-findings-plan.md
---

cmd/apogee/e2e_hooks_test.go: every other user of the package-level `runOnce` sets and restores it; `headlessHooksAgainst` only reads it, so its claim to drive the real `run.Once` rests on test ordering rather than on its own binding. Safe today, latent under `go test -shuffle`. Set-and-restore it like its siblings. Cite: cmd/apogee/e2e_hooks_test.go.
