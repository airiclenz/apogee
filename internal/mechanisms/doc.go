// Package mechanisms is the curated Mechanism catalogue: a constraint-declared
// registry that the loop resolves into a deterministic total order (topo-sort
// with a stable canonical-ID tiebreak — ADR 0003). Each Mechanism declares its
// hook point, descriptor, and ordering constraints; the hook point is data, not
// package structure (the package-per-hook layout remains provisional — TDD §6.4).
//
// Phase-0 scaffold: no implementation yet; the catalogue is ported from
// apogee-sim and A/B-validated, one Mechanism at a time, in Phase 4.
package mechanisms
