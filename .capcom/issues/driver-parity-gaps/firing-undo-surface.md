---
title: An Auto Firing has no undo surface
status: done
type: feature
priority: 3
labels: [driver-parity, residue]
plan: 2026-09-06 - 02 - undo-snapshots-and-tool-call-salvage-plan
aliases: [apogee-kk0.7]
created: 2026-09-03T18:41:27Z
closed: 2026-09-07T10:59:55Z
reason: "Revert half delivered by plan 'docs/plans/archived/2026-09-06 - 02 - undo-snapshots-and-tool-call-salvage-plan.md' (bead apogee-6gf, shipped v0.20.8): the journal is now snapshot-backed and persistent (internal/snapshot, journal.json), and 'apogee undo <session-id> [confirm]' reverts a headless or daemon Firing from any directory long after the run (cmd/apogee, commit 6dce9a99). Both unattended Drivers print the offer under their written-files block (undoVerbLine — headless.go:787, daemonfire.go:494), documented in docs/manual/headless.md:82-91 and docs/manual/daemon.md:82. The visibility half was already closed by the 2026-09-03 driver-parity plan."
---

- [ ] **An Auto Firing has nothing a human can revert.** `Agent.UndoRevert`
  (`internal/agent/agent.go:834`) is engine-level, but the only surface over it is the TUI's
  `runUndo` (`internal/tui/undo.go:45`). The VISIBILITY half of this bead is closed: plan
  `docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md` items 13-14 made a headless run
  and a daemon Firing report the files a Firing wrote (`Agent.WroteFiles`,
  `internal/agent/agent.go:855`). Those items deliberately kept the journal memory-only and added no
  `apogee undo` verb, so the revert half stays open — an auto **headless or daemon** run still
  leaves its Exchange groups with nothing a human can revert.
