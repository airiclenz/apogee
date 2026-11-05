package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/airiclenz/apogee/internal/gitexec"
	"github.com/airiclenz/apogee/internal/gitstatus"
)

// gitActionsConfig is the -c set every fetch, merge, push, checkout and rebase the actions host
// starts carries.
// submodule.recurse=false comes first: gitexec's command-config refusal reads only the root
// repository's config, so no verb may recurse into a submodule whose own config it never saw.
// The fetch chain's quiet and transport pairs follow, so a user's act reaches the network exactly
// as the footer's background fetch does.
var gitActionsConfig = slices.Concat(
	[]string{"-c", "submodule.recurse=false"},
	gitQuietConfig,
	gitTransportConfig,
)

// gitPushVerb is the push subcommand, never forced: it takes an explicit refspec with no `+`, so
// neither a flag nor a configured remote.<name>.push mapping can force it. --receive-pack names
// git's own receive-pack, so a repo-local remote.<name>.receivepack never runs, and
// --recurse-submodules=no keeps a push.recurseSubmodules setting from reaching a submodule.
var gitPushVerb = []string{"push", "--recurse-submodules=no", "--receive-pack=git-receive-pack"}

// gitUpstreamFormat reads a branch's upstream as two lines: its remote's name, then the ref the
// branch merges from on that remote. Both are empty for a branch with no upstream.
const gitUpstreamFormat = "--format=%(upstream:remotename)%0a%(upstream:remoteref)"

// gitBranchRefPrefix is the prefix of every local branch's full ref name.
const gitBranchRefPrefix = "refs/heads/"

// gitBranchListFormat lists each local branch as HEAD's marker ("*" on the checked-out branch,
// blank on the rest), a space, then the branch's name. lstrip=2 strips exactly "refs/heads/":
// %(refname:short) yields "heads/x" for a branch shadowed by a tag x, a name checkout reads as a
// detached ref.
const gitBranchListFormat = "--format=%(HEAD) %(refname:lstrip=2)"

// gitCheckedOutMarker is the marker gitBranchListFormat prints on the checked-out branch.
const gitCheckedOutMarker = "*"

// gitCleanCheckArgs reads the tracked changes alone, NUL-framed for gitstatus.Counter. Untracked
// files do not count: neither checkout nor rebase overwrites one — git refuses instead — so they
// cannot be lost. --ignore-submodules=dirty follows the footer's own status read.
var gitCleanCheckArgs = []string{
	"status", "--porcelain=v2", "--untracked-files=no", "--ignore-submodules=dirty", "-z",
}

// gitConflictArgs lists the unmerged paths a stopped rebase left, NUL-framed so any path name
// survives, and never through an external diff program.
var gitConflictArgs = []string{"diff", "--name-only", "--diff-filter=U", "--no-ext-diff", "-z"}

// gitOriginRemote is the remote a branch with no upstream is pushed to and set to track.
const gitOriginRemote = "origin"

// errGitTransportUnsafe refuses a networked action whose transport cannot run safely: an ssh
// command of the operator's own on Windows, where the child cannot be detached from the terminal.
// The footer's fetch skips such a call; a user's act is refused with this reason instead.
var errGitTransportUnsafe = errors.New(
	"your own ssh command cannot run detached from the terminal on Windows, so apogee does not run it",
)

// gitActionsHost backs [tui.GitActionsHost]: the user's own fetch, pull, push, sync, branch
// switch and rebase on the workspace (ADR 0108), each through internal/gitexec's hardened runner
// — no controlling terminal, no prompt, no submodule recursion — and each networked call on the
// footer fetch's transport: batch-mode ssh unless the operator named an ssh command, and the
// command-running transports closed. A refused repository runs no fetch, merge, push, checkout
// or rebase: gitexec returns the refusal, which comes back as the error. Every call is bounded only by [gitStatusBackstop]. The zero value runs the system git
// under the real environment.
type gitActionsHost struct {
	// base holds the runner, the environment lookup and the OS the transport rule is decided for,
	// with the env and ssh helpers the footer's status host already owns.
	base gitStatusHost
}

// gitActionRun is one action's resolved git: the binary, the work tree and the transport env
// every call of the action carries.
type gitActionRun struct {
	git     gitexec.Host
	gitPath string
	root    string
	env     []string
}

// Fetch updates root's remote-tracking refs from its default remote.
func (h gitActionsHost) Fetch(ctx context.Context, root string) (string, error) {
	run, err := h.networked(ctx, root)
	if err != nil {
		return "", err
	}
	if err := run.act(ctx, gitFetchVerb...); err != nil {
		return "", err
	}
	return "fetch: remote-tracking refs updated", nil
}

// Pull fetches, then fast-forwards the branch to its upstream. A diverged branch is git's own
// refusal, never a merge commit.
func (h gitActionsHost) Pull(ctx context.Context, root string) (string, error) {
	run, err := h.networked(ctx, root)
	if err != nil {
		return "", err
	}
	return run.pull(ctx)
}

// Push pushes the branch to its upstream, never forced. A branch with no upstream is pushed to
// origin and set to track it; without a remote named origin it is refused, naming the remotes.
func (h gitActionsHost) Push(ctx context.Context, root string) (string, error) {
	run, err := h.networked(ctx, root)
	if err != nil {
		return "", err
	}
	return run.push(ctx)
}

// Sync pulls, then pushes, and stops at the first failure: a failed pull runs no push.
func (h gitActionsHost) Sync(ctx context.Context, root string) (string, error) {
	run, err := h.networked(ctx, root)
	if err != nil {
		return "", err
	}
	pulled, err := run.pull(ctx)
	if err != nil {
		return "", fmt.Errorf("sync stopped at pull: %w", err)
	}
	pushed, err := run.push(ctx)
	if err != nil {
		return "", fmt.Errorf("sync pulled (%s), then stopped at push: %w", pulled, err)
	}
	return pulled + "; " + pushed, nil
}

// Branches lists the local branches by their bare names, the checked-out one first and the rest
// in name order.
func (h gitActionsHost) Branches(ctx context.Context, root string) ([]string, error) {
	run, err := h.local(ctx, root)
	if err != nil {
		return nil, err
	}
	names, _, err := run.localBranches(ctx)
	return names, err
}

// Switch checks out the local branch name, terminated by "--" so it can never be read as a
// pathspec. A name beginning with "-", a name with no local branch and a dirty tree are each
// refused before any checkout runs.
func (h gitActionsHost) Switch(ctx context.Context, root, name string) (string, error) {
	if strings.HasPrefix(name, "-") {
		return "", fmt.Errorf("git switch: %q is not a branch name: it may not begin with '-'", name)
	}
	run, err := h.local(ctx, root)
	if err != nil {
		return "", err
	}
	names, current, err := run.localBranches(ctx)
	if err != nil {
		return "", err
	}
	if !slices.Contains(names, name) {
		return "", fmt.Errorf("git switch: there is no local branch named %q", name)
	}
	if name == current {
		return "switch: already on " + name, nil
	}
	if err := run.requireClean(ctx, "switch"); err != nil {
		return "", err
	}
	if err := run.act(ctx, "checkout", name, "--"); err != nil {
		return "", err
	}
	return "switch: switched to " + name, nil
}

// Rebase rebases the branch onto its upstream as last fetched; it fetches nothing. A dirty tree
// is refused, and a rebase that fails is aborted (see [gitActionRun.abortRebase]).
func (h gitActionsHost) Rebase(ctx context.Context, root string) (string, error) {
	run, err := h.local(ctx, root)
	if err != nil {
		return "", err
	}
	upstream, err := run.read(ctx, "rev-parse", "--abbrev-ref", "@{u}")
	if err != nil {
		return "", err
	}
	if err := run.requireClean(ctx, "rebase"); err != nil {
		return "", err
	}
	behind, err := run.read(ctx, "rev-list", "--count", "HEAD..@{u}")
	if err != nil {
		return "", err
	}
	if behind == "0" {
		return "rebase: already up to date with " + upstream, nil
	}
	if err := run.act(ctx, "rebase", "@{u}"); err != nil {
		return "", run.abortRebase(ctx, err)
	}
	return "rebase: rebased onto " + upstream, nil
}

// local resolves git for an action that never reaches the network: the no-prompt env, with no
// transport to decide.
func (h gitActionsHost) local(ctx context.Context, root string) (gitActionRun, error) {
	gitPath, err := h.base.git.Resolve(ctx, root)
	if err != nil {
		return gitActionRun{}, err
	}
	return gitActionRun{git: h.base.git, gitPath: gitPath, root: root, env: h.base.baseEnv()}, nil
}

// networked resolves git for a networked action and decides its transport env. An action whose
// transport cannot run safely is refused with [errGitTransportUnsafe], never skipped.
func (h gitActionsHost) networked(ctx context.Context, root string) (gitActionRun, error) {
	gitPath, err := h.base.git.Resolve(ctx, root)
	if err != nil {
		return gitActionRun{}, err
	}
	env, detachable, err := h.base.transportEnv(ctx, gitPath, root)
	if err != nil {
		return gitActionRun{}, err
	}
	if !detachable {
		return gitActionRun{}, errGitTransportUnsafe
	}
	return gitActionRun{git: h.base.git, gitPath: gitPath, root: root, env: env}, nil
}

// pull runs the hardened fetch, then `merge --ff-only @{u}` when the upstream is ahead.
func (r gitActionRun) pull(ctx context.Context) (string, error) {
	if err := r.act(ctx, gitFetchVerb...); err != nil {
		return "", err
	}
	behind, err := r.read(ctx, "rev-list", "--count", "HEAD..@{u}")
	if err != nil {
		return "", err
	}
	if behind == "0" {
		return "pull: already up to date", nil
	}
	if err := r.act(ctx, "merge", "--ff-only", "@{u}"); err != nil {
		return "", err
	}
	if behind == "1" {
		return "pull: fast-forwarded 1 commit", nil
	}
	return "pull: fast-forwarded " + behind + " commits", nil
}

// push pushes the checked-out branch: to its upstream when it has one, else to origin with
// --set-upstream. The refspec is always explicit and never forced.
func (r gitActionRun) push(ctx context.Context) (string, error) {
	ref, err := r.read(ctx, "rev-parse", "--symbolic-full-name", "HEAD")
	if err != nil {
		return "", err
	}
	branch, isBranch := strings.CutPrefix(ref, gitBranchRefPrefix)
	if !isBranch {
		return "", errors.New("git push: HEAD is detached, so there is no branch to push")
	}
	upstream, err := r.read(ctx, "for-each-ref", gitUpstreamFormat, ref)
	if err != nil {
		return "", err
	}
	remote, remoteRef, _ := strings.Cut(upstream, "\n")
	if remote != "" && remoteRef != "" {
		if err := r.act(ctx, slices.Concat(gitPushVerb, []string{remote, ref + ":" + remoteRef})...); err != nil {
			return "", err
		}
		return fmt.Sprintf("push: pushed %s to %s/%s", branch, remote, strings.TrimPrefix(remoteRef, gitBranchRefPrefix)), nil
	}
	return r.pushNewUpstream(ctx, ref, branch)
}

// pushNewUpstream pushes a branch with no upstream to origin and sets it to track the branch of
// the same name there. Without a remote named origin it is refused, naming the remotes there are.
func (r gitActionRun) pushNewUpstream(ctx context.Context, ref, branch string) (string, error) {
	listing, err := r.read(ctx, "remote")
	if err != nil {
		return "", err
	}
	remotes := strings.Fields(listing)
	if !slices.Contains(remotes, gitOriginRemote) {
		named := "none"
		if len(remotes) > 0 {
			named = strings.Join(remotes, ", ")
		}
		return "", fmt.Errorf("git push: %s has no upstream and no remote is named %s (remotes: %s)",
			branch, gitOriginRemote, named)
	}
	args := slices.Concat(gitPushVerb, []string{"--set-upstream", gitOriginRemote, ref + ":" + ref})
	if err := r.act(ctx, args...); err != nil {
		return "", err
	}
	return fmt.Sprintf("push: pushed %s to %s/%s and set it as the upstream", branch, gitOriginRemote, branch), nil
}

// localBranches lists the local branches by their bare names, the checked-out one first and the
// rest in name order, and names the checked-out one — empty when HEAD is detached. A branch name
// holds no whitespace, so each line splits into its marker and its name.
func (r gitActionRun) localBranches(ctx context.Context) (names []string, current string, err error) {
	listing, err := r.read(ctx, "for-each-ref", gitBranchListFormat, gitBranchRefPrefix)
	if err != nil {
		return nil, "", err
	}
	var others []string
	for _, line := range strings.Split(listing, "\n") {
		switch fields := strings.Fields(line); {
		case len(fields) == 2 && fields[0] == gitCheckedOutMarker:
			current = fields[1]
		case len(fields) == 1:
			others = append(others, fields[0])
		}
	}
	if current == "" {
		return others, "", nil
	}
	return append([]string{current}, others...), current, nil
}

// requireClean refuses verb on a tree with tracked changes, staged or not, so a checkout or a
// rebase never runs over uncommitted work.
func (r gitActionRun) requireClean(ctx context.Context, verb string) error {
	var counter gitstatus.Counter
	if err := r.readTo(ctx, &counter, gitCleanCheckArgs...); err != nil {
		return err
	}
	if counter.Snapshot().Dirty > 0 {
		return fmt.Errorf("git %s: the working tree has uncommitted changes; commit or stash them first", verb)
	}
	return nil
}

// abortRebase runs `rebase --abort` after a failed rebase, so the branch and tree are as they
// were, and returns the error to report: the conflicting files when the rebase stopped on a
// conflict, else git's own rebase error. The unmerged paths are read before the abort clears
// them. A rebase that failed before it started leaves nothing to abort; the abort's refusal is
// then appended rather than dropped, since apogee cannot tell that case from a failed abort.
func (r gitActionRun) abortRebase(ctx context.Context, rebaseErr error) error {
	cause := rebaseErr
	conflicts, listErr := r.conflictedPaths(ctx)
	switch {
	case listErr != nil:
		cause = fmt.Errorf("%w (the conflicting files could not be read: %v)", rebaseErr, listErr)
	case len(conflicts) > 0:
		cause = fmt.Errorf("git rebase: conflict in %s", strings.Join(conflicts, ", "))
	}
	if err := r.act(ctx, "rebase", "--abort"); err != nil {
		return fmt.Errorf("%w; rebase --abort also failed: %v", cause, err)
	}
	return fmt.Errorf("%w; the rebase was aborted and the branch is as it was", cause)
}

// conflictedPaths lists the unmerged paths, in git's order.
func (r gitActionRun) conflictedPaths(ctx context.Context) ([]string, error) {
	var out bytes.Buffer
	if err := r.readTo(ctx, &out, gitConflictArgs...); err != nil {
		return nil, err
	}
	var paths []string
	for _, path := range strings.Split(out.String(), "\x00") {
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

// act runs one verb that reaches the network or moves the branch, with [gitActionsConfig] ahead
// of it and no controlling terminal. A non-zero exit is an error carrying git's own message.
func (r gitActionRun) act(ctx context.Context, args ...string) error {
	res, err := r.git.CaptureEnv(ctx, r.gitPath, r.root, r.env, gitexec.CaptureOptions{NoTerminal: true},
		gitStatusBackstop, slices.Concat(gitActionsConfig, args)...)
	if err != nil {
		return err
	}
	return gitOutcomeError(args[0], res)
}

// read runs one read-only query and returns its standard output, trimmed.
func (r gitActionRun) read(ctx context.Context, args ...string) (string, error) {
	var out bytes.Buffer
	if err := r.readTo(ctx, &out, args...); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// readTo runs one read-only query, streaming its standard output to stdout. A non-zero exit — a
// refused repository's included — is an error carrying git's own message.
func (r gitActionRun) readTo(ctx context.Context, stdout io.Writer, args ...string) error {
	res, err := r.git.CaptureEnvTo(ctx, r.gitPath, r.root, r.env, gitexec.CaptureOptions{}, gitStatusBackstop, stdout, args...)
	if err != nil {
		return err
	}
	return gitOutcomeError(args[0], res)
}
