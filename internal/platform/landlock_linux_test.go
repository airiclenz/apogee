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
	confinetest.Probe(t, newTestConfiner(t))
}

func TestLandlockProbeNetwork(t *testing.T) {
	confinetest.ProbeNetwork(t, newTestConfiner(t))
}

func TestLandlockCapabilitiesHonest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		abi              int
		wantFSWrite      bool
		wantNetwork      bool
		wantAutoEligible bool
	}{
		{"no_landlock", -1, false, false, false},
		{"abi1_kernel_5_13", 1, true, false, true}, // fs-only; AutoEligible on fs alone (ADR 0012)
		{"abi3_kernel_6_2", 3, true, false, true},  // still fs-only, still Auto-eligible
		{"abi4_kernel_6_7", 4, true, true, true},   // network egress now enforceable
		{"abi6_newer", 6, true, true, true},
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
		})
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
	if err := c.Confine(context.Background(), domain.ConfinementBox{}, cmd); err == nil {
		t.Fatal("Confine with empty argv returned nil, want error")
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
