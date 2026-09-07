package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/subprocess"
)

// maxSubprocessOutputBytes caps the combined stdout+stderr a subprocess call surfaces to the
// model — a noisy command cannot flood the context window. It is the core's ceiling under this
// package's own name, so the Console family's separate truncation (console_common.go) is measured
// against the very same number the funnel enforces rather than a second copy of it.
const maxSubprocessOutputBytes = subprocess.MaxSubprocessOutputBytes

// subprocessSpec is the platform-agnostic description of one subprocess execution: the argv
// to run, the working directory, the per-call timeout, and the optional stdin. The execution
// tools (terminal, python-exec) build a spec and hand it to runSubprocess, which owns the
// confinement handoff and the process-group teardown so each tool stays a thin front-end.
type subprocessSpec struct {
	// argv is the fully-resolved command and arguments (argv[0] is the program). It is
	// never empty when a tool calls runSubprocess.
	argv []string
	// dir is the working directory; empty means the process inherits the caller's.
	dir string
	// timeout bounds the run; zero means subprocess.DefaultSubprocessTimeout.
	timeout time.Duration
	// stdin, when non-empty, is fed to the process on its standard input.
	stdin string
	// env, when non-nil, is the exact environment the process runs with (each entry
	// "KEY=value"); nil means it inherits the caller's environment. EVERY tool that runs
	// something for the MODEL sets it — none of them inherits whole: git and the Go toolchain
	// take an allowlist scoped by platform.Host.ScopeEnv; the shell and interpreter tools take
	// subprocessEnvScopedPath() — the caller's environment minus every credential variable
	// (apogee's own and the host-configured ones), with the child's PATH scoped out of the
	// workspace; and the test runner takes subprocessEnv(), the same minus the credentials,
	// because a test suite needs the toolchain variables its user's shell has but no subprocess
	// of the model's needs apogee's key.
	env []string
	// splitStdout asks for the child's standard output to be captured ON ITS OWN
	// (subprocessResult.stdout) instead of interleaved with stderr. A caller that CONSUMES the
	// output as a payload sets it — a caller splicing a child's stdout into a file it will
	// write needs it clean, since a diagnostic in the middle of that would land in the file as
	// if it were code. The execution tools leave it false: they SHOW the model what a command
	// printed, and the interleaved order is the truthful one there.
	splitStdout bool
	// cmdline, when non-empty, is the verbatim process command line to launch argv with
	// instead of letting os/exec join it (platform.Shell.CommandLine). It is empty on
	// POSIX and for any argv that is a real argv; a tool handing a SHELL LINE to
	// cmd.exe on Windows sets it, because os/exec's argv joining mangles the quotes the
	// shell needs (internal/subprocess/cmdline_other.go).
	cmdline string
	// failFast reports that the caller prepended platform.FailFastPreamble to the line it is
	// running, so a non-zero exit may be the preamble aborting the script at its first failed
	// command rather than the line as a whole finishing badly. It rides through onto the
	// result, where subprocessToolResult says so on the exit-code line — the model acts on the
	// last tool result, not on a system-prompt line from a dozen calls earlier. Only the
	// terminal's POSIX branch sets it: python_exec, git and the Console family prepend nothing.
	failFast bool
}

// apogeeSecretEnvVars names the environment variables that carry apogee's OWN credentials. A
// subprocess launched for the MODEL — a shell command line, a Python snippet — has no business
// reading the key apogee talks to its inference server with: the model chooses what that
// subprocess does, and an exfiltration from inside the box is one request away (the issue
// register's `apogee-8wy` L3 accepts that reading is possible; it does not oblige apogee to hand
// over its own secrets).
//
// The name is a literal rather than internal/config's EnvAPIKey because internal/config imports
// THIS package (its tool-name reconciliation), so the dependency cannot point back — which is
// also why the CONFIGURED names reach the tools as plain strings from the host rather than being
// read from config here. Those names are the reason this list is only HALF the scrub: a server
// entry's key was file-only when the list was written ("APIKey is FILE-ONLY on purpose"), but
// `api-key-env:` (ADR 0047) lets an entry name an ENVIRONMENT VARIABLE instead, and a variable
// the operator exported is inherited by every subprocess unless it is dropped too. The host names
// those variables (HostTools.SecretEnvVars) and isSecretEnv drops them beside this list.
var apogeeSecretEnvVars = []string{"APOGEE_API_KEY"}

// subprocessEnv returns the environment an execution tool's subprocess runs with: everything
// the caller inherited MINUS the credentials it must not see — apogee's own, plus the
// host-configured secretEnv names (nil ⇒ apogee's own alone) — plus each extra "KEY=value" entry
// appended. Appended, so it wins over an inherited spelling of the same key, which is how
// every exec implementation resolves a duplicate.
//
// It is deliberately NOT git's allowlist (safeEnvKeys): the shell and interpreter tools run
// what the operator asked for in the developer environment they expect to be in, and an
// allowlist there would break ordinary tooling. What is removed is only what apogee itself put
// there, and what the operator told apogee its own keys are called.
func subprocessEnv(secretEnv []string, extra ...string) []string {
	inherited := os.Environ()
	env := make([]string, 0, len(inherited)+len(extra))
	for _, entry := range inherited {
		if isSecretEnv(entry, secretEnv) {
			continue
		}
		env = append(env, entry)
	}
	return append(env, extra...)
}

// subprocessEnvScopedPath returns subprocessEnv's environment with one further scrub applied to
// the inherited half: the child's PATH drops every entry that lies inside workspaceRoot and every
// entry that is not an absolute location (platform.Shell.ScopeInheritedEnv).
//
// It is what the tools handing the MODEL a shell or an interpreter take. They inherit the
// operator's environment because that is the developer tooling they exist to run, but a
// workspace-resident PATH entry — an activated .venv, node_modules/.bin — would otherwise let
// bytes the model wrote become the `git`, the `ssh` or the `curl` that the subprocess, or
// anything it spawns, resolves for itself: the plant-then-exec chain apogee refuses at its own
// resolution sites (security.ResolveProgram) and cannot check inside somebody else's process.
//
// The extras are appended AFTER the scoping — they are apogee's own additions rather than
// inherited values, and appending keeps them last-wins in the child, which is how every exec
// implementation resolves a duplicate.
func subprocessEnvScopedPath(workspaceRoot string, secretEnv []string, extra ...string) []string {
	return append(shellHost.ScopeInheritedEnv(workspaceRoot, subprocessEnv(secretEnv)), extra...)
}

// isSecretEnv reports whether a "KEY=value" entry names a credential no subprocess the model
// steers may see: one of apogee's own (isApogeeSecretEnv) or one of the configured names the
// host supplied — the variables its `api-key-env:` key sources read (ADR 0047), which arrive as
// plain strings because internal/config imports this package and the dependency cannot point
// back. They are compared the same case-insensitive way, for the same reason.
//
// An empty or whitespace-only configured name matches nothing: it is a blank entry in somebody's
// configuration, never permission to drop every variable in the environment.
func isSecretEnv(entry string, configured []string) bool {
	if isApogeeSecretEnv(entry) {
		return true
	}
	key, _, ok := strings.Cut(entry, "=")
	if !ok {
		return false
	}
	for _, configuredName := range configured {
		if name := strings.TrimSpace(configuredName); name != "" && strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}

// isApogeeSecretEnv reports whether a "KEY=value" entry names one of apogee's own credentials —
// the fixed half of the scrub, which every execution tool drops whatever the host configured.
// The name comparison is case-insensitive because Windows environment names are: APOGEE_API_KEY
// and Apogee_Api_Key are one variable there. On POSIX they are two, and dropping both is the
// safe direction — a lower-cased spelling is one apogee never reads anyway.
func isApogeeSecretEnv(entry string) bool {
	key, _, ok := strings.Cut(entry, "=")
	if !ok {
		return false
	}
	for _, secret := range apogeeSecretEnvVars {
		if strings.EqualFold(key, secret) {
			return true
		}
	}
	return false
}

// subprocessResult is the captured outcome of one subprocess execution.
type subprocessResult struct {
	// combinedOutput is stdout and stderr interleaved (capped), what the model reads. A spec
	// that split the streams (splitStdout) leaves it holding stderr ALONE — that caller took
	// the child's stdout as data, so what remains here is only what the command complained.
	combinedOutput string
	// stdout is the child's standard output alone (capped), captured only when the spec set
	// splitStdout; it is empty for every caller that reads combinedOutput.
	stdout string
	// exitCode is the process exit status; 0 on success, the child's code on a clean
	// non-zero exit, and -1 when the process was killed by a signal (e.g. a timeout).
	exitCode int
	// timedOut reports that the run was cut short by its own timeout (vs the model's ctx).
	timedOut bool
	// drainWedged reports that the process had exited but something it left running was still
	// holding the output pipe when platform.ProcessWaitDelay expired, so exec cut the drain
	// short and killed what was left. The captured output may be missing its tail, and the run
	// is not a success however cleanly the leader itself exited.
	drainWedged bool
	// confined reports that the run actually executed inside the confinement fence — a
	// Confinement handle was on ctx and its Confiner wrapped the cmd before it started.
	// subprocessToolResult keys on it to label a likely OS denial (EPERM-shaped output on a
	// failed confined run) so the model learns WHY a write outside the box failed; an
	// unconfined run must never carry that label, however EPERM-shaped its output.
	confined bool
	// box is the confinement policy the run actually executed under, carried through so the
	// denial labels can name the writable roots BY PATH instead of describing them. It is the
	// zero box on an unconfined run, where no label is rendered at all.
	box domain.ConfinementBox
	// denialStopped reports that the live kill-on-denial watch on a CONFINED run matched an
	// OS-denial signature and issued the process-group kill (fix A of the 2026-08-22
	// workspace-clobber incident). subprocessToolResult keys on it for the definitive
	// stopped-by-confinement label — but only on a non-zero exit: a run that still finished
	// cleanly (the match landed after the process was already done, or matched output that
	// was not a fatal denial) keeps its success result untouched.
	denialStopped bool
	// failFast carries the spec's failFast through to the rendering: the run was launched under
	// the fail-fast preamble, so subprocessToolResult can tell the model that a non-zero exit
	// stopped the rest of the line. It says nothing about whether the preamble actually fired —
	// a line whose LAST command failed exits the same way — so the note it drives is worded as
	// the mode that was in force, not as a verdict on which command failed.
	failFast bool
}

// confinementDenialLabel is the line appended to a FAILED confined result whose output looks
// like an OS confinement denial. It NAMES the roots the run may write to, because a model that
// is only told a fence exists has nowhere to put the file: the paths are what let it route the
// write instead of treating the EPERM as a broken command and blindly retrying around it.
func confinementDenialLabel(box domain.ConfinementBox) string {
	return "[likely blocked by workspace confinement: writes are allowed only inside " +
		confinementWritableRoots(box) + "]"
}

// confinementDenialStopLabel is the line appended when the live kill-on-denial watch stopped
// the run itself (subprocessResult.denialStopped, console.Console.DenialStopped): stronger than
// the "likely" label above, because here the harness matched the denial as it streamed and
// killed the process group, so the model is told plainly that the rest of its script did not
// run. It names the writable roots for the same reason that one does — the model's next act is
// to re-aim the write, and it can only do that against real paths. The OS-denial spellings both
// labels key on live in internal/platform (platform.LooksLikeConfinementDenial), which is also
// what the watch scans with.
//
// Both labels sit beside the funnel rather than beside one tool: the one-shot execution tools
// read them off subprocessResult and the Console family reads the stop label off a live
// Console, and there is one wording for the fence however the model met it.
func confinementDenialStopLabel(box domain.ConfinementBox) string {
	return "[blocked by workspace confinement: an operation was denied, so the command was" +
		" stopped; writes are allowed only inside " + confinementWritableRoots(box) + "]"
}

// confinementWritableRoots renders the box's writable roots as the tail both denial labels end
// with: the workspace by path, then every extra writable path the box carries — the session
// scratch dir among them, which Config.ConfinementBox already folds in. A box naming no root at
// all (an unconfined zero box reaching a label, which the callers below never do) falls back to
// the abstract wording rather than pointing the model at an empty path.
func confinementWritableRoots(box domain.ConfinementBox) string {
	roots := make([]string, 0, len(box.WritablePaths)+1)
	if box.WorkspaceRoot != "" {
		roots = append(roots, "the workspace "+box.WorkspaceRoot)
	}
	roots = append(roots, box.WritablePaths...)
	switch len(roots) {
	case 0:
		return "the workspace and the session scratch dir"
	case 1:
		return roots[0]
	default:
		return roots[0] + " and " + strings.Join(roots[1:], ", ")
	}
}

// resolveWorkdirInRoot resolves an execution tool's optional working directory within root
// (path-safe), or returns the root itself when none is given. Every tool taking a `workdir`
// argument resolves it the one way, so a path that escapes the workspace is refused with the
// same sentinel wherever the model tried it.
func resolveWorkdirInRoot(workdir, root string) (string, error) {
	if workdir == "" {
		return root, nil
	}
	return resolveInRoot(workdir, root)
}

// runSubprocess runs spec as a one-shot subprocess through internal/subprocess, the shared core
// that owns the §2.4 confinement-and-teardown contract for every spawner apogee has: the
// process-tree teardown, the confinement handoff that fails CLOSED, the live kill-on-denial watch
// on a confined run, the output cap and the timeout clamp
// (docs/design/confinement-execution-contract.md).
//
// This package keeps its own spec and result shapes and converts at the seam rather than aliasing
// the core's, so the execution tools and their tests go on building the spec they always built by
// field name; the two shapes are the same values under this package's spelling.
//
// The returned error is non-nil only for ctx cancellation (so the loop rolls the Turn back) or a
// confinement-unavailable demotion; a clean non-zero process exit is a normal result (exitCode
// set), not a Go error — the model reads it and routes around it.
func runSubprocess(ctx context.Context, spec subprocessSpec) (subprocessResult, error) {
	res, err := subprocess.RunSubprocess(ctx, spec.core())
	if err != nil {
		return subprocessResult{}, err
	}
	return fromCore(res), nil
}

// core renders the spec in the shared core's shape. It is a field-for-field rename and nothing
// else: a field added here without a line added there would be silently dropped, so the two
// structs are edited together.
func (s subprocessSpec) core() subprocess.SubprocessSpec {
	return subprocess.SubprocessSpec{
		Argv:        s.argv,
		Dir:         s.dir,
		Timeout:     s.timeout,
		Stdin:       s.stdin,
		Env:         s.env,
		SplitStdout: s.splitStdout,
		Cmdline:     s.cmdline,
		FailFast:    s.failFast,
	}
}

// fromCore renders the core's result in this package's shape, the other half of the same
// field-for-field rename.
func fromCore(res subprocess.SubprocessResult) subprocessResult {
	return subprocessResult{
		combinedOutput: res.CombinedOutput,
		stdout:         res.Stdout,
		exitCode:       res.ExitCode,
		timedOut:       res.TimedOut,
		drainWedged:    res.DrainWedged,
		confined:       res.Confined,
		box:            res.Box,
		denialStopped:  res.DenialStopped,
		failFast:       res.FailFast,
	}
}

// maxSubprocessErrorExcerptBytes caps how much of a failed command's diagnostics RunHookSubprocess
// quotes back in its error, so a noisy failure cannot drag the whole capped buffer into a log line.
const maxSubprocessErrorExcerptBytes = 256

// RunHookSubprocess runs argv as one subprocess through the SAME funnel every execution tool goes
// through (runSubprocess) and returns what the command wrote to standard output. It is the door a
// HOOK spawns through — a Mechanism runs outside the per-call Resolution and carries a
// domain.SubprocessPermit instead (docs/design/confinement-execution-contract.md §10) — so its
// subprocess gets every protection a tool's does rather than a hand-rolled exec.Command that has
// none: the credential scrub (subprocessEnv — neither apogee's own key nor an operator-declared one
// reaches a child), the §2.4 process-tree teardown, the output cap, the timeout clamp, and the
// confinement handoff, which fails CLOSED: a handle on ctx carrying no Confiner refuses the run
// rather than running unfenced.
//
// The funnel itself stays unexported; this is the whole of its outside surface. The caller
// installs its permit's box on ctx (domain.WithConfinement) before calling — no handle means an
// unfenced run, which is exactly what a permit carrying no box authorises. dir empty runs in
// apogee's own working directory. timeout zero takes the funnel's default and a timeout past the
// ceiling is clamped to it. stdin empty gives the child no input.
//
// workspaceRoot names the fence argv[0] is judged against: before the spec is built, argv[0] goes
// through security.ResolveProgram with that root and the box a Confinement handle on ctx carries
// (nil when there is none), and the absolute path it answers with replaces argv[0]. The funnel
// therefore fences its OWN argv[0] rather than trusting the caller to have done it: a caller's
// earlier resolution (a hook that probes its program once at construction) is belt, this is braces,
// and a caller that never fenced at all cannot spawn an unfenced program through this door. A
// refusal — the program resolves inside a path the model can write, or PATH answered with a
// relative entry — is returned as the funnel's error and nothing is spawned. An empty
// workspaceRoot with no box on ctx names no fence, which by security's empty-fence rule refuses
// nothing.
//
// secretEnv names the operator-declared credential variables to drop beside apogee's own — the
// same `api-key-env:` names (ADR 0047) the execution tools take from HostTools.SecretEnvVars,
// carried to a hook on mechanisms.Deps.SecretEnvVars and handed in here. A hook's child therefore
// scrubs exactly what a tool's child scrubs; nil names none and leaves the fixed half alone.
//
// The returned output is the child's stdout ALONE, never interleaved with its diagnostics, so a
// caller consuming it as a payload gets exactly the bytes the command produced. err is non-nil for
// a cancelled context, a refused confinement, a timeout, a wedged output drain and any non-zero
// exit: to a caller reading stdout as data every one of those means "no usable output", and the
// message quotes the command's diagnostics to say which.
func RunHookSubprocess(
	ctx context.Context,
	argv []string,
	dir string,
	workspaceRoot string,
	secretEnv []string,
	timeout time.Duration,
	stdin string,
) (string, error) {
	if len(argv) > 0 {
		program, err := security.ResolveProgram(nil, argv[0], workspaceRoot, confinementBox(ctx))
		if err != nil {
			return "", err
		}
		argv = append([]string{program}, argv[1:]...)
	}

	res, err := runSubprocess(ctx, subprocessSpec{
		argv:        argv,
		dir:         dir,
		timeout:     timeout,
		stdin:       stdin,
		env:         subprocessEnv(secretEnv),
		splitStdout: true,
	})
	if err != nil {
		return "", err
	}

	// argv is known non-empty here: runSubprocess refuses an empty one above.
	switch {
	case res.timedOut:
		return "", fmt.Errorf("apogee: %s timed out%s", argv[0], diagnosticsExcerpt(res.combinedOutput))
	case res.drainWedged:
		return "", fmt.Errorf("apogee: %s left its output pipe held open%s", argv[0], diagnosticsExcerpt(res.combinedOutput))
	case res.exitCode != 0:
		return "", fmt.Errorf("apogee: %s exited %d%s", argv[0], res.exitCode, diagnosticsExcerpt(res.combinedOutput))
	}
	return res.stdout, nil
}

// diagnosticsExcerpt renders a failed command's stderr for an error message: a bounded TAIL —
// a failing command's last line is the one naming the failure — or nothing at all when the
// command was silent. The cut is made byte-wise and then swept of the partial rune it may have
// left at the front, so the excerpt is always valid UTF-8.
func diagnosticsExcerpt(diagnostics string) string {
	trimmed := strings.TrimSpace(diagnostics)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > maxSubprocessErrorExcerptBytes {
		trimmed = "…" + strings.ToValidUTF8(trimmed[len(trimmed)-maxSubprocessErrorExcerptBytes:], "")
	}
	return ": " + trimmed
}

// shellHost is the platform shell/path facility the terminal tool wraps a command line with
// (sh -c on POSIX, cmd /c on Windows). It is a package var so a test can substitute a fake.
var shellHost platform.Host = platform.Current()

// resolveShell resolves the platform's shell NAME to the absolute program the tools launch,
// through the exec fence's complete form (security.ResolveProgram): PATH lookup, a refusal for a
// relative answer, then the writable-box refusal, measured against the workspace root and the
// confinement box riding on ctx.
//
// Resolving is what makes the fence meaningful here. platform hands back a bare "sh", and the
// fence measures argv[0] against the writable box — a bare name would be measured against
// apogee's own working directory, which is the workspace itself. Resolving first also puts the
// fence on the program PATH actually leads to, so an `sh` planted inside the workspace is refused
// by name rather than executed.
func resolveShell(ctx context.Context, root string) (string, error) {
	return security.ResolveProgram(nil, shellHost.Shell(), root, confinementBox(ctx))
}

// shellArgv returns the argv that runs command through the platform shell, with argv[0] replaced
// by the resolved, fenced program resolveShell answered with.
//
// It is binding for this package: shellHost.Command's bare argv[0] is never handed to
// runSubprocess: every consumer that wraps a model-supplied line in the platform shell builds its
// argv here, so there is exactly one place the shell is resolved and exactly one place it is
// fenced.
func shellArgv(ctx context.Context, root, command string) ([]string, error) {
	shell, err := resolveShell(ctx, root)
	if err != nil {
		return nil, err
	}
	argv := shellHost.Command(command)
	argv[0] = shell
	return argv, nil
}
