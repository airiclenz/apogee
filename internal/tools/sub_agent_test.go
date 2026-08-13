package tools

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// Compile-time proof sub_agent declares its prompt-carrying arguments (domain.PromptTool).
// The guard's exemption for delegated prose hangs off this declaration: lose it and a task
// that merely names a guarded path is hard-refused again.
var _ domain.PromptTool = (*SubAgent)(nil)

// TestSubAgentDescriptionInvitesConcurrentDelegations guards the one sentence that tells the
// model it may fan out: the ADR 0039 concurrent dispatch is only ever exercised when the model
// emits several sub_agent calls in ONE reply, and the description is the only place it learns
// that it may. A live run showed models dispatching one call per turn without it.
func TestSubAgentDescriptionInvitesConcurrentDelegations(t *testing.T) {
	t.Parallel()

	desc := NewSubAgent().Description()
	for _, want := range []string{"several times in a single reply", "concurrently"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description does not invite concurrent same-reply delegations (missing %q): %q", want, desc)
		}
	}
}

// TestSubAgentSchemaOffersAnOptionalName pins the model-facing contract for the delegation
// name: the published schema advertises a string `name` property, and `task` stays the ONLY
// required argument. A model that never names a delegation must keep making valid calls — every
// display falls back to the task's first line — so a `name` that crept into `required` would
// break every existing caller.
func TestSubAgentSchemaOffersAnOptionalName(t *testing.T) {
	t.Parallel()

	var schema struct {
		Required   []string                  `json:"required"`
		Properties map[string]map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(NewSubAgent().Schema(), &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	prop, ok := schema.Properties["name"]
	if !ok {
		t.Fatal("schema is missing the name property")
	}
	if prop["type"] != "string" {
		t.Errorf("name type = %v, want string", prop["type"])
	}
	if desc, _ := prop["description"].(string); desc == "" {
		t.Error("the name property carries no description — the model cannot tell what to put there")
	}
	if _, ok := schema.Properties["task"]; !ok {
		t.Error("schema lost the task property")
	}
	if len(schema.Required) != 1 || schema.Required[0] != "task" {
		t.Errorf("required = %v, want [task] — the name must stay optional", schema.Required)
	}
}

// TestSubAgentDeclaresBothArgumentsAsDelegationPrompts pins WHICH arguments the guard is
// allowed to look away from. Both of sub_agent's arguments are prose for the child — the task
// and the display name — so both are declared; a third argument added later is inspected by
// default until it is deliberately listed here.
func TestSubAgentDeclaresBothArgumentsAsDelegationPrompts(t *testing.T) {
	t.Parallel()

	got := domain.PromptArgKeys(NewSubAgent())

	if want := []string{"task", "name"}; !slices.Equal(got, want) {
		t.Errorf("PromptArgKeys = %v, want %v", got, want)
	}
}

// TestPromptArgKeysAreNoneForAToolThatDeclaresNone holds the safe default at the helper: a
// tool that makes no declaration exempts nothing. terminal is the case that matters — its
// `command` is prose-shaped text this host DOES execute, so every byte of it stays inspected.
func TestPromptArgKeysAreNoneForAToolThatDeclaresNone(t *testing.T) {
	t.Parallel()

	got := domain.PromptArgKeys(NewTerminal(t.TempDir()))

	if got != nil {
		t.Errorf("terminal declares prompt keys %v, want none — its command text is acted on", got)
	}
}

// TestSubAgentArgsParsesTheOptionalName proves the exported argument shape the dispatch layer
// unmarshals into actually carries the name across the JSON boundary, and that a call omitting
// it yields the empty string rather than failing to parse.
func TestSubAgentArgsParsesTheOptionalName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		payload  string
		wantTask string
		wantName string
	}{
		{"named", `{"task":"summarise the repo","name":"repo-scout"}`, "summarise the repo", "repo-scout"},
		{"unnamed", `{"task":"summarise the repo"}`, "summarise the repo", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var args SubAgentArgs
			if err := json.Unmarshal([]byte(tc.payload), &args); err != nil {
				t.Fatalf("unmarshal %s: %v", tc.payload, err)
			}
			if args.Task != tc.wantTask {
				t.Errorf("Task = %q, want %q", args.Task, tc.wantTask)
			}
			if args.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", args.Name, tc.wantName)
			}
		})
	}
}
