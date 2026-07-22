// Package platform abstracts shell execution and path handling across POSIX and
// Windows, and hosts the Confiner backends (seatbelt / landlock / AppContainer)
// that gate Auto mode as a capability matrix (ADR 0004). The Confiner interface
// itself is public (package apogee); only the backends live here.
//
// The Shell/Path interfaces and a Host accessor carry real POSIX and Windows
// implementations: one rule table (host.go), compiled on every target and
// selected by build tag, so Windows shell/quoting/path semantics are
// table-testable from any host and exercised natively on Windows. The real
// Confiner backends landed in Phase 3 and are selected per OS at build time:
// landlock on Linux, seatbelt on macOS, and denyConfiner — a deny-all stub
// reporting {false, false} — everywhere else (Windows until Phase 5). An
// incapable backend does not refuse Auto: the dispatch disposition gates the
// subprocess surface through Approval instead (ADR 0012; §4–5 of
// docs/design/confinement-execution-contract.md).
//
// HostID lives here for the same reason the backends do — it is a per-machine fact.
// It is the interlock that keeps a host-scoped confinement acknowledgement
// (`unconfined-hosts:`, ADR 0012 amendment 2026-07-21) from silently travelling
// between machines; it is not an authentication mechanism.
package platform
