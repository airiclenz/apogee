package agent

// White-box test for the domain→provider wire projection. It lives in package agent so it
// can call the unexported toProviderRequest directly with a hand-built domain.Request —
// the projection is a pure mapping, so no Step, no fake responder, and no Upstream is needed.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// TestProviderRequestOmitsInterjected pins the marker as process-local: an interjected user
// message projects onto the wire as an ordinary user message. The projection maps fields
// explicitly (wire.go), so the guarantee holds by construction — this test is what makes a
// future "just copy the struct" refactor fail loudly instead of leaking an Apogee-owned flag
// into a provider request.
func TestProviderRequestOmitsInterjected(t *testing.T) {
	t.Parallel()

	a := &Agent{cfg: domain.Config{Model: "test-model"}}
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "the ask"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file"}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: "contents"},
		{Role: domain.RoleUser, Content: "also check the tests", Interjected: true},
	}
	req := domain.NewRequest("test-model", msgs, nil, domain.Budget{}, 0, nil)

	got := a.toProviderRequest(req)

	want := []provider.Message{
		{Role: "user", Content: "the ask"},
		{Role: "assistant", ToolCalls: []provider.ToolCall{{
			ID:       "c1",
			Type:     "function",
			Function: provider.FunctionCall{Name: "read_file"},
		}}},
		{Role: "tool", ToolCallID: "c1", Content: "contents"},
		// The interjection: same shape as any other user message, no marker.
		{Role: "user", Content: "also check the tests"},
	}
	if !reflect.DeepEqual(got.Messages, want) {
		t.Errorf("projected messages = %+v, want %+v", got.Messages, want)
	}

	// The wire type has no carrier for the marker at all.
	for i := 0; i < reflect.TypeOf(provider.Message{}).NumField(); i++ {
		if name := reflect.TypeOf(provider.Message{}).Field(i).Name; strings.Contains(strings.ToLower(name), "interject") {
			t.Errorf("provider.Message grew field %q — the marker must never reach the wire", name)
		}
	}

	// Belt and braces: no trace of the marker anywhere in the serialized request.
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal provider request: %v", err)
	}
	if strings.Contains(strings.ToLower(string(encoded)), "interject") {
		t.Errorf("serialized provider request mentions the marker: %s", encoded)
	}
}

// TestProviderRequestCarriesResolvedEffort pins the resolution the projection applies — session
// override ▸ profile ▸ nothing (ADR 0050) — including the anchor at its foot: a session with
// neither asks for no effort at all, so the request stays byte-identical to the pre-effort loop. It
// covers toProviderEffort's whole widened vocabulary too (ADR 0060), the level names being all this
// mapping has to say.
func TestProviderRequestCarriesResolvedEffort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		profile  domain.ThinkingEffort
		override domain.ThinkingEffort
		want     provider.Effort
	}{
		{name: "neither: the anchor", want: ""},
		{name: "profile alone", profile: domain.EffortLow, want: provider.EffortLow},
		{name: "override alone", override: domain.EffortMedium, want: provider.EffortMedium},
		{name: "override beats profile", profile: domain.EffortLow, override: domain.EffortHigh, want: provider.EffortHigh},
		{name: "override off beats a thinking profile", profile: domain.EffortHigh, override: domain.EffortOff, want: provider.EffortOff},
		// The four levels ADR 0060 added to the vocabulary: the mapping is one conversion across the
		// whole eight-name union, so a widened level reaches the wire instead of degrading to "".
		{name: "widened: none", profile: domain.EffortNone, want: provider.EffortNone},
		{name: "widened: minimal", profile: domain.EffortMinimal, want: provider.EffortMinimal},
		{name: "widened: xhigh", profile: domain.EffortXHigh, want: provider.EffortXHigh},
		{name: "widened: max", override: domain.EffortMax, want: provider.EffortMax},
		// And the totality guard the widening must not lose: a level outside the union emits
		// nothing, so a value that slipped past the config loader's enum degrades to the model's own
		// template default rather than to a template error mid-Turn.
		{name: "unknown level emits nothing", profile: domain.ThinkingEffort("extreme"), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := &Agent{cfg: domain.Config{Model: "test-model"}}
			a.cfg.Profile.Thinking.Effort = tt.profile
			a.SetEffortOverride(tt.override)

			got := a.toProviderRequest(effortTestRequest())
			if got.ThinkingEffort != tt.want {
				t.Errorf("projected effort = %q, want %q", got.ThinkingEffort, tt.want)
			}
		})
	}
}

// TestClearedEffortOverrideFallsBackToTheProfile is the other half of the override contract: the
// zero value is not a fifth level but the removal of the layer, so /effort auto hands the next
// request straight back to the bound model's profile.
func TestClearedEffortOverrideFallsBackToTheProfile(t *testing.T) {
	t.Parallel()

	a := &Agent{cfg: domain.Config{Model: "test-model"}}
	a.cfg.Profile.Thinking.Effort = domain.EffortLow

	a.SetEffortOverride(domain.EffortHigh)
	if got := a.toProviderRequest(effortTestRequest()).ThinkingEffort; got != provider.EffortHigh {
		t.Fatalf("effort under the override = %q, want %q", got, provider.EffortHigh)
	}

	a.SetEffortOverride("")
	if got := a.toProviderRequest(effortTestRequest()).ThinkingEffort; got != provider.EffortLow {
		t.Errorf("effort after clearing the override = %q, want the profile's %q", got, provider.EffortLow)
	}
}

// TestEffortHoldsUnderBypass pins the engine stance: effort is CONFIGURATION, not a Mechanism, so
// Bypass — Mechanisms off, structure on — leaves the emitted effort exactly where it was. Bypass is
// the floor the Mechanisms must beat, and a floor run that silently stopped thinking would not be
// the same agent minus the Mechanisms.
func TestEffortHoldsUnderBypass(t *testing.T) {
	t.Parallel()

	a := &Agent{cfg: domain.Config{Model: "test-model"}}
	a.cfg.Profile.Thinking.Effort = domain.EffortMedium
	a.SetEffortOverride(domain.EffortHigh)

	before := a.toProviderRequest(effortTestRequest()).ThinkingEffort
	a.SetBypass(true)
	after := a.toProviderRequest(effortTestRequest()).ThinkingEffort

	if before != provider.EffortHigh || after != before {
		t.Errorf("effort before/after Bypass = %q/%q, want %q both times", before, after, provider.EffortHigh)
	}
}

// effortTestRequest builds the minimal domain.Request the effort projection tests hand in: the
// resolution reads nothing off the request itself, so one user message is enough.
func effortTestRequest() *domain.Request {
	msgs := []domain.Message{{Role: domain.RoleUser, Content: "the ask"}}
	return domain.NewRequest("test-model", msgs, nil, domain.Budget{}, 0, nil)
}
