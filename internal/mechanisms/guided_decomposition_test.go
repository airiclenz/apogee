package mechanisms

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// guidedBudget is a known context window with a calibrated chars→token ratio, so the gate's signal
// thresholds are exercisable: FileContext 1000 tokens (signal A) and History 2000 tokens (signal B),
// at 4 chars/token.
var guidedBudget = domain.Budget{ContextLimit: 8000, CharsPerToken: 4, FileContext: 1000, History: 2000}

// guidedMenu is a minimal tool menu carrying the sub_agent recursion point — the delegation target
// the gate requires before it steers.
var guidedMenu = []domain.ToolDef{{Name: tools.SubAgentToolName}}

// guidedNoSubAgentMenu is a non-empty menu WITHOUT sub_agent — nothing to delegate toward.
var guidedNoSubAgentMenu = []domain.ToolDef{{Name: "write_file"}}

// oversizedUser is a fresh user message whose token estimate (len/4) exceeds guidedBudget.FileContext
// (1000 tokens): 6000 chars → 1500 tokens. It trips signal A.
var oversizedUser = strings.Repeat("word2 ", 1000) // 6000 chars

// guidedRequest builds a pre-request working value with an explicit Budget and nesting depth — the
// gate reads all four (Budget, tools, conversation, depth) so the plain shaperRequest (zero Budget)
// cannot exercise it.
func guidedRequest(msgs []domain.Message, menu []domain.ToolDef, budget domain.Budget, depth int) *domain.Request {
	req := domain.NewRequest("m", msgs, menu, budget, 0, nil)
	req.SetDepth(depth)
	return req
}

func TestGuidedDecompositionDescriptorAndOrdering(t *testing.T) {
	t.Parallel()
	m, err := newGuidedDecomposition(Deps{})
	if err != nil {
		t.Fatalf("newGuidedDecomposition: %v", err)
	}
	d := m.Descriptor()
	if d.ID != guidedDecompositionID {
		t.Errorf("ID = %q, want %q", d.ID, guidedDecompositionID)
	}
	if d.Capability != domain.CapProactiveNudge {
		t.Errorf("Capability = %q, want proactive-nudge", d.Capability)
	}
	if d.Suppression != domain.SuppressStrikesThree {
		t.Errorf("Suppression = %q, want strikes-3", d.Suppression)
	}
	// IncompatibleWith decompose (locked decision 2) and Requires tool_result_cap (locked decision 3).
	if len(d.IncompatibleWith) != 1 || d.IncompatibleWith[0] != decomposeID {
		t.Errorf("IncompatibleWith = %v, want [%q]", d.IncompatibleWith, decomposeID)
	}
	if len(d.Requires) != 1 || d.Requires[0] != toolResultCapID {
		t.Errorf("Requires = %v, want [%q]", d.Requires, toolResultCapID)
	}
	// After toolfilter — the sub_agent-presence gate must read the final (post-toolfilter) menu.
	if o := m.Ordering(); len(o.After) != 1 || o.After[0] != toolFilterID {
		t.Errorf("Ordering.After = %v, want [%q]", o.After, toolFilterID)
	}
	if _, ok := m.(domain.PreRequestHook); !ok {
		t.Error("guided_decomposition does not implement PreRequestHook")
	}
	// Both halves live on the one struct: the pre-request gate/steer and the post-response
	// intercept + serialized follow-through. Suppressing the Mechanism disarms both as a unit.
	if _, ok := m.(domain.PostResponseHook); !ok {
		t.Error("guided_decomposition does not implement PostResponseHook (the intercept half)")
	}
}

func TestGuidedDecompositionBuildsFromCatalogue(t *testing.T) {
	t.Parallel()
	m, err := Build(guidedDecompositionID, Deps{})
	if err != nil {
		t.Fatalf("Build(%q): %v", guidedDecompositionID, err)
	}
	if m.Descriptor().ID != guidedDecompositionID {
		t.Errorf("built ID = %q, want %q", m.Descriptor().ID, guidedDecompositionID)
	}
}

// The gate fires only when every precondition holds and a measured signal trips; each disqualifying
// condition leaves the request untouched (no fire — Revision unchanged, R4).
func TestGuidedDecompositionGate(t *testing.T) {
	t.Parallel()

	// A mid-Exchange history that trips signal B: total content > 8000 chars (>2000 tokens) and the
	// last assistant message carried tool calls; it ends in a tool result, so signal A (last == fresh
	// user) does not also fire.
	midExchange := []domain.Message{
		{Role: domain.RoleUser, Content: "go"},
		{Role: domain.RoleAssistant, Content: strings.Repeat("z", 9000),
			ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file", Arguments: []byte(`{}`)}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: "ok"},
	}

	tests := []struct {
		name  string
		msgs  []domain.Message
		menu  []domain.ToolDef
		bud   domain.Budget
		depth int
		fire  bool
	}{
		{
			name: "signal A: oversized fresh user message fires",
			msgs: []domain.Message{{Role: domain.RoleSystem, Content: "SYS"}, {Role: domain.RoleUser, Content: oversizedUser}},
			menu: guidedMenu, bud: guidedBudget, fire: true,
		},
		{
			name: "signal B: oversized mid-Exchange history fires",
			msgs: midExchange, menu: guidedMenu, bud: guidedBudget, fire: true,
		},
		{
			name: "under both thresholds: small task does not fire",
			msgs: []domain.Message{{Role: domain.RoleSystem, Content: "SYS"}, {Role: domain.RoleUser, Content: "add a helper"}},
			menu: guidedMenu, bud: guidedBudget, fire: false,
		},
		{
			name: "unknown window (ContextLimit 0): never fires",
			msgs: []domain.Message{{Role: domain.RoleUser, Content: oversizedUser}},
			menu: guidedMenu, bud: domain.Budget{CharsPerToken: 4, FileContext: 1000}, fire: false,
		},
		{
			name: "uncalibrated ratio (CharsPerToken 0): never fires",
			msgs: []domain.Message{{Role: domain.RoleUser, Content: oversizedUser}},
			menu: guidedMenu, bud: domain.Budget{ContextLimit: 8000, FileContext: 1000}, fire: false,
		},
		{
			name: "nested call (Depth > 0): never fires",
			msgs: []domain.Message{{Role: domain.RoleUser, Content: oversizedUser}},
			menu: guidedMenu, bud: guidedBudget, depth: 1, fire: false,
		},
		{
			name: "no sub_agent on the menu: nothing to delegate toward",
			msgs: []domain.Message{{Role: domain.RoleUser, Content: oversizedUser}},
			menu: guidedNoSubAgentMenu, bud: guidedBudget, fire: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := guidedRequest(tc.msgs, tc.menu, tc.bud, tc.depth)
			before := req.Revision()
			if err := (guidedDecompositionMechanism{}).PreRequest(context.Background(), req); err != nil {
				t.Fatalf("PreRequest: %v", err)
			}
			fired := req.Revision() != before
			if fired != tc.fire {
				t.Fatalf("fired = %v, want %v (revision %d → %d)", fired, tc.fire, before, req.Revision())
			}
			if tc.fire {
				// The steer is injected verbatim and the user message is never trimmed (honesty, §2).
				if !guidedRequestHasSteer(req) {
					t.Error("gate fired but the enumeration steer was not injected")
				}
			}
		})
	}
}

// An outstanding steer or fan-out directive in the conversation stops the gate from steering again —
// no double-steer (locked decision 1). Both markers are exercised, over an otherwise-firing signal-A
// request.
func TestGuidedDecompositionNoDoubleSteer(t *testing.T) {
	t.Parallel()
	for _, marker := range []string{guidedDecompositionSteerMarker, guidedDecompositionDirectiveMarker} {
		t.Run(marker, func(t *testing.T) {
			t.Parallel()
			msgs := []domain.Message{
				{Role: domain.RoleUser, Content: "earlier: " + marker + " ..."},
				{Role: domain.RoleUser, Content: oversizedUser}, // would trip signal A on its own
			}
			req := guidedRequest(msgs, guidedMenu, guidedBudget, 0)
			before := req.Revision()
			if err := (guidedDecompositionMechanism{}).PreRequest(context.Background(), req); err != nil {
				t.Fatalf("PreRequest: %v", err)
			}
			if req.Revision() != before {
				t.Fatalf("re-steered despite an outstanding marker %q in history", marker)
			}
		})
	}
}

// guidedRequestHasSteer reports whether the request now carries the enumeration steer marker in one
// of its messages (the InjectContext landing point).
func guidedRequestHasSteer(req *domain.Request) bool {
	found := false
	req.View().Conversation().Range(func(_ int, m domain.Message) bool {
		if strings.Contains(m.Content, guidedDecompositionSteerMarker) {
			found = true
			return false
		}
		return true
	})
	return found
}

// ----------------------------------------------------------------------------
// PostResponse — the intercept + serialized follow-through half (ADR 0014 §2/§3)
// ----------------------------------------------------------------------------

// guidedResponse builds a post-response working value over history — a real domain.Request view so
// Conversation()/Turn() behave as in the loop (the intercept reads the markers and the enumeration
// off it). finish follows whether the model itself emitted tool calls.
func guidedResponse(history []domain.Message, text string, calls ...domain.ToolCall) *domain.Response {
	view := domain.NewRequest("m", history, guidedMenu, guidedBudget, 0, nil).View()
	finish := domain.FinishStop
	if len(calls) > 0 {
		finish = domain.FinishToolCalls
	}
	return domain.NewResponse(text, "", calls, finish, view)
}

// guidedSubAgentCall is a sub_agent tool call carrying task — the delegation the model emits itself
// on a follow-through Turn (and the shape the intercept synthesizes).
func guidedSubAgentCall(id, task string) domain.ToolCall {
	args, _ := json.Marshal(tools.SubAgentArgs{Task: task})
	return domain.ToolCall{ID: id, Tool: tools.SubAgentToolName, Arguments: args}
}

// guidedConv wraps a message slice as the read-only ConversationView the cursor helpers scan — the
// view-driven surface for the anchor/remainder derivation tests.
func guidedConv(history []domain.Message) domain.ConversationView {
	return domain.NewRequest("m", history, guidedMenu, guidedBudget, 0, nil).View().Conversation()
}

// fireGuidedPostResponse fires the intercept once against resp.
func fireGuidedPostResponse(t *testing.T, resp *domain.Response) domain.PostResponseDecision {
	t.Helper()
	decision, err := (guidedDecompositionMechanism{}).PostResponse(context.Background(), resp)
	if err != nil {
		t.Fatalf("PostResponse: %v", err)
	}
	return decision
}

// The list parser is deliberately lenient (ADR 0014 §2): numbered, bulleted, and plain-line variants
// all yield the ordered subtask texts with markers stripped; blank lines and code fences are dropped.
func TestGuidedDecompositionParseList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"numbered dot", "1. alpha\n2. beta\n3. gamma", []string{"alpha", "beta", "gamma"}},
		{"numbered paren", "1) alpha\n2) beta", []string{"alpha", "beta"}},
		{"numbered space-dash", "1 - alpha\n2 - beta", []string{"alpha", "beta"}},
		{"bulleted mix", "- alpha\n* beta\n• gamma", []string{"alpha", "beta", "gamma"}},
		{"plain lines", "alpha\nbeta\ngamma", []string{"alpha", "beta", "gamma"}},
		{"fenced noise", "```\n1. alpha\n2. beta\n```", []string{"alpha", "beta"}},
		{"blank lines dropped", "1. alpha\n\n2. beta\n", []string{"alpha", "beta"}},
		{"single item", "1. only", []string{"only"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := guidedDecompositionParseList(tc.text); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseList(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// An out-of-bounds enumeration (fewer than 2 or more than 12 items) is declined WHOLE — a benign
// no-op that synthesizes nothing and never truncates the list (locked decision 5 / ADR 0014 §5).
func TestGuidedDecompositionInterceptDeclinesOutOfBounds(t *testing.T) {
	t.Parallel()
	steer := domain.Message{Role: domain.RoleUser, Content: guidedDecompositionSteer}
	for _, tc := range []struct {
		name  string
		items int
	}{
		{"one item declines", 1},
		{"thirteen items decline whole", 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lines := make([]string, 0, tc.items)
			for i := 1; i <= tc.items; i++ {
				lines = append(lines, fmt.Sprintf("%d. subtask %d", i, i))
			}
			resp := guidedResponse([]domain.Message{steer}, strings.Join(lines, "\n"))
			before := resp.Revision()
			decision := fireGuidedPostResponse(t, resp)
			if decision.Action != "" {
				t.Fatalf("Action = %q, want empty (declined whole)", decision.Action)
			}
			if resp.Revision() != before {
				t.Fatalf("revision changed (%d → %d); a declined list must not synthesize a call", before, resp.Revision())
			}
			if len(resp.ToolCalls()) != 0 {
				t.Fatalf("declined list appended %d calls, want 0", len(resp.ToolCalls()))
			}
		})
	}
}

// F4 — a reply is an enumeration only when it is in-bounds AND a strict majority of its lines carried
// an explicit ordered/bullet marker. A compliant numbered list (including the accept-window edges of
// exactly 2 and exactly 12) is intercepted; an empty reply, multi-line prose, and an exactly-half
// marked reply are declined WHOLE — no synthesized call, zero decision (ADR 0014 §5 fail-soft).
func TestGuidedDecompositionInterceptMajorityMarked(t *testing.T) {
	t.Parallel()

	numbered := func(n int) string {
		lines := make([]string, 0, n)
		for i := 1; i <= n; i++ {
			lines = append(lines, fmt.Sprintf("%d. subtask %d", i, i))
		}
		return strings.Join(lines, "\n")
	}

	tests := []struct {
		name      string
		text      string
		intercept bool
	}{
		{"exactly two fully numbered intercepts", numbered(2), true},
		{"exactly twelve fully numbered intercepts", numbered(12), true},
		{"majority marked, minority plain intercepts", "1. alpha\n2. beta\nplain gamma", true},
		{"empty reply declines", "", false},
		{"whitespace-only reply declines", "   \n\t\n  ", false},
		{"three-line unmarked prose declines", "Could you clarify which module you mean?\nI want to scope this correctly.\nLet me know before I start.", false},
		{"exactly half marked declines (strict majority)", "1. do a\n2. do b\nplain c\nplain d", false},
	}
	steer := domain.Message{Role: domain.RoleUser, Content: guidedDecompositionSteer}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp := guidedResponse([]domain.Message{steer}, tc.text)
			before := resp.Revision()
			decision := fireGuidedPostResponse(t, resp)
			if tc.intercept {
				if decision.Action != domain.ActionDefer {
					t.Fatalf("Action = %q, want defer (a compliant enumeration is intercepted)", decision.Action)
				}
				if len(resp.ToolCalls()) != 1 {
					t.Fatalf("intercept appended %d calls, want exactly 1", len(resp.ToolCalls()))
				}
				if resp.Revision() == before {
					t.Error("intercept did not bump the revision (the acted-fire probe, R4)")
				}
				return
			}
			if decision.Action != "" {
				t.Fatalf("Action = %q, want empty (declined whole — prose is not an enumeration)", decision.Action)
			}
			if resp.Revision() != before {
				t.Fatalf("revision changed (%d → %d); a declined reply must not synthesize a call", before, resp.Revision())
			}
			if len(resp.ToolCalls()) != 0 {
				t.Fatalf("declined reply appended %d calls, want 0", len(resp.ToolCalls()))
			}
		})
	}
}

// A mixed list where the marked lines are a strict majority is intercepted with EVERY line kept as a
// subtask (unmarked lines included, small-model tolerance): the first becomes the synthesized
// delegation, the rest ride the deferred directive.
func TestGuidedDecompositionInterceptKeepsUnmarkedMinorityItems(t *testing.T) {
	t.Parallel()
	steer := domain.Message{Role: domain.RoleUser, Content: guidedDecompositionSteer}
	// Two marked lines, one plain — 2 of 3 marked is a strict majority, so the list is accepted.
	resp := guidedResponse([]domain.Message{steer}, "1. Refactor the parser\n2. Add unit tests\nupdate the changelog")
	decision := fireGuidedPostResponse(t, resp)

	calls := resp.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("appended %d calls, want exactly 1", len(calls))
	}
	var args tools.SubAgentArgs
	if err := json.Unmarshal(calls[0].Arguments, &args); err != nil {
		t.Fatalf("synthesized args do not unmarshal: %v", err)
	}
	if !strings.HasPrefix(args.Task, "Refactor the parser") {
		t.Errorf("first delegated task = %q, want it to start with the first subtask", args.Task)
	}
	if decision.Action != domain.ActionDefer {
		t.Fatalf("Action = %q, want defer", decision.Action)
	}
	// The remaining two — including the unmarked plain line — ride the directive; every line kept.
	for _, want := range []string{"Add unit tests", "update the changelog"} {
		if !strings.Contains(decision.Inject, want) {
			t.Errorf("deferred directive missing kept subtask %q", want)
		}
	}
}

// On the enumeration Turn (steer outstanding, no tool calls, a bounded list) the intercept appends
// exactly ONE valid sub_agent call for the first subtask, leaves the enumeration text verbatim, and
// defers the remaining subtasks under the directive marker.
func TestGuidedDecompositionEnumerationIntercept(t *testing.T) {
	t.Parallel()
	steer := domain.Message{Role: domain.RoleUser, Content: guidedDecompositionSteer}
	list := "1. Refactor the parser\n2. Add unit tests\n3. Update the changelog"
	resp := guidedResponse([]domain.Message{{Role: domain.RoleUser, Content: "big task"}, steer}, list)
	before := resp.Revision()
	decision := fireGuidedPostResponse(t, resp)

	calls := resp.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("appended %d calls, want exactly 1", len(calls))
	}
	if calls[0].Tool != tools.SubAgentToolName {
		t.Fatalf("synthesized call tool = %q, want %q", calls[0].Tool, tools.SubAgentToolName)
	}
	var args tools.SubAgentArgs
	if err := json.Unmarshal(calls[0].Arguments, &args); err != nil {
		t.Fatalf("synthesized args do not unmarshal to SubAgentArgs: %v", err)
	}
	if !strings.HasPrefix(args.Task, "Refactor the parser") {
		t.Errorf("task = %q, want it to start with the first subtask", args.Task)
	}
	if !strings.Contains(args.Task, guidedDecompositionReportHygiene) {
		t.Errorf("task = %q, want the compact-report hygiene ask appended (ADR 0014 §4)", args.Task)
	}
	if resp.Revision() == before {
		t.Error("AppendToolCall did not bump the revision (the acted-fire probe, R4)")
	}
	if resp.Text() != list {
		t.Errorf("response text mutated to %q; the enumeration must stay verbatim (locked decision 4)", resp.Text())
	}
	if decision.Action != domain.ActionDefer {
		t.Fatalf("Action = %q, want defer", decision.Action)
	}
	if !strings.Contains(decision.Inject, guidedDecompositionDirectiveMarker) {
		t.Error("deferred directive is missing its marker (the no-double-steer contract with the gate)")
	}
	for _, want := range []string{"Add unit tests", "Update the changelog"} {
		if !strings.Contains(decision.Inject, want) {
			t.Errorf("deferred directive missing remaining subtask %q", want)
		}
	}
	if strings.Contains(decision.Inject, "Refactor the parser") {
		t.Error("deferred directive still lists the already-dispatched first subtask")
	}
}

// On a follow-through Turn (directive steering, the model delegated the next subtask itself) the
// intercept re-derives the remainder from honest history MINUS this Turn's call and re-defers the
// shrunken directive — no response mutation, just carried work.
func TestGuidedDecompositionFollowThroughShrinksRemainder(t *testing.T) {
	t.Parallel()
	resp := guidedResponse(
		guidedFanOutHistory(),
		"",
		guidedSubAgentCall("c2", "Add unit tests "+guidedDecompositionReportHygiene),
	)
	before := resp.Revision()
	decision := fireGuidedPostResponse(t, resp)
	if decision.Action != domain.ActionDefer {
		t.Fatalf("Action = %q, want defer", decision.Action)
	}
	if resp.Revision() != before {
		t.Errorf("follow-through mutated the response (revision %d → %d); it only re-defers", before, resp.Revision())
	}
	if !strings.Contains(decision.Inject, "Update the changelog") {
		t.Error("shrunken directive dropped the still-outstanding subtask")
	}
	if strings.Contains(decision.Inject, "Add unit tests") {
		t.Error("shrunken directive still lists the just-delegated subtask")
	}
	if strings.Contains(decision.Inject, "Refactor the parser") {
		t.Error("shrunken directive still lists the first (already-dispatched) subtask")
	}
}

// An off-script tool Turn mid-fan-out (a directive is steering and the model called a tool OTHER than
// sub_agent) re-defers the remainder intact rather than letting the drained directive drop the queue
// (F2 / item 4). The branch fires only when all four conditions hold: drop the directive marker, the
// tool calls, or the remainder and the intercept books nothing.
func TestGuidedDecompositionOffScriptToolTurnReDefers(t *testing.T) {
	t.Parallel()
	hygiene := " " + guidedDecompositionReportHygiene
	offScriptCall := domain.ToolCall{ID: "r1", Tool: "read_file", Arguments: []byte(`{"path":"parser.go"}`)}

	t.Run("re-defers the remainder intact on an off-script tool call", func(t *testing.T) {
		t.Parallel()
		resp := guidedResponse(guidedFanOutHistory(), "", offScriptCall)
		before := resp.Revision()
		decision := fireGuidedPostResponse(t, resp)
		if decision.Action != domain.ActionDefer {
			t.Fatalf("Action = %q, want defer (an off-script tool call must keep the directive alive)", decision.Action)
		}
		if resp.Revision() != before {
			t.Errorf("off-script re-defer mutated the response (revision %d → %d); it only re-defers", before, resp.Revision())
		}
		for _, want := range []string{"Add unit tests", "Update the changelog"} {
			if !strings.Contains(decision.Inject, want) {
				t.Errorf("re-deferred directive dropped the still-outstanding subtask %q", want)
			}
		}
		if strings.Contains(decision.Inject, "Refactor the parser") {
			t.Error("re-deferred directive re-listed the already-dispatched first subtask")
		}
	})

	t.Run("no directive marker is a no-op", func(t *testing.T) {
		t.Parallel()
		// guidedEnumHistory carries the enumeration but no drained directive — nothing is steering, so
		// an off-script tool call is just an ordinary Turn the intercept ignores.
		history := guidedEnumHistory("1. Refactor the parser\n2. Add unit tests\n3. Update the changelog", "Refactor the parser"+hygiene)
		resp := guidedResponse(history, "", offScriptCall)
		before := resp.Revision()
		decision := fireGuidedPostResponse(t, resp)
		if decision.Action != "" || resp.Revision() != before {
			t.Fatalf("off-script call acted with no directive steering (Action %q, revision %d → %d)", decision.Action, before, resp.Revision())
		}
	})

	t.Run("no tool call is a no-op (the no-tool final-answer path)", func(t *testing.T) {
		t.Parallel()
		// A directive is steering but the model closed the fan-out with a bare answer — F2 never
		// re-defers there (a no-tool response ends the Exchange; item 7 clears any residue).
		resp := guidedResponse(guidedFanOutHistory(), "All subtasks handled; here is the synthesis.")
		before := resp.Revision()
		decision := fireGuidedPostResponse(t, resp)
		if decision.Action != "" || resp.Revision() != before {
			t.Fatalf("no-tool final answer re-deferred the directive (Action %q, revision %d → %d)", decision.Action, before, resp.Revision())
		}
	})

	t.Run("an exhausted remainder is a no-op", func(t *testing.T) {
		t.Parallel()
		enumeration := "1. Refactor the parser\n2. Add unit tests"
		call1 := guidedSubAgentCall("text_call_0", "Refactor the parser"+hygiene)
		call2 := guidedSubAgentCall("c2", "Add unit tests"+hygiene)
		history := []domain.Message{
			{Role: domain.RoleSystem, Content: guidedDecompositionDirective([]string{"Add unit tests"})},
			{Role: domain.RoleUser, Content: "big task"},
			{Role: domain.RoleAssistant, Content: enumeration, ToolCalls: []domain.ToolCall{call1}},
			{Role: domain.RoleTool, ToolCallID: "text_call_0", Content: "report 1"},
			{Role: domain.RoleAssistant, Content: "", ToolCalls: []domain.ToolCall{call2}},
			{Role: domain.RoleTool, ToolCallID: "c2", Content: "report 2"},
		}
		// Both enumeration items are already dispatched, so the off-script call re-derives an empty
		// remainder — nothing left to re-defer.
		resp := guidedResponse(history, "", offScriptCall)
		before := resp.Revision()
		decision := fireGuidedPostResponse(t, resp)
		if decision.Action != "" || resp.Revision() != before {
			t.Fatalf("exhausted remainder re-deferred on an off-script call (Action %q, revision %d → %d)", decision.Action, before, resp.Revision())
		}
	})
}

// A model-authored delegation that matches no enumeration item consumes nothing — the remainder is
// left intact (the model went off-script; tolerated, judged by self-regulation, ADR 0014 §5).
func TestGuidedDecompositionOffScriptTaskLeavesRemainderIntact(t *testing.T) {
	t.Parallel()
	resp := guidedResponse(
		guidedFanOutHistory(),
		"",
		guidedSubAgentCall("c9", "Investigate an unrelated flaky integration test"),
	)
	decision := fireGuidedPostResponse(t, resp)
	if decision.Action != domain.ActionDefer {
		t.Fatalf("Action = %q, want defer (the remainder is still non-empty)", decision.Action)
	}
	for _, want := range []string{"Add unit tests", "Update the changelog"} {
		if !strings.Contains(decision.Inject, want) {
			t.Errorf("off-script delegation wrongly dropped %q from the remainder", want)
		}
	}
}

// The remainder is a cursor over the sub_agent CALLS, not their results: an older child report
// capped to empty by tool_result_cap (the Required peer) leaves the derivation exact.
func TestGuidedDecompositionDerivesFromCallsNotCappedResults(t *testing.T) {
	t.Parallel()
	enumeration := "1. Refactor the parser\n2. Add unit tests\n3. Update the changelog"
	call1 := guidedSubAgentCall("text_call_0", "Refactor the parser "+guidedDecompositionReportHygiene)
	directive := domain.Message{Role: domain.RoleSystem, Content: guidedDecompositionDirective([]string{"Add unit tests", "Update the changelog"})}
	history := []domain.Message{
		directive,
		{Role: domain.RoleUser, Content: "big task"},
		{Role: domain.RoleAssistant, Content: enumeration, ToolCalls: []domain.ToolCall{call1}},
		{Role: domain.RoleTool, ToolCallID: "text_call_0", Content: ""}, // capped away by tool_result_cap
	}
	resp := guidedResponse(history, "", guidedSubAgentCall("c2", "Add unit tests "+guidedDecompositionReportHygiene))
	decision := fireGuidedPostResponse(t, resp)
	if decision.Action != domain.ActionDefer {
		t.Fatalf("Action = %q, want defer", decision.Action)
	}
	if !strings.Contains(decision.Inject, "Update the changelog") {
		t.Error("derivation lost the outstanding subtask")
	}
	if strings.Contains(decision.Inject, "Add unit tests") {
		t.Error("derivation did not shrink by the just-delegated subtask despite the capped result")
	}
}

// The cursor anchors on the delegation-bearing enumeration IN THE CURRENT EXCHANGE (F3), never on a
// prior-Exchange decoy the old lenient first-match anchor would have picked: a 3-line assistant answer
// that parses in-bounds but carries no delegation, or a compaction-summary-shaped multi-line assistant
// message. Both precede the current ask and neither carries a sub_agent call, so the anchor skips them.
func TestGuidedDecompositionAnchorsOnDelegationBearingEnumeration(t *testing.T) {
	t.Parallel()
	priorAnswer := "1. use the existing helper\n2. keep the signature\n3. no new deps"
	compactionSummary := "Summary of the conversation so far:\n\nWe scoped the parser work.\n" +
		"We agreed to refactor first.\nNext: add tests and update docs."
	enumeration := "1. Refactor the parser\n2. Add unit tests\n3. Update the changelog"
	call1 := guidedSubAgentCall("text_call_0", "Refactor the parser "+guidedDecompositionReportHygiene)
	history := []domain.Message{
		{Role: domain.RoleUser, Content: "an earlier, smaller ask"},
		{Role: domain.RoleAssistant, Content: priorAnswer},       // decoy: in-bounds list, no call
		{Role: domain.RoleAssistant, Content: compactionSummary}, // decoy: multi-line summary, no call
		{Role: domain.RoleUser, Content: "big task"},             // the current Exchange begins here
		{Role: domain.RoleAssistant, Content: enumeration, ToolCalls: []domain.ToolCall{call1}},
		{Role: domain.RoleTool, ToolCallID: "text_call_0", Content: "report 1"},
	}
	// Refactor already dispatched via call1; Add unit tests dispatched this Turn — so the remainder is
	// the third subtask, derived from the current-Exchange enumeration and neither prior-Exchange decoy.
	respCall := guidedSubAgentCall("c2", "Add unit tests "+guidedDecompositionReportHygiene)
	got := guidedDecompositionRemainder(guidedConv(history), []domain.ToolCall{respCall})
	want := []string{"Update the changelog"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("remainder = %v, want %v (must anchor on the delegation-bearing enumeration, not a prior-Exchange decoy)", got, want)
	}
}

// A completed fan-out in the PRIOR Exchange does not resume in a new one (F3): the current Exchange
// (after the fresh user ask) holds no delegation-bearing enumeration, so the remainder is nil rather
// than the old Exchange's leftover list.
func TestGuidedDecompositionNoCrossExchangeResumption(t *testing.T) {
	t.Parallel()
	enumeration := "1. Refactor the parser\n2. Add unit tests\n3. Update the changelog"
	call1 := guidedSubAgentCall("text_call_0", "Refactor the parser "+guidedDecompositionReportHygiene)
	history := []domain.Message{
		{Role: domain.RoleUser, Content: "big task"},
		{Role: domain.RoleAssistant, Content: enumeration, ToolCalls: []domain.ToolCall{call1}},
		{Role: domain.RoleTool, ToolCallID: "text_call_0", Content: "report 1"},
		{Role: domain.RoleUser, Content: "a brand new, unrelated ask"}, // the new Exchange — last RoleUser
	}
	if got := guidedDecompositionRemainder(guidedConv(history), nil); got != nil {
		t.Fatalf("remainder = %v, want nil (a previous Exchange's fan-out must not resume in a new Exchange)", got)
	}
}

// guidedEnumHistory is a current-Exchange conversation whose sole assistant message IS the enumeration
// (its verbatim list + the synthesized first delegation call1Task), followed by that child's report —
// the minimal shape the remainder cursor anchors on. It keeps the original ask as the last RoleUser
// message so the whole enumeration sits inside the current Exchange.
func guidedEnumHistory(enumeration, call1Task string) []domain.Message {
	call1 := guidedSubAgentCall("text_call_0", call1Task)
	return []domain.Message{
		{Role: domain.RoleUser, Content: "big task"},
		{Role: domain.RoleAssistant, Content: enumeration, ToolCalls: []domain.ToolCall{call1}},
		{Role: domain.RoleTool, ToolCallID: "text_call_0", Content: "report 1"},
	}
}

// Consumption is exact-match and consume-once (item 3): a dispatched task removes an enumeration item
// only when it equals the item or the item-plus-hygiene ask, and each dispatch consumes at most one
// occurrence. So a single dispatch of a duplicated item leaves the other copy outstanding, dispatching
// a longer prefix-nested item never absorbs the shorter one, and an off-script task consumes nothing.
func TestGuidedDecompositionConsumeOnceExactMatch(t *testing.T) {
	t.Parallel()
	hygiene := " " + guidedDecompositionReportHygiene
	tests := []struct {
		name        string
		enumeration string
		call1Task   string
		respCalls   []domain.ToolCall
		want        []string
	}{
		{
			name:        "duplicate item: one dispatch removes exactly one occurrence",
			enumeration: "1. Add tests\n2. Add tests",
			call1Task:   "Add tests" + hygiene,
			want:        []string{"Add tests"},
		},
		{
			name:        "prefix-nested: dispatching the longer leaves the shorter",
			enumeration: "1. Add tests for the CLI\n2. Add tests",
			call1Task:   "Add tests for the CLI" + hygiene,
			want:        []string{"Add tests"},
		},
		{
			name:        "off-script task matching nothing leaves the remainder intact",
			enumeration: "1. Add tests\n2. Update the changelog",
			call1Task:   "Add tests" + hygiene,
			respCalls:   []domain.ToolCall{guidedSubAgentCall("c9", "Investigate an unrelated flaky test")},
			want:        []string{"Update the changelog"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			history := guidedEnumHistory(tc.enumeration, tc.call1Task)
			got := guidedDecompositionRemainder(guidedConv(history), tc.respCalls)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("remainder = %v, want %v", got, tc.want)
			}
		})
	}
}

// The inspect-only no-ops: an exhausted remainder, no marker at all, and a steered response that also
// carries a model tool call all leave the response untouched with no decision (ADR 0014 §5 fail-soft).
func TestGuidedDecompositionInterceptNoOps(t *testing.T) {
	t.Parallel()

	t.Run("exhausted remainder ends the fan-out", func(t *testing.T) {
		t.Parallel()
		enumeration := "1. Refactor the parser\n2. Add unit tests"
		call1 := guidedSubAgentCall("text_call_0", "Refactor the parser "+guidedDecompositionReportHygiene)
		directive := domain.Message{Role: domain.RoleSystem, Content: guidedDecompositionDirective([]string{"Add unit tests"})}
		history := []domain.Message{
			directive,
			{Role: domain.RoleUser, Content: "big task"},
			{Role: domain.RoleAssistant, Content: enumeration, ToolCalls: []domain.ToolCall{call1}},
			{Role: domain.RoleTool, ToolCallID: "text_call_0", Content: "report 1"},
		}
		// The model delegates the LAST subtask, so the remainder drains to empty.
		resp := guidedResponse(history, "", guidedSubAgentCall("c2", "Add unit tests "+guidedDecompositionReportHygiene))
		before := resp.Revision()
		decision := fireGuidedPostResponse(t, resp)
		if decision.Action != "" {
			t.Fatalf("Action = %q, want empty (no directive once the queue is drained)", decision.Action)
		}
		if resp.Revision() != before {
			t.Fatalf("revision changed (%d → %d); an exhausted remainder is a pure no-op", before, resp.Revision())
		}
	})

	t.Run("no marker is a no-op", func(t *testing.T) {
		t.Parallel()
		// A bare subtask list with no steer/directive marker in history — an unrelated response.
		resp := guidedResponse([]domain.Message{{Role: domain.RoleUser, Content: "hi"}}, "1. do a\n2. do b")
		before := resp.Revision()
		decision := fireGuidedPostResponse(t, resp)
		if decision.Action != "" || resp.Revision() != before {
			t.Fatalf("no-marker list acted (Action %q, revision %d → %d); want a pure no-op", decision.Action, before, resp.Revision())
		}
		if len(resp.ToolCalls()) != 0 {
			t.Fatalf("no-marker list synthesized %d calls, want 0", len(resp.ToolCalls()))
		}
	})

	t.Run("steered response with a model tool call is a no-op", func(t *testing.T) {
		t.Parallel()
		// Steer present but the model also emitted a tool call — case 1 needs no tool calls and no
		// directive is in flight yet, so the intercept stays out (§5).
		steer := domain.Message{Role: domain.RoleUser, Content: guidedDecompositionSteer}
		other := domain.ToolCall{ID: "r1", Tool: "read_file", Arguments: []byte(`{"path":"x"}`)}
		resp := guidedResponse([]domain.Message{steer}, "1. do a\n2. do b", other)
		before := resp.Revision()
		decision := fireGuidedPostResponse(t, resp)
		if decision.Action != "" || resp.Revision() != before {
			t.Fatalf("intercepted despite a model tool call (Action %q, revision %d → %d)", decision.Action, before, resp.Revision())
		}
	})
}

// guidedFanOutHistory is a mid-fan-out conversation: the enumeration (its verbatim list + the
// synthesized first delegation), the first child's report, and the drained directive steering the
// next Turn — the shape a follow-through Turn's intercept reads. The directive rides the SYSTEM
// message, faithfully to production: buildRequest drains it and InjectContext appends to the system
// prompt because the committed history ends in a tool result (loop.go / Request.InjectContext). So
// the original ask stays the last RoleUser message and the enumeration sits inside the current
// Exchange the item-2 cursor scans.
func guidedFanOutHistory() []domain.Message {
	enumeration := "1. Refactor the parser\n2. Add unit tests\n3. Update the changelog"
	call1 := guidedSubAgentCall("text_call_0", "Refactor the parser "+guidedDecompositionReportHygiene)
	return []domain.Message{
		{Role: domain.RoleSystem, Content: guidedDecompositionDirective([]string{"Add unit tests", "Update the changelog"})},
		{Role: domain.RoleUser, Content: "big task"},
		{Role: domain.RoleAssistant, Content: enumeration, ToolCalls: []domain.ToolCall{call1}},
		{Role: domain.RoleTool, ToolCallID: "text_call_0", Content: "report 1"},
	}
}
