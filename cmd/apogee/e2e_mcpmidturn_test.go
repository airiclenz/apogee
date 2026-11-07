package main

// A watched `mcp-servers:` save that lands while a Turn is running dials at once and swaps only at
// the next idle point.
//
// The tool set is idle-only (Agent.SwapTools), and the save is folded on the Update goroutine while
// the worker is inside a Step. The request dials the changed server off the Update loop straight away
// (ADR 0109 decision 3), but its commit waits for the Turn to end: until then nothing is swapped and
// the session stays on the tools it had, and the per-server note arrives once the swap has landed.
// Before the probe, the reconnect's swap read the Agent's Exchange state off the worker without a
// lock. A watched `url-safety:` save that closes a server's host mid-Turn is the other half: the
// server is disconnected at once, though the host-list swap waits for idle.
//
// Driven in-process rather than through the pty: the binary the pty tests launch is built without
// -race (main_test.go buildE2EBinary), and the race half of this claim is only visible to a process
// the race detector is watching.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The gate the prompt's reply is held at, and the prompt whose reply it holds.
const (
	mcpMidTurnGate   = "mid-turn"
	mcpMidTurnPrompt = "Keep working while the config moves."
)

// mcpMidTurnServers is the `mcp-servers:` block the run starts with, pointed at endpoint.
func mcpMidTurnServers(endpoint string) string {
	return "mcp-servers:\n" +
		"    - name: docs\n" +
		"      transport: streamable-http\n" +
		"      endpoint: " + endpoint + "\n"
}

// newCountingPingMCPServer is newPingMCPServer with a counter on every HTTP request the server is
// sent — a dial is an initialize request at the least, so a count of zero is a server nobody dialled.
func newCountingPingMCPServer(t *testing.T, name string) (mcp.ServerConfig, *atomic.Int64) {
	t.Helper()
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: name, Version: "v0.0.1"}, nil)
	server.AddTool(&mcpsdk.Tool{Name: "ping", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "pong"}}}, nil
		})
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return mcp.ServerConfig{Name: name, Transport: mcp.TransportStreamableHTTP, Endpoint: srv.URL + "/mcp"}, &requests
}

func TestE2EWatchedMCPSaveMidTurnSwapsOnlyAtIdle(t *testing.T) {
	t.Parallel()

	first := newPingMCPServer(t, "first")
	second, secondRequests := newCountingPingMCPServer(t, "second")
	stub := stubllm.New(t, stubllm.Script{Model: "midturn-model", Turns: []stubllm.Turn{
		{When: &stubllm.Match{LastMessage: copyTitleRequest}, Repeat: true, Text: "Mid-turn save"},
		{Await: mcpMidTurnGate, Text: "Done."},
	}})
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, mcpMidTurnServers(first.Endpoint))
	// A failure below must not leave the held reply holding the run open past the test.
	t.Cleanup(func() { stub.Release(mcpMidTurnGate) })

	submit(drv, mcpMidTurnPrompt)
	drv.WaitText(busyPlaceholder)
	// The placeholder paints before the worker's Step starts; the held request is logged from inside
	// it, ahead of the gate, so seeing it means the save below lands on a Step in progress.
	drv.WaitFor(func() bool {
		return slices.ContainsFunc(stub.Requests(), func(r stubllm.Request) bool { return r.TurnIndex == 1 })
	}, tuitest.Awaiting("the prompt's request to reach the held turn"))

	renameHomeConfig(t, sess.Home(), first.Endpoint, second.Endpoint)
	drv.WaitText("config changed on disk — applied: mcp-servers")
	// The dial does not wait for the Turn: the repointed server is reached while the reply is held.
	drv.WaitFor(func() bool { return secondRequests.Load() > 0 },
		tuitest.Awaiting("the repointed server to be dialled mid-Turn"))

	if strings.Contains(drv.Frame().String(), mcpMidTurnReconnected) {
		t.Error("the repointed server's tools were swapped in mid-Turn; want the swap held for the idle point")
	}

	stub.Release(mcpMidTurnGate)
	waitIdle(drv)
	drv.WaitText(mcpMidTurnReconnected)
	if err := sess.Quit(); err != nil {
		t.Errorf("the run returned %v; want a clean quit", err)
	}
}

// mcpMidTurnReconnected is the note the repointed server's commit writes once the swap has landed.
const mcpMidTurnReconnected = "mcp docs reconnected"

// The url-safety gate the prompt's tool call is held at.
const urlSafetyMidTurnGate = "deny-mid-turn"

// A watched `url-safety.deny-hosts` save that closes an MCP server's host while a Turn is running
// disconnects that server on the keystroke, before and regardless of the host-list swap — which the
// engine refuses mid-Turn (ADR 0109). The call the held reply then makes to the server's tool is
// refused with `mcp server "docs" is not connected` inside the same Turn, rather than reaching a
// connection the operator has just closed.
func TestE2EWatchedURLSafetyDenyMidTurnRefusesTheMCPTools(t *testing.T) {
	t.Parallel()

	docs := newPingMCPServer(t, "docs")
	stub := stubllm.New(t, stubllm.Script{Model: "midturn-model", Turns: []stubllm.Turn{
		{When: &stubllm.Match{LastMessage: copyTitleRequest}, Repeat: true, Text: "Mid-turn deny"},
		{Await: urlSafetyMidTurnGate, ToolCalls: []stubllm.ToolCall{{ID: "call-ping", Name: "docs__ping", Arguments: "{}"}}},
		{Text: "Done."},
	}})
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, mcpMidTurnServers(docs.Endpoint))
	// A failure below must not leave the held reply holding the run open past the test.
	t.Cleanup(func() { stub.Release(urlSafetyMidTurnGate) })

	submit(drv, mcpMidTurnPrompt)
	drv.WaitText(busyPlaceholder)
	// The held request is logged from inside the Step, ahead of the gate, so seeing it means the
	// save below lands on a Step in progress.
	drv.WaitFor(func() bool {
		return slices.ContainsFunc(stub.Requests(), func(r stubllm.Request) bool { return r.TurnIndex == 1 })
	}, tuitest.Awaiting("the prompt's request to reach the held turn"))

	appendHomeConfig(t, sess.Home(), "url-safety:\n  deny-hosts: [127.0.0.1]\n")
	drv.WaitText("config changed on disk — applied: url-safety.deny-hosts")

	stub.Release(urlSafetyMidTurnGate)
	drv.WaitText(approvalMarker)
	awaitArmed(drv)
	decide(drv, "a")

	refused := `mcp server "docs" is not connected`
	drv.WaitFor(func() bool {
		return slices.ContainsFunc(stub.Requests(), func(r stubllm.Request) bool {
			return slices.ContainsFunc(r.Messages, func(m stubllm.Message) bool {
				return m.Role == "tool" && strings.Contains(m.Content, refused)
			})
		})
	}, tuitest.Awaiting("the held Turn's call to the denied server to be refused"))

	waitIdle(drv)
	if err := sess.Quit(); err != nil {
		t.Errorf("the run returned %v; want a clean quit", err)
	}
}
