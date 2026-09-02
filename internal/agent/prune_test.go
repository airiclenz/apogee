package agent

// The Turn-boundary trigger for stale-tool-result Pruning (prune.go). autoPrune drives the pure
// policy in internal/context over the committed conversation before a Turn's request is built,
// reports the reclaim as one PruneEvent, and is opted out by the file-only `prune-tool-results:
// false` key (Config.Context.PruneToolResults) rather than by Bypass — it is structural.

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// pruneConfig is baseConfig with a discovered window and Pruning armed. Generative Compaction is
// explicitly OFF: both reducers trip on the same over-budget history, and a fold would Replace the
// stubs with a summary before the request is built, so the fold is silenced to observe the prune.
func pruneConfig(sink domain.EventSink) domain.Config {
	cfg := baseConfig(sink)
	cfg.Context.MaxContextTokens = 8192 // History allocation ≈ 3.9k tokens; the 60% trigger ≈ 9.4k chars
	cfg.Context.CompactionEnabled = false
	cfg.Context.PruneToolResults = true
	return cfg
}

// seedToolTurns appends the protected-prefix user message followed by turns tool-calling Turns,
// each one assistant message carrying a single read call and the sized result answering it. It is
// the conversation a run of that many tool-using Turns leaves behind.
func seedToolTurns(a *Agent, turns, resultChars int) {
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "the OVERARCHING-GOAL to keep in the prefix"})
	for i := 0; i < turns; i++ {
		id := fmt.Sprintf("call_%d", i)
		a.conv.Append(domain.Message{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{{
				ID:        id,
				Tool:      "read_file",
				Arguments: json.RawMessage(fmt.Sprintf(`{"path":"file_%d.go"}`, i)),
			}},
		})
		a.conv.Append(domain.Message{
			Role:       domain.RoleTool,
			ToolCallID: id,
			Content:    strings.Repeat(fmt.Sprintf("line %d\n", i), resultChars/8),
		})
	}
}

// toolResultContents is every tool result in the conversation, in order — the before/after view
// the prune assertions compare.
func toolResultContents(a *Agent) []string {
	var out []string
	for i := 0; i < a.conv.Len(); i++ {
		if m := a.conv.At(i); m.Role == domain.RoleTool {
			out = append(out, m.Content)
		}
	}
	return out
}

// pruneEvents is every PruneEvent the sink saw.
func pruneEvents(events []domain.Event) []domain.PruneEvent {
	var out []domain.PruneEvent
	for _, e := range events {
		if pe, ok := e.(domain.PruneEvent); ok {
			out = append(out, pe)
		}
	}
	return out
}

// TestAutoPruneStubsOldTurnsAndKeepsTheRecentWindow is the trigger's success path: an over-budget
// history of six tool-calling Turns is pruned at the Turn boundary, and only the Turns outside the
// protected recent window lose their results. The pass emits exactly one PruneEvent whose Tokens is
// the Budget's estimate of the characters actually reclaimed, and the request built straight after
// carries the stubs rather than the dumps.
func TestAutoPruneStubsOldTurnsAndKeepsTheRecentWindow(t *testing.T) {
	sink := &recordingSink{}
	up := &recordingResponder{reply: "reply"}
	a, err := newAgent(pruneConfig(sink), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedToolTurns(a, apogeectx.PruneKeepTurns+2, 4000) // ~24k chars, far past the ~9.4k-char trigger
	before := toolResultContents(a)

	if err := a.Submit(domain.UserInput{Text: "the fresh question"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	after := toolResultContents(a)
	if len(after) != len(before) {
		t.Fatalf("tool results = %d after the prune, want %d — pruning must rewrite in place", len(after), len(before))
	}
	for i, content := range after {
		stubbed := strings.HasPrefix(content, "[pruned:")
		if want := i < 2; stubbed != want {
			t.Errorf("result %d stubbed = %v, want %v (only the Turns before the last %d are eligible)",
				i, stubbed, want, apogeectx.PruneKeepTurns)
		}
	}

	events := pruneEvents(sink.events)
	if len(events) != 1 {
		t.Fatalf("PruneEvents = %d, want exactly 1", len(events))
	}
	reclaimed := 0
	for i := 0; i < 2; i++ {
		reclaimed += len(before[i]) - len(after[i])
	}
	want := domain.PruneEvent{EventBase: events[0].EventBase, Results: 2, Tokens: a.budget().EstimateTokens(reclaimed)}
	if events[0] != want {
		t.Errorf("PruneEvent = %+v, want %+v", events[0], want)
	}

	sent := renderedRequest(up.last.Messages)
	if !strings.Contains(sent, "[pruned:") {
		t.Error("the request built after the prune carries no stub")
	}
	if strings.Contains(sent, before[0]) {
		t.Error("the request still carries a pruned result verbatim")
	}
}

// renderedRequest joins a provider request's message contents, so an assertion about what the model
// was shown reads over one string rather than a loop.
func renderedRequest(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// TestAutoPruneOptOutRespected pins the `prune-tool-results: false` opt-out: the same over-budget
// history is sent whole, with no stub and no event.
func TestAutoPruneOptOutRespected(t *testing.T) {
	sink := &recordingSink{}
	cfg := pruneConfig(sink)
	cfg.Context.PruneToolResults = false
	a, err := newAgent(cfg, &recordingResponder{reply: "reply"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedToolTurns(a, apogeectx.PruneKeepTurns+2, 4000)
	before := toolResultContents(a)

	if err := a.Submit(domain.UserInput{Text: "next"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	if got := toolResultContents(a); !slices.Equal(got, before) {
		t.Error("tool results were rewritten with the opt-out set")
	}
	if n := len(pruneEvents(sink.events)); n != 0 {
		t.Errorf("PruneEvents = %d with the opt-out set, want 0", n)
	}
}

// TestAutoPruneSkipsUnknownWindow pins the second gate: with no discovered window the Budget carries
// no History allocation, so there is no basis for a fraction and nothing is pruned however large the
// history grows.
func TestAutoPruneSkipsUnknownWindow(t *testing.T) {
	sink := &recordingSink{}
	cfg := pruneConfig(sink)
	cfg.Context.MaxContextTokens = 0
	a, err := newAgent(cfg, &recordingResponder{reply: "reply"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedToolTurns(a, apogeectx.PruneKeepTurns+2, 4000)
	before := toolResultContents(a)

	if err := a.Submit(domain.UserInput{Text: "next"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	if got := toolResultContents(a); !slices.Equal(got, before) {
		t.Error("tool results were rewritten with no window discovered")
	}
	if n := len(pruneEvents(sink.events)); n != 0 {
		t.Errorf("PruneEvents = %d with no window discovered, want 0", n)
	}
}

// TestAutoPruneGateIsLiveAndInherited pins the live seam and the spawn: SetPruneToolResults moves
// the gate mid-session like SetCompactionEnabled, and a child spawned afterwards is built with the
// value the parent holds at spawn, not the one it was constructed with.
func TestAutoPruneGateIsLiveAndInherited(t *testing.T) {
	cfg := pruneConfig(&recordingSink{})
	cfg.Context.PruneToolResults = false
	parent, err := newAgent(cfg, &recordingResponder{reply: "reply"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if parent.pruneEnabled() {
		t.Fatal("pruneEnabled() = true, want the construction seed false")
	}

	parent.SetPruneToolResults(true)

	if !parent.pruneEnabled() {
		t.Error("pruneEnabled() = false after SetPruneToolResults(true)")
	}
	child, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	if !child.pruneEnabled() {
		t.Error("the child did not inherit the parent's live Pruning gate")
	}
}

// TestAutoPruneLeavesTheCompactionTriggerIntact pins the two reducers' coexistence at the boundary
// they share: a text-only over-budget history offers pruning nothing to rewrite, so the generative
// fold still runs — once — exactly as it did before pruning was added.
func TestAutoPruneLeavesTheCompactionTriggerIntact(t *testing.T) {
	cfg := pruneConfig(&recordingSink{})
	cfg.Context.CompactionEnabled = true
	up := &compactSpyResponder{reply: "FOLDED"}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	seedLargeConv(a)

	for _, text := range []string{"first", "second"} {
		if err := a.Submit(domain.UserInput{Text: text}); err != nil {
			t.Fatalf("Submit(%q): %v", text, err)
		}
		if _, err := a.Step(context.Background()); err != nil {
			t.Fatalf("Step(%q): %v", text, err)
		}
	}

	if up.summaryCalls != 1 {
		t.Errorf("summarizer calls = %d across two Turns with pruning armed, want exactly 1", up.summaryCalls)
	}
}
