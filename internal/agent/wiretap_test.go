package agent

import (
	"net/http/httptest"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/workflow"
)

// sweepOrSeenScript answers a background sweep child with its finish receipt and every other
// request — the conversation's own Turns — with seenScript's one word.
func sweepOrSeenScript() stubllm.Script {
	return stubllm.Script{Turns: []stubllm.Turn{
		{
			When:      &stubllm.Match{LastMessage: `sweep `},
			Repeat:    true,
			ToolCalls: []stubllm.ToolCall{{Name: tools.FinishToolName, Arguments: `{"status":"ok","summary":"swept","count":1}`}},
		},
		{Text: "seen", Repeat: true},
	}}
}

// TestWireTap_BackgroundChildrenReadNoAgentState is the Inspector's wire tap under the one shape
// that reaches it from another goroutine: a background workflow's unrouted children stream over
// the session's client, so its observer runs on their goroutines while the conversation Steps
// (advancing the Turn counter) and rebinds (replacing the Agent's Config whole). Under -race any
// read of that live state from the tap is a reported race. It needs a real provider client over a
// hermetic HTTP Upstream, because the observer lives inside that client: a fake Responder never
// calls it. Every child record carries the top-level identity and a Turn the conversation has
// reached, the stamp WireEvent documents.
func TestWireTap_BackgroundChildrenReadNoAgentState(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(stubllm.InProcess(t, sweepOrSeenScript()).Handler())
	t.Cleanup(srv.Close)
	items := []string{"w1", "w2", "w3", "w4", "w5", "w6"}
	sink := newLockedSink()
	cfg := recipeConfig(t, sink, sweepRecipe("sweep", items...))
	cfg.Endpoint = srv.URL
	cfg.Inspector = true
	cfg.ParallelAgents = 3

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	id := launchBackground(t, a, "sweep")

	const rounds = 4
	for round := range rounds {
		stepOnce(t, a, "hi")
		// A switch unbinds the model, so the Rebind after it is what lets the next round Submit.
		if err := a.SwitchUpstream(UpstreamSpec{Endpoint: srv.URL}); err != nil {
			t.Fatalf("round %d: SwitchUpstream while the workflow runs: %v", round, err)
		}
		if err := a.Rebind(RebindSpec{Model: "moved-model"}); err != nil {
			t.Fatalf("round %d: Rebind while the workflow runs: %v", round, err)
		}
	}
	a.background.waitAll()

	phases := itemPhases(workflowInfo(t, a, id))
	for _, item := range items {
		if phases[item] != workflow.PhaseDone {
			t.Fatalf("item phases = %v, want every item done", phases)
		}
	}
	sink.mu.Lock()
	records := wireEvents(sink.events)
	sink.mu.Unlock()
	// One request and one response per conversation Turn and per child call, at the least.
	if want := 2 * (rounds + len(items)); len(records) < want {
		t.Fatalf("got %d WireEvents; want at least %d", len(records), want)
	}
	for _, rec := range records {
		if rec.Depth != 0 || rec.CallID != "" {
			t.Errorf("%s record stamped depth %d call %q; want the session's top-level identity", rec.Direction, rec.Depth, rec.CallID)
		}
		if rec.Turn < 0 || rec.Turn > rounds {
			t.Errorf("%s record stamped Turn %d; want one the conversation reached (0..%d)", rec.Direction, rec.Turn, rounds)
		}
	}
}
