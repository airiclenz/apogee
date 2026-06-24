package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ----------------------------------------------------------------------------
// The surfaced server tool — a domain.ExternalEffectTool of kind mcp
// ----------------------------------------------------------------------------
//
// serverTool adapts one tool advertised by a connected MCP server into Apogee's
// domain.Tool surface. It implements ExternalEffectTool of kind mcp, so the dispatch
// disposition classifies it as classMCP and gates it through Approval in Auto under
// confine-to-workspace=true (ADR 0012 D3) — the gating is inherited for free from the
// effect kind, not re-implemented here. The server's description and input schema are
// UNTRUSTED data passed to the model verbatim (the trust boundary in doc.go): this
// adapter never executes them, it only presents them and forwards a call to the server.

// toolNameSeparator joins a server alias to the server's own tool name so two servers
// advertising the same tool name (e.g. two "search" tools) do not collide in the single
// flat registry, and the human approving a call sees which server it reaches.
const toolNameSeparator = "__"

// serverTool is one MCP server tool surfaced as a domain.ExternalEffectTool. It holds the
// stable, registry-qualified name, the model-facing description/schema (the server's, as
// untrusted presentation data), the server's own unqualified tool name (what CallTool
// addresses), and the session caller it forwards the call through. It is stateless across
// Turns (ADR 0008): it holds no per-call state, only the live session handle the Client owns.
type serverTool struct {
	name        string          // registry-qualified: "<serverAlias>__<remoteName>"
	remoteName  string          // the server's own tool name (what CallTool addresses)
	description string          // the server's advertised description (untrusted presentation)
	schema      json.RawMessage // the server's input schema, normalised to JSON (untrusted)
	caller      toolCaller      // the live session this tool forwards a call to
}

// toolCaller is the narrow seam serverTool forwards a call through — the single method of a
// connected MCP session it needs. The live *mcp.ClientSession satisfies it; a test fake can
// stand in without a transport (though the package's hermetic tests prefer a real in-memory
// session, which exercises the SDK end to end).
type toolCaller interface {
	CallTool(ctx context.Context, params *mcpsdk.CallToolParams) (*mcpsdk.CallToolResult, error)
}

// newServerTool builds a surfaced tool from a server's advertised tool, qualifying its name
// with the server alias and normalising its input schema to JSON for the model. A tool whose
// schema does not marshal (an exotic server value) still surfaces, with an empty-object schema
// — the call can still be made; only the model's argument hint is degraded, never the tool lost.
func newServerTool(serverAlias string, t *mcpsdk.Tool, caller toolCaller) serverTool {
	return serverTool{
		name:        qualifyToolName(serverAlias, t.Name),
		remoteName:  t.Name,
		description: t.Description,
		schema:      normaliseSchema(t.InputSchema),
		caller:      caller,
	}
}

// qualifyToolName joins a server alias and the server's tool name into the stable registry
// key. A tool from a server with an empty alias keeps its bare name (the degenerate single-
// unnamed-server case); otherwise the "<alias>__<name>" form keeps two servers' identically
// named tools distinct and tells the human which server a call reaches.
func qualifyToolName(serverAlias, remoteName string) string {
	if serverAlias == "" {
		return remoteName
	}
	return serverAlias + toolNameSeparator + remoteName
}

// normaliseSchema renders a server's input schema (an arbitrary JSON-marshalable value the SDK
// hands back as map[string]any) into json.RawMessage for domain.Tool.Schema. A nil or
// unmarshalable schema degrades to the empty object schema rather than failing — the model
// still sees a callable tool, just without an argument hint.
func normaliseSchema(input any) json.RawMessage {
	if input == nil {
		return emptyObjectSchema
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return emptyObjectSchema
	}
	return raw
}

// emptyObjectSchema is the safe fallback argument schema: a parameterless object, so a tool
// with no usable advertised schema is still presented as callable.
var emptyObjectSchema = json.RawMessage(`{"type":"object"}`)

// Name returns the registry-qualified identifier the model calls and the registry keys on.
func (t serverTool) Name() string { return t.name }

// Description returns the server's advertised description (untrusted presentation data shown to
// the model, never executed). An empty server description is given a minimal stand-in so the
// model is not handed a nameless capability.
func (t serverTool) Description() string {
	if strings.TrimSpace(t.description) == "" {
		return fmt.Sprintf("MCP tool %q (provided by an external MCP server).", t.remoteName)
	}
	return t.description
}

// Schema returns the server's input schema as JSON (untrusted presentation data).
func (t serverTool) Schema() json.RawMessage { return t.schema }

// ExternalEffect reports the mcp effect kind — the signal the dispatch disposition keys on to
// gate this tool through Approval in Auto under confine-to-workspace=true (it executes on an
// external server Apogee cannot fence) and to route it through Config.ExternalEffects for the
// bench's deterministic stub (ADR 0008).
func (t serverTool) ExternalEffect() domain.ExternalEffectKind { return domain.EffectMCP }

// Execute forwards the call to the server over the live session and renders the server's result
// for the model. The server's arguments are passed through as raw JSON; the server's content is
// flattened to text. A server-side tool error is surfaced as an error tool-result (IsError) the
// model can self-correct from — NOT a Go error, which the loop reserves for ctx cancellation
// (ADR 0007). Only a cancelled context (or a lost connection, which ctx cancellation subsumes)
// returns a Go error.
//
// Note: in production this Execute is typically NOT reached — the dispatch loop routes an
// ExternalEffectTool through Config.ExternalEffects when the host injects one (the bench's
// stub). Execute is the LIVE path the host uses when it injects no stub: a real call to a real
// server. Both paths are exercised — the stub in the bench, this live path in this package's
// in-memory-transport tests.
func (t serverTool) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}
	if t.caller == nil {
		// A surfaced tool with no live session can never run — surface it as an error result
		// rather than panicking (defensive: the Client always wires a caller).
		return errorResult(call.ID, "mcp: tool is not connected to a server"), nil
	}

	params := &mcpsdk.CallToolParams{Name: t.remoteName}
	if len(call.Arguments) > 0 {
		params.Arguments = json.RawMessage(call.Arguments)
	}

	res, err := t.caller.CallTool(ctx, params)
	if err != nil {
		if ctx.Err() != nil {
			return domain.ToolResult{}, ctx.Err()
		}
		// A transport / protocol error (tool missing, server gone) is surfaced to the model as
		// an error result so the Turn survives and the model can route around it (ADR 0007).
		return errorResult(call.ID, "mcp: call failed: "+err.Error()), nil
	}

	content := renderContent(res)
	if res.IsError {
		return errorResult(call.ID, content), nil
	}
	return okResult(call.ID, content), nil
}

// renderContent flattens an MCP CallToolResult's content blocks into the single text string
// Apogee's ToolResult carries. Text blocks are joined verbatim; a non-text block (image, audio,
// embedded resource) is rendered as a typed placeholder line so the model knows the server
// returned non-textual content without this client trying to interpret it. An empty result
// renders as an explicit note rather than a blank string.
func renderContent(res *mcpsdk.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return "(mcp tool returned no content)"
	}
	var b strings.Builder
	for i, c := range res.Content {
		if i > 0 {
			b.WriteString("\n")
		}
		switch v := c.(type) {
		case *mcpsdk.TextContent:
			b.WriteString(v.Text)
		default:
			fmt.Fprintf(&b, "[mcp %T content omitted]", c)
		}
	}
	return b.String()
}

// errorResult builds a tool-level failure result surfaced to the model (IsError), mirroring the
// internal/tools helper of the same intent — the loop reserves a Go error for ctx cancellation.
func errorResult(callID, message string) domain.ToolResult {
	return domain.ToolResult{CallID: callID, Content: message, IsError: true}
}

// okResult builds a successful tool result.
func okResult(callID, content string) domain.ToolResult {
	return domain.ToolResult{CallID: callID, Content: content}
}

// Compile-time proof serverTool satisfies the external-effect tool surface the dispatch
// disposition classifies as classMCP.
var (
	_ domain.Tool               = serverTool{}
	_ domain.ExternalEffectTool = serverTool{}
)
