package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeCaller is a toolCaller test double that records the params it was called with and returns a
// canned result — so serverTool's forward/render behaviour is tested without a live session.
type fakeCaller struct {
	gotParams *mcpsdk.CallToolParams
	result    *mcpsdk.CallToolResult
	err       error
}

func (f *fakeCaller) CallTool(_ context.Context, params *mcpsdk.CallToolParams) (*mcpsdk.CallToolResult, error) {
	f.gotParams = params
	return f.result, f.err
}

// TestQualifyToolName covers the registry-key qualification: an aliased server prefixes its tool
// names; an empty alias keeps the bare name (the degenerate single-server case).
func TestQualifyToolName(t *testing.T) {
	t.Parallel()
	if got := qualifyToolName("github", "search"); got != "github__search" {
		t.Errorf("qualifyToolName = %q; want github__search", got)
	}
	if got := qualifyToolName("", "search"); got != "search" {
		t.Errorf("qualifyToolName with empty alias = %q; want search", got)
	}
}

// TestNormaliseSchema asserts a usable schema round-trips and an absent/unmarshalable one degrades
// to the empty-object schema (so the tool is never lost, only its arg hint).
func TestNormaliseSchema(t *testing.T) {
	t.Parallel()
	got := normaliseSchema(map[string]any{"type": "object"})
	if !json.Valid(got) || !strings.Contains(string(got), "object") {
		t.Errorf("normaliseSchema lost a valid schema: %s", got)
	}
	if got := normaliseSchema(nil); string(got) != `{"type":"object"}` {
		t.Errorf("normaliseSchema(nil) = %s; want the empty-object fallback", got)
	}
	// A value that cannot marshal (a channel) degrades to the fallback rather than panicking.
	if got := normaliseSchema(make(chan int)); string(got) != `{"type":"object"}` {
		t.Errorf("normaliseSchema(unmarshalable) = %s; want the empty-object fallback", got)
	}
}

// TestServerToolDescriptionFallback asserts an empty server description gets a non-empty stand-in
// so the model is never handed a nameless capability.
func TestServerToolDescriptionFallback(t *testing.T) {
	t.Parallel()
	tool := newServerTool("srv", &mcpsdk.Tool{Name: "thing"}, &fakeCaller{})
	if strings.TrimSpace(tool.Description()) == "" {
		t.Errorf("empty server description produced an empty Description()")
	}
	if !strings.Contains(tool.Description(), "thing") {
		t.Errorf("fallback description %q does not name the tool", tool.Description())
	}
}

// TestExecuteForwardsArguments asserts the call's raw arguments are forwarded under the server's
// OWN (unqualified) tool name — the model addresses the qualified name, the server sees its own.
func TestExecuteForwardsArguments(t *testing.T) {
	t.Parallel()
	caller := &fakeCaller{result: &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "ok"}}}}
	tool := newServerTool("github", &mcpsdk.Tool{Name: "search"}, caller)

	_, err := tool.Execute(context.Background(), domain.ToolCall{
		ID:        "c",
		Tool:      "github__search",
		Arguments: json.RawMessage(`{"q":"hi"}`),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if caller.gotParams == nil || caller.gotParams.Name != "search" {
		t.Fatalf("forwarded tool name = %v; want the server's own name %q", caller.gotParams, "search")
	}
	raw, _ := json.Marshal(caller.gotParams.Arguments)
	if !strings.Contains(string(raw), `"q":"hi"`) {
		t.Errorf("forwarded arguments = %s; want the call's raw arguments", raw)
	}
}

// TestExecuteNilCaller asserts a surfaced tool with no live session surfaces an error result
// rather than panicking (defensive — the Client always wires a caller).
func TestExecuteNilCaller(t *testing.T) {
	t.Parallel()
	tool := newServerTool("srv", &mcpsdk.Tool{Name: "x"}, nil)
	res, err := tool.Execute(context.Background(), domain.ToolCall{ID: "c", Tool: "srv__x"})
	if err != nil {
		t.Fatalf("Execute with nil caller returned a Go error: %v", err)
	}
	if !res.IsError {
		t.Errorf("Execute with nil caller did not surface an error result")
	}
}
