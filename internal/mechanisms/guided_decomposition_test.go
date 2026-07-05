package mechanisms

import (
	"context"
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
	// The post-response half lands in item 4; until then the struct must NOT satisfy
	// PostResponseHook (item 4 adjusts this assertion when it adds PostResponse).
	if _, ok := m.(domain.PostResponseHook); ok {
		t.Error("guided_decomposition implements PostResponseHook already (item 4's half)")
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
