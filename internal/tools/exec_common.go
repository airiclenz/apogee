package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/subprocess"
)

// maxSubprocessOutputBytes caps the combined stdout+stderr a subprocess call surfaces to the
// model — a noisy command cannot flood the context window. It is the core's ceiling under this
// package's own name, so the Console family's separate truncation (console_common.go) is measured
// against the very same number the funnel enforces rather than a second copy of it.
const maxSubprocessOutputBytes = subprocess.MaxSubprocessOutputBytes

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

// confinementDenialLabel is the line appended to a FAILED confined result whose output looks
// like an OS confinement denial. It NAMES the roots the run may write to, because a model that
// is only told a fence exists has nowhere to put the file: the paths are what let it route the
// write instead of treating the EPERM as a broken command and blindly retrying around it.
func confinementDenialLabel(box domain.ConfinementBox) string {
	return "[likely blocked by workspace confinement: writes are allowed only inside " +
		confinementWritableRoots(box) + "]"
}

// confinementDenialStopLabel is the line appended when the live kill-on-denial watch stopped
// the run itself (subprocess.SubprocessResult.DenialStopped, console.Console.DenialStopped): stronger than
// the "likely" label above, because here the harness matched the denial as it streamed and
// killed the process group, so the model is told plainly that the rest of its script did not
// run. It names the writable roots for the same reason that one does — the model's next act is
// to re-aim the write, and it can only do that against real paths. The OS-denial spellings both
// labels key on live in internal/platform (platform.LooksLikeConfinementDenial), which is also
// what the watch scans with.
//
// Both labels sit beside the funnel rather than beside one tool: the one-shot execution tools
// read them off subprocess.SubprocessResult and the Console family reads the stop label off a live
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
	return security.ResolveInRoot(workdir, root)
}

// runSubprocess runs spec as a one-shot subprocess through internal/subprocess, the shared core
// that owns the §2.4 confinement-and-teardown contract for every spawner apogee has: the
// process-tree teardown, the confinement handoff that fails CLOSED, the live kill-on-denial watch
// on a confined run, the output cap and the timeout clamp
// (docs/design/confinement-execution-contract.md). The execution tools build the core's
// subprocess.SubprocessSpec directly and read its subprocess.SubprocessResult back; what this
// funnel adds is the one door the remaining in-package callers (diagnostics, git and the hook
// door) go through; the tools built on an execHost (terminal, python_exec, run_tests) launch
// through execHost.run instead — the same subprocess.RunSubprocess in production, and a recorder
// in a test (capturedRunHost), which is how a test pins the exact spec a tool builds without
// swapping a package-level var.
//
// Two of the spec's fields carry a rule of this package's own. Env: EVERY tool that runs
// something for the MODEL sets it — none of them inherits whole. git and the Go toolchain take an
// allowlist scoped by platform.Host.ScopeEnv; the shell and interpreter tools take
// execHost.subprocessEnvScopedPath() — the caller's environment minus every credential variable (apogee's
// own and the host-configured ones), with the child's PATH scoped out of the workspace; and the
// test runner takes subprocessEnv(), the same minus the credentials, because a test suite needs
// the toolchain variables its user's shell has but no subprocess of the model's needs apogee's
// key. SplitStdout: the execution tools leave it false — they SHOW the model what a command
// printed, and the interleaved order is the truthful one there (write by write on a CONFINED
// run, whose stderr rides through the denial watch on its own copier — subprocess.CappedBuffer);
// only a caller that CONSUMES the output as a payload (RunHookSubprocess) sets it.
//
// The returned error is non-nil only for ctx cancellation (so the loop rolls the Turn back) or a
// confinement-unavailable demotion; a clean non-zero process exit is a normal result (ExitCode
// set), not a Go error — the model reads it and routes around it.
func runSubprocess(ctx context.Context, spec subprocess.SubprocessSpec) (subprocess.SubprocessResult, error) {
	return subprocess.RunSubprocess(ctx, spec)
}

// maxSubprocessErrorExcerptBytes caps how much of a failed command's diagnostics RunHookSubprocess
// quotes back in its error, so a noisy failure cannot drag the whole capped buffer into a log line.
const maxSubprocessErrorExcerptBytes = 256

// RunHookSubprocess runs argv as one subprocess through the SAME funnel every execution tool goes
// through (runSubprocess) and returns what the command wrote to standard output. It is the door a
// REACTION spawns through — a Reaction runs outside the per-call Resolution and carries a
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
// handed in here by whichever caller opens this door. A hook's child therefore scrubs exactly what
// a tool's child scrubs; nil names none and leaves the fixed half alone.
//
// extraEnv is the caller's own "KEY=value" additions, appended AFTER the scrub so they win over an
// inherited spelling of the same key — the headline facts a fired reaction finds its Moment under
// (domain.SeamPayload.Env), which is why the door takes them at all: the
// full document is on stdin, and these are the convenience a one-line script reads instead of
// parsing it. They are appended, never substituted, so a variable the caller does not name is
// exactly what the scrub left. nil adds nothing.
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
	extraEnv []string,
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

	res, err := runSubprocess(ctx, subprocess.SubprocessSpec{
		Argv:        argv,
		Dir:         dir,
		Timeout:     timeout,
		Stdin:       stdin,
		Env:         subprocessEnv(secretEnv, extraEnv...),
		SplitStdout: true,
	})
	if err != nil {
		return "", err
	}

	// argv is known non-empty here: runSubprocess refuses an empty one above.
	switch {
	case res.TimedOut:
		return "", fmt.Errorf("apogee: %s timed out%s", argv[0], diagnosticsExcerpt(res.CombinedOutput))
	case res.DrainWedged:
		return "", fmt.Errorf("apogee: %s left its output pipe held open%s", argv[0], diagnosticsExcerpt(res.CombinedOutput))
	case res.ExitCode != 0:
		return "", fmt.Errorf("apogee: %s exited %d%s", argv[0], res.ExitCode, diagnosticsExcerpt(res.CombinedOutput))
	}
	return res.Stdout, nil
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
