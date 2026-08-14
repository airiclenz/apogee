package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The run_tests tool (2026-08-10) — auto-detected runner, condensed output
// ----------------------------------------------------------------------------
//
// run_tests answers one question — does this project's test suite pass, and if not, which
// tests fail and why — WITHOUT handing the model the runner's log. A full `go test ./...`
// or pytest log is exactly the shape of output that buries a small model's context, which is
// the problem this tool exists to solve: the result is a verdict, the counts the runner
// reported, and the first failing tests with their first error lines, hard-capped at
// maxRunTestsResultBytes. The schema says so plainly, so a model that needs the whole log
// knows to reach for terminal instead.
//
// The runner is DETECTED from project markers rather than named by the model (ratified call
// 6): go.mod ⇒ `go test`, a pytest config ⇒ `pytest`, a package.json test script ⇒
// `npm test`, in that precedence order. Detection is a fact about the workspace, so a model
// that has never seen this repository can still run its tests — the discovery lesson this
// plan's poll round produced (capabilities are found by tool NAME, not by parameters).
//
// It is a SubprocessTool with terminal's disposition (not read-only: a test suite runs the
// project's own code, which writes): the dispatch disposition confines it in Auto and gates
// it when fs-confinement is unavailable, over the shared runSubprocess that owns the
// confinement handoff and the §2.4 process-group teardown. It is stateless across Turns
// (ADR 0008) — a fresh runner process per call.

const (
	// runTestsTimeout bounds one test run. A suite is the longest-running thing an execution
	// tool starts, so the ceiling sits well above the 120s subprocess default — but it is
	// still a ceiling: a wedged suite ends as a timed-out result the model can read rather
	// than a Turn nobody can end.
	runTestsTimeout = 300 * time.Second
	// maxRunTestsResultBytes hard-caps the WHOLE condensed result, closing note included.
	// Flooding a small context is the failure this tool is built to prevent, so the cap binds
	// the text that leaves the tool, not merely the part of it the condenser chose to quote.
	maxRunTestsResultBytes = 8192
	// maxCondensedFailures bounds how many failing tests are quoted; the rest are counted in
	// one line, because the eleventh failure almost never says anything the first ten did not.
	maxCondensedFailures = 10
	// maxFailureDetailLines bounds the lines quoted beneath each failing test. The runner
	// prints the assertion that failed FIRST and the stack around it after, so the head of a
	// failure is the half that says why.
	maxFailureDetailLines = 5
	// maxCondensedLineBytes clips one quoted line, so a single enormous line (a dumped diff, a
	// serialized fixture) cannot spend the whole budget on one failure.
	maxCondensedLineBytes = 200
	// maxFallbackLines is how much of the log's HEAD is quoted when a failing run carries no
	// recognisable failing test — a compile or collection error, which every supported runner
	// reports at the top of its output, before it ever runs a test.
	maxFallbackLines = 15
)

var runTestsSpec = toolSpec{
	name: "run_tests",
	// The description states the two things a model cannot learn by calling: that the runner
	// is detected (so no command is expected from it) and that the result is a SUMMARY, never
	// the log — with the escape hatch named, so needing the full output leads to terminal
	// rather than to a second run_tests call that returns the same summary.
	description: "Run the project's test suite and get a CONDENSED summary — never the full log. The runner is detected from project markers: go.mod ⇒ 'go test', pytest.ini or a pyproject.toml pytest section ⇒ 'pytest', package.json with a test script ⇒ 'npm test'. The result gives PASS/FAIL, the counts the runner reports, and the first failing tests with their first error lines, capped at 8 KB. Use terminal when you need the complete output.",
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Optional subtree to test, relative to the workspace root (default: the whole project). It is handed to the detected runner in that runner's own native form"},
    "filter": {"type": "string", "description": "Optional test-name pattern, mapped to the detected runner's own filter flag (go: -run, pytest: -k, npm: -t)"}
  }
}`),
}

type runTestsArgs struct {
	Path   string `json:"path"`
	Filter string `json:"filter"`
}

// testRunner describes one supported test runner: how it is detected, how its argv is built
// from the two optional arguments, and how its output is read back. The three shipped runners
// are values of this type (ratified call 6), so a fourth is a table entry rather than another
// branch inside Execute.
type testRunner struct {
	// name is the runner as the model is told about it, and what the verdict line states.
	name string
	// program is the executable probed on PATH.
	program string
	// marker is the sentence naming what detection looked for, quoted back when nothing matched.
	marker string
	// detect reports whether this runner's marker is present at the workspace root.
	detect func(root string) bool
	// args builds the argv AFTER the program for an optional workspace-relative subtree and an
	// optional test-name filter, each in this runner's native spelling.
	args func(subtree, filter string) []string
	// failureHead reports whether a line announces a failing test — the anchor a condensed
	// failure block starts at.
	failureHead func(line string) bool
	// summary returns the runner's OWN tally line when it prints one, else "". A runner that
	// reports no counts (go test) leaves this nil and the condenser derives one.
	summary func(out string) string
}

// testRunners is the detection order (ratified call 6): the first marker that matches wins, so
// a polyglot repository with both a go.mod and a package.json resolves the same way every time
// rather than by directory-listing order.
var testRunners = []testRunner{goTestRunner, pytestRunner, npmTestRunner}

// goTestRunner runs the Go toolchain's own test driver over a package pattern.
var goTestRunner = testRunner{
	name:    "go test",
	program: "go",
	marker:  "go.mod (go test)",
	detect:  func(root string) bool { return markerFileExists("go.mod", root) },
	args: func(subtree, filter string) []string {
		argv := []string{"test"}
		if filter != "" {
			argv = append(argv, "-run", filter)
		}
		return append(argv, goPackagePattern(subtree))
	},
	// go test heads every failing test with "--- FAIL: <name>", indented for a subtest.
	failureHead: func(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "--- FAIL:") },
}

// pytestRunner runs pytest over an optional path, filtered with -k.
var pytestRunner = testRunner{
	name:    "pytest",
	program: "pytest",
	marker:  "pytest.ini, or a pyproject.toml carrying a [tool.pytest…] section (pytest)",
	detect:  detectPytest,
	args: func(subtree, filter string) []string {
		var argv []string
		if filter != "" {
			argv = append(argv, "-k", filter)
		}
		if subtree != "" {
			argv = append(argv, subtree)
		}
		return argv
	},
	failureHead: pytestFailureHead,
	summary:     pytestSummary,
}

// npmTestRunner runs the project's own `npm test` script.
var npmTestRunner = testRunner{
	name:    "npm test",
	program: "npm",
	marker:  `package.json with a "test" script (npm test)`,
	detect:  detectNPMTest,
	args: func(subtree, filter string) []string {
		argv := []string{"test"}
		if filter == "" && subtree == "" {
			return argv
		}
		// npm forwards nothing to the script behind `npm test` unless the arguments follow a
		// "--" separator, so it is added exactly when there is something to forward.
		argv = append(argv, "--")
		// npm itself defines no filter flag — the JS test runner behind the script does. "-t"
		// is the jest/vitest spelling of --testNamePattern, which covers most of that
		// ecosystem; a project on another runner passes its own flags through terminal.
		if filter != "" {
			argv = append(argv, "-t", filter)
		}
		if subtree != "" {
			argv = append(argv, subtree)
		}
		return argv
	},
	failureHead: npmFailureHead,
	summary:     npmSummary,
}

// RunTests runs the workspace's own test suite through a detected runner and reports a
// condensed verdict. It is a SubprocessTool (domain.SubprocessTool) sharing terminal's
// disposition: confined in Auto, gated when fs-confinement is unavailable, and not read-only —
// a test suite runs the project's code, which writes.
type RunTests struct {
	toolSpec
	root string
	// secretEnv names the host-configured credential variables to drop from the runner's
	// environment beside apogee's own (HostTools.SecretEnvVars); nil drops apogee's own alone.
	secretEnv []string
}

// NewRunTests returns a run_tests tool that detects and runs the suite of the project at root,
// with the secretEnv variables dropped from the runner's environment on top of apogee's own
// credentials (nil ⇒ apogee's own alone — the scrub as it was before the host could name any).
func NewRunTests(root string, secretEnv []string) *RunTests {
	return &RunTests{toolSpec: runTestsSpec, root: root, secretEnv: secretEnv}
}

// ReadOnly reports that run_tests is write-capable (false): a test suite is the project's own
// code, which may write files, so the loop must confine or gate it rather than run it freely.
func (t *RunTests) ReadOnly() bool { return false }

// Subprocess reports that run_tests launches an OS subprocess (the detected runner) — the
// marker the disposition keys on to confine it in Auto rather than gating it.
func (t *RunTests) Subprocess() bool { return true }

// Execute detects the runner, runs it under the run-tests timeout, and returns the condensed
// result: a success result when the suite passed, an error result when it failed (so the model
// sees the failure the way it sees a non-zero terminal exit). No marker, a missing runner
// program, a path escape, and an option-shaped argument are all surfaced as results; only ctx
// cancellation or a confinement-unavailable demotion is a Go error.
func (t *RunTests) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[runTestsArgs](call)
	if !ok {
		return fail, nil
	}

	runner, ok := detectTestRunner(t.root)
	if !ok {
		return errorResult(call.ID, noTestRunnerMessage()), nil
	}

	filter := strings.TrimSpace(args.Filter)
	if looksLikeOption(filter) {
		// The runner is handed an argv (no shell), so a filter beginning with "-" is the one
		// remaining argument-injection class — the same guard the git tools put on a ref.
		return errorResult(call.ID, `filter must not begin with "-": it would be read as an option flag by `+runner.name), nil
	}
	subtree, errMsg := t.subtree(args.Path)
	if errMsg != "" {
		return errorResult(call.ID, errMsg), nil
	}

	program, ok := lookTestProgram(runner.program)
	if !ok {
		// Graceful degradation (§3a): the marker says which runner this project uses, so a
		// missing program is a clear, actionable result rather than a crash or a hard dep.
		return errorResult(call.ID, fmt.Sprintf("%s not available: the project's markers select %s, but no %q executable was found on PATH", runner.name, runner.name, runner.program)), nil
	}
	// The runner is on PATH, but a runner the model can write is the plant-then-exec chain
	// itself — node_modules/.bin ahead of the system entries is the everyday shape of it. The
	// fence refuses it and names the resolved path, so the cause is legible rather than
	// arriving as the "not available" message above.
	if err := refuseExecFromWritablePath(program, t.root, confinementBox(ctx)); err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	runnerArgs := runner.args(subtree, filter)
	// The environment is the caller's MINUS the credential variables (subprocessEnv, the same
	// environment terminal and python_exec run in): a test suite reads the toolchain's own
	// variables (build caches, virtualenv, NODE_PATH) and an allowlist written for git would break
	// runs that work in the user's shell — but the runner it starts is repo-authored code, which
	// under this threat model is untrusted bytes with no business reading the key apogee talks to
	// its inference server with.
	res, err := runTestsSubprocess(ctx, subprocessSpec{
		argv:    append([]string{program}, runnerArgs...),
		dir:     t.root,
		timeout: runTestsTimeout,
		env:     subprocessEnv(t.secretEnv),
	})
	if err != nil {
		return domain.ToolResult{}, err
	}

	condensed := condenseTestOutput(runner, displayCommand(runner, runnerArgs), res)
	if res.exitCode != 0 || res.timedOut {
		return errorResult(call.ID, condensed), nil
	}
	return okResult(call.ID, condensed), nil
}

// subtree resolves the optional path argument to the workspace-relative directory the runner is
// handed, or "" when the call names none. The second return is the model-facing refusal (empty
// when the path is fine): an option-shaped path, a path that escapes the fence, and a path that
// is not there are each reported as themselves rather than as a runner error the model would
// have to decode.
func (t *RunTests) subtree(input string) (rel, errMsg string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ""
	}
	if looksLikeOption(input) {
		return "", `path must not begin with "-": it would be read as an option flag by the test runner`
	}
	abs, err := resolveInRoot(input, t.root)
	if err != nil {
		return "", err.Error()
	}
	rel = filepath.ToSlash(workspaceRelative(abs, t.root))
	if _, err := statInRoot(rel, t.root); err != nil {
		return "", escapeOrMessage(err, "path not found: "+input)
	}
	if rel == "." {
		return "", ""
	}
	return rel, ""
}

// goPackagePattern renders a workspace-relative subtree as the package pattern `go test` takes:
// the whole module when none was named, and the subtree WITH its own subpackages otherwise —
// "test this part of the project" is what a model means by a directory, and go's plain
// directory form would silently test only the one package.
func goPackagePattern(subtree string) string {
	if subtree == "" {
		return "./..."
	}
	return "./" + strings.Trim(subtree, "/") + "/..."
}

// detectTestRunner returns the first runner whose marker is present at root, in the ratified
// precedence order, and ok=false when the project carries none of them.
func detectTestRunner(root string) (testRunner, bool) {
	for _, runner := range testRunners {
		if runner.detect(root) {
			return runner, true
		}
	}
	return testRunner{}, false
}

// noTestRunnerMessage tells the model exactly which markers were looked for and where to go
// instead — a project with a Makefile target or a bespoke script is not a broken project, it is
// one whose test command only the user's own tooling knows.
func noTestRunnerMessage() string {
	markers := make([]string, 0, len(testRunners))
	for _, runner := range testRunners {
		markers = append(markers, runner.marker)
	}
	return "no test runner detected: looked for " + strings.Join(markers, "; ") +
		". Use terminal to run this project's own test command."
}

// markerFileExists reports whether name is a regular file at the workspace root, statted THROUGH
// the fence (a marker reached by a symlink pointing out of the workspace is not this project's).
func markerFileExists(name, root string) bool {
	info, err := statInRoot(name, root)
	return err == nil && !info.IsDir()
}

// readMarkerFile returns the content of a marker file at the workspace root, or "" when it is
// absent, unreadable, over the read cap, or refused by the fence — every one of which means the
// same thing here: this marker does not select a runner.
func readMarkerFile(name, root string) string {
	data, refusal := readWorkspaceFileBounded(name, root)
	if refusal != "" {
		return ""
	}
	return string(data)
}

// detectPytest reports the pytest marker: a pytest.ini, or a pyproject.toml that CONFIGURES
// pytest. The pyproject half looks for the "[tool.pytest" table prefix rather than the mere
// presence of the file, because almost every modern Python project has a pyproject.toml and
// only some of them are tested with pytest.
func detectPytest(root string) bool {
	if markerFileExists("pytest.ini", root) {
		return true
	}
	return strings.Contains(readMarkerFile("pyproject.toml", root), "[tool.pytest")
}

// detectNPMTest reports the npm marker: a package.json whose scripts carry a non-empty "test"
// entry. A package.json without one gives `npm test` nothing to run, so it is not a marker.
func detectNPMTest(root string) bool {
	var pkg struct {
		Scripts struct {
			Test string `json:"test"`
		} `json:"scripts"`
	}
	if err := json.Unmarshal([]byte(readMarkerFile("package.json", root)), &pkg); err != nil {
		return false
	}
	return strings.TrimSpace(pkg.Scripts.Test) != ""
}

// lookTestProgram resolves a runner's executable on PATH (a package var so a test can inject a
// fake resolver — the shape lookGit and lookInterpreter already use). ok=false is the signal the
// tool degrades to a clear "not available" result (§3a).
var lookTestProgram = func(program string) (string, bool) {
	path, err := exec.LookPath(program)
	return path, err == nil
}

// runTestsSubprocess runs the detected test runner (a package var so a test can capture the exact
// argv and environment this tool builds without launching one — the shape runPythonSubprocess
// already uses).
var runTestsSubprocess = runSubprocess

// displayCommand renders the command that produced a result, as the model would type it into
// terminal — the closing note names it so "I need the whole log" has an obvious next step.
func displayCommand(runner testRunner, args []string) string {
	return strings.TrimSpace(runner.program + " " + strings.Join(args, " "))
}

// condenseTestOutput renders a finished run for the model: the verdict, the counts the runner
// reported (or the failing count derived from the blocks recognised here), the first failing
// tests with their first error lines, and a closing note stating the FULL output size beside the
// command that produced it. The model is told plainly that it is reading a summary, so it can
// reach for terminal when it needs everything; the whole result is capped.
//
// A failing run with no recognisable failing test — a compile error, a pytest collection error —
// falls back to the HEAD of the log, because that is where all three runners report the failure
// that stopped them before any test ran.
func condenseTestOutput(runner testRunner, display string, res subprocessResult) string {
	// The command echo is model-supplied text (a filter is copied into it verbatim), so it is
	// clipped like any other quoted line BEFORE the footer reserves room for it. Unclipped, a
	// filter longer than the cap would push the footer alone past the byte limit this tool
	// advertises — the one thing capCondensed cannot cut back.
	display = clipLine(display)
	lines := strings.Split(res.combinedOutput, "\n")
	failed := res.exitCode != 0 || res.timedOut
	blocks := collectFailureBlocks(runner, lines)

	var b strings.Builder
	verdict := "PASS"
	if failed {
		verdict = "FAIL"
	}
	fmt.Fprintf(&b, "%s (%s)", verdict, runner.name)
	switch {
	case res.timedOut:
		fmt.Fprintf(&b, " — timed out after %s", runTestsTimeout)
	case failed:
		fmt.Fprintf(&b, " — exit code %d", res.exitCode)
	}
	b.WriteByte('\n')

	if counts := runner.tally(res.combinedOutput, len(blocks)); counts != "" {
		b.WriteString(counts + "\n")
	}

	// The two meta lines — how many failures were left uncounted, and what this result is a
	// summary OF — are held back as a footer rather than written into the body, because the cap
	// below cuts the body and those two lines are the ones a cut result can least afford to lose.
	footer := make([]string, 0, 2)
	switch {
	case len(blocks) > 0:
		shown := blocks
		if len(shown) > maxCondensedFailures {
			shown = shown[:maxCondensedFailures]
		}
		for _, block := range shown {
			b.WriteString(strings.Join(block, "\n") + "\n")
		}
		if left := len(blocks) - len(shown); left > 0 {
			footer = append(footer, fmt.Sprintf("[… %d more failing test%s]", left, plural(left)))
		}
	case failed:
		for _, line := range headLines(lines, maxFallbackLines) {
			b.WriteString(line + "\n")
		}
	}

	footer = append(footer, fmt.Sprintf("[condensed from %d bytes of output; run `%s` with terminal for the full log]",
		len(res.combinedOutput), display))
	return capCondensed(b.String(), footer)
}

// tally renders the counts line: the runner's own summary sentence when it prints one, else the
// number of failing tests this condenser recognised. A passing run of a runner that reports no
// counts (go test) has nothing to state, and says nothing rather than inventing a figure.
func (r testRunner) tally(out string, failures int) string {
	if r.summary != nil {
		if s := r.summary(out); s != "" {
			return s
		}
	}
	if failures > 0 {
		return fmt.Sprintf("%d failing test%s", failures, plural(failures))
	}
	return ""
}

// collectFailureBlocks walks the output and returns one block per failing test: the line that
// announces it plus the first maxFailureDetailLines lines the runner INDENTED beneath it, each
// clipped. A block ends at the next failure headline, so a long stack trace cannot swallow the
// next test's name.
//
// Indentation is what separates a failure's own message from the log around it: all three runners
// indent the assertion they report under the test that failed, while a line at column 0 is the log
// moving on — the suite's own stdout, a package summary, the next section's rule. Without that
// rule a test that prints while it runs donates its noise to the previous failure's block. Lines
// before the first headline belong to no block and are dropped; the fallback path is what covers a
// failure that never reached a test at all.
func collectFailureBlocks(runner testRunner, lines []string) [][]string {
	var blocks [][]string
	for _, raw := range lines {
		line := clipLine(raw)
		if runner.failureHead(raw) {
			blocks = append(blocks, []string{line})
			continue
		}
		if len(blocks) == 0 || strings.TrimSpace(line) == "" || !isIndented(line) {
			continue
		}
		last := len(blocks) - 1
		if len(blocks[last]) > maxFailureDetailLines {
			continue
		}
		blocks[last] = append(blocks[last], line)
	}
	return blocks
}

// isIndented reports whether a line is written under something rather than at the log's own
// margin — the test above uses it to tell a failure's detail from the next thing the runner said.
func isIndented(line string) bool {
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
}

// headLines returns the first n non-blank lines, each clipped — the fallback quotation for a
// failing run whose output carries no recognisable failing test.
func headLines(lines []string, n int) []string {
	head := make([]string, 0, n)
	for _, raw := range lines {
		line := clipLine(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}
		head = append(head, line)
		if len(head) == n {
			break
		}
	}
	return head
}

// clipLine trims a line's carriage return and cuts it to maxCondensedLineBytes, marking the cut
// so the model knows the line continued. ToValidUTF8 drops the partial rune a byte cut can leave.
func clipLine(line string) string {
	line = strings.TrimRight(line, "\r")
	if len(line) <= maxCondensedLineBytes {
		return line
	}
	return strings.ToValidUTF8(line[:maxCondensedLineBytes], "") + "…"
}

// capCondensed enforces the hard cap on the whole result: the footer is reserved room FIRST (it is
// what tells the model how much it is not seeing, so it is the last thing to lose) and the body is
// cut back to a line boundary to fit around it.
func capCondensed(body string, footer []string) string {
	foot := strings.Join(footer, "\n")
	body = strings.TrimRight(body, "\n")
	limit := maxRunTestsResultBytes - len(foot) - 1 // -1 for the newline that joins the two
	if limit < 0 {
		limit = 0
	}
	if len(body) > limit {
		body = clipToLine(body, limit)
	}
	if body == "" {
		return foot
	}
	return body + "\n" + foot
}

// clipToLine cuts s to at most limit bytes, preferring the last complete line so the result never
// ends mid-sentence; a single line longer than the limit is cut where it must be, with any
// partial rune dropped.
func clipToLine(s string, limit int) string {
	s = s[:limit]
	if i := strings.LastIndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return strings.ToValidUTF8(s, "")
}

// pytestFailureHead recognises pytest's two ways of announcing a failing test: the short summary
// lines ("FAILED tests/test_x.py::test_y - AssertionError") and the underscore rule that heads
// each failure section.
func pytestFailureHead(line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "FAILED ") || strings.HasPrefix(trimmed, "ERROR ") {
		return true
	}
	return strings.HasPrefix(trimmed, "____") && strings.HasSuffix(trimmed, "____")
}

// pytestSummary returns pytest's own tally line ("=== 1 failed, 2 passed in 0.10s ===") with its
// rule of "=" stripped, or "" when the output carries none. The LAST such line is the run's, not
// a per-section header's.
func pytestSummary(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "=") {
			continue
		}
		tally := strings.TrimSpace(strings.Trim(line, "="))
		if strings.Contains(tally, "passed") || strings.Contains(tally, "failed") || strings.Contains(tally, "error") {
			return tally
		}
	}
	return ""
}

// npmFailureHead recognises the failure marks the common JS runners print: jest's "●" block
// heading and "✕" test line, and a TAP "not ok" line.
func npmFailureHead(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, mark := range []string{"●", "✕", "✗", "not ok "} {
		if strings.HasPrefix(trimmed, mark) {
			return true
		}
	}
	return false
}

// npmSummary returns jest's/vitest's own tally line ("Tests: 1 failed, 2 passed, 3 total"), or ""
// when the script behind `npm test` prints none.
func npmSummary(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "Tests:") {
			return line
		}
	}
	return ""
}

// plural returns the "s" a count needs, so a one-failure result does not read "1 failing tests".
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

var (
	_ domain.Tool           = (*RunTests)(nil)
	_ domain.SubprocessTool = (*RunTests)(nil)
)
