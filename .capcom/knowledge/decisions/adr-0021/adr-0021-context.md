---
title: ADR 0021 context
---

`apogee probe` was promised twice, from two directions, and neither promise has a command to
live in — the CLI has exactly one Cobra root and, until Phase 5 item 1, no subcommand seam at
all.

- **The host/confinement diagnosis.** `TODO.md`'s deferred residue from the
  auto-confinement-degradation work asks for "reporting the confinement backend and its
  capability matrix as a subcommand, diagnosable **without running an agent**". `/confine
  status` covers the need from inside the TUI, which is precisely the wrong place when the
  question is *why does Auto gate every command on this box* — the user wants an answer before
  they start a session, in a script, over SSH, in a CI log.
- **The model capability probe.** `../apogee-sim/mission.md` item 3: a probe command that sends
  test prompts (native tool call, JSON/structured output, multi-step tool chain) and
  **auto-generates a profile**, eliminating manual profile tuning. Item 2 of the same mission
  wants *adaptive prompt complexity* — a pipeline transform that strips tool descriptions,
  shortens system prompts, and simplifies formatting per `capabilityTier`.
- **The merge plan adds a third job to the second one:** the same battery yields the
  **behavioral model fingerprint** — a fuzzy feature match, **not** a response hash, logprobs
  preferred where the server exposes them — which is the identity used for Library keying (and
  for Validated-set keying) when the weights file is unreachable.

Those three jobs have wildly different costs and different blast radii, and one of them has a
consequence the other two do not:
ADR 0016 §5 auto-applies a
matching Validated set at **≥ medium** fingerprint confidence and merely *offers* it below that.
`domain.ConfidenceMedium` is documented as "a behavioral-probe identity (`apogee probe`, Phase
5)" (`internal/domain/fingerprint.go:20`) and **no resolver produces it**
(`internal/library/fingerprint.go:40`). The product wire-time resolution is a pure, synchronous,
offline call — `library.ResolveFingerprint(opts.model)` at `cmd/apogee/validatedsets.go:38`,
reached on every startup before anything is dialled. A tier that must be *earned by live model
calls* can therefore only reach that call site **through something written down**. That single
fact decides Q2 below.
