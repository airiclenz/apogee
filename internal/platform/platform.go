package platform

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/airiclenz/apogee/internal/domain"
)

// Shell abstracts how a command line is handed to the operating system's shell —
// POSIX wraps it in `sh -c`, Windows in `cmd /c` — plus the three things that
// differ per OS once a line is actually being built and run: the process command
// line itself, argument quoting, and the environment a subprocess is scoped to.
// The terminal tool is the first real caller (Phase 3, Command only); Phase 5
// widened the surface to what the Windows backend and the git tools consume.
type Shell interface {
	// Command returns the argv that runs line through the platform shell:
	// {"sh", "-c", line} on POSIX, {"cmd", "/c", line} on Windows. The caller
	// wires the result into os/exec.
	Command(line string) []string

	// CommandLine returns the exact process command line Command's argv must be
	// launched with, or "" when the platform's own argv joining is faithful.
	//
	// It is empty on POSIX, where execve takes a real argv. It is NOT optional on
	// Windows for any line that may contain a double quote: CreateProcess takes a
	// single string, so os/exec joins argv with syscall.EscapeArg, which escapes an
	// embedded quote as \" — a form cmd.exe does not understand, so `echo "hi"`
	// reaches the shell as `echo \"hi\"` and a quoted path with a space fails
	// outright. Handing this string to syscall.SysProcAttr.CmdLine delivers the
	// line verbatim instead (internal/tools/exec_cmdline_other.go).
	CommandLine(line string) string

	// Quote returns arg quoted so the platform shell reads it as one argument:
	// single quotes on POSIX, double quotes on cmd.exe. A line assembled with
	// Quote must be launched with CommandLine on Windows — quoting is exactly what
	// the argv path mangles there.
	Quote(arg string) string

	// ScopeEnv returns the environment ("KEY=value" entries) a subprocess runs
	// with when the caller wants a scrubbed, allowlisted environment: each key in
	// keys that is present, in the order given, plus this platform's own essential
	// variables. POSIX has none — PATH and HOME are the caller's policy — while a
	// Windows process without %SystemRoot%, %ComSpec% or %PATHEXT% fails in ways
	// that look nothing like a missing variable, so the platform contributes that
	// floor rather than every caller re-deriving it. Values are read through lookup
	// (nil ⇒ os.LookupEnv), an absent key is omitted, and a key named twice is
	// emitted once (case-insensitively on Windows).
	ScopeEnv(keys []string, lookup func(string) (string, bool)) []string
}

// Path abstracts the path semantics that the standard library's path/filepath
// does not settle on its own: the executable suffix, and containment — which is
// case-insensitive on Windows and exact on POSIX, and which the Windows Confiner
// needs to collapse a ConfinementBox's roots and to enforce its labelling
// guardrails (ADR 0020 §6).
//
// There is deliberately no LookPath here. os/exec already implements per-OS
// executable lookup, including %PATHEXT% resolution on Windows, and a wrapper
// would add a seam with nothing behind it; ExecExt covers the one thing os/exec
// does not expose — the suffix itself — for callers that name a binary.
type Path interface {
	// ExecExt returns the filename extension the platform appends to
	// executables ("" on POSIX, ".exe" on Windows).
	ExecExt() string

	// Contains reports whether target is root itself or lies beneath it,
	// comparing normalised path components under the platform's case rules
	// (folded on Windows, exact on POSIX) so C:\Work2 is not inside C:\Work.
	// It compares locations, not files: no symlink is resolved, and callers hand
	// it absolute paths.
	Contains(root, target string) bool
}

// Host is the per-OS platform facility: shell invocation plus path semantics.
// Current returns the implementation selected at build time for the target OS.
// It is an interface, not a concrete type, precisely because the implementation
// is chosen by build tag — while both rule sets behind it are compiled
// everywhere, so Windows semantics stay table-testable on any host (host.go).
type Host interface {
	Shell
	Path
}

// denyConfiner is the no-confinement backend. It enforces nothing: Capabilities
// reports neither fs-write nor network-egress confinement, so AutoEligible is false.
// It is the host backend on OSes without a real Confiner (Windows until Phase 5), and
// the seam the P0.6 harness used to exercise New's Auto gate before the real backends
// landed. Because it reports {false, false}, the dispatch disposition gates the
// subprocess surface rather than handing it a cmd to confine; if a cmd is passed
// anyway (a caller that skipped the caps check), Confine honestly reports
// ErrConfinementUnavailable — "confine if you can, gate if you can't" (ADR 0012).
type denyConfiner struct{}

// Capabilities reports a backend that can enforce nothing — both fs-write and
// network-egress are false, so this backend never satisfies the Auto gate.
func (denyConfiner) Capabilities() domain.ConfinementCaps {
	return domain.ConfinementCaps{FSWrite: false, NetworkEgress: false}
}

// Confine cannot prepare a confined command — this backend enforces nothing — so it
// returns ErrConfinementUnavailable rather than running cmd unconfined. The dispatch
// disposition checks Capabilities() first and never reaches here in normal flow
// (confinement-execution-contract §2.2/§2.3).
func (denyConfiner) Confine(_ context.Context, _ domain.ConfinementBox, _ *exec.Cmd) error {
	return fmt.Errorf("%w: no confinement backend on this host", domain.ErrConfinementUnavailable)
}

// NewDenyConfiner returns the no-confinement backend. It enforces nothing and never
// satisfies the Auto gate. It returns domain.Confiner — the same type the root
// re-exports as apogee.Confiner (ADR 0010), so callers in either package assign it
// interchangeably.
func NewDenyConfiner() domain.Confiner { return denyConfiner{} }

// The stub must satisfy the Confiner contract at compile time.
var _ domain.Confiner = (*denyConfiner)(nil)
