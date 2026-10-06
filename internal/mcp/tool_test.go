package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
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

// providerToolNamePattern is the tool-name pattern the model providers enforce.
var providerToolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// TestModelToolName covers the sanitise rule: a name inside the provider pattern is unchanged, and
// any other name comes out inside it, carrying the hash suffix that keeps it distinct.
func TestModelToolName(t *testing.T) {
	t.Parallel()
	long := "srv__" + strings.Repeat("a", 75)
	cases := []struct {
		name      string
		qualified string
		wantExact string // "" = only the pattern and prefix are asserted
		wantStart string
	}{
		{name: "valid name unchanged", qualified: "github__search", wantExact: "github__search"},
		{name: "valid hyphenated name unchanged", qualified: "a-b__c-d", wantExact: "a-b__c-d"},
		{name: "exactly 64 valid unchanged", qualified: strings.Repeat("b", 64), wantExact: strings.Repeat("b", 64)},
		{name: "dot replaced and hashed", qualified: "files.read", wantStart: "files_read_"},
		{name: "non-ASCII replaced per character", qualified: "srv__größe", wantStart: "srv__gr__e_"},
		{name: "80 characters truncated and hashed", qualified: long, wantStart: "srv__aaaa"},
		{name: "65 valid characters truncated and hashed", qualified: strings.Repeat("c", 65), wantStart: "ccc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := modelToolName(tc.qualified)
			if !providerToolNamePattern.MatchString(got) {
				t.Fatalf("modelToolName(%q) = %q; outside the provider pattern", tc.qualified, got)
			}
			if tc.wantExact != "" && got != tc.wantExact {
				t.Errorf("modelToolName(%q) = %q; want %q", tc.qualified, got, tc.wantExact)
			}
			if tc.wantStart != "" && !strings.HasPrefix(got, tc.wantStart) {
				t.Errorf("modelToolName(%q) = %q; want prefix %q", tc.qualified, got, tc.wantStart)
			}
			if again := modelToolName(tc.qualified); again != got {
				t.Errorf("modelToolName(%q) is unstable: %q then %q", tc.qualified, got, again)
			}
		})
	}
}

// TestServerToolNameDispatchesRemoteName asserts a tool whose qualified name had to be sanitised is
// offered under a valid name yet forwards the call under the server's own tool name.
func TestServerToolNameDispatchesRemoteName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		alias  string
		remote string
	}{
		{name: "dotted name", alias: "files", remote: "files.read"},
		{name: "80-character qualified name", alias: "srv", remote: strings.Repeat("x", 75)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			caller := &fakeCaller{result: &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "ok"}}}}
			tool := newServerTool(tc.alias, &mcpsdk.Tool{Name: tc.remote}, caller, nil)
			if !providerToolNamePattern.MatchString(tool.Name()) {
				t.Fatalf("Name() = %q; outside the provider pattern", tool.Name())
			}
			if _, err := tool.Execute(context.Background(), domain.ToolCall{ID: "c", Tool: tool.Name()}); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if caller.gotParams == nil || caller.gotParams.Name != tc.remote {
				t.Fatalf("forwarded tool name = %v; want the server's own name %q", caller.gotParams, tc.remote)
			}
		})
	}
}

// TestServerToolNameDistinct asserts remote tools whose qualified names sanitise alike still get
// distinct model-facing names: two tools on one server, and one tool behind two aliases.
func TestServerToolNameDistinct(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		aliasA, toolA string
		aliasB, toolB string
	}{
		{name: "one server, dotted and underscored tool", aliasA: "files", toolA: "files.read", aliasB: "files", toolB: "files_read"},
		{name: "two servers, aliases a.b and a_b", aliasA: "a.b", toolA: "search", aliasB: "a_b", toolB: "search"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := newServerTool(tc.aliasA, &mcpsdk.Tool{Name: tc.toolA}, nil, nil)
			b := newServerTool(tc.aliasB, &mcpsdk.Tool{Name: tc.toolB}, nil, nil)
			for _, tool := range []serverTool{a, b} {
				if !providerToolNamePattern.MatchString(tool.Name()) {
					t.Fatalf("Name() = %q; outside the provider pattern", tool.Name())
				}
			}
			if a.Name() == b.Name() {
				t.Errorf("both tools are offered as %q; want distinct model-facing names", a.Name())
			}
		})
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

// TestServerToolDescriptionCap pins the 8 KiB description cap: a description one byte past it is
// clipped to the cap and marked; one exactly at it is returned unchanged.
func TestServerToolDescriptionCap(t *testing.T) {
	t.Parallel()
	marker := fmt.Sprintf(mcpDescriptionTruncatedMarker, maxMCPToolDescriptionBytes)

	t.Run("one byte past the cap is clipped and marked", func(t *testing.T) {
		t.Parallel()
		tool := newServerTool("srv", &mcpsdk.Tool{Name: "long", Description: strings.Repeat("d", maxMCPToolDescriptionBytes+1)}, &fakeCaller{}, nil)

		got := tool.Description()

		if want := strings.Repeat("d", maxMCPToolDescriptionBytes) + marker; got != want {
			t.Errorf("Description() has length %d; want the %d-byte cap followed by %q", len(got), maxMCPToolDescriptionBytes, marker)
		}
	})

	t.Run("exactly the cap is unchanged", func(t *testing.T) {
		t.Parallel()
		description := strings.Repeat("d", maxMCPToolDescriptionBytes)
		tool := newServerTool("srv", &mcpsdk.Tool{Name: "full", Description: description}, &fakeCaller{}, nil)

		if got := tool.Description(); got != description {
			t.Errorf("Description() has length %d; want the %d-byte description unchanged", len(got), len(description))
		}
	})
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
