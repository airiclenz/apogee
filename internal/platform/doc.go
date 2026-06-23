// Package platform abstracts shell execution and path handling across POSIX and
// Windows, and hosts the Confiner backends (seatbelt / landlock / AppContainer)
// that gate Auto mode as a capability matrix (ADR 0004). The Confiner interface
// itself is public (package apogee); only the backends live here.
//
// Phase-0 seam (P0.5): the Shell/Path interfaces and a Host accessor exist with
// a real POSIX implementation and a Windows stub; the only Confiner backend is
// denyConfiner, a deny-all stub (AutoEligible == false) so New's Auto gate can
// be tested before the real backends land in Phase 3.
package platform
