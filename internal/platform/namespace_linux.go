//go:build linux

package platform

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Linux namespace Confiner backend (ADR 0012, confinement-execution-contract §2.3/§5/§6)
// ----------------------------------------------------------------------------
//
// namespaceConfiner is the second Linux backend, selected when landlock cannot fence on
// this kernel (an LSM left out of the boot line — Raspberry Pi OS — or a container whose
// seccomp profile answers ENOSYS). It realises the same single, subprocess-granularity
// confinement model as landlock and seatbelt: a confined tool call runs under a launcher
// that fences the real child, and the parent (the main apogee process) is never
// restricted. The launcher is bubblewrap (`bwrap`), resolved on PATH once at construction
// (the probing NewNamespaceConfiner, which then launches it once for real) and never
// re-queried, so Capabilities reports what this host can enforce here and now (§5). A host
// without bwrap, or whose kernel refuses it the namespaces, fences nothing and says why,
// and the dispatch disposition gates the subprocess surface instead ("confine if you can,
// gate if you can't", ADR 0012).
//
// Why bwrap and not a native unshare: the fence is a user namespace plus a mount
// namespace whose root is a read-only bind of `/` with the box's writable roots bound
// read-write over it. Entering a user namespace needs CLONE_NEWUSER, which the kernel
// refuses to a multithreaded process — and a CGO-free Go program is multithreaded from
// its first instruction, with no hook between fork and execve to unshare in (the same
// constraint that made landlock a re-exec helper). bwrap is a small, single-threaded,
// setuid-free binary built to do exactly this, present on every mainstream distribution
// (Flatpak's own sandbox), so it is used as an optional external enhancement in the
// ADR 0042 pattern: resolved on PATH, gracefully absent. A native launcher is a later,
// separate decision.
//
// The box bounds where a confined child may WRITE. `--ro-bind / /` makes the whole
// filesystem read-only, then each writable root is bound read-write over itself, so a
// write outside the box fails with EROFS — the errno the kill-on-denial signature
// (denialkill.go) recognises. The write-exempt set is bwrap's minimal `--dev /dev` — defined
// by bwrap and named by reference, not enumerated here. A live run shows the nodes null, zero,
// full, random, urandom, tty; the symlinks ptmx, fd, core and stdin/stdout/stderr into the
// child's own /proc view; a fresh devpts at /dev/pts; a private tmpfs at /dev/shm; and, only
// when bwrap holds a controlling terminal, that terminal at /dev/console. Each entry is
// side-effect-free (a sink, a source, a private scratch mount), a /proc symlink, or the
// terminal the child already owns, so the exemption widens nothing the box protects — wider
// than landlock's exact-/dev/null set, and the contract's §2.3 property 2 names this
// backend's set.
// `--proc /proc` mounts a fresh procfs so the child sees its own process tree.
//
// What passes through untouched: cwd (bwrap chdir's to the parent's directory, which the
// ro-bind of `/` carries), the environment, and the three standard streams — the
// execution tool's Dir/Env/Stdin/Stdout/Stderr reach the real child exactly as with the
// other backends. No `--new-session` (it would detach the child from the terminal the
// tool drives) and no `--unshare-pid` (a PID namespace would hide the child's process
// group from the teardown kill).
//
// Teardown (§2.4) is two-sided: Setpgid puts bwrap and its child in one process group so
// the execution tool's negative-PID kill reaps both, and `--die-with-parent` makes the
// kernel deliver SIGKILL to the child the moment bwrap itself dies, so a child that
// escaped the group cannot outlive its launcher.
//
// Network follows landlock's semantics (ADR 0012): open by default, and a non-empty
// NetworkAllow opts the box into deny-all via `--unshare-net` — an empty network
// namespace with only a loopback, the same coarse tightening landlock ABI 4 enforces.
// Tighter than landlock's in one direction (an empty namespace also cuts UDP and abstract
// AF_UNIX sockets, which landlock's TCP-only rights do not) and no tighter in another: a
// pathname AF_UNIX socket is reached through the filesystem, which the read-only root
// carries in, so a deny box is not a total egress fence and Capabilities discloses the
// residual rather than claiming one.

// namespaceConfiner is the bwrap-launched Linux Confiner backend. bwrapPath is the launcher
// Confine execs, resolved once at construction; "" means the backend cannot fence on this
// host and unavailable carries the reason it discloses (contract §5).
type namespaceConfiner struct {
	bwrapPath   string // absolute path of bwrap; "" when the backend cannot fence
	unavailable string // why the backend cannot fence; "" when bwrapPath is set
	// cause is the typed counterpart of unavailable, so a caller can tell a probe that ran out
	// of time from a bwrap that is not installed without reading the sentence; "" when
	// bwrapPath is set.
	cause domain.ConfinementCause
}

// newNamespaceConfiner builds the backend from an explicit probe result. It is the shared
// constructor both the probing NewNamespaceConfiner and the hermetic tests use, so the
// caps/argv/rewrite logic is exercised without a real bwrap (contract §5). unavailable and
// cause travel together: a backend that cannot fence carries both, and one that can carries
// neither.
func newNamespaceConfiner(bwrapPath, unavailable string, cause domain.ConfinementCause) *namespaceConfiner {
	return &namespaceConfiner{bwrapPath: bwrapPath, unavailable: unavailable, cause: cause}
}

// NewNamespaceConfiner probes this host once and returns the namespace backend: bwrap is
// resolved on PATH (ADR 0042 — an optional external enhancement, gracefully absent), and a
// resolved bwrap is then launched for real once, because "bwrap is installed" is not "bwrap
// can fence here" (contract §5). Kernels and profiles that refuse CLONE_NEWUSER to an
// unprivileged process — `kernel.apparmor_restrict_unprivileged_userns`, a seccomp filter,
// `user.max_user_namespaces=0` — refuse it at run time, not at PATH-lookup time, so only a
// real launch can answer. Either failure yields a backend that fences nothing and says why
// through Capabilities().Unavailable and, in the typed form a caller can branch on, through
// Capabilities().Cause: an absent bwrap is domain.CauseBackendAbsent, while a launch that
// refused or outlived the probe budget carries the cause probeNamespace returned. The probe
// has no disk side effect, so the report confiner may construct it too.
func NewNamespaceConfiner() *namespaceConfiner {
	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		return newNamespaceConfiner("", "bwrap not on PATH", domain.CauseBackendAbsent)
	}
	if reason, cause := probeNamespace(bwrapPath); reason != "" {
		return newNamespaceConfiner("", reason, cause)
	}
	return newNamespaceConfiner(bwrapPath, "", "")
}

// namespaceProbeTimeout bounds the construction probe's one real launch: a bwrap that hangs
// setting up its namespaces must not stall apogee's startup. A minute, not ten seconds,
// because this budget is only ever SPENT on a host that is failing to answer — a bwrap that
// can fence returns in milliseconds and the probe returns with it — so a generous ceiling
// costs a healthy host nothing, while a cold, loaded box (a Pi paging its first bwrap in off
// an SD card, a container under a saturated CPU) stops being told it cannot fence when it can.
// That is internal/tuitest's poll-and-return reasoning applied to production. The consequence
// to state: a host whose bwrap truly wedges now stalls startup for a minute rather than ten
// seconds, which is the price of not downgrading a slow host's whole session.
//
// It is a var rather than a const only so the timeout case can lower it; production never
// assigns it.
var namespaceProbeTimeout = 60 * time.Second

// probeNamespace launches the platform shell running a no-op under bwrapPath with the exact
// flag line Confine would generate for a box rooted at the temp dir, and returns ("", "")
// when the launch exits 0 — the host can fence — or the reason it cannot, in both forms: the
// sentence `bwrap refused: <last non-empty stderr line>` (bwrap prints one diagnostic line,
// e.g. `bwrap: setting up uid map: Permission denied`; the exit error stands in when it
// printed nothing) with domain.CauseLaunchRefused, or `bwrap timed out` with
// domain.CauseProbeTimedOut. The two are never collapsed: a refusal is this host's answer,
// while a timeout is no answer at all. Stdin and stdout are /dev/null; only stderr is
// captured. WaitDelay bounds the drain after a timeout kill so a child left holding the
// stderr pipe cannot wedge Wait.
func probeNamespace(bwrapPath string) (string, domain.ConfinementCause) {
	ctx, cancel := context.WithTimeout(context.Background(), namespaceProbeTimeout)
	defer cancel()

	argv := append(namespaceArgv(domain.ConfinementBox{WorkspaceRoot: os.TempDir()}, pathExists), "--")
	argv = append(argv, Current().Command(":")...)
	cmd := exec.CommandContext(ctx, bwrapPath, argv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.WaitDelay = ProcessWaitDelay

	err := cmd.Run()
	if err == nil {
		return "", ""
	}
	if ctx.Err() != nil {
		return "bwrap timed out", domain.CauseProbeTimedOut
	}
	reason := lastNonEmptyLine(stderr.String())
	if reason == "" {
		reason = err.Error()
	}
	return "bwrap refused: " + reason, domain.CauseLaunchRefused
}

// lastNonEmptyLine returns the last line of s that is not blank, trimmed, or "" when every
// line is blank — the one diagnostic line a refused launcher leaves on stderr.
func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// Capabilities reports what the namespace backend can enforce on this host, probed once at
// construction (confinement-execution-contract §5). One bwrap launch fences both the
// filesystem (read-only root, writable binds) and — when the box asks — the network, so
// with bwrap present both caps are true; without it both are false, Unavailable says why in
// prose and Cause says the same thing in the typed form a caller branches on, so the
// disposition gates rather than confines and Auto is not refused (ADR 0012).
//
// The network fence is not total, and the gap is DISCLOSED rather than hidden (amends
// ADR 0081 §4, dated note in place). `--unshare-net` gives the box an empty network
// namespace, which cuts UDP and abstract AF_UNIX sockets along with TCP — but a PATHNAME
// AF_UNIX socket is a filesystem object, not a network-namespace one, so the `--ro-bind / /`
// root carries /var/run/docker.sock and /run/user/<uid>/bus into the box and a confined
// command can still connect(2) to them. That is a standing fact about the backend, not a
// per-box answer — a deny box is what makes it matter, and the caps are read before any box
// exists — so the token rides in Residuals wherever NetworkEgress is true, spelled once in
// internal/domain and shared with landlock, which leaks the same access for its own reason.
// Only probe.CapabilityLine words it: it is not write-class, so probe.ResidualNotice
// filters it out and no host gains a startup banner for it. Without bwrap the backend
// fences nothing and discloses no residual — that path is wholly Unavailable.
func (c *namespaceConfiner) Capabilities() domain.ConfinementCaps {
	if c.bwrapPath == "" {
		return domain.ConfinementCaps{Unavailable: c.unavailable, Cause: c.cause}
	}
	return domain.ConfinementCaps{
		FSWrite:       true,
		NetworkEgress: true,
		Residuals:     []string{domain.ResidualUnixEgress},
	}
}

// Confine prepares cmd to execute confined to box, then returns — it does not run cmd
// (confinement-execution-contract §2.2). It rewrites cmd to launch under
// `bwrap <flags from the box> -- <original argv...>` and sets Setpgid so the caller's
// process-group kill reaches the wrapped child (§2.4). The original
// Stdin/Stdout/Stderr/Dir/Env are inherited by bwrap, which execs the real child. The
// parent process is never restricted.
//
// Confine is only meaningful when Capabilities().FSWrite is true; the dispatch disposition
// checks caps before calling it (§4). Without bwrap it returns ErrConfinementUnavailable
// carrying the reason, the contract's "confine if you can, gate if you can't" safety net
// (§2.2) — never an unfenced run. ctx covers only this synchronous preparation; the run's
// lifetime is governed by cmd's own context.
func (c *namespaceConfiner) Confine(_ context.Context, box domain.ConfinementBox, cmd *exec.Cmd) error {
	// The argv guard runs before the availability check, so a cmd with no argv reports "no
	// argv" on every host rather than becoming ErrConfinementUnavailable where bwrap is
	// absent. wrapArgvUnderLauncher guards the same condition with the same error; this one
	// exists purely to keep that ordering.
	if len(cmd.Args) == 0 {
		return errNoArgv
	}
	if c.bwrapPath == "" {
		return fmt.Errorf("%w: %s", domain.ErrConfinementUnavailable, c.unavailable)
	}

	// Launch the original command under bwrap; argv after "--" is the original command, run
	// inside the namespaces bwrap sets up from the flags. The wrap carries the resolved
	// program path and the process-group rule for every POSIX backend (confine_posix.go).
	flags := append(namespaceArgv(box, pathExists), "--")
	if err := wrapArgvUnderLauncher(cmd, c.bwrapPath, flags...); err != nil {
		return err
	}
	setConfinedPgid(cmd)
	return nil
}

// namespaceArgv builds bwrap's flags for box (confinement-execution-contract §2.3). It is a
// pure function of the box and the injected exists predicate — no process, no host state —
// so it is unit-tested hermetically. The flags, in this order:
//
//   - `--ro-bind / /` — the whole filesystem, read-only: deny-default for writes, reads and
//     exec stay open (the box bounds where a child may WRITE, like the other backends);
//   - `--dev /dev` — the minimal device set, the backend's write-exempt set (see the file
//     header);
//   - `--proc /proc` — a fresh procfs for the child's own view of its process tree;
//   - `--die-with-parent` — the child dies with bwrap (§2.4 teardown);
//   - `--unshare-net` — only when the box opts into network-deny via a non-empty
//     NetworkAllow (ADR 0012: open by default, deny is a coarse tightening);
//   - `--bind <root> <root>` for WorkspaceRoot and each WritablePaths entry, bound
//     read-write over the read-only root so writes beneath them succeed. Each root is
//     canonicalized first (canonicalWritableRoot) so the bind lands on the directory the
//     child's writes resolve to. Empty entries are skipped, and so is a root exists reports
//     absent — bwrap refuses to bind a source that does not exist, and the box should not
//     have to exist in full for the writable roots that do exist to be honoured (the
//     ENOENT rule of landlock's allowWriteBeneath).
//
// Never `--new-session` and never `--unshare-pid` (see the file header for why).
func namespaceArgv(box domain.ConfinementBox, exists func(string) bool) []string {
	roots := make([]string, 0, 1+len(box.WritablePaths))
	roots = append(roots, box.WorkspaceRoot)
	roots = append(roots, box.WritablePaths...)

	argv := make([]string, 0, 7+2*len(roots))
	argv = append(argv, "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--die-with-parent")

	// Network is open by default (ADR 0012). A non-empty NetworkAllow opts the box into
	// network-deny: an empty network namespace, the same deny-all landlock ABI 4 enforces
	// (per-host allow is a later additive change).
	if len(box.NetworkAllow) > 0 {
		argv = append(argv, "--unshare-net")
	}

	for _, root := range roots {
		if root == "" {
			continue
		}
		canonical := canonicalWritableRoot(root)
		if !exists(canonical) {
			continue
		}
		argv = append(argv, "--bind", canonical, canonical)
	}
	return argv
}

// pathExists is the exists predicate Confine hands namespaceArgv: whether p can be stat'ed.
// It is coarser than landlock's allowWriteBeneath, which skips only ENOENT and fails the
// confinement closed on every other open error: here a root that is present but
// unreachable (EACCES on a parent) is dropped silently, because the argv generator is a
// pure function with no error channel and bwrap would refuse the bind anyway. The outcome
// is fail-closed for the child either way — an unbound root stays read-only inside the box.
func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
