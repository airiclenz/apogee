package mechanisms

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// complexMultiStep scores "complex" in decomposeAssessComplexity (3 numbered steps → +4; the
// "sub-agent"/"delegate" delegation phrases → +8; total 12 ≥ 10) and opens with an action verb, so
// it is both collapsed in history and decomposed as a current prompt.
const complexMultiStep = "Build a full parser pipeline.\n" +
	"1. First, read the grammar spec.\n" +
	"2. Then create the tokenizer in `lexer.go`.\n" +
	"3. Finally, delegate to a sub-agent to write the tests."

// oneTool is a minimal non-empty tool menu — decompose skips entirely when no tools are present.
var oneTool = []domain.ToolDef{{Name: "write_file"}}

func TestDecomposeDescriptorAndOrdering(t *testing.T) {
	t.Parallel()
	m, err := newDecompose(Deps{})
	if err != nil {
		t.Fatalf("newDecompose: %v", err)
	}
	d := m.Descriptor()
	if d.ID != decomposeID {
		t.Errorf("ID = %q, want %q", d.ID, decomposeID)
	}
	if d.Capability != domain.CapProactiveNudge {
		t.Errorf("Capability = %q, want proactive-nudge", d.Capability)
	}
	if d.Suppression != domain.SuppressStrikesThree {
		t.Errorf("Suppression = %q, want strikes-3", d.Suppression)
	}
	// After toolfilter (catalogue Table A).
	if o := m.Ordering(); len(o.After) != 1 || o.After[0] != toolFilterID {
		t.Errorf("Ordering.After = %v, want [%q]", o.After, toolFilterID)
	}
	if _, ok := m.(domain.PreRequestHook); !ok {
		t.Error("decompose does not implement PreRequestHook")
	}
}

func TestDecomposeBuildsFromCatalogue(t *testing.T) {
	t.Parallel()
	m, err := Build(decomposeID, Deps{})
	if err != nil {
		t.Fatalf("Build(%q): %v", decomposeID, err)
	}
	if m.Descriptor().ID != decomposeID {
		t.Errorf("built ID = %q, want %q", m.Descriptor().ID, decomposeID)
	}
}

// No tools in the request → decompose is a no-op (apogee-sim Skip("no tools")).
func TestDecomposeSkipsWithoutTools(t *testing.T) {
	t.Parallel()
	req := shaperRequest([]domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
		{Role: domain.RoleUser, Content: complexMultiStep},
	}, nil)
	before := req.Revision()
	if err := (decomposeMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != before {
		t.Fatal("decompose mutated a request with no tools")
	}
}

// A complex, action-intent current prompt gets a single step hint injected into the system prompt,
// and a second pass is a no-op (the marker makes the inject idempotent).
func TestDecomposeInjectsStepHintOnce(t *testing.T) {
	t.Parallel()
	req := shaperRequest([]domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
		{Role: domain.RoleUser, Content: complexMultiStep},
	}, oneTool)

	before := req.Revision()
	if err := (decomposeMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() == before {
		t.Fatal("a complex action prompt should have injected a step hint")
	}
	sys := req.State().Messages[0]
	if sys.Role != domain.RoleSystem {
		t.Fatalf("first message role = %q, want system", sys.Role)
	}
	if !strings.Contains(sys.Content, decomposeStepHintMarker) {
		t.Errorf("step-hint marker %q not injected into the system prompt: %q", decomposeStepHintMarker, sys.Content)
	}
	// The step hint prepends the focus directive when none is present yet (apogee-sim injectStepHint).
	if !strings.Contains(sys.Content, decomposeFocusMarker) {
		t.Error("step hint should carry the focus directive when none was present")
	}
	// The user message is left intact — decompose hints the step into the system prompt, it does not
	// rewrite the user prompt (apogee-sim injectStepHint comment).
	if got := req.State().Messages[1].Content; got != complexMultiStep {
		t.Errorf("user message was rewritten: %q", got)
	}

	// Second pass: the marker is present, so nothing is re-injected.
	mid := req.Revision()
	if err := (decomposeMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("second PreRequest: %v", err)
	}
	if req.Revision() != mid {
		t.Fatal("step hint re-injected despite the marker already being present")
	}
}

// History collapse rewrites the older complex user message to a short summary while leaving the
// system prefix's original text and the latest user message intact, and never changes the message
// count (it edits content in place, it does not drop or insert).
func TestDecomposeCollapsesOlderComplexHistory(t *testing.T) {
	t.Parallel()
	req := shaperRequest([]domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
		{Role: domain.RoleUser, Content: complexMultiStep}, // older, complex → collapsed
		{Role: domain.RoleAssistant, Content: "On it."},
		{Role: domain.RoleUser, Content: "continue"}, // latest, simple → untouched
	}, oneTool)

	before := len(req.State().Messages)
	if err := (decomposeMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	msgs := req.State().Messages
	if len(msgs) != before {
		t.Fatalf("message count changed %d → %d; collapse must edit in place", before, len(msgs))
	}
	if msgs[1].Content == complexMultiStep {
		t.Error("older complex user message was not collapsed")
	}
	if !strings.Contains(msgs[1].Content, "Detailed steps omitted") {
		t.Errorf("collapsed message missing the omission note: %q", msgs[1].Content)
	}
	if msgs[3].Content != "continue" {
		t.Errorf("latest user message = %q, want it left intact", msgs[3].Content)
	}
	if !strings.Contains(msgs[0].Content, "SYS") {
		t.Error("system prefix's original text was lost")
	}
}

// The read-loop coupling gates active decomposition: when read_loop has already fired this Session,
// the step hint / focus directives are muted (apogee-sim S1 mute), but the harmless history collapse
// still runs. Contrast with the un-fired case, where the step hint injects.
func TestDecomposeMutedWhenReadLoopFired(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
		{Role: domain.RoleUser, Content: complexMultiStep}, // older, complex → collapsed
		{Role: domain.RoleAssistant, Content: "On it."},
		{Role: domain.RoleUser, Content: "now build the parser as described"}, // latest, action intent
	}

	// Un-fired: the step hint injects (proves the prompt would otherwise decompose).
	unfired := shaperRequest(history, oneTool)
	if err := (decomposeMechanism{}).PreRequest(context.Background(), unfired); err != nil {
		t.Fatalf("PreRequest (unfired): %v", err)
	}
	if !strings.Contains(unfired.State().Messages[0].Content, decomposeFocusMarker) {
		t.Fatal("baseline: expected the focus/step directives to inject when read_loop has not fired")
	}

	// Fired: build a request whose fire ledger records a read_loop fire this Session.
	fired := domain.NewRequest("m", history, oneTool, domain.Budget{}, 0, map[domain.MechanismID]int{readLoopID: 1})
	if err := (decomposeMechanism{}).PreRequest(context.Background(), fired); err != nil {
		t.Fatalf("PreRequest (fired): %v", err)
	}
	sys := fired.State().Messages[0].Content
	if strings.Contains(sys, decomposeFocusMarker) || strings.Contains(sys, decomposeStepHintMarker) {
		t.Errorf("active decomposition was not muted after a read_loop fire: %q", sys)
	}
	if sys != "SYS" {
		t.Errorf("system prompt = %q, want it untouched (all directives muted)", sys)
	}
	// The harmless collapse still runs even when muted.
	if fired.State().Messages[1].Content == complexMultiStep {
		t.Error("history collapse should still run when active decomposition is muted")
	}
	if fired.Revision() == 0 {
		t.Error("the collapse should still book a fire (Revision moved)")
	}
}

// Once the model has written a file, decompose stops steering — no continuation, no step hint
// (apogee-sim: return once HasWrittenFiles).
func TestDecomposeSkipsAfterWrite(t *testing.T) {
	t.Parallel()
	req := shaperRequest([]domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "write_file", Arguments: []byte(`{"path":"x.go","content":"package x"}`)}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: "ok"},
		{Role: domain.RoleUser, Content: complexMultiStep},
	}, oneTool)
	before := req.Revision()
	if err := (decomposeMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != before {
		t.Fatal("decompose steered a prompt after the model had already written a file")
	}
}
