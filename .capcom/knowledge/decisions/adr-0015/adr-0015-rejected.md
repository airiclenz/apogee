---
title: ADR 0015 considered options
---

- **Whole-binary arms via `cmd/apogee` config** — *rejected*: TUI-only today, so it needs
  new apogee work anyway, and it trades the in-process instrument for subprocess
  scraping; per-arm construction across leave-one-out subsets is combinatorial config-file
  management.
- **Experimental-hook re-implementations** — *rejected*: benches copies under the
  synthetic ID, not the shipped Mechanisms; invalid evidence for a default-ON flip.
- **A root builder function (`BuildMechanisms(ids, opts)`)** — *rejected*: `Deps` need
  `LibraryDir` and the model id, so the consumer supplies both twice (builder opts and
  Config), and a mismatch silently arms `library` against the wrong fingerprint;
  wire.go keeps a parallel path instead of collapsing.
- **A public `mechanisms` subpackage (Build/Deps/KnownIDs exported)** — *rejected*: leaks
  `*library.Store` and the fingerprint type into v1, and reads as an open Mechanism SPI
  against ADR 0002's curated-catalogue posture.
- **IDs-only export (no descriptors)** — *rejected*: apogee-sim would hand-mirror the
  stacking relations from the catalogue doc — the exact duplication the descriptor
  exists to prevent — and a future `Requires` change would surface only as a runtime
  arm failure, not at planning time.
