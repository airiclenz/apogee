package gitexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
// The probe behind that refusal is memoised per repository (commandConfigProbes) and re-run
// only when a file that decided its answer changes, so it costs its three subprocesses once per
// config rather than on every git call.
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

// CaptureUnchecked is Capture without the command-config probe. Only one caller may use it: a
// test asserting what the probe itself would see (internal/tools' runGitUnchecked). The probe's
// own invocations — which must reach git to ASK about the config, and execute no configured
// program — go through probeGit instead, which keeps git's diagnostics out of the listing it
// parses. Everything a MODEL causes goes through Capture.
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
// package, whose git tools resolve through the execHost they were built with rather than
// LookPath. It is Run minus the resolution: the probe, the hardening and the stdout-as-data
// contract are identical.
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
// The source string must stay POSIX-ERE-compatible — plain ( ) groups, no (?: — because the
// tools tests hand it to git verbatim (`git config --get-regexp`, internal/tools/git_test.go's
// global-scope guards), where git compiles it with regcomp; the probe itself lists every key and
// re-checks each against this compiled form. That source is [security.GitCommandConfigNameSource]: the
// shell write view builds its own [security.GitCommandConfigName] from it — the same names PLUS
// core.hookspath, because a `git config` line that sets the key is a write into .git/config
// whatever the hardening options do to it afterwards — and this package imports security, never
// the reverse, so the one string lives there.
var CommandConfigName = regexp.MustCompile(`^(` + security.GitCommandConfigNameSource + `)$`)

// FilterConfigScopes are the config scopes a command-valued key is refused from — the
// REPOSITORY's own files, which is what the workspace bytes can carry. --local is .git/config
// with whatever it include.path-s — followed only because the probe passes --includes, which a
// scoped read leaves off by default, so the refusal reaches a key an included file carries and
// the include's own path joins the fingerprinted set; --worktree is the
// per-worktree file, read only where the worktreeConfig extension is on and otherwise either a
// duplicate of --local or an outright error. The operator's --global and --system scopes are
// deliberately absent: that config is theirs, on the same trust boundary the hardeningEnv
// comment draws.
var FilterConfigScopes = []string{"--local", "--worktree"}

// maxNamedCommandKeys caps how many config keys a refusal names, so a repository that defines
// a thousand of them cannot turn the refusal sentence into the whole result.
const maxNamedCommandKeys = 5

// commandConfigProbes memoises the repo-local command-config probe, keyed by the git binary, the
// repository root AND the caller's extra environment together — the same root probed with a
// different git is a different question, a Driver may hold several roots at once, and a run
// redirected by GIT_DIR asks about a DIFFERENT repository than the plain run in the same
// directory does. Each value is a commandConfigProbe, which every reader treats as read-only.
//
// Why memoise at all: Capture is the choke point EVERY git tool call passes through, and the
// probe costs three subprocesses of its own — so probing per call would multiply the process
// cost of every git tool (a git_commit runs four real git commands).
//
// Why the memo is keyed on the config's identity rather than on the process: the writer that
// matters never goes through this package. A confined terminal running an opaque program can
// write a filter.<x>.clean into .git/config after the first probe — a write the shell write view
// cannot see — and a memo held for the process lifetime would serve the stale clean answer to the
// next git TOOL, unconfined and on the host, whose add or diff then executes it. So each answer
// carries the fingerprints (fileprint) of the files that DECIDED it — the scope files, HEAD and
// every include they name, present or absent — and probeCommandConfig re-stats them on every
// call, serving the cache only while every print still matches. Invalidating after the
// write-capable calls that pass through this package instead would miss exactly that writer.
//
// Two goroutines probing the same not-yet-probed key both run the probe and store the same
// answer; the duplicated work is wasted, never the outcome.
var commandConfigProbes sync.Map

// commandConfigProbe is one memoised answer: the command-valued names the repository's own
// config carries, and the prints of the files that decided them.
type commandConfigProbe struct {
	names  []string
	prints []fileprint
}

// holds reports whether every file the answer depends on still prints as it did when the probe
// ran. No prints at all — git named no file, as a fake git in a test does — holds trivially.
func (p commandConfigProbe) holds() bool {
	for _, print := range p.prints {
		if !print.holds() {
			return false
		}
	}
	return true
}

// fileprint is the identity of one file the probe's answer depends on, as os.Stat reports it:
// size, modification time, mode and the inode/device pair os.SameFile compares — so an edit in
// place, a rename over the file and a truncation each read as a change whatever the clock's
// granularity. An absent file is a print of its own: git skips a missing include silently, so
// the file the model creates later must be watched from the start. A stat that failed for any
// other reason keeps no info and never matches, which re-probes.
type fileprint struct {
	path   string
	absent bool
	info   os.FileInfo
}

// takeFileprint stats path into its print.
func takeFileprint(path string) fileprint {
	info, err := os.Stat(path)
	switch {
	case err == nil:
		return fileprint{path: path, info: info}
	case errors.Is(err, os.ErrNotExist):
		return fileprint{path: path, absent: true}
	default:
		return fileprint{path: path}
	}
}

// holds re-stats the file and reports whether it still prints as it did.
func (p fileprint) holds() bool {
	now := takeFileprint(p.path)
	if p.absent || now.absent {
		return p.absent && now.absent
	}
	if p.info == nil || now.info == nil {
		return false
	}
	return os.SameFile(p.info, now.info) &&
		p.info.Size() == now.info.Size() &&
		p.info.ModTime().Equal(now.info.ModTime()) &&
		p.info.Mode() == now.info.Mode()
}

// probeCommandConfig returns repoLocalCommandConfig's answer for the repository the run at root
// under env would actually reach, going to git only when the (gitPath, root, env) triple has no
// memoised answer or a file that decided the memoised one has changed since.
//
// A FAILED probe is never cached. Its error is the subprocess contract's — ctx cancellation or a
// confinement-unavailable demotion — which says nothing about the repository, so caching it
// would let one cancelled Turn refuse every later git call on that root.
func probeCommandConfig(ctx context.Context, gitPath, root string, env []string) ([]string, error) {
	key := gitPath + "\x00" + root + "\x00" + strings.Join(env, "\x00")
	if cached, ok := commandConfigProbes.Load(key); ok {
		if probe := cached.(commandConfigProbe); probe.holds() {
			return probe.names, nil
		}
	}
	probe, err := repoLocalCommandConfig(ctx, gitPath, root, env)
	if err != nil {
		return nil, err
	}
	commandConfigProbes.Store(key, probe)
	return probe.names, nil
}

// probeGit runs one of the probe's own git invocations with stdout split from the diagnostics,
// so a warning git prints can never pose as a record of the listing.
func probeGit(ctx context.Context, gitPath, root string, env []string, args ...string) (subprocess.SubprocessResult, error) {
	return subprocess.RunSubprocess(ctx, runSpec(gitPath, root, env, probeTimeout, true, args...))
}

// repoLocalCommandConfig lists the repo-local config names whose VALUE is a program git would
// execute (CommandConfigName) for the repository the run at root under env reaches, together
// with the prints of the files that decided the answer, by asking git itself rather than
// parsing .git/config — git is the only thing that agrees with git about includes, casing and
// quoting. Listing config executes none of them: a filter fires on add/checkout/diff and a
// credential helper on a network call, never on `git config`. One listing per scope yields the
// names, the include paths and the origins in one subprocess; its -z framing keeps an
// attacker-chosen VALUE from posing as a key (parseConfigListing).
//
// The scope files are printed BEFORE the listings, so a write racing a listing shows up as a
// mismatch on the next call rather than as a stale hit; the includes are only known afterwards.
//
// A non-zero exit is the pass case, not an error: git exits 128 when the scope does not apply
// at all (root is no repository; --worktree on a git that refuses the option). The error return
// is the subprocess contract's — ctx cancellation or a confinement-unavailable demotion — and it
// stops the caller, so a probe that could not run never lets the real command run un-probed.
func repoLocalCommandConfig(ctx context.Context, gitPath, root string, env []string) (commandConfigProbe, error) {
	files, top, err := configFiles(ctx, gitPath, root, env)
	if err != nil {
		return commandConfigProbe{}, err
	}
	var probe commandConfigProbe
	for _, file := range files {
		probe.prints = append(probe.prints, takeFileprint(file))
	}

	var names, includes orderedSet
	for _, scope := range FilterConfigScopes {
		res, err := probeGit(ctx, gitPath, root, env,
			"config", scope, "--includes", "--show-origin", "--list", "-z")
		if err != nil {
			return commandConfigProbe{}, err
		}
		if res.ExitCode != 0 {
			continue
		}
		for _, entry := range parseConfigListing(res.Stdout) {
			switch {
			case CommandConfigName.MatchString(entry.key):
				names.add(entry.key)
			case isIncludeKey(entry.key) && entry.value != "":
				includes.add(resolveInclude(top, entry.origin, entry.value))
			}
		}
	}

	probe.names = names.list
	for _, path := range includes.list {
		probe.prints = append(probe.prints, takeFileprint(path))
	}
	return probe, nil
}

// configFiles asks git which files carry the repository's own config for the run at root under
// env — the --local and --worktree files, and HEAD, which decides whether an
// includeIf.onbranch:<b>.path include is followed — plus the work-tree top the listing's relative
// origins are spelled from. No .git/ path is assumed: a run redirected by GIT_DIR names that
// store's files and is fingerprinted by its own config. --show-toplevel comes LAST because it
// dies without a work tree (a bare store) after the paths have been printed, so the exit code is
// not consulted and the paths are read from whatever stdout holds: a relative path is relative to
// root, a missing top (that bare store, whose origins are absolute anyway) falls back to root,
// and a git that printed nothing names no file — and so no print, never root itself.
func configFiles(ctx context.Context, gitPath, root string, env []string) (files []string, top string, err error) {
	res, err := probeGit(ctx, gitPath, root, env, "rev-parse",
		"--git-path", "config", "--git-path", "config.worktree", "--git-path", "HEAD", "--show-toplevel")
	if err != nil {
		return nil, "", err
	}
	var lines []string
	for _, line := range strings.Split(res.Stdout, "\n") {
		if line = strings.TrimRight(line, "\r"); line != "" {
			lines = append(lines, line)
		}
	}
	top = root
	if len(lines) > configFileCount {
		top = lines[configFileCount]
		lines = lines[:configFileCount]
	}
	for _, line := range lines {
		if !filepath.IsAbs(line) {
			line = filepath.Join(root, line)
		}
		files = append(files, filepath.Clean(line))
	}
	return files, top, nil
}

// configFileCount is how many --git-path answers configFiles asks rev-parse for ahead of the
// work-tree top.
const configFileCount = 3

// configEntry is one record of a `git config --show-origin --list -z` listing: the file it came
// from as git spells it — relative to the work-tree top, or absolute under GIT_DIR — the key as
// git canonicalises it, and the value.
type configEntry struct {
	origin, key, value string
}

// parseConfigListing cuts a -z listing into its records: <origin>\0<key>\n<value>\0, or
// <origin>\0<key>\0 for a valueless key. Each record is cut at its FIRST newline — a value may
// hold newlines of its own — and the NUL framing is what keeps an attacker-chosen value from
// posing as a key. The bytes after the final NUL are never a record: nothing for a complete
// listing, a partial record plus the capped path's "[output truncated" marker for one that
// overran subprocess.MaxSubprocessOutputBytes, and a fake git's free-form echo in a test — all
// dropped, as is an origin that is no file (a `command line:` override).
func parseConfigListing(listing string) []configEntry {
	tokens := strings.Split(listing, "\x00")
	tokens = tokens[:len(tokens)-1]
	entries := make([]configEntry, 0, len(tokens)/2)
	for i := 0; i+1 < len(tokens); i += 2 {
		origin, ok := strings.CutPrefix(tokens[i], "file:")
		if !ok {
			continue
		}
		key, value, _ := strings.Cut(tokens[i+1], "\n")
		entries = append(entries, configEntry{origin: origin, key: key, value: value})
	}
	return entries
}

// isIncludeKey reports whether key is an include.path or includeIf.<condition>.path key — the
// two forms git follows into another file — as the listing lowercases them.
func isIncludeKey(key string) bool {
	return key == "include.path" ||
		(strings.HasPrefix(key, "includeif.") && strings.HasSuffix(key, ".path"))
}

// resolveInclude turns an include value into the path git opens for it: a leading ~/ is the
// home directory, a relative path is relative to the config file that carries it, and a
// relative origin is itself spelled from top. A nested include is listed with the included file
// as its origin, so it resolves the same way.
func resolveInclude(top, origin, value string) string {
	if rest, ok := strings.CutPrefix(value, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	carrier := origin
	if !filepath.IsAbs(carrier) {
		carrier = filepath.Join(top, carrier)
	}
	return filepath.Join(filepath.Dir(carrier), value)
}

// orderedSet collects strings once each, in first-seen order — the probe's names and include
// paths, which a repository may repeat across scopes.
type orderedSet struct {
	seen map[string]struct{}
	list []string
}

// add records value unless it was seen before.
func (s *orderedSet) add(value string) {
	if s.seen == nil {
		s.seen = make(map[string]struct{})
	}
	if _, dup := s.seen[value]; dup {
		return
	}
	s.seen[value] = struct{}{}
	s.list = append(s.list, value)
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
