package tools

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func pythonCall(id, code string) domain.ToolCall {
	return domain.ToolCall{ID: id, Tool: "python_exec", Arguments: []byte(fmt.Sprintf(`{"code":%q}`, code))}
}

// withFakeInterpreter swaps lookInterpreter for the duration of a test (restored on cleanup),
// so the graceful-degradation path is exercisable without depending on the host's PATH.
func withFakeInterpreter(t *testing.T, found bool, path string) {
	t.Helper()
	orig := lookInterpreter
	lookInterpreter = func([]string) (string, bool) { return path, found }
	t.Cleanup(func() { lookInterpreter = orig })
}

func TestPythonExec_Markers(t *testing.T) {
	t.Parallel()
	py := NewPythonExec(t.TempDir())
	if py.Name() != "python_exec" {
		t.Errorf("Name() = %q, want python_exec", py.Name())
	}
	if py.ReadOnly() {
		t.Error("python_exec must be write-capable (ReadOnly()==false)")
	}
	if !domain.IsSubprocessTool(py) {
		t.Error("python_exec must be a SubprocessTool")
	}
}

func TestPythonExec_GracefulWhenAbsent(t *testing.T) {
	// Not parallel: withFakeInterpreter swaps the package-level lookInterpreter var, which
	// the parallel run-tests read — a non-parallel test completes before they resume.
	withFakeInterpreter(t, false, "")
	py := NewPythonExec(t.TempDir())
	res, err := py.Execute(context.Background(), pythonCall("c1", "print(1)"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil (absence must degrade gracefully, not crash)", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "python not available") {
		t.Errorf("result = %q, want a clear 'python not available' result", res.Content)
	}
}

func TestPythonExec_EmptyCode(t *testing.T) {
	t.Parallel()
	py := NewPythonExec(t.TempDir())
	res, err := py.Execute(context.Background(), pythonCall("c1", "   "))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "code is required") {
		t.Errorf("empty code result = %q, want a 'code is required' error", res.Content)
	}
}

// realPython resolves a Python interpreter on the test host, skipping the test when none is
// installed so the suite stays green on a host without Python (the tool's graceful contract).
func realPython(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("python-exec run-test uses POSIX assumptions; covered on unix")
	}
	for _, name := range pythonCandidates {
		if _, err := exec.LookPath(name); err == nil {
			return
		}
	}
	t.Skip("no Python interpreter on PATH; skipping the live python-exec run")
}

func TestPythonExec_RunsScriptFromStdin(t *testing.T) {
	realPython(t)
	t.Parallel()
	py := NewPythonExec(t.TempDir())
	res, err := py.Execute(context.Background(), pythonCall("c1", "print('hi from python')"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Errorf("clean script produced an error result: %q", res.Content)
	}
	if !strings.Contains(res.Content, "hi from python") {
		t.Errorf("output = %q, want it to contain the printed text", res.Content)
	}
}

func TestPythonExec_NonZeroExitIsErrorResult(t *testing.T) {
	realPython(t)
	t.Parallel()
	py := NewPythonExec(t.TempDir())
	res, err := py.Execute(context.Background(), pythonCall("c1", "import sys; sys.exit(2)"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "exit code 2") {
		t.Errorf("result = %q, want it to report exit code 2", res.Content)
	}
}

func TestPythonExec_RunsUnderConfine(t *testing.T) {
	realPython(t)
	t.Parallel()
	py := NewPythonExec(t.TempDir())
	conf := &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}}
	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: conf,
		Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
	})

	res, err := py.Execute(ctx, pythonCall("c1", "print('confined')"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if conf.confineCount() != 1 {
		t.Errorf("Confine called %d times, want 1", conf.confineCount())
	}
	if res.IsError {
		t.Errorf("confined run errored: %q", res.Content)
	}
}

func TestPythonExec_ConfinementUnavailablePropagates(t *testing.T) {
	// Not parallel: withFakeInterpreter swaps the package-level lookInterpreter var.
	withFakeInterpreter(t, true, "/usr/bin/python3")
	py := NewPythonExec(t.TempDir())
	conf := &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}, unavailable: true}
	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: conf,
		Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
	})

	_, err := py.Execute(ctx, pythonCall("c1", "print('should not run')"))
	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Fatalf("Execute err = %v, want ErrConfinementUnavailable (must not run unconfined)", err)
	}
}
