package tools

import (
	"context"
	"slices"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// foreignTool is a tool this package did not build, registered under whatever name a test gives
// it — the shape a foreign `sub_agent` or `fan_out` (an MCP tool, a test double) takes in a roster.
type foreignTool struct{ toolSpec }

func (*foreignTool) Execute(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	return domain.ToolResult{CallID: call.ID}, nil
}

func newForeignTool(name string) *foreignTool {
	return &foreignTool{toolSpec{name: name, description: "foreign", schema: []byte(`{"type":"object"}`)}}
}

// everySubAgentOptions lists every sub_agent variant: each combination of the three axes.
func everySubAgentOptions() []SubAgentOptions {
	var all []SubAgentOptions
	for _, seat := range []bool{false, true} {
		for _, background := range []bool{false, true} {
			for _, sharedContext := range []bool{false, true} {
				all = append(all, SubAgentOptions{SeatChoice: seat, Background: background, Context: sharedContext})
			}
		}
	}
	return all
}

// everyFanOutOptions lists every fan_out variant: each combination of its two axes.
func everyFanOutOptions() []FanOutOptions {
	var all []FanOutOptions
	for _, seat := range []bool{false, true} {
		for _, background := range []bool{false, true} {
			all = append(all, FanOutOptions{SeatChoice: seat, Background: background})
		}
	}
	return all
}

// everyOffer lists every delegation offer a roster built here can publish.
func everyOffer() []DelegationOffer {
	var all []DelegationOffer
	for _, subAgent := range everySubAgentOptions() {
		for _, fanOut := range everyFanOutOptions() {
			all = append(all, DelegationOffer{SubAgent: subAgent, FanOut: fanOut})
		}
	}
	return all
}

// registryOf registers tools in the order given, failing the test on a registration error.
func registryOf(t *testing.T, tools ...domain.Tool) *domain.ToolRegistry {
	t.Helper()
	reg := domain.NewToolRegistry()
	for _, tool := range tools {
		if err := reg.Register(tool); err != nil {
			t.Fatalf("Register(%q): %v", tool.Name(), err)
		}
	}
	return reg
}

// menuNames lists a registry's tool names in menu order.
func menuNames(reg *domain.ToolRegistry) []string {
	var names []string
	for _, tool := range reg.All() {
		names = append(names, tool.Name())
	}
	return names
}

// TestOfferOf_NilMissingForeignAndEveryVariant pins what OfferOf reads: the zero value for a nil
// roster, a roster without either tool and foreign tools under both names, and otherwise exactly
// the options each of the two tools was built with.
func TestOfferOf_NilMissingForeignAndEveryVariant(t *testing.T) {
	t.Parallel()

	zeroCases := []struct {
		name   string
		roster *domain.ToolRegistry
	}{
		{name: "nil roster", roster: nil},
		{name: "empty roster", roster: registryOf(t)},
		{name: "neither tool", roster: registryOf(t, newForeignTool("read_file"))},
		{
			name:   "foreign tools under both names",
			roster: registryOf(t, newForeignTool(SubAgentToolName), newForeignTool(FanOutToolName)),
		},
	}
	for _, tc := range zeroCases {
		if got := OfferOf(tc.roster); got != (DelegationOffer{}) {
			t.Errorf("%s: OfferOf = %+v, want the zero offer", tc.name, got)
		}
	}

	for _, want := range everyOffer() {
		roster := registryOf(t, NewSubAgentWith(want.SubAgent), NewFanOutWith(want.FanOut))
		if got := OfferOf(roster); got != want {
			t.Errorf("OfferOf = %+v, want %+v", got, want)
		}
		subAgentOnly := registryOf(t, NewSubAgentWith(want.SubAgent), newForeignTool(FanOutToolName))
		if got := OfferOf(subAgentOnly); got != (DelegationOffer{SubAgent: want.SubAgent}) {
			t.Errorf("foreign fan_out: OfferOf = %+v, want sub_agent's half only (%+v)", got, want.SubAgent)
		}
		fanOutOnly := registryOf(t, NewFanOutWith(want.FanOut))
		if got := OfferOf(fanOutOnly); got != (DelegationOffer{FanOut: want.FanOut}) {
			t.Errorf("missing sub_agent: OfferOf = %+v, want fan_out's half only (%+v)", got, want.FanOut)
		}
	}
}

// TestDelegationOfferChild_NeverPublishesSeatOrBackgroundAndKeepsContext pins the child rule over
// every offer: no seat choice and no background on either tool (ADR 0069 D3, ADR 0089 D1, ADR 0094
// D1), sub_agent's `context` exactly as the parent published it, and idempotence.
func TestDelegationOfferChild_NeverPublishesSeatOrBackgroundAndKeepsContext(t *testing.T) {
	t.Parallel()

	for _, offer := range everyOffer() {
		child := offer.Child()
		want := DelegationOffer{SubAgent: SubAgentOptions{Context: offer.SubAgent.Context}}
		if child != want {
			t.Errorf("(%+v).Child() = %+v, want %+v", offer, child, want)
		}
		if again := child.Child(); again != child {
			t.Errorf("(%+v).Child().Child() = %+v, want %+v — Child must be idempotent", offer, again, child)
		}
	}
}

// TestChildRoster_ReturnsTheSameRegistryWhenNothingIsStripped pins pointer identity: a roster
// whose offer is already its own child — nil, toolless, plain, context-only, or foreign under
// either name — comes back as the very registry that went in.
func TestChildRoster_ReturnsTheSameRegistryWhenNothingIsStripped(t *testing.T) {
	t.Parallel()

	if got := ChildRoster(nil); got != nil {
		t.Errorf("ChildRoster(nil) = %v, want nil", got)
	}
	cases := []struct {
		name   string
		roster *domain.ToolRegistry
	}{
		{name: "empty", roster: registryOf(t)},
		{name: "no delegation tools", roster: registryOf(t, newForeignTool("read_file"))},
		{name: "plain sub_agent and fan_out", roster: registryOf(t, NewSubAgent(), NewFanOut())},
		{
			name:   "context-only sub_agent",
			roster: registryOf(t, NewSubAgentWith(SubAgentOptions{Context: true}), NewFanOut()),
		},
		{name: "foreign sub_agent alone", roster: registryOf(t, newForeignTool(SubAgentToolName))},
		{
			name:   "foreign sub_agent and fan_out",
			roster: registryOf(t, newForeignTool(SubAgentToolName), newForeignTool(FanOutToolName)),
		},
	}
	for _, tc := range cases {
		if got := ChildRoster(tc.roster); got != tc.roster {
			t.Errorf("%s: ChildRoster returned a new registry; want the same pointer", tc.name)
		}
	}
}

// TestChildRoster_ReplacesAForeignSubAgent pins the swap of a foreign `sub_agent` for the child's
// own variant. A foreign sub_agent alone publishes nothing to strip, so it is paired with a
// choice-publishing fan_out; both delegation tools come back as the plain variants, every other
// tool arrives verbatim, and menu order is kept.
func TestChildRoster_ReplacesAForeignSubAgent(t *testing.T) {
	t.Parallel()

	before := newForeignTool("read_file")
	after := newForeignTool("write_file")
	roster := registryOf(t,
		before,
		newForeignTool(SubAgentToolName),
		NewFanOutWith(FanOutOptions{SeatChoice: true}),
		after,
	)

	got := ChildRoster(roster)
	if got == roster {
		t.Fatal("ChildRoster returned the same registry; want the fan_out choice stripped")
	}
	wantNames := []string{"read_file", SubAgentToolName, FanOutToolName, "write_file"}
	if names := menuNames(got); !slices.Equal(names, wantNames) {
		t.Fatalf("menu = %v, want %v", names, wantNames)
	}
	tool, _ := got.Lookup(SubAgentToolName)
	if spawner, ok := tool.(*SubAgent); !ok || spawner.opts != (SubAgentOptions{}) {
		t.Errorf("sub_agent = %T %+v, want the plain *SubAgent", tool, tool)
	}
	if offer := OfferOf(got); offer != (DelegationOffer{}) {
		t.Errorf("OfferOf(child roster) = %+v, want the zero offer", offer)
	}
	if tool, _ := got.Lookup("read_file"); tool != before {
		t.Error("read_file was replaced; want it verbatim")
	}
	if tool, _ := got.Lookup("write_file"); tool != after {
		t.Error("write_file was replaced; want it verbatim")
	}
}

// TestChildRoster_NarrowsEveryOfferToItsChild runs ChildRoster over every offer: the result
// publishes exactly the parent's Child, and a foreign fan_out beside a choice-publishing sub_agent
// keeps its value rather than becoming the plain variant.
func TestChildRoster_NarrowsEveryOfferToItsChild(t *testing.T) {
	t.Parallel()

	for _, offer := range everyOffer() {
		roster := registryOf(t, NewSubAgentWith(offer.SubAgent), NewFanOutWith(offer.FanOut))
		if got := OfferOf(ChildRoster(roster)); got != offer.Child() {
			t.Errorf("OfferOf(ChildRoster(%+v)) = %+v, want %+v", offer, got, offer.Child())
		}
	}

	foreignFanOut := newForeignTool(FanOutToolName)
	roster := registryOf(t, NewSubAgentWith(SubAgentOptions{SeatChoice: true}), foreignFanOut)
	got := ChildRoster(roster)
	if got == roster {
		t.Fatal("ChildRoster returned the same registry; want the sub_agent seat choice stripped")
	}
	if tool, _ := got.Lookup(FanOutToolName); tool != foreignFanOut {
		t.Errorf("fan_out = %T, want the foreign tool verbatim", tool)
	}
}
