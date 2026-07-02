//go:build !windows

package platform

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// These tests are hermetic: the seatbelt profile generator, the capability-honesty logic,
// and the Confine cmd-rewrite are all host-agnostic (seatbelt.go), so they run on every
// host — including this Linux dev env, where no real macOS / sandbox-exec exists. The live
// escape-probe that needs a real sandbox-exec child lives in seatbelt_darwin_test.go and
// is owner-run / CI-only on macOS (P3.3 acceptance).

func TestSeatbeltProfileInWorkspaceWriteAllowed(t *testing.T) {
	t.Parallel()

	box := domain.ConfinementBox{WorkspaceRoot: "/ws"}
	profile := seatbeltProfile(box)

	if !strings.Contains(profile, "(version 1)") {
		t.Errorf("profile missing version header:\n%s", profile)
	}
	if !strings.Contains(profile, "(deny file-write*)") {
		t.Errorf("profile must deny file-write by default:\n%s", profile)
	}
	if !strings.Contains(profile, `(allow file-write*`) {
		t.Errorf("profile must re-allow file-write under writable roots:\n%s", profile)
	}
	if !strings.Contains(profile, `(subpath "/ws")`) {
		t.Errorf("profile must allow writes beneath the workspace root /ws:\n%s", profile)
	}
}

func TestSeatbeltProfileOutOfWorkspaceDenied(t *testing.T) {
	t.Parallel()

	// A path outside the workspace must NOT appear in any allow-subpath clause — the
	// deny-default plus the bounded allow list is what fences out-of-box writes.
	box := domain.ConfinementBox{WorkspaceRoot: "/ws"}
	profile := seatbeltProfile(box)

	if strings.Contains(profile, `(subpath "/etc")`) || strings.Contains(profile, `(subpath "/home")`) {
		t.Errorf("profile must not allow writes outside the box:\n%s", profile)
	}
	// The deny-default clause must precede the allow clause so the allow narrows the deny.
	denyIdx := strings.Index(profile, "(deny file-write*)")
	allowIdx := strings.Index(profile, "(allow file-write*")
	if denyIdx < 0 || allowIdx < 0 || denyIdx > allowIdx {
		t.Errorf("deny-default must come before the writable-root allow:\n%s", profile)
	}
}

func TestSeatbeltProfileWritablePathsHonored(t *testing.T) {
	t.Parallel()

	box := domain.ConfinementBox{
		WorkspaceRoot: "/ws",
		WritablePaths: []string{"/cache/go-build", "/tmp/build"},
	}
	profile := seatbeltProfile(box)

	for _, want := range []string{`(subpath "/ws")`, `(subpath "/cache/go-build")`, `(subpath "/tmp/build")`} {
		if !strings.Contains(profile, want) {
			t.Errorf("profile must honour writable path clause %s:\n%s", want, profile)
		}
	}
}

func TestSeatbeltProfileSkipsEmptyRoots(t *testing.T) {
	t.Parallel()

	// An empty WorkspaceRoot / blank WritablePaths entry must not emit a (subpath "")
	// clause (which would allow writes beneath the filesystem root "").
	box := domain.ConfinementBox{WritablePaths: []string{"", "/tmp/build"}}
	profile := seatbeltProfile(box)

	if strings.Contains(profile, `(subpath "")`) {
		t.Errorf("profile must not emit an empty subpath clause:\n%s", profile)
	}
	if !strings.Contains(profile, `(subpath "/tmp/build")`) {
		t.Errorf("profile must still honour the non-empty writable path:\n%s", profile)
	}
}

func TestSeatbeltProfileCanonicalizesSymlinkedRoot(t *testing.T) {
	t.Parallel()

	// seatbelt matches a write against its kernel-canonical path, so a root reached
	// through a symlink (as on macOS, where /tmp and /var are symlinks into /private)
	// must appear RESOLVED in the profile — otherwise the (subpath ...) never matches and
	// every in-box write is wrongly denied (the v1.0.0 box-root-canonicalization bug).
	// This runs on any host: a symlinked root is constructed explicitly rather than
	// relying on the OS's tmp layout.
	tmp := t.TempDir()
	realDir := filepath.Join(tmp, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatalf("mkdir real dir: %v", err)
	}
	link := filepath.Join(tmp, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// The canonical path the kernel resolves the symlinked root to.
	want, err := filepath.EvalSymlinks(realDir)
	if err != nil {
		t.Fatalf("resolve real dir: %v", err)
	}

	box := domain.ConfinementBox{WorkspaceRoot: link}
	profile := seatbeltProfile(box)

	if !strings.Contains(profile, `(subpath "`+want+`")`) {
		t.Errorf("profile must allow writes beneath the RESOLVED root %q:\n%s", want, profile)
	}
	if strings.Contains(profile, `(subpath "`+link+`")`) {
		t.Errorf("profile must not embed the un-resolved symlinked root %q:\n%s", link, profile)
	}
}

func TestSeatbeltProfileNetworkOpenByDefault(t *testing.T) {
	t.Parallel()

	// Empty NetworkAllow => network is OPEN (ADR 0012): no deny-network clause.
	box := domain.ConfinementBox{WorkspaceRoot: "/ws"}
	profile := seatbeltProfile(box)

	if strings.Contains(profile, "(deny network") {
		t.Errorf("default box must leave the network open (no deny-network clause):\n%s", profile)
	}
}

func TestSeatbeltProfileNetworkDenyTightening(t *testing.T) {
	t.Parallel()

	// Non-empty NetworkAllow opts the box into network-deny (the coarse tightening,
	// matching landlock's deny-all-TCP).
	box := domain.ConfinementBox{WorkspaceRoot: "/ws", NetworkAllow: []string{"example.com:443"}}
	profile := seatbeltProfile(box)

	if !strings.Contains(profile, "(deny network*)") {
		t.Errorf("a box opting into network-deny must emit a deny-network clause:\n%s", profile)
	}
}

func TestSeatbeltQuoteEscapes(t *testing.T) {
	t.Parallel()

	box := domain.ConfinementBox{WorkspaceRoot: `/ws/with "quote"\and\slash`}
	profile := seatbeltProfile(box)

	if !strings.Contains(profile, `(subpath "/ws/with \"quote\"\\and\\slash")`) {
		t.Errorf("profile must escape quotes and backslashes in paths:\n%s", profile)
	}
}

func TestSeatbeltCapabilitiesHonest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		present          bool
		wantFSWrite      bool
		wantNetwork      bool
		wantAutoEligible bool
	}{
		// sandbox-exec absent => deny-all caps => fs-confinement unavailable => the
		// disposition gates the subprocess surface (Auto not refused, ADR 0012).
		{"sandbox_exec_absent", false, false, false, false},
		// sandbox-exec present => one profile enforces both fs-write and network egress.
		{"sandbox_exec_present", true, true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := newSeatbeltConfiner(tt.present)
			caps := c.Capabilities()
			if caps.FSWrite != tt.wantFSWrite {
				t.Errorf("present=%v: FSWrite = %v, want %v", tt.present, caps.FSWrite, tt.wantFSWrite)
			}
			if caps.NetworkEgress != tt.wantNetwork {
				t.Errorf("present=%v: NetworkEgress = %v, want %v", tt.present, caps.NetworkEgress, tt.wantNetwork)
			}
			// AutoEligible (P3.4) is FSWrite-only per ADR 0012; assert the contract-mandated
			// property at the cap level: fs-confinement availability tracks presence, so an
			// absent sandbox-exec must not make Auto eligible (it gates instead).
			if got := caps.FSWrite; got != tt.wantAutoEligible {
				t.Errorf("present=%v: fs-confinement availability = %v, want %v (Auto needs fs only, ADR 0012)", tt.present, got, tt.wantAutoEligible)
			}
		})
	}
}

func TestSeatbeltConfineRewritesCmd(t *testing.T) {
	t.Parallel()

	c := &seatbeltConfiner{present: true, execPath: "/path/to/sandbox-exec"}
	box := domain.ConfinementBox{WorkspaceRoot: "/ws"}
	cmd := exec.Command("/bin/echo", "hello", "world")

	if err := c.Confine(context.Background(), box, cmd); err != nil {
		t.Fatalf("Confine: %v", err)
	}

	if cmd.Path != "/path/to/sandbox-exec" {
		t.Errorf("cmd.Path = %q, want sandbox profiler %q", cmd.Path, "/path/to/sandbox-exec")
	}
	if len(cmd.Args) < 5 {
		t.Fatalf("cmd.Args = %v, too short", cmd.Args)
	}
	if cmd.Args[0] != "/path/to/sandbox-exec" {
		t.Errorf("cmd.Args[0] = %q, want profiler", cmd.Args[0])
	}
	if cmd.Args[1] != "-p" {
		t.Errorf("cmd.Args[1] = %q, want -p", cmd.Args[1])
	}
	// Args[2] is the profile string; it must be the pure function of the box.
	if cmd.Args[2] != seatbeltProfile(box) {
		t.Errorf("cmd.Args[2] is not the box's profile string:\n%s", cmd.Args[2])
	}
	gotOrig := strings.Join(cmd.Args[3:], " ")
	if gotOrig != "/bin/echo hello world" {
		t.Errorf("original argv = %q, want %q", gotOrig, "/bin/echo hello world")
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Error("Confine did not set SysProcAttr.Setpgid for process-group teardown")
	}
}

func TestSeatbeltConfineForwardsResolvedPath(t *testing.T) {
	t.Parallel()

	// Regression (P3.4 review): the profiled argv must carry the RESOLVED program
	// path (cmd.Path), not the bare cmd.Args[0], so the confined child execs the
	// same binary Go resolved (contract §2.3).
	c := &seatbeltConfiner{present: true, execPath: "/path/to/sandbox-exec"}
	cmd := exec.Command("echo", "hi") // bare name; Go resolves cmd.Path via LookPath
	resolved := cmd.Path
	if resolved == "" || !strings.HasPrefix(resolved, "/") {
		t.Skipf("echo did not resolve to an absolute path (%q); cannot exercise regression", resolved)
	}
	if err := c.Confine(context.Background(), domain.ConfinementBox{WorkspaceRoot: "/ws"}, cmd); err != nil {
		t.Fatalf("Confine: %v", err)
	}
	// Args layout: [profiler, "-p", profile, <program>, <args...>].
	if len(cmd.Args) < 5 {
		t.Fatalf("cmd.Args = %v, too short", cmd.Args)
	}
	if got := cmd.Args[3]; got != resolved {
		t.Errorf("confined program = %q, want resolved path %q (not bare name)", got, resolved)
	}
}

func TestSeatbeltConfineDefaultsProfilerPath(t *testing.T) {
	t.Parallel()

	// With no execPath override, Confine launches the stock-macOS sandbox profiler.
	c := newSeatbeltConfiner(true)
	cmd := exec.Command("/bin/echo", "hi")
	if err := c.Confine(context.Background(), domain.ConfinementBox{WorkspaceRoot: "/ws"}, cmd); err != nil {
		t.Fatalf("Confine: %v", err)
	}
	if cmd.Path != sandboxExecPath {
		t.Errorf("cmd.Path = %q, want default %q", cmd.Path, sandboxExecPath)
	}
}

func TestSeatbeltConfineUnavailableReturnsErr(t *testing.T) {
	t.Parallel()

	// sandbox-exec absent => Confine returns ErrConfinementUnavailable (the "confine if
	// you can, gate if you can't" safety net), so dispatch falls back to Approval.
	c := newSeatbeltConfiner(false)
	cmd := exec.Command("/bin/echo", "hi")
	err := c.Confine(context.Background(), domain.ConfinementBox{WorkspaceRoot: "/ws"}, cmd)
	if err == nil {
		t.Fatal("Confine on a host without sandbox-exec returned nil, want ErrConfinementUnavailable")
	}
	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Errorf("Confine error = %v, want wrapping ErrConfinementUnavailable", err)
	}
}

func TestSeatbeltConfineRejectsEmptyArgv(t *testing.T) {
	t.Parallel()

	// Confine must refuse a cmd with no argv rather than produce a malformed launch line —
	// the deterministic guard runs before the presence check.
	c := newSeatbeltConfiner(true)
	cmd := &exec.Cmd{} // no Args
	if err := c.Confine(context.Background(), domain.ConfinementBox{}, cmd); err == nil {
		t.Fatal("Confine with empty argv returned nil, want error")
	}
}
