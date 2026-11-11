//go:build linux

package platform

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// The pathname unix sockets a Linux backend closes (ADR 0112). A same-user service listening on
// a pathname AF_UNIX socket runs commands outside the fence for whoever connects, so a confined
// connect(2) to one is a write outside the box by another route. Two places hold the sockets
// that matter most and can be closed without breaking the box: the user runtime dir
// ($XDG_RUNTIME_DIR, /run/user/<uid> — the session bus with its systemd-run, rootless podman,
// the keyring) and the system container sockets (/run/docker.sock, /run/podman/podman.sock).
// The agent sockets a confined `git push` or `git commit -S` needs — ssh-agent and gpg-agent —
// are re-exposed beneath a masked runtime dir. Every other pathname socket (a tmux server under
// /tmp, say) stays reachable, so the backends keep disclosing connect(2) AF_UNIX.
//
// planUnixSockets is the one place that list is computed: the bwrap backend masks from it
// (namespaceArgv) and landlock at ABI ≥ 9 closes the same set with RESOLVE_UNIX.

// containerSocketCandidates are the system container sockets a confined command must not reach:
// docker's and rootful podman's, at their canonical /run paths (/var/run resolves there).
// Rootless podman listens under the user runtime dir, which the runtime-dir mask covers.
var containerSocketCandidates = []string{"/run/docker.sock", "/run/podman/podman.sock"}

// unixSocketHost is the host state planUnixSockets reads, injected so the plan is a pure
// function a table test drives with no host at all. currentUnixSocketHost supplies the real one.
type unixSocketHost struct {
	uid     int
	getenv  func(key string) string
	home    string // the user's home directory; "" when unknown
	tempDir string // os.TempDir()
	// resolve returns p's canonical form, symlinks followed, and true when p exists.
	resolve func(p string) (string, bool)
	// ownerMode returns the owning uid and the mode of the file at p (symlinks followed), and
	// true when p can be stat'ed.
	ownerMode func(p string) (int, fs.FileMode, bool)
}

// unixSocketPlan is what a backend closes and re-opens, every path canonical and existing at
// planning time. RuntimeDirs are masked whole; ContainerSockets are covered one by one;
// AgentSockets are the agent paths beneath a masked runtime dir, re-exposed after the mask.
type unixSocketPlan struct {
	RuntimeDirs      []string
	ContainerSockets []string
	AgentSockets     []string
}

// currentUnixSocketHost reads this process's host state for planUnixSockets: its uid, its
// environment, its home and temp directories, and the filesystem through os.Stat and
// filepath.EvalSymlinks. It is read at Confine time, so the box sees the environment the
// confined command is launched from.
func currentUnixSocketHost() unixSocketHost {
	// A home directory apogee cannot name protects nothing and is skipped by the plan.
	home, _ := os.UserHomeDir()
	return unixSocketHost{
		uid:       os.Getuid(),
		getenv:    os.Getenv,
		home:      home,
		tempDir:   os.TempDir(),
		resolve:   resolveExisting,
		ownerMode: statOwnerMode,
	}
}

// resolveExisting returns p with every symlink followed, and whether p exists at all.
func resolveExisting(p string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", false
	}
	return resolved, true
}

// statOwnerMode returns the owning uid and mode of p, following symlinks.
func statOwnerMode(p string) (int, fs.FileMode, bool) {
	info, err := os.Stat(p)
	if err != nil {
		return 0, 0, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(st.Uid), info.Mode(), true
}

// planUnixSockets computes which runtime dirs to mask, which container sockets to cover and
// which agent sockets to re-expose, for a box whose writable roots are roots.
//
// A runtime dir — $XDG_RUNTIME_DIR when absolute, and /run/user/<uid> — is masked only when it
// exists, is a directory owned by host.uid with mode 0700 (the XDG Base Directory spec's
// promise), and neither is nor contains `/`, the temp dir, the home dir or any box root: a
// stray `XDG_RUNTIME_DIR=/tmp` must never put an empty tmpfs over the temp dir, the home or
// the workspace. The agent sockets are $SSH_AUTH_SOCK (when absolute), each masked runtime
// dir's gnupg directory, and $GNUPGHOME or ~/.gnupg — each kept only when it exists and sits
// strictly beneath a masked runtime dir, since elsewhere nothing hides it. A container socket
// is kept only when it exists.
func planUnixSockets(host unixSocketHost, roots []string) unixSocketPlan {
	protected := make([]string, 0, 2+len(roots))
	for _, p := range append([]string{host.tempDir, host.home}, roots...) {
		if p == "" {
			continue
		}
		if canonical, ok := host.resolve(p); ok {
			p = canonical
		}
		protected = append(protected, filepath.Clean(p))
	}

	var plan unixSocketPlan
	candidates := []string{"/run/user/" + strconv.Itoa(host.uid)}
	if xdg := host.getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(xdg) {
		candidates = []string{xdg, candidates[0]}
	}
	for _, candidate := range candidates {
		dir, ok := host.resolve(candidate)
		if !ok || slices.Contains(plan.RuntimeDirs, dir) || !maskableRuntimeDir(host, dir, protected) {
			continue
		}
		plan.RuntimeDirs = append(plan.RuntimeDirs, dir)
	}

	for _, candidate := range containerSocketCandidates {
		if sock, ok := host.resolve(candidate); ok && !slices.Contains(plan.ContainerSockets, sock) {
			plan.ContainerSockets = append(plan.ContainerSockets, sock)
		}
	}

	agents := make([]string, 0, 2+len(plan.RuntimeDirs))
	if sock := host.getenv("SSH_AUTH_SOCK"); filepath.IsAbs(sock) {
		agents = append(agents, sock)
	}
	for _, dir := range plan.RuntimeDirs {
		agents = append(agents, filepath.Join(dir, "gnupg"))
	}
	if gnupgHome := host.getenv("GNUPGHOME"); filepath.IsAbs(gnupgHome) {
		agents = append(agents, gnupgHome)
	} else if host.home != "" {
		agents = append(agents, filepath.Join(host.home, ".gnupg"))
	}
	for _, agent := range agents {
		canonical, ok := host.resolve(agent)
		if !ok || slices.Contains(plan.AgentSockets, canonical) || !beneathAny(plan.RuntimeDirs, canonical) {
			continue
		}
		plan.AgentSockets = append(plan.AgentSockets, canonical)
	}
	return plan
}

// maskableRuntimeDir reports whether the canonical directory dir may be masked: owned by
// host.uid, mode exactly 0700, not `/`, and no ancestor of (nor equal to) any protected path.
func maskableRuntimeDir(host unixSocketHost, dir string, protected []string) bool {
	if dir == "/" {
		return false
	}
	uid, mode, ok := host.ownerMode(dir)
	if !ok || uid != host.uid || !mode.IsDir() || mode.Perm() != 0o700 {
		return false
	}
	for _, p := range protected {
		if p == dir || isBeneath(dir, p) {
			return false
		}
	}
	return true
}

// isBeneath reports whether the clean absolute path p lies strictly beneath the directory dir.
func isBeneath(dir, p string) bool {
	if dir == "/" {
		return p != "/"
	}
	return strings.HasPrefix(p, dir+"/")
}

// beneathAny reports whether p lies strictly beneath any of dirs.
func beneathAny(dirs []string, p string) bool {
	for _, dir := range dirs {
		if isBeneath(dir, p) {
			return true
		}
	}
	return false
}
