package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The fan_out tool — a Workflow the model asks for (ADR 0087, D1/D2)
// ----------------------------------------------------------------------------

// FanOutToolName is the stable name the model calls to ask the engine for a Workflow: one brief run
// over a list of items, a fresh helper per item, each handing back a Receipt, the call answered with
// one line per item plus a report path. The dispatch layer (internal/agent) recognises this name and
// runs the workflow itself, so the tool's own Execute is never reached on the real path. It is
// exported so dispatch can key on it without re-declaring the spelling.
const FanOutToolName = "fan_out"

// fanOutSchemaTemplate is the fan_out schema with TWO holes: the optional `run_on` property the
// seat-choice variant fills (the `sub-agents-choice: model` gate, ADR 0069, spelled as sub_agent's)
// and the optional `background` property the background variant fills (only where the workflow tool
// is offered — on the roster, and the Driver offers background workflows). Both holes empty is the plain variant, and one literal keeps every variant's
// shared properties byte-identical — the same reason subAgentSchemaTemplate is one literal.
//
// The properties follow ADR 0087 D1: a brief template, the items, the receipt fields wanted back, a
// per-item output path, shared context files, an optional tool narrowing, and at most one `verify`
// and one `merge` stage. Retries, waves, width and continuation are configuration, never arguments,
// so none of them is published. `recipe` + `inputs` start a named Recipe instead (ADR 0087 D2),
// where the model fills in only the recipe's declared inputs.
//
// Nothing is `required` at the top level: `task` and `over` are needed unless `recipe` is set, and
// a JSON Schema either/or (`oneOf`) is exactly the construct a small model fills in badly. The
// engine's plan check (workflow.ValidateModelPlan) refuses a call missing either with a fixable
// message, and the two descriptions say so up front.
const fanOutSchemaTemplate = `{
  "type": "object",
  "properties": {
    "task": {"type": "string", "minLength": 20, "description": "The brief every helper gets, one helper per item: {item} is replaced by the item and {out} by the file that helper writes its detail to. Write it self-containedly; each helper starts fresh. Required unless recipe is set."},
    "over": {"type": ["array", "object"], "items": {"type": "string"}, "properties": {"files": {"type": "string"}, "lines": {"type": "string"}, "split": {"type": "string"}}, "description": "The items: an array of strings, or an object with one of files (a workspace glob, ** allowed; each match is an item), lines (a file whose non-blank lines are the items) or split (a directory cut into parts sized for one helper). Required unless recipe is set."},
    "batch": {"type": "integer", "minimum": 1, "description": "optional; how many items one helper gets (default 1)."},
    "context": {"type": "array", "items": {"type": "string"}, "description": "optional; files every helper reads before its item."},
    "returns": {"type": "object", "additionalProperties": {"type": "string"}, "description": "optional; the typed fields each helper reports beside its status and one-line summary, as name: type — int, text, list, or an enum written a|b|c. Example: {\"findings\": \"int\"}."},
    "out": {"type": "string", "description": "optional; where each helper writes its detail output, with {item} in the path. Leave unset and each item gets a file in the workflow folder."},
    "verify": {"type": "object", "properties": {"when": {"type": "string", "description": "optional; which items to check, as a condition on the returns fields, e.g. \"findings > 0\". Unset checks every item."}, "task": {"type": "string", "description": "optional; what the checker should look at, with {item}."}}, "description": "optional; one adversarial check that tries to refute each selected item's report."},
    "merge": {"type": "object", "required": ["task"], "properties": {"task": {"type": "string", "description": "The brief for the one helper that reads every item's output and writes the combined report."}}, "description": "optional; one helper over all the items' outputs."},
    "tools": {"type": "array", "items": {"type": "string"}, "description": "optional; narrow each helper's tools to these names from your own menu. It can only remove tools, never add them."},
    "recipe": {"type": "string", "description": "optional; the name of a recipe to run instead of describing the fan-out yourself. Give only inputs with it."},
    "inputs": {"type": "object", "additionalProperties": {"type": "string"}, "description": "optional; the recipe's declared inputs, as name: value."}%s%s
  }
}`

// fanOutRunOnProperty is the property the seat-choice variant adds — sub_agent's `run_on`, the same
// two spellings, because it asks the same question (ADR 0087 D9: a stage may run on the other
// Delegation seat under the existing gate). It is OPTIONAL: leaving it unset runs every helper
// where `sub-agents-server:` would have sent a delegation.
const fanOutRunOnProperty = `,
    "run_on": {"type": "string", "enum": ["session", "sub-agents-server"], "description": "Optional; where the helpers run — see the Delegations line of the host orientation. Leave unset for the configured default."}`

// fanOutBackgroundProperty is the property the background variant adds (ADR 0089). It is published
// only where the workflow tool (WorkflowToolName) is offered: a background workflow ends by waking
// the agent, and the workflow tool is the only way the model can check on or stop one meanwhile.
const fanOutBackgroundProperty = `,
    "background": {"type": "boolean", "description": "Optional; true starts the workflow in the background and answers at once. You are woken with its result when it ends; use the workflow tool to check on it or stop it meanwhile."}`

// fanOutSchema renders the published schema for one variant. With neither option set the schema
// carries neither `run_on` nor `background`.
func fanOutSchema(opts FanOutOptions) json.RawMessage {
	runOn, background := "", ""
	if opts.SeatChoice {
		runOn = fanOutRunOnProperty
	}
	if opts.Background {
		background = fanOutBackgroundProperty
	}
	return json.RawMessage(fmt.Sprintf(fanOutSchemaTemplate, runOn, background))
}

var fanOutSpec = toolSpec{
	name: FanOutToolName,
	description: "Run one brief over a list of items: the engine starts a fresh helper per item, " +
		"several at once, and each reports a status, a one-line summary and the fields you " +
		"asked for in returns. You get back one line per item plus the path of a full report, " +
		"not the helpers' prose. Use it for many similar pieces of work over a list; use " +
		"sub_agent for one self-contained task. Calling it again with the same arguments " +
		"skips the items already done.",
	schema: fanOutSchema(FanOutOptions{}),
}

// FanOutOptions carries the host's choices about WHICH fan_out variant this build offers — the two
// optional arguments published only where something else makes them meaningful.
type FanOutOptions struct {
	// SeatChoice publishes `run_on`, as on sub_agent (the `sub-agents-choice: model` gate, ADR 0069).
	SeatChoice bool
	// Background publishes `background` (ADR 0089): set where the workflow tool is offered — the
	// Driver offers background workflows (HostTools.OffersBackground) and the roster lifts it.
	Background bool
}

// FanOut is the model-facing descriptor of the `fan_out` tool (ADR 0087 D2). Like SubAgent it is a
// PLACEHOLDER: it carries the name, description and schema the model sees, while running the
// workflow belongs to dispatch one layer up, which recognises FanOutToolName and never calls Execute
// here. It carries NO disposition marker either — its helpers are workflow children spawned through
// the recursion point (ADR 0014), and each of their tool calls gets the full per-call disposition one
// level down.
//
// It is registered DEFAULT-OFF (domain.DefaultOffTool, ADR 0087 D8): a whole new tool on every
// request is a cost the Floor invariant will not charge a model until bench evidence says it pays,
// so `tools.enabled:` or a model profile's roster lifts it per model.
//
// Execute returns an error result so a misconfigured wiring fails loudly rather than silently.
type FanOut struct {
	toolSpec
	opts FanOutOptions
}

// NewFanOut returns the plain fan_out placeholder — neither `run_on` nor `background` published.
func NewFanOut() *FanOut { return NewFanOutWith(FanOutOptions{}) }

// NewFanOutWith returns the fan_out placeholder in the variant opts asks for. Only the published
// schema differs between variants.
func NewFanOutWith(opts FanOutOptions) *FanOut {
	spec := fanOutSpec
	if opts != (FanOutOptions{}) {
		spec.schema = fanOutSchema(opts)
	}
	return &FanOut{toolSpec: spec, opts: opts}
}

// OffersSeatChoice reports whether this tool published the `run_on` argument, so the engine can
// tell a seat the model was offered from one it could not have been told about.
func (t *FanOut) OffersSeatChoice() bool { return t.opts.SeatChoice }

// OffersBackground reports whether this tool published the `background` argument.
func (t *FanOut) OffersBackground() bool { return t.opts.Background }

// DefaultOff keeps fan_out off the default menu until a roster rung lifts it (ADR 0087 D8).
func (t *FanOut) DefaultOff() bool { return true }

// ArgRoles declares `task`, `verify` and `merge` as delegation prompts (domain.ArgRolePrompt): each
// carries brief text written FOR the helpers, never an action this host performs, and every tool
// call a helper makes off the back of it is inspected at its own action site one level down.
// `verify` and `merge` are objects whose only prose is their own `task` (and verify's `when`, a
// condition on receipt fields the engine evaluates, never executes). The path-bearing arguments —
// `over`, `out`, `context` — and `inputs`, whose values a recipe may hand a script stage, are NOT
// declared: they stay fully inspected.
func (t *FanOut) ArgRoles() map[string]domain.ArgRole {
	return map[string]domain.ArgRole{
		"task":   domain.ArgRolePrompt,
		"verify": domain.ArgRolePrompt,
		"merge":  domain.ArgRolePrompt,
	}
}

// Execute is never reached on the real path: dispatch recognises FanOutToolName and runs the
// workflow itself. Reaching it means that wiring is missing, so it returns an error result rather
// than silently doing nothing.
func (t *FanOut) Execute(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	return domain.ToolResult{
		CallID:  call.ID,
		Content: "fan_out is a workflow handled by the orchestrator; it cannot run as a leaf tool",
		IsError: true,
	}, nil
}

// Compile-time proof fan_out declares itself default-off and carries no disposition marker.
var (
	_ domain.Tool           = (*FanOut)(nil)
	_ domain.DefaultOffTool = (*FanOut)(nil)
)
