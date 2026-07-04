package agent

// Loop-level tests for item 10 (sim parity): the retry-in-place appendage (the superseded
// attempt + its correction) must NOT be visible to the post-response scanners on the retry
// cycle. The sim's retry builders copied the request and mutated only the throwaway copy sent
// upstream, so its detectors always ran against the unmutated committed request; apogee mutates
// the request in place, so without the committedLen View() bound a never-executed superseded
// read / a superseded tool-call turn would masquerade as committed history to read_repeat /
// tool_loop_interceptor. These drive real registry-built Mechanisms through scripted responders.

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// wireUserCountContaining counts the user wire messages whose content contains substr.
func wireUserCountContaining(msgs []provider.Message, substr string) int {
	n := 0
	for _, m := range msgs {
		if m.Role == "user" && strings.Contains(m.Content, substr) {
			n++
		}
	}
	return n
}

// TestRetryView_ReadRepeatIgnoresSupersededRead: a validate-retry cycle whose superseded
// attempt read a.go must NOT make read_repeat treat a.go as already-read on the retry — the
// superseded read never executed and lives only in the request-scoped appendage. With the fix
// read_repeat stays inert, validate passes the corrected call, and the read dispatches; without
// it read_repeat would hijack the retry (the read never dispatches).
func TestRetryView_ReadRepeatIgnoresSupersededRead(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	readFile := schemaTool{
		fakeTool: fakeTool{name: "read_file", readOnly: true, ran: &ran, result: "package a\nfunc F() {}"},
		schema:   `{"type":"object","properties":{"path":{"type":"string"},"max_lines":{"type":"integer"}},"required":["path","max_lines"]}`,
	}
	cfg := configWithTools(sink, readFile)
	cfg.Mechanisms = wave1Registry(t, "read_repeat", "validate")
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "read_file", `{"path":"a.go"}`),                 // missing max_lines — validate retries; reads a.go
		toolCallScript("c2", "read_file", `{"path":"a.go","max_lines":100}`), // corrected — must dispatch, not re-fire read_repeat
		contentScript("done"), // next turn's final
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "edit a.go")

	if len(responder.got) != 3 {
		t.Fatalf("provider was called %d times, want 3 (draft, validate retry, next-turn final)", len(responder.got))
	}
	// The superseded read must not be counted: read_repeat never fires this Exchange.
	if n := fireCountFor(sink.events, "read_repeat"); n != 0 {
		t.Errorf("read_repeat fired %d times; the superseded a.go read must not read as committed history", n)
	}
	// validate DID drive the retry cycle whose appendage carried the superseded read.
	if !hasFire(sink.events, "validate", string(domain.ActionRetry)) {
		t.Error("validate did not retry — the test never exercised the retry-appendage path")
	}
	// The corrected read is the call that actually dispatched (read_repeat did not hijack it).
	calls := dispatchedCalls(sink.events)
	if len(calls) != 1 || calls[0].ID != "c2" || string(calls[0].Arguments) != `{"path":"a.go","max_lines":100}` {
		t.Errorf("dispatched calls = %+v, want only the corrected c2 read of a.go", calls)
	}
	if ran != 1 {
		t.Errorf("read_file ran %d times, want 1 (the corrected read executed)", ran)
	}
}

// TestRetryView_RepeatedValidateFailGetsCorrectionNotToolLoop: a model that repeats its
// validate-rejected call gets the validate correction again on the second retry — NOT the
// tool_loop_interceptor "STOP" escalation. The repeat only matches the SUPERSEDED first attempt
// (in the appendage), never a committed turn, so tool_loop must stay inert with the fix.
func TestRetryView_RepeatedValidateFailGetsCorrectionNotToolLoop(t *testing.T) {
	sink := &recordingSink{}
	writeFile := schemaTool{
		fakeTool: fakeTool{name: "write_file", result: "ok"},
		schema:   `{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`,
	}
	cfg := configWithTools(sink, writeFile)
	cfg.Mechanisms = wave1Registry(t, "tool_loop_interceptor", "validate")
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "write_file", `{"path":"a.go"}`), // missing content — validate retries
		toolCallScript("c2", "write_file", `{"path":"a.go"}`), // identical repeat — must get validate, not tool-loop STOP
		contentScript("giving up"),                            // final
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "write a.go")

	if len(responder.got) != 3 {
		t.Fatalf("provider was called %d times, want 3 (draft, validate retry, final)", len(responder.got))
	}
	// The repeat matches only the superseded attempt, so the tool-loop detector must not fire.
	if n := fireCountFor(sink.events, "tool_loop_interceptor"); n != 0 {
		t.Errorf("tool_loop_interceptor fired %d times; the superseded call must not read as the previous turn", n)
	}
	// Both attempts were rejected by validate, so validate retried twice — the model kept getting
	// the correction, never the STOP escalation.
	if n := fireCountFor(sink.events, "validate"); n != 2 {
		t.Errorf("validate fired %d times, want 2 (both rejected attempts got the correction)", n)
	}
	// The third (last) request carries the accumulated appendage; both corrections must be the
	// validate wording, and the tool-loop STOP directive must never have reached the model.
	third := responder.got[2].Messages
	if c := wireUserCountContaining(third, "Your previous tool call had errors"); c != 2 {
		t.Errorf("the retried request carries %d validate corrections, want 2: %+v", c, third)
	}
	if wireUserIndexContaining(third, "STOP. You are in a loop") >= 0 {
		t.Errorf("a tool-loop STOP directive reached the model — the superseded repeat wrongly read as a committed loop: %+v", third)
	}
}
