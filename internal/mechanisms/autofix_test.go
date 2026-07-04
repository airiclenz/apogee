package mechanisms

import (
	"encoding/json"
	"go/format"
	"os/exec"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// stubFormattersMissing points lookPath at a not-found stub, so no external formatter is
// discovered — the "gracefully absent" path (standing requirement #2). It restores the real
// lookPath after the test, and must not run in parallel (it mutates a package var).
func stubFormattersMissing(t *testing.T) {
	t.Helper()
	prev := lookPath
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { lookPath = prev })
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

// With no external formatter on PATH, a valid-but-unformatted Go payload is still formatted by the
// always-available in-process gofmt (go/format), and the tidied content is written back to the
// call the loop will dispatch.
func TestAutofixFormatsGoInProcess(t *testing.T) {
	stubFormattersMissing(t)

	const unformatted = "package main\nfunc  main(){}\n"
	want, err := format.Source([]byte(unformatted))
	if err != nil {
		t.Fatalf("format.Source(fixture): %v", err)
	}
	if string(want) == unformatted {
		t.Fatal("fixture is already gofmt-clean; pick an unformatted payload")
	}

	resp := responseWith(nil, writeCall("c1", "main.go", unformatted))
	decision := postResponse(t, autofixID, resp)

	if decision.Action != domain.ActionIntercept {
		t.Fatalf("Action = %q, want %q (autofix rewrote the payload in place)", decision.Action, domain.ActionIntercept)
	}
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != string(want) {
		t.Errorf("written-back content = %q, want the gofmt result %q", got, want)
	}
}

// Content in a language whose external formatter is absent is left untouched — no change, no
// error, a no-op decision.
func TestAutofixMissingExternalFormatterDegrades(t *testing.T) {
	stubFormattersMissing(t)

	const py = "x=1\n"
	resp := responseWith(nil, writeCall("c1", "script.py", py))
	decision := postResponse(t, autofixID, resp)

	if decision.Action != "" {
		t.Errorf("Action = %q, want the no-op zero decision when no formatter is available", decision.Action)
	}
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != py {
		t.Errorf("content = %q, want it unchanged (%q) when black is absent", got, py)
	}
}

// Already-gofmt-clean Go is a no-op: nothing to reformat, so autofix does not touch the call.
func TestAutofixAlreadyFormattedIsNoOp(t *testing.T) {
	stubFormattersMissing(t)

	const clean = "package main\n"
	resp := responseWith(nil, writeCall("c1", "main.go", clean))
	decision := postResponse(t, autofixID, resp)

	if decision.Action != "" {
		t.Errorf("Action = %q, want the no-op zero decision for already-formatted content", decision.Action)
	}
	if got := contentArg(t, resp.ToolCalls()[0].Arguments); got != clean {
		t.Errorf("content = %q, want it unchanged (%q)", got, clean)
	}
}

// A non-write tool carries no file content, so autofix never touches it.
func TestAutofixNonWriteToolIsNoOp(t *testing.T) {
	stubFormattersMissing(t)

	call := domain.ToolCall{ID: "c1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"main.go"}`)}
	decision := postResponse(t, autofixID, responseWith(nil, call))
	if decision.Action != "" {
		t.Errorf("Action = %q, want the no-op zero decision for a non-write tool", decision.Action)
	}
}
