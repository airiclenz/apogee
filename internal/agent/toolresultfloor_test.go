package agent

// The STRUCTURAL floor on a single oversized tool result (context-overflow-recovery item 5):
// a result whose estimated tokens exceed the ENTIRE History allocation is clamped to the shared
// head/tail elision as it enters the conversation. These tests pin the four properties the item
// names — over the allocation is clamped, under it passes verbatim, an unknown window measures
// against the conservative assumed ceiling instead (unknownwindow_test.go owns that case), and the
// tighter tool-result-cap Floor guard still governs what the REQUEST carries.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// floorWindow is the discovered context window these tests budget against: 8192 tokens ⇒ a
// History allocation of 3,584 tokens — the working room less the measured standing reservations,
// capped at the emergency fold's transcript budget (agent.HistoryCap), which is what binds at this
// window — and, at the uncalibrated 4.0 chars/token, a floor at ~14.3k characters. Small enough to
// overshoot with a readable payload, large enough that the two ceilings (the floor's History
// allocation and the Mechanism's 40% of working room) are far apart enough to test independently.
const floorWindow = 8192

// betweenTheCeilings is a numberedLines payload sized to sit BETWEEN the two ceilings at
// floorWindow: over the tool-result cap's 40%-of-working-room share and under the structural
// floor's whole History allocation. It is DERIVED from the allocation — the largest such payload
// still under 90% of the floor — rather than a pinned line count, because History follows what a
// session's standing content measures (Agent.budget) and a pinned count would silently cross it.
func betweenTheCeilings(t *testing.T) string {
	t.Helper()
	cfg := configWithTools(&recordingSink{})
	cfg.Context.MaxContextTokens = floorWindow
	a, err := newAgent(cfg, echoResponder(t, "unused"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	b := a.budget()
	ceiling := int(float64(b.History)*b.CharsPerToken) * 9 / 10
	n := 1
	for len(numberedLines(n+1)) < ceiling {
		n++
	}
	return numberedLines(n)
}

// numberedLines builds an n-line payload whose first and last lines are distinguishable, so a
// clamped result can be checked for a preserved head and tail around the elision marker.
func numberedLines(n int) string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = fmt.Sprintf("line %04d: tool output that repeats until the result is pathologically large", i)
	}
	return strings.Join(rows, "\n")
}

// floorAgent is an Agent with a known window and no calibration yet, plus its Budget-derived
// floor threshold in characters (History tokens × the chars→token ratio).
func floorAgent(t *testing.T, sink domain.EventSink, tools ...domain.Tool) (*Agent, int) {
	t.Helper()
	cfg := configWithTools(sink, tools...)
	cfg.Context.MaxContextTokens = floorWindow
	a, err := newAgent(cfg, echoResponder(t, "unused"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	b := a.budget()
	if b.History <= 0 {
		t.Fatalf("History allocation = %d, want a positive allocation from window %d", b.History, floorWindow)
	}
	return a, int(float64(b.History) * b.CharsPerToken)
}

// lastToolResultEvent returns the content of the last ToolResultEvent emitted.
func lastToolResultEvent(t *testing.T, events []domain.Event) string {
	t.Helper()
	out, ok := "", false
	for _, e := range events {
		if tre, isResult := e.(domain.ToolResultEvent); isResult {
			out, ok = tre.Result.Content, true
		}
	}
	if !ok {
		t.Fatal("no ToolResultEvent was emitted")
	}
	return out
}

// TestOversizedToolResultIsClampedByTheStructuralFloor drives a REAL Exchange whose tool returns a
// result far larger than the whole History allocation, and proves the floor clamped it on the way
// into the conversation: the committed tool message carries the head, the elision marker, and the
// tail — and so does the emitted ToolResultEvent, since the clamp edits the result itself rather
// than a projection of it.
func TestOversizedToolResultIsClampedByTheStructuralFloor(t *testing.T) {
	sink := &recordingSink{}
	huge := numberedLines(400)
	a, floorChars := floorAgent(t, sink, fakeTool{name: "lookup", readOnly: true, result: huge})
	if len(huge) <= floorChars {
		t.Fatalf("payload of %d chars does not exceed the floor of %d chars; the test proves nothing", len(huge), floorChars)
	}
	a.seating.upstream = newSharedUpstream(scriptedResponder(t,
		toolCallTurn("c1", "lookup", `{"q":"everything"}`),
		contentTurn("all done"),
	))

	runExchange(t, a, "read the whole world")

	committed := ""
	for i := 0; i < a.conv.Len(); i++ {
		if m := a.conv.At(i); m.Role == domain.RoleTool {
			committed = m.Content
		}
	}
	if committed == "" {
		t.Fatal("no tool message reached the conversation")
	}
	if len(committed) >= len(huge) {
		t.Errorf("tool result not clamped: %d chars committed, the raw result was %d", len(committed), len(huge))
	}
	if !strings.Contains(committed, "start_line/end_line") {
		t.Errorf("clamped result missing the elision marker:\n%.200s", committed)
	}
	if !strings.HasPrefix(committed, "line 0000:") {
		t.Errorf("head not preserved: %.60q", committed)
	}
	if !strings.HasSuffix(committed, "line 0399: tool output that repeats until the result is pathologically large") {
		t.Errorf("tail not preserved: %.60q", committed[max(0, len(committed)-60):])
	}
	if got := lastToolResultEvent(t, sink.events); got != committed {
		t.Errorf("ToolResultEvent content differs from the committed message; the raw result reached the transcript:\n%.200s", got)
	}
}

// TestToolResultFloorLeavesEverythingElseVerbatim pins the floor's inert cases — a result under the
// History allocation and a result under the conservative ceiling an unknown window falls back to —
// and the one shape the line elision cannot bound: a single enormous line, which is byte-elided to
// the floor with the marker instead of passing whole (2026-11-02).
func TestToolResultFloorLeavesEverythingElseVerbatim(t *testing.T) {
	sink := &recordingSink{}
	a, floorChars := floorAgent(t, sink)

	underTheFloor := numberedLines(100)
	if len(underTheFloor) >= floorChars {
		t.Fatalf("payload of %d chars is not under the floor of %d chars", len(underTheFloor), floorChars)
	}
	unshrinkable := strings.Repeat("x", floorChars+1) // one enormous line: the line elision's head+tail+marker is LONGER

	noWindow, ceilingChars := unknownWindowAgent(t, &recordingSink{})
	underTheCeiling := numberedLines(20)
	if len(underTheCeiling) >= ceilingChars {
		t.Fatalf("payload of %d chars is not under the assumed ceiling of %d chars", len(underTheCeiling), ceilingChars)
	}

	tests := []struct {
		name    string
		agent   *Agent
		content string
	}{
		{"under the History allocation", a, underTheFloor},
		{"unknown window, under the assumed ceiling", noWindow, underTheCeiling},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.agent.clampToolResult(tc.content); got != tc.content {
				t.Errorf("result was altered (%d chars in, %d out); want it verbatim", len(tc.content), len(got))
			}
		})
	}

	t.Run("single line the head/tail form cannot shrink", func(t *testing.T) {
		got := a.clampToolResult(unshrinkable)
		if len(got) > floorChars {
			t.Errorf("clamped result is %d chars, want at most the floor of %d chars", len(got), floorChars)
		}
		if !strings.Contains(got, "start_line/end_line") {
			t.Errorf("byte-elided result missing the elision marker:\n%.200s", got)
		}
		if !strings.HasPrefix(got, "xxxx") || !strings.HasSuffix(got, "xxxx") {
			t.Errorf("head and tail not both kept around the marker: %.60q ... %.60q", got, got[max(0, len(got)-60):])
		}
	})
}

// TestToolResultFloorByteElidesALongSingleLineResult drives a REAL Exchange whose tool returns one
// line four times floorWindow characters long — the minified-JSON shape the line elision cannot
// shrink — and proves the floor still bounds it on the way into the conversation: the committed
// tool message is under the structural floor and carries the elision marker.
func TestToolResultFloorByteElidesALongSingleLineResult(t *testing.T) {
	sink := &recordingSink{}
	minified := `{"items":[` + strings.Repeat(`{"id":1,"name":"entry"},`, 4*floorWindow/24) + `{"id":0}]}`
	a, floorChars := floorAgent(t, sink, fakeTool{name: "fetch", readOnly: true, result: minified})
	if len(minified) < 4*floorWindow || strings.Contains(minified, "\n") {
		t.Fatalf("payload is %d chars (want one line of at least %d); the test proves nothing", len(minified), 4*floorWindow)
	}
	a.seating.upstream = newSharedUpstream(scriptedResponder(t,
		toolCallTurn("c1", "fetch", `{"url":"https://example.test/api"}`),
		contentTurn("all done"),
	))

	runExchange(t, a, "fetch the whole api")

	committed := ""
	for i := 0; i < a.conv.Len(); i++ {
		if m := a.conv.At(i); m.Role == domain.RoleTool {
			committed = m.Content
		}
	}
	if committed == "" {
		t.Fatal("no tool message reached the conversation")
	}
	if len(committed) > floorChars {
		t.Errorf("committed tool result is %d chars, want at most the floor of %d chars", len(committed), floorChars)
	}
	if !strings.Contains(committed, "start_line/end_line") {
		t.Errorf("byte-elided result missing the elision marker:\n%.200s", committed)
	}
	if !strings.HasPrefix(committed, `{"items":[`) || !strings.HasSuffix(committed, `{"id":0}]}`) {
		t.Errorf("head and tail not both kept: %.60q ... %.60q", committed, committed[max(0, len(committed)-60):])
	}
}

// TestToolResultCapKeepsTheTighterCapAboveTheFloor pins the division of labour: a result between
// the two ceilings — over the guard's 40%-of-working-room cap, under the floor's whole
// History allocation — is committed to the conversation WHOLE (the floor is a floor, not the
// working cap) while the projected request carries the guard's tighter cap. The floor edits
// history; the guard edits only the request.
//
// The guard needs no arming: it is engine behaviour on by default (ADR 0071), so this drives the
// pre-request Moment the loop drives — a.fire — over a stock Config.
func TestToolResultCapKeepsTheTighterCapAboveTheFloor(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink)
	cfg.Context.MaxContextTokens = floorWindow
	a, err := newAgent(cfg, echoResponder(t, "unused"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	b := a.budget()
	floorChars := int(float64(b.History) * b.CharsPerToken)
	// The guard's ceiling is 40% of the working room (floor.toolResultBudgetFraction); the floor's
	// is the whole History allocation, which at this window is the fold's transcript budget and
	// comfortably the larger of the two. A payload at ~90% of the floor sits between them.
	between := betweenTheCeilings(t)
	if len(between) >= floorChars {
		t.Fatalf("payload of %d chars must sit UNDER the floor of %d chars", len(between), floorChars)
	}

	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "go"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "lookup"}}})
	a.appendToolResult(0, domain.ToolCall{}, "", domain.ToolResult{CallID: "c1", Content: between}, "", nil)
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c2", Tool: "lookup"}}})
	a.appendToolResult(0, domain.ToolCall{}, "", domain.ToolResult{CallID: "c2", Content: "small"}, "", nil)

	if got := a.conv.At(2).Content; got != between {
		t.Errorf("the floor clamped a result under its threshold: %d chars committed, want the whole %d", len(got), len(between))
	}

	req, _ := a.buildRequest(0)
	firePreRequest(t, a, req)
	projected := req.State().Messages[2].Content
	if len(projected) >= len(between) {
		t.Fatalf("the tool-result cap did not cap the older result: %d chars projected, was %d", len(projected), len(between))
	}
	if !strings.Contains(projected, "start_line/end_line") {
		t.Errorf("capped request message missing the shared elision marker:\n%.200s", projected)
	}
	if a.conv.At(2).Content != between {
		t.Error("the guard edited the conversation; it may only edit the projected request")
	}

	// The firing is booked as a ReactionFiredEvent naming the guard's own config key.
	if !hasGuardFire(sink.events, guardToolResultCap, guardActionCap) {
		t.Errorf("no %q ReactionFiredEvent; the seam capped without booking a firing", guardToolResultCap)
	}
}

// TestToolResultCapOptOutSendsTheResultWhole pins the key: with Floor.DisableToolResultCap set the
// pre-request seam leaves the oversized older result exactly as the conversation holds it.
func TestToolResultCapOptOutSendsTheResultWhole(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink)
	cfg.Context.MaxContextTokens = floorWindow
	cfg.Floor.DisableToolResultCap = true
	a, err := newAgent(cfg, echoResponder(t, "unused"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	between := betweenTheCeilings(t)
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "go"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "lookup"}}})
	a.appendToolResult(0, domain.ToolCall{}, "", domain.ToolResult{CallID: "c1", Content: between}, "", nil)
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c2", Tool: "lookup"}}})
	a.appendToolResult(0, domain.ToolCall{}, "", domain.ToolResult{CallID: "c2", Content: "small"}, "", nil)

	req, _ := a.buildRequest(0)
	firePreRequest(t, a, req)
	if got := req.State().Messages[2].Content; got != between {
		t.Errorf("the opted-out guard still capped: %d chars projected, want the whole %d", len(got), len(between))
	}
	if hasGuardFire(sink.events, guardToolResultCap, guardActionCap) {
		t.Error("an opted-out guard booked a firing")
	}
}

// The FRESH-result bound (plan 2026-11-02 - 03, item 4): with a known window a tool result is
// committed at no more than its share of the room left in the request — the advertised window less
// the reply reserve, less the estimate of the next request without it — divided across the results
// of the Turn still to append, clamped to [1/8 of the History allocation, the History allocation].
// The tests below observe whole-versus-elided at sizes either side of that share.

// requestTokens estimates, the way freshResultBound does, a request carrying msgs: the standing
// system content buildRequest seeds, msgs, and the tool menu, through the Budget's ratio — the
// request room's own measure (requestRoom.weight).
func requestTokens(a *Agent, msgs []domain.Message) int {
	return a.requestRoom().weight(msgs)
}

// freshRoom is the advertised room the bound and the predictive guard share: window less reply
// reserve (deriveGrowthBounds).
func freshRoom(t *testing.T, a *Agent) int {
	t.Helper()
	g := deriveGrowthBounds(a.budget())
	if !g.windowKnown {
		t.Fatal("the agent has no known window; the room-left bound does not apply")
	}
	return g.room
}

// singleLine is a one-line body of exactly tokens tokens at a's ratio — the shape only the byte
// elision can bound, so the marker in a committed result proves the room-left bound bit.
func singleLine(a *Agent, tokens int) string {
	return strings.Repeat("j", int(float64(tokens)*a.budget().CharsPerToken))
}

// committedToolMessages returns the tool messages of a's conversation with the index each sits at.
func committedToolMessages(a *Agent) (msgs []domain.Message, at []int) {
	for i := 0; i < a.conv.Len(); i++ {
		if m := a.conv.At(i); m.Role == domain.RoleTool {
			msgs, at = append(msgs, m), append(at, i)
		}
	}
	return msgs, at
}

// parallelProbe is a read-only tool whose result is the body its call's "n" argument names, so one
// reply's parallel calls can return different sizes.
func parallelProbe(bodies map[string]string) fakeTool {
	return fakeTool{name: "probe", readOnly: true, execute: func(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
		return domain.ToolResult{CallID: call.ID, Content: bodies[string(call.Arguments)]}, nil
	}}
}

// twoCallTurn is a reply carrying two parallel probe calls.
func twoCallTurn() stubllm.Turn {
	return stubllm.Turn{ToolCalls: []stubllm.ToolCall{
		{ID: "c1", Name: "probe", Arguments: `{"n":1}`},
		{ID: "c2", Name: "probe", Arguments: `{"n":2}`},
	}}
}

// TestFreshResultIsBoundedByTheRoomLeft: with the history already filling half the advertised
// room, a result no larger than the History allocation — so the structural ceiling alone would
// commit it whole — is over the room left, and is committed under it: the next request's estimate
// stays within the advertised room.
func TestFreshResultIsBoundedByTheRoomLeft(t *testing.T) {
	a, floorChars := floorAgent(t, &recordingSink{})
	room := freshRoom(t, a)
	b := a.budget()

	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: singleLine(a, room/2)})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "lookup"}}})
	left := room - requestTokens(a, a.conv.Messages())

	n := 1
	for len(numberedLines(n+1)) <= floorChars {
		n++
	}
	result := numberedLines(n)
	if b.EstimateTokens(len(result)) <= left || len(result) > floorChars {
		t.Fatalf("result of %d tokens must sit over the room left (%d) and within the History allocation (%d)",
			b.EstimateTokens(len(result)), left, b.History)
	}

	a.appendToolResult(0, domain.ToolCall{}, "", domain.ToolResult{CallID: "c1", Content: result}, "", nil)

	committed := a.conv.At(a.conv.Len() - 1).Content
	if got := b.EstimateTokens(len(committed)); got > left {
		t.Errorf("committed result is %d tokens, want at most the %d left in the request", got, left)
	}
	if !strings.Contains(committed, "start_line/end_line") {
		t.Errorf("the bounded result carries no elision marker:\n%.200s", committed)
	}
	if next := requestTokens(a, a.conv.Messages()); next > room {
		t.Errorf("the next request is estimated at %d tokens, past the advertised room of %d", next, room)
	}
}

// TestFreshResultParallelResultsShareTheRoom drives one reply with two parallel calls, each
// returning a single line between half the room left and the History allocation: the first is
// committed at no more than half the room (its even share), the second at no more than the room
// the first left — the bite each side of the share, with the count of results still to append
// carried from dispatchTools.
func TestFreshResultParallelResultsShareTheRoom(t *testing.T) {
	a, _ := floorAgent(t, &recordingSink{})
	room := freshRoom(t, a)
	b := a.budget()
	// The room left once the user line and the reply's two calls ride the request is a little under
	// room; a body halfway from room/2 to History sits over each share and within the structural
	// ceiling.
	size := room/2 + (b.History-room/2)/2
	if size <= room/2 || size > b.History {
		t.Fatalf("body of %d tokens must sit between room/2 (%d) and History (%d)", size, room/2, b.History)
	}
	body := singleLine(a, size)
	cfg := configWithTools(&recordingSink{}, parallelProbe(map[string]string{`{"n":1}`: body, `{"n":2}`: body}))
	cfg.Context.MaxContextTokens = floorWindow
	a, err := newAgent(cfg, scriptedResponder(t, twoCallTurn(), contentTurn("all done")))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runExchange(t, a, "probe both")

	msgs, at := committedToolMessages(a)
	if len(msgs) != 2 {
		t.Fatalf("conversation holds %d tool messages, want 2", len(msgs))
	}
	all := a.conv.Messages()
	leftFirst := room - requestTokens(a, all[:at[0]])
	leftSecond := room - requestTokens(a, all[:at[1]])
	if got := b.EstimateTokens(len(msgs[0].Content)); got > leftFirst/2 {
		t.Errorf("first result committed at %d tokens, want at most its share %d (half of %d left)", got, leftFirst/2, leftFirst)
	}
	if got := b.EstimateTokens(len(msgs[1].Content)); got > leftSecond {
		t.Errorf("second result committed at %d tokens, want at most the %d the first left", got, leftSecond)
	}
	for i, m := range msgs {
		if !strings.Contains(m.Content, "start_line/end_line") {
			t.Errorf("result %d carries no elision marker; the share did not bite", i+1)
		}
	}
}

// TestFreshResultSmallFirstLeavesItsShareToTheNext: the room a small first result leaves unused
// rolls forward — a second result between half the room and the room left (and within History)
// is committed whole.
func TestFreshResultSmallFirstLeavesItsShareToTheNext(t *testing.T) {
	a, _ := floorAgent(t, &recordingSink{})
	room := freshRoom(t, a)
	b := a.budget()
	size := room/2 + (b.History-room/2)/2
	body := singleLine(a, size)
	cfg := configWithTools(&recordingSink{}, parallelProbe(map[string]string{`{"n":1}`: "ok", `{"n":2}`: body}))
	cfg.Context.MaxContextTokens = floorWindow
	a, err := newAgent(cfg, scriptedResponder(t, twoCallTurn(), contentTurn("all done")))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runExchange(t, a, "probe both")

	msgs, at := committedToolMessages(a)
	if len(msgs) != 2 {
		t.Fatalf("conversation holds %d tool messages, want 2", len(msgs))
	}
	leftSecond := room - requestTokens(a, a.conv.Messages()[:at[1]])
	if size <= room/2 || size > min(leftSecond, b.History) {
		t.Fatalf("body of %d tokens must sit between room/2 (%d) and min(room left %d, History %d)", size, room/2, leftSecond, b.History)
	}
	if msgs[0].Content != "ok" || msgs[1].Content != body {
		t.Errorf("results committed at %d and %d chars, want both whole (2 and %d)", len(msgs[0].Content), len(msgs[1].Content), len(body))
	}
}

// TestFreshResultNeverBelowTheFloor pins the floor without a bite on the room: with the history
// already past the advertised room, the bound is 1/8 of the History allocation — not zero — and a
// result over it is committed within it; the predictive fold, not the clamp, makes the rest of
// the room.
func TestFreshResultNeverBelowTheFloor(t *testing.T) {
	a, _ := floorAgent(t, &recordingSink{})
	room := freshRoom(t, a)
	b := a.budget()
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: singleLine(a, room)})

	floor := b.History / freshResultFloorDivisor
	if got := a.freshResultBound(); got != floor {
		t.Fatalf("bound with no room left = %d, want the floor %d (History %d / %d)", got, floor, b.History, freshResultFloorDivisor)
	}
	a.appendToolResult(0, domain.ToolCall{}, "", domain.ToolResult{CallID: "c1", Content: numberedLines(200)}, "", nil)
	if got := b.EstimateTokens(len(a.conv.At(a.conv.Len() - 1).Content)); got > floor {
		t.Errorf("committed result is %d tokens, want at most the floor %d", got, floor)
	}
}

// TestFreshResultUnknownWindowKeepsTheStructuralFloor pins the unknown-window case without a bite:
// with no window there is no room to share, and the bound is structuralFloor() — however full the
// conversation and however many results are still to append.
func TestFreshResultUnknownWindowKeepsTheStructuralFloor(t *testing.T) {
	a, err := newAgent(configWithTools(&recordingSink{}), echoResponder(t, "unused"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: strings.Repeat("u", 20_000)})
	a.turns.resultsToAppend = 4
	if got, want := a.freshResultBound(), a.structuralFloor(); got != want {
		t.Errorf("unknown-window bound = %d, want structuralFloor() = %d", got, want)
	}
}
