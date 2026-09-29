package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
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
	tool := newServerTool("srv", &mcpsdk.Tool{Name: "thing"}, &fakeCaller{}, nil)
	if strings.TrimSpace(tool.Description()) == "" {
		t.Errorf("empty server description produced an empty Description()")
	}
	if !strings.Contains(tool.Description(), "thing") {
		t.Errorf("fallback description %q does not name the tool", tool.Description())
	}
}

// TestServerToolDeclaresNoArgRoles pins that an MCP server tool declares no argument roles,
// so the dangerous-action guard inspects every one of its arguments in full: a server
// cannot rename an action into a payload-shaped key (`body`, `message`) to slip past the
// hard-refuse floor, because the payload exemption is only ever the calling tool's own
// declaration.
func TestServerToolDeclaresNoArgRoles(t *testing.T) {
	t.Parallel()
	tool := newServerTool("srv", &mcpsdk.Tool{Name: "post"}, &fakeCaller{}, nil)

	if _, declares := domain.Tool(tool).(domain.ArgRoleTool); declares {
		t.Fatal("serverTool implements domain.ArgRoleTool; an MCP tool must declare no argument roles")
	}
	for _, role := range []domain.ArgRole{domain.ArgRolePayload, domain.ArgRoleReadSource, domain.ArgRolePrompt, domain.ArgRoleShellCommand} {
		if keys := domain.ArgKeysWithRole(tool, role); len(keys) != 0 {
			t.Errorf("ArgKeysWithRole(serverTool, %q) = %v, want none", role, keys)
		}
	}

	args, _ := json.Marshal(map[string]any{"body": "rm -rf ~/.ssh"})
	call := domain.ToolCall{ID: "c1", Tool: tool.Name(), Arguments: args}
	if d := security.DefaultDangerousActionGuard().Inspect(call, tool, nil); d.Tier != security.TierHardRefuse {
		t.Errorf("MCP body argument tier = %d (rule %q), want TierHardRefuse", d.Tier, d.RuleID)
	}
}

// TestExecuteForwardsArguments asserts the call's raw arguments are forwarded under the server's
// OWN (unqualified) tool name — the model addresses the qualified name, the server sees its own.
func TestExecuteForwardsArguments(t *testing.T) {
	t.Parallel()
	caller := &fakeCaller{result: &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "ok"}}}}
	tool := newServerTool("github", &mcpsdk.Tool{Name: "search"}, caller, nil)

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
	tool := newServerTool("srv", &mcpsdk.Tool{Name: "x"}, nil, nil)
	res, err := tool.Execute(context.Background(), domain.ToolCall{ID: "c", Tool: "srv__x"})
	if err != nil {
		t.Fatalf("Execute with nil caller returned a Go error: %v", err)
	}
	if !res.IsError {
		t.Errorf("Execute with nil caller did not surface an error result")
	}
}

// stallingTool connects an in-memory server advertising one tool, "stall", whose handler blocks
// until its request ctx ends (or the test finishes), and returns the surfaced tool — the silent,
// wedged server the per-call deadline exists for.
func stallingTool(t *testing.T) domain.Tool {
	t.Helper()
	release := make(chan struct{})
	tools := listFromInProcessServer(t, 0, func(server *mcpsdk.Server) {
		server.AddTool(
			&mcpsdk.Tool{Name: "stall", Description: "Never answers.", InputSchema: map[string]any{"type": "object"}},
			func(ctx context.Context, _ *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-release:
					return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "late"}}}, nil
				}
			},
		)
	})
	// Registered after the sessions' own cleanups, so it runs first and frees the handler.
	t.Cleanup(func() { close(release) })
	return findTool(t, tools, "many__stall")
}

// TestExecute_CallTimeoutIsErrorResult proves a server that never answers cannot hold a call past
// mcpCallTimeout: the call returns an error result naming the timeout and a nil Go error, so the
// Turn survives (ADR 0007). Not parallel: it shrinks the package-level mcpCallTimeout.
func TestExecute_CallTimeoutIsErrorResult(t *testing.T) {
	saved := mcpCallTimeout
	mcpCallTimeout = 100 * time.Millisecond
	t.Cleanup(func() { mcpCallTimeout = saved })
	tool := stallingTool(t)

	start := time.Now()
	res, err := tool.Execute(context.Background(), domain.ToolCall{ID: "c", Tool: "many__stall"})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute past the call deadline returned a Go error %v; want an error result", err)
	}
	if !res.IsError {
		t.Fatalf("Execute past the call deadline returned %+v; want an error result", res)
	}
	if !strings.Contains(res.Content, "timed out after 100ms") {
		t.Errorf("timeout result text = %q; want it to name the deadline", res.Content)
	}
	if bound := 20 * mcpCallTimeout; elapsed > bound {
		t.Errorf("Execute took %v; want it back within %v of a %v deadline", elapsed, bound, mcpCallTimeout)
	}
}

// TestExecute_CancelMidCallIsGoError proves the per-call deadline does not swallow the caller's
// own cancellation: a ctx cancelled while the server is still working returns the Go error
// context.Canceled, never an error result. Not parallel: it reads the mcpCallTimeout the timeout
// test shrinks.
func TestExecute_CancelMidCallIsGoError(t *testing.T) {
	tool := stallingTool(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(50*time.Millisecond, cancel)
	defer timer.Stop()

	_, err := tool.Execute(ctx, domain.ToolCall{ID: "c", Tool: "many__stall"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute with a ctx cancelled mid-call returned err %v; want context.Canceled", err)
	}
}
