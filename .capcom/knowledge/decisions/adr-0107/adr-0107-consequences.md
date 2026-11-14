---
title: ADR 0107 consequences
---

- go.mod's `retract [v1.0.0, v1.8.0]` becomes a `retract ( … )` block that names the four burned
  versions; the release tool refuses a go.mod that retracts anything else. ADR 0001's retract
  note of 2026-09-30 is superseded by this record (its amendment of 2026-11-05).
- `internal/update` owns the list, and its update check refuses exactly the burned versions, not
  the range. A release that carries this check is the bridge, and it must ship before v1.0.0.
- `cmd/release publish` gains the `placeholder` step and is no longer tag-free: it creates
  exactly one tag, the placeholder, while CI still creates every release tag (ADR 0098,
  amendment of 2026-11-05). Its asset build drops the burned tags from its clone, so a binary's
  stamped version is the release, never the placeholder sitting on the same commit.
- `smoke` checks the Go module proxy: an exact-version lookup that reports the release retracted
  fails the smoke; an `@latest` that lags only warns, since the proxy caches.
- An offline rehearsal test proves the scheme in CI against a `file://` proxy; the real proxy is
  never polled from a test.
- The burned set can only grow. Each wrong placeholder burns one more v1.8.x, and a release
  number, once published anywhere, is never reused.
- The v1 release criteria and the stability promise are not decided here: they are held by the
  issue `v1-release-criteria`.
