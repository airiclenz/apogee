package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// MCP allow-for-session at SERVER grain (item 3; ADR 0012 conformance)
// ----------------------------------------------------------------------------
//
// ADR 0012 promises MCP "allow for this session" caches at SERVER grain: approving one
// `github` tool allows `github.*` for the Session. resolve() keys a classMCP gate's
// allow-for-session cache on "mcp-server:<alias>" (the CacheKey seam), obtained through the
// optional ServerAlias() interface so internal/agent does not import internal/mcp. A classMCP
// tool that does not expose an alias falls back to the tool name — a tighten-only degradation.

// mcpServerTool is a fake ExternalEffectTool of kind mcp that ALSO exposes a ServerAlias — the
// optional interface the resolver keys the allow-for-session cache on at server grain. It mirrors
// internal/mcp's surfaced serverTool without importing that package (the resolver reaches the
// alias structurally). Two tools sharing an alias are two tools of one server.
type mcpServerTool struct {
	name  string
	alias string
	ran   *int
}

func (t mcpServerTool) Name() string                              { return t.name }
func (t mcpServerTool) Description() string                       { return t.name + " (mcp)" }
func (t mcpServerTool) Schema() json.RawMessage                   { return json.RawMessage(`{"type":"object"}`) }
func (t mcpServerTool) ExternalEffect() domain.ExternalEffectKind { return domain.EffectMCP }
func (t mcpServerTool) ServerAlias() string                       { return t.alias }
func (t mcpServerTool) Execute(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if t.ran != nil {
		*t.ran++
	}
	return domain.ToolResult{CallID: call.ID, Content: "ran"}, nil
}

// TestResolve_MCPGateCacheKeyServerGrain pins the CacheKey shape item 3 introduces: a gated MCP
// tool exposing a ServerAlias keys the allow-for-session cache on "mcp-server:<alias>" (server
// grain, ADR 0012); the empty-alias (single unnamed server) case is still one grain; and a
// classMCP tool that does NOT expose an alias falls back to the tool name (tighten-only).
func TestResolve_MCPGateCacheKeyServerGrain(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		tool     domain.Tool
		wantKey  string
		wantKind resolutionKind
	}{
		{"named server", mcpServerTool{name: "github__search", alias: "github"}, "mcp-server:github", resolveGate},
		{"empty alias is one grain", mcpServerTool{name: "thing", alias: ""}, "mcp-server:", resolveGate},
		// externalTool (dispatch_test.go) is classMCP but exposes NO ServerAlias, so it degrades
		// to the tool-name key — today's tighter grain, unchanged.
		{"no ServerAlias falls back to tool name", externalTool{name: "legacy_mcp", kind: domain.EffectMCP}, "legacy_mcp", resolveGate},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Auto + confine-to-workspace gates an MCP tool (unfenceable server), Approver present.
			got := resolve(resolutionInput{
				mode:               domain.ModeAuto,
				call:               domain.ToolCall{ID: "c1", Tool: tc.tool.Name()},
				tool:               tc.tool,
				guard:              proceed,
				confineToWorkspace: true,
				fsConfineAvailable: true,
				approverPresent:    true,
			})
			if got.kind != tc.wantKind {
				t.Fatalf("kind = %s, want %s", got.kind, tc.wantKind)
			}
			if got.cacheKey != tc.wantKey {
				t.Errorf("cacheKey = %q, want %q", got.cacheKey, tc.wantKey)
			}
			if got.force {
				t.Errorf("force = true; a plain MCP gate is not forced")
			}
		})
	}
}

// TestMCPServerGrain_SecondToolOfServerPreCleared proves the ADR 0012 behavior end-to-end:
// approving one of a server's tools "for the session" pre-clears its SIBLING tools — the second
// call runs without a second Approval prompt (server grain, not per-tool).
func TestMCPServerGrain_SecondToolOfServerPreCleared(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{}
	conf := &fakeConfiner{caps: capsBoth()}
	searchRan, createRan := 0, 0
	cfg := autoConfig(sink, conf, true,
		mcpServerTool{name: "github__search", alias: "github", ran: &searchRan},
		mcpServerTool{name: "github__create_issue", alias: "github", ran: &createRan},
	)
	approver := &fakeApprover{decision: domain.ApprovalAllowForSession}
	cfg.Approver = approver

	driveTwoToolCalls(t, cfg, sink,
		toolReq{"c1", "github__search", `{}`},
		toolReq{"c2", "github__create_issue", `{}`},
	)

	if approver.calls != 1 {
		t.Errorf("Approver consulted %d times; approving one github tool for the session must pre-clear its siblings (server grain, ADR 0012)", approver.calls)
	}
	if searchRan != 1 || createRan != 1 {
		t.Errorf("ran counts: search=%d create=%d; both must run (first approved, second pre-cleared)", searchRan, createRan)
	}
}

// TestMCPServerGrain_DifferentServerNotPreCleared proves the grain is per-SERVER, not global:
// approving a `github` tool for the session does NOT pre-clear a `jira` tool — the second server
// still prompts.
func TestMCPServerGrain_DifferentServerNotPreCleared(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{}
	conf := &fakeConfiner{caps: capsBoth()}
	githubRan, jiraRan := 0, 0
	cfg := autoConfig(sink, conf, true,
		mcpServerTool{name: "github__search", alias: "github", ran: &githubRan},
		mcpServerTool{name: "jira__search", alias: "jira", ran: &jiraRan},
	)
	approver := &fakeApprover{decision: domain.ApprovalAllowForSession}
	cfg.Approver = approver

	driveTwoToolCalls(t, cfg, sink,
		toolReq{"c1", "github__search", `{}`},
		toolReq{"c2", "jira__search", `{}`},
	)

	if approver.calls != 2 {
		t.Errorf("Approver consulted %d times; a different MCP server must NOT be pre-cleared by another server's approval", approver.calls)
	}
	if githubRan != 1 || jiraRan != 1 {
		t.Errorf("ran counts: github=%d jira=%d; both must run (each server prompts once, both allowed)", githubRan, jiraRan)
	}
}

// TestMCPServerGrain_ForcedGateStillPromptsOnCachedServer proves force semantics are unchanged
// under the server grain: once a server is cached for the session, a Tier-2 forced gate on one of
// its tools STILL prompts — a forced gate skips the allow-for-session cache entirely.
func TestMCPServerGrain_ForcedGateStillPromptsOnCachedServer(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{}
	conf := &fakeConfiner{caps: capsBoth()}
	searchRan, adminRan := 0, 0
	cfg := autoConfig(sink, conf, true,
		mcpServerTool{name: "github__search", alias: "github", ran: &searchRan},
		mcpServerTool{name: "github__admin", alias: "github", ran: &adminRan},
	)
	approver := &fakeApprover{decision: domain.ApprovalAllowForSession}
	cfg.Approver = approver

	driveTwoToolCalls(t, cfg, sink,
		toolReq{"c1", "github__search", `{}`},               // caches the github server for the session
		toolReq{"c2", "github__admin", `{"cmd":"sudo rm"}`}, // Tier-2 (sudo) forces a gate despite the cache
	)

	if approver.calls != 2 {
		t.Errorf("Approver consulted %d times; a forced gate must still prompt even when the server is cached for the session", approver.calls)
	}
	if searchRan != 1 || adminRan != 1 {
		t.Errorf("ran counts: search=%d admin=%d; both must run (each prompted, both allowed)", searchRan, adminRan)
	}
}
