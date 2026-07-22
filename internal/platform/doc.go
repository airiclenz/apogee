// Package platform abstracts shell execution and path handling across POSIX and
// Windows, and hosts the Confiner backends that gate Auto mode as a capability
// matrix (ADR 0012). The Confiner interface itself is public (package apogee);
// only the backends live here.
//
// The Shell/Path interfaces and a Host accessor carry real POSIX and Windows
// implementations: one rule table (host.go), compiled on every target and
// selected by build tag, so Windows shell/quoting/path semantics are
// table-testable from any host and exercised natively on Windows. The real
// Confiner backends are selected per OS at build time: landlock on Linux
// (Phase 3), seatbelt on macOS (Phase 3), a restricted low-integrity token on
// Windows (Phase 5, ADR 0020), and denyConfiner — a deny-all stub reporting
// {false, false} — everywhere else, including a Windows host below the version
// floor. An incapable backend does not refuse Auto: the dispatch disposition
// gates the subprocess surface through Approval instead (ADR 0012; §4–5 and §9
// of docs/design/confinement-execution-contract.md).
//
// The Windows backend is the one that mutates the machine: because a token
// cannot carry path policy, a box's writable roots are expressed as mandatory
// labels ON THE DISK for the duration of the run and reverted on teardown
// (an io.Closer the composition root defers), journalled under the apogee home
// so an interrupted run is recoverable and reportable. That asymmetry is
// recorded in ADR 0020 rather than discovered by a user.
//
// HostID lives here for the same reason the backends do — it is a per-machine fact.
// It is the interlock that keeps a host-scoped confinement acknowledgement
// (`unconfined-hosts:`, ADR 0012 amendment 2026-07-21) from silently travelling
// between machines; it is not an authentication mechanism.
package platform
