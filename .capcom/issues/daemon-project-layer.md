---
title: Daemon firings layer their own workspace's Project config
status: done
type: feature
priority: 4
plan: 2026-10-17 - 00 - quit-keys-stage-kinds-daemon-layer-plan.md
aliases: [apogee-daemon-project-layer]
created: 2026-10-10T09:03:44Z
closed: 2026-10-17T12:41:46Z
reason: "Resolved by plan '2026-10-17 - 00 - quit-keys-stage-kinds-daemon-layer-plan' items 4-6 (archived at c46ba088): each daemon firing overlays its workspace's Project layer read fresh, notices latched per root; ADR 0096 amended and manual updated."
---

ADR 0096 §4: apogee daemon resolves options once from its start folder and every firing reuses them, so it reads no Project config (one repo's layer would leak into another's firings). Resolve each firing's Project root and layer its config per firing.
