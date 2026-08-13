package domain

import (
	"context"
	"encoding/json"
	"fmt"
)

// ----------------------------------------------------------------------------
// Tools (ADR 0002 open extension point; ADR 0008 stateless across Turns)
// ----------------------------------------------------------------------------

// Tool is the public, open extension point: embedders may register their own.
//
// Contract — stateless across Turns (ADR 0008): a tool's only durable side effect
// is filesystem writes; nothing live (process, REPL, socket, cursor) survives the
// quiescent boundary. terminal and python-exec are one-shot (fresh process per
// call). A tool needing persistence must serialize it into conversation state, not
// hold it live — this is what makes snapshot/resume and the bench's fork coherent.
type Tool interface {
	// Name is the stable identifier the model calls and the registry keys on.
	Name() string
	// Description and Schema are presented to the model (the JSON-schema of args).
	Description() string
	Schema() json.RawMessage
	// Execute runs the call. It must honour ctx cancellation (ADR 0007) and the
	// statelessness contract above. A panic here is caught at the loop's extension
	// boundary and surfaced as an ErrorEvent.
	Execute(ctx context.Context, call ToolCall) (ToolResult, error)
}

// ExternalEffectTool is an optional interface a Tool implements when it reaches
// state Apogee does not own (network, MCP). The loop routes these through
// Config.ExternalEffects so the bench can stub them deterministically (ADR 0008),
// and the Confiner/Approval gate treats them as unconfinable in Auto (ADR 0004).
type ExternalEffectTool interface {
	Tool
	ExternalEffect() ExternalEffectKind
}

// ReadOnlyTool is an optional interface a Tool implements to declare that it performs
// no writes. It is the signal Plan mode filters on (only read-only tools run) and that
// Ask-Before uses to skip Approval for a harmless read. A Tool that does not implement
// it — or implements it returning false — is treated as write-capable, the safe
// default that gates. IsReadOnly is the helper the loop should call rather than the
// type assertion directly.
type ReadOnlyTool interface {
	Tool
	ReadOnly() bool
}

// IsReadOnly reports whether t has declared itself read-only via ReadOnlyTool. A tool
// that makes no such declaration is treated as write-capable.
func IsReadOnly(t Tool) bool {
	ro, ok := t.(ReadOnlyTool)
	return ok && ro.ReadOnly()
}

// ReadSourceTool is an optional interface a write-capable Tool implements to declare
// which of its argument keys name a path the tool only READS — a copy's source, never
// its destination. The dangerous-action guard consults it so a rule that names a
// write/delete target does not fire on a value the tool cannot write through. It
// declares ARGUMENT ROLES, not read permission: what the tool may actually read is still
// the read fence's decision (its own path scope and the extra read-only roots). A tool
// that deletes or moves its source must NOT implement this — that source is a write
// target by another name. ReadSourceArgKeys is the helper the guard calls rather than
// the type assertion directly.
type ReadSourceTool interface {
	Tool
	// ReadSourceKeys returns the argument keys whose value names a read-only source path.
	ReadSourceKeys() []string
}

// ReadSourceArgKeys returns the argument keys t has declared as read-only source paths
// via ReadSourceTool. A tool that makes no such declaration — including a nil t — has
// none, the safe default: every argument is judged as a potential write target.
func ReadSourceArgKeys(t Tool) []string {
	if rs, ok := t.(ReadSourceTool); ok {
		return rs.ReadSourceKeys()
	}
	return nil
}

// PromptTool is an optional interface a Tool implements to declare which of its argument
// keys carry instruction PROSE addressed to ANOTHER agent — a delegated task, never
// something this host itself acts on. The dangerous-action guard consults it so no rule
// matches text the tool only forwards: a delegation prompt that merely NAMES a guarded path
// is a description, and the delegated agent's own tool calls are each inspected at the
// action site one level down, so the exemption loses no coverage. It declares ARGUMENT
// ROLES, not trust: a key whose value this host would itself execute, open or write through
// must NOT be declared here. PromptArgKeys is the helper the guard calls rather than the
// type assertion directly.
type PromptTool interface {
	Tool
	// PromptArgKeys returns the argument keys whose value is prose for another agent.
	PromptArgKeys() []string
}

// PromptArgKeys returns the argument keys t has declared as delegation prompts via
// PromptTool. A tool that makes no such declaration — including a nil t — has none, the
// safe default: every one of its arguments stays fully inspected.
func PromptArgKeys(t Tool) []string {
	if pt, ok := t.(PromptTool); ok {
		return pt.PromptArgKeys()
	}
	return nil
}

// SubprocessTool is an optional interface a Tool implements to declare that it launches
// an OS subprocess (a shell, an interpreter, a child program) whose blast radius is the
// whole filesystem unless OS-confined — the unbounded surface ADR 0012 fences with the
// Confiner. The dispatch disposition keys on this marker to RUN such a tool inside
// Confiner.Confine in Auto with confine-to-workspace on (rather than gating it), and to
// gate it when fs-confinement is unavailable ("confine if you can, gate if you can't").
// terminal / python-exec (P3.8) carry it; the in-process write tools do not (they are
// path-safety-bounded, not OS-confined). IsSubprocessTool is the helper the loop calls.
type SubprocessTool interface {
	Tool
	// Subprocess reports that this tool launches an OS subprocess. It exists so a tool
	// can implement the marker yet still report false (a degraded build), the safe
	// default being treated as a non-subprocess tool.
	Subprocess() bool
}

// IsSubprocessTool reports whether t has declared itself a subprocess tool via
// SubprocessTool — the signal the disposition confines it (Auto) rather than gating it.
// A tool that makes no such declaration is not a subprocess tool.
func IsSubprocessTool(t Tool) bool {
	st, ok := t.(SubprocessTool)
	return ok && st.Subprocess()
}

// ExternalEffectKind classifies a non-forkable external effect.
type ExternalEffectKind string

const (
	EffectNetwork ExternalEffectKind = "network"
	EffectMCP     ExternalEffectKind = "mcp"
)

// ToolCall is a parsed request from the model to run a tool.
type ToolCall struct {
	ID        string
	Tool      string
	Arguments json.RawMessage
}

// ToolResult is what a tool returns to the loop (pre tool-result-capping).
type ToolResult struct {
	CallID  string
	Content string
	IsError bool
	// Summary is the OPTIONAL structured half of the outcome, for a host that renders
	// this result (toolsummary.go). Nil — the default, and what an embedder's tool
	// always returns — means there is nothing but the prose: the host reads Content.
	//
	// It describes what the TOOL DID, not what the text says, so text rewriting never
	// invalidates it: a Mechanism that edits Content on the PostToolResult seam, and the
	// loop's own clampToolResult elision of an oversized result, both leave the summary
	// true. It is view-facing only — the model reads Content — and is never persisted.
	Summary ToolSummary
}

// ToolRegistry is the injectable set of available tools (ADR 0001 — injectable, no
// globals). A sub-agent receives a subset of the parent's registry, never a
// superset (ADR 0005). Registration order is preserved so the tool menu the model
// sees is deterministic across runs (load-bearing for the bench's reproducibility).
type ToolRegistry struct {
	byName map[string]Tool // keyed on Tool.Name for O(1) dispatch lookup
	order  []string        // registration order, for a deterministic All()
}

// NewToolRegistry returns an empty registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{byName: make(map[string]Tool)}
}

// Register adds a tool, returning ErrDuplicateTool on a name already present and
// ErrInvalidTool on an empty name (the model keys calls on the name, so it must be a
// stable, non-empty identifier).
func (r *ToolRegistry) Register(t Tool) error {
	name := t.Name()
	if name == "" {
		return fmt.Errorf("%w: tool name must not be empty", ErrInvalidTool)
	}
	if _, exists := r.byName[name]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateTool, name)
	}
	r.byName[name] = t
	r.order = append(r.order, name)
	return nil
}

// Lookup returns the tool registered under name, and whether it was found — the seam
// the loop's dispatch resolves a parsed ToolCall against.
func (r *ToolRegistry) Lookup(name string) (Tool, bool) {
	t, ok := r.byName[name]
	return t, ok
}

// All returns the registered tools in registration order — the read seam the loop
// builds the model's tool menu from without reaching into unexported storage.
func (r *ToolRegistry) All() []Tool {
	tools := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		tools = append(tools, r.byName[name])
	}
	return tools
}

// Subset returns a new registry containing only the named tools, in the order named
// — the primitive a caller uses to narrow a sub-agent's tools (ADR 0005). Names not
// present in this registry are skipped, so the result can never be a superset of the
// parent; a repeated name is registered once.
func (r *ToolRegistry) Subset(names ...string) *ToolRegistry {
	sub := NewToolRegistry()
	for _, name := range names {
		t, ok := r.byName[name]
		if !ok {
			continue
		}
		if _, already := sub.byName[name]; already {
			continue
		}
		_ = sub.Register(t) // cannot fail: name is non-empty (it keyed r.byName) and unique here
	}
	return sub
}

// ExternalEffects is the single injectable boundary for non-forkable external
// effects (ADR 0008). Production uses a live implementation; the bench injects a
// deterministic stub (network-unreachable / empty-MCP) without touching tool code.
type ExternalEffects interface {
	Do(ctx context.Context, call ToolCall) (ToolResult, error)
}
