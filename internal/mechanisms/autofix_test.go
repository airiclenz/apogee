package mechanisms

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

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
// construction-time PATH prober (Deps.LookPath, D3).
func buildAutofix(t *testing.T, look func(string) (string, error)) domain.PostResponseHook {
	t.Helper()
	m, err := Build(autofixID, Deps{LookPath: look})
	if err != nil {
		t.Fatalf("Build(%q): %v", autofixID, err)
	}
	hook, ok := m.(domain.PostResponseHook)
	if !ok {
		t.Fatalf("mechanism %q does not implement PostResponseHook", autofixID)
	}
	return hook
}

// fireAutofix runs one post-response pass over resp and returns its decision.
func fireAutofix(t *testing.T, hook domain.PostResponseHook, resp *domain.Response) domain.PostResponseDecision {
	t.Helper()
	decision, err := hook.PostResponse(t.Context(), resp)
	if err != nil {
		t.Fatalf("PostResponse: %v", err)
	}
	return decision
}

// fakeFormatter writes a hermetic executable stand-in for an external formatter: it swallows
// stdin and emits output, resolvable only through the injected LookPath (never the real PATH).
func fakeFormatter(t *testing.T, output string) string {
	t.Helper()
	dir := t.TempDir()
	data := filepath.Join(dir, "output")
	if err := os.WriteFile(data, []byte(output), 0o644); err != nil {
		t.Fatalf("write formatter output: %v", err)
	}
	script := filepath.Join(dir, "fake-formatter")
	body := "#!/bin/sh\ncat >/dev/null\ncat '" + data + "'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write formatter script: %v", err)
	}
	return script
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

	want := []string{"goimports", "black", "prettier", "rustfmt"}
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

	decision := fireAutofix(t, hook, resp)
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

	if decision := fireAutofix(t, hook, resp); decision.Action != "" {
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
// sanitizePath guard), even when a repairing formatter is available.
func TestAutofixRejectsControlCharacterPath(t *testing.T) {
	t.Parallel()
	hook := buildAutofix(t, resolveOnly("black", fakeFormatter(t, fixedPy)))
	resp := responseWith(nil, writeCall("c1", "evil\npath.py", brokenPy))

	if decision := fireAutofix(t, hook, resp); decision.Action != "" {
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
