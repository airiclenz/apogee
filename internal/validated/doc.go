// Package validated implements the Validated-set runtime surface (ADR 0016 and its
// 2026-07-19 runtime-surface realisation): loading per-model Validated-set entries from
// the two sources (the embedded shipped bundle and the user-local drop-in dir), matching
// them against the resolved model fingerprint under the confidence-graded rule —
// auto-apply at ≥ medium confidence, offer at low, alias applies at any — and validating
// that an entry's enable set is whole and buildable against the current Mechanism
// catalogue (whole-set-or-nothing: a subset is an unvalidated stack and must not apply).
//
// The embedded SHIPPED bundle is empty since v0.20.0: its one entry named fifteen catalogued
// Mechanisms and every one of them retired in that wave (ADR 0071), so it had nothing left to
// enable. The bundle, the entry roll that keeps its key from refusing a start, and the whole
// surface stay — a user's own measured entry under ~/.apogee/validated applies exactly as
// before, and a bench build that registers experimental rows can ship its own roster.
//
// The package is deliberately mechanism-catalogue-agnostic: it imports internal/domain
// only, and the caller passes the catalogue descriptors in. The decision to apply lives
// at product wire time (cmd/apogee) — the engine and the bench never see this package,
// so a bench arm's EnableMechanisms can never be contaminated by a matching set.
package validated
