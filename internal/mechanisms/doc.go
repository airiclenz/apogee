// Package mechanisms is the retired roll and nothing else: the list of lab-row IDs apogee once
// catalogued and no longer does, so a saved configuration naming one is tolerated rather than
// refused.
//
// The catalogue itself is GONE. The rows ported from apogee-sim and A/B-validated in Phase 4 were
// all resolved in v0.20.0 on ADR 0071's ratified verdict — six became Floor guards, plain engine
// behaviour in internal/floor governed by their own config keys, and the other fourteen retired
// outright — and the machinery that carried them (the hook API, the registry, `EnableMechanisms`,
// the build path) was deleted with the Reaction core (ADR 0076 D1). What a Driver arms today is a
// [github.com/airiclenz/apogee/internal/domain.Reaction].
//
// The `mechanisms:` configuration key still parses and still drives NOTHING (ADR 0076 D11). This
// package is the whole of what it means now: RetiredNotices validates a block against the roll and
// hands back the lines a removed ID earns. Stage 2's `reactions:` resolver is where a migration
// table for those IDs will live.
//
// File naming here is compact, no underscores, following the package's own majority and
// internal/tui (ADR 0043); an underscore now means only a _test.go twin.
//
// # The files, one line each
//
// retired.go is the roll — the rows with their release and, for a PROMOTED row, the top-level key
// that governs its behaviour now — plus the RetiredIDs / IsRetired / RetiredRelease / Successor
// lookups over it and RetiredNotices, the door every Driver reaches this package through.
//
// And doc.go this map.
package mechanisms
