---
title: make check OOM-kills internal/tui under -race on the Pi; gate slow
status: done
type: bug
priority: 1
aliases: [apogee-test-gate-oom]
created: 2026-10-11T13:32:19Z
closed: 2026-10-11T15:01:11Z
reason: "Fixed: frame-fit tables park on a small stack (efda9e93), suite runs as short-lived chunks over lanes (be4de5c5), timing harvest counts subtests (405e5654), plus speedups b63627eb 6d4f8434 1650150e 8cad4ba0 c29bab25. make check green on the Pi: rc=0, 1067 s, peak 733 MB."
---

tui.test reaches ~4 GB anon RSS under -race (swap full) and is OOM-killed at ~1937/1960 tests; cmd/apogee e2e shard serialised ~1057 s. Findings and fix list: docs/handoffs/2026-10-11 - 01 - make-check-slowness-and-oom.md (local). Fixes A (heartbeat seam), B (bounded-lifetime test lanes), D (timing harvest), E (leak-grace self-test), G, H.
