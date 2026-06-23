package domain

import (
	"context"
	"encoding/json"
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
}

// ToolRegistry is the injectable set of available tools (ADR 0001 — injectable, no
// globals). A sub-agent receives a subset of the parent's registry, never a
// superset (ADR 0005).
type ToolRegistry struct {
	// unexported map[string]Tool
}

// NewToolRegistry returns an empty registry.
func NewToolRegistry() *ToolRegistry { panic("sketch: not implemented") }

// Register adds a tool, returning an error on a duplicate name.
func (r *ToolRegistry) Register(t Tool) error { panic("sketch: not implemented") }

// Subset returns a new registry containing only the named tools — the primitive a
// caller uses to narrow a sub-agent's tools (ADR 0005).
func (r *ToolRegistry) Subset(names ...string) *ToolRegistry { panic("sketch: not implemented") }

// ExternalEffects is the single injectable boundary for non-forkable external
// effects (ADR 0008). Production uses a live implementation; the bench injects a
// deterministic stub (network-unreachable / empty-MCP) without touching tool code.
type ExternalEffects interface {
	Do(ctx context.Context, call ToolCall) (ToolResult, error)
}
