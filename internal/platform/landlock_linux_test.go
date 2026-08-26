//go:build linux

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform/confinetest"
)

// TestMain intercepts the __confined-exec sentinel so the test binary itself acts as the
// in-child half of the re-exec wrapper (the standard TestHelperProcess idiom,
// confinement-execution-contract §6.1). When the harness's confined *exec.Cmd re-execs
// os.Args[0] (the test binary) with the sentinel, this dispatches to ApplyLandlockAndExec
// — exactly what cmd/apogee's main does for the product binary in P3.4.
func TestMain(m *testing.M) {
	if len(os.Args) >= 2 && os.Args[1] == confinedExecSentinel {
		os.Exit(runConfinedExecChild(os.Args[2:]))
	}
	os.Exit(m.Run())
}

// runConfinedExecChild mirrors the cmd/apogee sentinel dispatcher (P3.4): argv is
// [<encoded-box>, "--", <real argv...>]. It decodes the box, then hands off to
// ApplyLandlockAndExec, which confines this process and exec's the real argv.
func runConfinedExecChild(args []string) int {
	if len(args) < 2 || args[1] != "--" {
		fmt.Fprintln(os.Stderr, "confined-exec: malformed argv")
		return 2
	}
	box, err := DecodeConfinedBox(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := ApplyLandlockAndExec(box, args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0 // unreachable on success: ApplyLandlockAndExec exec's away.
}

// newTestConfiner returns a landlock confiner whose re-exec target is the test binary,
// so the harness's confined children land in TestMain's sentinel dispatcher above.
func newTestConfiner(t *testing.T) *landlockConfiner {
	t.Helper()
	c := NewLandlockConfiner()
	c.reexecPath = os.Args[0]
	return c
}

func TestLandlockProbe(t *testing.T) {
	// Not parallel: the confined children are real subprocesses of this binary.
	confinetest.Probe(t, newTestConfiner(t), Current(), FailFastPreamble(), newProbeDenialKiller)
}

func TestLandlockProbeNetwork(t *testing.T) {
	confinetest.ProbeNetwork(t, newTestConfiner(t), Current())
}

// The battery's row #12 branches on the backend's OWN disclosure, so on its own it cannot catch a
// backend that discloses the wrong thing consistently. This pins the disclosure to the kernel the
// tests actually run on: below ABI 3 landlock has no LANDLOCK_ACCESS_FS_TRUNCATE bit and must say
// so; at ABI 3 and above the fence is complete and must say nothing (C-06, live-reproduced
// 2026-08-25). Together the two make the truncate residual un-fakeable in either direction.
func TestLandlockResidualsMatchHostABI(t *testing.T) {
	t.Parallel()
	c := NewLandlockConfiner()
	caps := c.Capabilities()
	if !caps.FSWrite {
		t.Skip("no landlock on this kernel; there is no fence to disclose a residual in")
	}
	wantResiduals := []string(nil)
	if c.abi < landlockABITruncate {
		wantResiduals = []string{"truncate(2)"}
	}
	if !slices.Equal(caps.Residuals, wantResiduals) {
		t.Errorf("host landlock ABI %d discloses Residuals = %v, want %v", c.abi, caps.Residuals, wantResiduals)
	}
}

func TestLandlockCapabilitiesHonest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		abi              int
		wantFSWrite      bool
		wantNetwork      bool
		wantAutoEligible bool
		wantAccess       uint64   // mask applyLandlock would request; asserted only when wantFSWrite
		wantResiduals    []string // write-class accesses this ABI leaves unfenced while FSWrite is true
	}{
		{"no_landlock", -1, false, false, false, 0, nil},
		// fs-only; AutoEligible on fs alone (ADR 0012). Its mask must stay at the ABI-1
		// baseline: asking for TRUNCATE (ABI 3) here was EINVAL on every create_ruleset — so
		// the access goes unfenced and is DISCLOSED instead (C-06, live-reproduced 2026-08-25).
		{"abi1_kernel_5_13", 1, true, false, true, baselineFSWriteAccess, []string{"truncate(2)"}},
		// Debian 12 (6.1). REFER exists from here, and must be handled or the kernel denies
		// cross-directory rename/link outright — including inside the workspace. TRUNCATE still
		// does not, so the residual still stands.
		{"abi2_kernel_5_19", 2, true, false, true, baselineFSWriteAccess | unix.LANDLOCK_ACCESS_FS_REFER, []string{"truncate(2)"}},
		// still fs-only, still Auto-eligible; TRUNCATE is finally requestable, so the fence is
		// complete and nothing is disclosed.
		{"abi3_kernel_6_2", 3, true, false, true, fullFSWriteAccess, nil},
		{"abi4_kernel_6_7", 4, true, true, true, fullFSWriteAccess, nil}, // network egress now enforceable
		{"abi6_newer", 6, true, true, true, fullFSWriteAccess, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &landlockConfiner{abi: tt.abi}
			caps := c.Capabilities()
			if caps.FSWrite != tt.wantFSWrite {
				t.Errorf("abi %d: FSWrite = %v, want %v", tt.abi, caps.FSWrite, tt.wantFSWrite)
			}
			if caps.NetworkEgress != tt.wantNetwork {
				t.Errorf("abi %d: NetworkEgress = %v, want %v", tt.abi, caps.NetworkEgress, tt.wantNetwork)
			}
			// AutoEligible is FSWrite-only per ADR 0012 once P3.4 loosens it; until then the
			// domain predicate is FSWrite&&NetworkEgress. Assert the contract-mandated
			// property — fs-confinement alone makes the host confinement-usable — at the cap
			// level, independent of the (P3.4) AutoEligible predicate.
			if got := caps.FSWrite; got != tt.wantAutoEligible {
				t.Errorf("abi %d: fs-confinement availability = %v, want %v (Auto needs fs only, ADR 0012)", tt.abi, got, tt.wantAutoEligible)
			}
			// Capability honesty's second half (contract §5): a fence that leaves a write-class
			// access open must SAY which one. Disclosing it is what keeps FSWrite=true honest at
			// ABI 1–2 — and a backend that grows a residual without disclosing it, or keeps
			// disclosing one it has since closed, fails here.
			if !slices.Equal(caps.Residuals, tt.wantResiduals) {
				t.Errorf("abi %d: Residuals = %v, want %v (an unfenced write-class access must be disclosed)",
					tt.abi, caps.Residuals, tt.wantResiduals)
			}
			// A residual is disclosure, never a gate: Auto still keys on FSWrite alone.
			if caps.AutoEligible() != tt.wantFSWrite {
				t.Errorf("abi %d: AutoEligible = %v, want %v (a residual must not change the Auto gate)",
					tt.abi, caps.AutoEligible(), tt.wantFSWrite)
			}
			if !tt.wantFSWrite {
				return // nothing is advertised, so no mask is ever handed to this kernel
			}
			// Capability honesty is a claim this kernel will ACCEPT the ruleset, not just that
			// landlock exists: advertising FSWrite while applyLandlock requests a right the ABI
			// does not know makes every confined call fail with EINVAL, so Auto becomes a mode
			// in which nothing runs. Exercising the derivation here is what makes the ABI-1 and
			// ABI-2 rows mean something below ABI 3.
			if got := accessMaskForABI(tt.abi); got != tt.wantAccess {
				t.Errorf("abi %d: accessMaskForABI = %#x, want %#x (advertising FSWrite obliges a mask this kernel accepts)", tt.abi, got, tt.wantAccess)
			}
		})
	}
}

// baselineFSWriteAccess and fullFSWriteAccess are the two mask shapes the tests pin, spelled
// out from the unix constants rather than from landlockFSWriteAccessABI1 so a right silently
// dropped from (or added to) the production constant fails the table instead of moving with it.
const (
	baselineFSWriteAccess = uint64(unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
		unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
		unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SYM |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE)

	fullFSWriteAccess = baselineFSWriteAccess |
		unix.LANDLOCK_ACCESS_FS_REFER |
		unix.LANDLOCK_ACCESS_FS_TRUNCATE
)

func TestAccessMaskForABI(t *testing.T) {
	t.Parallel()

	// The exact mask per ABI. Both call sites (ruleset creation and every path-beneath rule)
	// derive from this function, and the kernel cross-checks them: a rule's allowed_access must
	// be a subset of the ruleset's handled_access_fs, so one shared derivation is the invariant.
	// wantTruncate is spelled per row rather than left implicit in the mask constants: it is the
	// bit Capabilities().Residuals answers for, so the mask and the disclosure are pinned against
	// the same table and cannot drift apart (C-06).
	tests := []struct {
		name         string
		abi          int
		want         uint64
		wantTruncate bool
	}{
		// Not a valid input (applyLandlock refuses below the floor); pinned so the clamp can
		// never become a mask that handles nothing — a ruleset fencing no write at all.
		{"below_floor_clamps_to_baseline", -1, baselineFSWriteAccess, false},
		{"abi1_kernel_5_13", 1, baselineFSWriteAccess, false},
		{"abi2_kernel_5_19", 2, baselineFSWriteAccess | unix.LANDLOCK_ACCESS_FS_REFER, false},
		{"abi3_kernel_6_2", 3, fullFSWriteAccess, true},
		{"abi4_kernel_6_7", 4, fullFSWriteAccess, true},
		{"abi6_newer", 6, fullFSWriteAccess, true},
		{"abi99_future", 99, fullFSWriteAccess, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := accessMaskForABI(tt.abi); got != tt.want {
				t.Errorf("accessMaskForABI(%d) = %#x, want %#x", tt.abi, got, tt.want)
			}
			if got := accessMaskForABI(tt.abi)&unix.LANDLOCK_ACCESS_FS_TRUNCATE != 0; got != tt.wantTruncate {
				t.Errorf("accessMaskForABI(%d) handles TRUNCATE = %v, want %v (below ABI %d it is unfenced and disclosed as a residual)",
					tt.abi, got, tt.wantTruncate, landlockABITruncate)
			}
		})
	}
}

func TestAccessMaskForABIRightsTrackTheKernel(t *testing.T) {
	t.Parallel()

	// The properties behind the table, stated so a future right added to the mask cannot
	// regress them by moving the pinned constants too.
	for abi := landlockABIFSWrite; abi <= 8; abi++ {
		mask := accessMaskForABI(abi)

		// Never above the kernel: an unknown bit in handled_access_fs is EINVAL from
		// landlock_create_ruleset, and the child dies before exec.
		if abi < landlockABITruncate && mask&unix.LANDLOCK_ACCESS_FS_TRUNCATE != 0 {
			t.Errorf("abi %d: mask %#x requests TRUNCATE, which no kernel below ABI %d knows (EINVAL)", abi, mask, landlockABITruncate)
		}
		if abi < landlockABIRefer && mask&unix.LANDLOCK_ACCESS_FS_REFER != 0 {
			t.Errorf("abi %d: mask %#x requests REFER, which no kernel below ABI %d knows (EINVAL)", abi, mask, landlockABIRefer)
		}

		// Never below the kernel: REFER is denied by default even when UNHANDLED, so leaving it
		// out of a ruleset that could carry it forbids cross-directory rename/link (`git mv`)
		// even wholly inside the workspace.
		if abi >= landlockABIRefer && mask&unix.LANDLOCK_ACCESS_FS_REFER == 0 {
			t.Errorf("abi %d: mask %#x omits REFER; cross-directory rename/link is then denied inside the box too", abi, mask)
		}
		if abi >= landlockABITruncate && mask&unix.LANDLOCK_ACCESS_FS_TRUNCATE == 0 {
			t.Errorf("abi %d: mask %#x omits TRUNCATE; truncating a file outside the box would go unfenced", abi, mask)
		}

		// The write-class baseline is the floor at every ABI — the fence itself.
		if mask&baselineFSWriteAccess != baselineFSWriteAccess {
			t.Errorf("abi %d: mask %#x drops part of the ABI-1 write baseline %#x", abi, mask, baselineFSWriteAccess)
		}
		// Read and execute stay unhandled: the box bounds where a child WRITES (ADR 0012).
		for _, unhandled := range []struct {
			name string
			bit  uint64
		}{
			{"LANDLOCK_ACCESS_FS_READ_FILE", unix.LANDLOCK_ACCESS_FS_READ_FILE},
			{"LANDLOCK_ACCESS_FS_READ_DIR", unix.LANDLOCK_ACCESS_FS_READ_DIR},
			{"LANDLOCK_ACCESS_FS_EXECUTE", unix.LANDLOCK_ACCESS_FS_EXECUTE},
		} {
			if mask&unhandled.bit != 0 {
				t.Errorf("abi %d: mask %#x handles %s; the box is workspace-WRITE-only", abi, mask, unhandled.name)
			}
		}
	}
}

func TestDeviceAccessMaskForABI(t *testing.T) {
	t.Parallel()

	// The mask carried by the /dev/null exemption rule. Its parent_fd is a file, so the mask is
	// derived separately from the roots' directory mask — see the subset/file-applicability
	// properties pinned in TestDeviceAccessMaskStaysRuleApplicable below.
	tests := []struct {
		name string
		abi  int
		want uint64
	}{
		// Not a valid input (applyLandlock refuses below the floor); pinned so the clamp stays a
		// mask that still grants the write the exemption exists for.
		{"below_floor_clamps_to_write_file", -1, unix.LANDLOCK_ACCESS_FS_WRITE_FILE},
		{"abi1_kernel_5_13", 1, unix.LANDLOCK_ACCESS_FS_WRITE_FILE},
		{"abi2_kernel_5_19", 2, unix.LANDLOCK_ACCESS_FS_WRITE_FILE},
		// From ABI 3 the ruleset handles TRUNCATE, so `> /dev/null` (O_TRUNC) is denied unless
		// the exemption rule carries it too.
		{"abi3_kernel_6_2", 3, devNullAccessWithTruncate},
		{"abi4_kernel_6_7", 4, devNullAccessWithTruncate},
		{"abi6_newer", 6, devNullAccessWithTruncate},
		{"abi99_future", 99, devNullAccessWithTruncate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := deviceAccessMaskForABI(tt.abi); got != tt.want {
				t.Errorf("deviceAccessMaskForABI(%d) = %#x, want %#x", tt.abi, got, tt.want)
			}
		})
	}
}

// devNullAccessWithTruncate is the ABI-3+ shape of the exemption mask, spelled from the unix
// constants rather than from the production function so the table pins a value instead of
// restating the code.
const devNullAccessWithTruncate = uint64(unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_TRUNCATE)

func TestDeviceAccessMaskStaysRuleApplicable(t *testing.T) {
	t.Parallel()

	// The two properties the kernel enforces on the exemption rule. Breaking either one does not
	// merely lose the exemption: landlock_add_rule answers EINVAL, applyLandlock fails, and every
	// confined call on the host dies before exec.
	for abi := landlockABIFSWrite; abi <= 8; abi++ {
		mask := deviceAccessMaskForABI(abi)

		// A rule's allowed_access must be a subset of the ruleset's handled_access_fs.
		if handled := accessMaskForABI(abi); mask&^handled != 0 {
			t.Errorf("abi %d: device mask %#x is not a subset of the handled mask %#x (landlock_add_rule EINVAL)", abi, mask, handled)
		}
		// A rule on a FILE parent_fd may carry only file-applicable rights.
		for _, dirOnly := range []struct {
			name string
			bit  uint64
		}{
			{"LANDLOCK_ACCESS_FS_MAKE_DIR", unix.LANDLOCK_ACCESS_FS_MAKE_DIR},
			{"LANDLOCK_ACCESS_FS_MAKE_REG", unix.LANDLOCK_ACCESS_FS_MAKE_REG},
			{"LANDLOCK_ACCESS_FS_MAKE_SYM", unix.LANDLOCK_ACCESS_FS_MAKE_SYM},
			{"LANDLOCK_ACCESS_FS_MAKE_SOCK", unix.LANDLOCK_ACCESS_FS_MAKE_SOCK},
			{"LANDLOCK_ACCESS_FS_MAKE_FIFO", unix.LANDLOCK_ACCESS_FS_MAKE_FIFO},
			{"LANDLOCK_ACCESS_FS_MAKE_BLOCK", unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK},
			{"LANDLOCK_ACCESS_FS_MAKE_CHAR", unix.LANDLOCK_ACCESS_FS_MAKE_CHAR},
			{"LANDLOCK_ACCESS_FS_REMOVE_DIR", unix.LANDLOCK_ACCESS_FS_REMOVE_DIR},
			{"LANDLOCK_ACCESS_FS_REMOVE_FILE", unix.LANDLOCK_ACCESS_FS_REMOVE_FILE},
			{"LANDLOCK_ACCESS_FS_REFER", unix.LANDLOCK_ACCESS_FS_REFER},
		} {
			if mask&dirOnly.bit != 0 {
				t.Errorf("abi %d: device mask %#x carries directory-only %s on a file parent_fd (landlock_add_rule EINVAL)", abi, mask, dirOnly.name)
			}
		}
		// And the exemption must still grant the write it exists for.
		if mask&unix.LANDLOCK_ACCESS_FS_WRITE_FILE == 0 {
			t.Errorf("abi %d: device mask %#x grants no WRITE_FILE; `2>/dev/null` stays denied inside the box", abi, mask)
		}
	}
}

func TestLandlockAllowsDevNullThroughTheFence(t *testing.T) {
	// Not parallel: the confined children are real subprocesses of this binary.
	c := newTestConfiner(t)
	if !c.Capabilities().FSWrite {
		t.Skip("landlock unavailable on this host (FSWrite==false); skipping the live /dev/null exemption probe")
	}

	ws := t.TempDir()
	outside := t.TempDir()
	box := domain.ConfinementBox{WorkspaceRoot: ws}

	t.Run("dev_null_write_succeeds", func(t *testing.T) {
		// Both halves of the idiom that the fence broke: the stderr redirect every confined
		// terminal call uses, and a truncating `>` redirect (O_TRUNC, hence the TRUNCATE right).
		const line = `: 2>/dev/null && echo x > /dev/null`
		if err := runConfinedShellLine(t, c, box, line); err != nil {
			t.Fatalf("confined `sh -c %q` failed, want success (/dev/null is exempt from the fence): %v", line, err)
		}
	})

	t.Run("out_of_box_write_still_denied", func(t *testing.T) {
		// The exemption is exactly one device, not a hole in the fence: an ordinary out-of-box
		// write must still be OS-denied.
		target := filepath.Join(outside, "escape.txt")
		if err := runConfinedShellLine(t, c, box, "echo x > "+Current().Quote(target)); err == nil {
			t.Fatalf("confined write to %q succeeded, want OS denial (the exemption must not widen the fence)", target)
		}
		if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat %q = %v, want not-exist: the out-of-box write was not blocked", target, err)
		}
	})
}

// runConfinedShellLine runs line through the platform shell, confined to box by c, and returns
// the run error. It mirrors confinetest's runConfined for probes that are landlock-specific and
// so cannot join the cross-platform battery — /dev/null is a POSIX device with no Windows
// counterpart the battery could assert on.
func runConfinedShellLine(t *testing.T, c *landlockConfiner, box domain.ConfinementBox, line string) error {
	t.Helper()
	ctx := context.Background()
	argv := Current().Command(line)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if err := c.Confine(ctx, box, cmd); err != nil {
		t.Fatalf("Confine(%v): %v", argv, err)
	}
	return cmd.Run()
}

func TestLandlockConfineRewritesCmd(t *testing.T) {
	t.Parallel()

	c := &landlockConfiner{abi: 4, reexecPath: "/path/to/apogee"}
	box := domain.ConfinementBox{WorkspaceRoot: "/ws"}
	cmd := exec.Command("/bin/echo", "hello", "world")

	if err := c.Confine(context.Background(), box, cmd); err != nil {
		t.Fatalf("Confine: %v", err)
	}

	if cmd.Path != "/path/to/apogee" {
		t.Errorf("cmd.Path = %q, want re-exec self %q", cmd.Path, "/path/to/apogee")
	}
	if len(cmd.Args) < 5 {
		t.Fatalf("cmd.Args = %v, too short", cmd.Args)
	}
	if cmd.Args[0] != "/path/to/apogee" {
		t.Errorf("cmd.Args[0] = %q, want self", cmd.Args[0])
	}
	if cmd.Args[1] != confinedExecSentinel {
		t.Errorf("cmd.Args[1] = %q, want sentinel %q", cmd.Args[1], confinedExecSentinel)
	}
	// Args[2] is the encoded box; it must round-trip.
	gotBox, err := DecodeConfinedBox(cmd.Args[2])
	if err != nil {
		t.Fatalf("decode box arg: %v", err)
	}
	if gotBox.WorkspaceRoot != "/ws" {
		t.Errorf("decoded box WorkspaceRoot = %q, want /ws", gotBox.WorkspaceRoot)
	}
	if cmd.Args[3] != "--" {
		t.Errorf("cmd.Args[3] = %q, want separator --", cmd.Args[3])
	}
	gotOrig := strings.Join(cmd.Args[4:], " ")
	if gotOrig != "/bin/echo hello world" {
		t.Errorf("original argv = %q, want %q", gotOrig, "/bin/echo hello world")
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Error("Confine did not set SysProcAttr.Setpgid for process-group teardown")
	}
}

func TestLandlockConfineForwardsResolvedPath(t *testing.T) {
	t.Parallel()

	// Regression (P3.4 review): the confined argv must carry the RESOLVED program
	// path (cmd.Path), not the bare cmd.Args[0]. The in-child half re-execs via
	// syscall.Exec, which does NO PATH lookup, so a bare "echo" would ENOENT.
	c := &landlockConfiner{abi: 4, reexecPath: "/path/to/apogee"}
	cmd := exec.Command("echo", "hi") // bare name; Go resolves cmd.Path via LookPath
	resolved := cmd.Path
	if resolved == "" || !strings.HasPrefix(resolved, "/") {
		t.Skipf("echo did not resolve to an absolute path (%q); cannot exercise regression", resolved)
	}
	if err := c.Confine(context.Background(), domain.ConfinementBox{WorkspaceRoot: "/ws"}, cmd); err != nil {
		t.Fatalf("Confine: %v", err)
	}
	// Args layout: [self, sentinel, box, "--", <program>, <args...>].
	if len(cmd.Args) < 6 {
		t.Fatalf("cmd.Args = %v, too short", cmd.Args)
	}
	if got := cmd.Args[4]; got != resolved {
		t.Errorf("confined program = %q, want resolved path %q (not bare name)", got, resolved)
	}
}

func TestLandlockConfineRejectsEmptyArgv(t *testing.T) {
	t.Parallel()

	// Confine must refuse a cmd with no argv rather than produce a malformed re-exec
	// command line — the deterministic guard before any self-resolution.
	c := &landlockConfiner{abi: 4}
	cmd := &exec.Cmd{} // no Args
	err := c.Confine(context.Background(), domain.ConfinementBox{}, cmd)
	if err == nil {
		t.Fatal("Confine with empty argv returned nil, want error")
	}
	// The message is the shared POSIX one (confine_posix.go's errNoArgv), and it must survive
	// the guard being duplicated here to keep it ahead of the self-executable resolution:
	// pinned on both backends so neither drifts after the argv wrap moved out of them.
	if got, want := err.Error(), "apogee: confine: cmd has no argv"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

func TestApplyLandlockAndExecRejectsEmptyArgv(t *testing.T) {
	t.Parallel()

	// The in-child half must refuse an empty argv before touching landlock or exec — there
	// is nothing to exec, so it returns an error rather than producing a malformed exec.
	// This guard fires regardless of the host kernel (no landlock call is reached), so the
	// test is hermetic on this dev host where landlock is off.
	if err := ApplyLandlockAndExec(domain.ConfinementBox{}, nil); err == nil {
		t.Fatal("ApplyLandlockAndExec(nil argv) returned nil, want error")
	}
	if err := ApplyLandlockAndExec(domain.ConfinementBox{}, []string{}); err == nil {
		t.Fatal("ApplyLandlockAndExec(empty argv) returned nil, want error")
	}
}

func TestNetworkDenyDecision(t *testing.T) {
	t.Parallel()

	denyBox := domain.ConfinementBox{WorkspaceRoot: "/ws", NetworkAllow: []string{"example.com:443"}}
	openBox := domain.ConfinementBox{WorkspaceRoot: "/ws"}

	tests := []struct {
		name          string
		box           domain.ConfinementBox
		abi           int
		wantHandleNet bool
		wantErr       bool // expect ErrConfinementUnavailable (fail-closed)
	}{
		// Network open (empty NetworkAllow): never restricts, never fails, any ABI.
		{"open_abi1", openBox, 1, false, false},
		{"open_abi4", openBox, 4, false, false},
		// Network deny + enforceable (ABI >= 4): restrict TCP connect.
		{"deny_abi4", denyBox, 4, true, false},
		{"deny_abi6", denyBox, 6, true, false},
		// Network deny but NOT enforceable (ABI < 4): FAIL CLOSED rather than running open.
		{"deny_abi1_failclosed", denyBox, 1, false, true},
		{"deny_abi3_failclosed", denyBox, 3, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handleNet, err := networkDenyDecision(tt.box, tt.abi)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("abi %d, deny box: err = nil, want fail-closed (network-deny unenforceable must not run open)", tt.abi)
				}
				if !errors.Is(err, domain.ErrConfinementUnavailable) {
					t.Errorf("abi %d: err = %v, want ErrConfinementUnavailable so dispatch gates the call", tt.abi, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("abi %d: unexpected err: %v", tt.abi, err)
			}
			if handleNet != tt.wantHandleNet {
				t.Errorf("abi %d: handleNet = %v, want %v", tt.abi, handleNet, tt.wantHandleNet)
			}
		})
	}
}

func TestEncodeDecodeBoxRoundTrip(t *testing.T) {
	t.Parallel()

	box := domain.ConfinementBox{
		WorkspaceRoot: "/ws",
		WritablePaths: []string{"/tmp/cache", "/tmp/build"},
		NetworkAllow:  []string{"example.com:443"},
	}
	enc, err := encodeBox(box)
	if err != nil {
		t.Fatalf("encodeBox: %v", err)
	}
	got, err := DecodeConfinedBox(enc)
	if err != nil {
		t.Fatalf("DecodeConfinedBox: %v", err)
	}
	if got.WorkspaceRoot != box.WorkspaceRoot ||
		len(got.WritablePaths) != len(box.WritablePaths) ||
		len(got.NetworkAllow) != len(box.NetworkAllow) {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, box)
	}
}

func TestConfinedExecSentinelAccessor(t *testing.T) {
	t.Parallel()
	if ConfinedExecSentinel() != confinedExecSentinel {
		t.Errorf("ConfinedExecSentinel() = %q, want %q", ConfinedExecSentinel(), confinedExecSentinel)
	}
}
