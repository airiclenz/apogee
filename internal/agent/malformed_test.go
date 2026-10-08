package agent

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// malformedTurnScript is one reply of visible text and a native tool call, ended by the given
// terminal Delta — the shape a stream that skipped undecodable chunks still delivers. A Delta
// script rather than a stubllm Turn: the stub's Script cannot emit malformed SSE, so the drop
// reaches the loop only as the count the provider puts on the terminal Delta.
func malformedTurnScript(terminal provider.Delta) scriptedDeltas {
	return scriptedDeltas{
		{Kind: provider.DeltaContent, Content: "looking it up"},
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID: "c1", Type: "function",
			Function: provider.FunctionCall{Name: "lookup", Arguments: `{"q":"meaning"}`},
		}},
		terminal,
	}
}

// TestMalformedChunksOnASuccessfulReplyEmitOneNote pins the engine half of the drop's surface: a
// reply whose stream ended on Done after skipping undecodable chunks is used as delivered — its
// tool call runs and the Turn completes — and the loop says so with exactly one
// MalformedChunksEvent carrying the count. A clean reply emits none, and a faulted stream keeps
// its existing fault (whose message already names the count) with no note beside it.
func TestMalformedChunksOnASuccessfulReplyEmitOneNote(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		terminal    provider.Delta
		wantNotes   int
		wantCount   int
		wantFaulted bool
		wantRan     int
	}{
		{
			name:      "a successful reply that dropped two chunks emits one note with the count",
			terminal:  provider.Delta{Kind: provider.DeltaDone, FinishReason: "tool_calls", MalformedChunks: 2},
			wantNotes: 1,
			wantCount: 2,
			wantRan:   1,
		},
		{
			name:     "a clean successful reply emits no note",
			terminal: provider.Delta{Kind: provider.DeltaDone, FinishReason: "tool_calls"},
			wantRan:  1,
		},
		{
			name: "a faulted stream keeps its fault and emits no note",
			terminal: provider.Delta{
				Kind:            provider.DeltaError,
				Err:             "apogee: read stream: boom (3 malformed chunks skipped)",
				MalformedChunks: 3,
			},
			wantFaulted: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink := &recordingSink{}
			ran := 0
			cfg := configWithTools(sink, fakeTool{name: "lookup", readOnly: true, ran: &ran, result: "42"})
			a, err := newAgent(cfg, malformedTurnScript(tc.terminal))
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}

			res := stepOnce(t, a, "look it up")

			var notes []domain.MalformedChunksEvent
			for _, e := range sink.events {
				if note, ok := e.(domain.MalformedChunksEvent); ok {
					notes = append(notes, note)
				}
			}
			if len(notes) != tc.wantNotes {
				t.Fatalf("MalformedChunksEvents = %d (%v), want %d", len(notes), notes, tc.wantNotes)
			}
			if tc.wantNotes == 1 && notes[0].Count != tc.wantCount {
				t.Errorf("MalformedChunksEvent.Count = %d, want %d", notes[0].Count, tc.wantCount)
			}
			if res.Faulted != tc.wantFaulted {
				t.Errorf("StepResult.Faulted = %v, want %v", res.Faulted, tc.wantFaulted)
			}
			if ran != tc.wantRan {
				t.Errorf("tool ran %d times, want %d", ran, tc.wantRan)
			}
			if got := len(errorEvents(sink.events)); tc.wantFaulted != (got > 0) {
				t.Errorf("ErrorEvents = %d, want a fault only on the faulted stream", got)
			}
		})
	}
}
