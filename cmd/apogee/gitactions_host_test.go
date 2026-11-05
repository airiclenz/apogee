package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// gitFixtureOutput runs one fixture git command in dir and returns its trimmed standard output,
// failing the test on a non-zero exit.
func gitFixtureOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitFixtureCmd(dir, args...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// commitFixtureFile writes name in dir and commits it.
func commitFixtureFile(t *testing.T, dir, name string) {
	t.Helper()
	writeFixtureFile(t, dir, name, name+"\n")
	gitFixture(t, dir, "add", name)
	gitFixture(t, dir, "commit", "-q", "-m", name)
}

// pushUpstreamCommit lands one new commit on remote's main from a clone of its own, so a clone
// made earlier is one commit behind its upstream.
func pushUpstreamCommit(t *testing.T, remote, name string) {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	gitFixture(t, filepath.Dir(other), "clone", "-q", remote, other)
	commitFixtureFile(t, other, name)
	gitFixture(t, other, "push", "-q", "origin", "HEAD:main")
}

// recordedArgvs returns every recorded argv running the subcommand verb.
func (r *recordingStatusHost) recordedArgvs(verb string) [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found [][]string
	for _, argv := range r.argvs {
		if sub := gitSubcommand(argv); len(sub) > 0 && sub[0] == verb {
			found = append(found, argv)
		}
	}
	return found
}

// TestGitActionsHostPullFastForwards pins that a pull behind its upstream fast-forwards to it.
func TestGitActionsHostPullFastForwards(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)
	remote, clone := statusHostClone(t)
	pushUpstreamCommit(t, remote, "upstream.txt")

	result, err := gitActionsHost{}.Pull(context.Background(), clone)

	if err != nil {
		t.Fatalf("Pull = %v", err)
	}
	if result != "pull: fast-forwarded 1 commit" {
		t.Errorf("Pull result = %q", result)
	}
	if head, upstream := gitFixtureOutput(t, clone, "rev-parse", "HEAD"), gitFixtureOutput(t, remote, "rev-parse", "main"); head != upstream {
		t.Errorf("HEAD = %s after the pull, want the upstream's %s", head, upstream)
	}
}

// TestGitActionsHostPullRefusesADivergedBranch pins that a pull never makes a merge commit: a
// branch with commits of its own and new upstream commits is refused and left where it was.
func TestGitActionsHostPullRefusesADivergedBranch(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)
	remote, clone := statusHostClone(t)
	pushUpstreamCommit(t, remote, "upstream.txt")
	commitFixtureFile(t, clone, "local.txt")
	before := gitFixtureOutput(t, clone, "rev-parse", "HEAD")

	_, err := gitActionsHost{}.Pull(context.Background(), clone)

	if err == nil {
		t.Fatal("Pull of a diverged branch = nil, want git's refusal")
	}
	if after := gitFixtureOutput(t, clone, "rev-parse", "HEAD"); after != before {
		t.Errorf("HEAD moved from %s to %s on a refused pull", before, after)
	}
}

// TestGitActionsHostPushPushesToTheUpstream pins that a push lands the branch on its upstream.
func TestGitActionsHostPushPushesToTheUpstream(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)
	remote, clone := statusHostClone(t)
	commitFixtureFile(t, clone, "local.txt")

	result, err := gitActionsHost{}.Push(context.Background(), clone)

	if err != nil {
		t.Fatalf("Push = %v", err)
	}
	if result != "push: pushed main to origin/main" {
		t.Errorf("Push result = %q", result)
	}
	if head, pushed := gitFixtureOutput(t, clone, "rev-parse", "HEAD"), gitFixtureOutput(t, remote, "rev-parse", "main"); head != pushed {
		t.Errorf("remote main = %s after the push, want %s", pushed, head)
	}
}

// TestGitActionsHostPushSetsUpstreamOnOrigin pins that a branch with no upstream is pushed to
// origin and set to track the branch of the same name there.
func TestGitActionsHostPushSetsUpstreamOnOrigin(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)
	remote, clone := statusHostClone(t)
	gitFixture(t, clone, "checkout", "-q", "-b", "feature")
	commitFixtureFile(t, clone, "feature.txt")

	result, err := gitActionsHost{}.Push(context.Background(), clone)

	if err != nil {
		t.Fatalf("Push = %v", err)
	}
	if result != "push: pushed feature to origin/feature and set it as the upstream" {
		t.Errorf("Push result = %q", result)
	}
	if got := gitFixtureOutput(t, remote, "rev-parse", "feature"); got != gitFixtureOutput(t, clone, "rev-parse", "HEAD") {
		t.Errorf("remote feature = %s, want the clone's HEAD", got)
	}
	if got := gitFixtureOutput(t, clone, "rev-parse", "--abbrev-ref", "@{u}"); got != "origin/feature" {
		t.Errorf("upstream = %q after the push, want origin/feature", got)
	}
}

// TestGitActionsHostPushWithoutOriginNamesTheRemotes pins that a branch with no upstream and no
// remote named origin is refused, naming the remotes there are, and runs no push.
func TestGitActionsHostPushWithoutOriginNamesTheRemotes(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)
	_, clone := statusHostClone(t)
	gitFixture(t, clone, "remote", "rename", "origin", "upstream")
	gitFixture(t, clone, "checkout", "-q", "-b", "feature")
	rec := &recordingStatusHost{}

	_, err := gitActionsHost{base: rec.host()}.Push(context.Background(), clone)

	if err == nil || !strings.Contains(err.Error(), "remotes: upstream") {
		t.Fatalf("Push without origin = %v, want a refusal naming the remote upstream", err)
	}
	if rec.ran("push") {
		t.Error("a push with no origin ran `git push`")
	}
}

// TestGitActionsHostSyncStopsAfterAFailedPull pins that sync runs no push once its pull fails.
func TestGitActionsHostSyncStopsAfterAFailedPull(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)
	remote, clone := statusHostClone(t)
	pushUpstreamCommit(t, remote, "upstream.txt")
	commitFixtureFile(t, clone, "local.txt")
	upstream := gitFixtureOutput(t, remote, "rev-parse", "main")
	rec := &recordingStatusHost{}

	_, err := gitActionsHost{base: rec.host()}.Sync(context.Background(), clone)

	if err == nil {
		t.Fatal("Sync of a diverged branch = nil, want the pull's refusal")
	}
	if rec.ran("push") {
		t.Error("sync ran `git push` after its pull failed")
	}
	if got := gitFixtureOutput(t, remote, "rev-parse", "main"); got != upstream {
		t.Errorf("remote main moved from %s to %s", upstream, got)
	}
}

// TestGitActionsHostArgvsCarryTheHardening pins every fetch, merge and push argv: the hardening
// pairs, submodule recursion off, the transport pairs, the named pack programs, and never a force.
func TestGitActionsHostArgvsCarryTheHardening(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)
	remote, clone := statusHostClone(t)
	pushUpstreamCommit(t, remote, "upstream.txt")
	rec := &recordingStatusHost{}
	host := gitActionsHost{base: rec.host()}
	ctx := context.Background()

	if _, err := host.Pull(ctx, clone); err != nil {
		t.Fatalf("Pull = %v", err)
	}
	commitFixtureFile(t, clone, "local.txt")
	if _, err := host.Push(ctx, clone); err != nil {
		t.Fatalf("Push = %v", err)
	}

	verbFlags := map[string][]string{
		"fetch": {"--recurse-submodules=no", "--upload-pack=git-upload-pack"},
		"merge": {"--ff-only"},
		"push":  {"--recurse-submodules=no", "--receive-pack=git-receive-pack"},
	}
	for verb, flags := range verbFlags {
		argvs := rec.recordedArgvs(verb)
		if len(argvs) == 0 {
			t.Errorf("no `git %s` ran", verb)
		}
		for _, argv := range argvs {
			assertActionArgv(t, argv, flags)
		}
	}
}

// assertActionArgv checks one action argv for the -c pairs every action carries, the verb's own
// flags, and the absence of any force.
func assertActionArgv(t *testing.T, argv, flags []string) {
	t.Helper()
	for _, key := range []string{
		"core.hooksPath=", "core.fsmonitor=false", "submodule.recurse=false",
		"maintenance.auto=false", "gc.auto=0", "protocol.ext.allow=never", "protocol.git.allow=never",
	} {
		if !hasPair(argv, "-c", key) {
			t.Errorf("argv %q lacks -c %s", argv, key)
		}
	}
	for _, flag := range flags {
		if !slices.Contains(argv, flag) {
			t.Errorf("argv %q lacks %q", argv, flag)
		}
	}
	for _, arg := range gitSubcommand(argv) {
		if arg == "-f" || strings.HasPrefix(arg, "--force") || strings.HasPrefix(arg, "+") {
			t.Errorf("argv %q forces with %q", argv, arg)
		}
	}
}

// TestGitActionsHostPushTransportRunsNoCommand pins that a push runs no program the repository
// names: a repo-local receivepack, or an ext:: push URL the repository allowed.
func TestGitActionsHostPushTransportRunsNoCommand(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)

	cases := []struct {
		name string
		// arm configures clone so a plain push runs a program that writes marker; script is an
		// executable that writes marker and then runs git's own receive-pack.
		arm func(t *testing.T, clone, script, marker string)
	}{
		{
			name: "remote.origin.receivepack on a local-path remote",
			arm: func(t *testing.T, clone, script, _ string) {
				gitFixture(t, clone, "config", "remote.origin.receivepack", script)
			},
		},
		{
			name: "protocol.ext.allow=always with an ext:: push URL",
			arm: func(t *testing.T, clone, _, marker string) {
				gitFixture(t, clone, "config", "protocol.ext.allow", "always")
				gitFixture(t, clone, "remote", "set-url", "--push", "origin", "ext::sh -c touch% "+marker)
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
			writeFixtureFile(t, outside, "program.sh", "#!/bin/sh\ntouch '"+marker+"'\nexec git-receive-pack \"$@\"\n")
			if err := os.Chmod(script, 0o755); err != nil {
				t.Fatalf("chmod: %v", err)
			}
			tc.arm(t, clone, script, marker)
			commitFixtureFile(t, clone, "first.txt")

			_ = gitFixtureCmd(clone, "push", "--quiet", "origin", "HEAD:main").Run()
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("the vector is not live: a plain push wrote no marker (%v)", err)
			}
			if err := os.Remove(marker); err != nil {
				t.Fatalf("remove marker: %v", err)
			}
			commitFixtureFile(t, clone, "second.txt")

			_, _ = gitActionsHost{}.Push(context.Background(), clone)
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("the host's push ran the repository's program: the marker was written")
			}
		})
	}
}

// TestGitActionsHostWindowsOperatorSSHIsAnError pins that on Windows, where the child cannot be
// detached from the terminal, an ssh command of the operator's own refuses every networked action
// with an error — never the footer fetch's silent skip — and starts no fetch or push.
func TestGitActionsHostWindowsOperatorSSHIsAnError(t *testing.T) {
	t.Parallel()

	actions := map[string]func(gitActionsHost, context.Context, string) (string, error){
		"fetch": gitActionsHost.Fetch,
		"pull":  gitActionsHost.Pull,
		"push":  gitActionsHost.Push,
		"sync":  gitActionsHost.Sync,
	}
	for name, action := range actions {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := &gitStatusFake{}
			host := gitActionsHost{base: fake.host(t, map[string]string{"GIT_SSH_COMMAND": "ssh -i key"}, "windows")}

			_, err := action(host, context.Background(), t.TempDir())

			if err == nil || !strings.Contains(err.Error(), "Windows") {
				t.Fatalf("%s = %v, want an error naming why it did not run", name, err)
			}
			for _, verb := range []string{"fetch", "merge", "push"} {
				if _, ran := fake.spec(verb); ran {
					t.Errorf("%s started `git %s`", name, verb)
				}
			}
		})
	}
}

// TestGitActionsHostRefusedRepositoryRunsNoNetworkVerb pins that a repository gitexec refuses
// gets no fetch, merge or push from any action, and each action reports the refusal as an error.
// gitexec's own probe still spawns git, so the assertion is on the verbs that ran.
func TestGitActionsHostRefusedRepositoryRunsNoNetworkVerb(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)
	_, clone := statusHostClone(t)
	gitFixture(t, clone, "config", "core.sshCommand", "ssh -v")
	rec := &recordingStatusHost{}
	host := gitActionsHost{base: rec.host()}
	ctx := context.Background()

	for name, action := range map[string]func(context.Context, string) (string, error){
		"fetch": host.Fetch, "pull": host.Pull, "push": host.Push, "sync": host.Sync,
	} {
		if _, err := action(ctx, clone); err == nil {
			t.Errorf("%s in a refused repository = nil, want the refusal", name)
		}
	}

	for _, verb := range []string{"fetch", "merge", "push"} {
		if rec.ran(verb) {
			t.Errorf("a refused repository ran `git %s`", verb)
		}
	}
}
