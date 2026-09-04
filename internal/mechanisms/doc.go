// Package mechanisms is the curated Mechanism catalogue: a constraint-declared
// registry that the loop resolves into a deterministic total order (topo-sort
// with a stable canonical-ID tiebreak — ADR 0003). Each Mechanism declares its
// hook point, descriptor, and ordering constraints; the hook point is data, not
// package structure (the package-per-hook layout remains provisional — TDD §6.4).
//
// The SHIPPED catalogue is EMPTY. The rows ported from apogee-sim and A/B-validated in Phase 4
// were all resolved in v0.20.0 on ADR 0071's ratified verdict: six became Floor guards — plain
// engine behaviour in internal/floor, on for every model and governed by their own config keys —
// and the other fourteen retired outright. What survives here is the MACHINERY, kept as the
// bench's lab surface (AGENTS.md): the hook API, this registry, `EnableMechanisms`, the
// `mechanisms:` config key, the `/settings` row and `--bypass`. A Driver that wants to A/B a new
// idea registers a row and drives it through exactly the seams the shipped rows used.
//
// File naming here is compact, no underscores, following the package's own majority and
// internal/tui (ADR 0043); an underscore now means only a _test.go twin.
//
// # The files, one line each
//
// catalogue.go is the registry: the row, the package-private register() a Mechanism file would
// call from init(), the (now empty) injected Deps, and the Build / KnownIDs / Descriptors surface
// the engine resolves a stack through. It also carries the package's one exported TEST seam,
// SwapCatalogue(rows []Row) (restore func()) — it stands a temporary table in the curated
// catalogue's place so a test outside this package can drive the config -> EnableMechanisms ->
// engine-build path with a real row, and hands back the closure that restores the shipped
// (empty) one. Its only production use is none, and it is not concurrency-safe: a test that
// swaps must not call t.Parallel(). retired.go is the roll of IDs this build no longer
// catalogues, so a saved config or Validated set naming one is tolerated rather than refused — and
// the home of ResolveEnabled, the resolver a Driver runs a `mechanisms:` block through: it
// validates every key against the catalogue, drops the retired ones, and hands back the notices
// they earn — one for a removed row the block still turns on, and one for a PROMOTED row the block
// tries to switch off with a key that no longer does that.
//
// And doc.go this map.
package mechanisms
