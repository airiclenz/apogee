package main

// A watched `mcp-servers:` save dials the servers it adds off the Update loop (ADR 0109), so a slow
// or broken server never freezes the TUI or keeps a healthy one out.
//
// The session launches with no MCP servers and a save adds them. While a dial is held the status
// line carries the busy word and the prompt still echoes what is typed; once it lands, a per-server
// note says how it went and the new server's tool answers a model turn. A server that fails its
// dial is noted and stays absent, while the one saved beside it connects and works.
//
// The live Event journal follows the same lifecycle (ADR 0111 decision 8): each journal opens on the
// servers' current state right after run_started — at the first prompt, again after `/clear`, and at
// the boot of a resumed session — and a save's dials are journaled as they start and finish. A
// server that dies mid-session is noticed with no tool call (ADR 0111 decision 9): the journal
// records it disconnected with reason lost and `/mcp` reads `disconnected (lost)`.
//
// Driven in-process rather than through the pty for the reason e2e_mcpmidturn_test.go gives: the
// claim includes the dial's hand-off back to the Update goroutine, which only a process the race
// detector is watching can see.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/eventjson"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/testtime"
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

// liveJournal is the path and the decoded lines of the run's one live Event journal
// (`<home>/sessions/<id>.events.jsonl`, ADR 0103), read while the session runs — the owner removes
// it at quit. It is empty while no journal, or more than one, is open; a last line still being
// written ends the read, so a poll sees it on its next pass.
func liveJournal(t *testing.T, home string) (string, []eventjson.Line) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(home, "sessions", "*.events.jsonl"))
	if err != nil || len(paths) != 1 {
		return "", nil
	}
	body, err := os.ReadFile(paths[0])
	if err != nil {
		return "", nil
	}
	var lines []eventjson.Line
	for _, raw := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		line, err := eventjson.Decode([]byte(raw))
		if err != nil {
			break
		}
		lines = append(lines, line)
	}
	return paths[0], lines
}

// journalMCPEvents is every mcp_server line among lines, in order.
func journalMCPEvents(lines []eventjson.Line) []domain.MCPServerEvent {
	var events []domain.MCPServerEvent
	for _, line := range lines {
		if event, isMCP := line.Event.(domain.MCPServerEvent); isMCP {
			events = append(events, event)
		}
	}
	return events
}

// journalSeed is the mcp_server lines between a journal's run_started and its first prompt line —
// the seed its birth wrote — and whether the journal opened on run_started and holds that prompt.
func journalSeed(lines []eventjson.Line) ([]domain.MCPServerEvent, bool) {
	if len(lines) == 0 || lines[0].Started == nil {
		return nil, false
	}
	at := slices.IndexFunc(lines, func(line eventjson.Line) bool { return line.Prompt != nil })
	if at < 0 {
		return nil, false
	}
	seed := journalMCPEvents(lines[1:at])
	if len(seed) != at-1 {
		return nil, false
	}
	return seed, true
}

// journalOpening is the run of mcp_server lines immediately after a journal's run_started — the seed
// its birth wrote, read without journalSeed's prompt line, since a resumed session's journal is born
// at boot before any prompt — and whether the journal opened on run_started.
func journalOpening(lines []eventjson.Line) ([]domain.MCPServerEvent, bool) {
	if len(lines) == 0 || lines[0].Started == nil {
		return nil, false
	}
	var seed []domain.MCPServerEvent
	for _, line := range lines[1:] {
		event, isMCP := line.Event.(domain.MCPServerEvent)
		if !isMCP {
			break
		}
		seed = append(seed, event)
	}
	return seed, true
}

// isConnectedWith reports whether event says server connected with tools tools.
func isConnectedWith(event domain.MCPServerEvent, server string, tools int) bool {
	return event.Server == server && event.Phase == domain.MCPServerConnected && event.Tools == tools
}

// mcpSeedScript is the model script the seed tests run: the session title, then a reply per prompt.
func mcpSeedScript() stubllm.Script {
	return stubllm.Script{Model: "seed-model", Turns: []stubllm.Turn{
		{When: &stubllm.Match{LastMessage: copyTitleRequest}, Repeat: true, Text: "MCP seed"},
		{Text: "First reply."},
		{Text: "Second reply."},
	}}
}

// A server startup connected dialled before anything listened, so the journal born at the first
// prompt opens on its state: right after run_started, before the prompt line, the server is
// connected with its one tool (ADR 0111 decision 8).
func TestE2EMCPStartupStateSeedsTheJournal(t *testing.T) {
	t.Parallel()

	docs := newPingMCPServer(t, "docs")
	stub := stubllm.New(t, mcpSeedScript())
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, mcpMidTurnServers(docs.Endpoint))

	submit(drv, "Say something.")
	drv.WaitText("First reply.")
	waitIdle(drv)

	var seed []domain.MCPServerEvent
	drv.WaitFor(func() bool {
		_, lines := liveJournal(t, sess.Home())
		var isSeeded bool
		seed, isSeeded = journalSeed(lines)
		return isSeeded
	}, tuitest.Awaiting("the live journal's opening frame, seed and prompt"))
	if len(seed) != 1 || !isConnectedWith(seed[0], "docs", 1) {
		t.Errorf("the journal's seed is %+v; want docs connected with 1 tool", seed)
	}
	if err := sess.Quit(); err != nil {
		t.Errorf("the run returned %v; want a clean quit", err)
	}
}

// `/clear` ends the session's journal and the next prompt bears a new one: that one is seeded with
// the servers' state too, since the server stayed connected across the boundary.
func TestE2EMCPClearReseedsTheJournal(t *testing.T) {
	t.Parallel()

	docs := newPingMCPServer(t, "docs")
	stub := stubllm.New(t, mcpSeedScript())
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, mcpMidTurnServers(docs.Endpoint))

	submit(drv, "Say something.")
	drv.WaitText("First reply.")
	waitIdle(drv)
	var first string
	drv.WaitFor(func() bool {
		var lines []eventjson.Line
		first, lines = liveJournal(t, sess.Home())
		_, isSeeded := journalSeed(lines)
		return isSeeded
	}, tuitest.Awaiting("the first session's seeded journal"))

	submit(drv, "/clear")
	drv.WaitGone("First reply.")
	drv.WaitText(idlePromptHead)
	submit(drv, "Say more.")
	drv.WaitText("Second reply.")
	waitIdle(drv)

	var seed []domain.MCPServerEvent
	drv.WaitFor(func() bool {
		path, lines := liveJournal(t, sess.Home())
		var isSeeded bool
		seed, isSeeded = journalSeed(lines)
		return path != "" && path != first && isSeeded
	}, tuitest.Awaiting("the cleared session's own seeded journal"))
	if len(seed) != 1 || !isConnectedWith(seed[0], "docs", 1) {
		t.Errorf("the rotated journal's seed is %+v; want docs connected with 1 tool", seed)
	}
	if err := sess.Quit(); err != nil {
		t.Errorf("the run returned %v; want a clean quit", err)
	}
}

// A session resumed with --continue holds its record from construction, so its journal is born at
// boot, before any prompt — and opens on the servers' current state there too: right after
// run_started, one mcp_server line per configured server, each connected with its one tool, ahead of
// any other mcp_server line (ADR 0111 decision 8).
func TestE2EMCPResumeSeedsTheJournal(t *testing.T) {
	t.Parallel()

	docs := newPingMCPServer(t, "docs")
	wiki := newPingMCPServer(t, "wiki")
	stub := stubllm.New(t, mcpSeedScript())
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, "mcp-servers:\n"+
		mcpServerEntry("docs", docs.Endpoint)+
		mcpServerEntry("wiki", wiki.Endpoint))

	submit(drv, "Say something.")
	drv.WaitText("First reply.")
	waitIdle(drv)
	// The relaunch resumes the saved record, and a painted reply is no proof it has been written —
	// the idle save is a Cmd queued behind the reply's last frame.
	drv.WaitFor(func() bool {
		return len(sess.sessionRecords()) > 0 && strings.Contains(sessionRecordText(t, sess.Home()), "First reply.")
	}, tuitest.Awaiting("the reply to reach the session record"))
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}

	// The wiring births the resumed journal, seed and all, before the launch paints anything, so
	// the restored reply on screen means the opening run is on disk in full.
	next := sess.RelaunchWith("--continue")
	next.WaitText("First reply.")
	var seed []domain.MCPServerEvent
	next.WaitFor(func() bool {
		_, lines := liveJournal(t, sess.Home())
		var isOpened bool
		seed, isOpened = journalOpening(lines)
		return isOpened
	}, tuitest.Awaiting("the resumed session's journal, born at boot"))
	if len(seed) != 2 {
		t.Errorf("the resumed journal opens on %+v; want one mcp_server line per configured server", seed)
	}
	for _, server := range []string{"docs", "wiki"} {
		at := slices.IndexFunc(seed, func(event domain.MCPServerEvent) bool { return event.Server == server })
		if at < 0 || !isConnectedWith(seed[at], server, 1) {
			t.Errorf("the resumed journal's seed is %+v; want %s connected with 1 tool", seed, server)
		}
	}
	if err := sess.Quit(); err != nil {
		t.Errorf("the relaunched run returned %v; want a clean quit", err)
	}
}

// A settings save that adds a healthy and a broken server is journaled as it happens: each is
// dialling first, then the healthy one connected with its tool count and the broken one failed with
// the dial's cause. The journal born before the save had nothing to seed.
func TestE2EMCPReconnectAndFailureReachTheJournal(t *testing.T) {
	t.Parallel()

	broken := newBrokenMCPServer(t)
	healthy := newPingMCPServer(t, "healthy")
	stub := stubllm.New(t, mcpSeedScript())
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)

	submit(drv, "Say something.")
	drv.WaitText("First reply.")
	waitIdle(drv)
	drv.WaitFor(func() bool {
		_, lines := liveJournal(t, sess.Home())
		seed, isSeeded := journalSeed(lines)
		return isSeeded && len(seed) == 0
	}, tuitest.Awaiting("the journal born with no server to seed"))

	appendHomeConfig(t, sess.Home(), "mcp-servers:\n"+
		mcpServerEntry("broken", broken)+
		mcpServerEntry("healthy", healthy.Endpoint))
	drv.WaitText("mcp connect failed: broken: ")
	drv.WaitText("mcp healthy connected")

	var events []domain.MCPServerEvent
	drv.WaitFor(func() bool {
		_, lines := liveJournal(t, sess.Home())
		events = journalMCPEvents(lines)
		return len(events) >= 4
	}, tuitest.Awaiting("both servers' dial and finish in the journal"))
	for _, server := range []string{"healthy", "broken"} {
		var phases []domain.MCPServerEvent
		for _, event := range events {
			if event.Server == server {
				phases = append(phases, event)
			}
		}
		if len(phases) != 2 || phases[0].Phase != domain.MCPServerDialling {
			t.Errorf("%s journaled %+v; want dialling then its finish", server, phases)
			continue
		}
		switch server {
		case "healthy":
			if !isConnectedWith(phases[1], "healthy", 1) {
				t.Errorf("healthy finished %+v; want connected with 1 tool", phases[1])
			}
		case "broken":
			if phases[1].Phase != domain.MCPServerFailed || phases[1].Cause == "" || phases[1].Kept {
				t.Errorf("broken finished %+v; want failed with its cause, nothing kept", phases[1])
			}
		}
	}
	if err := sess.Quit(); err != nil {
		t.Errorf("the run returned %v; want a clean quit", err)
	}
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

// mcpLostBudget is how long a killed streamable-http server may take to be reported lost. The SDK
// retries the session's standalone stream five times, 1 s × 1.5^n apart plus up to as much jitter —
// about 13 s, at worst about 26 s — before it ends the session, so the budget sits well past that.
var mcpLostBudget = testtime.Budget(2 * time.Minute)

// mcpPaneTitle is the `/mcp` pane's title as it paints.
const mcpPaneTitle = "mcp servers"

// isLostServer reports whether event says server's session ended on its own.
func isLostServer(event domain.MCPServerEvent, server string) bool {
	return event.Server == server && event.Phase == domain.MCPServerDisconnected &&
		event.Reason == domain.MCPDisconnectLost
}

// journalLostAt is the index in lines of the first mcp_server line reporting server lost, or -1.
func journalLostAt(lines []eventjson.Line, server string) int {
	return slices.IndexFunc(lines, func(line eventjson.Line) bool {
		event, isMCP := line.Event.(domain.MCPServerEvent)
		return isMCP && isLostServer(event, server)
	})
}

// hasMCPPaneRow reports whether frame holds an `/mcp` table row for server reading state. The row's
// transport column is what tells it from the transcript's note, which words the same state.
func hasMCPPaneRow(frame tuitest.Frame, server, state string) bool {
	return slices.ContainsFunc(frame.Rows(), func(row string) bool {
		return strings.Contains(row, server) && strings.Contains(row, string(mcp.TransportStreamableHTTP)) &&
			strings.Contains(row, state)
	})
}

// A streamable-http server killed mid-session is noticed with no tool call: once the SDK gives up
// re-opening the session's standalone stream, the journal records the server disconnected with
// reason lost, after the seed its birth wrote, and `/mcp` reads `disconnected (lost)` (ADR 0111
// decision 9). A fresh session's journal is born at its first prompt, so one prompt and its seed
// come before the kill.
func TestE2EMCPServerDeathReachesMCPPane(t *testing.T) {
	t.Parallel()

	docs, srv := newPingMCPHTTPServer(t, "docs")
	stub := stubllm.New(t, mcpSeedScript())
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, mcpMidTurnServers(docs.Endpoint))

	submit(drv, "Say something.")
	drv.WaitText("First reply.")
	waitIdle(drv)
	drv.WaitFor(func() bool {
		_, lines := liveJournal(t, sess.Home())
		seed, isSeeded := journalSeed(lines)
		return isSeeded && len(seed) == 1 && isConnectedWith(seed[0], "docs", 1)
	}, tuitest.Awaiting("the live journal's seed, docs connected with 1 tool"))

	srv.CloseClientConnections()
	srv.Close()

	var lines []eventjson.Line
	drv.WaitFor(func() bool {
		_, lines = liveJournal(t, sess.Home())
		return journalLostAt(lines, "docs") >= 0
	}, tuitest.Within(mcpLostBudget), tuitest.Awaiting("the journal to report docs disconnected (lost)"))
	if _, isSeeded := journalSeed(lines); !isSeeded {
		t.Errorf("the journal no longer opens on its seed and prompt: %+v", lines)
	}
	prompt := slices.IndexFunc(lines, func(line eventjson.Line) bool { return line.Prompt != nil })
	if lost := journalLostAt(lines, "docs"); lost < prompt {
		t.Errorf("the lost line sits at %d, ahead of the prompt at %d; want it after the seed", lost, prompt)
	}

	submit(drv, "/mcp")
	drv.WaitText(mcpPaneTitle)
	drv.WaitFor(func() bool {
		return hasMCPPaneRow(drv.Frame(), "docs", "disconnected (lost)")
	}, tuitest.Awaiting("/mcp to read docs disconnected (lost)"))
	drv.Press(tuitest.Esc)
	drv.WaitGone(mcpPaneTitle)

	if err := sess.Quit(); err != nil {
		t.Errorf("the run returned %v; want a clean quit", err)
	}
}
