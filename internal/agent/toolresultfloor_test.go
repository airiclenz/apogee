package agent

// The STRUCTURAL floor on a single oversized tool result (context-overflow-recovery item 5):
// a result whose estimated tokens exceed the ENTIRE History allocation is clamped to the shared
// head/tail elision as it enters the conversation. These tests pin the four properties the item
// names — over the allocation is clamped, under it passes verbatim, an unknown window is inert,
// and the tighter `tool_result_cap` Mechanism still governs what the REQUEST carries.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// floorWindow is the discovered context window these tests budget against: 8192 tokens ⇒ a
// History allocation of ~3.9k tokens which, at the uncalibrated 4.0 chars/token, puts the floor
// at ~15.7k characters. Small enough to overshoot with a readable payload, large enough that the
// two ceilings (the floor's History allocation and the Mechanism's 40% of working room) are far
// apart enough to test independently.
const floorWindow = 8192

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
	a, err := newAgent(cfg, echoResponder{reply: "unused"})
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
	a.upstream = &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "lookup", `{"q":"everything"}`),
		contentScript("all done"),
	}}

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

// TestToolResultFloorLeavesEverythingElseVerbatim pins the floor's inert cases: a result under the
// History allocation, any result at all while the window is unknown (a zero allocation — no basis
// to bound), and a pathological single-line body the head/tail form cannot shrink (never grown).
func TestToolResultFloorLeavesEverythingElseVerbatim(t *testing.T) {
	sink := &recordingSink{}
	a, floorChars := floorAgent(t, sink)

	underTheFloor := numberedLines(100)
	if len(underTheFloor) >= floorChars {
		t.Fatalf("payload of %d chars is not under the floor of %d chars", len(underTheFloor), floorChars)
	}
	unshrinkable := strings.Repeat("x", floorChars+1) // one enormous line: head+tail+marker is LONGER

	unbudgeted := &recordingSink{}
	noWindow, err := newAgent(baseConfig(unbudgeted), echoResponder{reply: "unused"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if b := noWindow.budget(); b.History != 0 {
		t.Fatalf("History = %d with no configured window, want the zero allocation", b.History)
	}

	tests := []struct {
		name    string
		agent   *Agent
		content string
	}{
		{"under the History allocation", a, underTheFloor},
		{"unknown window", noWindow, numberedLines(400)},
		{"single line the head/tail form cannot shrink", a, unshrinkable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.agent.clampToolResult(tc.content); got != tc.content {
				t.Errorf("result was altered (%d chars in, %d out); want it verbatim", len(tc.content), len(got))
			}
		})
	}
}

// TestToolResultCapKeepsTheTighterCapAboveTheFloor pins the division of labour: a result between
// the two ceilings — over the Mechanism's 40%-of-working-room nudge, under the floor's whole
// History allocation — is committed to the conversation WHOLE (the floor is a floor, not the
// working cap) while the projected request carries the Mechanism's tighter cap. The floor edits
// history; the Mechanism edits only the request.
func TestToolResultCapKeepsTheTighterCapAboveTheFloor(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink)
	cfg.Context.MaxContextTokens = floorWindow
	cfg.EnableMechanisms = []domain.MechanismID{"tool_result_cap"}
	a, err := newAgent(cfg, echoResponder{reply: "unused"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	b := a.budget()
	floorChars := int(float64(b.History) * b.CharsPerToken)
	// The Mechanism's ceiling is 40% of the working room (mechanisms.toolResultBudgetFraction);
	// the floor's is the whole History allocation (~60% of it). A payload at ~85% of the floor
	// sits between the two.
	between := numberedLines(200)
	if len(between) >= floorChars {
		t.Fatalf("payload of %d chars must sit UNDER the floor of %d chars", len(between), floorChars)
	}

	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "go"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "lookup"}}})
	a.appendToolResult(0, domain.ToolResult{CallID: "c1", Content: between})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c2", Tool: "lookup"}}})
	a.appendToolResult(0, domain.ToolResult{CallID: "c2", Content: "small"})

	if got := a.conv.At(2).Content; got != between {
		t.Errorf("the floor clamped a result under its threshold: %d chars committed, want the whole %d", len(got), len(between))
	}

	req, _ := a.buildRequest(0)
	if err := a.runPreRequestHooks(context.Background(), 0, req); err != nil {
		t.Fatalf("runPreRequestHooks: %v", err)
	}
	projected := req.State().Messages[2].Content
	if len(projected) >= len(between) {
		t.Fatalf("tool_result_cap did not cap the older result: %d chars projected, was %d", len(projected), len(between))
	}
	if !strings.Contains(projected, "start_line/end_line") {
		t.Errorf("capped request message missing the shared elision marker:\n%.200s", projected)
	}
	if a.conv.At(2).Content != between {
		t.Error("the Mechanism edited the conversation; it may only edit the projected request")
	}
}
