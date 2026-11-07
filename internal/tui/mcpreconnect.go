package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

// MCPReconnectingNote is the word the busy indicator shows while an MCP server's dial is in flight
// (ADR 0109): one word whatever the count of servers.
const MCPReconnectingNote = "reconnecting MCP…"

// MCPHost is the TUI's seam onto the live MCP servers (ADR 0109): a save requests the servers it
// changes on the host, the host hands the Model the dials that request queued, the Model runs each
// off the Update loop, and the finished dials are taken in at the next idle point in one rebuild.
// Every method is called on the Update goroutine and returns at once; only an [MCPDial] blocks,
// and it runs on a Cmd goroutine. A nil host is inert: no work, no commit.
type MCPHost interface {
	// TakeMCPWork hands out the dials queued since the last call, each exactly once, and reports
	// whether a commit is wanted with no dial behind it (a removed server's tools must leave).
	TakeMCPWork() (dials []MCPDial, commit bool)
	// CommitMCP takes every finished dial into the live tool set in one rebuild and one tool swap,
	// returning one outcome per server it settled. pending reports that the swap was refused
	// because the engine was busy: every result is kept for the next commit and no outcome is
	// returned for it.
	CommitMCP() (outcomes []MCPOutcome, pending bool)
	// CancelMCPDials cancels every dial in flight; after it no dial the host handed out does work.
	CancelMCPDials()
}

// MCPDial is one server's dial. It blocks until the server answers, fails or ctx is cancelled, and
// stores its own result on the host for [MCPHost.CommitMCP]; it never touches the Model.
type MCPDial func(ctx context.Context)

// MCPOutcome is what one server's commit has to say. Note is the transcript line, written
// verbatim; RowFailure is the cause the mcp-servers settings row shows when the commit came from
// it. Either may be empty.
type MCPOutcome struct {
	Note, RowFailure string
}

// mcpState is the Model's half of the MCP reconnect: how many dials are running off the loop, and
// whether finished work still waits for a commit. Plain values, so it is safe inside the
// value-copied Model (ADR 0011).
type mcpState struct {
	// inFlight counts the dial Cmds started and not yet folded back.
	inFlight int
	// uncommitted reports that a dial finished, or removal-only work was taken, and no commit has
	// landed it yet — including a commit the host answered pending.
	uncommitted bool
	// stopped latches the quit: the dials were cancelled, and from then on no dial starts and
	// nothing commits. It is its own latch because quitSession's immediate exit never sets
	// quitting, so the holdQuitting gates alone would not cover it.
	stopped bool
}

// mcpDialDoneMsg is one dial Cmd's return: the dial has stored its result on the host.
type mcpDialDoneMsg struct{}

// mcpAfterFold is Update's tail hook for the MCP reconnect, asked after every fold: while dials may
// start it takes the host's queued work and runs each dial as a Cmd under the program context, and
// while the engine is free for a tool swap it commits what has finished. Asking here rather than at
// a list of sites is what makes the commit land at whichever fold first leaves the engine free —
// every engine release included — and what starts a dial requested mid-Turn at once. After a quit
// ([Model.stopMCP]) it does neither, whichever of the quit's exits was taken.
func mcpAfterFold(next tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m, ok := next.(Model)
	if !ok || m.opts.MCP == nil || m.mcp.stopped {
		return next, cmd
	}
	holds := m.engineHolds()
	if holds.canStartMCPDials() {
		var dials tea.Cmd
		m, dials = m.startMCPDials()
		cmd = tea.Batch(cmd, dials)
	}
	if m.mcp.uncommitted && holds.canSwapTools() {
		m.commitMCP()
	}
	return m, cmd
}

// startMCPDials takes the host's queued work: each dial becomes a Cmd counted in flight, and work
// with a commit behind it marks one wanted. The Cmd is nil when nothing was queued.
func (m Model) startMCPDials() (Model, tea.Cmd) {
	dials, commit := m.opts.MCP.TakeMCPWork()
	if commit {
		m.mcp.uncommitted = true
	}
	cmds := make([]tea.Cmd, 0, len(dials))
	for _, dial := range dials {
		cmds = append(cmds, mcpDialCmd(m.parent, dial))
	}
	m.mcp.inFlight += len(cmds)
	return m, tea.Batch(cmds...)
}

// mcpDialCmd runs dial off the Update loop under the program context, so a shutdown cancels a dial
// still waiting on its server; no deadline is added (a slow box makes the dial slower, never
// failed).
func mcpDialCmd(ctx context.Context, dial MCPDial) tea.Cmd {
	return func() tea.Msg {
		dial(ctx)
		return mcpDialDoneMsg{}
	}
}

// foldMCPDialDone counts one finished dial in: its result waits on the host for the next commit.
// Unwired, it is inert.
func (m Model) foldMCPDialDone() (tea.Model, tea.Cmd) {
	if m.opts.MCP == nil {
		return m, nil
	}
	if m.mcp.inFlight > 0 {
		m.mcp.inFlight--
	}
	m.mcp.uncommitted = true
	return m, nil
}

// stopMCP is the quit's half of the MCP reconnect: it cancels every dial in flight and latches
// stopped, so no later fold starts a dial or commits one. The teardown that closes the servers runs
// after the program returns, outside the Model. Unwired, it is inert.
func (m *Model) stopMCP() {
	if m.opts.MCP == nil {
		return
	}
	m.opts.MCP.CancelMCPDials()
	m.mcp.stopped = true
}

// commitMCP lands the finished work and writes each outcome's note, verbatim and display-only. A
// pending answer keeps the commit wanted, so the next fold that finds the engine free asks again.
func (m *Model) commitMCP() {
	outcomes, pending := m.opts.MCP.CommitMCP()
	m.mcp.uncommitted = pending
	for _, outcome := range outcomes {
		if outcome.Note != "" {
			m.transcript.addEphemeralNote(outcome.Note)
		}
	}
}
