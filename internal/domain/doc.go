// Package domain is the ubiquitous language (CONTEXT.md) rendered as Go: every
// type, interface, enum, sentinel error, and hook working-value in Apogee's public
// surface, plus the pure logic intrinsic to those types (the Mechanism registry's
// ordering-cycle detection, ConfinementCaps.AutoEligible, the Session envelope and
// its versioning).
//
// It is the foundational layer of the package layout decided in ADR 0010: the engine
// (internal/agent), the provider (internal/provider), and the platform backends
// (internal/platform) all import domain for these types and never import the root
// apogee package; the root facade re-exports the public ones as aliases. Domain
// depends only on the standard library, so the language has a dependency-free home
// and the invariant "internal/* never imports root" holds at the bottom of the graph.
//
// Naming: "domain", not "core" — the retired "Apogee Core" library term
// (CONTEXT.md "Retired terms") is unrelated to this internal package.
package domain
