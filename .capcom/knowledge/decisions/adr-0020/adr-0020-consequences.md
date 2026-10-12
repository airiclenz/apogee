---
title: ADR 0020 consequences
---

- **Windows joins Linux and macOS as Auto-eligible.** The degradation notice vanishes on a
  capable host and persists below the floor (item 8's acceptance).
- **Apogee mutates the user's disk on Windows and on no other OS.** Reverted on teardown,
  journalled against a crash, reported by `probe host`, and documented in README/CHANGELOG at
  item 10. This is the headline consequence and it must not be soft-pedalled anywhere it is
  described.
- **`domain.Confiner` is unchanged.** Teardown rides an optional `io.Closer`, asserted at the
  composition root.
- **Contract §2.2's "performs no I/O and blocks on nothing" is amended** (contract §9): the
  Windows backend performs bounded, idempotent, once-per-box label I/O inside `Confine`. It still
  never runs the command and never blocks on it.
- **Contract §4's precomputed `Confine`-fallback becomes a routine path**, not a theoretical one.
  Every surface that describes it as a rare safety net is now describing the common Windows case
  of a box root on a share or a read-only path.
- **The below-floor path is UNTESTED.** The execution machine is build 26200 and no below-floor
  Windows host exists here; the plan's owner-run checklist keeps the item, and this ADR is where
  the gap is recorded rather than assumed away.
- **`NetworkEgress` stays false on Windows** until an AppContainer or WFP backend exists, so
  `--mode auto` with a non-empty `NetworkAllow` gates on Windows. That is the honest outcome, and
  it is identical to Linux below kernel 6.7.
- **Item 6 owes `Path.Contains`** with case folding as a parameterised pure function; item 7's
  job objects are untouched by this ADR and remain teardown, never a fence.
- **ADR 0021's `probe host` gains its first Windows-specific line** — the outstanding-label
  journal — which is the surface that makes an interrupted cleanup diagnosable off-session.
