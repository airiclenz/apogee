// Package present holds the HOST-SIDE mechanisms of the presentation ladder (ADR 0019):
// the locality/desktop detection that decides which rung applies, the OS opener that
// auto-opens a deliverable on a user's own desktop, and the capability-token doc server
// that makes one reachable from the user's machine when Apogee runs remotely.
//
// It is mechanism, not policy. The delegate the tool routes through is domain.Presenter;
// the ladder itself — rung 0 (the transcript baseline, always) first, then the highest
// applicable rung above it — is walked by the host implementation that owns these pieces
// (the TUI's presenter). Nothing here decides that a document should be presented, and
// nothing here renders the transcript: the package answers "what can this machine do, and
// where would the user reach the file" and performs the mechanism when asked.
//
// Everything is an injectable seam — the environment lookup, the GOOS string, the command
// runner — so the mechanisms are table-testable off the machine the tests run on, which is
// the only way a ladder whose whole subject is the local platform can be tested at all.
//
// The package imports the standard library only. Under ADR 0010 it may depend on
// internal/domain downward and never on the root facade; today it needs neither, because
// the domain types cross the seam in the caller, not here.
//
// It runs OUTSIDE tool confinement by design (ADR 0019 §5): an opener is the host's own
// act on the user's own desktop session, the same category as the TUI drawing on the
// terminal — not a model-chosen subprocess ADR 0012's workspace fence exists to bound. The
// blast radius is bounded by what may be presented (a path already resolved inside the
// workspace root and confirmed to be a regular file), not by fencing a browser.
package present
