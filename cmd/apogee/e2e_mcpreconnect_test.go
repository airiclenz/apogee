package main

// A watched `mcp-servers:` save dials the servers it adds off the Update loop (ADR 0109), so a slow
// or broken server never freezes the TUI or keeps a healthy one out.
//
// The session launches with no MCP servers and a save adds them. While a dial is held the status
// line carries the busy word and the prompt still echoes what is typed; once it lands, a per-server
// note says how it went and the new server's tool answers a model turn. A server that fails its
// dial is noted and stays absent, while the one saved beside it connects and works.
//
// Driven in-process rather than through the pty for the reason e2e_mcpmidturn_test.go gives: the
// claim includes the dial's hand-off back to the Update goroutine, which only a process the race
// detector is watching can see.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tui"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// mcpServerEntry is one `mcp-servers:` list item for a streamable-http server at endpoint.
func mcpServerEntry(name, endpoint string) string {
	return "    - name: " + name + "\n" +
		"      transport: streamable-http\n" +
		"      endpoint: " + endpoint + "\n"
}

// newGatedPingMCPServer is newPingMCPServer with every HTTP request held until the returned release
// is called — the initialize request included, so a dial of it does not finish until then. A held
// request also gives up when its client does. release closes the gate through one sync.Once, so the
// test may call it mid-run and the cleanup registered here calls it again without a second close.
// That cleanup is registered after the server's, so it runs first and frees every held handler
// before the server's Close waits on them.
func newGatedPingMCPServer(t *testing.T, name string) (cfg mcp.ServerConfig, release func()) {
	t.Helper()
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: name, Version: "v0.0.1"}, nil)
	server.AddTool(&mcpsdk.Tool{Name: "ping", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "pong"}}}, nil
		})
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
	gate := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-gate:
			handler.ServeHTTP(w, r)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	t.Cleanup(release)
	return mcp.ServerConfig{Name: name, Transport: mcp.TransportStreamableHTTP, Endpoint: srv.URL + "/mcp"}, release
}

// newBrokenMCPServer is an endpoint that answers every request with a 500, so a dial of it fails.
func newBrokenMCPServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "broken on purpose", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/mcp"
}

// pingThenDone is the model script both tests run: the session title, one call to tool, then a
// closing reply.
func pingThenDone(tool string) stubllm.Script {
	return stubllm.Script{Model: "reconnect-model", Turns: []stubllm.Turn{
		{When: &stubllm.Match{LastMessage: copyTitleRequest}, Repeat: true, Text: "MCP reconnect"},
		{ToolCalls: []stubllm.ToolCall{{ID: "call-ping", Name: tool, Arguments: "{}"}}},
		{Text: "Done."},
	}}
}

// answerPingAndAwaitPong allows the MCP tool call the model's turn raised — MCP tools ask in every
// mode — and waits until the tool's `pong` has gone back to the model.
func answerPingAndAwaitPong(drv *tuitest.Driver, stub *stubllm.Server) {
	drv.WaitText(approvalMarker)
	awaitArmed(drv)
	decide(drv, "a")
	drv.WaitFor(func() bool {
		return slices.ContainsFunc(stub.Requests(), func(r stubllm.Request) bool {
			return slices.ContainsFunc(r.Messages, func(m stubllm.Message) bool {
				return m.Role == "tool" && strings.Contains(m.Content, "pong")
			})
		})
	}, tuitest.Awaiting("the new server's tool to answer pong"))
}

// A server whose dial is held keeps the TUI live: the status line says `reconnecting MCP…` and the
// prompt echoes what is typed while the dial waits. Once the server answers, the note says it
// connected, the busy word goes, and a model turn calling its tool gets `pong`.
func TestE2EMCPSlowDialKeepsTheTUILive(t *testing.T) {
	t.Parallel()

	slow, release := newGatedPingMCPServer(t, "slow")
	stub := stubllm.New(t, pingThenDone("slow__ping"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)
	waitIdle(drv)

	appendHomeConfig(t, sess.Home(), "mcp-servers:\n"+mcpServerEntry("slow", slow.Endpoint))
	drv.WaitText("config changed on disk — applied: mcp-servers")
	drv.WaitText(tui.MCPReconnectingNote)

	const prompt = "Ping the slow server."
	drv.Type(prompt)
	awaitPrompt(drv, prompt)
	frame := drv.Frame().String()
	if !strings.Contains(frame, tui.MCPReconnectingNote) {
		t.Errorf("the busy word left before the held dial was released; frame:\n%s", frame)
	}
	if strings.Contains(frame, "mcp slow connected") {
		t.Errorf("the held server was noted connected before it answered; frame:\n%s", frame)
	}

	release()
	drv.WaitText("mcp slow connected")
	drv.WaitGone(tui.MCPReconnectingNote)

	drv.Press(tuitest.Enter)
	answerPingAndAwaitPong(drv, stub)
	waitIdle(drv)
	if err := sess.Quit(); err != nil {
		t.Errorf("the run returned %v; want a clean quit", err)
	}
}

// A save that adds a broken server beside a healthy one connects the healthy one regardless: the
// broken one is noted as a failed connect, the healthy one as connected, and its tool answers.
func TestE2EMCPBrokenServerSparesTheHealthyOne(t *testing.T) {
	t.Parallel()

	broken := newBrokenMCPServer(t)
	healthy := newPingMCPServer(t, "healthy")
	stub := stubllm.New(t, pingThenDone("healthy__ping"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)
	waitIdle(drv)

	appendHomeConfig(t, sess.Home(), "mcp-servers:\n"+
		mcpServerEntry("broken", broken)+
		mcpServerEntry("healthy", healthy.Endpoint))
	drv.WaitText("config changed on disk — applied: mcp-servers")
	drv.WaitText("mcp connect failed: broken: ")
	drv.WaitText("mcp healthy connected")
	drv.WaitGone(tui.MCPReconnectingNote)

	submit(drv, "Ping the healthy server.")
	answerPingAndAwaitPong(drv, stub)
	waitIdle(drv)
	if err := sess.Quit(); err != nil {
		t.Errorf("the run returned %v; want a clean quit", err)
	}
}
