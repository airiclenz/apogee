package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/gitexec"
	"github.com/airiclenz/apogee/internal/gitstatus"
	"github.com/airiclenz/apogee/internal/subprocess"
)

// gitStatusBackstop bounds one status or fetch call. It is a backstop, not a budget: a fetch
// over a slow link from a loaded single-board box may take minutes and must still land, so the
// ceiling only reaps a git that will never finish. The command-config probe in front of each
// call keeps gitexec's own, shorter timeout.
const gitStatusBackstop = 10 * time.Minute

// gitStatusArgs is the footer's status read. --ignore-submodules=dirty follows the git tools'
// own submodule decision (internal/tools/git.go): a submodule whose checked-out commit moved
// counts, dirt inside a submodule does not. -z frames each record with a NUL, so a path holding
// a newline cannot pose as a second record.
var gitStatusArgs = []string{
	"status", "--porcelain=v2", "--branch", "--untracked-files=all", "--ignore-submodules=dirty", "-z",
}

// gitFetchArgs is the footer's background fetch. The -c pairs keep a fetch apogee starts on its
// own from writing FETCH_HEAD or kicking off maintenance and gc, and close the two transports
// that run a command (ext::) or a configured proxy (git://). --upload-pack names git's own
// upload-pack, so a repo-local remote.<name>.uploadpack never runs: together they widen
// gitexec's command-config refusal — which does not cover those keys — for this verb only.
var gitFetchArgs = []string{
	"-c", "fetch.writeFetchHead=false",
	"-c", "maintenance.auto=false",
	"-c", "gc.auto=0",
	"-c", "protocol.ext.allow=never",
	"-c", "protocol.git.allow=never",
	"fetch", "--quiet", "--no-tags", "--recurse-submodules=no", "--upload-pack=git-upload-pack",
}

// gitNoPromptEnv is the environment both calls carry. GIT_OPTIONAL_LOCKS=0 keeps status from
// taking index.lock for its opportunistic index refresh, so a poll never collides with the
// operator's own git; the other two stop git and Git Credential Manager from asking for
// anything.
var gitNoPromptEnv = []string{"GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never"}

// gitSSHPassThrough are the variables gitexec's allowlist drops that a fetch over ssh needs from
// the operator's environment: the agent socket, and the operator's own choice of ssh command.
var gitSSHPassThrough = []string{"SSH_AUTH_SOCK", "GIT_SSH_COMMAND", "GIT_SSH"}

// gitUserSSHVars are the variables that name an ssh command of the operator's own.
var gitUserSSHVars = []string{"GIT_SSH_COMMAND", "GIT_SSH"}

// gitBatchModeSSH makes git's default ssh fail rather than ask for a password or a host key. It
// is set only where the operator named no ssh command of their own, which it would override.
const gitBatchModeSSH = "GIT_SSH_COMMAND=ssh -o BatchMode=yes"

// gitStatusHost is the git-status host the footer's poll chain runs through: one status read and
// one background fetch, both through internal/gitexec's hardened runner, never prompting and
// never taking index.lock. The zero value runs the system git under the real environment.
type gitStatusHost struct {
	// git is the hardened runner; the zero Host is the system git.
	git gitexec.Host
	// lookupEnv reads the operator's environment for the ssh pass-through. nil is os.LookupEnv.
	lookupEnv func(string) (string, bool)
	// goos is the operating system the fetch decides its skip rule for. "" is runtime.GOOS.
	goos string
}

// Status reads root's git state. inside is false — with a nil error — where root is no work tree,
// where no usable git is on PATH, and where gitexec refuses the repository for a repo-local
// command-valued key; none of those runs the status read. "Inside" is decided by
// `rev-parse --is-inside-work-tree`, never by git's stderr text.
//
// A non-nil error means no trustworthy answer this time — a timed-out or wedged call, a failed
// status, or a command-config probe that could not complete — and the snapshot and inside carry
// nothing: the caller keeps its last answer rather than showing a zero one. The status output is
// counted as a stream, so the counts are exact at any size.
func (h gitStatusHost) Status(ctx context.Context, root string) (gitstatus.Snapshot, bool, error) {
	gitPath, err := h.git.Resolve(ctx, root)
	if err != nil {
		return gitstatus.Snapshot{}, false, nil
	}
	env := h.baseEnv()
	inside, err := h.insideWorkTree(ctx, gitPath, root, env)
	if err != nil || !inside {
		return gitstatus.Snapshot{}, false, err
	}
	var counter gitstatus.Counter
	res, err := h.git.CaptureEnvTo(ctx, gitPath, root, env, gitexec.CaptureOptions{}, gitStatusBackstop, &counter, gitStatusArgs...)
	if err != nil {
		return gitstatus.Snapshot{}, false, err
	}
	if err := gitOutcomeError("status", res); err != nil {
		return gitstatus.Snapshot{}, false, err
	}
	return counter.Snapshot(), true, nil
}

// Fetch updates root's remote-tracking refs from its default remote, so the next Status sees new
// upstream commits. The child always starts with no controlling terminal; on Windows, where that
// detachment is a no-op, a fetch that would run an ssh command of the operator's own is skipped
// (nil) rather than risk a prompt. A refused repository gets no fetch: gitexec returns the
// refusal, which comes back as the error.
func (h gitStatusHost) Fetch(ctx context.Context, root string) error {
	gitPath, err := h.git.Resolve(ctx, root)
	if err != nil {
		return err
	}
	env := h.baseEnv()
	userSSH, err := h.userSSHCommand(ctx, gitPath, root, env)
	if err != nil {
		return err
	}
	if userSSH && h.targetOS() == "windows" {
		return nil
	}
	if !userSSH {
		env = append(env, gitBatchModeSSH)
	}
	res, err := h.git.CaptureEnv(ctx, gitPath, root, env, gitexec.CaptureOptions{NoTerminal: true}, gitStatusBackstop, gitFetchArgs...)
	if err != nil {
		return err
	}
	if res.Truncated {
		return fmt.Errorf("git fetch: output passed %d bytes", subprocess.MaxSubprocessOutputBytes)
	}
	return gitOutcomeError("fetch", res)
}

// insideWorkTree asks git whether root is inside a work tree. A non-zero exit — no repository, or
// a repository gitexec refuses — is "not inside"; only a probe that could not complete, which the
// next call asks again, is an error.
func (h gitStatusHost) insideWorkTree(ctx context.Context, gitPath, root string, env []string) (bool, error) {
	var out bytes.Buffer
	res, err := h.git.CaptureEnvTo(ctx, gitPath, root, env, gitexec.CaptureOptions{}, gitStatusBackstop, &out,
		"rev-parse", "--is-inside-work-tree")
	if err != nil {
		return false, err
	}
	if res.TimedOut || res.DrainWedged {
		return false, gitOutcomeError("rev-parse", res)
	}
	if res.ExitCode != 0 {
		if gitexec.IsIncompleteRefusal(res.CombinedOutput) {
			return false, errors.New(strings.TrimSpace(res.CombinedOutput))
		}
		return false, nil
	}
	return strings.TrimSpace(out.String()) == "true", nil
}

// userSSHCommand reports whether the fetch would run an ssh command of the operator's own: a
// GIT_SSH_COMMAND or GIT_SSH in the environment, or a core.sshCommand git reads from the
// operator's config. A config read that cannot answer — an absent key, a refused repository — is
// no such command; only a read that timed out or wedged is an error.
func (h gitStatusHost) userSSHCommand(ctx context.Context, gitPath, root string, env []string) (bool, error) {
	for _, key := range gitUserSSHVars {
		if _, ok := h.lookup(key); ok {
			return true, nil
		}
	}
	var out bytes.Buffer
	res, err := h.git.CaptureEnvTo(ctx, gitPath, root, env, gitexec.CaptureOptions{}, gitStatusBackstop, &out,
		"config", "--get", "core.sshCommand")
	if err != nil {
		return false, err
	}
	if res.TimedOut || res.DrainWedged {
		return false, gitOutcomeError("config", res)
	}
	return res.ExitCode == 0 && strings.TrimSpace(out.String()) != "", nil
}

// baseEnv is the env both calls append to gitexec's hardened one: the no-prompt variables, then
// each ssh pass-through variable the operator has set.
func (h gitStatusHost) baseEnv() []string {
	env := make([]string, 0, len(gitNoPromptEnv)+len(gitSSHPassThrough)+1)
	env = append(env, gitNoPromptEnv...)
	for _, key := range gitSSHPassThrough {
		if value, ok := h.lookup(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

// lookup reads one variable of the operator's environment.
func (h gitStatusHost) lookup(key string) (string, bool) {
	if h.lookupEnv == nil {
		return os.LookupEnv(key)
	}
	return h.lookupEnv(key)
}

// targetOS returns the operating system the fetch's skip rule is decided for.
func (h gitStatusHost) targetOS() string {
	if h.goos == "" {
		return runtime.GOOS
	}
	return h.goos
}

// gitOutcomeError flattens a finished call's failure into one error, nil for a clean exit.
func gitOutcomeError(verb string, res subprocess.SubprocessResult) error {
	switch {
	case res.TimedOut:
		return fmt.Errorf("git %s: timed out after %s", verb, gitStatusBackstop)
	case res.DrainWedged:
		return fmt.Errorf("git %s: output drain wedged", verb)
	case res.ExitCode != 0:
		return fmt.Errorf("git %s: exit %d: %s", verb, res.ExitCode, strings.TrimSpace(res.CombinedOutput))
	}
	return nil
}
