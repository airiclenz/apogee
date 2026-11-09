package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// mcpTableRow spells one row of the /mcp table the way the pane must lay it out: the five columns
// padded to the widths the fixture's widest cells set, joined by the popup gutter, right-trimmed.
func mcpTableRow(server, transport, source, state, tools string) string {
	return strings.TrimRight(fmt.Sprintf("%-6s  %-9s  %-7s  %-33s  %s", server, transport, source, state, tools), " ")
}

// mcpFixtureEvents is four servers in every state the table words: connected, failed with a cause,
// a failed reconnect whose previous connection is kept, and dialling.
func mcpFixtureEvents() []domain.MCPServerEvent {
	return []domain.MCPServerEvent{
		{Server: "alpha", Transport: "stdio", Phase: domain.MCPServerConnected, Tools: 3},
		{Server: "beta", Transport: "sse", Project: true, Phase: domain.MCPServerFailed, Cause: "connection refused"},
		{Server: "gamma", Transport: "http", Phase: domain.MCPServerConnected, Tools: 2},
		{Server: "delta", Transport: "stdio", Phase: domain.MCPServerDialling},
		{Server: "gamma", Transport: "http", Phase: domain.MCPServerFailed, Cause: "timeout", Kept: true},
	}
}

// foldMCPEvents delivers each Event through the Update loop's own message, the path a live
// MCPServerEvent takes.
func foldMCPEvents(t *testing.T, m Model, events ...domain.MCPServerEvent) Model {
	t.Helper()
	for _, e := range events {
		m = step(t, m, eventMsg{Event: e})
	}
	return m
}

// mcpPaneModel folds `servers` connected servers and opens the /mcp pane at the top — more rows
// than it can seat, the state the shared scroll and hit-test drivers need a full window under.
func mcpPaneModel(t *testing.T, servers int) Model {
	t.Helper()
	m := newTestModel(t)
	for i := range servers {
		m = foldMCPEvents(t, m, mcpConnected(fmt.Sprintf("s%02d", i), 1))
	}
	m.mcpPane = reportPane{open: true}
	m.layout()
	return m
}

// mcpRowTexts is the pane's rows for this frame as plain text, one string per row.
func mcpRowTexts(m Model) []string {
	rows, _ := m.mcpRows(m.thinkingWrapColumn())
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		texts = append(texts, strings.Join(row, "|"))
	}
	return texts
}

// TestMCPPaneListsEveryServer drives /mcp typed at the prompt and pins the pane's exact rows: the
// heading, one row per server in first-reported order with transport, source, state and tool count,
// and a failed server's cause beneath it — `failed (previous connection kept)` with the kept tool
// count where a reconnect failed over a serving session.
func TestMCPPaneListsEveryServer(t *testing.T) {
	t.Parallel()
	m := foldMCPEvents(t, newTestModel(t), mcpFixtureEvents()...)
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 60}) // tall enough to seat every row

	m, cmd := typeCommand(t, m, "/mcp")

	if !m.mcpPane.open {
		t.Fatal("/mcp did not open its pane")
	}
	if m.state != stateIdle || cmd != nil {
		t.Errorf("state = %v, cmd = %v; /mcp drives no worker", m.state, cmd)
	}
	want := []string{
		mcpTableRow("server", "transport", "source", "state", "tools"),
		mcpTableRow("alpha", "stdio", "global", "connected", "3 tools"),
		mcpTableRow("beta", "sse", "project", "failed", "0 tools"),
		"  cause: connection refused",
		mcpTableRow("gamma", "http", "global", "failed (previous connection kept)", "2 tools"),
		"  cause: timeout",
		mcpTableRow("delta", "stdio", "global", "dialling", "0 tools"),
	}
	if got := mcpRowTexts(m); !reflect.DeepEqual(got, want) {
		t.Errorf("/mcp rows =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if _, kinds := m.mcpRows(m.thinkingWrapColumn()); kinds[0] != popupRowHeading || kinds[1] != popupRowPlain {
		t.Errorf("row kinds = %v; want the column heading first and plain rows under it", kinds)
	}
	if !m.openPanes().has(paneMCP) {
		t.Fatal("the open pane is not in the frame's pane set — it would be drawn on rows nothing budgeted")
	}
	painted := strip(m.frameOverlays().block(paneMCP))
	for _, line := range append([]string{mcpTitle}, want...) {
		if !strings.Contains(painted, strings.TrimSpace(line)) {
			t.Errorf("the painted pane lacks %q:\n%s", strings.TrimSpace(line), painted)
		}
	}

	m = step(t, m, keyEsc())
	if m.mcpPane.open || m.openPanes().has(paneMCP) {
		t.Error("esc did not close the /mcp pane")
	}
}

// TestMCPPaneSaysSoWithNoServers pins the empty pane — one row saying there is no server — and that
// the verb runs while a Turn runs, leaving the run as it was.
func TestMCPPaneSaysSoWithNoServers(t *testing.T) {
	t.Parallel()

	t.Run("no servers", func(t *testing.T) {
		t.Parallel()
		m, _ := typeCommand(t, newTestModel(t), "/mcp")
		if got := mcpRowTexts(m); !reflect.DeepEqual(got, []string{mcpEmptyRow}) {
			t.Errorf("empty /mcp rows = %q; want the one row %q", got, mcpEmptyRow)
		}
		if painted := strip(m.frameOverlays().block(paneMCP)); !strings.Contains(painted, mcpEmptyRow) {
			t.Errorf("the painted pane lacks %q:\n%s", mcpEmptyRow, painted)
		}
	})

	t.Run("while a Turn runs", func(t *testing.T) {
		t.Parallel()
		m := foldMCPEvents(t, runningModel(t), mcpConnected("alpha", 3))
		m, _ = typeCommand(t, m, "/mcp")
		if !m.mcpPane.open {
			t.Fatal("/mcp did not open its pane while a Turn runs")
		}
		if m.state != stateRunning {
			t.Errorf("state = %v; want the Turn still running", m.state)
		}
		if painted := strip(m.frameOverlays().block(paneMCP)); !strings.Contains(painted, "alpha") {
			t.Errorf("the painted pane lacks the server:\n%s", painted)
		}
	})
}

// TestMCPPaneUpdatesWhileOpen pins that the pane is derived at render: a server whose dial finishes
// while the pane is up is shown in its new state on the next frame, and a new server joins it.
func TestMCPPaneUpdatesWhileOpen(t *testing.T) {
	t.Parallel()
	m := foldMCPEvents(t, newTestModel(t), domain.MCPServerEvent{Server: "alpha", Transport: "stdio", Phase: domain.MCPServerDialling})
	m, _ = typeCommand(t, m, "/mcp")
	if painted := strip(m.frameOverlays().block(paneMCP)); !strings.Contains(painted, "dialling") {
		t.Fatalf("setup: the pane does not show the dialling server:\n%s", painted)
	}

	m = foldMCPEvents(t, m, mcpConnected("alpha", 4), mcpConnected("beta", 1))

	if !m.mcpPane.open {
		t.Fatal("an MCP Event closed the pane")
	}
	rows := mcpRowTexts(m)
	if len(rows) != 3 {
		t.Fatalf("rows = %q; want the heading and two servers", rows)
	}
	if got, want := strings.Fields(rows[1]), []string{"alpha", "stdio", "global", "connected", "4", "tools"}; !reflect.DeepEqual(got, want) {
		t.Errorf("alpha's row reads %q; want %q", got, want)
	}
	if got, want := strings.Fields(rows[2]), []string{"beta", "stdio", "global", "connected", "1", "tool"}; !reflect.DeepEqual(got, want) {
		t.Errorf("beta's row reads %q; want %q", got, want)
	}
	if painted := strip(m.frameOverlays().block(paneMCP)); !strings.Contains(painted, "4 tools") || strings.Contains(painted, "dialling") {
		t.Errorf("the painted pane does not show the finished dial:\n%s", painted)
	}
}

// TestMCPPaneStripsEscapes pins that the untrusted server name, transport and cause reach the pane
// with their terminal escapes and control bytes stripped.
func TestMCPPaneStripsEscapes(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.seedMCP([]domain.MCPServerEvent{{
		Server:    "evil\x1b[31mred",
		Transport: "std\x1b]0;title\x07io",
		Phase:     domain.MCPServerFailed,
		Cause:     "bad\x1b[2Jthing\x07",
	}})
	m, _ = typeCommand(t, m, "/mcp")

	rows := mcpRowTexts(m)
	for _, row := range rows {
		if strings.ContainsAny(row, "\x1b\x07") {
			t.Errorf("row %q carries an escape or control byte", row)
		}
	}
	joined := strings.Join(rows, "\n")
	for _, want := range []string{"evilred", "stdio", "  cause: badthing"} {
		if !strings.Contains(joined, want) {
			t.Errorf("rows lack %q:\n%s", want, joined)
		}
	}
}

// TestMCPPaneInWatchMode pins /mcp as a watch verb: it is not refused while watching, and it shows
// the WATCHED process's MCP fold, built from its journal's mcp_server lines, as they arrive.
func TestMCPPaneInWatchMode(t *testing.T) {
	t.Parallel()
	f := newWatchFixture(t, "watched", nil, 0)
	m := enterWatchOn(t, watchEngine(), f)

	m = step(t, typeText(t, m, "/mcp"), keyEnter())
	if m.flash == watchRefusedFlash || !m.mcpPane.open {
		t.Fatalf("/mcp was refused while watching (flash %q, open %t)", m.flash, m.mcpPane.open)
	}

	f.emit(mcpConnected("watched-server", 5))
	m = pollWatch(t, m)

	painted := strip(m.frameOverlays().block(paneMCP))
	for _, want := range []string{"watched-server", "connected", "5 tools"} {
		if !strings.Contains(painted, want) {
			t.Errorf("the pane under a watch lacks %q:\n%s", want, painted)
		}
	}
}
