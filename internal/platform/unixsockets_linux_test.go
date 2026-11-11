//go:build linux

package platform

import (
	"io/fs"
	"slices"
	"testing"
)

// fakeHostUID is the uid every fake unix-socket host runs as.
const fakeHostUID = 1000

// fakePath is one path on a fake unix-socket host: where it resolves (itself when canonical is
// empty) and the owner and mode stat reports for it.
type fakePath struct {
	canonical string
	uid       int
	mode      fs.FileMode
}

// runtimeDir is a fake path that is a directory owned by the fake uid with the XDG mode 0700.
var runtimeDir = fakePath{uid: fakeHostUID, mode: fs.ModeDir | 0o700}

// socketFile is a fake path that is a socket owned by the fake uid.
var socketFile = fakePath{uid: fakeHostUID, mode: fs.ModeSocket | 0o600}

// fakeUnixSocketHost returns a unix-socket host running as fakeHostUID with env as its
// environment, home and tmp as its home and temp dirs, and exactly paths on its filesystem.
func fakeUnixSocketHost(env map[string]string, home, tmp string, paths map[string]fakePath) unixSocketHost {
	return unixSocketHost{
		uid:     fakeHostUID,
		getenv:  func(key string) string { return env[key] },
		home:    home,
		tempDir: tmp,
		resolve: func(p string) (string, bool) {
			f, ok := paths[p]
			if !ok {
				return "", false
			}
			if f.canonical != "" {
				return f.canonical, true
			}
			return p, true
		},
		ownerMode: func(p string) (int, fs.FileMode, bool) {
			f, ok := paths[p]
			return f.uid, f.mode, ok
		},
	}
}

// noUnixSockets is a host with no runtime dir, no container socket and no agent socket, so a
// box planned against it masks nothing.
var noUnixSockets = fakeUnixSocketHost(nil, "/home/u", "/tmp", nil)

func TestUnixSocketPlan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		env   map[string]string
		roots []string
		paths map[string]fakePath
		want  unixSocketPlan
	}{
		{
			name:  "xdg_and_run_user_name_one_dir_masked_once",
			env:   map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"},
			paths: map[string]fakePath{"/run/user/1000": runtimeDir},
			want:  unixSocketPlan{RuntimeDirs: []string{"/run/user/1000"}},
		},
		{
			name: "xdg_and_run_user_both_masked_when_distinct",
			env:  map[string]string{"XDG_RUNTIME_DIR": "/srv/rt"},
			paths: map[string]fakePath{
				"/srv/rt":        runtimeDir,
				"/run/user/1000": runtimeDir,
			},
			want: unixSocketPlan{RuntimeDirs: []string{"/srv/rt", "/run/user/1000"}},
		},
		{
			// /var/run is a symlink to /run on a modern host: the mask lands on the directory the
			// child's paths resolve to.
			name: "symlinked_xdg_is_masked_at_its_canonical_path",
			env:  map[string]string{"XDG_RUNTIME_DIR": "/var/run/user/1000"},
			paths: map[string]fakePath{
				"/var/run/user/1000": {canonical: "/run/user/1000"},
				"/run/user/1000":     runtimeDir,
			},
			want: unixSocketPlan{RuntimeDirs: []string{"/run/user/1000"}},
		},
		{
			name: "relative_xdg_is_ignored",
			env:  map[string]string{"XDG_RUNTIME_DIR": "run/user/1000"},
			paths: map[string]fakePath{
				"run/user/1000":  runtimeDir,
				"/run/user/1000": runtimeDir,
			},
			want: unixSocketPlan{RuntimeDirs: []string{"/run/user/1000"}},
		},
		{
			name:  "missing_runtime_dir_is_skipped",
			paths: nil,
			want:  unixSocketPlan{},
		},
		{
			name:  "runtime_dir_owned_by_another_uid_is_refused",
			paths: map[string]fakePath{"/run/user/1000": {uid: 0, mode: fs.ModeDir | 0o700}},
			want:  unixSocketPlan{},
		},
		{
			name:  "runtime_dir_not_mode_0700_is_refused",
			paths: map[string]fakePath{"/run/user/1000": {uid: fakeHostUID, mode: fs.ModeDir | 0o755}},
			want:  unixSocketPlan{},
		},
		{
			name:  "runtime_dir_that_is_not_a_directory_is_refused",
			paths: map[string]fakePath{"/run/user/1000": {uid: fakeHostUID, mode: 0o700}},
			want:  unixSocketPlan{},
		},
		{
			name:  "root_is_never_masked",
			env:   map[string]string{"XDG_RUNTIME_DIR": "/"},
			paths: map[string]fakePath{"/": runtimeDir},
			want:  unixSocketPlan{},
		},
		{
			// The stray XDG_RUNTIME_DIR=/tmp the guard exists for: an empty tmpfs over the temp
			// dir would hide every scratch file the box writes and reads.
			name:  "temp_dir_is_never_masked",
			env:   map[string]string{"XDG_RUNTIME_DIR": "/tmp"},
			paths: map[string]fakePath{"/tmp": runtimeDir},
			want:  unixSocketPlan{},
		},
		{
			name:  "home_is_never_masked",
			env:   map[string]string{"XDG_RUNTIME_DIR": "/home/u"},
			paths: map[string]fakePath{"/home/u": runtimeDir},
			want:  unixSocketPlan{},
		},
		{
			name:  "an_ancestor_of_home_is_never_masked",
			env:   map[string]string{"XDG_RUNTIME_DIR": "/home"},
			paths: map[string]fakePath{"/home": runtimeDir},
			want:  unixSocketPlan{},
		},
		{
			name:  "an_ancestor_of_a_box_root_is_never_masked",
			env:   map[string]string{"XDG_RUNTIME_DIR": "/srv/rt"},
			roots: []string{"/srv/rt/ws"},
			paths: map[string]fakePath{"/srv/rt": runtimeDir, "/srv/rt/ws": runtimeDir},
			want:  unixSocketPlan{},
		},
		{
			name:  "a_box_root_itself_is_never_masked",
			env:   map[string]string{"XDG_RUNTIME_DIR": "/srv/rt"},
			roots: []string{"", "/srv/rt"},
			paths: map[string]fakePath{"/srv/rt": runtimeDir},
			want:  unixSocketPlan{},
		},
		{
			name:  "a_box_root_beside_the_runtime_dir_does_not_block_it",
			roots: []string{"/run/user/ws"},
			paths: map[string]fakePath{"/run/user/1000": runtimeDir},
			want:  unixSocketPlan{RuntimeDirs: []string{"/run/user/1000"}},
		},
		{
			name:  "docker_socket_returned_only_when_it_exists",
			paths: map[string]fakePath{"/run/docker.sock": socketFile},
			want:  unixSocketPlan{ContainerSockets: []string{"/run/docker.sock"}},
		},
		{
			name: "both_container_sockets_returned_when_both_exist",
			paths: map[string]fakePath{
				"/run/docker.sock":        socketFile,
				"/run/podman/podman.sock": socketFile,
			},
			want: unixSocketPlan{ContainerSockets: []string{"/run/docker.sock", "/run/podman/podman.sock"}},
		},
		{
			// A container socket is covered whether or not a runtime dir is masked: the two
			// halves of the plan are independent.
			name: "container_socket_covered_beside_a_refused_runtime_dir",
			env:  map[string]string{"XDG_RUNTIME_DIR": "/tmp"},
			paths: map[string]fakePath{
				"/tmp":                    runtimeDir,
				"/run/podman/podman.sock": socketFile,
			},
			want: unixSocketPlan{ContainerSockets: []string{"/run/podman/podman.sock"}},
		},
		{
			name: "agent_sockets_beneath_the_masked_dir_are_re_exposed",
			env:  map[string]string{"SSH_AUTH_SOCK": "/run/user/1000/keyring/ssh"},
			paths: map[string]fakePath{
				"/run/user/1000":             runtimeDir,
				"/run/user/1000/keyring/ssh": socketFile,
				"/run/user/1000/gnupg":       runtimeDir,
				"/home/u/.gnupg":             runtimeDir,
			},
			want: unixSocketPlan{
				RuntimeDirs:  []string{"/run/user/1000"},
				AgentSockets: []string{"/run/user/1000/keyring/ssh", "/run/user/1000/gnupg"},
			},
		},
		{
			name: "gnupghome_beneath_the_masked_dir_is_re_exposed",
			env:  map[string]string{"GNUPGHOME": "/run/user/1000/gpg-home"},
			paths: map[string]fakePath{
				"/run/user/1000":          runtimeDir,
				"/run/user/1000/gpg-home": runtimeDir,
			},
			want: unixSocketPlan{
				RuntimeDirs:  []string{"/run/user/1000"},
				AgentSockets: []string{"/run/user/1000/gpg-home"},
			},
		},
		{
			// Outside a masked dir nothing hides the agent, so there is nothing to re-expose.
			name: "agent_socket_outside_the_masked_dir_needs_no_rebind",
			env:  map[string]string{"SSH_AUTH_SOCK": "/tmp/ssh-x/agent.1"},
			paths: map[string]fakePath{
				"/run/user/1000":     runtimeDir,
				"/tmp/ssh-x/agent.1": socketFile,
			},
			want: unixSocketPlan{RuntimeDirs: []string{"/run/user/1000"}},
		},
		{
			name: "missing_and_relative_agent_sockets_are_skipped",
			env:  map[string]string{"SSH_AUTH_SOCK": "/run/user/1000/gone", "GNUPGHOME": "rel/gnupg"},
			paths: map[string]fakePath{
				"/run/user/1000": runtimeDir,
				"rel/gnupg":      runtimeDir,
			},
			want: unixSocketPlan{RuntimeDirs: []string{"/run/user/1000"}},
		},
		{
			name: "agent_socket_under_a_refused_runtime_dir_is_not_re_exposed",
			env:  map[string]string{"XDG_RUNTIME_DIR": "/srv/rt", "SSH_AUTH_SOCK": "/srv/rt/agent"},
			paths: map[string]fakePath{
				"/srv/rt":       {uid: fakeHostUID, mode: fs.ModeDir | 0o755},
				"/srv/rt/agent": socketFile,
			},
			want: unixSocketPlan{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			host := fakeUnixSocketHost(tt.env, "/home/u", "/tmp", tt.paths)

			got := planUnixSockets(host, tt.roots)

			if !slices.Equal(got.RuntimeDirs, tt.want.RuntimeDirs) {
				t.Errorf("RuntimeDirs = %q, want %q", got.RuntimeDirs, tt.want.RuntimeDirs)
			}
			if !slices.Equal(got.ContainerSockets, tt.want.ContainerSockets) {
				t.Errorf("ContainerSockets = %q, want %q", got.ContainerSockets, tt.want.ContainerSockets)
			}
			if !slices.Equal(got.AgentSockets, tt.want.AgentSockets) {
				t.Errorf("AgentSockets = %q, want %q", got.AgentSockets, tt.want.AgentSockets)
			}
		})
	}
}
