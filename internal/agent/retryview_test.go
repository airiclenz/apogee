package agent

// Loop-level tests for item 10 (sim parity): the retry-in-place appendage (the superseded
// attempt + its correction) must NOT be visible to the post-response scanners on the retry
// cycle. The sim's retry builders copied the request and mutated only the throwaway copy sent
// upstream, so its detectors always ran against the unmutated committed request; apogee mutates
// the request in place, so without the committedLen View() bound a never-executed superseded
// read / a superseded tool-call turn would masquerade as committed history to read_repeat /
// tool_loop_interceptor. These drive real registry-built Mechanisms through scripted responders.

import (
	"context"
	"reflect"
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

// ---------------------------------------------------------------------------
// F2 — committedLen boundary maintenance for the empty-superseded retry (item 2).
//
// The empty-superseded path breaks the item-10 boundary: nothing is appended, so the retry
// correction lands BELOW the frozen committedLen — the insert-before-last-user branch would
// evict the real user ask from View(), and the system-prepend branch would shift every index
// while the boundary stayed frozen (evicting the newest tool result). F2's fix maintains the
// boundary: a below-boundary insert or a system prepend advances committedLen so View() stays
// pinned to the same committed history. State() (the model-facing projection) is untouched —
// asserted byte-identical below.
// ---------------------------------------------------------------------------

// viewCaptureHook records the conversation View() it is handed on each post-response pass that
// reaches it. A catalogued ActionRetry short-circuits the cascade before the experimental
// hooks, so this only fires on a pass whose response STANDS (the retry pass's recovered reply)
// — exactly the committedLen-bounded View() under test. It keeps the ConversationView itself so
// the test can exercise the real LastUser / ResultFor helpers the scanners key on; the view is
// stable once its pass's response stands (the loop appends the committed message to a.conv, not
// to the request the view is bound to).
type viewCaptureHook struct {
	views *[]domain.ConversationView
}

func (h viewCaptureHook) PostResponse(_ context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	*h.views = append(*h.views, resp.View().Conversation())
	return domain.PostResponseDecision{}, nil
}

// emptyRecoveryWithCapture builds a config that runs the production empty_response_recovery
// off-ramp AND an experimental view-capturing hook, so a test can drive a real empty-retry
// cycle and observe the committedLen-bounded View() on the retry pass.
func emptyRecoveryWithCapture(t *testing.T, sink domain.EventSink, views *[]domain.ConversationView, tools ...domain.Tool) domain.Config {
	t.Helper()
	cfg := configWithTools(sink, tools...)
	cfg.Mechanisms = wave1Registry(t, "empty_response_recovery")
	if err := cfg.Mechanisms.AddExperimental(domain.HookPostResponse, viewCaptureHook{views: views}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}
	return cfg
}

// convUserCount counts the user messages in cv whose content equals content.
func convUserCount(cv domain.ConversationView, content string) int {
	n := 0
	cv.Range(func(_ int, m domain.Message) bool {
		if m.Role == domain.RoleUser && m.Content == content {
			n++
		}
		return true
	})
	return n
}

// TestRetryView_EmptySupersededExchangeOpeningKeepsRealUserAsk: an Exchange-opening empty reply
// retries in place; because nothing is appended the recovery nudge is inserted BELOW the frozen
// committedLen. With F2's boundary maintenance the retry pass's View() still ends at the REAL
// user ask (the nudge is a mid-history user message, inert to the scanners), and the retried
// request the model sees is byte-identical (State() carries the nudge then the real ask).
func TestRetryView_EmptySupersededExchangeOpeningKeepsRealUserAsk(t *testing.T) {
	sink := &recordingSink{}
	var views []domain.ConversationView
	cfg := emptyRecoveryWithCapture(t, sink, &views,
		fakeTool{name: "read_file", readOnly: true, result: "contents"})
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		emptyScript(),              // Exchange-opening empty reply — empty_response_recovery retries
		contentScript("recovered"), // the retry pass; its bounded View() is what the capture observes
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "please implement the parser")

	if len(responder.got) != 2 {
		t.Fatalf("provider was called %d times, want 2 (empty draft, retry)", len(responder.got))
	}
	if !hasFire(sink.events, "empty_response_recovery", string(domain.ActionRetry)) {
		t.Fatal("empty_response_recovery did not retry — the empty-superseded path was never exercised")
	}
	if len(views) != 1 {
		t.Fatalf("captured %d retry-pass views, want 1", len(views))
	}
	cv := views[0]

	// The last user message the scanners key on is the REAL ask, not the recovery nudge —
	// without F2 the below-boundary insert evicts the real ask, leaving only the nudge in View().
	if msg, _, ok := cv.LastUser(); !ok || msg.Content != "please implement the parser" {
		t.Errorf("View() LastUser = %q (ok=%v), want the real user ask", msg.Content, ok)
	}
	// No committed message is missing from View(): the real ask is present.
	if convUserCount(cv, "please implement the parser") != 1 {
		t.Errorf("the committed user ask is missing from the bounded View() (len=%d)", cv.Len())
	}
	// Accepted F2 residual: the nudge is still VISIBLE as a mid-history user message before the
	// real ask — it just no longer evicts or shifts committed history.
	if convUserCount(cv, wave1Nudge) != 1 {
		t.Errorf("the recovery nudge is not visible in View() as the F2 residual expects (len=%d)", cv.Len())
	}

	// State() byte-identical: the retried request carries the nudge then the real ask, unchanged
	// by the View-only committedLen fix (native profile ⇒ wire messages == State().Messages).
	want := []provider.Message{
		{Role: "user", Content: wave1Nudge},
		{Role: "user", Content: "please implement the parser"},
	}
	if got := responder.got[1].Messages; !reflect.DeepEqual(got, want) {
		t.Errorf("retried request messages = %+v, want %+v", got, want)
	}
}

// TestRetryView_EmptySupersededToolContinuationKeepsToolResult: a tool-continuation turn (its
// request carries no domain system message) returns empty; the recovery nudge is prepended as a
// system message, shifting every index. With F2's boundary maintenance the retry pass's View()
// still ends at the newest tool result (ResultFor resolves it; no dangling assistant tool-call
// tail), and the model-facing retried request carries the full committed exchange in order.
func TestRetryView_EmptySupersededToolContinuationKeepsToolResult(t *testing.T) {
	sink := &recordingSink{}
	var views []domain.ConversationView
	ran := 0
	cfg := emptyRecoveryWithCapture(t, sink, &views,
		fakeTool{name: "read_file", readOnly: true, ran: &ran, result: "package a\nfunc F() {}"})
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "read_file", `{"path":"a.go"}`), // turn 0: a tool call commits assistant + tool result
		emptyScript(),              // turn 1: empty reply — empty_response_recovery retries
		contentScript("recovered"), // the retry pass; its bounded View() is what the capture observes
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "read a.go")

	if len(responder.got) != 3 {
		t.Fatalf("provider was called %d times, want 3 (tool call, empty draft, retry)", len(responder.got))
	}
	if ran != 1 {
		t.Errorf("read_file ran %d times, want 1", ran)
	}
	if !hasFire(sink.events, "empty_response_recovery", string(domain.ActionRetry)) {
		t.Fatal("empty_response_recovery did not retry — the tool-continuation empty path was never exercised")
	}
	if len(views) == 0 {
		t.Fatal("no retry-pass view captured")
	}
	cv := views[len(views)-1] // the turn-1 retry pass

	// View() still ends at the newest tool result — without F2 the system prepend shifts every
	// index and the frozen boundary evicts the tool result, leaving a dangling assistant tail.
	if last := cv.At(cv.Len() - 1); last.Role != domain.RoleTool || last.ToolCallID != "c1" {
		t.Errorf("View() ends at %+v, want the newest tool result (role tool, call c1)", last)
	}
	res, i, ok := cv.ResultFor("c1")
	if !ok || i != cv.Len()-1 || res.Content != "package a\nfunc F() {}" {
		t.Errorf("ResultFor(c1) in View() = %+v idx=%d (ok=%v), want the committed tool result last", res, i, ok)
	}

	// State() unchanged: the retried request leads with the prepended nudge, then the real ask,
	// the superseded assistant call and its tool result — in order.
	retried := responder.got[2].Messages
	gotRoles := make([]string, len(retried))
	for j, m := range retried {
		gotRoles[j] = m.Role
	}
	if want := []string{"system", "user", "assistant", "tool"}; !reflect.DeepEqual(gotRoles, want) {
		t.Errorf("retried request role sequence = %v, want %v", gotRoles, want)
	}
	if retried[0].Content != wave1Nudge {
		t.Errorf("retried request does not lead with the prepended nudge: %q", retried[0].Content)
	}
	if wireMessageIndex(retried, "user", "read a.go") < 0 {
		t.Errorf("the real user ask is missing from the retried request: %+v", retried)
	}
	if tc := retried[2].ToolCalls; len(tc) != 1 || tc[0].ID != "c1" {
		t.Errorf("committed assistant tool calls = %+v, want the c1 read call", tc)
	}
	if retried[3].ToolCallID != "c1" || retried[3].Content != "package a\nfunc F() {}" {
		t.Errorf("committed tool result = %+v, want c1's result", retried[3])
	}
}

// TestRetryView_DoubleEmptyRetryKeepsBoundary: two consecutive empty retries in one Turn each
// insert their nudge below the boundary; F2 advances committedLen on each, so after the
// accumulation the retry pass's View() still ends at the real user ask (multi-retry).
func TestRetryView_DoubleEmptyRetryKeepsBoundary(t *testing.T) {
	sink := &recordingSink{}
	var views []domain.ConversationView
	cfg := emptyRecoveryWithCapture(t, sink, &views,
		fakeTool{name: "read_file", readOnly: true, result: "contents"})
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		emptyScript(),              // attempt 0 empty → retry
		emptyScript(),              // attempt 1 empty → retry
		contentScript("recovered"), // attempt 2 stands
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "please implement the parser")

	if len(responder.got) != 3 {
		t.Fatalf("provider was called %d times, want 3 (two empty retries + recovery)", len(responder.got))
	}
	if n := fireCountFor(sink.events, "empty_response_recovery"); n != 2 {
		t.Errorf("empty_response_recovery fired %d times, want 2 (both empty replies retried)", n)
	}
	if len(views) != 1 {
		t.Fatalf("captured %d retry-pass views, want 1", len(views))
	}
	cv := views[0]

	// The real ask is still the last user message after two below-boundary inserts.
	if msg, _, ok := cv.LastUser(); !ok || msg.Content != "please implement the parser" {
		t.Errorf("after two retries View() LastUser = %q (ok=%v), want the real user ask", msg.Content, ok)
	}
	// Both nudges are visible mid-history (the accepted F2 residual accumulates below the boundary).
	if n := convUserCount(cv, wave1Nudge); n != 2 {
		t.Errorf("View() carries %d recovery nudges, want 2 (both accumulated below the boundary)", n)
	}
}
