package mechanisms

import (
	"context"
	"encoding/json"
	"errors"
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
	m, err := Build(guidedDecompositionID, Deps{})
	if err != nil {
		t.Fatalf("Build(%q): %v", guidedDecompositionID, err)
	}
	d := m.Descriptor
	if d.ID != guidedDecompositionID {
		t.Errorf("ID = %q, want %q", d.ID, guidedDecompositionID)
	}
	if d.Capability != domain.CapProactiveNudge {
		t.Errorf("Capability = %q, want proactive-nudge", d.Capability)
	}
	if d.Suppression != domain.SuppressStrikesThree {
		t.Errorf("Suppression = %q, want strikes-3", d.Suppression)
	}
	// IncompatibleWith truncate_history alone (F7 — a mid-Exchange truncation can drop the
	// enumeration message the cursor re-derives from). The decompose edge (locked decision 2) went
	// with that row when it retired in v0.20.0 (ADR 0071).
	if len(d.IncompatibleWith) != 1 || d.IncompatibleWith[0] != truncateHistoryID {
		t.Errorf("IncompatibleWith = %v, want [%q]", d.IncompatibleWith, truncateHistoryID)
	}
	// NO Requires edge: locked decision 3's Required peer, tool_result_cap, is the `tool-result-cap`
	// Floor guard now (ADR 0071), so the capping it insisted on runs in every arm and a gate naming a
	// retired ID could only ever refuse.
	if len(d.Requires) != 0 {
		t.Errorf("Requires = %v, want none — the Required peer is a Floor guard now", d.Requires)
	}
	// After toolfilter — the sub_agent-presence gate must read the final (post-toolfilter) menu.
	if o := m.Ordering; len(o.After) != 1 || o.After[0] != toolFilterID {
		t.Errorf("Ordering.After = %v, want [%q]", o.After, toolFilterID)
	}
	if _, ok := m.Hook.(domain.PreRequestHook); !ok {
		t.Error("guided_decomposition does not implement PreRequestHook")
	}
	// Both halves live on the one struct: the pre-request gate/steer and the post-response
	// intercept + serialized follow-through. Suppressing the Mechanism disarms both as a unit.
	if _, ok := m.Hook.(domain.PostResponseHook); !ok {
		t.Error("guided_decomposition does not implement PostResponseHook (the intercept half)")
	}
}

// guided_decomposition is IncompatibleWith truncate_history (F7): a mid-Exchange truncation longer
// than its keep window can drop the enumeration message the cursor re-derives the remainder from,
// destroying the fan-out mid-flight. One-sided declaration on guided_decomposition suffices —
// detectIncompatibility is symmetric in effect — so co-registering the two fails
// ValidateIncompatibilities and names the pair, while guided_decomposition on its own validates
// cleanly.
func TestGuidedDecompositionIncompatibleWithTruncateHistory(t *testing.T) {
	t.Parallel()

	// The row the bench arms plus truncate_history is refused, naming both offenders.
	reg := domain.NewMechanismRegistry()
	for _, id := range []domain.MechanismID{guidedDecompositionID, truncateHistoryID} {
		if err := reg.Add(mustBuild(t, id)); err != nil {
			t.Fatalf("Add(%q): %v", id, err)
		}
	}
	err := reg.ValidateIncompatibilities()
	if !errors.Is(err, domain.ErrIncompatibleMechanisms) {
		t.Fatalf("ValidateIncompatibilities with truncate_history co-enabled = %v, want ErrIncompatibleMechanisms", err)
	}
	if msg := err.Error(); !strings.Contains(msg, string(guidedDecompositionID)) || !strings.Contains(msg, string(truncateHistoryID)) {
		t.Errorf("error %q does not name the incompatible pair %q/%q", msg, guidedDecompositionID, truncateHistoryID)
	}

	// guided_decomposition without truncate_history still validates on every gate — the new relation
	// refuses only the truncate_history combination.
	valid := domain.NewMechanismRegistry()
	for _, id := range []domain.MechanismID{guidedDecompositionID} {
		if err := valid.Add(mustBuild(t, id)); err != nil {
			t.Fatalf("Add(%q): %v", id, err)
		}
	}
	if err := valid.ValidateIncompatibilities(); err != nil {
		t.Errorf("ValidateIncompatibilities on guided_decomposition alone = %v, want nil", err)
	}
	if err := valid.ValidateRequirements(); err != nil {
		t.Errorf("ValidateRequirements on guided_decomposition alone = %v, want nil", err)
	}
}

func TestGuidedDecompositionBuildsFromCatalogue(t *testing.T) {
	t.Parallel()
	m, err := Build(guidedDecompositionID, Deps{})
	if err != nil {
		t.Fatalf("Build(%q): %v", guidedDecompositionID, err)
	}
	if m.Descriptor.ID != guidedDecompositionID {
		t.Errorf("built ID = %q, want %q", m.Descriptor.ID, guidedDecompositionID)
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

// Interjections (ADR 0025) and the gate. Signal A is a TAIL test, not an opening test — it fires
// wherever the conversation ends in an oversized user message — so a mid-Exchange remark carrying big
// @file refs arms it, deliberately: that is the same "the task just grew too big while the model
// works" event signal B answers, arriving through the other door
// (guidedDecompositionTailUserOversized carries the full rationale). What an Interjection must NEVER
// do is move the Exchange window the once-per-Exchange F1 guard reads, because the derived opening
// skips it — so a fan-out already begun in this Exchange keeps the gate quiet with a remark on the
// tail.
func TestGuidedDecompositionInterjections(t *testing.T) {
	t.Parallel()

	// A running Exchange: a small opening ask, the model mid-work on a read call, the result.
	openExchange := []domain.Message{
		{Role: domain.RoleUser, Content: "go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file", Arguments: []byte(`{}`)}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: "ok"},
	}
	// The same Exchange, but a fan-out has already begun in it (a committed sub_agent call).
	fannedOut := []domain.Message{
		{Role: domain.RoleUser, Content: "go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{guidedSubAgentCall("c1", "do a")}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: "child report"},
	}
	interjection := domain.Message{Role: domain.RoleUser, Content: oversizedUser, Interjected: true}

	tests := []struct {
		name string
		msgs []domain.Message
		fire bool
	}{
		{
			name: "control: the running Exchange alone is under both thresholds",
			msgs: openExchange,
			fire: false,
		},
		{
			name: "an oversized interjection arms signal A mid-Exchange",
			msgs: append(append([]domain.Message{}, openExchange...), interjection),
			fire: true,
		},
		{
			name: "an interjection does not re-arm the once-per-Exchange F1 guard",
			msgs: append(append([]domain.Message{}, fannedOut...), interjection),
			fire: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := guidedRequest(tc.msgs, guidedMenu, guidedBudget, 0)
			before := req.Revision()
			if err := (guidedDecompositionMechanism{}).PreRequest(context.Background(), req); err != nil {
				t.Fatalf("PreRequest: %v", err)
			}
			if fired := req.Revision() != before; fired != tc.fire {
				t.Fatalf("fired = %v, want %v (revision %d → %d)", fired, tc.fire, before, req.Revision())
			}
		})
	}
}

// An outstanding steer or fan-out directive in the conversation stops the gate from steering again —
// no double-steer (locked decision 1). Both markers are exercised, over an otherwise-firing signal-A
// request. The marker rides a user message at a LINE START — the shape the loop's InjectContext writes
// a genuine injection in (F5); a mid-line echo would no longer count (see the role-scoped/line-anchored
// tests below).
func TestGuidedDecompositionNoDoubleSteer(t *testing.T) {
	t.Parallel()
	for _, marker := range []string{guidedDecompositionSteerMarker, guidedDecompositionDirectiveMarker} {
		t.Run(marker, func(t *testing.T) {
			t.Parallel()
			msgs := []domain.Message{
				{Role: domain.RoleUser, Content: marker + " is already steering this request ..."},
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

// F5 — marker detection is line-anchored and role-scoped: a marker counts only where it starts a line
// of a RoleUser or RoleSystem message (the only places the loop's InjectContext writes an injection).
// An assistant echo, a tool result, or @file-style user content carrying the phrase mid-line never
// counts; the real injected steer and drained directive (marker at a line start in a user or system
// message) still do. Exercised through both marker scanners — the gate's guidedDecompositionOutstanding
// and the intercept's guidedDecompositionMarkerPresent.
func TestGuidedDecompositionMarkersLineAnchoredRoleScoped(t *testing.T) {
	t.Parallel()

	// A drained directive as production writes it when history ends in a tool result: appended to the
	// system prompt after appendOrCreateSystem's "\n\n", so its marker starts a line mid-message.
	directiveInSystem := "You are a helpful assistant.\n\n" + guidedDecompositionDirective([]string{"do a", "do b"}, 1)

	t.Run("guidedDecompositionOutstanding", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name string
			msgs []domain.Message
			want bool
		}{
			{
				name: "steer at a user-message line start matches",
				msgs: []domain.Message{{Role: domain.RoleUser, Content: guidedDecompositionSteer}},
				want: true,
			},
			{
				name: "drained directive at a system-message line start matches",
				msgs: []domain.Message{{Role: domain.RoleSystem, Content: directiveInSystem}},
				want: true,
			},
			{
				name: "phrase mid-line in a user @file-style message does not match",
				msgs: []domain.Message{{Role: domain.RoleUser, Content: "see notes.md: " + guidedDecompositionSteerMarker + " is discussed here"}},
				want: false,
			},
			{
				name: "assistant echo of the directive phrase at a line start does not match — wrong role",
				msgs: []domain.Message{{Role: domain.RoleAssistant, Content: guidedDecompositionDirectiveMarker + " — as you asked, here they are"}},
				want: false,
			},
			{
				name: "tool result carrying the phrase does not match — wrong role",
				msgs: []domain.Message{{Role: domain.RoleTool, ToolCallID: "c1", Content: guidedDecompositionSteerMarker + " logged"}},
				want: false,
			},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				if got := guidedDecompositionOutstanding(guidedConv(tc.msgs)); got != tc.want {
					t.Fatalf("guidedDecompositionOutstanding = %v, want %v", got, tc.want)
				}
			})
		}
	})

	t.Run("guidedDecompositionMarkerPresent", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name   string
			msgs   []domain.Message
			marker string
			want   bool
		}{
			{
				name:   "injected steer at a user-message line start matches",
				msgs:   []domain.Message{{Role: domain.RoleUser, Content: guidedDecompositionSteer}},
				marker: guidedDecompositionSteerMarker,
				want:   true,
			},
			{
				name:   "drained directive at a system-message line start matches",
				msgs:   []domain.Message{{Role: domain.RoleSystem, Content: directiveInSystem}},
				marker: guidedDecompositionDirectiveMarker,
				want:   true,
			},
			{
				name:   "assistant echo of the directive phrase mid-reply does not match — no bogus follow-through",
				msgs:   []domain.Message{{Role: domain.RoleAssistant, Content: "I will address the " + guidedDecompositionDirectiveMarker + " you mentioned."}},
				marker: guidedDecompositionDirectiveMarker,
				want:   false,
			},
			{
				name:   "tool result echoing the steer phrase does not match — wrong role",
				msgs:   []domain.Message{{Role: domain.RoleTool, ToolCallID: "c1", Content: guidedDecompositionSteerMarker + " noted"}},
				marker: guidedDecompositionSteerMarker,
				want:   false,
			},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				if got := guidedDecompositionMarkerPresent(guidedConv(tc.msgs), tc.marker); got != tc.want {
					t.Fatalf("guidedDecompositionMarkerPresent(%q) = %v, want %v", tc.marker, got, tc.want)
				}
			})
		}
	})

	// The follow-through case keys on guidedDecompositionMarkerPresent, so an assistant message merely
	// echoing the directive phrase does not steer it: a mid-fan-out-shaped tool Turn stays a pure no-op.
	t.Run("assistant echo does not trigger the follow-through case", func(t *testing.T) {
		t.Parallel()
		history := []domain.Message{
			{Role: domain.RoleUser, Content: "big task"},
			{Role: domain.RoleAssistant, Content: "Here are the " + guidedDecompositionDirectiveMarker + ": step one, step two."},
		}
		resp := guidedResponse(history, "", domain.ToolCall{ID: "r1", Tool: "read_file", Arguments: []byte(`{"path":"x"}`)})
		before := resp.Revision()
		decision := fireGuidedPostResponse(t, resp)
		if decision.Action != "" || resp.Revision() != before {
			t.Fatalf("assistant echo triggered the follow-through (Action %q, revision %d → %d)", decision.Action, before, resp.Revision())
		}
	})
}

// A committed sub_agent fan-out in the CURRENT Exchange silences the gate for the rest of that
// Exchange (F1 — once-per-Exchange on committed evidence): even with signal B live and NO
// steer/directive marker outstanding (the request-scoped markers have drained by the synthesis Turn),
// the gate does not re-steer. A fan-out in a PRIOR Exchange (before the last user ask) does not
// silence it — a new Exchange re-arms the gate.
func TestGuidedDecompositionOncePerExchange(t *testing.T) {
	t.Parallel()
	// A mid-Exchange assistant message that IS a committed fan-out (a bounded list plus a sub_agent
	// call). No steer/directive marker rides it — the committed-evidence scan, not a marker, is what
	// must keep the gate quiet.
	bigList := "1. Refactor the parser\n2. Add unit tests\n3. Update the changelog"
	subCall := guidedSubAgentCall("text_call_0", "Refactor the parser "+guidedDecompositionReportHygiene)

	t.Run("committed sub_agent call in the current Exchange keeps the gate quiet under a live signal B", func(t *testing.T) {
		t.Parallel()
		// Signal B is live: the last assistant carried tool calls and the total is >2000 tokens.
		msgs := []domain.Message{
			{Role: domain.RoleUser, Content: "big task"}, // the current Exchange begins here
			{Role: domain.RoleAssistant, Content: bigList, ToolCalls: []domain.ToolCall{subCall}},
			{Role: domain.RoleTool, ToolCallID: "text_call_0", Content: strings.Repeat("z", 9000)},
		}
		req := guidedRequest(msgs, guidedMenu, guidedBudget, 0)
		before := req.Revision()
		if err := (guidedDecompositionMechanism{}).PreRequest(context.Background(), req); err != nil {
			t.Fatalf("PreRequest: %v", err)
		}
		if req.Revision() != before {
			t.Fatal("gate re-steered despite a committed sub_agent fan-out in the current Exchange (F1 once-per-Exchange)")
		}
	})

	t.Run("a prior-Exchange fan-out re-arms the gate for a new oversized ask", func(t *testing.T) {
		t.Parallel()
		// The fan-out is in the PRIOR Exchange; the fresh oversized user ask (signal A) opens a new one.
		msgs := []domain.Message{
			{Role: domain.RoleUser, Content: "an earlier ask"},
			{Role: domain.RoleAssistant, Content: bigList, ToolCalls: []domain.ToolCall{subCall}},
			{Role: domain.RoleTool, ToolCallID: "text_call_0", Content: "report"},
			{Role: domain.RoleUser, Content: oversizedUser}, // the NEW Exchange — trips signal A
		}
		req := guidedRequest(msgs, guidedMenu, guidedBudget, 0)
		before := req.Revision()
		if err := (guidedDecompositionMechanism{}).PreRequest(context.Background(), req); err != nil {
			t.Fatalf("PreRequest: %v", err)
		}
		if req.Revision() == before {
			t.Fatal("gate stayed quiet in a new Exchange despite the fan-out being in the PRIOR one (F1 must re-arm)")
		}
	})
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
// off it). finish follows whether the model itself emitted tool calls. The view carries NO Parallel
// agents width, the unstamped 0 a server advertising no slots produces — so every test written
// before the batch amendment keeps exercising the serialized floor.
func guidedResponse(history []domain.Message, text string, calls ...domain.ToolCall) *domain.Response {
	return guidedCappedResponse(history, 0, text, calls...)
}

// guidedCappedResponse is guidedResponse with an explicit Parallel agents width stamped on the view
// — the seam the intercept sizes a batch by (ADR 0039 / ADR 0014 amendment 2026-08-07).
func guidedCappedResponse(history []domain.Message, width int, text string, calls ...domain.ToolCall) *domain.Response {
	req := domain.NewRequest("m", history, guidedMenu, guidedBudget, 0, nil)
	req.SetParallelAgents(width)
	finish := domain.FinishStop
	if len(calls) > 0 {
		finish = domain.FinishToolCalls
	}
	return domain.NewResponse(text, "", calls, finish, req.View())
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

// The batch rule (ADR 0014 amendment 2026-08-07): a Turn dispatches min(cap, remaining) delegations,
// so a 7-item enumeration on a cap-3 server fans out 3 + 3 + 1 across THREE Turns — a quiescent
// boundary (a whole Turn, its reports committed to honest history) still separates the batches. The
// remainder is re-derived exactly as at cap 1: the cursor counts items, never batches, and the last
// batch shrinks to what is left, so the directive asks in the singular again when one item remains.
func TestGuidedDecompositionDispatchesOneBatchPerTurn(t *testing.T) {
	t.Parallel()
	const width = 3
	items := []string{"one", "two", "three", "four", "five", "six", "seven"}
	enumeration := "1. one\n2. two\n3. three\n4. four\n5. five\n6. six\n7. seven"
	opening := domain.Message{Role: domain.RoleUser, Content: "big task"}
	steer := domain.Message{Role: domain.RoleUser, Content: guidedDecompositionSteer}

	// Turn 1 — the enumeration Turn: the intercept synthesizes the first batch of 3.
	resp := guidedCappedResponse([]domain.Message{opening, steer}, width, enumeration)
	decision := fireGuidedPostResponse(t, resp)
	batch1 := resp.ToolCalls()
	if len(batch1) != width {
		t.Fatalf("first batch = %d calls, want %d (min(cap, remaining))", len(batch1), width)
	}
	if got := guidedTasks(t, batch1); !reflect.DeepEqual(got, items[:3]) {
		t.Errorf("first batch delegated %v, want the first three subtasks %v", got, items[:3])
	}
	guidedAssertDistinctIDs(t, batch1)
	if decision.Action != domain.ActionDefer {
		t.Fatalf("Turn 1 Action = %q, want defer (four subtasks are still outstanding)", decision.Action)
	}
	if !strings.Contains(decision.Inject, "(4 left)") || !strings.Contains(decision.Inject, "next 3 subtasks") {
		t.Errorf("Turn 1 directive does not carry 4 left / a batch of 3:\n%s", decision.Inject)
	}
	for i, outstanding := range items[3:] {
		if !strings.Contains(decision.Inject, fmt.Sprintf("\n%d. %s\n", i+1, outstanding)) {
			t.Errorf("Turn 1 directive is missing the outstanding subtask line %q:\n%s", outstanding, decision.Inject)
		}
	}
	for _, dispatched := range items[:3] {
		if strings.Contains(decision.Inject, ". "+dispatched+"\n") {
			t.Errorf("Turn 1 directive re-lists the already-dispatched subtask %q", dispatched)
		}
	}

	// Turn 2 — the model follows the directive with its own batch of 3; one item is left.
	history := guidedBatchHistory(decision.Inject, opening, enumeration, batch1)
	batch2 := []domain.ToolCall{
		guidedSubAgentCall("c4", "four "+guidedDecompositionReportHygiene),
		guidedSubAgentCall("c5", "five "+guidedDecompositionReportHygiene),
		guidedSubAgentCall("c6", "six "+guidedDecompositionReportHygiene),
	}
	resp2 := guidedCappedResponse(history, width, "", batch2...)
	decision2 := fireGuidedPostResponse(t, resp2)
	if decision2.Action != domain.ActionDefer {
		t.Fatalf("Turn 2 Action = %q, want defer (one subtask is still outstanding)", decision2.Action)
	}
	if !strings.Contains(decision2.Inject, "(1 left)") || !strings.Contains(decision2.Inject, "a single "+tools.SubAgentToolName+" call") {
		t.Errorf("Turn 2 directive does not shrink to a single-delegation ask:\n%s", decision2.Inject)
	}
	if !strings.Contains(decision2.Inject, "seven") {
		t.Error("Turn 2 directive dropped the last outstanding subtask")
	}

	// Turn 3 — the last delegation drains the queue: nothing left to carry, no decision.
	history = append(history, domain.Message{Role: domain.RoleAssistant, ToolCalls: batch2})
	for _, c := range batch2 {
		history = append(history, domain.Message{Role: domain.RoleTool, ToolCallID: c.ID, Content: "report"})
	}
	resp3 := guidedCappedResponse(history, width, "",
		guidedSubAgentCall("c7", "seven "+guidedDecompositionReportHygiene))
	before := resp3.Revision()
	decision3 := fireGuidedPostResponse(t, resp3)
	if decision3.Action != "" || resp3.Revision() != before {
		t.Fatalf("Turn 3 acted on an exhausted remainder (Action %q, revision %d → %d)", decision3.Action, before, resp3.Revision())
	}
}

// guidedBatchHistory is the honest history a follow-through Turn reads after one batch has been
// dispatched: the drained directive in the system message, the opening ask, the model's enumeration
// carrying the batch's calls, and one report per call.
func guidedBatchHistory(directive string, opening domain.Message, enumeration string, batch []domain.ToolCall) []domain.Message {
	history := []domain.Message{
		{Role: domain.RoleSystem, Content: directive},
		opening,
		{Role: domain.RoleAssistant, Content: enumeration, ToolCalls: batch},
	}
	for _, c := range batch {
		history = append(history, domain.Message{Role: domain.RoleTool, ToolCallID: c.ID, Content: "report"})
	}
	return history
}

// guidedTasks returns each call's delegated subtask with the hygiene ask stripped — what the batch
// actually asked for, comparable against the enumeration.
func guidedTasks(t *testing.T, calls []domain.ToolCall) []string {
	t.Helper()
	var tasks []string
	for _, c := range calls {
		if c.Tool != tools.SubAgentToolName {
			t.Fatalf("synthesized call tool = %q, want %q", c.Tool, tools.SubAgentToolName)
		}
		var args tools.SubAgentArgs
		if err := json.Unmarshal(c.Arguments, &args); err != nil {
			t.Fatalf("synthesized args do not unmarshal to SubAgentArgs: %v", err)
		}
		if !strings.HasSuffix(args.Task, " "+guidedDecompositionReportHygiene) {
			t.Errorf("task = %q, want the compact-report hygiene ask appended (ADR 0014 §4)", args.Task)
		}
		tasks = append(tasks, strings.TrimSuffix(args.Task, " "+guidedDecompositionReportHygiene))
	}
	return tasks
}

// guidedAssertDistinctIDs proves a batch's calls are individually addressable: the call ID is what
// every child event, tool result and TUI block is attributed by (ADR 0039 decision 5), so two
// siblings sharing one would collapse into each other.
func guidedAssertDistinctIDs(t *testing.T, calls []domain.ToolCall) {
	t.Helper()
	seen := map[string]bool{}
	for _, c := range calls {
		if c.ID == "" || seen[c.ID] {
			t.Fatalf("batch call IDs are not distinct: %q repeats (ids so far %v)", c.ID, seen)
		}
		seen[c.ID] = true
	}
}

// Cap 1 — and the unstamped 0 a server advertising no slots produces — reproduces the ratified
// serialized floor exactly: ONE delegation carrying the loop's bare synthesized-call ID, and the
// singular directive word for word as before the batch amendment.
func TestGuidedDecompositionCapOneKeepsTheSerializedFloor(t *testing.T) {
	t.Parallel()
	steer := domain.Message{Role: domain.RoleUser, Content: guidedDecompositionSteer}
	list := "1. Refactor the parser\n2. Add unit tests\n3. Update the changelog"

	for _, width := range []int{0, 1} {
		resp := guidedCappedResponse(
			[]domain.Message{{Role: domain.RoleUser, Content: "big task"}, steer}, width, list)
		decision := fireGuidedPostResponse(t, resp)
		calls := resp.ToolCalls()
		if len(calls) != 1 {
			t.Fatalf("width %d appended %d calls, want exactly 1 (the serialized floor)", width, len(calls))
		}
		if calls[0].ID != "text_call_0" {
			t.Errorf("width %d synthesized call ID = %q, want the loop's bare text_call_0", width, calls[0].ID)
		}
		if decision.Inject != guidedDecompositionDirective([]string{"Add unit tests", "Update the changelog"}, 1) {
			t.Errorf("width %d directive is not the width-1 directive:\n%s", width, decision.Inject)
		}
		for _, want := range []string{
			"one delegation per turn",
			"a single " + tools.SubAgentToolName + " call",
			"do not delegate more than one at a time",
			"Give the sub-agent this instruction too",
		} {
			if !strings.Contains(decision.Inject, want) {
				t.Errorf("width %d directive lost the singular clause %q:\n%s", width, want, decision.Inject)
			}
		}
	}
}

// A cap wider than the enumeration dispatches the whole list in one batch — and defers NOTHING,
// because there is no remainder to carry. The appended calls still book the fire (R4).
func TestGuidedDecompositionBatchCoveringTheListDefersNothing(t *testing.T) {
	t.Parallel()
	steer := domain.Message{Role: domain.RoleUser, Content: guidedDecompositionSteer}
	resp := guidedCappedResponse(
		[]domain.Message{{Role: domain.RoleUser, Content: "big task"}, steer}, 5,
		"1. one\n2. two\n3. three")
	before := resp.Revision()
	decision := fireGuidedPostResponse(t, resp)

	if got := guidedTasks(t, resp.ToolCalls()); !reflect.DeepEqual(got, []string{"one", "two", "three"}) {
		t.Errorf("dispatched %v, want the whole three-item enumeration in one batch", got)
	}
	if decision.Action != "" || decision.Inject != "" {
		t.Errorf("deferred %q/%q with an empty remainder; there is nothing to carry", decision.Action, decision.Inject)
	}
	if resp.Revision() == before {
		t.Error("the batch did not bump the revision (the acted-fire probe, R4)")
	}
}

// An off-script tool Turn mid-fan-out re-defers the remainder INTACT (F2), now batch-shaped: the
// re-deferred directive asks for min(cap, remaining) — two, not the cap's three, because only two
// subtasks are left.
func TestGuidedDecompositionOffScriptToolTurnReDefersABatch(t *testing.T) {
	t.Parallel()
	offScriptCall := domain.ToolCall{ID: "r1", Tool: "read_file", Arguments: []byte(`{"path":"parser.go"}`)}
	resp := guidedCappedResponse(guidedFanOutHistory(), 3, "", offScriptCall)
	decision := fireGuidedPostResponse(t, resp)
	if decision.Action != domain.ActionDefer {
		t.Fatalf("Action = %q, want defer (an off-script tool call must keep the directive alive)", decision.Action)
	}
	if !strings.Contains(decision.Inject, "next 2 subtasks") {
		t.Errorf("re-deferred directive does not ask for the 2 remaining as one batch:\n%s", decision.Inject)
	}
	for _, want := range []string{"Add unit tests", "Update the changelog"} {
		if !strings.Contains(decision.Inject, want) {
			t.Errorf("re-deferred directive dropped the still-outstanding subtask %q", want)
		}
	}
}

// The batch size is the one width rule: min(cap, remaining), with anything below 1 — an unstamped
// view, a hand-built one carrying nonsense — reading as the serial floor.
func TestGuidedDecompositionBatchSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		width, remaining, want int
	}{
		{width: 0, remaining: 5, want: 1},  // unstamped view — the serialized floor
		{width: -2, remaining: 5, want: 1}, // nonsense — never negative, never zero
		{width: 1, remaining: 5, want: 1},
		{width: 3, remaining: 5, want: 3},
		{width: 5, remaining: 3, want: 3}, // the tail batch shrinks to what is left
		{width: 3, remaining: 0, want: 0},
	}
	for _, tt := range tests {
		if got := guidedDecompositionBatchSize(tt.width, tt.remaining); got != tt.want {
			t.Errorf("guidedDecompositionBatchSize(%d, %d) = %d, want %d", tt.width, tt.remaining, got, tt.want)
		}
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
			{Role: domain.RoleSystem, Content: guidedDecompositionDirective([]string{"Add unit tests"}, 1)},
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
// capped to empty by the tool-result-cap Floor guard leaves the derivation exact.
func TestGuidedDecompositionDerivesFromCallsNotCappedResults(t *testing.T) {
	t.Parallel()
	enumeration := "1. Refactor the parser\n2. Add unit tests\n3. Update the changelog"
	call1 := guidedSubAgentCall("text_call_0", "Refactor the parser "+guidedDecompositionReportHygiene)
	directive := domain.Message{Role: domain.RoleSystem, Content: guidedDecompositionDirective([]string{"Add unit tests", "Update the changelog"}, 1)}
	history := []domain.Message{
		directive,
		{Role: domain.RoleUser, Content: "big task"},
		{Role: domain.RoleAssistant, Content: enumeration, ToolCalls: []domain.ToolCall{call1}},
		{Role: domain.RoleTool, ToolCallID: "text_call_0", Content: ""}, // capped away by the tool-result-cap guard
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

// The cursor helpers read the current Exchange through the domain seam (ADR 0017 §1): on a
// two-Exchange history the enumeration anchor, the dispatched-task window, and the F1 fan-out-begun
// check all agree with domain.CurrentExchange. The expectations are derived from the seam's own
// output (After()), so the derivations cannot drift apart now that they share an implementation —
// and then pinned concretely so the seam-derived expectations stay honest.
func TestGuidedDecompositionAgreesWithDomainCurrentExchange(t *testing.T) {
	t.Parallel()
	hygiene := " " + guidedDecompositionReportHygiene
	history := []domain.Message{
		// Exchange 1 — a completed fan-out whose enumeration and dispatch must not leak forward.
		{Role: domain.RoleUser, Content: "first big task"},
		{Role: domain.RoleAssistant, Content: "1. Old subtask one\n2. Old subtask two",
			ToolCalls: []domain.ToolCall{guidedSubAgentCall("a1", "Old subtask one"+hygiene)}},
		{Role: domain.RoleTool, ToolCallID: "a1", Content: "old report"},
		{Role: domain.RoleAssistant, Content: "the first task is done"},
		// Exchange 2 — the current one: its own enumeration plus the first dispatch.
		{Role: domain.RoleUser, Content: "second big task"},
		{Role: domain.RoleAssistant, Content: "1. Refactor the parser\n2. Add unit tests\n3. Update the changelog",
			ToolCalls: []domain.ToolCall{guidedSubAgentCall("b1", "Refactor the parser"+hygiene)}},
		{Role: domain.RoleTool, ToolCallID: "b1", Content: "report 1"},
	}
	conv := guidedConv(history)

	// Derive the expected anchor, window, and F1 evidence from the seam's own output.
	var wantEnumeration, wantDispatched []string
	wantFanOutBegun := false
	for _, m := range domain.CurrentExchange(conv).After() {
		if m.Role != domain.RoleAssistant {
			continue
		}
		wantDispatched = append(wantDispatched, guidedDecompositionCallTasks(m.ToolCalls)...)
		if guidedDecompositionHasSubAgentCall(m.ToolCalls) {
			wantFanOutBegun = true
			if wantEnumeration == nil {
				if parsed := guidedDecompositionParseList(m.Content); guidedDecompositionListInBounds(parsed) {
					wantEnumeration = parsed
				}
			}
		}
	}

	if got := guidedDecompositionEnumeration(conv); !reflect.DeepEqual(got, wantEnumeration) {
		t.Fatalf("enumeration = %v, want %v (the seam's derivation)", got, wantEnumeration)
	}
	if got := guidedDecompositionDispatchedTasks(conv); !reflect.DeepEqual(got, wantDispatched) {
		t.Fatalf("dispatched tasks = %v, want %v (the seam's derivation)", got, wantDispatched)
	}
	if got := guidedDecompositionFanOutBegun(conv); got != wantFanOutBegun {
		t.Fatalf("fanOutBegun = %v, want %v (the seam's derivation)", got, wantFanOutBegun)
	}

	// Concrete pins: only Exchange 2's enumeration and dispatch — nothing from Exchange 1.
	if want := []string{"Refactor the parser", "Add unit tests", "Update the changelog"}; !reflect.DeepEqual(wantEnumeration, want) {
		t.Fatalf("seam-derived enumeration = %v, want %v", wantEnumeration, want)
	}
	if want := []string{"Refactor the parser" + hygiene}; !reflect.DeepEqual(wantDispatched, want) {
		t.Fatalf("seam-derived dispatched tasks = %v, want %v", wantDispatched, want)
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
		directive := domain.Message{Role: domain.RoleSystem, Content: guidedDecompositionDirective([]string{"Add unit tests"}, 1)}
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
		{Role: domain.RoleSystem, Content: guidedDecompositionDirective([]string{"Add unit tests", "Update the changelog"}, 1)},
		{Role: domain.RoleUser, Content: "big task"},
		{Role: domain.RoleAssistant, Content: enumeration, ToolCalls: []domain.ToolCall{call1}},
		{Role: domain.RoleTool, ToolCallID: "text_call_0", Content: "report 1"},
	}
}
