package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tasklist"
)

// taskListCall builds a task_list call from a raw tasks array, so a test can hand the tool
// exactly the JSON a model would — including shapes a typed helper would quietly fix up.
func taskListCall(callID, tasksJSON string) domain.ToolCall {
	return domain.ToolCall{
		ID:        callID,
		Tool:      "task_list",
		Arguments: []byte(`{"tasks":` + tasksJSON + `}`),
	}
}

// TestTaskList_ReplaceReturnsTheRenderedList is the tool's whole job: the call lands on the list
// the engine put on the context, and what comes back is the very block the standing content will
// carry — so a model reads its own update in the same shape it will meet again next request.
func TestTaskList_ReplaceReturnsTheRenderedList(t *testing.T) {
	t.Parallel()

	list := tasklist.New()
	ctx := tasklist.WithList(context.Background(), list)

	res, err := NewTaskList().Execute(ctx, taskListCall(
		"c1",
		`[{"text":"read the plan","done":true},{"text":"write the tool"}]`,
	))
	if err != nil {
		t.Fatalf("Execute() returned a Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute() = error result %q", res.Content)
	}
	if res.Content != list.Render() {
		t.Errorf("Execute() content = %q, want the rendered list %q", res.Content, list.Render())
	}
	if !strings.Contains(res.Content, "[✔] read the plan") || !strings.Contains(res.Content, "[ ] write the tool") {
		t.Errorf("the rendered list does not carry both rows: %q", res.Content)
	}

	items := list.Items()
	if len(items) != 2 || items[0].Text != "read the plan" || !items[0].Done || items[1].Done {
		t.Errorf("the held list is %+v, want the two tasks the call carried", items)
	}
}

// TestTaskList_WithoutACarrierIsAnErrorResult pins the absent-list case on the side of the line
// the model can act on: a call outside an engine is a well-formed call this session cannot serve,
// which is an error RESULT — a Go error would abort the Turn over a checklist.
func TestTaskList_WithoutACarrierIsAnErrorResult(t *testing.T) {
	t.Parallel()

	res, err := NewTaskList().Execute(context.Background(), taskListCall("c1", `[{"text":"anything"}]`))
	if err != nil {
		t.Fatalf("Execute() without a list returned a Go error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("Execute() without a list = %q, want an error result", res.Content)
	}
	if !strings.Contains(res.Content, "not available in this session") {
		t.Errorf("refusal does not say the session carries no list: %q", res.Content)
	}
}

// TestTaskList_CancelledContextIsAGoError keeps the package's one Go-error rule: cancellation is
// the loop tearing the Turn down, never something the model is asked to react to.
func TestTaskList_CancelledContextIsAGoError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(tasklist.WithList(context.Background(), tasklist.New()))
	cancel()

	if _, err := NewTaskList().Execute(ctx, taskListCall("c1", `[{"text":"anything"}]`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() on a cancelled context returned err = %v, want context.Canceled", err)
	}
}

// TestTaskList_OverCapInputIsAnErrorResult pins the refusal a model must be able to correct: the
// cap the schema states is enforced by the list, reported as a result naming the limit, and the
// list it already had is left exactly as it was rather than half-applied.
func TestTaskList_OverCapInputIsAnErrorResult(t *testing.T) {
	t.Parallel()

	list := tasklist.New()
	if err := list.Replace([]tasklist.Item{{Text: "the one task it already had"}}); err != nil {
		t.Fatalf("seeding the list: %v", err)
	}
	ctx := tasklist.WithList(context.Background(), list)

	tasks := make([]tasklist.Item, tasklist.MaxItems+1)
	for i := range tasks {
		tasks[i] = tasklist.Item{Text: "task"}
	}
	encoded, err := json.Marshal(tasks)
	if err != nil {
		t.Fatalf("encoding the over-cap call: %v", err)
	}

	res, err := NewTaskList().Execute(ctx, taskListCall("c1", string(encoded)))
	if err != nil {
		t.Fatalf("Execute() over the cap returned a Go error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("Execute() over the cap = %q, want an error result", res.Content)
	}
	if items := list.Items(); len(items) != 1 || items[0].Text != "the one task it already had" {
		t.Errorf("a refused replace changed the held list to %+v", items)
	}
}

// TestTaskList_SpecIsModelFacing pins the half of the tool a model actually reads: the name a
// roster and a config key spell, the one discriminating fact the description must cap — that a
// call REPLACES the whole list rather than appending to it — and the schema's shape.
func TestTaskList_SpecIsModelFacing(t *testing.T) {
	t.Parallel()

	tool := NewTaskList()
	if tool.Name() != "task_list" {
		t.Errorf("name = %q, want %q", tool.Name(), "task_list")
	}
	if !strings.Contains(tool.Description(), "REPLACES") {
		t.Errorf("description does not cap the whole-list replace: %q", tool.Description())
	}

	var schema struct {
		Required   []string `json:"required"`
		Properties struct {
			Tasks struct {
				Type  string `json:"type"`
				Items struct {
					Required   []string                  `json:"required"`
					Properties map[string]map[string]any `json:"properties"`
				} `json:"items"`
			} `json:"tasks"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "tasks" {
		t.Errorf("required = %v, want [tasks]", schema.Required)
	}
	if schema.Properties.Tasks.Type != "array" {
		t.Errorf("tasks type = %q, want array", schema.Properties.Tasks.Type)
	}
	items := schema.Properties.Tasks.Items
	if len(items.Required) != 1 || items.Required[0] != "text" {
		t.Errorf("item required = %v, want [text]", items.Required)
	}
	for _, property := range []string{"text", "done"} {
		if _, ok := items.Properties[property]; !ok {
			t.Errorf("schema is missing the item property %q", property)
		}
	}
}

// TestTaskList_IsRegisteredAndReadOnly pins the two roster facts the tool ships with: every model
// is offered it (no default-off marker), and it takes the read-only floor so no mode gates it.
func TestTaskList_IsRegisteredAndReadOnly(t *testing.T) {
	t.Parallel()

	tool, ok := NewDefaultRegistry(t.TempDir()).Lookup("task_list")
	if !ok {
		t.Fatal("task_list is missing from the default registry — it ships default-ON")
	}
	readOnly, ok := tool.(domain.ReadOnlyTool)
	if !ok {
		t.Fatal("task_list does not implement domain.ReadOnlyTool")
	}
	if !readOnly.ReadOnly() {
		t.Error("task_list declares itself write-capable; writing a checklist touches nothing the host owns")
	}
	if _, off := tool.(domain.DefaultOffTool); off {
		t.Error("task_list must NOT declare DefaultOff — it is offered to every model")
	}
}
