package tools

import (
	"context"
	"encoding/json"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The sub_agent tool — the recursion point (ADR 0013, D2)
// ----------------------------------------------------------------------------

// SubAgentToolName is the stable name the model calls to delegate a focused sub-task to a
// nested agent. The dispatch layer (internal/agent) recognises this name as the RECURSION
// POINT — it drives a nested Agent rather than executing a leaf tool — so the tool's own
// Execute is never reached on the real path. It is exported so dispatch can key on it
// without importing an unexported constant.
const SubAgentToolName = "sub_agent"

var subAgentSchema = json.RawMessage(`{
  "type": "object",
  "required": ["task"],
  "properties": {
    "task": {"type": "string", "description": "The focused sub-task to delegate to a nested agent. Describe it self-containedly: the sub-agent starts with a fresh conversation and reports a single result back."}
  }
}`)

// SubAgentArgs is the sub_agent tool's argument shape: a single self-contained task string.
// It is exported so the dispatch layer parses the delegated task without re-declaring the
// schema.
type SubAgentArgs struct {
	Task string `json:"task"`
}

// SubAgent is the model-facing descriptor for delegating a sub-task to a nested agent
// (ADR 0013). It is a PLACEHOLDER tool: it carries the name, description, and schema the
// model sees, but its blast radius and execution belong to the orchestrator one layer up —
// dispatch special-cases SubAgentToolName as the recursion point and never calls Execute
// here. It deliberately carries NO disposition marker (not ReadOnly, not a
// workspaceScopedWriter, not an ExternalEffectTool, not a SubprocessTool): the sub-agent is
// never confined or gated AS A UNIT; each CHILD tool call gets the full per-call disposition
// one level down, using the parent's threaded mode / confiner / approver / guardrails.
//
// Execute returns an error result so a misconfigured wiring (the tool reached as a leaf
// because dispatch did not special-case it) fails loudly rather than silently no-op'ing.
type SubAgent struct{}

// NewSubAgent returns the sub_agent placeholder tool. The orchestrator (internal/agent)
// supplies the real nested-agent execution; this value only carries the model-facing menu
// entry and is the registry handle Subset narrows on.
func NewSubAgent() *SubAgent { return &SubAgent{} }

// Name returns the stable identifier the model calls and dispatch keys the recursion point on.
func (t *SubAgent) Name() string { return SubAgentToolName }

// Description returns the model-facing summary of the delegation tool.
func (t *SubAgent) Description() string {
	return "Delegate a focused sub-task to a nested sub-agent. The sub-agent runs with the " +
		"same (or stricter) privileges as you, has a subset of your tools, and reports a " +
		"single result back. Use it to isolate a self-contained piece of work."
}

// Schema returns the JSON schema of the tool's arguments.
func (t *SubAgent) Schema() json.RawMessage { return subAgentSchema }

// Execute is never reached on the real path: dispatch recognises SubAgentToolName as the
// recursion point and drives a nested Agent instead. Reaching it means the recursion point
// was not wired, so it returns an error result rather than silently doing nothing.
func (t *SubAgent) Execute(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	return domain.ToolResult{
		CallID:  call.ID,
		Content: "sub_agent is a recursion point handled by the orchestrator; it cannot run as a leaf tool",
		IsError: true,
	}, nil
}

// Compile-time proof the sub_agent tool carries NONE of the disposition markers — it is a
// plain domain.Tool. The dispatch recursion point owns its blast radius (per-child, one level
// down), so it must not be classified as read-only / workspace-writer / external / subprocess.
var (
	_ domain.Tool = (*SubAgent)(nil)
)
