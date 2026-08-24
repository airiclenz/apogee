package mechanisms

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// testFormatterTimeout is the bound every test that expects a formatter to SUCCEED runs under.
// The 3s production bound is load-sensitive: under the concurrent `make check` suite (all packages
// in parallel, -race slowdown, several t.Parallel() autofix tests each forking /bin/sh + cat) the
// fork/exec can miss the deadline, and a missed deadline reads as "the formatter produced
// nothing" — silently turning an expected repair into a no-op. The bound now lives on the
// constructed Mechanism, so raising it here changes nothing for production and nothing for the two
// tests that deliberately set a short bound of their own.
const testFormatterTimeout = 60 * time.Second

// The shared broken-Python fixtures: brokenPy carries exactly one syntax issue (an unclosed
// parenthesis), fixedPy is its zero-issue repair, and stillBrokenPy is a "formatted" output that
// carries one issue of its own (an unclosed bracket) — a fix that does not reduce the count.
const (
	brokenPy      = "x = (1\n"
	fixedPy       = "x = (1)\n"
	stillBrokenPy = "y = [2\n"
)

// notFound is a LookPath stub for a host with no external formatters: every probe misses — the
// "gracefully absent" path (standing requirement #2).
func notFound(string) (string, error) { return "", exec.ErrNotFound }

// resolveOnly is a LookPath stub resolving exactly command to path; every other probe misses.
func resolveOnly(command, path string) func(string) (string, error) {
	return func(c string) (string, error) {
		if c == command {
			return path, nil
		}
		return "", exec.ErrNotFound
	}
}

// buildAutofix constructs autofix through the production catalogue with look injected as the
// construction-time PATH prober (Deps.LookPath, D3), under the generous test bound.
func buildAutofix(t *testing.T, look func(string) (string, error)) domain.PostResponseHook {
	t.Helper()
	return buildAutofixBounded(t, look, testFormatterTimeout)
}

// buildAutofixBounded is buildAutofix with an explicit subprocess bound, set on the CONSTRUCTED
// Mechanism (a value, so the copy the test fires is the only one affected) rather than on a
// package global — which is what makes the kill-path test below possible at all.
func buildAutofixBounded(t *testing.T, look func(string) (string, error), timeout time.Duration) domain.PostResponseHook {
	t.Helper()
	m, err := Build(autofixID, Deps{LookPath: look})
	if err != nil {
		t.Fatalf("Build(%q): %v", autofixID, err)
	}
	af, ok := m.Hook.(autofixMechanism)
	if !ok {
		t.Fatalf("mechanism %q is not an autofixMechanism (%T)", autofixID, m.Hook)
	}
	af.timeout = timeout
	return af
}

// buildAutofixScrubbing is buildAutofix with the operator-declared credential names a spawning
// Mechanism receives on its Deps — the injection point the formatter's environment scrub reads.
func buildAutofixScrubbing(t *testing.T, look func(string) (string, error), secretEnv []string) domain.PostResponseHook {
	t.Helper()
	m, err := Build(autofixID, Deps{LookPath: look, SecretEnvVars: secretEnv})
	if err != nil {
		t.Fatalf("Build(%q): %v", autofixID, err)
	}
	af, ok := m.Hook.(autofixMechanism)
	if !ok {
		t.Fatalf("mechanism %q is not an autofixMechanism (%T)", autofixID, m.Hook)
	}
	af.timeout = testFormatterTimeout
	return af
}

// fireAutofix runs one post-response pass over resp with NO subprocess permit on the ctx — the
// default state, in which every external formatter rung is refused — and returns its decision.
func fireAutofix(t *testing.T, hook domain.PostResponseHook, resp *domain.Response) domain.PostResponseDecision {
	t.Helper()
	decision, err := hook.PostResponse(t.Context(), resp)
	if err != nil {
		t.Fatalf("PostResponse: %v", err)
	}
	return decision
}

// firePermitted runs one post-response pass under a granted hook-time subprocess permit — the ctx
// the engine installs for a post-response hook in Auto. conf is the box the permit carries; nil is
// the unfenced grant (Auto with confine-to-workspace off).
func firePermitted(t *testing.T, hook domain.PostResponseHook, resp *domain.Response, conf *domain.Confinement) domain.PostResponseDecision {
	t.Helper()
	ctx := domain.WithSubprocessPermit(t.Context(), domain.SubprocessPermit{Confinement: conf})
	decision, err := hook.PostResponse(ctx, resp)
	if err != nil {
		t.Fatalf("PostResponse: %v", err)
	}
	return decision
}

// fakeConfiner is a hermetic domain.Confiner stand-in. It records the box it was handed and
// whether the formatter's marker already existed at that moment — proving Confine runs BEFORE the
// process — and returns err: nil lets the command run untouched, a refusal is what the rung must
// treat as fatal. Every field is touched only from the test goroutine PostResponse runs on.
type fakeConfiner struct {
	err    error  // what Confine returns
	marker string // optional: the path the formatter creates when it runs

	calls        int
	box          domain.ConfinementBox
	markerAtCall bool
}

func (c *fakeConfiner) Capabilities() domain.ConfinementCaps {
	return domain.ConfinementCaps{FSWrite: true, NetworkEgress: true}
}

func (c *fakeConfiner) Confine(_ context.Context, box domain.ConfinementBox, _ *exec.Cmd) error {
	c.calls++
	c.box = box
	if c.marker != "" {
		_, err := os.Stat(c.marker)
		c.markerAtCall = err == nil
	}
	return c.err
}

// formatterScript writes an executable /bin/sh stand-in for an external formatter and returns its
// path, resolvable only through the injected LookPath (never the real PATH). body runs after stdin
// has been swallowed.
func formatterScript(t *testing.T, body string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "fake-formatter")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\n"+body), 0o755); err != nil {
		t.Fatalf("write formatter script: %v", err)
	}
	return script
}

// formatterOutputFile parks output in a file the fake formatter cats back, keeping the payload out
// of the script's argv.
func formatterOutputFile(t *testing.T, output string) string {
	t.Helper()
	data := filepath.Join(t.TempDir(), "output")
	if err := os.WriteFile(data, []byte(output), 0o644); err != nil {
		t.Fatalf("write formatter output: %v", err)
	}
	return data
}

// fakeFormatter writes a hermetic executable stand-in for an external formatter: it swallows
// stdin and emits output.
func fakeFormatter(t *testing.T, output string) string {
	t.Helper()
	return formatterScript(t, "cat '"+formatterOutputFile(t, output)+"'\n")
}

// markingFormatter is fakeFormatter plus a side effect visible from OUTSIDE the process: it creates
// marker before emitting output, so a test proves whether the subprocess ran AT ALL instead of
// inferring it from the payload.
func markingFormatter(t *testing.T, marker, output string) string {
	t.Helper()
	return formatterScript(t, ": >'"+marker+"'\ncat '"+formatterOutputFile(t, output)+"'\n")
}

// hangingFormatter never emits in time — it sleeps far past any bound a test sets. wrapper true is
// the shape cmd.WaitDelay exists for: the sleep is a CHILD of the shell, so killing the shell
// leaves a grandchild holding the pipes it inherited and cmd.Run() would otherwise block on the
// output copy until that grandchild exits. wrapper false exec's the sleep, so the process the
// runner kills is the only one holding them.
func hangingFormatter(t *testing.T, wrapper bool) string {
	t.Helper()
	if wrapper {
		return formatterScript(t, "sleep 30\n")
	}
	return formatterScript(t, "exec sleep 30\n")
}

// contentArg reads the "content" field back out of a tool call's arguments — how a test inspects
// what autofix wrote back.
func contentArg(t *testing.T, args json.RawMessage) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		t.Fatalf("unmarshal args %q: %v", args, err)
	}
	s, _ := m["content"].(string)
	return s
}

// The formatter table is resolved at construction, once per command (prettier backs two
// languages but is probed once), and a fire never probes PATH again — Deps.LookPath is a
// construction-time seam (D3), not a fire-time one.
func TestAutofixProbesFormattersAtConstructionOnly(t *testing.T) {
	t.Parallel()
	var probed []string
	look := func(command string) (string, error) {
		probed = append(probed, command)
		return "", exec.ErrNotFound
	}
	hook := buildAutofix(t, look)

	want := []string{"goimports", "black", "rustfmt"}
	if !slices.Equal(probed, want) {
		t.Errorf("construction probed %v, want %v (each command once, in ladder order)", probed, want)
	}
	atConstruction := len(probed)

	resp := responseWith(nil,
		writeCall("c1", "script.py", brokenPy),
		writeCall("c2", "main.go", "package main\nfunc main() {\n"),
	)
	fireAutofix(t, hook, resp)
	if got := len(probed); got != atConstruction {
		t.Errorf("firing probed PATH %d more time(s); the formatter table must be construction-cached", got-atConstruction)
	}
}

// A valid-but-unformatted Go payload is left alone: autofix acts only on syntax-broken content
// (the sim's AttemptFix skips a clean check), never unconditional beautification — even though
// the always-available in-process gofmt WOULD reformat it if consulted.
func TestAutofixLeavesCleanContentUntouched(t *testing.T) {
	t.Parallel()
	const unformatted = "package main\nfunc  main(){}\n" // valid Go, not gofmt-clean
	hook := buildAutofix(t, notFound)
	resp := responseWith(nil, writeCall("c1", "main.go", unformatted))

	if decision := fireAutofix(t, hook, resp); decision.Action != "" {
		t.Errorf("Action = %q, want the no-op zero decision for syntactically clean content", decision.Action)
	}
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != unformatted {
		t.Errorf("content = %q, want it untouched (%q) — no beautification of clean content", got, unformatted)
	}
}

// Syntax-broken content whose formatter output reduces the issue count is repaired: the fixed
// payload is written back to the call the loop will dispatch, and the decision is the in-place
// intercept.
func TestAutofixRepairsBrokenContentWhenFormatterImproves(t *testing.T) {
	t.Parallel()
	hook := buildAutofix(t, resolveOnly("black", fakeFormatter(t, fixedPy)))
	resp := responseWith(nil, writeCall("c1", "script.py", brokenPy))

	decision := firePermitted(t, hook, resp, nil)
	if decision.Action != domain.ActionIntercept {
		t.Fatalf("Action = %q, want %q (autofix repaired the payload in place)", decision.Action, domain.ActionIntercept)
	}
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != fixedPy {
		t.Errorf("written-back content = %q, want the repaired %q", got, fixedPy)
	}
}

// Formatter output that does not REDUCE the issue count is discarded (the sim's AttemptFix
// gate): the original payload stays on the call and the pass is a no-op.
func TestAutofixDiscardsFormatThatDoesNotReduceIssues(t *testing.T) {
	t.Parallel()
	hook := buildAutofix(t, resolveOnly("black", fakeFormatter(t, stillBrokenPy)))
	resp := responseWith(nil, writeCall("c1", "script.py", brokenPy))

	if decision := firePermitted(t, hook, resp, nil); decision.Action != "" {
		t.Errorf("Action = %q, want the no-op zero decision when formatting did not reduce the issue count", decision.Action)
	}
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != brokenPy {
		t.Errorf("content = %q, want the original (%q) kept when formatting did not improve it", got, brokenPy)
	}
}

// With every external formatter absent at construction, broken content degrades silently: no
// repairer for Python, and broken Go falls through to the in-process gofmt tail, which cannot
// parse what the checker flagged — both payloads pass through untouched for syntax to correct.
func TestAutofixMissingExternalFormatterDegrades(t *testing.T) {
	t.Parallel()
	const brokenGo = "package main\nfunc main() {\n"
	hook := buildAutofix(t, notFound)
	resp := responseWith(nil,
		writeCall("c1", "script.py", brokenPy),
		writeCall("c2", "main.go", brokenGo),
	)

	if decision := fireAutofix(t, hook, resp); decision.Action != "" {
		t.Errorf("Action = %q, want the no-op zero decision when no formatter can repair", decision.Action)
	}
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != brokenPy {
		t.Errorf("python content = %q, want it unchanged (%q) when black is absent", got, brokenPy)
	}
	if got := contentArg(t, resp.ToolCalls()[1].Arguments); got != brokenGo {
		t.Errorf("go content = %q, want it unchanged (%q) when gofmt cannot parse it", got, brokenGo)
	}
}

// A write path carrying control characters is refused before any formatter sees it (the sim's
// sanitizePath guard), even when a repairing formatter is available AND a permit authorises the
// spawn — so the guard, not the permit gate, is what this test proves.
func TestAutofixRejectsControlCharacterPath(t *testing.T) {
	t.Parallel()
	hook := buildAutofix(t, resolveOnly("black", fakeFormatter(t, fixedPy)))
	resp := responseWith(nil, writeCall("c1", "evil\npath.py", brokenPy))

	if decision := firePermitted(t, hook, resp, nil); decision.Action != "" {
		t.Errorf("Action = %q, want the no-op zero decision for a control-character path", decision.Action)
	}
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != brokenPy {
		t.Errorf("content = %q, want it untouched (%q) behind the sanitizePath guard", got, brokenPy)
	}
}

// A non-write tool carries no file content, so autofix never touches it.
func TestAutofixNonWriteToolIsNoOp(t *testing.T) {
	t.Parallel()
	hook := buildAutofix(t, notFound)
	call := domain.ToolCall{ID: "c1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"main.go"}`)}

	if decision := fireAutofix(t, hook, responseWith(nil, call)); decision.Action != "" {
		t.Errorf("Action = %q, want the no-op zero decision for a non-write tool", decision.Action)
	}
}

// Absence of a hook-time subprocess permit is REFUSAL, not permission: a resolvable, repairing
// formatter is never spawned, which the marker it would have created proves from outside the
// process. This is the mode every rung but Auto lands in.
func TestAutofixWithoutPermitNeverSpawns(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "formatter-ran")
	hook := buildAutofix(t, resolveOnly("black", markingFormatter(t, marker, fixedPy)))
	resp := responseWith(nil, writeCall("c1", "script.py", brokenPy))

	if decision := fireAutofix(t, hook, resp); decision.Action != "" {
		t.Errorf("Action = %q, want the no-op zero decision when nothing authorised a spawn", decision.Action)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the formatter subprocess ran with no permit on the ctx; absence of a permit is refusal")
	}
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != brokenPy {
		t.Errorf("content = %q, want the original %q — an unauthorised rung repairs nothing", got, brokenPy)
	}
}

// The gate skips the SPAWNING rungs only. An in-process rung spawns nothing, so it still runs with
// no permit in sight — which is what keeps autofix's repair available in Plan.
//
// The ladder is built by hand in the production shape (external rung first, in-process rung behind
// it) because the real in-process tail cannot serve as the witness: go/format.Source only succeeds
// on content whose parse errors come from the missing package clause, and it never adds one, so its
// output never reduces checkSyntax's error count.
func TestAutofixWithoutPermitStillRunsInProcessRungs(t *testing.T) {
	t.Parallel()
	externalRan := false
	m := autofixMechanism{
		timeout: testFormatterTimeout,
		repairs: map[string][]repairer{"python": {
			{external: true, run: func(context.Context, spawnGate, string) (string, bool) {
				externalRan = true
				return fixedPy, true
			}},
			{run: func(context.Context, spawnGate, string) (string, bool) { return fixedPy, true }},
		}},
	}
	resp := responseWith(nil, writeCall("c1", "script.py", brokenPy))

	if decision := fireAutofix(t, m, resp); decision.Action != domain.ActionIntercept {
		t.Fatalf("Action = %q, want %q — an in-process rung is not permit-gated", decision.Action, domain.ActionIntercept)
	}
	if externalRan {
		t.Error("the external rung ran without a permit; only the in-process rung may run there")
	}
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != fixedPy {
		t.Errorf("content = %q, want the in-process rung's repair %q", got, fixedPy)
	}
}

// A permit carrying a Confinement confines the command BEFORE it runs: Confine sees the permit's
// own box, and the formatter's marker does not yet exist when it is called.
func TestAutofixConfinesTheFormatterBeforeRunning(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "formatter-ran")
	confiner := &fakeConfiner{marker: marker}
	box := domain.ConfinementBox{WorkspaceRoot: "/work", WritablePaths: []string{"/work/out"}}
	hook := buildAutofix(t, resolveOnly("black", markingFormatter(t, marker, fixedPy)))
	resp := responseWith(nil, writeCall("c1", "script.py", brokenPy))

	decision := firePermitted(t, hook, resp, &domain.Confinement{Confiner: confiner, Box: box})

	if decision.Action != domain.ActionIntercept {
		t.Fatalf("Action = %q, want %q (the confined formatter repaired the payload)", decision.Action, domain.ActionIntercept)
	}
	if confiner.calls != 1 {
		t.Fatalf("Confine called %d time(s), want exactly 1 — the permit's box must be established", confiner.calls)
	}
	if confiner.box.WorkspaceRoot != box.WorkspaceRoot || !slices.Equal(confiner.box.WritablePaths, box.WritablePaths) {
		t.Errorf("Confine got box %+v, want the permit's %+v", confiner.box, box)
	}
	if confiner.markerAtCall {
		t.Error("the formatter had already run when Confine was called; the box must be established first")
	}
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != fixedPy {
		t.Errorf("content = %q, want the repaired %q", got, fixedPy)
	}
}

// A Confiner that cannot establish the box kills the RUNG, never the confinement: the formatter is
// not spawned unfenced as a fallback, and the payload degrades to "left as-is".
func TestAutofixConfinementRefusalSkipsTheRung(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "formatter-ran")
	confiner := &fakeConfiner{err: domain.ErrConfinementUnavailable, marker: marker}
	hook := buildAutofix(t, resolveOnly("black", markingFormatter(t, marker, fixedPy)))
	resp := responseWith(nil, writeCall("c1", "script.py", brokenPy))

	decision := firePermitted(t, hook, resp, &domain.Confinement{Confiner: confiner})

	if decision.Action != "" {
		t.Errorf("Action = %q, want the no-op zero decision when the box could not be established", decision.Action)
	}
	if confiner.calls != 1 {
		t.Errorf("Confine called %d time(s), want exactly 1", confiner.calls)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the formatter ran after Confine refused; a refusal must never fall back to unfenced")
	}
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != brokenPy {
		t.Errorf("content = %q, want the original %q kept when the rung was skipped", got, brokenPy)
	}
}

// The JavaScript/TypeScript rungs are GONE — the one formatter that executed repo-authored code to
// decide how to format. A broken .ts payload is left for syntax to correct even with a permit in
// hand and a resolvable formatter on the injected PATH.
func TestAutofixHasNoJavaScriptFormatterRung(t *testing.T) {
	t.Parallel()
	const brokenTS = "const x = (1\n"
	hook := buildAutofix(t, resolveOnly("prettier", fakeFormatter(t, "const x = 1;\n")))
	resp := responseWith(nil, writeCall("c1", "app.ts", brokenTS))

	if decision := firePermitted(t, hook, resp, nil); decision.Action != "" {
		t.Errorf("Action = %q, want the no-op zero decision — TypeScript has no formatter rung", decision.Action)
	}
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != brokenTS {
		t.Errorf("content = %q, want it untouched (%q); a JS/TS formatter must not be reachable", got, brokenTS)
	}
}

// A cancel while a formatter is in flight stops it: the fire's ctx bounds the subprocess, so
// PostResponse returns promptly with the payload untouched instead of waiting out the timeout.
func TestAutofixCancelStopsAnInFlightFormatter(t *testing.T) {
	t.Parallel()
	hook := buildAutofix(t, resolveOnly("black", hangingFormatter(t, false)))
	resp := responseWith(nil, writeCall("c1", "script.py", brokenPy))

	ctx, cancel := context.WithCancel(domain.WithSubprocessPermit(t.Context(), domain.SubprocessPermit{}))
	time.AfterFunc(150*time.Millisecond, cancel)
	defer cancel()

	start := time.Now()
	decision, err := hook.PostResponse(ctx, resp)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("PostResponse: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("PostResponse took %s after the cancel; the fire's ctx must bound the formatter", elapsed)
	}
	if decision.Action != "" {
		t.Errorf("Action = %q, want the no-op zero decision for a cancelled formatter", decision.Action)
	}
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != brokenPy {
		t.Errorf("content = %q, want the original %q after the cancel", got, brokenPy)
	}
}

// The KILL path is bounded too. A wrapper-shaped formatter leaves a grandchild holding the pipes it
// inherited when the wrapper is killed at the deadline; without cmd.WaitDelay, cmd.Run() would
// block on the output copy until that grandchild exits — freezing the single-goroutine loop for the
// full 30s sleep. Here the bound is a fraction of a second, so a pass that returns in seconds could
// only have come from WaitDelay.
func TestAutofixBoundsTheFormatterKillPath(t *testing.T) {
	t.Parallel()
	const bound = 250 * time.Millisecond
	hook := buildAutofixBounded(t, resolveOnly("black", hangingFormatter(t, true)), bound)
	resp := responseWith(nil, writeCall("c1", "script.py", brokenPy))

	start := time.Now()
	decision := firePermitted(t, hook, resp, nil)
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("PostResponse took %s with a %s bound; the kill path is unbounded", elapsed, bound)
	}
	if decision.Action != "" {
		t.Errorf("Action = %q, want the no-op zero decision for a formatter that never produced output", decision.Action)
	}
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != brokenPy {
		t.Errorf("content = %q, want the original %q when the formatter was killed", got, brokenPy)
	}
}

// TestAutofixRefusesAFormatterInsideTheWritableBox pins the exec fence at autofix's single
// resolution site. A formatter that resolves inside the workspace is treated exactly as an
// absent one — its rung is left out of the ladder — because bytes the model was allowed to write
// must never become the program a later spawn runs.
//
// The refusal has to sit at CONSTRUCTION rather than at the spawn: a permit may authorise an
// unfenced spawn (nil Confinement, the shape a host with no confinement backend produces), and
// that is precisely the run whose argv[0] must not come out of the box. python is the language
// under test because Go's ladder ends in the in-process gofmt tail, which would mask the
// difference between a skipped rung and a repaired payload.
//
// The fence is the WHOLE box, not just its root: an operator-declared extra writable path (the
// arm below) is as model-writable as the workspace, and the session scratch dir arrives as one of
// those paths, so a formatter resolving inside either is refused the same way.
func TestAutofixRefusesAFormatterInsideTheWritableBox(t *testing.T) {
	t.Parallel()

	// plant writes an executable stand-in for black at path, creating its parents — the shape a
	// formatter shipped inside a writable tree (node_modules/.bin, a vendored venv) has.
	plant := func(path string) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write formatter: %v", err)
		}
		return path
	}

	workspace := t.TempDir()
	extra := t.TempDir() // an operator-declared writable path outside the workspace root
	inside := plant(filepath.Join(workspace, "node_modules", ".bin", "black"))
	inExtra := plant(filepath.Join(extra, "bin", "black"))
	outside := fakeFormatter(t, fixedPy)

	ladder := func(t *testing.T, box domain.ConfinementBox, path string) []repairer {
		t.Helper()
		m, err := Build(autofixID, Deps{
			LookPath:    resolveOnly("black", path),
			WritableBox: box,
		})
		if err != nil {
			t.Fatalf("Build(%q): %v", autofixID, err)
		}
		af, ok := m.Hook.(autofixMechanism)
		if !ok {
			t.Fatalf("mechanism %q is not an autofixMechanism (%T)", autofixID, m.Hook)
		}
		return af.repairs["python"]
	}

	workspaceOnly := domain.ConfinementBox{WorkspaceRoot: workspace}
	withExtra := domain.ConfinementBox{WorkspaceRoot: workspace, WritablePaths: []string{extra}}

	if rungs := ladder(t, workspaceOnly, inside); len(rungs) != 0 {
		t.Errorf("python ladder = %d rung(s), want 0 — a formatter inside the writable box must be left out", len(rungs))
	}
	if rungs := ladder(t, withExtra, inExtra); len(rungs) != 0 {
		t.Errorf("python ladder = %d rung(s), want 0 — a formatter inside an extra writable path must be left out", len(rungs))
	}
	// The control arm: the same formatter OUTSIDE the box is still laddered, so the test pins
	// the fence rather than a broken probe.
	if rungs := ladder(t, withExtra, outside); len(rungs) != 1 {
		t.Errorf("python ladder = %d rung(s), want 1 for a formatter outside the box", len(rungs))
	}
}

// TestAutofixScrubsApogeeCredentialsFromTheFormatter pins that the formatter rung spawns through
// internal/tools' subprocess funnel (tools.RunHookSubprocess) rather than an exec.Command of its
// own. The funnel's credential scrub is the observable: a formatter that echoes the variables back
// into its output sees apogee's own key EMPTY while an ordinary variable still arrives, which no
// hand-rolled spawn inheriting os.Environ() could produce.
func TestAutofixScrubsApogeeCredentialsFromTheFormatter(t *testing.T) {
	// Not parallel: t.Setenv.
	t.Setenv("APOGEE_API_KEY", "sk-secret-value")
	t.Setenv("APOGEE_TEST_ENDPOINT", "http://192.0.2.1:1111")

	echoing := formatterScript(t, `printf 'x = (1)  # key:%s endpoint:%s\n' "$APOGEE_API_KEY" "$APOGEE_TEST_ENDPOINT"`+"\n")
	hook := buildAutofix(t, resolveOnly("black", echoing))
	resp := responseWith(nil, writeCall("c1", "script.py", brokenPy))

	if decision := firePermitted(t, hook, resp, nil); decision.Action != domain.ActionIntercept {
		t.Fatalf("Action = %q, want %q (the formatter repaired the payload)", decision.Action, domain.ActionIntercept)
	}
	want := "x = (1)  # key: endpoint:http://192.0.2.1:1111\n"
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != want {
		t.Errorf("content = %q, want %q — apogee's key must not reach the formatter", got, want)
	}
}

// TestAutofixScrubsTheConfiguredKeyVariablesFromTheFormatter pins the whole route the
// operator-declared `api-key-env:` names (ADR 0047) travel: Deps.SecretEnvVars at construction →
// the spawn gate → tools.RunHookSubprocess → the child's environment. Until Deps carried them a
// formatter inherited the operator's key while terminal/python_exec/run_tests dropped it; the
// control variable proves the scrub is still a subtraction rather than an emptied environment.
func TestAutofixScrubsTheConfiguredKeyVariablesFromTheFormatter(t *testing.T) {
	// Not parallel: t.Setenv.
	t.Setenv("APOGEE_TEST_PROVIDER_KEY", "sk-configured-value")
	t.Setenv("APOGEE_TEST_ENDPOINT", "http://192.0.2.1:1111")

	echoing := formatterScript(t, `printf 'x = (1)  # key:%s endpoint:%s\n' "$APOGEE_TEST_PROVIDER_KEY" "$APOGEE_TEST_ENDPOINT"`+"\n")
	hook := buildAutofixScrubbing(t, resolveOnly("black", echoing), []string{"APOGEE_TEST_PROVIDER_KEY"})
	resp := responseWith(nil, writeCall("c1", "script.py", brokenPy))

	if decision := firePermitted(t, hook, resp, nil); decision.Action != domain.ActionIntercept {
		t.Fatalf("Action = %q, want %q (the formatter repaired the payload)", decision.Action, domain.ActionIntercept)
	}
	want := "x = (1)  # key: endpoint:http://192.0.2.1:1111\n"
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != want {
		t.Errorf("content = %q, want %q — an operator-declared key must not reach the formatter", got, want)
	}
}
