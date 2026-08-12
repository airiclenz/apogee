package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
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

// withFakePythonVersion pins what the version probe reports, so the isolation decision is
// testable on either side of the 3.11 boundary without depending on the Python the host ships.
func withFakePythonVersion(t *testing.T, major, minor int, ok bool) {
	t.Helper()
	orig := interpreterVersion
	interpreterVersion = func(context.Context, string) (int, int, bool) { return major, minor, ok }
	t.Cleanup(func() { interpreterVersion = orig })
}

// withCapturedPythonRun swaps the interpreter runner for one that records the spec and launches
// nothing, so a test can pin the exact argv and environment the tool builds.
func withCapturedPythonRun(t *testing.T) *subprocessSpec {
	t.Helper()
	orig := runPythonSubprocess
	var captured subprocessSpec
	runPythonSubprocess = func(_ context.Context, spec subprocessSpec) (subprocessResult, error) {
		captured = spec
		return subprocessResult{}, nil
	}
	t.Cleanup(func() { runPythonSubprocess = orig })
	return &captured
}

// envValue returns the value of key in a "KEY=value" environment, and whether it is present.
func envValue(env []string, key string) (string, bool) {
	for _, entry := range env {
		if name, value, ok := strings.Cut(entry, "="); ok && name == key {
			return value, true
		}
	}
	return "", false
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

// TestPythonExec_RunsUnderConfine pins that EVERY interpreter this tool launches goes through
// the confinement handle — the version probe as well as the snippet, hence two calls. The probe
// is a subprocess like any other and takes the same contract: a box that cannot be established
// stops it too (TestPythonExec_ConfinementUnavailablePropagates), rather than leaving one
// unconfined exec beside a confined one.
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
	if conf.confineCount() != 2 {
		t.Errorf("Confine called %d times, want 2 (the version probe and the snippet)", conf.confineCount())
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

// TestPythonExec_IsolationFollowsTheInterpreterVersion pins the load-path policy at the level
// that decides it: PYTHONSAFEPATH is always set, and -I is added exactly when the interpreter is
// too old to honour it. The version is stubbed rather than measured, so the assertion does not
// depend on which Python the CI host happens to ship — and an UNKNOWN version isolates, which is
// the fail-closed direction (an unreadable version is not evidence of a modern interpreter).
func TestPythonExec_IsolationFollowsTheInterpreterVersion(t *testing.T) {
	const interp = "/usr/bin/python3"
	cases := []struct {
		name         string
		major, minor int
		known        bool
		wantArgv     []string
	}{
		{"3.11 honours PYTHONSAFEPATH, so no flag", 3, 11, true, []string{interp, "-"}},
		{"3.13 honours PYTHONSAFEPATH, so no flag", 3, 13, true, []string{interp, "-"}},
		{"4.0 is past the floor, so no flag", 4, 0, true, []string{interp, "-"}},
		{"3.9 ignores it silently, so -I", 3, 9, true, []string{interp, "-I", "-"}},
		{"2.7 ignores it silently, so -I", 2, 7, true, []string{interp, "-I", "-"}},
		{"an unreadable version isolates", 0, 0, false, []string{interp, "-I", "-"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Not parallel: these swap package-level vars (lookInterpreter, interpreterVersion,
			// runPythonSubprocess) that the parallel run-tests read.
			withFakeInterpreter(t, true, interp)
			withFakePythonVersion(t, tc.major, tc.minor, tc.known)
			captured := withCapturedPythonRun(t)

			if _, err := NewPythonExec(t.TempDir()).Execute(context.Background(), pythonCall("c1", "print(1)")); err != nil {
				t.Fatalf("Execute err = %v, want nil", err)
			}
			if got := captured.argv; !slices.Equal(got, tc.wantArgv) {
				t.Errorf("argv = %q, want %q", got, tc.wantArgv)
			}
			if value, ok := envValue(captured.env, "PYTHONSAFEPATH"); !ok || value != "1" {
				t.Errorf("PYTHONSAFEPATH = %q (present=%v), want \"1\" on every version", value, ok)
			}
			if _, ok := envValue(captured.env, "PYTHONPATH"); ok {
				t.Error("PYTHONPATH must never be set by the tool: its entries precede the stdlib")
			}
		})
	}
}

// TestPythonExec_DropsApogeeCredentialsFromTheChildEnvironment pins that apogee's own key does
// not travel into a model-chosen snippet, while the rest of the operator's environment does —
// this is a targeted removal of apogee's secrets, not git's allowlist.
func TestPythonExec_DropsApogeeCredentialsFromTheChildEnvironment(t *testing.T) {
	// Not parallel: t.Setenv, plus the package-level interpreter/runner swaps.
	t.Setenv("APOGEE_API_KEY", "sk-secret-value")
	t.Setenv("APOGEE_ENDPOINT", "http://192.0.2.1:1111")
	withFakeInterpreter(t, true, "/usr/bin/python3")
	withFakePythonVersion(t, 3, 12, true)
	captured := withCapturedPythonRun(t)

	if _, err := NewPythonExec(t.TempDir()).Execute(context.Background(), pythonCall("c1", "print(1)")); err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if value, ok := envValue(captured.env, "APOGEE_API_KEY"); ok {
		t.Errorf("APOGEE_API_KEY = %q reached the child environment, want it dropped", value)
	}
	for _, entry := range captured.env {
		if strings.Contains(entry, "sk-secret-value") {
			t.Errorf("the api key survived under another name: %q", entry)
		}
	}
	if value, _ := envValue(captured.env, "APOGEE_ENDPOINT"); value != "http://192.0.2.1:1111" {
		t.Errorf("APOGEE_ENDPOINT = %q, want it inherited (only the SECRETS are dropped)", value)
	}
	if _, ok := envValue(captured.env, "PATH"); !ok {
		t.Error("PATH did not survive: the interpreter runs in the operator's environment")
	}
}

func TestParsePythonVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                 string
		out                  string
		wantMajor, wantMinor int
		wantOK               bool
	}{
		{"a clean probe", "3.12\n", 3, 12, true},
		{"a warning ahead of the answer", "DeprecationWarning: blah\n3.9\n", 3, 9, true},
		{"python 2 answers too", "2.7\n", 2, 7, true},
		{"no output at all", "", 0, 0, false},
		{"not a version", "python\n", 0, 0, false},
		{"an unparseable minor", "3.x\n", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			major, minor, ok := parsePythonVersion(tc.out)
			if major != tc.wantMajor || minor != tc.wantMinor || ok != tc.wantOK {
				t.Errorf("parsePythonVersion(%q) = (%d, %d, %v), want (%d, %d, %v)",
					tc.out, major, minor, ok, tc.wantMajor, tc.wantMinor, tc.wantOK)
			}
		})
	}
}

// TestPythonExec_WorkspaceDoesNotShadowTheStdlib is the mechanism test: a repo-root json.py that
// writes a marker, and a snippet whose approved `code` says only `import json`. Before the load
// path was fixed the marker appeared, because a program read from stdin puts the working
// directory — the workspace — ahead of the standard library on sys.path.
//
// It runs a REAL interpreter, so it reports which one it exercised and skips loudly when there
// is none: a silent pass on a host where the mechanism was never executed would be worse than no
// test at all.
func TestPythonExec_WorkspaceDoesNotShadowTheStdlib(t *testing.T) {
	realPython(t)
	t.Parallel()

	interp, found := lookInterpreter(pythonCandidates)
	if !found {
		t.Skip("no Python interpreter on PATH; the stdlib-shadowing mechanism cannot be exercised here")
	}
	major, minor, known := interpreterVersion(context.Background(), interp)
	if !known {
		t.Logf("%s did not report a version; the run below exercises the -I fallback", interp)
	} else {
		t.Logf("interpreter %s reports %d.%d, so this run exercises %s", interp, major, minor,
			map[bool]string{true: "PYTHONSAFEPATH", false: "the -I fallback"}[honoursSafePath(major, minor, known)])
	}

	root := t.TempDir()
	marker := filepath.Join(root, "SHADOWED")
	shadow := "import os\nopen(" + strconv.Quote(marker) + ", \"w\").write(\"x\")\n"
	if err := os.WriteFile(filepath.Join(root, "json.py"), []byte(shadow), 0o600); err != nil {
		t.Fatalf("write the shadowing json.py: %v", err)
	}

	res, err := NewPythonExec(root).Execute(context.Background(), pythonCall("c1", "import json; print(json.__file__)"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("the snippet failed: %q", res.Content)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatalf("the workspace json.py ran: `import json` must resolve to the stdlib, got %q", res.Content)
	}
	if strings.Contains(res.Content, root) {
		t.Errorf("json resolved inside the workspace: %q", res.Content)
	}
}
