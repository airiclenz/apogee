package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/gitexec"
	"github.com/airiclenz/apogee/internal/subprocess"
	"github.com/airiclenz/apogee/internal/testtime"
)

// requireGitForStatusHost skips the test when no git is on PATH: the live behaviour is only
// assertable where git exists.
func requireGitForStatusHost(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH; skipping the live git run")
	}
}

// gitFixtureCmd builds one fixture git command in dir, with an identity and no signing so a commit
// needs nothing from the operator's config. It never goes through the host under test, so a
// fixture cannot be shaped by the behaviour being asserted.
func gitFixtureCmd(dir string, args ...string) *exec.Cmd {
	full := append([]string{"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_TERMINAL_PROMPT=0",
	)
	return cmd
}

// gitFixture runs one fixture git command in dir, failing the test on a non-zero exit.
func gitFixture(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := gitFixtureCmd(dir, args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// writeFixtureFile writes content to dir/name, creating its parent directories.
func writeFixtureFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// statusHostClone returns a bare remote with one commit on main and a clone of it that tracks
// origin/main.
func statusHostClone(t *testing.T) (remote, clone string) {
	t.Helper()
	base := t.TempDir()
	remote = filepath.Join(base, "remote.git")
	seed := filepath.Join(base, "seed")
	clone = filepath.Join(base, "clone")
	gitFixture(t, base, "init", "-q", "--bare", remote)
	gitFixture(t, base, "clone", "-q", remote, seed)
	writeFixtureFile(t, seed, "README", "seed\n")
	gitFixture(t, seed, "add", "README")
	gitFixture(t, seed, "commit", "-q", "-m", "seed")
	gitFixture(t, seed, "push", "-q", "origin", "HEAD:main")
	gitFixture(t, base, "clone", "-q", remote, clone)
	return remote, clone
}

// recordingStatusHost is the real git under a launcher that records every argv it starts.
type recordingStatusHost struct {
	mu    sync.Mutex
	argvs [][]string
}

// host returns a gitStatusHost whose two launchers record, then run the real subprocess.
func (r *recordingStatusHost) host() gitStatusHost {
	git := gitexec.OS()
	git.Spawn = func(ctx context.Context, spec subprocess.SubprocessSpec) (subprocess.SubprocessResult, error) {
		r.record(spec.Argv)
		return subprocess.RunSubprocess(ctx, spec)
	}
	git.SpawnTo = func(ctx context.Context, spec subprocess.SubprocessSpec, stdout io.Writer) (subprocess.SubprocessResult, error) {
		r.record(spec.Argv)
		return subprocess.RunSubprocessTo(ctx, spec, stdout)
	}
	return gitStatusHost{git: git}
}

// record keeps one started argv.
func (r *recordingStatusHost) record(argv []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.argvs = append(r.argvs, slices.Clone(argv))
}

// ran reports whether any recorded argv ran the subcommand verb.
func (r *recordingStatusHost) ran(verb string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, argv := range r.argvs {
		if sub := gitSubcommand(argv); len(sub) > 0 && sub[0] == verb {
			return true
		}
	}
	return false
}

// gitSubcommand returns argv from its subcommand on: past argv[0] and every leading `-c` pair,
// the hardening's and the fetch's own alike.
func gitSubcommand(argv []string) []string {
	i := 1
	for i+1 < len(argv) && argv[i] == "-c" {
		i += 2
	}
	return argv[i:]
}

// gitStatusFake scripts git for the host with no git installed: the probe's own rev-parse finds no
// repository (so nothing is memoised and nothing refused), rev-parse says inside, the
// core.sshCommand read answers sshCommand, and status and fetch answer their scripts. Every spec
// it is handed is recorded.
type gitStatusFake struct {
	mu    sync.Mutex
	specs []subprocess.SubprocessSpec

	// probe answers the command-config probe's `rev-parse --git-path`; nil finds no repository.
	probe func() subprocess.SubprocessResult
	// status writes the status stream and answers its outcome; nil is a clean, empty status.
	status func(io.Writer) subprocess.SubprocessResult
	// sshCommand is the configured core.sshCommand; "" is none.
	sshCommand string
}

// host returns a gitStatusHost over the fake, with env as the operator's environment and goos as
// its operating system.
func (f *gitStatusFake) host(t *testing.T, env map[string]string, goos string) gitStatusHost {
	t.Helper()
	gitPath := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(gitPath, nil, 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	git := gitexec.OS()
	git.Look = func(string) (string, error) { return gitPath, nil }
	git.Spawn = f.spawn
	git.SpawnTo = f.spawnTo
	return gitStatusHost{
		git: git,
		lookupEnv: func(key string) (string, bool) {
			value, ok := env[key]
			return value, ok
		},
		goos: goos,
	}
}

// spawn answers the captured launches: the probe and the fetch.
func (f *gitStatusFake) spawn(_ context.Context, spec subprocess.SubprocessSpec) (subprocess.SubprocessResult, error) {
	f.mu.Lock()
	f.specs = append(f.specs, spec)
	probe := f.probe
	f.mu.Unlock()
	sub := gitSubcommand(spec.Argv)
	switch {
	case len(sub) > 1 && sub[0] == "rev-parse" && sub[1] == "--git-path":
		if probe != nil {
			return probe(), nil
		}
		return subprocess.SubprocessResult{ExitCode: 128}, nil
	case len(sub) > 0 && sub[0] == "fetch":
		return subprocess.SubprocessResult{}, nil
	}
	return subprocess.SubprocessResult{ExitCode: 128}, nil
}

// spawnTo answers the streamed launches: rev-parse, the core.sshCommand read and status.
func (f *gitStatusFake) spawnTo(_ context.Context, spec subprocess.SubprocessSpec, stdout io.Writer) (subprocess.SubprocessResult, error) {
	f.mu.Lock()
	f.specs = append(f.specs, spec)
	status, sshCommand := f.status, f.sshCommand
	f.mu.Unlock()
	sub := gitSubcommand(spec.Argv)
	switch {
	case len(sub) > 0 && sub[0] == "rev-parse":
		_, _ = io.WriteString(stdout, "true\n")
		return subprocess.SubprocessResult{}, nil
	case len(sub) > 0 && sub[0] == "config":
		if sshCommand == "" {
			return subprocess.SubprocessResult{ExitCode: 1}, nil
		}
		_, _ = io.WriteString(stdout, sshCommand+"\n")
		return subprocess.SubprocessResult{}, nil
	case len(sub) > 0 && sub[0] == "status":
		if status != nil {
			return status(stdout), nil
		}
		return subprocess.SubprocessResult{}, nil
	}
	return subprocess.SubprocessResult{ExitCode: 128}, nil
}

// spec returns the last recorded spec running verb, and whether there was one.
func (f *gitStatusFake) spec(verb string) (subprocess.SubprocessSpec, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.specs) - 1; i >= 0; i-- {
		if sub := gitSubcommand(f.specs[i].Argv); len(sub) > 0 && sub[0] == verb {
			return f.specs[i], true
		}
	}
	return subprocess.SubprocessSpec{}, false
}

// hasPair reports whether argv holds first immediately followed by second.
func hasPair(argv []string, first, second string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == first && argv[i+1] == second {
			return true
		}
	}
	return false
}

// TestGitStatusHostCountsAKnownTree pins the counts against a real tree: two modified files, one
// staged new file and a new directory of three untracked files are six changed paths.
func TestGitStatusHostCountsAKnownTree(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)

	root := t.TempDir()
	gitFixture(t, root, "init", "-q")
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		writeFixtureFile(t, root, name, "one\n")
	}
	gitFixture(t, root, "add", ".")
	gitFixture(t, root, "commit", "-q", "-m", "seed")
	writeFixtureFile(t, root, "a.txt", "two\n")
	writeFixtureFile(t, root, "b.txt", "two\n")
	writeFixtureFile(t, root, "staged.txt", "new\n")
	gitFixture(t, root, "add", "staged.txt")
	for _, name := range []string{"x.txt", "y.txt", "z.txt"} {
		writeFixtureFile(t, root, filepath.Join("newdir", name), "new\n")
	}

	snap, inside, err := gitStatusHost{}.Status(context.Background(), root)
	if err != nil || !inside {
		t.Fatalf("Status = inside %v, err %v; want inside, nil", inside, err)
	}
	if snap.Branch != "main" || snap.Dirty != 6 || snap.Staged != 1 || snap.Unstaged != 2 || snap.Untracked != 3 {
		t.Fatalf("Status = %+v; want branch main, Dirty 6, Staged 1, Unstaged 2, Untracked 3", snap)
	}
}

// TestGitStatusHostReportsAheadAndBehindAfterFetch pins the fetch's purpose: a local commit is
// ahead at once, and a commit pushed from elsewhere is behind only after Fetch.
func TestGitStatusHostReportsAheadAndBehindAfterFetch(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)

	remote, clone := statusHostClone(t)
	other := filepath.Join(filepath.Dir(remote), "other")
	gitFixture(t, filepath.Dir(remote), "clone", "-q", remote, other)
	writeFixtureFile(t, other, "theirs.txt", "theirs\n")
	gitFixture(t, other, "add", "theirs.txt")
	gitFixture(t, other, "commit", "-q", "-m", "theirs")
	gitFixture(t, other, "push", "-q", "origin", "HEAD:main")
	writeFixtureFile(t, clone, "ours.txt", "ours\n")
	gitFixture(t, clone, "add", "ours.txt")
	gitFixture(t, clone, "commit", "-q", "-m", "ours")

	host := gitStatusHost{}
	ctx := context.Background()
	before, inside, err := host.Status(ctx, clone)
	if err != nil || !inside || !before.HasAheadBehind || before.Ahead != 1 || before.Behind != 0 {
		t.Fatalf("Status before fetch = %+v, inside %v, err %v; want ahead 1, behind 0", before, inside, err)
	}
	if err := host.Fetch(ctx, clone); err != nil {
		t.Fatalf("Fetch = %v, want nil", err)
	}
	after, inside, err := host.Status(ctx, clone)
	if err != nil || !inside || after.Ahead != 1 || after.Behind != 1 {
		t.Fatalf("Status after fetch = %+v, inside %v, err %v; want ahead 1, behind 1", after, inside, err)
	}
}

// TestGitStatusHostNotARepositoryIsNotInside pins the plain degrade: a directory no repository
// reaches is not inside, with no error.
func TestGitStatusHostNotARepositoryIsNotInside(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)

	rec := &recordingStatusHost{}
	_, inside, err := rec.host().Status(context.Background(), t.TempDir())
	if err != nil || inside {
		t.Fatalf("Status = inside %v, err %v; want not inside, nil", inside, err)
	}
	if rec.ran("status") {
		t.Fatal("Status ran `git status` outside a work tree")
	}
}

// TestGitStatusHostRefusedRepositoryRunsNoStatusOrFetch pins the refused repo: a repo-local
// core.sshCommand makes the repository not inside, and neither verb ever reaches git.
func TestGitStatusHostRefusedRepositoryRunsNoStatusOrFetch(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)

	_, clone := statusHostClone(t)
	gitFixture(t, clone, "config", "core.sshCommand", "ssh -v")

	rec := &recordingStatusHost{}
	host := rec.host()
	ctx := context.Background()
	if _, inside, err := host.Status(ctx, clone); err != nil || inside {
		t.Fatalf("Status = inside %v, err %v; want not inside, nil", inside, err)
	}
	if err := host.Fetch(ctx, clone); err == nil {
		t.Fatal("Fetch in a refused repository = nil, want the refusal")
	}
	for _, verb := range []string{"status", "fetch"} {
		if rec.ran(verb) {
			t.Fatalf("a refused repository ran `git %s`", verb)
		}
	}
}

// TestGitStatusHostEnvCarriesNoPromptAndSSHPassThrough pins the child environment: the no-prompt
// variables always, a set SSH_AUTH_SOCK passed through, and the BatchMode ssh command only where
// the operator named no ssh command of their own — which on Windows skips the fetch instead.
func TestGitStatusHostEnvCarriesNoPromptAndSSHPassThrough(t *testing.T) {
	t.Parallel()

	const batchMode = "GIT_SSH_COMMAND=ssh -o BatchMode=yes"
	noPrompt := []string{"GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never"}
	cases := []struct {
		name       string
		env        map[string]string
		sshCommand string
		goos       string
		wantFetch  bool
		wantEnv    []string
		wantNot    []string
	}{
		{
			name:      "no user ssh command gets BatchMode",
			env:       map[string]string{"SSH_AUTH_SOCK": "/run/agent.sock"},
			goos:      "linux",
			wantFetch: true,
			wantEnv:   []string{"SSH_AUTH_SOCK=/run/agent.sock", batchMode},
		},
		{
			name:      "a GIT_SSH_COMMAND passes through alone",
			env:       map[string]string{"SSH_AUTH_SOCK": "/run/agent.sock", "GIT_SSH_COMMAND": "my-ssh"},
			goos:      "linux",
			wantFetch: true,
			wantEnv:   []string{"SSH_AUTH_SOCK=/run/agent.sock", "GIT_SSH_COMMAND=my-ssh"},
			wantNot:   []string{batchMode},
		},
		{
			name:      "a GIT_SSH passes through with no BatchMode",
			env:       map[string]string{"GIT_SSH": "/usr/bin/plink"},
			goos:      "linux",
			wantFetch: true,
			wantEnv:   []string{"GIT_SSH=/usr/bin/plink"},
			wantNot:   []string{batchMode},
		},
		{
			name:       "a configured core.sshCommand gets no BatchMode",
			sshCommand: "my-ssh -i key",
			goos:       "linux",
			wantFetch:  true,
			wantNot:    []string{batchMode},
		},
		{
			name: "Windows skips the fetch with a user ssh command",
			env:  map[string]string{"GIT_SSH_COMMAND": "my-ssh"},
			goos: "windows",
		},
		{
			name:      "Windows fetches with BatchMode otherwise",
			goos:      "windows",
			wantFetch: true,
			wantEnv:   []string{batchMode},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &gitStatusFake{sshCommand: tc.sshCommand}
			host := fake.host(t, tc.env, tc.goos)
			root := t.TempDir()
			ctx := context.Background()
			if _, inside, err := host.Status(ctx, root); err != nil || !inside {
				t.Fatalf("Status = inside %v, err %v; want inside, nil", inside, err)
			}
			if err := host.Fetch(ctx, root); err != nil {
				t.Fatalf("Fetch = %v, want nil", err)
			}
			status, _ := fake.spec("status")
			for _, want := range noPrompt {
				if !slices.Contains(status.Env, want) {
					t.Errorf("status env lacks %q", want)
				}
			}
			fetch, fetched := fake.spec("fetch")
			if fetched != tc.wantFetch {
				t.Fatalf("fetch ran = %v, want %v", fetched, tc.wantFetch)
			}
			if !fetched {
				return
			}
			for _, want := range append(slices.Clone(noPrompt), tc.wantEnv...) {
				if !slices.Contains(fetch.Env, want) {
					t.Errorf("fetch env lacks %q", want)
				}
			}
			for _, unwanted := range tc.wantNot {
				if slices.Contains(fetch.Env, unwanted) {
					t.Errorf("fetch env carries %q", unwanted)
				}
			}
		})
	}
}

// TestGitStatusHostArgvsCarryTheHardening pins both argvs: status ignores dirt inside submodules,
// and the fetch carries its five -c keys, no submodule recursion, git's own upload-pack, and no
// controlling terminal.
func TestGitStatusHostArgvsCarryTheHardening(t *testing.T) {
	t.Parallel()

	fake := &gitStatusFake{}
	host := fake.host(t, nil, "linux")
	root := t.TempDir()
	ctx := context.Background()
	if _, _, err := host.Status(ctx, root); err != nil {
		t.Fatalf("Status = %v", err)
	}
	if err := host.Fetch(ctx, root); err != nil {
		t.Fatalf("Fetch = %v", err)
	}

	status, _ := fake.spec("status")
	for _, want := range []string{"--porcelain=v2", "--branch", "--untracked-files=all", "--ignore-submodules=dirty", "-z"} {
		if !slices.Contains(status.Argv, want) {
			t.Errorf("status argv %q lacks %q", status.Argv, want)
		}
	}
	fetch, _ := fake.spec("fetch")
	for _, key := range []string{
		"fetch.writeFetchHead=false", "maintenance.auto=false", "gc.auto=0",
		"protocol.ext.allow=never", "protocol.git.allow=never",
	} {
		if !hasPair(fetch.Argv, "-c", key) {
			t.Errorf("fetch argv %q lacks -c %s", fetch.Argv, key)
		}
	}
	for _, want := range []string{"--quiet", "--no-tags", "--recurse-submodules=no", "--upload-pack=git-upload-pack"} {
		if !slices.Contains(fetch.Argv, want) {
			t.Errorf("fetch argv %q lacks %q", fetch.Argv, want)
		}
	}
	if !fetch.NoTerminal {
		t.Error("fetch spec NoTerminal = false, want true")
	}
}

// TestGitStatusHostTimedOutStatusIsAnError pins that a status git did not finish is an error,
// never a snapshot of whatever it printed first.
func TestGitStatusHostTimedOutStatusIsAnError(t *testing.T) {
	t.Parallel()

	fake := &gitStatusFake{status: func(w io.Writer) subprocess.SubprocessResult {
		_, _ = io.WriteString(w, "# branch.head main\x00? half.txt\x00")
		return subprocess.SubprocessResult{TimedOut: true, ExitCode: -1}
	}}
	snap, _, err := fake.host(t, nil, "linux").Status(context.Background(), t.TempDir())
	if err == nil {
		t.Fatalf("Status of a timed-out run = %+v, nil error; want an error", snap)
	}
}

// TestGitStatusHostIncompleteProbeIsAnError pins the slow-probe rule: a command-config probe
// that could not complete is an error — the caller keeps its last counts — never "not inside".
func TestGitStatusHostIncompleteProbeIsAnError(t *testing.T) {
	t.Parallel()

	fake := &gitStatusFake{status: func(w io.Writer) subprocess.SubprocessResult {
		_, _ = io.WriteString(w, "# branch.head main\x00? new.txt\x00")
		return subprocess.SubprocessResult{}
	}}
	host := fake.host(t, nil, "linux")
	root := t.TempDir()
	ctx := context.Background()
	if snap, inside, err := host.Status(ctx, root); err != nil || !inside || snap.Untracked != 1 {
		t.Fatalf("first Status = %+v, inside %v, err %v; want one untracked, inside, nil", snap, inside, err)
	}

	fake.mu.Lock()
	fake.probe = func() subprocess.SubprocessResult {
		return subprocess.SubprocessResult{TimedOut: true, ExitCode: -1}
	}
	fake.mu.Unlock()
	_, _, err := host.Status(ctx, root)
	if err == nil {
		t.Fatal("Status under an incomplete probe = nil error; want an error that keeps the last counts")
	}
	if !gitexec.IsIncompleteRefusal(err.Error()) {
		t.Fatalf("Status error = %q, want the incomplete-probe refusal", err)
	}
}

// TestGitStatusHostStreamsAStatusPastTheCaptureCap pins the streamed count: a status stream past
// the subprocess capture cap, written in chunks that split records, is counted exactly.
func TestGitStatusHostStreamsAStatusPastTheCaptureCap(t *testing.T) {
	t.Parallel()

	const untracked = 10000
	var stream strings.Builder
	stream.WriteString("# branch.head main\x00")
	for i := range untracked {
		fmt.Fprintf(&stream, "? some/deeper/directory/untracked-%05d.txt\x00", i)
	}
	payload := stream.String()
	if len(payload) <= subprocess.MaxSubprocessOutputBytes {
		t.Fatalf("payload %d bytes does not pass the %d-byte cap", len(payload), subprocess.MaxSubprocessOutputBytes)
	}
	fake := &gitStatusFake{status: func(w io.Writer) subprocess.SubprocessResult {
		const chunk = 7777
		for start := 0; start < len(payload); start += chunk {
			_, _ = io.WriteString(w, payload[start:min(start+chunk, len(payload))])
		}
		return subprocess.SubprocessResult{}
	}}
	snap, inside, err := fake.host(t, nil, "linux").Status(context.Background(), t.TempDir())
	if err != nil || !inside {
		t.Fatalf("Status = inside %v, err %v; want inside, nil", inside, err)
	}
	if snap.Branch != "main" || snap.Untracked != untracked || snap.Dirty != untracked {
		t.Fatalf("Status = branch %q, Untracked %d, Dirty %d; want main, %d, %d",
			snap.Branch, snap.Untracked, snap.Dirty, untracked, untracked)
	}
}

// TestGitStatusHostFetchTransportRunsNoCommand pins the fetch transport: each repo-local way a
// remote names a program — an uploadpack, an ext:: URL, a gitProxy — is first shown live under a
// plain fetch, then runs nothing under the host's.
func TestGitStatusHostFetchTransportRunsNoCommand(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)

	cases := []struct {
		name string
		// arm configures clone so a plain fetch runs a program that writes marker; script is an
		// executable that writes marker and then runs git's own upload-pack.
		arm func(t *testing.T, clone, script, marker string)
	}{
		{
			name: "remote.origin.uploadpack on a local-path remote",
			arm: func(t *testing.T, clone, script, _ string) {
				gitFixture(t, clone, "config", "remote.origin.uploadpack", script)
			},
		},
		{
			name: "protocol.ext.allow=always with an ext:: URL",
			arm: func(t *testing.T, clone, _, marker string) {
				gitFixture(t, clone, "config", "protocol.ext.allow", "always")
				gitFixture(t, clone, "remote", "set-url", "origin", "ext::sh -c touch% "+marker)
			},
		},
		{
			name: "core.gitProxy on a git:// remote",
			arm: func(t *testing.T, clone, script, _ string) {
				gitFixture(t, clone, "config", "core.gitProxy", script)
				gitFixture(t, clone, "remote", "set-url", "origin", "git://127.0.0.1:1/remote.git")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, clone := statusHostClone(t)
			outside := t.TempDir()
			marker := filepath.Join(outside, "marker")
			script := filepath.Join(outside, "program.sh")
			writeFixtureFile(t, outside, "program.sh", "#!/bin/sh\ntouch '"+marker+"'\nexec git-upload-pack \"$@\"\n")
			if err := os.Chmod(script, 0o755); err != nil {
				t.Fatalf("chmod: %v", err)
			}
			tc.arm(t, clone, script, marker)

			_ = gitFixtureCmd(clone, "fetch", "--quiet").Run()
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("the vector is not live: a plain fetch wrote no marker (%v)", err)
			}
			if err := os.Remove(marker); err != nil {
				t.Fatalf("remove marker: %v", err)
			}

			_ = gitStatusHost{}.Fetch(context.Background(), clone)
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("the host's fetch ran the repository's program: the marker was written")
			}
		})
	}
}

// TestGitStatusHostFetchUnreachableErrorsWithoutPrompt pins that a fetch that cannot reach its
// remote — refused, or answered with a credential challenge — fails rather than waiting on a
// prompt.
func TestGitStatusHostFetchUnreachableErrorsWithoutPrompt(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)

	challenge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(challenge.Close)

	cases := []struct {
		name string
		url  string
	}{
		{name: "refused https", url: "https://127.0.0.1:1/remote.git"},
		{name: "refused ssh", url: "ssh://git@127.0.0.1:1/remote.git"},
		{name: "credential challenge", url: challenge.URL + "/remote.git"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, clone := statusHostClone(t)
			gitFixture(t, clone, "remote", "set-url", "origin", tc.url)

			ctx, cancel := context.WithTimeout(context.Background(), testtime.Budget(2*time.Minute))
			defer cancel()
			if err := (gitStatusHost{}).Fetch(ctx, clone); err == nil {
				t.Fatal("Fetch of an unreachable remote = nil, want an error")
			}
			if ctx.Err() != nil {
				t.Fatal("Fetch ran to the test's deadline: it waited instead of failing")
			}
		})
	}
}
