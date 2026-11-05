package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/airiclenz/apogee/internal/gitexec"
)

// gitActionsConfig is the -c set every fetch, merge and push the actions host starts carries.
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

// gitOriginRemote is the remote a branch with no upstream is pushed to and set to track.
const gitOriginRemote = "origin"

// errGitTransportUnsafe refuses a networked action whose transport cannot run safely: an ssh
// command of the operator's own on Windows, where the child cannot be detached from the terminal.
// The footer's fetch skips such a call; a user's act is refused with this reason instead.
var errGitTransportUnsafe = errors.New(
	"your own ssh command cannot run detached from the terminal on Windows, so apogee does not run it",
)

// gitActionsHost backs [tui.GitActionsHost]: the user's own fetch, pull, push and sync on the
// workspace (ADR 0108), each through internal/gitexec's hardened runner on the footer fetch's
// transport — no controlling terminal, no prompt, batch-mode ssh unless the operator named an ssh
// command, the command-running transports closed, and no submodule recursion. A refused
// repository runs no fetch, merge or push: gitexec returns the refusal, which comes back as the
// error. Every call is bounded only by [gitStatusBackstop]. The zero value runs the system git
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

// read runs one read-only query and returns its standard output, trimmed. A non-zero exit — a
// refused repository's included — is an error carrying git's own message.
func (r gitActionRun) read(ctx context.Context, args ...string) (string, error) {
	var out bytes.Buffer
	res, err := r.git.CaptureEnvTo(ctx, r.gitPath, r.root, r.env, gitexec.CaptureOptions{}, gitStatusBackstop, &out, args...)
	if err != nil {
		return "", err
	}
	if err := gitOutcomeError(args[0], res); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}
