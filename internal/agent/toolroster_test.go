package agent

// The tool ROSTER as a per-model binding (ADR 0057, plan item 5). Which tools a model is offered is
// a fact about the model — a small model drowns in a menu a big one wants — so the roster is the
// Model profile's third axis and rides the same swap the other two do: construction composes it,
// Rebind re-composes it, and the one applyProfile both doors run is where that happens.
//
// These tests own the boundary of that binding as much as its effect: the engine re-composes only
// the set it ASSEMBLED, so an injected Config.Tools and a set installed through SwapTools are the
// host's authority verbatim and no model switch touches them.

import (
	"reflect"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// rosterAgent builds an Agent whose tool set the ENGINE composes: a workspace, no injected
// Config.Tools, and whatever roster lists cfg already carries.
func rosterAgent(t *testing.T, cfg domain.Config) *Agent {
	t.Helper()
	cfg.WorkspaceDir = t.TempDir()
	a, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a
}

// offers reports whether the Agent's live set holds a tool by that name.
func offers(a *Agent, name string) bool {
	if a.tools == nil {
		return false
	}
	_, found := a.tools.Lookup(name)
	return found
}

// TestRebindComposesTheNewModelsRoster is the item's core acceptance: a switch to a model whose
// profile spells roster deltas moves the tool set with the rest of the per-model bindings, and a
// switch BACK to a model that spells none restores the menu rather than leaving the departed
// model's pruning in force — the zero roster axis is a meaningful value, exactly as the zero
// thinking axis is.
func TestRebindComposesTheNewModelsRoster(t *testing.T) {
	t.Parallel()

	a := rosterAgent(t, baseConfig(&recordingSink{}))
	if !offers(a, "view_diff") || !offers(a, "read_file") {
		t.Fatalf("the default set is missing a tool before any roster is applied: view_diff=%v read_file=%v",
			offers(a, "view_diff"), offers(a, "read_file"))
	}

	pruned := domain.ModelProfile{Tools: domain.ToolRosterDelta{Disabled: []string{"view_diff"}}}
	if err := a.Rebind(RebindSpec{Model: "small-4b", Profile: pruned}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	if offers(a, "view_diff") {
		t.Error("view_diff survived a rebind to a model whose profile disables it")
	}
	if !offers(a, "read_file") {
		t.Error("the roster axis pruned a tool it does not name")
	}

	if err := a.Rebind(RebindSpec{Model: "big-70b"}); err != nil {
		t.Fatalf("Rebind back: %v", err)
	}
	if !offers(a, "view_diff") {
		t.Error("the departed model's roster is still in force after a rebind to an unprofiled model")
	}
}

// TestRebindLeavesAnInjectedToolSetAlone pins the stated bound of the whole axis: an injected
// Config.Tools is the host's own assembly, taken exactly as given (ADR 0001), so the roster ladder
// never reaches it — not at CONSTRUCTION, where global lists and the profile axis both name the
// injected tool and neither subtracts it, and not at a SWITCH, where the very same registry object
// is still the session's set afterwards.
func TestRebindLeavesAnInjectedToolSetAlone(t *testing.T) {
	t.Parallel()

	cfg := configWithTools(&recordingSink{}, fakeTool{name: "host_tool", readOnly: true})
	cfg.WorkspaceDir = t.TempDir()
	// Both configuration rungs of the ladder point at the injected tool. Neither may be honoured:
	// the host built this set, so the host's word on it is the set itself.
	cfg.DisabledTools = []string{"host_tool"}
	cfg.Profile = domain.ModelProfile{Tools: domain.ToolRosterDelta{Disabled: []string{"host_tool"}}}

	injected := cfg.Tools
	a, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if a.tools != injected {
		t.Fatal("construction replaced the injected Config.Tools with a composed set")
	}
	if !offers(a, "host_tool") {
		t.Error("the roster ladder subtracted from an injected Config.Tools at construction")
	}
	if a.ownsToolSet {
		t.Error("the engine claims ownership of a tool set the host injected")
	}

	if err := a.Rebind(RebindSpec{
		Model:   "small-4b",
		Profile: domain.ModelProfile{Tools: domain.ToolRosterDelta{Disabled: []string{"host_tool"}}},
	}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	if a.tools != injected {
		t.Error("a rebind rebuilt the tool set under an injected Config.Tools")
	}
	if !offers(a, "host_tool") {
		t.Error("a rebind applied the profile's roster axis to an injected Config.Tools")
	}
}

// TestSwapToolsEndsTheEnginesRosterComposition covers the other half of that boundary, the one a
// live TUI session actually runs on: once the host has handed a whole registry over (ADR 0037
// binding F — host tools plus the folded MCP tools), a later model switch must not rebuild the
// built-in menu over the top of it. The swapped set stands, MCP stand-in and all.
func TestSwapToolsEndsTheEnginesRosterComposition(t *testing.T) {
	t.Parallel()

	a := rosterAgent(t, baseConfig(&recordingSink{}))
	if !a.ownsToolSet {
		t.Fatal("the engine did not claim the set it composed itself")
	}

	swapped := domain.NewToolRegistry()
	if err := swapped.Register(fakeTool{name: "mcp__srv__probe", readOnly: true}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := a.SwapTools(swapped); err != nil {
		t.Fatalf("SwapTools: %v", err)
	}

	if err := a.Rebind(RebindSpec{
		Model:   "small-4b",
		Profile: domain.ModelProfile{Tools: domain.ToolRosterDelta{Disabled: []string{"view_diff"}}},
	}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	if a.tools != swapped {
		t.Fatal("a rebind rebuilt the default menu over a set the host swapped in")
	}
	if !offers(a, "mcp__srv__probe") {
		t.Error("the host's swapped-in tool is gone after a model switch")
	}
	if offers(a, "read_file") {
		t.Error("a rebind put the built-in menu back on top of the host's set")
	}
}

// TestConstructionComposesTheRosterLadder pins the sentence the switch rests on — startup and
// switch compose the same way — at the startup end: the engine's own assembly reads BOTH global
// lists and the profile axis off Config, and the profile has the last word per tool in either
// direction. Without it a session would start on one roster and switch to another rule.
func TestConstructionComposesTheRosterLadder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		disabled []string
		enabled  []string
		profile  domain.ToolRosterDelta
		want     bool
	}{
		{
			name: "nothing named leaves the build's own menu",
			want: true,
		},
		{
			name:     "the global list alone takes a tool off",
			disabled: []string{"view_diff"},
			want:     false,
		},
		{
			name:     "the profile puts back what the global list dropped",
			disabled: []string{"view_diff"},
			profile:  domain.ToolRosterDelta{Enabled: []string{"view_diff"}},
			want:     true,
		},
		{
			name:    "the profile takes off what the global list allows",
			enabled: []string{"view_diff"},
			profile: domain.ToolRosterDelta{Disabled: []string{"view_diff"}},
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := baseConfig(&recordingSink{})
			cfg.DisabledTools = tc.disabled
			cfg.EnabledTools = tc.enabled
			cfg.Profile = domain.ModelProfile{Tools: tc.profile}

			if got := offers(rosterAgent(t, cfg), "view_diff"); got != tc.want {
				t.Errorf("the composed set offers view_diff = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSubAgentRosterIsItsOwnModels pins the delegation half by CONSTRUCTION rather than by new
// code. A routed sub-agent runs another model, so its Config carries that model's profile — the
// roster axis with it — and never the orchestrator's; that is what "a sub-agent resolves its own
// model's roster" means here.
//
// The second half is the ceiling ADR 0005 puts on it: a child's tool set is the PARENT's set
// narrowed (defaultSubAgentTools hands it over as an injected Config.Tools), so the child's own
// axis is not the engine's to apply there and an `enabled:` entry can never hand a sub-agent a
// privilege its parent lacks. The axis prunes for the child's model where its own set is composed;
// it can never expand.
func TestSubAgentRosterIsItsOwnModels(t *testing.T) {
	t.Parallel()

	parent := rosterAgent(t, baseConfig(&recordingSink{}))
	target := routedTarget()
	target.Profile.Tools = domain.ToolRosterDelta{
		Disabled: []string{"view_diff"},
		Enabled:  []string{"a_tool_the_parent_does_not_have"},
	}
	parent.SetDelegationTarget(target)

	child := spawn(t, parent)

	if !reflect.DeepEqual(child.cfg.Profile.Tools, target.Profile.Tools) {
		t.Errorf("routed child roster axis = %+v, want the target model's %+v",
			child.cfg.Profile.Tools, target.Profile.Tools)
	}
	if !reflect.DeepEqual(parent.cfg.Profile.Tools, domain.ToolRosterDelta{}) {
		t.Errorf("the child's roster axis leaked onto the parent: %+v", parent.cfg.Profile.Tools)
	}
	// ADR 0005: the child's set is the parent's, narrowed by construction — so the axis it carries
	// cannot expand it, whatever the target's `enabled:` list names.
	if child.ownsToolSet {
		t.Error("the engine claims ownership of a sub-agent's inherited set, which ADR 0005 narrows")
	}
	if offers(child, "a_tool_the_parent_does_not_have") {
		t.Error("a routed child's roster axis handed it a tool its parent never had")
	}
	if !offers(child, "read_file") {
		t.Error("the routed child lost the parent's set it is meant to inherit")
	}
}
