// Package platform abstracts shell execution and path handling across POSIX and
// Windows, and hosts the Confiner backends (seatbelt / landlock / AppContainer)
// that gate Auto mode as a capability matrix (ADR 0004). The Confiner interface
// itself is public (package apogee); only the backends live here.
//
// Phase-0 scaffold: no implementation yet (POSIX first; Windows in Phase 5).
package platform
