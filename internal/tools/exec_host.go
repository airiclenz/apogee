package tools

import (
	"context"
	"os/exec"

	"github.com/airiclenz/apogee/internal/console"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/subprocess"
)

// execHost is the operating system as the execution tools see it: the four facilities a tool
// that launches a program for the MODEL reaches for — resolving a program name on PATH, running
// a one-shot subprocess, the platform shell's rules, and opening a Console. It is ONE value the
// five execution tools (terminal, python_exec, run_tests, diagnostics, console_open) are built
// with, so a test hands a tool a host whose facilities are fakes rather than swapping a
// package-level var beside every other test that reads it.
//
// It is unexported by decision (2026-09-20): the composition root builds exactly one through
// defaultExecHost and builtinTools hands it to the five; a bench-facing HostTools field is one
// addition away should a Driver need to supply its own.
type execHost struct {
	// look resolves a program NAME to the absolute path PATH leads to — the lookup the exec fence
	// (security.ResolveProgram) measures against the writable box.
	look func(string) (string, error)
	// run launches one subprocess through internal/subprocess, the shared core owning the §2.4
	// confinement-and-teardown contract.
	run func(context.Context, subprocess.SubprocessSpec) (subprocess.SubprocessResult, error)
	// shell is the platform shell/path facility a command line is wrapped with (sh -c on POSIX,
	// cmd /c on Windows) and the environment is scoped through.
	shell platform.Host
	// openConsole starts a Console under the registry (ADR 0059).
	openConsole func(*console.Registry, console.OpenSpec) (*console.Console, error)
}

// defaultExecHost returns the real operating system: exec.LookPath, subprocess.RunSubprocess,
// the platform rules for this build target and the Console registry's own Open.
func defaultExecHost() execHost {
	return execHost{
		look:        exec.LookPath,
		run:         subprocess.RunSubprocess,
		shell:       platform.Current(),
		openConsole: (*console.Registry).Open,
	}
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
func (h execHost) subprocessEnvScopedPath(workspaceRoot string, secretEnv []string, extra ...string) []string {
	return append(h.shell.ScopeInheritedEnv(workspaceRoot, subprocessEnv(secretEnv)), extra...)
}

// resolveShell resolves the platform's shell NAME to the absolute program the tools launch,
// through the exec fence's complete form (security.ResolveProgram): the host's own PATH lookup
// (h.look — never the fence's real fallback, so a fake look drives the shell lookup too), a
// refusal for a relative answer, then the writable-box refusal, measured against the workspace
// root and the confinement box riding on ctx.
//
// Resolving is what makes the fence meaningful here. platform hands back a bare "sh", and the
// fence measures argv[0] against the writable box — a bare name would be measured against
// apogee's own working directory, which is the workspace itself. Resolving first also puts the
// fence on the program PATH actually leads to, so an `sh` planted inside the workspace is refused
// by name rather than executed.
func (h execHost) resolveShell(ctx context.Context, root string) (string, error) {
	return security.ResolveProgram(h.look, h.shell.Shell(), root, confinementBox(ctx))
}

// shellArgv returns the argv that runs command through the platform shell, with argv[0] replaced
// by the resolved, fenced program resolveShell answered with.
//
// It is binding for this package: the shell's bare argv[0] (h.shell.Command) is never handed to
// runSubprocess: every consumer that wraps a model-supplied line in the platform shell builds its
// argv here, so there is exactly one place the shell is resolved and exactly one place it is
// fenced.
func (h execHost) shellArgv(ctx context.Context, root, command string) ([]string, error) {
	shell, err := h.resolveShell(ctx, root)
	if err != nil {
		return nil, err
	}
	argv := h.shell.Command(command)
	argv[0] = shell
	return argv, nil
}
