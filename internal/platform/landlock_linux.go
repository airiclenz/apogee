//go:build linux

package platform

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Linux landlock Confiner backend (P3.2 — ADR 0012, confinement-execution-contract §2.3/§5/§6)
// ----------------------------------------------------------------------------
//
// landlockConfiner realises the single, subprocess-granularity confinement model
// (ADR 0012): a confined tool call runs as a child process that applies a landlock
// domain to itself *after fork, before execve*, so the target inherits the domain
// across exec while the parent (the main apogee process) is never restricted. There
// is NO in-process per-thread landlock anywhere, hence no thread-discard trick and
// no goroutine-escape hole — Apogee's own in-process writes are path-safety-bounded
// instead (the contract's blast-radius split).
//
// Go cannot run user code between fork and execve without CGO, and the cross-build is
// CGO_ENABLED=0, so "apply landlock before execve" is realised as a re-exec wrapper:
// Confine rewrites the command to re-invoke the apogee binary itself in a hidden
// __confined-exec helper mode that, as a separate process, calls ApplyLandlockAndExec
// — which builds the ruleset, calls landlock_restrict_self, then syscall.Exec's the
// real argv. Both halves use raw golang.org/x/sys/unix syscalls (SYS_LANDLOCK_* over
// the typed attrs); x/sys exposes no high-level wrappers, so raw syscalls are the only
// CGO-free option and the github.com/landlock-l/go-landlock helper was not needed.

// confinedExecSentinel is the argv[1] marker that puts the apogee binary into the
// in-child landlock helper mode (confinement-execution-contract §2.3). The normal CLI
// never surfaces it; cmd/apogee dispatches it before Cobra (P3.4 host wiring). The
// confinetest harness re-execs the test binary under the same sentinel via the
// TestHelperProcess idiom.
const confinedExecSentinel = "__confined-exec"

// landlockConfiner is the Linux Confiner backend. Its ABI version is probed once at
// construction (NewLandlockConfiner) and never re-queried, so Capabilities reports what
// this kernel can enforce here and now (the contract's capability-honesty rule, §5).
//
// reexecPath is the executable Confine re-invokes for the in-child half. It defaults to
// the running binary (os.Executable, per the contract) and is overridable only so the
// confinetest harness can target the test binary; production code never sets it.
type landlockConfiner struct {
	abi        int    // landlock ABI version; <= 0 means landlock is unavailable on this host
	reexecPath string // executable to re-exec for the confined child ("" => os.Executable())
}

// landlock ABI thresholds (confinement-execution-contract §5; ADR 0012):
//
//   - ABI >= 1 (kernel >= 5.13) => filesystem-write confinement is enforceable.
//   - ABI >= 4 (kernel >= 6.7)  => TCP-connect (network-egress) restriction is enforceable.
const (
	landlockABIFSWrite = 1
	landlockABINetwork = 4
)

// landlockFSWriteAccess is the set of filesystem accesses the ruleset HANDLES (restricts).
// Handling the write-class accesses and then allowing them only beneath the box's writable
// roots is what fences writes to the workspace. Read and execute are deliberately NOT
// handled, so a confined child may still read and run programs anywhere — the box bounds
// where it can WRITE, matching ConfinementBox semantics (workspace-write-only).
const landlockFSWriteAccess = unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
	unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
	unix.LANDLOCK_ACCESS_FS_MAKE_REG |
	unix.LANDLOCK_ACCESS_FS_MAKE_SYM |
	unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
	unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
	unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
	unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
	unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
	unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
	unix.LANDLOCK_ACCESS_FS_TRUNCATE

// NewLandlockConfiner constructs the Linux landlock backend, probing the kernel's
// landlock ABI once. On a kernel without landlock the probe reports an unavailable ABI
// and Capabilities returns {false, false}, so the disposition gates the subprocess
// surface rather than confining it ("confine if you can, gate if you can't", ADR 0012).
// It returns domain.Confiner's sibling — the prepare-in-place backend — as a concrete
// type; the domain.Confiner interface itself gains the *exec.Cmd signature in P3.4.
func NewLandlockConfiner() *landlockConfiner {
	return &landlockConfiner{abi: probeLandlockABI()}
}

// probeLandlockABI returns the landlock ABI version supported by this kernel, or a
// value <= 0 when landlock is unavailable. It calls landlock_create_ruleset(NULL, 0,
// LANDLOCK_CREATE_RULESET_VERSION), which the kernel answers with the ABI version and
// never creates a ruleset (confinement-execution-contract §5).
func probeLandlockABI() int {
	abi, _, errno := unix.Syscall(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		0,
		0,
		unix.LANDLOCK_CREATE_RULESET_VERSION,
	)
	if errno != 0 {
		return -1
	}
	return int(abi)
}

// Capabilities reports what landlock can enforce on this kernel, probed once at
// construction (confinement-execution-contract §5). FSWrite is true at ABI >= 1
// (kernel >= 5.13); NetworkEgress is true only at ABI >= 4 (kernel >= 6.7). A kernel
// without landlock reports {false, false}.
func (c *landlockConfiner) Capabilities() domain.ConfinementCaps {
	return domain.ConfinementCaps{
		FSWrite:       c.abi >= landlockABIFSWrite,
		NetworkEgress: c.abi >= landlockABINetwork,
	}
}

// Confine prepares cmd to execute confined to box, then returns — it does not run cmd
// (confinement-execution-contract §2.2). It rewrites cmd to re-invoke the apogee binary
// in the __confined-exec helper mode, which applies the landlock domain to itself before
// exec'ing the original argv, and sets Setpgid so the caller's process-group kill reaches
// the wrapped child (§2.4). The parent process is never restricted.
//
// Confine is only meaningful when Capabilities().FSWrite is true; the dispatch disposition
// checks caps before calling it (§4). If the backend cannot resolve the binary to re-exec,
// it returns ErrConfinementUnavailable, the contract's "confine if you can, gate if you
// can't" safety net (§2.2). ctx covers only this synchronous preparation; the run's
// lifetime is governed by cmd's own context.
func (c *landlockConfiner) Confine(_ context.Context, box domain.ConfinementBox, cmd *exec.Cmd) error {
	if len(cmd.Args) == 0 {
		return fmt.Errorf("apogee: confine: cmd has no argv")
	}

	self := c.reexecPath
	if self == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("%w: cannot resolve self executable: %v", domain.ErrConfinementUnavailable, err)
		}
		self = exe
	}

	encoded, err := encodeBox(box)
	if err != nil {
		return fmt.Errorf("apogee: confine: encode box: %w", err)
	}

	// Re-exec the apogee binary in __confined-exec mode; argv after "--" is the
	// original command, run confined by the in-child half (ApplyLandlockAndExec).
	// It must carry the RESOLVED program path (cmd.Path), not the bare cmd.Args[0]:
	// the child re-execs via syscall.Exec, which does NO PATH lookup, so a bare
	// "sh" would fail with ENOENT (contract §2.3).
	prog := cmd.Path
	if prog == "" {
		prog = cmd.Args[0]
	}
	orig := append([]string{prog}, cmd.Args[1:]...)
	cmd.Path = self
	cmd.Args = append([]string{self, confinedExecSentinel, encoded, "--"}, orig...)

	// Setpgid puts the wrapped child and its descendants in one process group so the
	// tool's negative-PID kill reaps the whole group (contract §2.4); the tool sets
	// cmd.Cancel/WaitDelay (P3.8).
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	return nil
}

// ApplyLandlockAndExec is the in-child half of the re-exec wrapper (confinement-execution
// -contract §2.3). Running as a separate process, it builds a landlock ruleset from box,
// restricts itself to it (landlock_restrict_self), then replaces itself with argv via
// syscall.Exec. Because landlock domains survive execve, the exec'd program runs confined;
// because only this child ever called restrict_self, the parent stays unrestricted.
// It returns only on failure to establish the domain or exec — on success it never returns.
func ApplyLandlockAndExec(box domain.ConfinementBox, argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("apogee: confined-exec: empty argv")
	}
	if err := applyLandlock(box); err != nil {
		return err
	}
	if err := syscall.Exec(argv[0], argv, os.Environ()); err != nil {
		return fmt.Errorf("apogee: confined-exec: exec %q: %w", argv[0], err)
	}
	return nil // unreachable: syscall.Exec replaces the process image on success.
}

// applyLandlock creates the ruleset for box, adds the path-beneath allow rules for each
// writable root (and a TCP-connect handler when the box opts into network-deny), then
// calls landlock_restrict_self with NO_NEW_PRIVS set. After it returns nil the calling
// process is confined for the remainder of its life and across any subsequent execve.
func applyLandlock(box domain.ConfinementBox) error {
	abi := probeLandlockABI()
	if abi < landlockABIFSWrite {
		return fmt.Errorf("%w: landlock unavailable (abi %d)", domain.ErrConfinementUnavailable, abi)
	}

	handleNet, err := networkDenyDecision(box, abi)
	if err != nil {
		return err
	}

	attr := unix.LandlockRulesetAttr{Access_fs: landlockFSWriteAccess}
	if handleNet {
		attr.Access_net = unix.LANDLOCK_ACCESS_NET_CONNECT_TCP
	}

	rulesetFD, _, errno := unix.Syscall(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attr)),
		unsafe.Sizeof(attr),
		0,
	)
	if errno != 0 {
		return fmt.Errorf("apogee: confined-exec: landlock_create_ruleset: %w", errno)
	}
	fd := int(rulesetFD)
	defer unix.Close(fd)

	// Allow writes beneath the workspace root and each extra writable path. The box's
	// in-child semantics: handle the write-class accesses globally, then re-grant them
	// only under these roots — anything else is denied.
	roots := append([]string{box.WorkspaceRoot}, box.WritablePaths...)
	for _, root := range roots {
		if root == "" {
			continue
		}
		if err := allowWriteBeneath(fd, root); err != nil {
			return err
		}
	}

	// NO_NEW_PRIVS is mandatory before restrict_self for an unprivileged process.
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("apogee: confined-exec: prctl(NO_NEW_PRIVS): %w", err)
	}

	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, rulesetFD, 0, 0); errno != 0 {
		return fmt.Errorf("apogee: confined-exec: landlock_restrict_self: %w", errno)
	}
	return nil
}

// networkDenyDecision resolves what to do with the box's network policy at the given
// landlock ABI, as a pure function so the fail-closed rule is testable without a kernel.
//
//   - Empty NetworkAllow ⇒ network OPEN (ADR 0012 default): handleNet=false, no error.
//   - Non-empty NetworkAllow ⇒ the box opts into network-DENY (NetworkAllow is a
//     TIGHTENING list). When the kernel can enforce it (ABI >= 4) ⇒ handleNet=true.
//   - Non-empty NetworkAllow but ABI < 4 ⇒ FAIL CLOSED. The box explicitly requested a
//     network tightening; silently running network-OPEN would violate that intent — the
//     dangerous opposite of a fence the user believes is in place. It returns
//     ErrConfinementUnavailable so the dispatch disposition's "confine if you can, gate
//     if you can't" net routes the call to Approval rather than letting it run unfenced
//     (ADR 0012 — deny is a tightening the box requested, never a silent no-op).
func networkDenyDecision(box domain.ConfinementBox, abi int) (handleNet bool, err error) {
	if len(box.NetworkAllow) == 0 {
		return false, nil
	}
	if abi < landlockABINetwork {
		return false, fmt.Errorf("%w: box requests network-deny but landlock network rules need ABI >= %d (have %d); refusing to run network-open silently",
			domain.ErrConfinementUnavailable, landlockABINetwork, abi)
	}
	return true, nil
}

// allowWriteBeneath adds a path-beneath rule granting the handled write accesses under
// root. A root that cannot be opened (e.g. a not-yet-created WritablePaths entry) is
// skipped rather than failing the whole confinement — the box should not have to exist
// in full for the writable roots that do exist to be honoured.
func allowWriteBeneath(rulesetFD int, root string) error {
	rootFD, err := unix.Open(root, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("apogee: confined-exec: open %q: %w", root, err)
	}
	defer unix.Close(rootFD)

	rule := unix.LandlockPathBeneathAttr{
		Allowed_access: landlockFSWriteAccess,
		Parent_fd:      int32(rootFD),
	}
	if _, _, errno := unix.Syscall6(
		unix.SYS_LANDLOCK_ADD_RULE,
		uintptr(rulesetFD),
		uintptr(unix.LANDLOCK_RULE_PATH_BENEATH),
		uintptr(unsafe.Pointer(&rule)),
		0, 0, 0,
	); errno != 0 {
		return fmt.Errorf("apogee: confined-exec: landlock_add_rule %q: %w", root, errno)
	}
	return nil
}

// encodeBox serialises box to the base64-JSON form passed inline as the __confined-exec
// argument, so the helper child needs no shared state with the parent (ADR 0008
// statelessness; confinement-execution-contract §2.3).
func encodeBox(box domain.ConfinementBox) (string, error) {
	raw, err := json.Marshal(box)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// DecodeConfinedBox parses the base64-JSON box argument the __confined-exec dispatcher
// reads from argv. It is the inverse of encodeBox, exported for the cmd/apogee sentinel
// dispatcher (P3.4 host wiring) and the confinetest harness.
func DecodeConfinedBox(encoded string) (domain.ConfinementBox, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return domain.ConfinementBox{}, fmt.Errorf("apogee: confined-exec: decode box: %w", err)
	}
	var box domain.ConfinementBox
	if err := json.Unmarshal(raw, &box); err != nil {
		return domain.ConfinementBox{}, fmt.Errorf("apogee: confined-exec: unmarshal box: %w", err)
	}
	return box, nil
}

// ConfinedExecSentinel reports the argv[1] marker that selects the in-child landlock
// helper mode, so cmd/apogee (P3.4) can dispatch it before Cobra without importing the
// unexported constant.
func ConfinedExecSentinel() string { return confinedExecSentinel }
