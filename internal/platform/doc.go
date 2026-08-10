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
// recorded in ADR 0020 rather than discovered by a user. The label mechanism is
// a module of its own — internal/platform/winlabel owns the SDDL vocabulary,
// the journal that is written before any label lands, the walk that applies and
// clears them, and the wording every surface quotes — and this package is its
// composer: the restricted token, the guardrails that veto a root, and the
// Confiner seam itself stay here.
//
// HostID lives here for the same reason the backends do — it is a per-machine fact.
// It is the interlock that keeps a host-scoped confinement acknowledgement
// (`unconfined-hosts:`, ADR 0012 amendment 2026-07-21) from silently travelling
// between machines; it is not an authentication mechanism.
//
// # The files, one line each
//
// Eighteen files, in three groups: the shell/path Host every OS-touching caller reads, the
// Confiner backends, and the per-machine identity. The Windows label mechanism is a module of
// its own beside them (internal/platform/winlabel).
//
// The Host abstraction. platform.go is the interface set — Shell (the argv, the raw command
// line, quoting, the scoped environment) and Path — plus the Host accessor they hang off and
// denyConfiner, the deny-all backend every OS without a facility falls back to. host.go is the
// one behaviour table behind those interfaces: BOTH the POSIX and the Windows rule sets,
// compiled on every target, so Windows quoting and path semantics are table-testable from any
// host. platform_posix.go and platform_windows.go are the build-tagged selectors saying which
// rule set Current returns; only the Windows one carries anything of its own — the OS
// long-path resolver that lets Contains expand an 8.3 short name instead of refusing to
// compare it.
//
// The Confiner selectors. confiner_linux.go, confiner_darwin.go and confiner_other.go are
// three-line per-OS choices of NewConfiner and NewReportConfiner — landlock, seatbelt, and the
// deny-all stub. confiner_windows.go is the odd one out: it is both the Windows selector and
// the token backend itself, the only backend that mutates the machine (it labels the box's
// roots on disk and reverts them on Close, ADR 0020).
//
// The POSIX backends. confine_posix.go is the argv rewrite landlock and seatbelt share —
// neither runs the command, both re-exec it under a launcher in its own process group — so
// each backend keeps only what is genuinely its own. landlock_linux.go is the Linux backend:
// the ABI probe, the ruleset built from the box, the encode/decode of that box across the
// re-exec, and ApplyLandlockAndExec, the helper mode the apogee binary re-enters as the
// launcher. seatbelt.go is the host-agnostic half of the macOS backend — the generated
// profile, its canonical roots and quoting — so it unit-tests on any host, and
// seatbelt_darwin.go is the darwin-tagged constructor that probes once for sandbox-exec.
//
// The Windows backend's parts. winguard.go is the rules, and confines nothing: the version
// floor below which there is no token backend, the guardrails saying which roots may never be
// labelled Low, the fail-closed network-deny decision, and the residue and teardown wording
// every surface quotes. wintoken_windows.go is the fence itself — minting the restricted,
// Low-integrity primary token and reading the user-profile root the guardrails need.
// prewarm_windows.go hoists that backend's one-time label walk to startup behind a printed
// notice, so a large tree reads as an explained wait rather than a silent hang, and
// prewarm_other.go is the no-op it collapses to everywhere else, which is what keeps startup
// output byte-identical off Windows.
//
// The per-machine fact. hostid.go computes HostID once per process from the systemd or dbus
// machine-id file, falling back to the hostname, and reports whether the result is the
// unidentified sentinel.
//
// And doc.go this map.
package platform
