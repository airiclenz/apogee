---
title: ADR 0051 consequences
---

- The human has a real way back from a bad Exchange that does not touch what they wrote
  themselves, and a preview that names every recorded path in full before anything moves.
- The coverage boundary is a property of the funnel, so it holds automatically for tools added
  later — and the uncovered classes (subprocess, git checkout, MCP, embedder-registered) are a
  documented contract rather than an omission. A future MCP or subprocess undo would need its own
  capture mechanism and its own record; it is not blocked by anything decided here.
- The engine gains one long-lived in-memory structure holding pre-image bytes for the current
  process. Its size is bounded only by what the agent wrote this run — the accepted cost of call
  8, and the first thing to revisit if a long session's footprint is ever a sighting.
- `internal/undo` stays engine-, tool- and TUI-ignorant, so the bench and any future Driver can
  drive undo through the same seam the TUI does. The surface is deliberately internal: no export
  on `apogee.go` until an embedder asks for one.
- The generation guard makes the two-step protocol safe at the quiescent boundary only; the
  Driver's idle-only command gate is the enforcement, and both engine methods document it.
