package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// writeProject writes a fixture project (paths are slash-separated, relative to the root) into a
// fresh temp dir and returns its root.
func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

// runTestsCall drives the tool over root with the given arguments.
func runTestsCall(t *testing.T, root string, args map[string]any) domain.ToolResult {
	t.Helper()
	res, err := NewRunTests(root, nil).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "run_tests", Arguments: jsonArgs(t, args),
	})
	if err != nil {
		t.Fatalf("Execute returned a Go error (reserved for cancellation): %v", err)
	}
	return res
}

// withCapturedTestRun swaps the runner subprocess for one that records the spec and launches
// nothing, so a test can pin the exact argv and environment the tool builds.
func withCapturedTestRun(t *testing.T) *subprocessSpec {
	t.Helper()
	orig := runTestsSubprocess
	var captured subprocessSpec
	runTestsSubprocess = func(_ context.Context, spec subprocessSpec) (subprocessResult, error) {
		captured = spec
		return subprocessResult{}, nil
	}
	t.Cleanup(func() { runTestsSubprocess = orig })
	return &captured
}

// TestRunTestsDetectsRunnerByMarkerPrecedence pins ratified call 6: which runner a project gets is
// a fact about its markers, in one fixed order — a polyglot repository must resolve the same way
// every time rather than by whichever marker a directory listing happened to yield first.
func TestRunTestsDetectsRunnerByMarkerPrecedence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		files map[string]string
		want  string // the runner's name, "" when none should be detected
	}{
		{
			name:  "go.mod selects go test",
			files: map[string]string{"go.mod": "module example.test/x\n\ngo 1.21\n"},
			want:  "go test",
		},
		{
			name:  "pytest.ini selects pytest",
			files: map[string]string{"pytest.ini": "[pytest]\n"},
			want:  "pytest",
		},
		{
			name:  "pyproject with a pytest section selects pytest",
			files: map[string]string{"pyproject.toml": "[project]\nname = \"x\"\n\n[tool.pytest.ini_options]\ntestpaths = [\"tests\"]\n"},
			want:  "pytest",
		},
		{
			name:  "pyproject without pytest config selects nothing",
			files: map[string]string{"pyproject.toml": "[project]\nname = \"x\"\n"},
			want:  "",
		},
		{
			name:  "package.json with a test script selects npm test",
			files: map[string]string{"package.json": `{"name":"x","scripts":{"test":"jest"}}`},
			want:  "npm test",
		},
		{
			name:  "package.json without a test script selects nothing",
			files: map[string]string{"package.json": `{"name":"x","scripts":{"build":"tsc"}}`},
			want:  "",
		},
		{
			name: "go.mod outranks package.json",
			files: map[string]string{
				"go.mod":       "module example.test/x\n\ngo 1.21\n",
				"package.json": `{"name":"x","scripts":{"test":"jest"}}`,
			},
			want: "go test",
		},
		{
			name: "pytest outranks package.json",
			files: map[string]string{
				"pytest.ini":   "[pytest]\n",
				"package.json": `{"name":"x","scripts":{"test":"jest"}}`,
			},
			want: "pytest",
		},
		{
			name:  "an unmarked project selects nothing",
			files: map[string]string{"README.md": "no tests here\n"},
			want:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner, ok := detectTestRunner(writeProject(t, tc.files))
			if !ok {
				if tc.want != "" {
					t.Fatalf("no runner detected, want %q", tc.want)
				}
				return
			}
			if tc.want == "" {
				t.Fatalf("detected %q, want no runner", runner.name)
			}
			if runner.name != tc.want {
				t.Errorf("detected %q, want %q", runner.name, tc.want)
			}
		})
	}
}

// TestRunTestsNoMarkerIsAnErrorNamingTheMarkers pins the no-marker result: the model is told what
// was looked for and where to go instead, so an unmarked project is an actionable answer rather
// than a dead end it retries.
func TestRunTestsNoMarkerIsAnErrorNamingTheMarkers(t *testing.T) {
	t.Parallel()

	res := runTestsCall(t, writeProject(t, map[string]string{"Makefile": "test:\n\t./run.sh\n"}), nil)
	if !res.IsError {
		t.Fatalf("a project with no runner marker must be an error result: %q", res.Content)
	}
	for _, want := range []string{"go.mod", "pytest", "package.json", "terminal"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("the no-runner message must name %q: %q", want, res.Content)
		}
	}
}

// TestRunTestsCommandConstructionPerRunner pins the filter and subtree mapping for all three
// runners without needing any of them installed — the argv IS the mapping, and a wrong flag would
// otherwise only surface on a host that happens to have pytest or npm.
func TestRunTestsCommandConstructionPerRunner(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		runner  testRunner
		subtree string
		filter  string
		want    []string
	}{
		{name: "go: whole module", runner: goTestRunner, want: []string{"test", "./..."}},
		{
			name: "go: subtree carries its own subpackages", runner: goTestRunner,
			subtree: "internal/tools", want: []string{"test", "./internal/tools/..."},
		},
		{
			name: "go: filter maps to -run", runner: goTestRunner,
			filter: "TestFoo", want: []string{"test", "-run", "TestFoo", "./..."},
		},
		{name: "pytest: no arguments", runner: pytestRunner, want: nil},
		{
			name: "pytest: filter maps to -k, subtree is positional", runner: pytestRunner,
			subtree: "tests", filter: "adds", want: []string{"-k", "adds", "tests"},
		},
		{name: "npm: bare script", runner: npmTestRunner, want: []string{"test"}},
		{
			name: "npm: arguments follow the -- separator", runner: npmTestRunner,
			subtree: "src", filter: "adds", want: []string{"test", "--", "-t", "adds", "src"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.runner.args(tc.subtree, tc.filter); !slices.Equal(got, tc.want) {
				t.Errorf("args(%q, %q) = %v, want %v", tc.subtree, tc.filter, got, tc.want)
			}
		})
	}
}

// TestRunTestsGoSuccessCondensesAVerboseRun runs a real (tiny) module whose passing test prints
// half a megabyte of output: the model must get the verdict and nothing else, under the cap. A
// summary that grows with the log would defeat the whole tool.
func TestRunTestsGoSuccessCondensesAVerboseRun(t *testing.T) {
	t.Parallel()
	requireGo(t)

	root := writeProject(t, map[string]string{
		"go.mod": "module example.test/verbose\n\ngo 1.21\n",
		"verbose_test.go": `package verbose

import (
	"fmt"
	"strings"
	"testing"
)

func TestNoisyButPassing(t *testing.T) {
	line := strings.Repeat("x", 100)
	for i := 0; i < 5000; i++ {
		fmt.Println(line)
	}
}
`,
	})

	res := runTestsCall(t, root, nil)
	if res.IsError {
		t.Fatalf("a passing suite must not be an error result: %q", res.Content)
	}
	if !strings.HasPrefix(res.Content, "PASS (go test)") {
		t.Errorf("the verdict must lead the result: %q", firstLineOf(res.Content))
	}
	if strings.Contains(res.Content, strings.Repeat("x", 50)) {
		t.Error("the runner's own output must not be quoted back on a passing run")
	}
	assertUnderCap(t, res.Content)
	if !strings.Contains(res.Content, "terminal") {
		t.Errorf("the closing note must point at terminal for the full log: %q", res.Content)
	}
}

// TestRunTestsGoFailureCondensesAndCapsANoisyRun runs a real module with twelve loud failing tests:
// the result must name the failing tests with their first error lines, count the ones it did not
// quote, and still fit the cap.
func TestRunTestsGoFailureCondensesAndCapsANoisyRun(t *testing.T) {
	t.Parallel()
	requireGo(t)

	var src strings.Builder
	src.WriteString("package noisy\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n\t\"testing\"\n)\n")
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&src, `
func TestFail%02d(t *testing.T) {
	noise := strings.Repeat("n", 300)
	for j := 0; j < 40; j++ {
		fmt.Println(noise)
	}
	t.Errorf("assertion %02d failed: want 1, got 2")
	t.Logf("extra context %%s", noise)
}
`, i, i)
	}

	root := writeProject(t, map[string]string{
		"go.mod":        "module example.test/noisy\n\ngo 1.21\n",
		"noisy_test.go": src.String(),
	})

	res := runTestsCall(t, root, nil)
	if !res.IsError {
		t.Fatalf("a failing suite must be an error result: %q", res.Content)
	}
	if !strings.HasPrefix(res.Content, "FAIL (go test)") {
		t.Errorf("the verdict must lead the result: %q", firstLineOf(res.Content))
	}
	if !strings.Contains(res.Content, "12 failing tests") {
		t.Errorf("the counts line must state every failing test, quoted or not: %q", res.Content)
	}
	if !strings.Contains(res.Content, "TestFail01") {
		t.Errorf("the first failing test must be named: %q", res.Content)
	}
	if !strings.Contains(res.Content, "assertion 01 failed") {
		t.Errorf("a failing test's first error line is why it failed and must survive: %q", res.Content)
	}
	if !strings.Contains(res.Content, "2 more failing tests") {
		t.Errorf("only 10 failures are quoted; the rest must be counted: %q", res.Content)
	}
	if strings.Contains(res.Content, "TestFail12") {
		t.Errorf("the eleventh and twelfth failures must be counted, not quoted: %q", res.Content)
	}
	assertUnderCap(t, res.Content)
}

// TestRunTestsRefusesArgumentsItCannotSafelyPass covers the three model-facing refusals that never
// reach a runner: an option-shaped filter or path (the argv injection class the git tools close
// with the same guard), a path outside the workspace fence, and a path that is not there.
func TestRunTestsRefusesArgumentsItCannotSafelyPass(t *testing.T) {
	t.Parallel()

	root := writeProject(t, map[string]string{"go.mod": "module example.test/x\n\ngo 1.21\n"})

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "option-shaped filter", args: map[string]any{"filter": "-race"}, want: "filter must not begin"},
		{name: "option-shaped path", args: map[string]any{"path": "--help"}, want: "path must not begin"},
		{name: "path escape", args: map[string]any{"path": "../outside"}, want: "outside the workspace"},
		{name: "missing path", args: map[string]any{"path": "nope"}, want: "path not found"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := runTestsCall(t, root, tc.args)
			if !res.IsError {
				t.Fatalf("%s must be refused: %q", tc.name, res.Content)
			}
			if !strings.Contains(res.Content, tc.want) {
				t.Errorf("refusal should say %q: %q", tc.want, res.Content)
			}
		})
	}
}

// TestRunTestsMissingRunnerProgramDegradesGracefully pins §3a for this tool: the markers said which
// runner this project uses, so an absent executable is a clear result naming both, never a crash
// and never a hard dependency.
func TestRunTestsMissingRunnerProgramDegradesGracefully(t *testing.T) {
	original := lookTestProgram
	lookTestProgram = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { lookTestProgram = original })

	root := writeProject(t, map[string]string{"go.mod": "module example.test/x\n\ngo 1.21\n"})
	res := runTestsCall(t, root, nil)
	if !res.IsError {
		t.Fatalf("a missing runner must be an error result: %q", res.Content)
	}
	for _, want := range []string{"go test", `"go"`, "PATH"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("the unavailable message must contain %q: %q", want, res.Content)
		}
	}
}

// TestRunTestsDropsApogeeCredentialsFromTheRunnerEnvironment pins that a repo-authored test
// runner — untrusted bytes under this threat model — is not handed the key apogee talks to its
// inference server with, while the rest of the operator's environment (the toolchain variables a
// suite actually needs) still travels. This is the targeted removal terminal and python_exec make,
// not git's allowlist.
func TestRunTestsDropsApogeeCredentialsFromTheRunnerEnvironment(t *testing.T) {
	// Not parallel: t.Setenv, plus the package-level program/runner swaps.
	t.Setenv("APOGEE_API_KEY", "sk-secret-value")
	t.Setenv("APOGEE_ENDPOINT", "http://192.0.2.1:1111")
	// A resolver pointing outside the workspace, so neither the exec fence nor the host's own
	// toolchain decides whether this test runs.
	program := filepath.Join(t.TempDir(), "go")
	original := lookTestProgram
	lookTestProgram = func(string) (string, error) { return program, nil }
	t.Cleanup(func() { lookTestProgram = original })
	captured := withCapturedTestRun(t)

	root := writeProject(t, map[string]string{"go.mod": "module example.test/x\n\ngo 1.21\n"})
	runTestsCall(t, root, nil)

	if captured.env == nil {
		t.Fatal("spec.env is nil: the runner would inherit the parent environment whole, credentials included")
	}
	if value, ok := envValue(captured.env, "APOGEE_API_KEY"); ok {
		t.Errorf("APOGEE_API_KEY = %q reached the runner environment, want it dropped", value)
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
		t.Error("PATH did not survive: a test suite runs in the operator's environment")
	}
}

// TestRunTestsDropsTheConfiguredSecretNamesFromTheRunnerEnvironment pins the caller-named half of
// the same subtraction: a variable the host named — the `api-key-env:` credential an operator
// exported into the shell apogee was started from (ADR 0047) — is dropped from the runner's
// environment too, while the toolchain variables a suite needs still travel.
func TestRunTestsDropsTheConfiguredSecretNamesFromTheRunnerEnvironment(t *testing.T) {
	// Not parallel: t.Setenv, plus the package-level program/runner swaps.
	t.Setenv("APOGEE_TEST_PROVIDER_KEY", "sk-configured-value")
	t.Setenv("APOGEE_TEST_ENDPOINT", "http://192.0.2.1:1111")
	// A resolver pointing outside the workspace, so neither the exec fence nor the host's own
	// toolchain decides whether this test runs.
	program := filepath.Join(t.TempDir(), "go")
	original := lookTestProgram
	lookTestProgram = func(string) (string, error) { return program, nil }
	t.Cleanup(func() { lookTestProgram = original })
	captured := withCapturedTestRun(t)

	root := writeProject(t, map[string]string{"go.mod": "module example.test/x\n\ngo 1.21\n"})
	res, err := NewRunTests(root, []string{"apogee_test_provider_key"}).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "run_tests", Arguments: jsonArgs(t, nil),
	})
	if err != nil {
		t.Fatalf("Execute returned a Go error (reserved for cancellation): %v (%q)", err, res.Content)
	}
	if value, ok := envValue(captured.env, "APOGEE_TEST_PROVIDER_KEY"); ok {
		t.Errorf("APOGEE_TEST_PROVIDER_KEY = %q reached the runner environment, want the configured name dropped", value)
	}
	if value, _ := envValue(captured.env, "APOGEE_TEST_ENDPOINT"); value != "http://192.0.2.1:1111" {
		t.Errorf("APOGEE_TEST_ENDPOINT = %q, want it inherited (only the NAMED variables are dropped)", value)
	}
}

// TestRunTestsCondensesRunnerReportedCounts pins the two runners that print their OWN tally: their
// sentence is quoted rather than re-derived, and their failure blocks are recognised — asserted on
// captured output, so neither pytest nor npm has to be installed for the mapping to be pinned.
func TestRunTestsCondensesRunnerReportedCounts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		runner testRunner
		output string
		want   []string
	}{
		{
			name:   "pytest quotes its summary rule and its FAILED lines",
			runner: pytestRunner,
			output: "collected 3 items\n\ntests/test_x.py .F\n\nFAILED tests/test_x.py::test_add - assert 1 == 2\n=========== 1 failed, 2 passed in 0.10s ===========\n",
			want:   []string{"1 failed, 2 passed in 0.10s", "FAILED tests/test_x.py::test_add"},
		},
		{
			name:   "npm quotes jest's Tests: line and its ● blocks",
			runner: npmTestRunner,
			output: "● adds numbers\n\n    expected 1 to equal 2\n\nTests:       1 failed, 2 passed, 3 total\n",
			want:   []string{"Tests:       1 failed, 2 passed, 3 total", "● adds numbers", "expected 1 to equal 2"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := condenseTestOutput(tc.runner, "cmd", subprocessResult{combinedOutput: tc.output, exitCode: 1})
			if !strings.HasPrefix(got, "FAIL ("+tc.runner.name+")") {
				t.Errorf("the verdict must lead: %q", firstLineOf(got))
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("condensed result must contain %q:\n%s", want, got)
				}
			}
			assertUnderCap(t, got)
		})
	}
}

// TestRunTestsCondensesAFailureWithNoRecognisableTest pins the fallback: a run that died before any
// test ran (a compile error, a collection error) still tells the model WHAT stopped it, by quoting
// the head of the log where all three runners report exactly that.
func TestRunTestsCondensesAFailureWithNoRecognisableTest(t *testing.T) {
	t.Parallel()

	output := "# example.test/x [example.test/x.test]\n./x_test.go:6:2: undefined: helper\nFAIL\texample.test/x [build failed]\n"
	got := condenseTestOutput(goTestRunner, "go test ./...", subprocessResult{combinedOutput: output, exitCode: 2})

	if !strings.Contains(got, "undefined: helper") {
		t.Errorf("a build failure's own message must survive the condensing:\n%s", got)
	}
	if !strings.Contains(got, "exit code 2") {
		t.Errorf("the verdict should carry the exit code: %q", firstLineOf(got))
	}
	assertUnderCap(t, got)
}

// TestRunTestsCapKeepsTheClosingNote pins the cap's priority: the note is the line that tells the
// model it is NOT reading everything, so it is the last thing dropped — a body far over the cap is
// cut back around it, never the other way round.
func TestRunTestsCapKeepsTheClosingNote(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&out, "--- FAIL: TestHuge%03d (0.00s)\n", i)
		out.WriteString("    " + strings.Repeat("d", 400) + "\n")
	}
	got := condenseTestOutput(goTestRunner, "go test ./...", subprocessResult{combinedOutput: out.String(), exitCode: 1})

	assertUnderCap(t, got)
	if !strings.HasSuffix(strings.TrimRight(got, "\n"), "full log]") {
		t.Errorf("the closing note must survive the cap:\n%s", got[max(0, len(got)-200):])
	}
	if !strings.Contains(got, "200 failing tests") {
		t.Errorf("the counts line must survive the cap:\n%s", got[:min(200, len(got))])
	}
}

// TestRunTestsCapHoldsAgainstAnOversizedFilter pins the cap against the one input the model itself
// controls: the filter is echoed back inside the closing note, so an enormous filter would carry the
// footer — the part capCondensed reserves room for and never cuts — past the advertised limit.
func TestRunTestsCapHoldsAgainstAnOversizedFilter(t *testing.T) {
	t.Parallel()

	filter := strings.Repeat("T", 20000)
	display := displayCommand(goTestRunner, goTestRunner.args("", filter))
	got := condenseTestOutput(goTestRunner, display, subprocessResult{combinedOutput: "ok\n", exitCode: 0})

	assertUnderCap(t, got)
	if !strings.HasSuffix(strings.TrimRight(got, "\n"), "full log]") {
		t.Errorf("the closing note must survive an oversized filter:\n%s", got[max(0, len(got)-200):])
	}
}

// assertUnderCap fails when a condensed result exceeds the hard cap the tool advertises.
func assertUnderCap(t *testing.T, content string) {
	t.Helper()
	if len(content) > maxRunTestsResultBytes {
		t.Errorf("condensed result is %d bytes, over the %d-byte cap", len(content), maxRunTestsResultBytes)
	}
}

// firstLineOf returns s up to its first newline, for a readable failure message.
func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// requireGo skips a test that shells out to the Go toolchain when the host has none — the same
// graceful posture the tool itself takes (§3a).
func requireGo(t *testing.T) {
	t.Helper()
	if _, err := lookTestProgram("go"); err != nil {
		t.Skip("no go toolchain on PATH")
	}
}
