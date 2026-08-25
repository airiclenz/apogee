package agent

// The engine doors on the Agent itself. Today that is the Thinking-effort override
// (SetEffortOverride / ThinkingEffort): what the door does to the session, what it deliberately
// does NOT do to a delegated child, and how it composes with a model switch — plus the effort WIRE
// DIALECT the same switch carries in from the server (ADR 0060).

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// TestEffortOverrideLayersOverTheProfile is the door's own contract: the two layers are reported
// separately so a Driver can SHOW the layering (bare /effort), the override moves freely, and the
// zero value clears it rather than meaning a fifth level.
func TestEffortOverrideLayersOverTheProfile(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.Profile.Thinking.Effort = domain.EffortLow
	a, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	override, profile := a.ThinkingEffort()
	if override != "" || profile != domain.EffortLow {
		t.Fatalf("fresh session reports override %q / profile %q, want \"\" / %q", override, profile, domain.EffortLow)
	}
	if got := a.resolvedEffort(); got != domain.EffortLow {
		t.Errorf("resolved effort with no override = %q, want the profile's %q", got, domain.EffortLow)
	}

	a.SetEffortOverride(domain.EffortHigh)
	override, profile = a.ThinkingEffort()
	if override != domain.EffortHigh || profile != domain.EffortLow {
		t.Errorf("under the override: override %q / profile %q, want %q / %q", override, profile, domain.EffortHigh, domain.EffortLow)
	}
	if got := a.resolvedEffort(); got != domain.EffortHigh {
		t.Errorf("resolved effort under the override = %q, want %q", got, domain.EffortHigh)
	}

	a.SetEffortOverride("")
	if override, _ = a.ThinkingEffort(); override != "" {
		t.Errorf("override after clearing = %q, want the empty absence", override)
	}
	if got := a.resolvedEffort(); got != domain.EffortLow {
		t.Errorf("resolved effort after clearing = %q, want the profile's %q back", got, domain.EffortLow)
	}
}

// TestChildDoesNotInheritTheEffortOverride pins the override as PRIMARY-loop state (ADR 0050): a
// delegated child resolves effort from its own profile, so a user asking the session to think hard
// about the conversation does not silently re-price every sub-agent it spawns. It holds by
// construction — the override lives on the Agent, not on the Config a child is built from — and
// this is what makes a future move of it onto the Config fail loudly.
func TestChildDoesNotInheritTheEffortOverride(t *testing.T) {
	t.Parallel()

	cfg := configWithTools(&recordingSink{}, fakeTool{name: "w"})
	cfg.Profile.Thinking.Effort = domain.EffortLow
	parent, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	parent.SetEffortOverride(domain.EffortHigh)

	child, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}

	if override, _ := child.ThinkingEffort(); override != "" {
		t.Errorf("child override = %q, want none — the session override is the parent's alone", override)
	}
	if got := child.resolvedEffort(); got != domain.EffortLow {
		t.Errorf("child resolved effort = %q, want its own profile's %q", got, domain.EffortLow)
	}
	if got := parent.resolvedEffort(); got != domain.EffortHigh {
		t.Errorf("parent resolved effort = %q, want its override %q left standing", got, domain.EffortHigh)
	}
}

// TestEffortOverrideSurvivesARebind is the model-switch regression: the profile half re-resolves
// with the new model (the effort rides the profile through the existing door, no code of its own),
// while the SESSION override — an instruction about this conversation, not about a model — stays
// exactly where the user put it.
func TestEffortOverrideSurvivesARebind(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.Profile.Thinking.Effort = domain.EffortLow
	a, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.SetEffortOverride(domain.EffortHigh)

	if err := a.Rebind(RebindSpec{
		Model:            "another-model",
		SystemPrompt:     "you are bound to the new model",
		MaxContextTokens: 8192,
		Profile:          domain.ModelProfile{Thinking: domain.ThinkingProfile{Effort: domain.EffortMedium}},
	}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}

	override, profile := a.ThinkingEffort()
	if override != domain.EffortHigh {
		t.Errorf("override after the rebind = %q, want the session's %q intact", override, domain.EffortHigh)
	}
	if profile != domain.EffortMedium {
		t.Errorf("profile effort after the rebind = %q, want the new model's %q", profile, domain.EffortMedium)
	}
	if got := a.resolvedEffort(); got != domain.EffortHigh {
		t.Errorf("resolved effort after the rebind = %q, want the override %q still on top", got, domain.EffortHigh)
	}

	// Clearing now falls back to the NEW model's profile, not the departed one's.
	a.SetEffortOverride("")
	if got := a.resolvedEffort(); got != domain.EffortMedium {
		t.Errorf("resolved effort after clearing = %q, want the new profile's %q", got, domain.EffortMedium)
	}
}

// TestRebindCarriesTheEffortDialectOntoTheRequest is the server half of the effort seam: the
// dialect is not a per-model binding but a fact about the server the session is on, and it reaches
// the engine through the one channel a server fact has — the RebindSpec (ADR 0060). What the next
// request carries is what the last spec stated.
func TestRebindCarriesTheEffortDialectOntoTheRequest(t *testing.T) {
	t.Parallel()

	a, err := newAgent(baseConfig(&recordingSink{}), &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	// The anchor first: a session nobody has dialled asks for no dialect at all, so the request is
	// byte-identical to the pre-dialect loop (ADR 0031).
	if got := a.toProviderRequest(effortTestRequest()).EffortDialect; got != provider.EffortDialectNone {
		t.Fatalf("dialect before any rebind = %q, want the zero anchor", got)
	}

	if err := a.Rebind(RebindSpec{
		Model:            "another-model",
		MaxContextTokens: 8192,
		EffortDialect:    provider.EffortDialectReasoning,
	}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	if got := a.toProviderRequest(effortTestRequest()).EffortDialect; got != provider.EffortDialectReasoning {
		t.Errorf("dialect after the rebind = %q, want the spec's %q", got, provider.EffortDialectReasoning)
	}

	// And a move onto a server that advertises no dial states the zero rather than staying silent:
	// keeping the departed server's shape would express this session's effort in a dialect the
	// server it is now on does not read.
	if err := a.Rebind(RebindSpec{Model: "third-model", MaxContextTokens: 8192}); err != nil {
		t.Fatalf("Rebind onto an undialled server: %v", err)
	}
	if got := a.toProviderRequest(effortTestRequest()).EffortDialect; got != provider.EffortDialectNone {
		t.Errorf("dialect after rebinding onto an undialled server = %q, want the zero", got)
	}
}
