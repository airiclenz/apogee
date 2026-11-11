//go:build linux

package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform/confinetest"
	"github.com/airiclenz/apogee/internal/testtime"
)

// namespaceBaseFlags is the fixed prefix every bwrap launch line carries before the
// network flag and the writable binds: the read-only root, the device and proc mounts, the
// die-with-parent teardown and the PID namespace (namespace_linux.go, namespaceArgv).
var namespaceBaseFlags = []string{"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--die-with-parent", "--unshare-pid"}

// bwrapAbsentReason is the reason a host with no bwrap in any trusted directory discloses: the
// four directories of bwrapCandidates, in order, and never PATH.
const bwrapAbsentReason = "bwrap not found in /usr/bin, /bin, /usr/local/bin or /run/current-system/sw/bin"

// existsIn returns an exists predicate answering true for exactly the given paths, so the
// argv generator is exercised with no host state at all.
func existsIn(paths ...string) func(string) bool {
	return func(p string) bool { return slices.Contains(paths, p) }
}

func TestNamespaceArgv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		box     domain.ConfinementBox
		exists  func(string) bool
		sockets unixSocketHost
		want    []string
	}{
		{
			name:    "default_box_binds_workspace_and_writable_paths",
			box:     domain.ConfinementBox{WorkspaceRoot: "/ws", WritablePaths: []string{"/ws/.cache"}},
			exists:  existsIn("/ws", "/ws/.cache"),
			sockets: noUnixSockets,
			want: append(slices.Clone(namespaceBaseFlags),
				"--bind", "/ws", "/ws", "--bind", "/ws/.cache", "/ws/.cache"),
		},
		{
			// Network is open by default (ADR 0012); a non-empty NetworkAllow opts the box into
			// deny-all, and the flag sits between the fixed prefix and the binds.
			name:    "network_allow_unshares_net_before_binds",
			box:     domain.ConfinementBox{WorkspaceRoot: "/ws", NetworkAllow: []string{"example.com"}},
			exists:  existsIn("/ws"),
			sockets: noUnixSockets,
			want:    append(slices.Clone(namespaceBaseFlags), "--unshare-net", "--bind", "/ws", "/ws"),
		},
		{
			// A writable path that does not exist yet is skipped rather than failing the launch —
			// bwrap refuses to bind an absent source (the ENOENT rule of allowWriteBeneath).
			name:    "missing_writable_path_is_skipped",
			box:     domain.ConfinementBox{WorkspaceRoot: "/ws", WritablePaths: []string{"/missing", "/ws/.cache"}},
			exists:  existsIn("/ws", "/ws/.cache"),
			sockets: noUnixSockets,
			want: append(slices.Clone(namespaceBaseFlags),
				"--bind", "/ws", "/ws", "--bind", "/ws/.cache", "/ws/.cache"),
		},
		{
			// An empty WorkspaceRoot (and an empty WritablePaths entry) yields no bind at all.
			name:    "empty_workspace_root_yields_no_bind",
			box:     domain.ConfinementBox{WorkspaceRoot: "", WritablePaths: []string{"", "/ws/.cache"}},
			exists:  func(string) bool { return true },
			sockets: noUnixSockets,
			want:    append(slices.Clone(namespaceBaseFlags), "--bind", "/ws/.cache", "/ws/.cache"),
		},
		{
			// The user runtime dir is masked with an empty tmpfs AFTER the writable binds (ADR
			// 0112), so no writable root can bind it back over the mask.
			name:   "runtime_dir_is_masked_after_the_binds",
			box:    domain.ConfinementBox{WorkspaceRoot: "/ws"},
			exists: existsIn("/ws"),
			sockets: fakeUnixSocketHost(nil, "/home/u", "/tmp",
				map[string]fakePath{"/run/user/1000": runtimeDir}),
			want: append(slices.Clone(namespaceBaseFlags),
				"--bind", "/ws", "/ws", "--tmpfs", "/run/user/1000"),
		},
		{
			// A writable root that CONTAINS the runtime dir and the docker socket still leaves
			// both masked: the masks come after its bind. The agent sockets beneath the mask are
			// bound back last, on top of it.
			name:   "container_sockets_covered_and_agents_rebound_over_the_mask",
			box:    domain.ConfinementBox{WorkspaceRoot: "/ws", WritablePaths: []string{"/run"}},
			exists: existsIn("/ws", "/run"),
			sockets: fakeUnixSocketHost(map[string]string{"SSH_AUTH_SOCK": "/run/user/1000/keyring/ssh"}, "/home/u", "/tmp",
				map[string]fakePath{
					"/run/user/1000":             runtimeDir,
					"/run/user/1000/keyring/ssh": socketFile,
					"/run/user/1000/gnupg":       runtimeDir,
					"/run/docker.sock":           socketFile,
				}),
			want: append(slices.Clone(namespaceBaseFlags),
				"--bind", "/ws", "/ws", "--bind", "/run", "/run",
				"--tmpfs", "/run/user/1000",
				"--ro-bind", "/dev/null", "/run/docker.sock",
				"--ro-bind", "/run/user/1000/keyring/ssh", "/run/user/1000/keyring/ssh",
				"--ro-bind", "/run/user/1000/gnupg", "/run/user/1000/gnupg"),
		},
		{
			// $SSH_AUTH_SOCK names a link beneath the mask: the canonical socket is bound back
			// and the link recreated over the tmpfs, so the env-named path resolves in the box.
			name:   "agent_link_beneath_the_mask_is_recreated",
			box:    domain.ConfinementBox{WorkspaceRoot: "/ws"},
			exists: existsIn("/ws"),
			sockets: fakeUnixSocketHost(map[string]string{"SSH_AUTH_SOCK": "/run/user/1000/ssh"}, "/home/u", "/tmp",
				map[string]fakePath{
					"/run/user/1000":             runtimeDir,
					"/run/user/1000/ssh":         {canonical: "/run/user/1000/keyring/ssh", link: "keyring/ssh"},
					"/run/user/1000/keyring/ssh": socketFile,
				}),
			want: append(slices.Clone(namespaceBaseFlags),
				"--bind", "/ws", "/ws",
				"--tmpfs", "/run/user/1000",
				"--ro-bind", "/run/user/1000/keyring/ssh", "/run/user/1000/keyring/ssh",
				"--symlink", "keyring/ssh", "/run/user/1000/ssh"),
		},
		{
			// A socket path that does not exist yields no flag: bwrap refuses to bind an absent
			// source, and an absent socket needs no cover. Here podman, the agent socket and the
			// gnupg dir are all missing; only the docker socket exists.
			name:   "missing_socket_paths_are_skipped",
			box:    domain.ConfinementBox{WorkspaceRoot: "/ws"},
			exists: existsIn("/ws"),
			sockets: fakeUnixSocketHost(map[string]string{"SSH_AUTH_SOCK": "/run/user/1000/gone"}, "/home/u", "/tmp",
				map[string]fakePath{
					"/run/user/1000":   runtimeDir,
					"/run/docker.sock": socketFile,
				}),
			want: append(slices.Clone(namespaceBaseFlags),
				"--bind", "/ws", "/ws",
				"--tmpfs", "/run/user/1000",
				"--ro-bind", "/dev/null", "/run/docker.sock"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := namespaceArgv(tt.box, tt.exists, tt.sockets)

			if !slices.Equal(got, tt.want) {
				t.Errorf("namespaceArgv =\n  %q\nwant\n  %q", got, tt.want)
			}
			if slices.Contains(got, "--new-session") {
				t.Error("namespaceArgv carries --new-session, which the backend must never emit")
			}
			// The PID namespace is what keeps the host's process table — and the
			// /proc/<pid>/root escape it opens — out of the box, so every box carries it.
			if !slices.Contains(got, "--unshare-pid") {
				t.Error("namespaceArgv lacks --unshare-pid; the child would see the host's processes")
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
	c := newNamespaceConfiner("", bwrapAbsentReason, domain.CauseBackendAbsent)
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
	c := newNamespaceConfiner("", bwrapAbsentReason, domain.CauseBackendAbsent)
	cmd := exec.Command("/bin/echo", "hi")

	err := c.Confine(context.Background(), domain.ConfinementBox{WorkspaceRoot: "/ws"}, cmd)

	if err == nil {
		t.Fatal("Confine on a host without bwrap returned nil, want ErrConfinementUnavailable")
	}
	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Errorf("Confine error = %v, want wrapping ErrConfinementUnavailable", err)
	}
	if !strings.Contains(err.Error(), bwrapAbsentReason) {
		t.Errorf("Confine error = %q, want it to carry the reason %q", err, bwrapAbsentReason)
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
		// AF_UNIX socket, which the read-only root carries into the box, so that one
		// write-class access (ADR 0112) is disclosed by syscall and nothing else is.
		{"bwrap_present", "/usr/bin/bwrap", "", "", true, true, "", "", []string{domain.ResidualUnixEgress}},
		// bwrap absent => deny-all caps => the disposition gates the subprocess surface (Auto
		// not refused, ADR 0012) and the reason is disclosed (contract §5) in both forms.
		// Nothing is fenced, so nothing is residual either: a residual is an admitted gap in a
		// fence that exists.
		{"bwrap_absent", "", bwrapAbsentReason, domain.CauseBackendAbsent, false, false, bwrapAbsentReason, domain.CauseBackendAbsent, nil},
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
				t.Errorf("Residuals = %v, want %v: a fenceable box still passes a pathname AF_UNIX connect, "+
					"and an absent backend admits no gap at all", caps.Residuals, tt.wantResiduals)
			}
			if got := caps.AutoEligible(); got != tt.wantFSWrite {
				t.Errorf("AutoEligible = %v, want %v (Auto needs fs only, ADR 0012)", got, tt.wantFSWrite)
			}
		})
	}
}

// TestNamespaceProbe drives the shared escape battery (confinement-execution-contract §6.2
// rows #1–#6, #11, #12, #14 and #15) against the namespace backend as this host constructs it. The
// harness skips itself when the construction probe reported FSWrite==false (no bwrap, or a
// kernel that refuses unprivileged user namespaces), so a CI container without userns
// skips cleanly; wherever bwrap and userns work — landlock or not — every row runs for
// real. Row #12 must DENY with the bytes intact: the read-only root fences truncate(2), and
// the only access this backend discloses as residual is connect(2) AF_UNIX, never truncate(2).
// Row #11 passes only because the kill-on-denial signature matches the EROFS the read-only
// root answers — the journey test for the announced fence.
func TestNamespaceProbe(t *testing.T) {
	// Not parallel: the confined children are real subprocesses.
	confinetest.Probe(t, NewNamespaceConfiner(), Current(), FailFastPreamble(), newProbeDenialKiller, newProbeMergedDenialKiller)
}

// TestNamespaceProbeNetwork drives the network arm (rows #7–#8 and #13): `--unshare-net` on a
// network-deny box, an open network on the default box, and one real datagram sent out of the
// deny box — which the listener must never receive here, because this backend discloses
// connect(2) AF_UNIX alone and `--unshare-net` cuts datagram egress with the rest.
func TestNamespaceProbeNetwork(t *testing.T) {
	confinetest.ProbeNetwork(t, NewNamespaceConfiner(), Current())
}

// fencingNamespaceConfiner returns the namespace backend as this host constructs it, skipping
// the test where it cannot fence (no trusted bwrap, or a kernel that refuses it the namespaces),
// the same rule the confinetest harness applies.
func fencingNamespaceConfiner(t *testing.T) *namespaceConfiner {
	t.Helper()
	c := NewNamespaceConfiner()
	if caps := c.Capabilities(); !caps.FSWrite {
		t.Skipf("namespace backend cannot fence on this host: %s", caps.Unavailable)
	}
	return c
}

// hostStatusName returns the Name: line of the host's /proc/<pid>/status, the line a child
// reading that file would see if the host's process table reached it.
func hostStatusName(t *testing.T, pid int) string {
	t.Helper()
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		t.Fatalf("read host status of PID %d: %v", pid, err)
	}
	for line := range strings.SplitSeq(string(status), "\n") {
		if strings.HasPrefix(line, "Name:") {
			return line
		}
	}
	t.Fatalf("host status of PID %d has no Name: line", pid)
	return ""
}

// TestNamespaceHidesHostProcesses pins the PID namespace (`--unshare-pid`) from inside the box:
// the child's procfs must not reach this test process (standing in for apogee), so it can
// neither read its status nor write outside the box through `/proc/<pid>/root`, which resolves
// against the host's unconfined mount namespace.
func TestNamespaceHidesHostProcesses(t *testing.T) {
	// Not parallel: the confined child is a real subprocess.
	c := fencingNamespaceConfiner(t)

	box := domain.ConfinementBox{WorkspaceRoot: t.TempDir()}
	outside := filepath.Join(t.TempDir(), "escaped")
	hostProc := fmt.Sprintf("/proc/%d", os.Getpid())
	hostName := hostStatusName(t, os.Getpid())

	// Each step's failure is the expected outcome, so the line always exits 0: what the test
	// reads is the file the write left and the status the read printed.
	line := fmt.Sprintf("printf x > %s 2>/dev/null; cat %s 2>/dev/null; exit 0",
		posixQuote(hostProc+"/root"+outside), posixQuote(hostProc+"/status"))
	cmd := exec.Command("/bin/sh", "-c", line)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := c.Confine(context.Background(), box, cmd); err != nil {
		t.Fatalf("Confine: %v", err)
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("confined run: %v", err)
	}

	if _, err := os.Lstat(outside); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("confined child wrote %s through %s/root (Lstat err = %v); the host process table reaches the box", outside, hostProc, err)
	}
	if strings.Contains(stdout.String(), hostName) {
		t.Errorf("confined child read %s/status and saw the host process (%q); the box shares the host PID namespace", hostProc, hostName)
	}
}

// unixAcceptWait is how long a test listener waits for a confined connect once the confined
// client has exited. A connect that got through is already queued by then, so the window only
// covers scheduling — and it is the exact cost of the negative case, which always waits it out.
var unixAcceptWait = testtime.Fixed(2 * time.Second)

// listenUnix binds a unix-socket listener at path for the rest of the test.
func listenUnix(t *testing.T, path string) *net.UnixListener {
	t.Helper()
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen unix %q: %v", path, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// acceptedWithin reports whether a connect reached ln within unixAcceptWait, failing the test
// on any accept error other than the deadline expiring.
func acceptedWithin(t *testing.T, ln *net.UnixListener) bool {
	t.Helper()
	if err := ln.SetDeadline(time.Now().Add(unixAcceptWait)); err != nil {
		t.Fatalf("set accept deadline: %v", err)
	}
	conn, err := ln.Accept()
	if err == nil {
		_ = conn.Close()
		return true
	}
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("accept on %s: %v", ln.Addr(), err)
	}
	return false
}

// TestNamespaceMasksRuntimeDirSockets pins the runtime-dir mask from inside the box (ADR 0112):
// a same-user service listening under $XDG_RUNTIME_DIR — the session bus's place — is out of a
// confined command's reach, while the ssh-agent socket $SSH_AUTH_SOCK names beneath that dir
// stays reachable. Both listeners are bound by this unconfined test and are the witnesses, as in
// the battery's row #15, whose in-box client this test reuses. The mask is planned from the
// environment at Confine time, so t.Setenv points it at a runtime dir the test owns.
func TestNamespaceMasksRuntimeDirSockets(t *testing.T) {
	// Not parallel: t.Setenv, and the confined clients are real subprocesses.
	c := fencingNamespaceConfiner(t)

	runtimeDir := maskableRuntimeDirForTest(t)
	bus := filepath.Join(runtimeDir, "bus")
	agent := filepath.Join(runtimeDir, "agent")
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("SSH_AUTH_SOCK", agent)
	busLn := listenUnix(t, bus)
	agentLn := listenUnix(t, agent)
	box := domain.ConfinementBox{WorkspaceRoot: t.TempDir()}

	output, runErr := confinetest.UnixConnectFromBox(t, c, box, agent)
	if !acceptedWithin(t, agentLn) {
		t.Errorf("no confined connect reached $SSH_AUTH_SOCK at %s (client: %v, output:\n%s); "+
			"the agent socket beneath the masked runtime dir must stay reachable", agent, runErr, output)
	}

	output, runErr = confinetest.UnixConnectFromBox(t, c, box, bus)
	if acceptedWithin(t, busLn) {
		t.Errorf("a confined connect reached %s under the masked $XDG_RUNTIME_DIR (client: %v, output:\n%s); "+
			"the runtime dir must be masked", bus, runErr, output)
	}
}

// TestNamespaceMasksContainerSockets pins the container-socket mask from inside the box (ADR
// 0112): a confined connect to a container socket the plan covers reaches no listener, while the
// same connect from a box that does not cover that path is accepted. The production candidates
// (/run/docker.sock, /run/podman/podman.sock) are not the test's to bind, so the masked arm's
// confiner plans from a host whose only candidate is a listener this unconfined test owns; the
// control arm keeps the production candidates, which leave that listener reachable.
func TestNamespaceMasksContainerSockets(t *testing.T) {
	// Not parallel: the confined clients are real subprocesses.
	control := fencingNamespaceConfiner(t)

	// os.MkdirTemp, not t.TempDir(): a test-named path can outgrow sun_path's 108 bytes.
	dir, err := os.MkdirTemp("", "cs")
	if err != nil {
		t.Fatalf("socket dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "docker.sock")
	ln := listenUnix(t, sock)
	box := domain.ConfinementBox{WorkspaceRoot: t.TempDir()}

	masked := *control
	masked.socketHost = func() unixSocketHost {
		host := currentUnixSocketHost()
		host.containerSockets = []string{sock}
		return host
	}
	output, runErr := confinetest.UnixConnectFromBox(t, &masked, box, sock)
	if acceptedWithin(t, ln) {
		t.Errorf("a confined connect reached the container socket %s (client: %v, output:\n%s); "+
			"a container socket the plan covers must be masked", sock, runErr, output)
	}
	// The mask binds /dev/null over the socket, and connect(2) to a path that is not a socket
	// is refused: the refusal shows the client ran in the box and met the mask, so a box that
	// failed to launch cannot pass for one that masked.
	if !strings.Contains(output, "connection refused") {
		t.Errorf("the confined client was not refused at the masked container socket %s (client: %v, output:\n%s)",
			sock, runErr, output)
	}

	output, runErr = confinetest.UnixConnectFromBox(t, control, box, sock)
	if !acceptedWithin(t, ln) {
		t.Errorf("no confined connect reached %s from a box that does not cover it (client: %v, output:\n%s); "+
			"the control arm must reach the listener the masked arm does not", sock, runErr, output)
	}
}

// TestNamespaceResolvesSymlinkedAgentSocket pins the runtime-dir mask's agent re-exposure for a
// $SSH_AUTH_SOCK that names a symlink beneath the masked dir, as a keyring daemon sets it: the
// path the environment names must resolve inside the box, not only its canonical target, or a
// confined `git push` over ssh finds no agent. The listener is bound by this unconfined test.
func TestNamespaceResolvesSymlinkedAgentSocket(t *testing.T) {
	// Not parallel: t.Setenv, and the confined client is a real subprocess.
	c := fencingNamespaceConfiner(t)

	runtimeDir := maskableRuntimeDirForTest(t)
	agent := filepath.Join(runtimeDir, "keyring", "ssh")
	if err := os.Mkdir(filepath.Dir(agent), 0o700); err != nil {
		t.Fatalf("agent dir: %v", err)
	}
	named := filepath.Join(runtimeDir, "ssh")
	if err := os.Symlink("keyring/ssh", named); err != nil {
		t.Fatalf("agent link: %v", err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("SSH_AUTH_SOCK", named)
	agentLn := listenUnix(t, agent)
	box := domain.ConfinementBox{WorkspaceRoot: t.TempDir()}

	output, runErr := confinetest.UnixConnectFromBox(t, c, box, named)
	if !acceptedWithin(t, agentLn) {
		t.Errorf("no confined connect through $SSH_AUTH_SOCK=%s reached the agent (client: %v, output:\n%s); "+
			"the path the environment names must resolve beneath the masked runtime dir", named, runErr, output)
	}
}

// maskableRuntimeDirForTest returns a fresh directory the runtime-dir mask accepts — owned by
// this uid, mode 0700, outside every box root — removed when t ends. os.MkdirTemp, not
// t.TempDir(): a test-named path can outgrow sun_path's 108 bytes.
func maskableRuntimeDirForTest(t *testing.T) string {
	t.Helper()
	runtimeDir, err := os.MkdirTemp("", "rt")
	if err != nil {
		t.Fatalf("runtime dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	// The XDG spec's mode, which the mask requires; MkdirTemp's 0700 is narrowed by the umask.
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		t.Fatalf("chmod runtime dir: %v", err)
	}
	return runtimeDir
}

// hostPIDsWithArg returns the PIDs whose host /proc/<pid>/cmdline carries arg as one argument.
// The scan runs on the host side, so it finds a process inside the box by the PID the host
// knows it by, never by one the box reports.
func hostPIDsWithArg(arg string) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil {
			continue
		}
		if slices.Contains(strings.Split(string(cmdline), "\x00"), arg) {
			pids = append(pids, pid)
		}
	}
	return pids
}

// TestNamespaceTeardownLeavesNoSurvivor pins §2.4 teardown on this backend: a confined run's
// whole tree dies with it, including a descendant that called setsid to leave the run's
// process group — the POSIX residual TestTeardownDoesNotReachASetsidEscapee pins for an
// unconfined run does not hold here, because the PID namespace dies with its pid 1. Each
// backgrounded sleep carries a unique duration as its marker, and survivors are looked for in
// the host's own /proc.
func TestNamespaceTeardownLeavesNoSurvivor(t *testing.T) {
	// Not parallel: the confined children are real subprocesses.
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skipf("setsid not on PATH (%v); the escapee it kills needs the tool to detach with", err)
	}
	c := fencingNamespaceConfiner(t)

	// marker returns a sleep duration no other process on the host carries: ten minutes plus a
	// fraction made of this process's PID, the clock and n.
	stamp := time.Now().UnixNano() % 1_000_000_000
	marker := func(n int) string { return fmt.Sprintf("600.%d%09d%d", os.Getpid(), stamp, n) }

	// runConfinedTree starts line confined under the §2.4 seam exactly as a spawner does, waits
	// until every marker is alive on the host, then lets the run end through end — and returns
	// once the run has. The markers' processes are SIGKILLed on cleanup whatever the outcome.
	runConfinedTree := func(t *testing.T, line string, markers []string, end func(cancel context.CancelFunc)) error {
		t.Helper()
		t.Cleanup(func() {
			for _, m := range markers {
				for _, pid := range hostPIDsWithArg(m) {
					killPID(pid)
				}
			}
		})

		ctx, cancel := context.WithTimeout(context.Background(), escapeeRunTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", line)
		td := NewProcessTeardown(cmd)
		if err := c.Confine(ctx, domain.ConfinementBox{WorkspaceRoot: t.TempDir()}, cmd); err != nil {
			t.Fatalf("Confine: %v", err)
		}
		done := make(chan error, 1)
		go func() { done <- RunWithTeardown(cmd, td) }()

		testtime.Eventually(t, testtime.Budget(20*time.Second), func() bool {
			for _, m := range markers {
				if len(hostPIDsWithArg(m)) == 0 {
					return false
				}
			}
			return true
		}, "every confined sleep to be running")
		end(cancel)
		return <-done
	}

	assertNoSurvivor := func(t *testing.T, markers []string) {
		t.Helper()
		testtime.Eventually(t, testtime.Budget(20*time.Second), func() bool {
			for _, m := range markers {
				if len(hostPIDsWithArg(m)) > 0 {
					return false
				}
			}
			return true
		}, "every confined sleep to be gone after teardown")
	}

	t.Run("a cancelled run leaves no survivor", func(t *testing.T) {
		markers := []string{marker(1), marker(2), marker(3)}
		line := fmt.Sprintf("sleep %s & setsid sleep %s >/dev/null 2>&1 </dev/null & sleep %s",
			markers[0], markers[1], markers[2])

		// Cancelling a run is a result, not a Go error the assertion needs: what this test
		// reads is what the kill reached, not what the run reported.
		_ = runConfinedTree(t, line, markers, func(cancel context.CancelFunc) { cancel() })

		assertNoSurvivor(t, markers)
	})

	t.Run("a cleanly exited run leaves no survivor", func(t *testing.T) {
		markers := []string{marker(4), marker(5)}
		release := filepath.Join(t.TempDir(), "release")
		// The leader waits for the release file so both sleeps are up before it exits; the box
		// reads the host filesystem, so the file the test creates is visible inside.
		line := fmt.Sprintf("sleep %s >/dev/null 2>&1 & setsid sleep %s >/dev/null 2>&1 </dev/null &\n"+
			"until [ -e %s ]; do sleep 0.05; done",
			markers[0], markers[1], posixQuote(release))

		err := runConfinedTree(t, line, markers, func(context.CancelFunc) {
			if err := os.WriteFile(release, nil, 0o600); err != nil {
				t.Fatalf("write release file: %v", err)
			}
		})
		if err != nil {
			t.Fatalf("RunWithTeardown err = %v, want nil — the leader exits cleanly", err)
		}

		assertNoSurvivor(t, markers)
	})
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

	t.Run("candidates_absent_names_the_trusted_dirs", func(t *testing.T) {
		// The production list, with nothing executable in it and nothing on PATH either: the
		// reason names every directory apogee looked in, so the user knows where to install.
		c := newProbedNamespaceConfiner(bwrapCandidates, func(string) bool { return false }, lookNothing)

		caps := c.Capabilities()
		if caps.FSWrite || caps.NetworkEgress {
			t.Errorf("Capabilities = %+v, want neither cell when bwrap is absent", caps)
		}
		if caps.Unavailable != bwrapAbsentReason {
			t.Errorf("Unavailable = %q, want %q", caps.Unavailable, bwrapAbsentReason)
		}
		if caps.Cause != domain.CauseBackendAbsent {
			t.Errorf("Cause = %q, want %q: nothing ran, so nothing refused or timed out",
				caps.Cause, domain.CauseBackendAbsent)
		}
	})

	t.Run("bwrap_only_on_path_is_never_run", func(t *testing.T) {
		// A bwrap reachable only through PATH — a direnv bin/, a project shim — must not build
		// the fence: the backend is unavailable, the reason names where that bwrap is, and the
		// stub never runs (it would leave the marker behind if it did).
		marker := filepath.Join(t.TempDir(), "ran")
		launcher := stubLauncher(t, "touch '"+marker+"'\nexit 0\n")
		t.Setenv("PATH", filepath.Dir(launcher))

		c := newProbedNamespaceConfiner([]string{filepath.Join(t.TempDir(), "bwrap")}, isExecutableFile, exec.LookPath)

		caps := c.Capabilities()
		if caps.FSWrite || caps.NetworkEgress {
			t.Errorf("Capabilities = %+v, want neither cell for a bwrap found only on PATH", caps)
		}
		if want := "bwrap at " + launcher + " is outside the trusted system directories"; caps.Unavailable != want {
			t.Errorf("Unavailable = %q, want %q", caps.Unavailable, want)
		}
		if caps.Cause != domain.CauseBackendAbsent {
			t.Errorf("Cause = %q, want %q: an untrusted bwrap is no trusted bwrap", caps.Cause, domain.CauseBackendAbsent)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Errorf("the PATH-only bwrap at %s was executed; it must never run", launcher)
		}
	})

	t.Run("candidate_present_is_used", func(t *testing.T) {
		// The first EXECUTABLE candidate wins: a missing one and a non-executable one are
		// skipped, and the stub that exits 0 is probed and kept as the launcher.
		dir := t.TempDir()
		notExecutable := filepath.Join(dir, "bwrap")
		if err := os.WriteFile(notExecutable, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
			t.Fatalf("write non-executable candidate: %v", err)
		}
		launcher := stubLauncher(t, "exit 0\n")
		candidates := []string{filepath.Join(t.TempDir(), "bwrap"), notExecutable, launcher}

		c := newProbedNamespaceConfiner(candidates, isExecutableFile, lookNothing)

		if c.bwrapPath != launcher {
			t.Errorf("bwrapPath = %q, want the executable candidate %q", c.bwrapPath, launcher)
		}
		if caps := c.Capabilities(); !caps.FSWrite || caps.Unavailable != "" {
			t.Errorf("Capabilities = %+v, want a fenceable backend from a candidate that exits 0", caps)
		}
	})
}

// lookNothing is a PATH lookup that finds no program at all.
func lookNothing(string) (string, error) { return "", exec.ErrNotFound }

func TestResolveBwrap(t *testing.T) {
	t.Parallel()

	const pathBwrap = "/home/user/project/.direnv/bin/bwrap"
	lookPathBwrap := func(string) (string, error) { return pathBwrap, nil }

	tests := []struct {
		name       string
		executable []string
		look       func(string) (string, error)
		wantPath   string
		wantReason string
	}{
		{"none_anywhere", nil, lookNothing, "", bwrapAbsentReason},
		{"first_trusted_dir_wins", []string{"/bin/bwrap", "/usr/local/bin/bwrap"}, lookNothing, "/bin/bwrap", ""},
		{"nixos_profile", []string{"/run/current-system/sw/bin/bwrap"}, lookNothing, "/run/current-system/sw/bin/bwrap", ""},
		{"trusted_dir_beats_path", []string{"/usr/bin/bwrap"}, lookPathBwrap, "/usr/bin/bwrap", ""},
		{"path_only_is_refused", nil, lookPathBwrap, "", "bwrap at " + pathBwrap + " is outside the trusted system directories"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotPath, gotReason := resolveBwrap(bwrapCandidates, existsIn(tt.executable...), tt.look)

			if gotPath != tt.wantPath || gotReason != tt.wantReason {
				t.Errorf("resolveBwrap = (%q, %q), want (%q, %q)", gotPath, gotReason, tt.wantPath, tt.wantReason)
			}
		})
	}
}
