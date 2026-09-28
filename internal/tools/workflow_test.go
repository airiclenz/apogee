package tools

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestWorkflow_IsADefaultOffPlaceholder pins the tool's standing: off the default menu, a known name
// a roster can lift, write-capable rather than read-only (stop and message steer running work), and
// an Execute that fails loudly because dispatch owns the call.
func TestWorkflow_IsADefaultOffPlaceholder(t *testing.T) {
	t.Parallel()

	tool := NewWorkflow()

	if !domain.IsDefaultOff(tool) {
		t.Error("workflow must declare itself default-off (ADR 0089 D1)")
	}
	if domain.IsReadOnly(tool) {
		t.Error("workflow must carry no read-only declaration: stop and message steer running work")
	}
	if !slices.Contains(KnownToolNames(), WorkflowToolName) {
		t.Error("KnownToolNames must list workflow, or `tools.enabled: [workflow]` is reported as a typo")
	}
	result, err := tool.Execute(context.Background(), domain.ToolCall{ID: "c1", Tool: WorkflowToolName})
	if err != nil || !result.IsError || result.CallID != "c1" {
		t.Errorf("Execute = (%+v, %v), want an error result for c1", result, err)
	}
}

// TestWorkflowSchema_PublishesTheThreeActions pins the schema to ADR 0089 D4: an action that is one
// of status, stop and message, the workflow id, the item to message and the text.
func TestWorkflowSchema_PublishesTheThreeActions(t *testing.T) {
	t.Parallel()

	var schema struct {
		Required   []string `json:"required"`
		Properties struct {
			Action struct {
				Enum []string `json:"enum"`
			} `json:"action"`
			ID   json.RawMessage `json:"id"`
			Item json.RawMessage `json:"item"`
			Text json.RawMessage `json:"text"`
		} `json:"properties"`
	}

	if err := json.Unmarshal(NewWorkflow().Schema(), &schema); err != nil {
		t.Fatalf("workflow schema is not valid JSON: %v", err)
	}

	want := []string{WorkflowActionStatus, WorkflowActionStop, WorkflowActionMessage}
	if !slices.Equal(schema.Properties.Action.Enum, want) {
		t.Errorf("action enum = %v, want %v", schema.Properties.Action.Enum, want)
	}
	if !slices.Equal(schema.Required, []string{"action"}) {
		t.Errorf("required = %v, want only action", schema.Required)
	}
	if schema.Properties.ID == nil || schema.Properties.Item == nil || schema.Properties.Text == nil {
		t.Error("the schema must publish id, item and text")
	}
	if got := domain.ArgKeysWithRole(NewWorkflow(), domain.ArgRolePrompt); !slices.Equal(got, []string{"text"}) {
		t.Errorf("prompt-role keys = %v, want the message text alone", got)
	}
}

// TestWorkflow_TravelsWithTheDriversBackgroundOptIn pins the pair ADR 0089 D1/D4 ties together: the
// workflow tool is offered, and fan_out publishes `background`, exactly where the Driver offers
// background workflows AND the roster lifts `workflow`. A Driver that does not — a headless run, a
// daemon firing, the engine's own default roster while the embedder leaves Config.OffersBackground
// unset — offers neither, whatever `tools.enabled:` names; an embedder that sets it gets both.
func TestWorkflow_TravelsWithTheDriversBackgroundOptIn(t *testing.T) {
	t.Parallel()

	lifted := []string{FanOutToolName, WorkflowToolName}
	cases := []struct {
		name string
		host HostTools
		want bool
	}{
		{name: "the TUI with workflow lifted", host: HostTools{Enabled: lifted, OffersBackground: true}, want: true},
		{name: "the TUI without workflow lifted", host: HostTools{Enabled: []string{FanOutToolName}, OffersBackground: true}},
		{name: "a firing with workflow lifted", host: HostTools{Enabled: lifted}},
		{name: "the engine's own roster", host: HostToolsOf(domain.Config{EnabledTools: lifted}, false, false)},
		{
			name: "the engine's roster with the embedder's opt-in",
			host: HostToolsOf(domain.Config{EnabledTools: lifted, OffersBackground: true}, false, true),
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			menu := DefaultToolsWithHost(t.TempDir(), tc.host)

			offered := slices.ContainsFunc(menu, func(tool domain.Tool) bool { return tool.Name() == WorkflowToolName })
			if offered != tc.want {
				t.Errorf("workflow offered = %v, want %v", offered, tc.want)
			}
			if _, got := fanOutProperties(t, fanOutOn(t, tc.host).Schema())["background"]; got != tc.want {
				t.Errorf("fan_out publishes background = %v, want %v", got, tc.want)
			}
		})
	}
}
