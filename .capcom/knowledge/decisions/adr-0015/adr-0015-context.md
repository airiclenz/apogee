---
title: ADR 0015 context
---

The bench (apogee-sim, the ADR 0001
consumer) must arm catalogued Mechanisms **in-process** to run the A/B campaign: the
aggregate mechanisms-on vs Bypass gate and the per-Mechanism leave-one-out arms of
ADR 0009, including the leave-the-STACK-out arm for
`guided_decomposition + tool_result_cap` (ADR 0014 §4).

Today the enable path is cmd-only. `Config.Mechanisms *MechanismRegistry` is public, but
the catalogued constructor table (`internal/mechanisms.Build`) and its injected `Deps`
are internal; only `cmd/apogee/wire.go` walks the YAML `mechanisms:` map into a built
registry. The executable embedding contract, `benchreadiness_test.go`, arms Mechanisms
only by being in-repo — its header apologises for importing `internal/mechanisms` — so a
separate module cannot do today what the contract demonstrates.

The grill (2026-07-05) rejected the two ways around a new surface before settling its
shape:

- **Whole-binary arms via the `cmd/apogee` config** — the binary is TUI-only (headless
  subcommands are deferred), so this path *also* requires new apogee work, and it
  abandons the in-process instrument the bench already has: event capture as Go values,
  snapshot/resume forking, hermetic workspace scoring.
- **Re-implementing Mechanisms as experimental hooks** — experimental hooks fire under
  the synthetic `experimental` ID and are, by the glossary's own definition, behaviours
  that are *not (yet)* Mechanisms. Benching a re-implementation is evidence about a copy,
  not about the shipped Mechanism; a default-ON flip cannot rest on it.
