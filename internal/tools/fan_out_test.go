package tools

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// fanOutProperties decodes a fan_out schema's property table, failing the test when the schema is
// not the JSON object a wire would carry.
func fanOutProperties(t *testing.T, schema json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var decoded struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("fan_out schema is not valid JSON: %v\n%s", err, schema)
	}
	if decoded.Type != "object" {
		t.Fatalf("fan_out schema type = %q, want object", decoded.Type)
	}
	return decoded.Properties
}

// fanOutOn returns the fan_out tool a registry assembled from host offers, failing when none does.
func fanOutOn(t *testing.T, host HostTools) *FanOut {
	t.Helper()
	for _, tool := range DefaultToolsWithHost(t.TempDir(), host) {
		if fanOut, ok := tool.(*FanOut); ok {
			return fanOut
		}
	}
	t.Fatalf("no fan_out on the menu for host %+v", host)
	return nil
}

// TestFanOutSchema_PublishesTheADRFields pins the plain variant's argument set to ADR 0087 D1/D2:
// the brief, the items and their batching, the shared context, the receipt fields, the per-item
// output, one verify and one merge, the tool narrowing and the recipe start — and neither `run_on`
// nor `background`, which only a gate publishes.
func TestFanOutSchema_PublishesTheADRFields(t *testing.T) {
	t.Parallel()

	properties := fanOutProperties(t, NewFanOut().Schema())
	for _, name := range []string{
		"task", "over", "batch", "context", "returns", "out", "verify", "merge", "tools", "recipe", "inputs",
	} {
		if _, ok := properties[name]; !ok {
			t.Errorf("fan_out schema is missing %q", name)
		}
	}
	for _, gated := range []string{"run_on", "background"} {
		if _, ok := properties[gated]; ok {
			t.Errorf("the plain fan_out schema publishes %q, which only its gate may add", gated)
		}
	}
	if got := len(properties); got != 11 {
		t.Errorf("fan_out schema publishes %d properties, want 11", got)
	}
}

// TestFanOutSchema_EachGateAddsOnlyItsProperty pins the two variants: each option adds exactly its
// one property, and every shared property stays byte-identical to the plain variant's.
func TestFanOutSchema_EachGateAddsOnlyItsProperty(t *testing.T) {
	t.Parallel()

	plain := fanOutProperties(t, NewFanOut().Schema())
	cases := []struct {
		name  string
		opts  FanOutOptions
		added []string
	}{
		{name: "seat choice", opts: FanOutOptions{SeatChoice: true}, added: []string{"run_on"}},
		{name: "background", opts: FanOutOptions{Background: true}, added: []string{"background"}},
		{name: "both", opts: FanOutOptions{SeatChoice: true, Background: true}, added: []string{"background", "run_on"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tool := NewFanOutWith(tc.opts)
			properties := fanOutProperties(t, tool.Schema())
			var added []string
			for name, body := range properties {
				base, shared := plain[name]
				if !shared {
					added = append(added, name)
					continue
				}
				if string(base) != string(body) {
					t.Errorf("shared property %q differs from the plain variant's", name)
				}
			}
			slices.Sort(added)
			if !slices.Equal(added, tc.added) {
				t.Errorf("added properties = %v, want %v", added, tc.added)
			}
			if tool.OffersSeatChoice() != tc.opts.SeatChoice || tool.OffersBackground() != tc.opts.Background {
				t.Errorf("Offers* = (%v, %v), want (%v, %v)",
					tool.OffersSeatChoice(), tool.OffersBackground(), tc.opts.SeatChoice, tc.opts.Background)
			}
		})
	}
}

// TestFanOutSchema_RunOnMatchesSubAgent pins `run_on` to sub_agent's spelling: the two tools ask
// the same seat question (ADR 0087 D9), so the engine resolves both against one vocabulary.
func TestFanOutSchema_RunOnMatchesSubAgent(t *testing.T) {
	t.Parallel()

	type enumOnly struct {
		Enum []string `json:"enum"`
	}
	var fanOut, subAgent enumOnly
	if err := json.Unmarshal(fanOutProperties(t, NewFanOutWith(FanOutOptions{SeatChoice: true}).Schema())["run_on"], &fanOut); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fanOutProperties(t, NewSubAgentWith(SubAgentOptions{SeatChoice: true}).Schema())["run_on"], &subAgent); err != nil {
		t.Fatal(err)
	}
	if want := []string{RunOnSession, RunOnSubAgentsServer}; !slices.Equal(fanOut.Enum, want) || !slices.Equal(subAgent.Enum, want) {
		t.Errorf("run_on enums: fan_out %v, sub_agent %v, want both %v", fanOut.Enum, subAgent.Enum, want)
	}
}

// TestFanOut_IsADefaultOffPlaceholder pins the tool's standing: off the default menu, a known name a
// roster can lift, and an Execute that fails loudly because dispatch owns the call.
func TestFanOut_IsADefaultOffPlaceholder(t *testing.T) {
	t.Parallel()

	tool := NewFanOut()
	if !domain.IsDefaultOff(tool) {
		t.Error("fan_out must declare itself default-off (ADR 0087 D8)")
	}
	if domain.IsReadOnly(tool) {
		t.Error("fan_out must carry no read-only declaration: its helpers' calls are classified one level down")
	}
	if !slices.Contains(KnownToolNames(), FanOutToolName) {
		t.Error("KnownToolNames must list fan_out, or `tools.enabled: [fan_out]` is reported as a typo")
	}
	for _, offered := range DefaultTools(t.TempDir()) {
		if offered.Name() == FanOutToolName {
			t.Fatal("fan_out reached the default menu with nothing lifting it")
		}
	}

	result, err := tool.Execute(context.Background(), domain.ToolCall{ID: "c1", Tool: FanOutToolName})
	if err != nil || !result.IsError || result.CallID != "c1" {
		t.Errorf("Execute = (%+v, %v), want an error result for c1", result, err)
	}
}

// TestFanOut_RegistryGates pins where the assembly reads each gate: `run_on` off the seat-choice
// flag, `background` off the Driver's opt-in AND the roster ladder's verdict for the workflow tool —
// global and profile rungs alike, the profile having the last word. Without the opt-in no roster
// publishes it (ADR 0089 D1: a headless run and a daemon firing offer no background).
func TestFanOut_RegistryGates(t *testing.T) {
	t.Parallel()

	lift := []string{FanOutToolName}
	cases := []struct {
		name           string
		host           HostTools
		wantRunOn      bool
		wantBackground bool
	}{
		{name: "lifted alone", host: HostTools{Enabled: lift, OffersBackground: true}},
		{name: "seat choice", host: HostTools{Enabled: lift, SubAgentSeatChoice: true}, wantRunOn: true},
		{
			name:           "workflow on the global roster",
			host:           HostTools{Enabled: []string{FanOutToolName, WorkflowToolName}, OffersBackground: true},
			wantBackground: true,
		},
		{
			name: "workflow on the profile roster",
			host: HostTools{
				Enabled:          lift,
				ProfileRoster:    domain.ToolRosterDelta{Enabled: []string{WorkflowToolName}},
				OffersBackground: true,
			},
			wantBackground: true,
		},
		{
			name: "the profile keeps off what the global rung lifted",
			host: HostTools{
				Enabled:          []string{FanOutToolName, WorkflowToolName},
				ProfileRoster:    domain.ToolRosterDelta{Disabled: []string{WorkflowToolName}},
				OffersBackground: true,
			},
		},
		{
			name: "a same-scope conflict fails closed",
			host: HostTools{
				Enabled:          []string{FanOutToolName, WorkflowToolName},
				Disabled:         []string{WorkflowToolName},
				OffersBackground: true,
			},
		},
		{
			name: "a Driver that offers no background publishes none whatever the roster says",
			host: HostTools{Enabled: []string{FanOutToolName, WorkflowToolName}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			properties := fanOutProperties(t, fanOutOn(t, tc.host).Schema())
			if _, got := properties["run_on"]; got != tc.wantRunOn {
				t.Errorf("run_on published = %v, want %v", got, tc.wantRunOn)
			}
			if _, got := properties["background"]; got != tc.wantBackground {
				t.Errorf("background published = %v, want %v", got, tc.wantBackground)
			}
		})
	}
}
