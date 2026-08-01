//go:build linux

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	confinetest.Probe(t, newTestConfiner(t), Current())
}

func TestLandlockProbeNetwork(t *testing.T) {
	confinetest.ProbeNetwork(t, newTestConfiner(t), Current())
}

func TestLandlockCapabilitiesHonest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		abi              int
		wantFSWrite      bool
		wantNetwork      bool
		wantAutoEligible bool
		wantAccess       uint64 // mask applyLandlock would request; asserted only when wantFSWrite
	}{
		{"no_landlock", -1, false, false, false, 0},
		// fs-only; AutoEligible on fs alone (ADR 0012). Its mask must stay at the ABI-1
		// baseline: asking for TRUNCATE (ABI 3) here was EINVAL on every create_ruleset.
		{"abi1_kernel_5_13", 1, true, false, true, baselineFSWriteAccess},
		// Debian 12 (6.1). REFER exists from here, and must be handled or the kernel denies
		// cross-directory rename/link outright — including inside the workspace.
		{"abi2_kernel_5_19", 2, true, false, true, baselineFSWriteAccess | unix.LANDLOCK_ACCESS_FS_REFER},
		// still fs-only, still Auto-eligible; TRUNCATE is finally requestable.
		{"abi3_kernel_6_2", 3, true, false, true, fullFSWriteAccess},
		{"abi4_kernel_6_7", 4, true, true, true, fullFSWriteAccess}, // network egress now enforceable
		{"abi6_newer", 6, true, true, true, fullFSWriteAccess},
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
	tests := []struct {
		name string
		abi  int
		want uint64
	}{
		// Not a valid input (applyLandlock refuses below the floor); pinned so the clamp can
		// never become a mask that handles nothing — a ruleset fencing no write at all.
		{"below_floor_clamps_to_baseline", -1, baselineFSWriteAccess},
		{"abi1_kernel_5_13", 1, baselineFSWriteAccess},
		{"abi2_kernel_5_19", 2, baselineFSWriteAccess | unix.LANDLOCK_ACCESS_FS_REFER},
		{"abi3_kernel_6_2", 3, fullFSWriteAccess},
		{"abi4_kernel_6_7", 4, fullFSWriteAccess},
		{"abi6_newer", 6, fullFSWriteAccess},
		{"abi99_future", 99, fullFSWriteAccess},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := accessMaskForABI(tt.abi); got != tt.want {
				t.Errorf("accessMaskForABI(%d) = %#x, want %#x", tt.abi, got, tt.want)
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
