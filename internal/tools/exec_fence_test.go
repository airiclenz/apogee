package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// plantExecutable writes an executable file at root/rel and returns its absolute path — the
// planted argv[0] this fence exists to refuse. It is also the everyday shape of the collision
// the refusal accepts: an activated .venv or a node_modules/.bin ahead of the system entries on
// PATH resolves to exactly such a file.
func plantExecutable(t *testing.T, root, rel string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	return path
}

// prependPATH puts dir ahead of the inherited PATH for the duration of one test — the everyday
// shape of the collision the fence judges: an activated .venv or a node_modules/.bin sitting
// ahead of the system entries and winning the lookup.
func prependPATH(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestEveryExecSiteRefusesAProgramInsideTheWorkspace walks the tool exec sites one by one and
// pins the same rule at each: a program resolved on PATH that lands inside the workspace is
// refused, and the refusal NAMES the resolved path. The table is the item's scope statement —
// git, python_exec, run_tests, diagnostics, terminal and console_open are every exec site in
// this package (the next is internal/mechanisms/autofix, pinned in its own package; then
// internal/present's opener, which resolves its program in the presentation rung).
//
// The last two rows resolve the PLATFORM SHELL rather than a named tool, through the one
// resolver both take (shellArgv → security.ResolveProgram), which is why a planted `sh` is
// refused where a bare "sh" handed straight to os/exec would have been run.
//
// Naming the path is half the requirement, not a nicety: without it the operator reads a bare
// "not available" and goes looking for an install, when the cause is a workspace-resident entry
// on their own PATH.
func TestEveryExecSiteRefusesAProgramInsideTheWorkspace(t *testing.T) {
	// No t.Parallel anywhere below: each case swaps a package-level look* var.
	tests := []struct {
		name string
		// run plants a program inside root, points the tool's resolver at it, and returns
		// the model-facing text of the call plus whether the call reported an error result.
		run func(t *testing.T, root string) (planted, content string, isError bool)
		// wantError is false for diagnostics alone, whose vet half is the optional
		// enhancement: the syntax verdict still stands and the refusal rides on the note.
		wantError bool
	}{
		{
			name: "git",
			run: func(t *testing.T, root string) (string, string, bool) {
				planted := plantExecutable(t, root, "node_modules/.bin/git")
				withFakeGit(t, true, planted)
				res, err := NewGitStatus(root).Execute(context.Background(), statusCall("c1"))
				if err != nil {
					t.Fatalf("Execute returned a Go error (reserved for cancellation): %v", err)
				}
				return planted, res.Content, res.IsError
			},
			wantError: true,
		},
		{
			name: "python_exec",
			run: func(t *testing.T, root string) (string, string, bool) {
				planted := plantExecutable(t, root, ".venv/bin/python3")
				withFakeInterpreter(t, true, planted)
				res, err := NewPythonExec(root, nil).Execute(context.Background(), pythonCall("c1", "print(1)"))
				if err != nil {
					t.Fatalf("Execute returned a Go error (reserved for cancellation): %v", err)
				}
				return planted, res.Content, res.IsError
			},
			wantError: true,
		},
		{
			name: "run_tests",
			run: func(t *testing.T, root string) (string, string, bool) {
				if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/x\n\ngo 1.21\n"), 0o644); err != nil {
					t.Fatalf("write go.mod: %v", err)
				}
				planted := plantExecutable(t, root, "node_modules/.bin/go")
				original := lookTestProgram
				lookTestProgram = func(string) (string, bool) { return planted, true }
				t.Cleanup(func() { lookTestProgram = original })
				res := runTestsCall(t, root, nil)
				return planted, res.Content, res.IsError
			},
			wantError: true,
		},
		{
			name: "diagnostics",
			run: func(t *testing.T, root string) (string, string, bool) {
				writeGoModule(t, root)
				writeGoFile(t, root, "clean.go", "package diagtest\n\nfunc F() int { return 1 }\n")
				planted := plantExecutable(t, root, "tools/go")
				withFakeGo(t, true, planted)
				res, err := NewDiagnostics(root).Execute(context.Background(), diagnosticsCall("c1", "clean.go"))
				if err != nil {
					t.Fatalf("Execute returned a Go error (reserved for cancellation): %v", err)
				}
				return planted, res.Content, res.IsError
			},
			wantError: false,
		},
		{
			name: "terminal",
			run: func(t *testing.T, root string) (string, string, bool) {
				skipWithoutPOSIXShell(t)
				planted := plantExecutable(t, root, "node_modules/.bin/sh")
				prependPATH(t, filepath.Dir(planted))
				res, err := NewTerminal(root, nil).Execute(context.Background(), terminalCall("c1", "echo hi"))
				if err != nil {
					t.Fatalf("Execute returned a Go error (reserved for cancellation): %v", err)
				}
				return planted, res.Content, res.IsError
			},
			wantError: true,
		},
		{
			name: "console_open",
			run: func(t *testing.T, root string) (string, string, bool) {
				skipWithoutPOSIXShell(t)
				planted := plantExecutable(t, root, "node_modules/.bin/sh")
				prependPATH(t, filepath.Dir(planted))
				ctx, _ := consoleTestCtx(t)
				res, err := NewConsoleOpen(root, nil).Execute(ctx, consoleOpenCall("c1", "echo hi", 10))
				if err != nil {
					t.Fatalf("Execute returned a Go error (reserved for cancellation): %v", err)
				}
				return planted, res.Content, res.IsError
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := tempRoot(t)
			planted, content, isError := tt.run(t, root)
			if isError != tt.wantError {
				t.Fatalf("IsError = %v, want %v: %q", isError, tt.wantError, content)
			}
			if !strings.Contains(content, "resolves inside") {
				t.Errorf("result = %q, want the exec fence's refusal, not another degradation", content)
			}
			if !strings.Contains(content, planted) {
				t.Errorf("result = %q, want it to name the resolved program %q", content, planted)
			}
		})
	}
}

// TestPythonExecRefusesAnInRepoVirtualenvByName is the acceptance case for the ergonomics
// collision this item deliberately accepts: an activated <repo>/.venv/bin/python3 is resolved
// against apogee's own inherited PATH and lands inside the workspace, so python_exec refuses.
// The refusal must not reuse the graceful "no Python interpreter found" wording — that message
// describes a host without Python and would send the operator installing one they already have.
func TestPythonExecRefusesAnInRepoVirtualenvByName(t *testing.T) {
	root := tempRoot(t)
	venv := plantExecutable(t, root, ".venv/bin/python3")
	withFakeInterpreter(t, true, venv)

	res, err := NewPythonExec(root, nil).Execute(context.Background(), pythonCall("c1", "print(1)"))
	if err != nil {
		t.Fatalf("Execute returned a Go error (reserved for cancellation): %v", err)
	}
	if !res.IsError {
		t.Fatalf("a workspace-resident interpreter must be refused: %q", res.Content)
	}
	if !strings.Contains(res.Content, venv) {
		t.Errorf("refusal = %q, want it to name %q", res.Content, venv)
	}
	if strings.Contains(res.Content, "no Python interpreter found") {
		t.Errorf("refusal = %q, must not reuse the graceful-degradation message", res.Content)
	}
}

// TestPythonExecRefusalIsTerminalAcrossTheCandidates pins that a fence refusal on a candidate
// that WAS found ends the probe: python3 resolves inside the workspace and python resolves
// cleanly outside it, and the call is still refused. Falling through to the clean candidate
// would run an interpreter the operator never chose and swallow the one sentence that names the
// in-workspace PATH entry they must fix.
func TestPythonExecRefusalIsTerminalAcrossTheCandidates(t *testing.T) {
	root := tempRoot(t)
	planted := plantExecutable(t, root, ".venv/bin/python3")
	clean := plantExecutable(t, t.TempDir(), "python")

	original := lookInterpreter
	lookInterpreter = func(name string) (string, error) {
		if name == "python3" {
			return planted, nil
		}
		return clean, nil
	}
	t.Cleanup(func() { lookInterpreter = original })

	res, err := NewPythonExec(root, nil).Execute(context.Background(), pythonCall("c1", "print(1)"))
	if err != nil {
		t.Fatalf("Execute returned a Go error (reserved for cancellation): %v", err)
	}
	if !res.IsError {
		t.Fatalf("a refused python3 must not fall through to %q: %q", clean, res.Content)
	}
	if !strings.Contains(res.Content, planted) {
		t.Errorf("refusal = %q, want it to name the refused %q", res.Content, planted)
	}
	if strings.Contains(res.Content, clean) {
		t.Errorf("refusal = %q, must not have resolved the next candidate %q", res.Content, clean)
	}
}

// TestExecFenceCoversTheConfinementBoxNotOnlyTheRoot pins that the box the disposition installs
// on the call context is part of the fence: an extra writable path is as model-writable as the
// workspace, and a program planted there is the same attack.
func TestExecFenceCoversTheConfinementBoxNotOnlyTheRoot(t *testing.T) {
	root := tempRoot(t)
	extra := tempRoot(t)
	planted := plantExecutable(t, extra, "python3")
	withFakeInterpreter(t, true, planted)

	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Box: domain.ConfinementBox{WorkspaceRoot: root, WritablePaths: []string{extra}},
	})
	res, err := NewPythonExec(root, nil).Execute(ctx, pythonCall("c1", "print(1)"))
	if err != nil {
		t.Fatalf("Execute returned a Go error (reserved for cancellation): %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, planted) {
		t.Errorf("result = %q, want a refusal naming the program planted in the box's writable path", res.Content)
	}
}

// TestTerminalResolvesTheShellToAnAbsoluteProgram is the success half of the terminal row above:
// on an ordinary PATH the tool still runs, and what it runs is the ABSOLUTE shell the fence
// approved. A bare "sh" reaching the subprocess layer would be resolved again, by the child,
// against a working directory that is the workspace itself — which is the resolution this item
// took away from os/exec.
func TestTerminalResolvesTheShellToAnAbsoluteProgram(t *testing.T) {
	// Not parallel: withCapturedTerminalRun swaps a package-level var.
	captured := withCapturedTerminalRun(t)

	res, err := NewTerminal(tempRoot(t), nil).Execute(context.Background(), terminalCall("c1", "echo hi"))

	if err != nil {
		t.Fatalf("Execute returned a Go error (reserved for cancellation): %v", err)
	}
	if res.IsError {
		t.Fatalf("result = %q, want an ordinary PATH to resolve the platform shell", res.Content)
	}
	if len(captured.argv) == 0 || !filepath.IsAbs(captured.argv[0]) {
		t.Errorf("argv = %q, want argv[0] resolved to an absolute program", captured.argv)
	}
}
