package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/sidepanel"
)

// The /mcp pane — the process's MCP servers, as the MCP fold last saw them (ADR 0111 decision 9)
//
// This file holds the pane's content and its verb; everything else about it — the key contract, the
// scroll, the dismiss and the mouse — is the shared report module's (reportpane.go). The pane keeps no
// state beyond its reportPane: the rows are derived for the frame that asks for them from the MCP
// fold ([sessionView.mcpServers], folded from every domain.MCPServerEvent by foldEventAgainst and
// seeded at startup by [Model.seedMCP]), so a server changing state while the pane is up repaints
// on the next frame with nothing to keep in step. Under a watch that fold is the WATCHED process's,
// built from its journal's mcp_server lines, which is what makes the pane a watch verb (watchVerbs).
//
// It is read-only. Reconnecting a failed server stays on the /settings `mcp-servers` row
// (mcpreconnect.go); this pane only says what the dials last reported.
//
// The rows are a table — one heading row, then one row per server in the order the servers were
// first reported, a failed one's `failed` painted in errorText at paint time (mcpRowKind) — laid out here into single-cell rows rather than handed to the popup module as
// cells, because a failed or lost server's cause follows its row as wrapped prose, and a cause seated as a
// cell would widen a column of the table to its own length. The table's columns are measured by
// the popup module's own column arithmetic (popupColumnWidths, layoutPopupRow), so the layout is
// the one every celled pane has. Server, transport and cause are untrusted text: the fold strips
// them, and this file strips them again at render, which is idempotent.

// mcpTitle names the pane, and mcpHint spells the keys it owns — the report's four scroll keys and
// esc, with no ctrl+r: one rendering.
const (
	mcpTitle = "mcp servers"
	mcpHint  = "↑/↓ scroll · esc close"
)

// mcpEmptyRow is the whole pane when the fold holds no server. It is a ROW rather than a body, the
// shape adviceEmptyRow uses, so an empty pane scrolls, budgets and paints as every other report does.
const mcpEmptyRow = "no MCP servers configured"

// mcpCauseLead opens the first row of a failed or lost server's cause; the rows it wraps onto carry the
// readable wrap's own two-space indent (wrapReadable).
const mcpCauseLead = "  cause: "

// mcpHeaderCells is the table's heading row, one cell per column of [mcpServerCells].
var mcpHeaderCells = popupRow{"server", "transport", "source", "state", "tools"}

// mcpSource is the source column's word: where the server's configuration came from.
func mcpSource(row sidepanel.MCPServerRow) string {
	if row.Project {
		return "project"
	}
	return "global"
}

// mcpStateWord is the state column's words for one server: the phase the fold last saw, qualified
// where the phase alone would mislead — a failed reconnect whose previous connection still serves,
// and a session that a `url-safety:` change closed or that ended on its own (`disconnected (lost)`).
func mcpStateWord(row sidepanel.MCPServerRow) string {
	switch {
	case row.Phase == domain.MCPServerFailed && row.Kept:
		return "failed (previous connection kept)"
	case row.Phase == domain.MCPServerDisconnected && row.Reason != "":
		return "disconnected (" + string(row.Reason) + ")"
	}
	return string(row.Phase)
}

// mcpServerCells is one server's row of the table: name, transport, source, state and tool count.
func mcpServerCells(row sidepanel.MCPServerRow) popupRow {
	return popupRow{
		stripEscapes(row.Server),
		stripEscapes(row.Transport),
		mcpSource(row),
		mcpStateWord(row),
		plural(row.Tools, "tool"),
	}
}

// mcpRows composes the report at the given wrap column: the heading row, then per server its table
// row and, for a failed or lost one with a cause, the cause wrapped beneath it. An empty fold is ONE row.
func (m Model) mcpRows(column int) ([]popupRow, []popupRowKind) {
	servers := m.mcpServers.Rows()
	if len(servers) == 0 {
		return []popupRow{{mcpEmptyRow}}, []popupRowKind{popupRowPlain}
	}
	table := make([]popupRow, 0, len(servers)+1)
	table = append(table, mcpHeaderCells)
	for _, server := range servers {
		table = append(table, mcpServerCells(server))
	}
	widths := popupColumnWidths(m.th, table)

	rows := []popupRow{{layoutPopupRow(m.th, table[0], widths)}}
	kinds := []popupRowKind{popupRowHeading}
	for i, server := range servers {
		rows = append(rows, popupRow{layoutPopupRow(m.th, table[i+1], widths)})
		kinds = append(kinds, mcpRowKind(server))
		cause := strings.TrimRight(stripEscapes(server.Cause), "\n")
		if !hasMCPCause(server) || cause == "" {
			continue
		}
		for _, line := range wrapReadable(mcpCauseLead, cause, column) {
			rows = append(rows, popupRow{line})
			kinds = append(kinds, popupRowPlain)
		}
	}
	return rows, kinds
}

// mcpRowKind is how the popup module paints one server's table row: a failed server's as a failed
// row, its `failed` in errorText as the side panel's mcp section paints it, the cells themselves
// staying plain text (popupFailedLine); every other server's as a plain row.
func mcpRowKind(row sidepanel.MCPServerRow) popupRowKind {
	if row.Phase == domain.MCPServerFailed {
		return popupRowFailed
	}
	return popupRowPlain
}

// hasMCPCause reports whether row's phase carries a cause the pane words beneath it: a failed dial,
// or a session lost on its own — the one disconnect that was not closed on purpose.
func hasMCPCause(row sidepanel.MCPServerRow) bool {
	return row.Phase == domain.MCPServerFailed ||
		row.Phase == domain.MCPServerDisconnected && row.Reason == domain.MCPDisconnectLost
}

// mcpContent is what the pane tells the shared report module about itself for one frame
// (reportpane.go). The wrap column is the pane's real width for this frame
// ([Model.thinkingWrapColumn]), because report rows are truncated at the border and never re-wrapped.
func (m Model) mcpContent() reportContent {
	rows, kinds := m.mcpRows(m.thinkingWrapColumn())
	return reportContent{
		title:  mcpTitle,
		hint:   mcpHint,
		rowCap: maxInspectorRows,
		rows:   rows,
		kinds:  kinds,
	}
}

// runMCPCommand drives the /mcp verb: it opens the pane at the top and does nothing else.
// Synchronous like /usage — no engine call, no worker, no I/O — and safe while a worker works and
// while watching, because every row it shows was folded onto this view when its Event arrived. A
// second /mcp on an open pane re-opens it at the top; closing is esc or a click outside the box.
func (m Model) runMCPCommand() (tea.Model, tea.Cmd) {
	m.mcpPane = reportPane{open: true}
	return m, nil
}
