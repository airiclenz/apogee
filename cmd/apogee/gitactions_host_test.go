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

// identifyFixture gives dir an identity and no signing in its own config, so a commit the host
// makes there — a rebase's — needs nothing from the operator's config.
func identifyFixture(t *testing.T, dir string) {
	t.Helper()
	gitFixture(t, dir, "config", "user.name", "Test")
	gitFixture(t, dir, "config", "user.email", "test@example.com")
	gitFixture(t, dir, "config", "commit.gpgsign", "false")
}

// divergeFromUpstream lands one upstream commit adding upstream.txt and one local commit adding
// localName, then fetches, so the clone's branch has diverged from its last-fetched upstream.
// A localName of "upstream.txt" makes the two commits conflict.
func divergeFromUpstream(t *testing.T, remote, clone, localName string) {
	t.Helper()
	pushUpstreamCommit(t, remote, "upstream.txt")
	writeFixtureFile(t, clone, localName, "local\n")
	gitFixture(t, clone, "add", localName)
	gitFixture(t, clone, "commit", "-q", "-m", "local")
	gitFixture(t, clone, "fetch", "-q")
}

// TestGitActionsHostBranchesListsTheCurrentBranchFirst pins the list order — the checked-out
// branch, then the rest by name — and that a branch shadowed by a tag of the same name is listed
// by its bare name, never as heads/<name>.
func TestGitActionsHostBranchesListsTheCurrentBranchFirst(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)
	_, clone := statusHostClone(t)
	gitFixture(t, clone, "branch", "alpha")
	gitFixture(t, clone, "tag", "zeta")
	gitFixture(t, clone, "branch", "zeta")
	gitFixture(t, clone, "checkout", "-q", "-b", "mid")

	names, err := gitActionsHost{}.Branches(context.Background(), clone)

	if err != nil {
		t.Fatalf("Branches = %v", err)
	}
	if want := []string{"mid", "alpha", "main", "zeta"}; !slices.Equal(names, want) {
		t.Errorf("Branches = %q, want %q", names, want)
	}
}

// TestGitActionsHostSwitch pins each switch outcome: a clean switch lands on the branch (a
// tag-shadowed one included, never a detached HEAD), and a dirty tree, an option-like name and a
// name with no local branch are refused with no checkout run and HEAD where it was.
func TestGitActionsHostSwitch(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)

	cases := []struct {
		name string
		// arm shapes the clone before the switch.
		arm     func(t *testing.T, clone string)
		target  string
		wantRef string
		wantErr string
	}{
		{
			name:    "clean tree",
			arm:     func(t *testing.T, clone string) { gitFixture(t, clone, "branch", "feature") },
			target:  "feature",
			wantRef: "refs/heads/feature",
		},
		{
			name: "branch shadowed by a tag",
			arm: func(t *testing.T, clone string) {
				gitFixture(t, clone, "tag", "x")
				gitFixture(t, clone, "branch", "x")
			},
			target:  "x",
			wantRef: "refs/heads/x",
		},
		{
			name: "dirty tree",
			arm: func(t *testing.T, clone string) {
				gitFixture(t, clone, "branch", "feature")
				writeFixtureFile(t, clone, "README", "edited\n")
			},
			target:  "feature",
			wantRef: "refs/heads/main",
			wantErr: "uncommitted changes",
		},
		{
			name:    "option-like name",
			arm:     func(t *testing.T, clone string) {},
			target:  "-f",
			wantRef: "refs/heads/main",
			wantErr: "may not begin with '-'",
		},
		{
			name:    "a tag with no local branch",
			arm:     func(t *testing.T, clone string) { gitFixture(t, clone, "tag", "v1") },
			target:  "v1",
			wantRef: "refs/heads/main",
			wantErr: "no local branch",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, clone := statusHostClone(t)
			tc.arm(t, clone)
			rec := &recordingStatusHost{}

			result, err := gitActionsHost{base: rec.host()}.Switch(context.Background(), clone, tc.target)

			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("Switch(%q) = %v", tc.target, err)
			case tc.wantErr == "" && result != "switch: switched to "+tc.target:
				t.Errorf("Switch result = %q", result)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("Switch(%q) = %v, want an error containing %q", tc.target, err, tc.wantErr)
			case tc.wantErr != "" && rec.ran("checkout"):
				t.Errorf("a refused switch ran `git checkout`")
			}
			if ref := gitFixtureOutput(t, clone, "symbolic-ref", "HEAD"); ref != tc.wantRef {
				t.Errorf("HEAD = %s after the switch, want %s", ref, tc.wantRef)
			}
		})
	}
}

// TestGitActionsHostRebaseOntoTheUpstream pins that a clean, diverged branch is rebased onto its
// last-fetched upstream: the local commit now sits on the upstream's tip.
func TestGitActionsHostRebaseOntoTheUpstream(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)
	remote, clone := statusHostClone(t)
	identifyFixture(t, clone)
	divergeFromUpstream(t, remote, clone, "local.txt")

	result, err := gitActionsHost{}.Rebase(context.Background(), clone)

	if err != nil {
		t.Fatalf("Rebase = %v", err)
	}
	if result != "rebase: rebased onto origin/main" {
		t.Errorf("Rebase result = %q", result)
	}
	if parent, upstream := gitFixtureOutput(t, clone, "rev-parse", "HEAD~1"), gitFixtureOutput(t, remote, "rev-parse", "main"); parent != upstream {
		t.Errorf("HEAD~1 = %s after the rebase, want the upstream's %s", parent, upstream)
	}
}

// TestGitActionsHostRebaseRefusesADirtyTree pins that a tree with uncommitted changes gets no
// rebase and keeps its HEAD.
func TestGitActionsHostRebaseRefusesADirtyTree(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)
	remote, clone := statusHostClone(t)
	identifyFixture(t, clone)
	divergeFromUpstream(t, remote, clone, "local.txt")
	writeFixtureFile(t, clone, "README", "edited\n")
	before := gitFixtureOutput(t, clone, "rev-parse", "HEAD")
	rec := &recordingStatusHost{}

	_, err := gitActionsHost{base: rec.host()}.Rebase(context.Background(), clone)

	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("Rebase of a dirty tree = %v, want the dirty-tree refusal", err)
	}
	if rec.ran("rebase") {
		t.Error("a dirty tree ran `git rebase`")
	}
	if after := gitFixtureOutput(t, clone, "rev-parse", "HEAD"); after != before {
		t.Errorf("HEAD moved from %s to %s on a refused rebase", before, after)
	}
}

// TestGitActionsHostRebaseConflictIsAborted pins that a rebase stopping on a conflict is aborted:
// the error names the conflicting file, HEAD is unchanged, no rebase is left in progress, and the
// tree holds the local content again.
func TestGitActionsHostRebaseConflictIsAborted(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)
	remote, clone := statusHostClone(t)
	identifyFixture(t, clone)
	divergeFromUpstream(t, remote, clone, "upstream.txt")
	before := gitFixtureOutput(t, clone, "rev-parse", "HEAD")

	_, err := gitActionsHost{}.Rebase(context.Background(), clone)

	if err == nil || !strings.Contains(err.Error(), "conflict in upstream.txt") || !strings.Contains(err.Error(), "aborted") {
		t.Fatalf("Rebase with a conflict = %v, want an aborted conflict naming upstream.txt", err)
	}
	if after := gitFixtureOutput(t, clone, "rev-parse", "HEAD"); after != before {
		t.Errorf("HEAD moved from %s to %s on an aborted rebase", before, after)
	}
	if status := gitFixtureOutput(t, clone, "status", "--porcelain"); status != "" {
		t.Errorf("the tree is not as it was after the abort: %q", status)
	}
	if out, err := gitFixtureCmd(clone, "rev-parse", "-q", "--verify", "REBASE_HEAD").Output(); err == nil {
		t.Errorf("a rebase is still in progress at %s", out)
	}
}

// TestGitActionsHostBranchArgvsCarryTheHardening pins the checkout, rebase and rebase --abort
// argvs: the hardening pairs, submodule recursion off, and never a force.
func TestGitActionsHostBranchArgvsCarryTheHardening(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)
	remote, clone := statusHostClone(t)
	identifyFixture(t, clone)
	gitFixture(t, clone, "branch", "feature")
	rec := &recordingStatusHost{}
	host := gitActionsHost{base: rec.host()}
	ctx := context.Background()

	if _, err := host.Switch(ctx, clone, "feature"); err != nil {
		t.Fatalf("Switch = %v", err)
	}
	if _, err := host.Switch(ctx, clone, "main"); err != nil {
		t.Fatalf("Switch back = %v", err)
	}
	divergeFromUpstream(t, remote, clone, "upstream.txt")
	if _, err := host.Rebase(ctx, clone); err == nil {
		t.Fatal("Rebase with a conflict = nil, want the aborted conflict")
	}

	checkouts := rec.recordedArgvs("checkout")
	rebases := rec.recordedArgvs("rebase")
	if len(checkouts) != 2 || len(rebases) != 2 {
		t.Fatalf("ran %d checkouts and %d rebases, want 2 of each", len(checkouts), len(rebases))
	}
	for _, argv := range checkouts {
		assertActionArgv(t, argv, []string{"--"})
	}
	for _, argv := range rebases {
		assertActionArgv(t, argv, nil)
	}
	if !slices.Contains(rebases[1], "--abort") {
		t.Errorf("the second rebase argv %q is not the abort", rebases[1])
	}
}

// TestGitActionsHostRefusedRepositoryRunsNoBranchVerb pins that a repository gitexec refuses
// gets no checkout or rebase, and that the branch list, switch and rebase each report the
// refusal as an error.
func TestGitActionsHostRefusedRepositoryRunsNoBranchVerb(t *testing.T) {
	t.Parallel()
	requireGitForStatusHost(t)
	_, clone := statusHostClone(t)
	gitFixture(t, clone, "branch", "feature")
	gitFixture(t, clone, "config", "core.sshCommand", "ssh -v")
	rec := &recordingStatusHost{}
	host := gitActionsHost{base: rec.host()}
	ctx := context.Background()

	if _, err := host.Branches(ctx, clone); err == nil {
		t.Error("Branches in a refused repository = nil, want the refusal")
	}
	if _, err := host.Switch(ctx, clone, "feature"); err == nil {
		t.Error("Switch in a refused repository = nil, want the refusal")
	}
	if _, err := host.Rebase(ctx, clone); err == nil {
		t.Error("Rebase in a refused repository = nil, want the refusal")
	}

	for _, verb := range []string{"checkout", "rebase"} {
		if rec.ran(verb) {
			t.Errorf("a refused repository ran `git %s`", verb)
		}
	}
}
