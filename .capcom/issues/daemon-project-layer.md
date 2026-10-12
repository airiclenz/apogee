---
title: Daemon firings layer their own workspace's Project config
status: open
type: feature
priority: 4
aliases: [apogee-daemon-project-layer]
created: 2026-10-10T09:03:44Z
---

ADR 0096 §4: apogee daemon resolves options once from its start folder and every firing reuses them, so it reads no Project config (one repo's layer would leak into another's firings). Resolve each firing's Project root and layer its config per firing.
