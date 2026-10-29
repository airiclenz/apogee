---
title: ADR 0071 consequences
---

- A stock install runs six Floor guards and zero Mechanisms. *(Note 2026-10-29: seven since
  `tool-call-salvage` joined, `adr-0071-amend-0907`; the Mechanism layer itself is gone, ADR 0076,
  and with it the `/settings` mechanisms row.)* Where the old default armed two
  off-ramps and nothing else, the shipped posture now includes tool-call repair, the tool-loop
  breaker, the tool-result cap and the read cache — behaviour that previously required a
  hand-written `mechanisms:` block or a Validated set.
- **The catalogue and the shipped Validated roster end empty.** Every invariant, test and doc
  sentence that counted rows ("21 mechanisms") is rewritten to assert emptiness; `/settings` shows
  an empty mechanisms list on a stock install.
- Fourteen rows' source files, tests and assets are deleted. `internal/mechanisms/retired.go`
  carries each retired ID with the release that retired it (`v0.20.0`) and, for a promoted ID, the
  config key that succeeds it — so an old config naming any of the twenty gets a specific message
  rather than an unknown-ID failure. *(Note 2026-10-29: `internal/mechanisms` was deleted on
  2026-09-08 (`e03dee88`); the successor table now lives as `retiredMechanismSuccessors` in
  `internal/config/configmigrate.go`, and the startup config migration strips the `mechanisms:` key
  with a notice naming each promoted id's Floor key, rather than refusing it.)*
- `~/.apogee/library` on disk is never touched, and `internal/library`'s fingerprint and
  probe-record halves stay: they serve Validated sets and `probe model`, not the retired `library`
  Mechanism. *(Note 2026-09-16: Validated sets went with ADR 0076 A9 and `internal/library` with its
  last reader — the `ModelFingerprint` type lives in `internal/domain`, the probe record in
  `internal/probe`, serving `probe model`'s drift check alone; ADR 0021 §3 carries the amendment.)*
- Guard firings surface as `domain.FloorGuardEvent`s keyed by config key, reaching every Driver;
  `MechanismFiredEvent` stays for lab hooks. A Driver that only knew about Mechanism firings would
  otherwise have gone quiet the moment the catalogue emptied. *(Amended 2026-09-30: both events
  are gone; every firing, a Floor guard's included, is now one `domain.ReactionFiredEvent` keyed by
  the same config key — ADR 0076 decision 13, `internal/domain/events.go`.)*
- The catalogue document is archived with a per-row verdict — promoted, retired, or deferred —
  so the record of what was measured, and what was decided without measurement, survives the
  deletions.
- A future intervention still has a route in: arm it as an experimental hook, measure it under ADR
  0009, and bring a record. What it may **not** do is join a frozen shipped catalogue on the
  strength of a port.
