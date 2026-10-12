---
title: "Image input: image_url parts on the wire and paste-to-image in the TUI"
status: done
type: feature
priority: 3
aliases: [apogee-bmj]
created: 2026-09-07T10:48:55Z
closed: 2026-10-03T20:16:59Z
reason: "Resolved by plan 2026-10-03 - 00 - table-stakes-and-image-input (archived 1d6dacc3): image parts on both wire dialects, vision: key, image @refs, ctrl+v clipboard image paste and pasted-path attach in the TUI (items 10-13)"
---

Gap 8 of the 2026-09-06 contender gap assessment. DEFERRED by the owner 2026-09-07 — not selected for wave 2 (no design work, no plan yet).

Today there is no image input: wirejson.go has no image_url part. pi, Claude Code and aider all take images, and Gemma 4 / Qwen3.6-VL run locally, so the capability is reachable for apogee's target models.

Wire-only change plus a TUI paste path. Effort M.
