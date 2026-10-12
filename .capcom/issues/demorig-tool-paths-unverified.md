---
title: "demorig: ffmpeg/ffprobe/gifsicle paths unverified against the live tools (plan 2026-09-16 - 02)"
status: done
type: task
priority: 3
plan: 2026-09-19 - 01 - ten-urgent-beads-plan
aliases: [apogee-bxi]
created: 2026-09-16T17:08:32Z
closed: 2026-09-20T06:57:55Z
reason: implemented by docs/plans/archived/2026-09-19 - 01 - ten-urgent-beads-plan.md (merged to main, closeout 5a415e13)
---

Deferred out of the 2026-09-16 - 02 demo-storyboard-rig run: no ffmpeg/ffprobe/gifsicle on the implementing host, so the adapters and the encode ran only under fakes.
- The ffmpeg/ffprobe adapter (ffmpegFirstPainter, takeDuration in cmd/demorig/align.go) is unexercised — TestFfmpegAdapters (cmd/demorig/align_test.go:81) skips without the tools; run it once on a host with ffmpeg before relying on `demorig beats` against a real take.
- The built graph (scale…eval=frame with t, per-frame crop x/y in cmd/demorig/filtergraph.go) and writeRenderSummary/optimizeGIF (cmd/demorig/render.go:222) are proven only by the golden cmd/demorig/testdata/filtergraph-hero.txt and the plan's 7.0.2 probe; run one real `demorig render` on a host with ffmpeg before the hero re-record.
