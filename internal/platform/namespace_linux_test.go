//go:build linux

package platform

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// namespaceBaseFlags is the fixed prefix every bwrap launch line carries before the
// network flag and the writable binds: the read-only root, the device and proc mounts, and
// the die-with-parent teardown (namespace_linux.go, namespaceArgv).
var namespaceBaseFlags = []string{"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--die-with-parent"}

// existsIn returns an exists predicate answering true for exactly the given paths, so the
// argv generator is exercised with no host state at all.
func existsIn(paths ...string) func(string) bool {
	return func(p string) bool { return slices.Contains(paths, p) }
}

func TestNamespaceArgv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		box    domain.ConfinementBox
		exists func(string) bool
		want   []string
	}{
		{
			name:   "default_box_binds_workspace_and_writable_paths",
			box:    domain.ConfinementBox{WorkspaceRoot: "/ws", WritablePaths: []string{"/ws/.cache"}},
			exists: existsIn("/ws", "/ws/.cache"),
			want: append(slices.Clone(namespaceBaseFlags),
				"--bind", "/ws", "/ws", "--bind", "/ws/.cache", "/ws/.cache"),
		},
		{
			// Network is open by default (ADR 0012); a non-empty NetworkAllow opts the box into
			// deny-all, and the flag sits between the fixed prefix and the binds.
			name:   "network_allow_unshares_net_before_binds",
			box:    domain.ConfinementBox{WorkspaceRoot: "/ws", NetworkAllow: []string{"example.com"}},
			exists: existsIn("/ws"),
			want:   append(slices.Clone(namespaceBaseFlags), "--unshare-net", "--bind", "/ws", "/ws"),
		},
		{
			// A writable path that does not exist yet is skipped rather than failing the launch —
			// bwrap refuses to bind an absent source (the ENOENT rule of allowWriteBeneath).
			name:   "missing_writable_path_is_skipped",
			box:    domain.ConfinementBox{WorkspaceRoot: "/ws", WritablePaths: []string{"/missing", "/ws/.cache"}},
			exists: existsIn("/ws", "/ws/.cache"),
			want: append(slices.Clone(namespaceBaseFlags),
				"--bind", "/ws", "/ws", "--bind", "/ws/.cache", "/ws/.cache"),
		},
		{
			// An empty WorkspaceRoot (and an empty WritablePaths entry) yields no bind at all.
			name:   "empty_workspace_root_yields_no_bind",
			box:    domain.ConfinementBox{WorkspaceRoot: "", WritablePaths: []string{"", "/ws/.cache"}},
			exists: func(string) bool { return true },
			want:   append(slices.Clone(namespaceBaseFlags), "--bind", "/ws/.cache", "/ws/.cache"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := namespaceArgv(tt.box, tt.exists)

			if !slices.Equal(got, tt.want) {
				t.Errorf("namespaceArgv =\n  %q\nwant\n  %q", got, tt.want)
			}
			for _, forbidden := range []string{"--new-session", "--unshare-pid"} {
				if slices.Contains(got, forbidden) {
					t.Errorf("namespaceArgv carries %s, which the backend must never emit", forbidden)
				}
			}
		})
	}
}

func TestNamespaceConfineRewritesCmd(t *testing.T) {
	t.Parallel()

	c := newNamespaceConfiner("/usr/bin/bwrap", "")
	box := domain.ConfinementBox{WorkspaceRoot: "/"}
	cmd := exec.Command("/bin/echo", "hello", "world")

	if err := c.Confine(context.Background(), box, cmd); err != nil {
		t.Fatalf("Confine: %v", err)
	}

	if cmd.Path != "/usr/bin/bwrap" {
		t.Errorf("cmd.Path = %q, want launcher %q", cmd.Path, "/usr/bin/bwrap")
	}
	if len(cmd.Args) == 0 || cmd.Args[0] != "/usr/bin/bwrap" {
		t.Errorf("cmd.Args[0] = %v, want launcher", cmd.Args)
	}
	if !slices.Equal(cmd.Args[1:1+len(namespaceBaseFlags)], namespaceBaseFlags) {
		t.Errorf("cmd.Args after the launcher = %q, want the fixed bwrap prefix %q", cmd.Args[1:], namespaceBaseFlags)
	}
	separator := slices.Index(cmd.Args, "--")
	if separator < 0 {
		t.Fatalf("cmd.Args = %q, has no -- separator before the original argv", cmd.Args)
	}
	if got := strings.Join(cmd.Args[separator+1:], " "); got != "/bin/echo hello world" {
		t.Errorf("original argv after -- = %q, want %q", got, "/bin/echo hello world")
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Error("Confine did not set SysProcAttr.Setpgid for process-group teardown")
	}
}

func TestNamespaceConfineForwardsResolvedPath(t *testing.T) {
	t.Parallel()

	// The confined argv must carry the RESOLVED program path (cmd.Path), not the bare
	// cmd.Args[0]: bwrap execs the command with no PATH lookup of its own, so a bare "echo"
	// would ENOENT inside the namespace.
	c := newNamespaceConfiner("/usr/bin/bwrap", "")
	cmd := exec.Command("echo", "hi") // bare name; Go resolves cmd.Path via LookPath
	resolved := cmd.Path
	if resolved == "" || !strings.HasPrefix(resolved, "/") {
		t.Skipf("echo did not resolve to an absolute path (%q); cannot exercise regression", resolved)
	}

	if err := c.Confine(context.Background(), domain.ConfinementBox{WorkspaceRoot: "/"}, cmd); err != nil {
		t.Fatalf("Confine: %v", err)
	}

	separator := slices.Index(cmd.Args, "--")
	if separator < 0 || separator+1 >= len(cmd.Args) {
		t.Fatalf("cmd.Args = %q, has no program after the -- separator", cmd.Args)
	}
	if got := cmd.Args[separator+1]; got != resolved {
		t.Errorf("confined program = %q, want resolved path %q (not bare name)", got, resolved)
	}
}

func TestNamespaceConfineRejectsEmptyArgv(t *testing.T) {
	t.Parallel()

	// Confine must refuse a cmd with no argv rather than produce a malformed launch line —
	// the deterministic guard runs before the availability check, so an unavailable backend
	// still reports "no argv" rather than ErrConfinementUnavailable.
	c := newNamespaceConfiner("", "bwrap not on PATH")
	cmd := &exec.Cmd{} // no Args

	err := c.Confine(context.Background(), domain.ConfinementBox{}, cmd)

	if err == nil {
		t.Fatal("Confine with empty argv returned nil, want error")
	}
	if got, want := err.Error(), "apogee: confine: cmd has no argv"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
	if errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Errorf("empty-argv refusal wraps ErrConfinementUnavailable; the argv guard must run first")
	}
}

func TestNamespaceConfineUnavailableIsErrConfinementUnavailable(t *testing.T) {
	t.Parallel()

	// bwrap absent => Confine returns ErrConfinementUnavailable (the "confine if you can,
	// gate if you can't" safety net) carrying the reason, so dispatch falls back to Approval
	// and the user reads why.
	c := newNamespaceConfiner("", "bwrap not on PATH")
	cmd := exec.Command("/bin/echo", "hi")

	err := c.Confine(context.Background(), domain.ConfinementBox{WorkspaceRoot: "/ws"}, cmd)

	if err == nil {
		t.Fatal("Confine on a host without bwrap returned nil, want ErrConfinementUnavailable")
	}
	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Errorf("Confine error = %v, want wrapping ErrConfinementUnavailable", err)
	}
	if !strings.Contains(err.Error(), "bwrap not on PATH") {
		t.Errorf("Confine error = %q, want it to carry the reason %q", err, "bwrap not on PATH")
	}
	if cmd.Path != "/bin/echo" {
		t.Errorf("cmd.Path = %q after a refused Confine, want it untouched", cmd.Path)
	}
}

func TestNamespaceCapabilitiesHonest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		bwrapPath       string
		unavailable     string
		wantFSWrite     bool
		wantNetwork     bool
		wantUnavailable string
	}{
		// bwrap present => one launch fences both fs-write and network egress; nothing to
		// disclose, so Unavailable stays empty on the fenceable cell.
		{"bwrap_present", "/usr/bin/bwrap", "", true, true, ""},
		// bwrap absent => deny-all caps => the disposition gates the subprocess surface (Auto
		// not refused, ADR 0012) and the reason is disclosed (contract §5).
		{"bwrap_absent", "", "bwrap not on PATH", false, false, "bwrap not on PATH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := newNamespaceConfiner(tt.bwrapPath, tt.unavailable)

			caps := c.Capabilities()

			if caps.FSWrite != tt.wantFSWrite {
				t.Errorf("FSWrite = %v, want %v", caps.FSWrite, tt.wantFSWrite)
			}
			if caps.NetworkEgress != tt.wantNetwork {
				t.Errorf("NetworkEgress = %v, want %v", caps.NetworkEgress, tt.wantNetwork)
			}
			if caps.Unavailable != tt.wantUnavailable {
				t.Errorf("Unavailable = %q, want %q", caps.Unavailable, tt.wantUnavailable)
			}
			if caps.Residuals != nil {
				t.Errorf("Residuals = %v, want nil: the namespace fence is complete or absent, never partial", caps.Residuals)
			}
			if got := caps.AutoEligible(); got != tt.wantFSWrite {
				t.Errorf("AutoEligible = %v, want %v (Auto needs fs only, ADR 0012)", got, tt.wantFSWrite)
			}
		})
	}
}
