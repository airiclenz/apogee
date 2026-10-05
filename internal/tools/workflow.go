package tools

import (
	"context"
	"encoding/json"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The workflow tool — the model's control over background Workflows (ADR 0089, D4)
// ----------------------------------------------------------------------------

// WorkflowToolName is the stable name of the background-workflow control tool. The dispatch layer
// (internal/agent) recognises it and answers the call itself, so the tool's own Execute is never
// reached on the real path; it is exported so dispatch can key on it without re-declaring the
// spelling. fan_out and sub_agent publish their `background` argument only where this tool is
// offered, because a workflow started in the background is one the model can only check on or stop
// through it.
const WorkflowToolName = "workflow"

// The three actions the tool publishes (ADR 0089 D4), exported so dispatch reads the same spellings
// the schema offers.
const (
	// WorkflowActionStatus lists every workflow of the session, or one in detail when `id` is set.
	WorkflowActionStatus = "status"
	// WorkflowActionStop stops the background workflow `id` and keeps its finished items (ADR 0088).
	WorkflowActionStop = "stop"
	// WorkflowActionMessage sends `text` to one running item's child, named by `item` — the run id
	// or item name the status listing shows — as a human Interjection is sent (ADR 0063).
	WorkflowActionMessage = "message"
)

// workflowSchema is the one variant the tool publishes. Only `action` is required: which of the
// other three a call needs depends on the action, and a JSON Schema either/or is exactly the
// construct a small model fills in badly — dispatch refuses a call missing one with a message
// naming it, and the descriptions say which action reads which property.
const workflowSchema = `{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action": {"type": "string", "enum": ["status", "stop", "message"], "description": "status lists every workflow of this session, or one in detail when id is set; stop stops the workflow id and keeps its finished items; message sends text to one running item's helper."},
    "id": {"type": "string", "description": "The workflow's id, as fan_out, sub_agent or status gave it. Optional for status, required for stop; for message it narrows item to that workflow."},
    "item": {"type": "string", "description": "For message: the running item's run id or name, as status lists it."},
    "text": {"type": "string", "description": "For message: what to tell the item's helper. It reaches the helper between its steps, as a note from the user would."}
  }
}`

var workflowSpec = toolSpec{
	name: WorkflowToolName,
	description: "Check on, stop or message the workflows you started with fan_out or sub_agent in the " +
		"background. status lists them with their items; stop ends one and keeps its " +
		"finished items; message sends a note to one running item's helper. You are woken " +
		"with a workflow's result when it ends, so you need not poll.",
	schema: json.RawMessage(workflowSchema),
}

// Workflow is the model-facing descriptor of the `workflow` tool (ADR 0089 D4). Like FanOut it is a
// PLACEHOLDER: it carries the name, description and schema the model sees, while answering the call
// belongs to dispatch one layer up, which recognises WorkflowToolName and never calls Execute here.
// It carries NO disposition marker: it touches no file, starts no process and reaches no network
// itself — it reads the session's workflow folders and steers the workflows the engine runs.
//
// It is registered DEFAULT-OFF (domain.DefaultOffTool): like fan_out it is lifted per model by
// `tools.enabled:` or a model profile's roster. And it is offered only where the Driver offers
// background workflows at all (HostTools.OffersBackground) — the TUI, never a headless run or a
// daemon firing (ADR 0089 D1) — because it travels with fan_out's and sub_agent's `background`
// switch: a tool that controls background workflows means nothing where none can start.
//
// Execute returns an error result so a misconfigured wiring fails loudly rather than silently.
type Workflow struct {
	toolSpec
}

// NewWorkflow returns the workflow placeholder.
func NewWorkflow() *Workflow { return &Workflow{toolSpec: workflowSpec} }

// DefaultOff keeps workflow off the default menu until a roster rung lifts it.
func (t *Workflow) DefaultOff() bool { return true }

// ArgRoles declares `text` a delegation prompt (domain.ArgRolePrompt): it is a message written FOR
// a running helper, never an action this host performs, and every tool call the helper makes off
// the back of it is inspected at its own action site one level down — sub_agent's `task` precedent.
// `action`, `id` and `item` are identifiers the engine looks up and stay fully inspected.
func (t *Workflow) ArgRoles() map[string]domain.ArgRole {
	return map[string]domain.ArgRole{"text": domain.ArgRolePrompt}
}

// Execute is never reached on the real path: dispatch recognises WorkflowToolName and answers the
// call itself. Reaching it means that wiring is missing, so it returns an error result rather than
// silently doing nothing.
func (t *Workflow) Execute(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	return domain.ToolResult{
		CallID:  call.ID,
		Content: "workflow is answered by the orchestrator; it cannot run as a leaf tool",
		IsError: true,
	}, nil
}

// Compile-time proof workflow declares itself default-off and carries no disposition marker.
var (
	_ domain.Tool           = (*Workflow)(nil)
	_ domain.DefaultOffTool = (*Workflow)(nil)
	_ domain.ArgRoleTool    = (*Workflow)(nil)
)
