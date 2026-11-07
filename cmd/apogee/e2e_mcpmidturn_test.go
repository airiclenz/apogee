package main

// A watched `mcp-servers:` save that lands while a Turn is running dials nothing.
//
// The tool set is idle-only (Agent.SwapTools), and the save is folded on the Update goroutine while
// the worker is inside a Step. The reconnect therefore asks the engine first (CanSwapTools): the swap
// it would end in is refused, so no server is dialled for it, the row's outcome is the refusal it has
// always been, and the session stays on the tools it had. Before the probe, the reconnect dialled the
// new set mid-Turn only to tear it down again, and its swap read the Agent's Exchange state off the
// worker without a lock.
//
// Driven in-process rather than through the pty: the binary the pty tests launch is built without
// -race (main_test.go buildE2EBinary), and the race half of this claim is only visible to a process
// the race detector is watching.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
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

func TestE2EWatchedMCPSaveMidTurnDialsNothing(t *testing.T) {
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
	// The note posts after the key's apply has returned, in the same fold: by the time it paints, a
	// reconnect that was going to dial has dialled.
	drv.WaitText("config changed on disk — applied: mcp-servers")

	if got := secondRequests.Load(); got != 0 {
		t.Errorf("the repointed server saw %d request(s) from a save folded mid-Turn; want none", got)
	}

	stub.Release(mcpMidTurnGate)
	waitIdle(drv)
	if err := sess.Quit(); err != nil {
		t.Errorf("the run returned %v; want a clean quit", err)
	}
}
