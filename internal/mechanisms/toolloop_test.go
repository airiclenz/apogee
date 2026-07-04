package mechanisms

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// A response repeating the previous turn's exact tool calls retries in place with the loop-breaking
// directive (apogee-sim detectToolCallLoop + retryWithToolLoopDirective @pin).
func TestToolLoopRetriesOnIdenticalRepeat(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("build the thing"),
		assistantCall(writeCall("w1", "a.go", "package main")),
		toolResult("w1", "ok"),
	}
	resp := offrampResponse(history, nil, "", writeCall("w2", "a.go", "package main"))
	d := postResponse(t, toolLoopInterceptorID, resp)
	if d.Action != domain.ActionRetry {
		t.Fatalf("Action = %q, want ActionRetry", d.Action)
	}
	if !strings.Contains(d.Inject, "in a loop") {
		t.Errorf("directive = %q, want the loop-breaking wording", d.Inject)
	}
}

// A response with different tool calls than the previous turn is not a loop — no retry.
func TestToolLoopInertOnDifferentCalls(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("build the thing"),
		assistantCall(writeCall("w1", "a.go", "package main")),
		toolResult("w1", "ok"),
	}
	resp := offrampResponse(history, nil, "", writeCall("w2", "b.go", "package b"))
	if d := postResponse(t, toolLoopInterceptorID, resp); d.Action != "" {
		t.Errorf("Action = %q, want no action on differing calls", d.Action)
	}
}

// With no previous tool-call turn there is nothing to loop against — the first tool-call response
// never fires the interceptor.
func TestToolLoopInertWithoutPreviousTurn(t *testing.T) {
	t.Parallel()
	history := []domain.Message{userMsg("build the thing")}
	resp := offrampResponse(history, nil, "", writeCall("w1", "a.go", "package main"))
	if d := postResponse(t, toolLoopInterceptorID, resp); d.Action != "" {
		t.Errorf("Action = %q, want no action on the first tool-call turn", d.Action)
	}
}
