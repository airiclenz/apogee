package agent

// Loop-level pins for the Seqcom overflow (plan 2026-11-02 - 03, item 5; issue
// fresh-result-overflow, Seqcom run 20261102T1700Z): one `web_fetch` returned a whole model list as
// a single minified JSON line, the next request overflowed the window, the server answered
// HTTP 400 `context_length_exceeded`, and the Turn was abandoned. These drive that Exchange end to
// end through the real provider client: the fresh result is byte-elided to the room left so the
// next request fits, and when the server still refuses it with the verbatim Seqcom body, ADR 0018's
// one emergency fold runs and the retried request completes the Turn.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// seqcomOverflowBody is the 400 body the Seqcom run's server sent, verbatim — the
// context_length_exceeded shape the provider now classes a context overflow.
const seqcomOverflowBody = `{"error":{"message":"prompt exceeds the context window","type":"invalid_request_error","code":"context_length_exceeded"}}`

// elisionMarkerMark is the stable substring of the shared head/tail elision marker a clamped tool
// result carries.
const elisionMarkerMark = "start_line/end_line"

// seqcomOverflowTurn answers one request with the Seqcom run's HTTP 400.
func seqcomOverflowTurn() stubllm.Turn {
	return stubllm.Turn{HTTP: &stubllm.HTTPReply{
		Status:      http.StatusBadRequest,
		Body:        seqcomOverflowBody,
		ContentType: "application/json",
	}}
}

// modelListLine is a model list as one minified JSON line, twice floorWindow tokens long at the
// uncalibrated 4 chars/token — the shape of the OpenRouter `/api/v1/models` document the Seqcom
// run fetched, which the line elision cannot shrink.
func modelListLine() string {
	entry := `{"id":"vendor/model","context_length":131072},`
	return `{"data":[` + strings.Repeat(entry, 2*floorWindow*4/len(entry)+1) + `{"id":"last"}]}`
}

// seqcomAgent is an Agent at floorWindow with Compaction on and only a `web_fetch` tool that
// returns body — no default menu, so the room left in the request is what bounds the result.
func seqcomAgent(t *testing.T, sink domain.EventSink, body string, up *scriptedUpstream) *Agent {
	t.Helper()
	cfg := configWithTools(sink, fakeTool{name: "web_fetch", readOnly: true, result: body})
	cfg.Context.MaxContextTokens = floorWindow
	cfg.Context.CompactionEnabled = true
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	b := a.budget()
	if got := b.EstimateTokens(len(body)); got <= b.Window || strings.Contains(body, "\n") {
		t.Fatalf("test setup: body is %d tokens (want one line past the %d-token window)", got, b.Window)
	}
	return a
}

// wireToolMessage returns the content of the tool message a logged request carried.
func wireToolMessage(t *testing.T, req stubllm.Request) string {
	t.Helper()
	for _, m := range req.Messages {
		if m.Role == string(domain.RoleTool) {
			return m.Content
		}
	}
	t.Fatalf("request %d carried no tool message", req.N)
	return ""
}

// wireTokens estimates a logged request's size through a's Budget: every message's content and
// tool-call arguments.
func wireTokens(a *Agent, req stubllm.Request) int {
	chars := 0
	for _, m := range req.Messages {
		chars += len(m.Content)
		for _, c := range m.ToolCalls {
			chars += len(c.Name) + len(c.Arguments)
		}
	}
	return a.budget().EstimateTokens(chars)
}

// TestFreshOversizedResultFitsTheNextRequest: a tool result larger than the whole window is
// committed byte-elided to the room left, so the request that carries it is estimated within the
// advertised room — no fold needed, no overflow — and the Turn completes.
func TestFreshOversizedResultFitsTheNextRequest(t *testing.T) {
	sink := &recordingSink{}
	up := scriptResponder(t, stubllm.Script{Turns: []stubllm.Turn{
		summaryTurn("UNEXPECTED-SUMMARY"),
		toolCallTurn("c1", "web_fetch", `{"url":"https://openrouter.ai/api/v1/models"}`),
		contentTurn("all done"),
	}})
	body := modelListLine()
	a := seqcomAgent(t, sink, body, up)
	room := freshRoom(t, a)

	res := runExchange(t, a, "list the models")

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("the Exchange surfaced %d ErrorEvent(s) %v; want none", len(errs), errs)
	}
	if n := up.summaryCalls(); n != 0 {
		t.Errorf("summarizer calls = %d, want 0 — the bound alone keeps the request within the room", n)
	}
	mains := up.mains()
	if len(mains) != 2 {
		t.Fatalf("main requests = %d, want 2 (the tool call and the continuation)", len(mains))
	}

	msgs, at := committedToolMessages(a)
	if len(msgs) != 1 {
		t.Fatalf("conversation holds %d tool messages, want 1", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, elisionMarkerMark) || len(msgs[0].Content) >= len(body) {
		t.Errorf("committed result is %d of %d chars and carries no elision marker", len(msgs[0].Content), len(body))
	}
	if next := requestTokens(a, a.conv.Messages()[:at[0]+1]); next > room {
		t.Errorf("the request carrying the result is estimated at %d tokens, past the advertised room of %d", next, room)
	}
	wire := wireToolMessage(t, mains[1])
	if !strings.Contains(wire, elisionMarkerMark) {
		t.Errorf("the continuation's tool message carries no elision marker:\n%.200s", wire)
	}
	if got, window := wireTokens(a, mains[1]), a.budget().Window; got > window {
		t.Errorf("the continuation carried %d tokens, past the %d-token window", got, window)
	}
}

// TestSeqcomOverflowBodyFoldsAndRetries: the server refuses the continuation that carries the
// bounded result with the verbatim Seqcom 400 — an estimator off from the server's tokenizer —
// and the Turn is not abandoned: one emergency fold runs, its summary call fits its budget and
// carries the elided result, and the retried request completes the Exchange.
func TestSeqcomOverflowBodyFoldsAndRetries(t *testing.T) {
	sink := &recordingSink{}
	up := scriptResponder(t, stubllm.Script{Turns: []stubllm.Turn{
		summaryTurn("EMERGENCY-SUMMARY"),
		toolCallTurn("c1", "web_fetch", `{"url":"https://openrouter.ai/api/v1/models"}`),
		seqcomOverflowTurn(),
		contentTurn("recovered reply"),
	}})
	a := seqcomAgent(t, sink, modelListLine(), up)

	res := runExchange(t, a, "list the models")

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("recovery surfaced %d ErrorEvent(s) %v; the Turn must not be abandoned", len(errs), errs)
	}
	if n := up.summaryCalls(); n != 1 {
		t.Fatalf("summarizer calls = %d, want exactly 1 emergency fold", n)
	}
	mains := up.mains()
	if len(mains) != 3 {
		t.Fatalf("main requests = %d, want 3 (the tool call, the refused continuation, the retry)", len(mains))
	}
	if wire := wireToolMessage(t, mains[1]); !strings.Contains(wire, elisionMarkerMark) {
		t.Errorf("the refused continuation's tool message carries no elision marker:\n%.200s", wire)
	}
	assertRequestTemplateLegal(t, mains[2])

	summary := up.lastSummary()
	budget := a.budget().Window - compactMaxTokens
	if got := wireTokens(a, summary); got > budget {
		t.Errorf("the summary call carried %d tokens, past its budget of %d (window less the summary's reply reserve)", got, budget)
	}
	carries := false
	for _, m := range summary.Messages {
		carries = carries || strings.Contains(m.Content, elisionMarkerMark)
	}
	if !carries {
		t.Error("the summary call's transcript carries no elision marker; the elided result did not reach the fold")
	}

	last := a.conv.At(a.conv.Len() - 1)
	if last.Role != domain.RoleAssistant || last.Content != "recovered reply" {
		t.Errorf("the retried request's reply did not land: last message %+v (roles %s)", last, convRoles(a))
	}
}

// The two pins below hold the mechanisms the Seqcom pins above pass through without biting on
// (issue fresh-result-pins-blunt): reverting either one leaves those two green, so each gets a
// loop-level scenario where it alone decides the outcome.

// TestFoldElidesAnOversizedNewestMessageEndToEnd: a second Exchange's prompt larger than the whole
// transcript budget is the most recent message the predictive fold's summary call must carry. A
// tool result cannot be that message — the structural clamp holds it under the History allocation,
// which is at most the transcript budget — so it is an unclamped user prompt past the protected
// prefix. The summary call must fit its budget and carry that prompt byte-elided head-and-tail
// (elideNewestMessage, internal/context/compact.go): kept whole, it overflows the very call meant
// to relieve the overflow.
func TestFoldElidesAnOversizedNewestMessageEndToEnd(t *testing.T) {
	sink := &recordingSink{}
	up := scriptResponder(t, stubllm.Script{Turns: []stubllm.Turn{
		summaryTurn("FOLD-SUMMARY"),
		contentTurn("hello"),
		contentTurn("all done"),
	}})
	cfg := configWithTools(sink)
	cfg.Context.MaxContextTokens = floorWindow
	cfg.Context.CompactionEnabled = true
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	const head, tail = "PROMPT-HEAD:", ":PROMPT-TAIL"
	prompt := head + strings.Repeat("p", 2*floorWindow*4) + tail
	budget := deriveGrowthBounds(a.budget()).transcriptBudget
	if got := a.budget().EstimateTokens(len(prompt)); got <= budget {
		t.Fatalf("test setup: prompt is %d tokens (want past the %d-token transcript budget)", got, budget)
	}

	runExchange(t, a, "hi")
	res := runExchange(t, a, prompt)

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("the Exchange surfaced %d ErrorEvent(s) %v; want none", len(errs), errs)
	}
	if n := up.summaryCalls(); n != 1 {
		t.Fatalf("summarizer calls = %d, want exactly 1 fold of the oversized prompt", n)
	}
	summary := up.lastSummary()
	// The transcript budget plus the prompt overhead it was sized for is the window less the
	// summary's reply reserve.
	if got, room := wireTokens(a, summary), a.budget().Window-compactMaxTokens; got > room {
		t.Errorf("the summary call carried %d tokens, past its budget of %d (window less the summary's reply reserve)", got, room)
	}
	transcript := ""
	for _, m := range summary.Messages {
		transcript += m.Content
	}
	if !strings.Contains(transcript, elisionMarkerMark) {
		t.Error("the summary call's transcript carries no elision marker; the oversized prompt was not elided")
	}
	if !strings.Contains(transcript, head) || !strings.Contains(transcript, tail) {
		t.Error("the summary call's transcript lost the prompt's head or tail; want it elided head-and-tail")
	}
}

// TestFreshResultTakesOnlyTheRoomLeftEndToEnd: on a calibrated window — the predictive guard
// measures the bare room, no uncalibrated margin — a prompt fills the request until less room is
// left than the History allocation, and one call returns a body past the whole window. The result
// is committed at the room left (freshResultBound), not at the allocation, so the request carrying
// it estimates within the room and goes out without a fold. Bounded at the allocation instead, it
// overruns the room and the predictive guard folds before the continuation.
func TestFreshResultTakesOnlyTheRoomLeftEndToEnd(t *testing.T) {
	sink := &recordingSink{}
	up := scriptResponder(t, stubllm.Script{Turns: []stubllm.Turn{
		summaryTurn("UNEXPECTED-SUMMARY"),
		toolCallTurn("c1", "web_fetch", `{"url":"https://openrouter.ai/api/v1/models"}`),
		contentTurn("all done"),
	}})
	a := seqcomAgent(t, sink, modelListLine(), up)
	calibrate(a)
	room, ceiling := freshRoom(t, a), a.requestRoom().structuralFloor()
	// A prompt that leaves half the History allocation in the request: under the ceiling, so the
	// room left is what bounds the result, and over the 1/8 floor, so the share is not floored.
	standing := requestTokens(a, []domain.Message{{Role: domain.RoleUser}})
	prompt := singleLine(a, room-ceiling/2-standing)

	res := runExchange(t, a, prompt)

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
	if n := up.summaryCalls(); n != 0 {
		t.Errorf("summarizer calls = %d, want 0 — a result bounded at the room left needs no fold", n)
	}
	msgs, at := committedToolMessages(a)
	if len(msgs) != 1 {
		t.Fatalf("conversation holds %d tool messages, want 1", len(msgs))
	}
	left := room - requestTokens(a, a.conv.Messages()[:at[0]])
	if floor := ceiling / freshResultFloorDivisor; left >= ceiling || left <= floor {
		t.Fatalf("test setup: %d tokens left at the append, want between the floor %d and the History allocation %d", left, floor, ceiling)
	}
	if next := requestTokens(a, a.conv.Messages()[:at[0]+1]); next > room {
		t.Errorf("the request carrying the result is estimated at %d tokens, past the room of %d", next, room)
	}
	mains := up.mains()
	if len(mains) != 2 {
		t.Fatalf("main requests = %d, want 2 (the tool call and the continuation)", len(mains))
	}
	if got := wireTokens(a, mains[1]); got > room {
		t.Errorf("the continuation carried %d tokens, past the room of %d", got, room)
	}
}
