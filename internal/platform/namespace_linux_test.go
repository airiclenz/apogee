//go:build linux

package platform

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform/confinetest"
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

	c := newNamespaceConfiner("/usr/bin/bwrap", "", "")
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
	c := newNamespaceConfiner("/usr/bin/bwrap", "", "")
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
	c := newNamespaceConfiner("", "bwrap not on PATH", domain.CauseBackendAbsent)
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
	c := newNamespaceConfiner("", "bwrap not on PATH", domain.CauseBackendAbsent)
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
		cause           domain.ConfinementCause
		wantFSWrite     bool
		wantNetwork     bool
		wantUnavailable string
		wantCause       domain.ConfinementCause
		wantResiduals   []string
	}{
		// bwrap present => one launch fences both fs-write and network egress, so Unavailable
		// stays empty on the fenceable cell — but `--unshare-net` cannot reach a PATHNAME
		// AF_UNIX socket, which the read-only root carries into the box, so that one access is
		// disclosed by syscall and nothing else is.
		{"bwrap_present", "/usr/bin/bwrap", "", "", true, true, "", "", []string{domain.ResidualUnixEgress}},
		// bwrap absent => deny-all caps => the disposition gates the subprocess surface (Auto
		// not refused, ADR 0012) and the reason is disclosed (contract §5) in both forms.
		// Nothing is fenced, so nothing is residual either: a residual is an admitted gap in a
		// fence that exists.
		{"bwrap_absent", "", "bwrap not on PATH", domain.CauseBackendAbsent, false, false, "bwrap not on PATH", domain.CauseBackendAbsent, nil},
		// A probe that outlived its budget reaches the caps as its OWN cause: the sentence and
		// the token both say the probe never answered, which is not the same host fact as a
		// bwrap that is not installed.
		{"probe_timed_out", "", "bwrap timed out", domain.CauseProbeTimedOut, false, false, "bwrap timed out", domain.CauseProbeTimedOut, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := newNamespaceConfiner(tt.bwrapPath, tt.unavailable, tt.cause)

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
			if caps.Cause != tt.wantCause {
				t.Errorf("Cause = %q, want %q: the typed cause is what a caller branches on, "+
					"and a fenceable backend carries none", caps.Cause, tt.wantCause)
			}
			if !slices.Equal(caps.Residuals, tt.wantResiduals) {
				t.Errorf("Residuals = %v, want %v: a fenceable box still passes pathname AF_UNIX egress, "+
					"and an absent backend admits no gap at all", caps.Residuals, tt.wantResiduals)
			}
			if got := caps.AutoEligible(); got != tt.wantFSWrite {
				t.Errorf("AutoEligible = %v, want %v (Auto needs fs only, ADR 0012)", got, tt.wantFSWrite)
			}
		})
	}
}

// TestNamespaceProbe drives the shared escape battery (confinement-execution-contract §6.2
// rows #1–#6, #11, #12) against the namespace backend as this host constructs it. The
// harness skips itself when the construction probe reported FSWrite==false (no bwrap, or a
// kernel that refuses unprivileged user namespaces), so a CI container without userns
// skips cleanly; wherever bwrap and userns work — landlock or not — every row runs for
// real. Row #12 must DENY with the bytes intact: the read-only root fences truncate(2), and
// the only access this backend discloses as residual is network-class, not write-class. Row
// #11 passes only because the kill-on-denial signature matches the EROFS the read-only root
// answers — the journey test for the announced fence.
func TestNamespaceProbe(t *testing.T) {
	// Not parallel: the confined children are real subprocesses.
	confinetest.Probe(t, NewNamespaceConfiner(), Current(), FailFastPreamble(), newProbeDenialKiller)
}

// TestNamespaceProbeNetwork drives the network arm (rows #7–#8 and #13): `--unshare-net` on a
// network-deny box, an open network on the default box, and one real datagram sent out of the
// deny box — which the listener must never receive here, because this backend discloses
// connect(2) AF_UNIX alone and `--unshare-net` cuts datagram egress with the rest.
func TestNamespaceProbeNetwork(t *testing.T) {
	confinetest.ProbeNetwork(t, NewNamespaceConfiner(), Current())
}

// stubLauncher writes an executable shell script named bwrap under a fresh temp dir and
// returns its path, so probeNamespace is exercised against a launcher whose verdict the
// test controls rather than the host's real bwrap.
func stubLauncher(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bwrap")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatalf("write stub launcher: %v", err)
	}
	return path
}

func TestNamespaceProbeReasonNamesTheCause(t *testing.T) {
	// Not parallel: the PATH case uses t.Setenv.

	t.Run("refused_launch_carries_the_launcher_line", func(t *testing.T) {
		// A kernel that refuses CLONE_NEWUSER surfaces as bwrap's own diagnostic on stderr
		// and a non-zero exit; the reason must carry that line so the probe surfaces say
		// what refused, not just that something did.
		const diagnostic = "bwrap: setting up uid map: Permission denied"
		launcher := stubLauncher(t, "echo '"+diagnostic+"' >&2\nexit 1\n")

		reason, cause := probeNamespace(launcher)

		if !strings.Contains(reason, diagnostic) {
			t.Errorf("reason = %q, want it to contain %q", reason, diagnostic)
		}
		if !strings.HasPrefix(reason, "bwrap refused: ") {
			t.Errorf("reason = %q, want the %q prefix", reason, "bwrap refused: ")
		}
		if cause != domain.CauseLaunchRefused {
			t.Errorf("cause = %q, want %q: the launcher ran and said no", cause, domain.CauseLaunchRefused)
		}
	})

	t.Run("successful_launch_is_fenceable", func(t *testing.T) {
		launcher := stubLauncher(t, "exit 0\n")

		reason, cause := probeNamespace(launcher)

		if reason != "" {
			t.Errorf("reason = %q, want \"\" for a launcher that exits 0", reason)
		}
		if cause != "" {
			t.Errorf("cause = %q, want \"\" for a launcher that exits 0", cause)
		}
	})

	t.Run("silent_refusal_falls_back_to_the_exit_error", func(t *testing.T) {
		// A launcher that fails without a word still yields a reason that says something.
		launcher := stubLauncher(t, "exit 3\n")

		reason, cause := probeNamespace(launcher)

		if want := "bwrap refused: exit status 3"; reason != want {
			t.Errorf("reason = %q, want %q", reason, want)
		}
		if cause != domain.CauseLaunchRefused {
			t.Errorf("cause = %q, want %q: a wordless non-zero exit is still a refusal", cause, domain.CauseLaunchRefused)
		}
	})

	t.Run("a_launcher_that_outlives_the_budget_times_out", func(t *testing.T) {
		// The cold, loaded box the production budget was raised for, driven at a budget a test
		// can afford: a launcher that never returns must classify as a probe that did not
		// ANSWER, never as one that refused — the whole point of the typed cause is that a
		// caller can tell "this host cannot fence" from "this host did not say in time".
		// `exec` so the sleep REPLACES the shell rather than being its child: the probe
		// captures stderr through a pipe, and a surviving grandchild holding the write end
		// would make Wait pay the whole ProcessWaitDelay drain (5 s) on every suite run.
		launcher := stubLauncher(t, "exec sleep 30\n")
		restore := namespaceProbeTimeout
		namespaceProbeTimeout = 50 * time.Millisecond
		t.Cleanup(func() { namespaceProbeTimeout = restore })

		reason, cause := probeNamespace(launcher)

		if want := "bwrap timed out"; reason != want {
			t.Errorf("reason = %q, want %q", reason, want)
		}
		if cause != domain.CauseProbeTimedOut {
			t.Errorf("cause = %q, want %q: a launcher that never answered did not refuse", cause, domain.CauseProbeTimedOut)
		}
	})

	t.Run("bwrap_absent_from_path", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())

		c := NewNamespaceConfiner()

		caps := c.Capabilities()
		if caps.FSWrite || caps.NetworkEgress {
			t.Errorf("Capabilities = %+v, want neither cell when bwrap is absent", caps)
		}
		if caps.Unavailable != "bwrap not on PATH" {
			t.Errorf("Unavailable = %q, want %q", caps.Unavailable, "bwrap not on PATH")
		}
		if caps.Cause != domain.CauseBackendAbsent {
			t.Errorf("Cause = %q, want %q: nothing ran, so nothing refused or timed out",
				caps.Cause, domain.CauseBackendAbsent)
		}
	})
}
