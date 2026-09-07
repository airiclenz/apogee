package gitexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/subprocess"
)

// UnavailableMessage is the graceful-degradation sentence for a host with no git on PATH (§3a —
// git is a convenience dependency, never a hard one). It is worded for the MODEL, which reads it
// as a tool result, and is deliberately different from the exec fence's refusal below: a fence
// refusal that read "git not available" would send the operator installing a git they already
// have.
const UnavailableMessage = "git not available: no git executable found on PATH"

// probeTimeout bounds one repo-local command-config probe. The probe is a local `git config`
// listing, so a short ceiling is ample and a hung git never wedges the call it guards (the §2.4
// teardown reaps the process group when it fires).
const probeTimeout = 15 * time.Second

// LookFunc is the PATH lookup shape security.ResolveProgram performs for git: the absolute path
// and a nil error, or exec.LookPath's error when git is absent.
type LookFunc func(string) (string, error)

// LookPath is the lookup Resolve uses when a caller names none — a package var so a test can
// inject a fake resolver without reaching for the host's real git.
var LookPath LookFunc = exec.LookPath

// host is the per-OS facility set the environment scoping goes through; the platform's own
// essentials are appended by ScopeEnv rather than restated in safeEnvKeys.
var host platform.Host = platform.Current()

// safeEnvKeys is the allowlist of environment variables a git subprocess inherits
// (ported from the TS oracle's SAFE_ENV_KEYS). Everything else is dropped, so a
// surprising inherited variable cannot redirect git (config, auth, pager) — the
// process sees only the keys a normal git invocation needs.
//
// The list is POSIX-shaped, which is a policy choice, not a portability one: the
// variables a Windows process cannot start without (%SystemRoot%, %ComSpec%,
// %PATHEXT%, the profile paths git reads its user config from) are the platform's
// floor and are appended by platform.Shell.ScopeEnv rather than restated here.
var safeEnvKeys = []string{
	"PATH", "HOME", "USER", "SHELL", "LOGNAME", "HOSTNAME", "PWD",
	"LANG", "LC_ALL", "LC_CTYPE", "LC_MESSAGES", "LC_COLLATE",
	"TERM", "TERM_PROGRAM", "COLORTERM",
	"TMPDIR", "TMP", "TEMP",
	"XDG_RUNTIME_DIR", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME",
	"EDITOR", "VISUAL", "PAGER",
	"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL",
	"CI", "GITHUB_ACTIONS", "GITLAB_CI", "JENKINS_URL",
}

// SafeEnv returns the allowlisted environment for a git subprocess scoped to root: each
// safeEnvKeys entry that is present in the host environment, in "KEY=value" form,
// followed by the platform's own essentials (none on POSIX — the output there is
// exactly the allowlist).
//
// root is the workspace the PATH value is scoped to: the allowlist decides which VARIABLES
// git inherits, and this decides that the one variable saying where programs come from cannot
// point back inside the box the model writes to. git resolves programs of its own — hooks,
// credential helpers, pagers, diff drivers — so an unscoped PATH would hand every one of those
// resolutions to the workspace. An empty root scopes nothing (the shape a test wants).
func SafeEnv(root string) []string {
	return host.ScopeEnv(root, safeEnvKeys, os.LookupEnv)
}

// hardeningOptions are the global options every git invocation carries, ahead of its
// subcommand. They close the one seam the allowlisted environment above does not reach: git
// runs programs the REPOSITORY names, not just the ones apogee names.
//
// core.hooksPath= empties the directory git resolves hooks from, so nothing in an
// attacker-authored .git/hooks/ ever executes. git joins the setting with the hook name, so an
// empty value resolves every hook to "/<name>" at the filesystem root — a path no in-workspace
// write can create — and unlike --no-verify it covers EVERY hook, including the post-* ones
// (post-checkout fires on the branch tools, which have no --no-verify to pass). The delivery
// this stops is a repository shipped WITH its .git — a tarball, a mirror, an NFS checkout — or
// a single in-workspace write into an existing .git/hooks/; a plain `git clone` does not carry
// hooks, so the write is the realistic variant. A hook is a shell script git executes on the
// operator's behalf with no gate of ours in front of it, which is exactly the unbounded blast
// radius ADR 0012 requires a gate for.
//
// core.fsmonitor=false covers the other key git runs on nearly every command. The setting takes
// a COMMAND — the repository names a program and git executes it to ask which files changed — so
// an attacker-authored config turns a plain `git status` into a launcher. false disables both the
// hook form and git's own builtin daemon, which is why it is a neutralisation rather than a
// refusal: a repository that legitimately set core.fsmonitor=true (the everyday builtin case)
// keeps working, just without the index-refresh shortcut, instead of losing every git tool.
var hardeningOptions = []string{"-c", "core.hooksPath=", "-c", "core.fsmonitor=false"}

// hardeningEnv is appended to the allowlisted environment of every git subprocess.
// GIT_CONFIG_NOSYSTEM drops the system config (/etc/gitconfig, and the Git-for-Windows
// equivalent) from the files git merges, shrinking the set of places a configured hook path,
// pager, credential helper or diff driver can come from.
//
// One residual is deliberate, not an oversight: HOME stays on safeEnvKeys, so the OPERATOR's
// own ~/.gitconfig still applies — that config is theirs, and the threat model here trusts the
// operator and distrusts the bytes in the workspace. Command-valued config keys are the class
// git has no global switch for: a filter driver (clean/smudge/process), an sshCommand, a
// credential helper, a merge driver, a textconv, a pager, a gpg.program — each names a COMMAND
// that lives in config, and there is no --no-configured-programs to pass. Since 2026-08-14 that
// half is closed one level up instead, and since 2026-08-26 for the whole class rather than
// filters alone — every run through this package REFUSES the call outright when the
// REPOSITORY's own config defines any such key (repoLocalCommandConfig), which is exactly why
// the operator's global ones keep working. DiffHardeningArgs closes the read-path diff drivers,
// which are the ones a mere inspection would otherwise execute.
var hardeningEnv = []string{"GIT_CONFIG_NOSYSTEM=1"}

// DiffHardeningArgs are the diff-level refusals the read paths carry. A textconv driver and
// an external diff command are both programs the repository selects (in .gitattributes) and
// configures (in config), and both run on a plain diff or log — so `git_diff_range` and
// `git_log`, tools whose whole promise is that they only LOOK at the repository, would execute
// attacker-chosen commands. Passing both refusals makes the promise true: the caller sees the
// stored bytes rather than a driver's rendering of them.
var DiffHardeningArgs = []string{"--no-textconv", "--no-ext-diff"}

// Program resolves the system git for a call scoped to root and applies the exec fence to what
// it found, rendering a failure as the model-facing sentence its caller shows. ok=false carries
// that sentence in refusal: the graceful UnavailableMessage when no git is on PATH (§3a), and
// the fence's own refusal — which NAMES the resolved path — when the git that was found is one
// the model could have written (a workspace-resident PATH entry).
//
// look is the lookup seam; nil takes LookPath. A caller holding its own swappable lookup var
// passes it through, so a fake git installed for a test reaches this resolution.
func Program(ctx context.Context, root string, look LookFunc) (gitPath, refusal string, ok bool) {
	path, err := Resolve(ctx, root, look)
	if err != nil {
		return "", err.Error(), false
	}
	return path, "", true
}

// Resolve is Program's error-returning form, for the callers that pass the failure ON as an
// error rather than rendering it into a tool result: it keeps the fence's sentinel
// (security.ErrExecFromWritablePath) intact through errors.Is, which a message string cannot.
// Both forms resolve and fence identically — Program is this function plus the render.
func Resolve(ctx context.Context, root string, look LookFunc) (string, error) {
	if look == nil {
		look = LookPath
	}
	path, err := security.ResolveProgram(look, "git", root, confinementBox(ctx))
	if err != nil {
		// The fence's refusal passes through with its sentinel and its own sentence — it
		// NAMES the resolved path, so an operator reads which PATH entry to fix. Every other
		// resolver failure is an absent git and becomes the graceful message (§3a); the
		// mapping lives here rather than in Program because the query entries consume this
		// function directly and would otherwise emit the raw resolver wording.
		if errors.Is(err, security.ErrExecFromWritablePath) {
			return "", err
		}
		return "", errors.New(UnavailableMessage)
	}
	return path, nil
}

// confinementBox returns the box a confined call runs inside, or nil when no Confinement handle
// rides on ctx — the gated/unconfined case, where the workspace root is the whole fence. It is
// the small read security.ResolveProgram needs so bytes the model was allowed to write can never
// become the git this package launches.
func confinementBox(ctx context.Context) *domain.ConfinementBox {
	if conf, ok := domain.ConfinementFromContext(ctx); ok {
		return &conf.Box
	}
	return nil
}

// Capture runs git with args in root under the per-call timeout and the scrubbed environment,
// honouring the confinement handle the disposition installed (if any), and returns the CAPTURED
// OUTCOME — exit code, output and all — for a caller that shows the model what git printed.
// Every tool invocation goes through here, so this is where hardeningOptions and hardeningEnv
// are applied: a caller cannot forget them, and a git tool added later inherits them by
// construction. A missing git is signalled by the caller's Resolve, not here. The Go error is
// non-nil only for ctx cancellation or a confinement-unavailable demotion (the subprocess
// contract).
//
// It is also the choke point for the repo-local command-config refusal: a repository whose own
// config names a program git would execute gets no git call at all, on the read paths as much as
// the write ones (a clean filter runs on a plain diff or status, a pager on a plain log). The
// refusal is returned as an ordinary failed outcome — non-zero exit, the model-facing sentence
// as its output — so every caller's existing "git ... failed" branch surfaces it verbatim, with
// no signature for a future git tool to forget to handle.
//
// The probe behind that refusal is memoised per repository (commandConfigProbes), so it costs
// its two subprocesses once rather than on every git call.
func Capture(ctx context.Context, gitPath, root string, timeout time.Duration, args ...string) (subprocess.SubprocessResult, error) {
	drivers, err := probeCommandConfig(ctx, gitPath, root, nil)
	if err != nil {
		return subprocess.SubprocessResult{}, err
	}
	if len(drivers) > 0 {
		return subprocess.SubprocessResult{CombinedOutput: CommandConfigRefusal(drivers), ExitCode: 1}, nil
	}
	return CaptureUnchecked(ctx, gitPath, root, nil, timeout, args...)
}

// CaptureUnchecked is Capture without the command-config probe. Only two callers may use it:
// this package's own probe — which must reach git to ASK about the config, and whose own
// invocation (`git config --get-regexp`) executes no configured program — and a test asserting
// what the probe itself would see. Everything a MODEL causes goes through Capture.
func CaptureUnchecked(ctx context.Context, gitPath, root string, env []string, timeout time.Duration, args ...string) (subprocess.SubprocessResult, error) {
	return subprocess.RunSubprocess(ctx, runSpec(gitPath, root, env, timeout, false, args...))
}

// runSpec builds the subprocess spec for one git invocation in root: hardeningOptions ahead of
// the subcommand, and the PATH-scoped allowlist environment with hardeningEnv and then the
// caller's own env appended. It is the one place the hardening is composed, so every entry point
// into the funnel — the git TOOLS through Capture, the engine's own read-side git through Query,
// the snapshot store's GIT_DIR runs through Run — carries the identical argv and environment and
// none can drift from the others.
//
// The caller's env comes LAST because it is an addition, never a removal: a GIT_DIR or
// GIT_INDEX_FILE that redirects the run to a store of apogee's own overrides nothing the
// hardening put there, and no entry it carries can drop GIT_CONFIG_NOSYSTEM.
func runSpec(gitPath, root string, env []string, timeout time.Duration, splitStdout bool, args ...string) subprocess.SubprocessSpec {
	argv := make([]string, 0, 1+len(hardeningOptions)+len(args))
	argv = append(argv, gitPath)
	argv = append(argv, hardeningOptions...)
	argv = append(argv, args...)

	full := SafeEnv(root)
	full = append(full[:len(full):len(full)], hardeningEnv...)
	full = append(full, env...)

	return subprocess.SubprocessSpec{
		Argv:        argv,
		Dir:         root,
		Timeout:     timeout,
		Env:         full,
		SplitStdout: splitStdout,
	}
}

// Run resolves the system git and runs one read-side git command in dir, returning its standard
// output alone. It is the funnel entry for apogee's own bookkeeping git — the tracked-file
// mutation floor (internal/agent/treesnapshot.go) and the session-owned snapshot store — so that
// git gets everything a git TOOL's git gets: the exec fence on the resolved binary,
// hardeningOptions, GIT_CONFIG_NOSYSTEM, the allowlisted workspace-scoped environment (no
// APOGEE_API_KEY, no inherited config redirection), the repo-local command-config refusal, and
// the §2.4 process-tree teardown.
//
// env is appended to that environment and is how a caller redirects the run to an object
// database of its own (GIT_DIR, GIT_WORK_TREE, GIT_INDEX_FILE); a nil env is the plain
// workspace read. The probe that decides the refusal runs under the SAME env, so a run pointed
// at apogee's own store is judged by that store's config rather than by the workspace's.
//
// Every failure is ONE error and they are deliberately not distinguished: git absent, a fenced
// binary, a refused repository, a non-zero exit, a timeout, a wedged drain and a cancelled ctx
// all mean "no trustworthy answer", which is precisely what the caller acts on — the floor's
// contract is that any failure skips the check silently for that call (ADR 0056 decision 4).
//
// It is NOT for tool results. A tool shows the model what git printed, exit code and stderr
// included, so the git tools take Capture's captured outcome; this returns stdout as DATA, with
// the diagnostics left out of the payload.
func Run(ctx context.Context, dir string, env []string, timeout time.Duration, args ...string) (string, error) {
	gitPath, err := Resolve(ctx, dir, LookPath)
	if err != nil {
		return "", err
	}
	return Query(ctx, gitPath, dir, env, timeout, args...)
}

// RunTo is Run with the child's standard output streamed UNCAPPED into stdout instead of
// returned as a capped string. It exists for the caller whose payload IS the child's output — a
// blob read out of an object database, a whole-tree diff listing — where truncation at
// subprocess.MaxSubprocessOutputBytes would silently corrupt the answer. Every other guarantee
// is Run's: the same resolution, the same hardening, the same probe under the same env, the same
// teardown, and stderr still capped. A nil stdout discards the payload.
func RunTo(ctx context.Context, dir string, env []string, timeout time.Duration, stdout io.Writer, args ...string) error {
	gitPath, err := Resolve(ctx, dir, LookPath)
	if err != nil {
		return err
	}
	_, err = query(ctx, gitPath, dir, env, timeout, stdout, args...)
	return err
}

// Query is Run for a caller that has already resolved (and fenced) its own git — the tools
// package, whose lookup seam is its own package var. It is Run minus the resolution: the probe,
// the hardening and the stdout-as-data contract are identical.
func Query(ctx context.Context, gitPath, dir string, env []string, timeout time.Duration, args ...string) (string, error) {
	return query(ctx, gitPath, dir, env, timeout, nil, args...)
}

// query is the body Run, RunTo and Query share. A non-nil stdout takes the child's standard
// output uncapped and leaves the returned string empty.
func query(ctx context.Context, gitPath, dir string, env []string, timeout time.Duration, stdout io.Writer, args ...string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("apogee: gitexec: no git subcommand")
	}
	drivers, err := probeCommandConfig(ctx, gitPath, dir, env)
	if err != nil {
		return "", err
	}
	if len(drivers) > 0 {
		return "", errors.New(CommandConfigRefusal(drivers))
	}

	spec := runSpec(gitPath, dir, env, timeout, stdout == nil, args...)
	var res subprocess.SubprocessResult
	if stdout != nil {
		res, err = subprocess.RunSubprocessTo(ctx, spec, stdout)
	} else {
		res, err = subprocess.RunSubprocess(ctx, spec)
	}
	if err != nil {
		return "", err
	}
	switch {
	case res.TimedOut:
		return "", fmt.Errorf("git %s: timed out after %s", args[0], timeout)
	case res.DrainWedged:
		return "", fmt.Errorf("git %s: output drain wedged", args[0])
	case res.ExitCode != 0:
		return "", fmt.Errorf("git %s: exit %d: %s", args[0], res.ExitCode, strings.TrimSpace(res.CombinedOutput))
	}
	return res.Stdout, nil
}

// CommandConfigName matches every config name whose VALUE is a program git executes — an
// sshCommand, an editor, a pager, an askpass, a filter or merge or diff driver, a credential
// helper, a gpg.program, a proxy command. A key that merely SELECTS one of those (a
// filter.<driver>.required, a .gitattributes attribute) names no program and runs nothing on its
// own, so it is deliberately absent.
//
// Two classes are deliberately absent for their own reasons. core.hooksPath and core.fsmonitor
// are neutralised by hardeningOptions above, so refusing them as well would cost a repository
// its git tools for a key that can no longer reach a program. alias.* cannot shadow a builtin
// subcommand, and every subcommand these callers invoke is a builtin, so a repo-local alias
// never changes what apogee's own argv runs.
//
// The source string must stay POSIX-ERE-compatible — plain ( ) groups, no (?: — because it is
// handed to git verbatim and git's --get-regexp compiles it with regcomp; the same string is
// kept here to re-check what came back, since git's combined output can carry a warning line the
// listing never intended as a name.
var CommandConfigName = regexp.MustCompile(`^(core\.(sshcommand|editor|pager|askpass|gitproxy|alternaterefscommand)|sequence\.editor|diff\.external|diff\..*\.(command|textconv)|merge\..*\.driver|mergetool\..*\.cmd|difftool\..*\.cmd|filter\..*\.(clean|smudge|process)|credential\.helper|credential\..*\.helper|gpg\.program|gpg\..*\.program|uploadpack\.packobjectshook|remote\..*\.proxy|pager\..*)$`)

// FilterConfigScopes are the config scopes a command-valued key is refused from — the
// REPOSITORY's own files, which is what the workspace bytes can carry. --local is .git/config
// (with whatever it include.path-s, since git resolves the includes for us); --worktree is the
// per-worktree file, read only where the worktreeConfig extension is on and otherwise either a
// duplicate of --local or an outright error. The operator's --global and --system scopes are
// deliberately absent: that config is theirs, on the same trust boundary the hardeningEnv
// comment draws.
var FilterConfigScopes = []string{"--local", "--worktree"}

// maxNamedCommandKeys caps how many config keys a refusal names, so a repository that defines
// a thousand of them cannot turn the refusal sentence into the whole result.
const maxNamedCommandKeys = 5

// commandConfigProbes memoises the repo-local command-config probe for the process lifetime,
// keyed by the git binary, the repository root AND the caller's extra environment together — the
// same root probed with a different git is a different question, a Driver may hold several roots
// at once, and a run redirected by GIT_DIR asks about a DIFFERENT repository than the plain run
// in the same directory does. Each value is the probed name slice, which every reader treats as
// read-only.
//
// Why memoise at all: Capture is the choke point EVERY git tool call passes through, and the
// probe costs two subprocesses of its own — so probing per call tripled the process cost of every
// git tool (one git_commit ran its four real git commands behind twelve git processes).
//
// The accepted staleness, deliberate: a repository's config is probed once and never re-read. A
// command-valued key added to that config after the first probe is not refused until apogee
// restarts, and one removed keeps refusing just as long. The threat this refusal answers is an
// attacker-AUTHORED checkout — hostile in its config before apogee ever opens it — not a config
// edited underneath a running session, so a per-session answer is the right granularity.
//
// Two goroutines probing the same not-yet-probed key both run the probe and store the same
// answer; the duplicated work is wasted, never the outcome.
var commandConfigProbes sync.Map

// probeCommandConfig returns repoLocalCommandConfig's answer for the repository the run at root
// under env would actually reach, going to git only the first time it is asked about a given
// (gitPath, root, env) triple.
//
// A FAILED probe is never cached. Its error is the subprocess contract's — ctx cancellation or a
// confinement-unavailable demotion — which says nothing about the repository, so caching it
// would let one cancelled Turn refuse every later git call on that root.
func probeCommandConfig(ctx context.Context, gitPath, root string, env []string) ([]string, error) {
	key := gitPath + "\x00" + root + "\x00" + strings.Join(env, "\x00")
	if cached, ok := commandConfigProbes.Load(key); ok {
		return cached.([]string), nil
	}
	names, err := repoLocalCommandConfig(ctx, gitPath, root, env)
	if err != nil {
		return nil, err
	}
	commandConfigProbes.Store(key, names)
	return names, nil
}

// repoLocalCommandConfig lists the repo-local config names whose VALUE is a program git would
// execute (CommandConfigName) for the repository the run at root under env reaches, by asking
// git itself rather than parsing .git/config — git is the only thing that agrees with git about
// includes, casing and quoting. Listing config executes none of them: a filter fires on
// add/checkout/diff and a credential helper on a network call, never on `git config`, and
// --name-only keeps an attacker-chosen VALUE out of the output entirely.
//
// A non-zero exit is the pass case, not an error: git exits 1 when nothing matched and 128 when
// the scope does not apply at all (root is no repository; --worktree on a git that refuses the
// option). The error return is the subprocess contract's — ctx cancellation or a
// confinement-unavailable demotion — and it stops the caller, so a probe that could not run
// never lets the real command run un-probed.
func repoLocalCommandConfig(ctx context.Context, gitPath, root string, env []string) ([]string, error) {
	var names []string
	seen := make(map[string]struct{})
	for _, scope := range FilterConfigScopes {
		res, err := CaptureUnchecked(ctx, gitPath, root, env, probeTimeout,
			"config", scope, "--name-only", "--get-regexp", CommandConfigName.String())
		if err != nil {
			return nil, err
		}
		if res.ExitCode != 0 {
			continue
		}
		for _, line := range strings.Split(res.CombinedOutput, "\n") {
			name := strings.TrimSpace(line)
			if !CommandConfigName.MatchString(name) {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	return names, nil
}

// CommandConfigRefusal is the model-facing sentence for a repository whose own config names a
// program git would execute. Like the exec fence's refusal it NAMES what it refused — a message
// that only said "refused" would send the model retrying the same call — and states the rule,
// including the half that still works, so the operator's own global config is not read as broken.
func CommandConfigRefusal(names []string) string {
	shown, extra := names, ""
	if len(names) > maxNamedCommandKeys {
		shown = names[:maxNamedCommandKeys]
		extra = fmt.Sprintf(" (+%d more)", len(names)-maxNamedCommandKeys)
	}
	return fmt.Sprintf("git refused: this repository's own config names a program git would execute (%s%s). "+
		"Repo-local command-valued keys are refused for every git tool; the operator's global git config is untouched and still applies.",
		strings.Join(shown, ", "), extra)
}
