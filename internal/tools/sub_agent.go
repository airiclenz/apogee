package tools

import (
	"context"
	"encoding/json"
	"fmt"

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

// The two Delegation seats a delegation may name in `run_on` (ADR 0069): the SESSION server the
// top-level model itself is bound to, and the Sub-agent server the `sub-agents-server` key flags.
// They are the whole vocabulary — routing to an arbitrary `servers:` entry or to a tier is not
// offered — and they are exported so the engine resolves a seat against the same two spellings the
// schema publishes rather than against its own copies of them.
const (
	// RunOnSession asks for the child to run on the session server, with the parent's posture.
	RunOnSession = "session"
	// RunOnSubAgentsServer asks for the child to run on the Sub-agent server; where that server
	// has no usable target the engine falls back to the session seat and says so in the result.
	RunOnSubAgentsServer = "sub-agents-server"
)

// subAgentSchemaTemplate is the sub_agent schema with ONE hole: the optional `run_on` property the
// seat-choice variant fills and the plain variant leaves empty. Keeping both variants in one
// literal is what keeps the plain schema byte-identical across the two constructors — a second
// literal would be a second thing to keep in step, and the plain variant is prefill on every
// request of every session that never enables the choice.
//
// The two floors — `minLength` on task, `minimum` on max_steps — are SCHEMA TEXT the model reads,
// not a validator the engine runs: nothing in apogee refuses a call over them (the recursion point
// still checks only that the task is non-empty). They tell a small model, in the one place it looks
// before composing a call, that a three-word task is not a delegation and that `max_steps: 0` was
// never a way to ask for "unbounded" (a request above the configured cap is applied AS the cap and
// the result says so — plan 2026-09-14 - 03, item 4).
//
// `tools` is the per-task roster ADR 0005 always allowed and never published: the string
// SubAgentToolsReadOnly for the read-only set, or an array of tool names. It narrows and never
// widens — the engine intersects it with the parent's own roster — and an unknown name is refused
// with a result naming it, so a model learns the spelling instead of silently losing the tool (plan
// 2026-09-14 - 03, item 5).
//
// `output_path` is the file the delegation is expected to write. It changes nothing about the
// task; it names the ONE file the child may still write on its step-cap wrap-up Turn, where every
// other tool is withdrawn (turnLifecycle.wrapUp — plan 2026-09-14 - 03, item 8). Without it that
// Turn still keeps one write_file, aimed wherever the child's Mode admits (plan 2026-09-18 - 00,
// item 5); the tool description tells the model when to name the path, because the engine reads
// no path out of the task text.
//
// `continue` names a delegation this conversation retained after the engine stopped it at a bound
// (plan 2026-09-18 - 00, P6): the sub-agent restarts from that run's engine fold on a fresh cap,
// and `task` says what to do next. It sits before `output_path` and after `tools` — the properties
// the continuation inherits when the call leaves them unset — so the model reads the handle beside
// the arguments it stands in for.
const subAgentSchemaTemplate = `{
  "type": "object",
  "required": ["task"],
  "properties": {
    "task": {"type": "string", "minLength": 20, "description": "The focused sub-task to delegate to a nested agent. Describe it self-containedly: the sub-agent starts with a fresh conversation and reports a single result back."},
    "name": {"type": "string", "description": "Short name for this delegation, shown in the UI: 2–4 words naming the job, e.g. \"scout config keys\". Give one."},
    "max_steps": {"type": "integer", "minimum": 1, "description": "optional; a lower cap for this delegation only, in Turns. A request above the configured cap is clamped to it, and the result says so."},
    "tools": {"type": ["string", "array"], "items": {"type": "string"}, "description": "optional; narrow the sub-agent's tools: the string \"read-only\" for the read-only set, or an array of tool names from your own menu. It can only remove tools, never add them; an unknown name is refused."},
    "continue": {"type": "string", "description": "optional; the name of a delegate that stopped at a bound earlier in this conversation. The sub-agent restarts from that run's engine summary with a fresh step cap; task says what to do next."},
    "output_path": {"type": "string", "description": "optional; the file the sub-agent is expected to write, relative to the workspace root or absolute. If it hits its step cap, write_file to this one path stays available for its final reply."}%s
  }
}`

// SubAgentToolsReadOnly is the one keyword the `tools` argument accepts in place of a list: the
// read-only set — every tool the child would hold whose class Plan mode admits on every target,
// plus sub_agent where the depth bound still offers it. It is exported beside the seat spellings for
// the same reason: the engine reads the argument against the word the schema publishes.
const SubAgentToolsReadOnly = "read-only"

// subAgentRunOnProperty is the one property the seat-choice variant adds. It is OPTIONAL like every
// other argument but `task`: a model that never names a seat keeps making valid calls and gets the
// seat the `sub-agents-server` key decides. The description points at the orientation block rather
// than describing the two servers here, because what each seat IS (its entry name, its
// `description:`, the model it pins) is session state the schema cannot know.
const subAgentRunOnProperty = `,
    "run_on": {"type": "string", "enum": ["session", "sub-agents-server"], "description": "Optional; where this delegation runs — see the Delegations line of the host orientation. Leave unset for the configured default."}`

// subAgentSchema renders the published schema for one variant: with seatChoice the `run_on`
// property is offered, without it the schema is exactly what it was before seat choice existed.
func subAgentSchema(seatChoice bool) json.RawMessage {
	runOn := ""
	if seatChoice {
		runOn = subAgentRunOnProperty
	}
	return json.RawMessage(fmt.Sprintf(subAgentSchemaTemplate, runOn))
}

var subAgentSpec = toolSpec{
	name: SubAgentToolName,
	description: "Delegate a focused sub-task to a nested sub-agent. The sub-agent runs with the " +
		"same (or stricter) privileges as you, has a subset of your tools, and reports a " +
		"single result back. Use it to isolate a self-contained piece of work. " +
		"You may call sub_agent several times in a single reply; sibling delegations run " +
		"concurrently, so dispatch independent sub-tasks together in one reply rather than " +
		"one per turn. If the task names a file the sub-agent must write, pass it as output_path.",
	schema: subAgentSchema(false),
}

// SubAgentArgs is the sub_agent tool's argument shape: a self-contained task string plus an
// OPTIONAL short name for the delegation and an OPTIONAL lowered step cap. It is exported so the
// dispatch layer parses the delegated task without re-declaring the schema.
//
// Name is display identity only, never privilege (ADR 0005): it is what the session chat calls
// the child instead of the task's first line. The schema asks the model outright for one, but it
// is not REQUIRED: an absent or blank name is a valid call, and the host then names that
// delegation out of band while it runs (ADR 0068). Every display wears the task's first line until
// that name lands, and keeps wearing it where out-of-band naming is switched off or fails.
//
// MaxSteps is the Turns this ONE delegation may take before the engine ends it, and it can only
// ever LOWER the host's configured cap (`delegate-max-steps`): the orchestrator applies
// min(configured, requested) when both are positive and ignores the request otherwise — 0 or
// absent means "use the configured cap", a value at or above it changes nothing, and a request
// against a cap the host switched off is likewise ignored. Like Name it is never privilege: it
// tightens a bound, so it can only make a delegation cheaper.
//
// RunOn is the Delegation seat this ONE delegation asks for — RunOnSession or
// RunOnSubAgentsServer (ADR 0069) — and it is never privilege either: both seats run the child
// with the posture that seat already has, so naming one moves the work, not what the work may do.
// It is only ever OFFERED where the host enables seat choice (NewSubAgentWith), so a call that
// carries it against the plain variant, and every synthesised call in this repo, decodes to the
// empty string — the value that means "the configured default decides".
//
// Tools is the roster this ONE delegation asks the child to be narrowed to (SubAgentRoster). It is
// the third argument that can only ever tighten: the engine intersects it with the tools the child
// would otherwise inherit, so it names a subset or it names nothing — the zero value, which leaves
// the inherited roster alone.
//
// OutputPath is the file this ONE delegation is expected to write — the same path spelling
// write_file's `path` takes, relative to the workspace root or absolute. The task text is left
// untouched by it; what it buys is the step-cap wrap-up: a capped child that named one keeps
// write_file for exactly that path on its closing Turn, where every other tool is withdrawn (plan
// 2026-09-14 - 03, item 8). Empty leaves the wrap-up's one write_file unnarrowed (plan 2026-09-18
// - 00, item 5). Never privilege: the write still runs through the same fence and Mode the child's
// every other write does.
//
// Continue is the display name of a delegation the engine stopped at a bound earlier in the same
// Exchange — the handle the capped result itself told the model to spell back (plan 2026-09-18 -
// 00, P6). The orchestrator spawns a FRESH child whose opening context is the retained task plus
// that run's engine fold, with Task as the continuation instructions, and inherits Name, Tools and
// OutputPath from the retained entry wherever this call leaves them unset. A name nothing is
// retained under is refused with a result listing the retained names. Empty is an ordinary spawn.
// Like Name it is prose for the child, never privilege, and never a key the engine serialises.
type SubAgentArgs struct {
	Task       string         `json:"task"`
	Name       string         `json:"name"`
	MaxSteps   int            `json:"max_steps"`
	RunOn      string         `json:"run_on"`
	Tools      SubAgentRoster `json:"tools"`
	Continue   string         `json:"continue"`
	OutputPath string         `json:"output_path"`
}

// SubAgentRoster is the decoded `tools` argument of a sub_agent call — the one argument whose wire
// shape is a union: the keyword SubAgentToolsReadOnly, or an array of tool names. Exactly one of
// the two fields is set when the call named a roster; the zero value means the call named none
// (absent, null, "" or an empty array all decode to it) and the child keeps its inherited set.
type SubAgentRoster struct {
	// ReadOnly asks for the read-only set (SubAgentToolsReadOnly).
	ReadOnly bool
	// Names lists the tools asked for, in the order the call named them.
	Names []string
}

// IsSet reports whether the call named a roster at all — the zero value is "leave it alone".
func (r SubAgentRoster) IsSet() bool { return r.ReadOnly || len(r.Names) > 0 }

// MarshalJSON renders the roster in the wire shape UnmarshalJSON reads back — the keyword, the
// array, or `null` for the zero value — so a SubAgentArgs round-trips and a synthesised call (the
// engine's tests build them by marshalling the struct) never carries a shape the decoder refuses.
func (r SubAgentRoster) MarshalJSON() ([]byte, error) {
	switch {
	case r.ReadOnly:
		return json.Marshal(SubAgentToolsReadOnly)
	case len(r.Names) > 0:
		return json.Marshal(r.Names)
	default:
		return []byte("null"), nil
	}
}

// UnmarshalJSON accepts the two wire shapes the schema publishes and refuses everything else with a
// message that names them, so the refusal the recursion point returns tells the model what the
// argument takes. A keyword other than SubAgentToolsReadOnly is refused too: the alternative —
// treating "readonly" or "ro" as a list of one unknown tool — would answer with the wrong
// correction.
func (r *SubAgentRoster) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*r = SubAgentRoster{}
		return nil
	}
	var keyword string
	if err := json.Unmarshal(b, &keyword); err == nil {
		switch keyword {
		case "":
			*r = SubAgentRoster{}
		case SubAgentToolsReadOnly:
			*r = SubAgentRoster{ReadOnly: true}
		default:
			return fmt.Errorf("tools: unknown keyword %q — use %q or an array of tool names", keyword, SubAgentToolsReadOnly)
		}
		return nil
	}
	var names []string
	if err := json.Unmarshal(b, &names); err != nil {
		return fmt.Errorf("tools: must be the string %q or an array of tool names", SubAgentToolsReadOnly)
	}
	*r = SubAgentRoster{Names: names}
	return nil
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
type SubAgent struct {
	toolSpec
	seatChoice bool
}

// SubAgentOptions carries the host's choices about WHICH sub_agent variant this build offers. It
// is a struct rather than a parameter list because the variants are a host policy that grows: a
// second axis added later is a field here, not a new constructor.
type SubAgentOptions struct {
	// SeatChoice publishes the `run_on` argument, letting the top-level model name the Delegation
	// seat per delegation (ADR 0069, the `sub-agents-choice: model` gate). False — the default and
	// the whole of `sub-agents-choice: fixed` — publishes the schema unchanged, so a model that
	// never gets the choice is never told about a seat it cannot pick.
	SeatChoice bool
}

// NewSubAgent returns the plain sub_agent placeholder tool — no seat choice, the schema this tool
// published before ADR 0069. The orchestrator (internal/agent) supplies the real nested-agent
// execution; this value only carries the model-facing menu entry and is the registry handle Subset
// narrows on.
func NewSubAgent() *SubAgent { return NewSubAgentWith(SubAgentOptions{}) }

// NewSubAgentWith returns the sub_agent placeholder tool in the variant opts asks for. Only the
// published schema and OffersSeatChoice differ between variants: the name, the description and the
// recursion point are one tool, so nothing downstream keys on which variant it holds.
func NewSubAgentWith(opts SubAgentOptions) *SubAgent {
	spec := subAgentSpec
	if opts.SeatChoice {
		spec.schema = subAgentSchema(true)
	}
	return &SubAgent{toolSpec: spec, seatChoice: opts.SeatChoice}
}

// OffersSeatChoice reports whether this tool published the `run_on` argument. It exists so the
// engine can ask, at spawn, whether the choice was ever offered: a seat named against a variant
// that published no `run_on` is a value the model could not have been told about.
func (t *SubAgent) OffersSeatChoice() bool { return t.seatChoice }

// PromptArgKeys declares `task`, `name` and `continue` as delegation prompts (domain.PromptTool):
// all three carry prose written FOR the nested agent, never an action this host performs. The
// dangerous-action guard therefore matches no rule against their text — a task that merely
// NAMES a guarded path ("report on the readable git surfaces — .git/config") is a
// description, and every tool call the child makes off the back of it is inspected at its
// own action site, one level down. `continue` is on the list because it spells a NAME back — the
// same prose that rode `name`, and a name the capped result itself told the model to repeat, so a
// guard tripping on it ("audit .git/config") would refuse the very call the engine asked for.
// `max_steps`, `run_on` and `tools` are NOT declared: none of them carries prose, so none needs an
// exemption from a guard that matches rules against text.
func (t *SubAgent) PromptArgKeys() []string { return []string{"task", "name", "continue"} }

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

// Compile-time proof the sub_agent tool carries NONE of the disposition markers — its only
// declaration is the prompt-carrying argument keys above, an inspection hint rather than a
// disposition. The dispatch recursion point owns its blast radius (per-child, one level
// down), so it must not be classified as read-only / workspace-writer / external / subprocess.
var (
	_ domain.Tool = (*SubAgent)(nil)
)
