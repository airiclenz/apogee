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
	// Shell returns the bare program Command wraps a line in: `sh` on POSIX,
	// `cmd` on Windows. It is a NAME, resolved by the caller, never a path —
	// this rung says which shell the platform speaks, and deliberately not
	// where that shell lives: resolving a name on PATH is a judgement about
	// what may be executed (security.ResolveProgram), which belongs above.
	Shell() string

	// Command returns the argv that runs line through the platform shell:
	// {"sh", "-c", line} on POSIX, {"cmd", "/c", line} on Windows. The caller
	// wires the result into os/exec, having first resolved argv[0] — the bare
	// name Shell returns — through the exec fence.
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

	// Quote returns arg quoted so the platform shell reads it as one argument, and
	// so the child the shell launches reads it back byte for byte: single quotes on
	// POSIX, and on Windows the double-quote-plus-backslash form CommandLineToArgvW
	// specifies, caret-escaped for cmd when arg contains a quote of its own (see
	// windowsQuote — cmd and CommandLineToArgvW disagree, and both parse the line).
	// A line assembled with Quote must be launched with CommandLine on Windows —
	// quoting is exactly what the argv path mangles there.
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
	//
	// PATH is the one value the scrub INSPECTS rather than copies: entries inside
	// workspaceRoot (and every entry that is not an absolute location, which names a
	// directory relative to the child's own cwd) are dropped, so a subprocess and its
	// children cannot resolve a program out of the box the model can write. An empty
	// workspaceRoot scopes nothing — a caller with no workspace has no fence to apply.
	ScopeEnv(workspaceRoot string, keys []string, lookup func(string) (string, bool)) []string

	// ScopeInheritedEnv returns env — a whole inherited environment, each entry
	// "KEY=value" — with exactly one value rewritten: PATH, scoped to workspaceRoot
	// under the same per-entry rule ScopeEnv applies (entries inside the workspace,
	// and entries that are not absolute locations, are dropped). Every other variable
	// is passed through byte for byte, in the order given, and an entry that is not a
	// "KEY=value" pair at all is passed through as it stands.
	//
	// It is the shape ScopeEnv cannot express. The tools that hand the MODEL a shell
	// or an interpreter inherit the operator's environment whole — an allowlist there
	// would break the developer tooling they exist to run — but the box the model can
	// write must still not supply the programs their children resolve, which is the
	// half of the exec fence that happens inside somebody else's process. PATH is
	// matched the way the OS resolves it: case-insensitively on Windows, where Path
	// and PATH are one variable, and exactly on POSIX, where they are two. An empty
	// workspaceRoot scopes nothing and returns env as it stands.
	ScopeInheritedEnv(workspaceRoot string, env []string) []string
}

// Path abstracts the one path semantic the standard library's path/filepath does
// not settle on its own: containment — case-insensitive on Windows and exact on
// POSIX, which the Windows Confiner needs to collapse a ConfinementBox's roots and
// to enforce its labelling guardrails (ADR 0020 §6).
//
// There is deliberately no LookPath and no ExecExt here. os/exec already
// implements per-OS executable lookup, including %PATHEXT% resolution on Windows,
// so a wrapper would add a seam with nothing behind it; the executable suffix was
// the one lookup-shaped fact os/exec does not expose, but nothing in the tree ever
// named a binary through it. It returns with its first real caller — Phase 5's own
// acceptance rule: no platform surface without a production caller.
type Path interface {
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
// It is the host backend on OSes without a real Confiner — including a Windows host below
// ADR 0020's build floor — and the seam the P0.6 harness used to exercise New's Auto gate
// before the real backends landed. Because it reports {false, false}, the dispatch disposition gates the
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
