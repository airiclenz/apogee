---
title: No offline refusal before send outside the TUI
status: done
type: feature
priority: 2
labels: [driver-parity, residue]
plan: 2026-09-03 - 01 - driver-parity-and-residues-plan
aliases: [apogee-kk0.5]
created: 2026-09-03T18:41:26Z
closed: 2026-09-04T10:52:57Z
reason: delivered by plan docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md, items 7-9 (one beat per Firing; headless and the daemon refuse an unreachable server before submit)
---

- [ ] **No offline refusal before send outside the TUI.** The heartbeat that flips a session
  offline and refuses the send lives at `internal/tui/heartbeat.go:620` (`foldBeatFailure`). A
  **headless or daemon** Firing has no liveness probe, so an unreachable server surfaces as the
  run's error after the attempt rather than as a refusal before it.
