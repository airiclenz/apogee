---
title: ADR 0051 consequences
---

- The human has a real way back from a bad Exchange that does not touch what they wrote
  themselves, and a preview that names every recorded path in full before anything moves.
- *(Superseded with decision 3 by ADR 0074, 2026-09-06: the boundary is now the workspace tree's
  snapshot pair, which covers the subprocess, git-checkout and MCP writes named here; the funnel
  stays a capture path. Kept as written:)* The coverage boundary is a property of the funnel, so it
  holds automatically for tools added
  later — and the uncovered classes (subprocess, git checkout, MCP, embedder-registered) are a
  documented contract rather than an omission. A future MCP or subprocess undo would need its own
  capture mechanism and its own record; it is not blocked by anything decided here.
- *(Superseded with decision 8 by ADR 0074, 2026-09-06: a Driver that seeds a snapshot store gets a
  per-session persisted journal; the in-memory form below remains only for an engine handed no
  journal. Kept as written:)* The engine gains one long-lived in-memory structure holding pre-image
  bytes for the current
  process. Its size is bounded only by what the agent wrote this run — the accepted cost of call
  8, and the first thing to revisit if a long session's footprint is ever a sighting.
- `internal/undo` stays engine-, tool- and TUI-ignorant, so the bench and any future Driver can
  drive undo through the same seam the TUI does. The surface is deliberately internal: no export
  on `apogee.go` until an embedder asks for one. (Held until 2026-10-27: `apogee.go` now exports the
  construction-seed option `WithUndoJournal(*undo.Journal, string)`; the journal type stays in
  `internal/undo`, so only an in-module Driver can construct one.)
- The generation guard makes the two-step protocol safe at the quiescent boundary only; the
  Driver's idle-only command gate is the enforcement, and both engine methods document it.
