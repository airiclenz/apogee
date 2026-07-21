// Package platform abstracts shell execution and path handling across POSIX and
// Windows, and hosts the Confiner backends (seatbelt / landlock / AppContainer)
// that gate Auto mode as a capability matrix (ADR 0004). The Confiner interface
// itself is public (package apogee); only the backends live here.
//
// The Shell/Path interfaces and a Host accessor exist with a real POSIX
// implementation and a Windows stub that ships unexercised (Phase 5). The real
// Confiner backends landed in Phase 3 and are selected per OS at build time:
// landlock on Linux, seatbelt on macOS, and denyConfiner — a deny-all stub
// reporting {false, false} — everywhere else (Windows until Phase 5). An
// incapable backend does not refuse Auto: the dispatch disposition gates the
// subprocess surface through Approval instead (ADR 0012; §4–5 of
// docs/design/confinement-execution-contract.md).
package platform
