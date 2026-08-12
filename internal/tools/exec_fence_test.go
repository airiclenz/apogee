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

// TestEveryExecSiteRefusesAProgramInsideTheWorkspace walks the tool exec sites one by one and
// pins the same rule at each: a program resolved on PATH that lands inside the workspace is
// refused, and the refusal NAMES the resolved path. The table is the item's scope statement —
// git, python_exec, run_tests and diagnostics are every exec site in this package (the fifth is
// internal/mechanisms/autofix, pinned in its own package; the sixth is internal/present's
// opener, which resolves its program in the presentation rung).
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
				res, err := NewPythonExec(root).Execute(context.Background(), pythonCall("c1", "print(1)"))
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
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
	root := t.TempDir()
	venv := plantExecutable(t, root, ".venv/bin/python3")
	withFakeInterpreter(t, true, venv)

	res, err := NewPythonExec(root).Execute(context.Background(), pythonCall("c1", "print(1)"))
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

// TestExecFenceCoversTheConfinementBoxNotOnlyTheRoot pins that the box the disposition installs
// on the call context is part of the fence: an extra writable path is as model-writable as the
// workspace, and a program planted there is the same attack.
func TestExecFenceCoversTheConfinementBoxNotOnlyTheRoot(t *testing.T) {
	root := t.TempDir()
	extra := t.TempDir()
	planted := plantExecutable(t, extra, "python3")
	withFakeInterpreter(t, true, planted)

	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Box: domain.ConfinementBox{WorkspaceRoot: root, WritablePaths: []string{extra}},
	})
	res, err := NewPythonExec(root).Execute(ctx, pythonCall("c1", "print(1)"))
	if err != nil {
		t.Fatalf("Execute returned a Go error (reserved for cancellation): %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, planted) {
		t.Errorf("result = %q, want a refusal naming the program planted in the box's writable path", res.Content)
	}
}
