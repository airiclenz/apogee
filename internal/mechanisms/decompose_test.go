package mechanisms

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
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
	m, err := Build(decomposeID, Deps{})
	if err != nil {
		t.Fatalf("Build(%q): %v", decomposeID, err)
	}
	d := m.Descriptor
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
	if o := m.Ordering; len(o.After) != 1 || o.After[0] != toolFilterID {
		t.Errorf("Ordering.After = %v, want [%q]", o.After, toolFilterID)
	}
	if _, ok := m.Hook.(domain.PreRequestHook); !ok {
		t.Error("decompose does not implement PreRequestHook")
	}
}

func TestDecomposeBuildsFromCatalogue(t *testing.T) {
	t.Parallel()
	m, err := Build(decomposeID, Deps{})
	if err != nil {
		t.Fatalf("Build(%q): %v", decomposeID, err)
	}
	if m.Descriptor.ID != decomposeID {
		t.Errorf("built ID = %q, want %q", m.Descriptor.ID, decomposeID)
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

// An Interjection — the human's remark committed INTO the running Exchange (ADR 0025) — is NOT an
// Exchange opening, so the history collapse must anchor on the derived opening rather than on "the
// last user message": the LIVE ask stays whole even though a later user message follows it, and only
// what precedes the opening collapses. The two complex prompts carry IDENTICAL text, so their
// opposite fates are decided purely by the boundary, and the interjection itself is left alone as
// part of the running Exchange's body.
func TestDecomposeCollapseSparesTheLiveAskAcrossAnInterjection(t *testing.T) {
	t.Parallel()
	req := shaperRequest([]domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
		{Role: domain.RoleUser, Content: complexMultiStep}, // 1: the previous Exchange's ask → collapsed
		{Role: domain.RoleAssistant, Content: "Done."},
		{Role: domain.RoleUser, Content: complexMultiStep}, // 3: the LIVE opening ask → kept whole
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file", Arguments: []byte(`{"path":"lexer.go"}`)}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: "package lexer"},
		{Role: domain.RoleUser, Content: "also check the tests", Interjected: true}, // 6: mid-Exchange remark
	}, oneTool)

	before := len(req.State().Messages)
	if err := (decomposeMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	msgs := req.State().Messages
	if len(msgs) != before {
		t.Fatalf("message count changed %d → %d; collapse must edit in place", before, len(msgs))
	}
	if msgs[3].Content != complexMultiStep {
		t.Errorf("the live opening ask was collapsed mid-Exchange: %q", msgs[3].Content)
	}
	if msgs[1].Content == complexMultiStep {
		t.Error("the previous Exchange's complex ask was not collapsed")
	}
	if !strings.Contains(msgs[1].Content, "Detailed steps omitted") {
		t.Errorf("collapsed message missing the omission note: %q", msgs[1].Content)
	}
	if msgs[6].Content != "also check the tests" {
		t.Errorf("the interjection was rewritten: %q", msgs[6].Content)
	}
}

// Once the model has written a file, decompose stops steering — no continuation, no step hint
// (apogee-sim: return once HasWrittenFiles). Every spelling in wave4WriteTools counts as that write,
// the file-operation trio (copy_file / move_file / delete_file) included: moving or deleting bytes
// that exist is the model acting on the workspace just as much as writing new ones is, so the same
// stand-down applies.
func TestDecomposeSkipsAfterWrite(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{
		"write_file", "edit_existing_file", "copy_file", "move_file", "delete_file",
	} {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			req := shaperRequest([]domain.Message{
				{Role: domain.RoleSystem, Content: "SYS"},
				{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: tool, Arguments: []byte(`{"path":"x.go","content":"package x"}`)}}},
				{Role: domain.RoleTool, ToolCallID: "c1", Content: "ok"},
				{Role: domain.RoleUser, Content: complexMultiStep},
			}, oneTool)
			before := req.Revision()
			if err := (decomposeMechanism{}).PreRequest(context.Background(), req); err != nil {
				t.Fatalf("PreRequest: %v", err)
			}
			if req.Revision() != before {
				t.Errorf("decompose steered a prompt after the model had already called %s", tool)
			}
		})
	}
}

// Control for the table above: a tool call that mutates nothing leaves decompose steering, so the
// stand-down each write spelling produces is the write's doing and not the mere presence of a call.
func TestDecomposeStillSteersAfterARead(t *testing.T) {
	t.Parallel()
	req := shaperRequest([]domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file", Arguments: []byte(`{"path":"x.go"}`)}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: "package x"},
		{Role: domain.RoleUser, Content: complexMultiStep},
	}, oneTool)
	before := req.Revision()
	if err := (decomposeMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() == before {
		t.Fatal("decompose stood down after a read_file; only a write ends its steering")
	}
}

// workspaceWritingBuiltins names the registered built-ins whose EXECUTION mutates a named workspace
// file — internal/tools' workspaceScopedWriter set, mirrored here because that marker is unexported.
// Every one of them must appear in wave4WriteTools, or the whole history family (read_repeat,
// read_loop, cached_content_intercept, error_enrichment, the off-ramps, greenfield detection) treats
// a real write as a non-write, which is exactly how copy_file, move_file and delete_file went
// unnoticed from 2026-08-10 until this pin.
var workspaceWritingBuiltins = map[string]bool{
	"write_file":              true,
	"edit_existing_file":      true,
	"single_find_and_replace": true,
	"multi_find_and_replace":  true,
	"copy_file":               true,
	"move_file":               true,
	"delete_file":             true,
}

// writeCapableNonFileBuiltins names the registered built-ins that are write-CAPABLE (they gate
// through Approval) yet mutate no workspace file a NAME can classify: the subprocess tools, whose
// effect is whatever command the model composed; the two git tools that write .git rather than the
// worktree; the network tools; and the sub_agent recursion point. They belong OUT of
// wave4WriteTools — counting them would make "the model has written a file" true for a web_search.
var writeCapableNonFileBuiltins = map[string]bool{
	"terminal":     true,
	"python_exec":  true,
	"run_tests":    true,
	"git_branch":   true,
	"git_commit":   true,
	"web_fetch":    true,
	"http_request": true,
	"web_search":   true,
	"sub_agent":    true,
	// The Console family's write-capable half (ADR 0059): what a Console writes is whatever the
	// model typed into a live shell, so no NAME in the call classifies it — the same reason
	// terminal sits here. Its read-only half (console_read, console_close) never reaches this
	// table, which only classifies write-capable tools.
	"console_open": true,
	"console_send": true,
}

// stubAsker and stubPresenter are non-nil host delegates: the registry omits ask_user and
// present_document when either is nil, and the pin below wants the whole roster, not one Driver's.
type stubAsker struct{}

func (stubAsker) Ask(context.Context, domain.AskRequest) (domain.AskAnswer, error) {
	return domain.AskAnswer{}, nil
}

type stubPresenter struct{}

func (stubPresenter) Present(context.Context, domain.PresentRequest) (domain.PresentOutcome, error) {
	return domain.PresentOutcome{}, nil
}

// The drift pin defect (a) closes: wave4WriteTools is the history family's single source for "did
// this call mutate a file", but nothing tied it to the tool menu it describes, so the file-operation
// trio was registered in internal/tools without ever reaching it. Walk the REGISTERED built-ins, and
// require every write-capable one to be classified — a workspace file writer that wave4WriteTools
// must contain, or a documented non-writer it must not. A new write tool in internal/tools now fails
// this test until someone answers the question for it.
//
// The sim spellings in wave4WriteTools (write_to_file, editFile, …) name no registered tool; the pin
// walks the menu, never the map, so those stay additive.
func TestWave4WriteToolsCoversEveryWorkspaceWritingBuiltin(t *testing.T) {
	t.Parallel()
	// Every built-in, default-off ones included: a tool registered default-off (the Console family,
	// ADR 0059) is still a tool a configured roster can lift, so the classification question has to
	// be answered for it here rather than the day someone turns it on. Lifting the whole build rung
	// by name is what makes the menu the pin walks the same set KnownToolNames spells.
	menu := tools.DefaultToolsWithHost("", tools.HostTools{
		Asker:     stubAsker{},
		Presenter: stubPresenter{},
		Enabled:   tools.KnownToolNames(),
	})
	if len(menu) != len(tools.KnownToolNames()) {
		t.Fatalf("menu has %d tools, KnownToolNames %d: the pin is not walking the whole roster", len(menu), len(tools.KnownToolNames()))
	}

	for _, tool := range menu {
		name := tool.Name()
		if domain.IsReadOnly(tool) {
			if workspaceWritingBuiltins[name] {
				t.Errorf("%q is declared ReadOnly but classified as a workspace file writer", name)
			}
			continue
		}
		switch {
		case workspaceWritingBuiltins[name]:
			if !wave4WriteTools[name] {
				t.Errorf("%q mutates workspace files but is missing from wave4WriteTools; the history family would read its writes as non-writes", name)
			}
		case writeCapableNonFileBuiltins[name]:
			if wave4WriteTools[name] {
				t.Errorf("%q mutates no named workspace file but sits in wave4WriteTools", name)
			}
		default:
			t.Errorf("built-in %q is write-capable but classified by neither table; decide whether it mutates workspace files and add it to workspaceWritingBuiltins (and wave4WriteTools) or to writeCapableNonFileBuiltins", name)
		}
	}
}

// A prompt with no numbered steps falls back to the first ACTION sentence: the opening sentence
// becomes the context and the action sentence becomes the single step, reported as step 0 of 1.
// The fallback extracts nothing in two cases — no sentence carries action intent (a description, or
// an action verb inside a question), and the action sentence IS the opening sentence, which would
// leave the step with no context to frame it and the prompt no simpler than it already is.
func TestDecomposeExtractStepProseFallback(t *testing.T) {
	t.Parallel()
	const prose = "The parser lives in `lexer.go`. Please update the tokenizer to handle escaped strings."
	const wantProse = "The parser lives in `lexer.go`.\n\n" +
		"Your next step: Please update the tokenizer to handle escaped strings."

	cases := []struct {
		name      string
		msg       string
		completed int
		want      string // "" means: the fallback bails out and extracts nothing
	}{
		{
			name: "opening sentence frames the first action sentence",
			msg:  prose,
			want: wantProse,
		},
		{
			// The fallback is not step-indexed: it has one step to offer and offers it again.
			name:      "completed steps do not advance the single fallback step",
			msg:       prose,
			completed: 3,
			want:      wantProse,
		},
		{
			name: "a description carries no action intent",
			msg:  "This module is quite old. Nobody remembers who owns it.",
		},
		{
			name: "an action verb inside a question is not an action sentence",
			msg:  "The loader is slow.\nWhat should I change in the tokenizer?",
		},
		{
			name: "a lone action sentence has no context to frame it",
			msg:  "Update the tokenizer to handle escaped strings.",
		},
		{
			name: "an opening action sentence has no context to frame it either",
			msg:  "Update the tokenizer. Then run the tests.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, stepIdx, total, ok := decomposeExtractStep(tc.msg, tc.completed)

			if ok != (tc.want != "") {
				t.Fatalf("extracted = %v, want %v (simplified %q)", ok, tc.want != "", got)
			}
			if got != tc.want {
				t.Errorf("simplified =\n%q\nwant\n%q", got, tc.want)
			}
			wantTotal := 0
			if ok {
				wantTotal = 1
			}
			if stepIdx != 0 || total != wantTotal {
				t.Errorf("step = %d of %d, want 0 of %d", stepIdx, total, wantTotal)
			}
		})
	}
}
