package sidepanel

import (
	"slices"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/sanitize"
)

// MCPServerRow is one MCP server as the fold last saw it (ADR 0111 decisions 9 and 10). Server and
// Transport are escape-stripped to one line and Cause escape-stripped, since all three are untrusted
// text; a Driver still strips them again at render, which is idempotent.
//
// Phase is the last lifecycle phase reported. Project says the server comes from the Project config
// rather than the global one. Tools is the tool count of the connection that serves: set by a
// connected phase, kept through a dialling one and a failed one whose previous connection was Kept,
// and 0 once no connection serves. Cause is the failure's text on a failed phase, "" otherwise.
// Reason says why a disconnected phase closed the session; a removed server has no row at all.
type MCPServerRow struct {
	Server    string
	Transport string
	Project   bool
	Phase     domain.MCPServerPhase
	Tools     int
	Cause     string
	Kept      bool
	Reason    domain.MCPDisconnectReason
}

// MCPServers is the folded MCP server state of one process, keyed by server name, the rows in the
// order their servers were first reported. Every Driver view folds the same MCPServerEvents into
// it — live, a watched journal's, and the startup seed — so they all answer the same rows.
//
// It is a plain value, safe in a value-copied holder (ADR 0011): Fold replaces its slice whole and
// never writes into the one a copy still holds. The zero value holds no server.
type MCPServers struct {
	rows []mcpEntry
}

// mcpEntry is one row with the raw server name it is keyed by: the row's own Server is stripped,
// and two raw names that strip alike are still two servers.
type mcpEntry struct {
	key string
	row MCPServerRow
}

// Fold applies one MCPServerEvent. A disconnected server removed from the configuration drops its
// row; every other phase sets or updates the server's row.
func (s *MCPServers) Fold(event domain.MCPServerEvent) {
	at := slices.IndexFunc(s.rows, func(entry mcpEntry) bool { return entry.key == event.Server })
	if event.Phase == domain.MCPServerDisconnected && event.Reason == domain.MCPDisconnectRemoved {
		if at >= 0 {
			s.rows = slices.Delete(slices.Clone(s.rows), at, at+1)
		}
		return
	}
	var previous MCPServerRow
	if at >= 0 {
		previous = s.rows[at].row
	}
	next := slices.Clone(s.rows)
	entry := mcpEntry{key: event.Server, row: foldMCPRow(previous, event)}
	if at >= 0 {
		next[at] = entry
	} else {
		next = append(next, entry)
	}
	s.rows = next
}

// foldMCPRow is previous moved by event: the identity and phase fields are the event's, the tool
// count follows the connection that serves (MCPServerRow).
func foldMCPRow(previous MCPServerRow, event domain.MCPServerEvent) MCPServerRow {
	row := MCPServerRow{
		Server:    sanitize.StripEscapesToLine(event.Server),
		Transport: sanitize.StripEscapesToLine(event.Transport),
		Project:   event.Project,
		Phase:     event.Phase,
		Tools:     previous.Tools,
		Kept:      event.Kept,
		Reason:    event.Reason,
	}
	switch event.Phase {
	case domain.MCPServerConnected:
		row.Tools = event.Tools
	case domain.MCPServerFailed:
		row.Cause = sanitize.StripEscapes(event.Cause)
		if !event.Kept {
			row.Tools = 0
		}
	case domain.MCPServerDisconnected:
		row.Tools = 0
	}
	return row
}

// Rows answers every server's row in the order the servers were first reported, as a fresh slice.
func (s MCPServers) Rows() []MCPServerRow {
	rows := make([]MCPServerRow, 0, len(s.rows))
	for _, entry := range s.rows {
		rows = append(rows, entry.row)
	}
	return rows
}

// IsLive reports whether any server's row is not connected: one dialling, failed or disconnected.
func (s MCPServers) IsLive() bool {
	return slices.ContainsFunc(s.rows, func(entry mcpEntry) bool {
		return entry.row.Phase != domain.MCPServerConnected
	})
}
