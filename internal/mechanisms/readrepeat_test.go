package mechanisms

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// A response that only re-reads a file read successfully in a recent turn retries in place with the
// "already read, proceed" correction (apogee-sim detectRepeatReads + retryWithReadRepeatHint @pin).
func TestReadRepeatRetriesOnRedundantReRead(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a\nfunc F() {}"),
	}
	resp := offrampResponse(history, nil, "", readCall("r2", "a.go"))
	d := postResponse(t, readRepeatID, resp)
	if d.Action != domain.ActionRetry {
		t.Fatalf("Action = %q, want ActionRetry", d.Action)
	}
	if !strings.Contains(d.Inject, "already read") || !strings.Contains(d.Inject, "a.go") {
		t.Errorf("retry correction = %q, want the already-read wording naming a.go", d.Inject)
	}
}

// A response that mixes a read with real progress (a write) is not a redundant re-read — read_repeat
// stays inert (apogee-sim requires the WHOLE response to be reads).
func TestReadRepeatInertOnMixedResponse(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
	}
	resp := offrampResponse(history, nil, "", readCall("r2", "a.go"), writeCall("w1", "b.go", "package b"))
	if d := postResponse(t, readRepeatID, resp); d.Action != "" {
		t.Errorf("Action = %q, want no action on a mixed read+write response", d.Action)
	}
}

// Re-reading a file that was NOT read before is legitimate — no recent successful read, no retry.
func TestReadRepeatInertOnNovelRead(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
	}
	resp := offrampResponse(history, nil, "", readCall("r2", "c.go"))
	if d := postResponse(t, readRepeatID, resp); d.Action != "" {
		t.Errorf("Action = %q, want no action when the response reads a not-yet-read file", d.Action)
	}
}
