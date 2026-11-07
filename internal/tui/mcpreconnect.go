package tui

import (
	"context"
	"strings"

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
	// whether a commit is wanted with no dial behind it (a removed server's tools must leave, or the
	// live servers' tools must take a new config order).
	TakeMCPWork() (dials []MCPDial, commit bool)
	// CommitMCP takes every finished dial into the live tool set in one rebuild and one tool swap,
	// returning one outcome per server it settled. pending reports that the swap was refused
	// because the engine was busy: every result is kept for the next commit, and the one outcome
	// returned is the refusal as a RowFailure with no Note.
	CommitMCP() (outcomes []MCPOutcome, pending bool)
	// CancelMCPDials cancels every dial in flight; after it no dial the host handed out does work.
	CancelMCPDials()
	// RetryFailedMCP is the `mcp-servers` row's retry gesture over an unchanged file (ADR 0109
	// decision 7): when some server's last dial failed it requests every failed server again and
	// answers [MCPReconnectingNote]; when none did it requests nothing and answers "". The error is
	// a refused request, with nothing requested.
	RetryFailedMCP() (note string, err error)
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
	// row watches the `mcp-servers` settings row while the dials its apply requested are in flight.
	row mcpRowWatch
}

// mcpRowWatch is the `mcp-servers` row's watch: armed by an apply or a retry that answered
// [MCPReconnectingNote], it collects each commit's row failures and resolves the row once no dial is
// in flight and nothing is left uncommitted (settleMCPRow). failures is the joined text rather than
// a slice, so a value-copied Model shares no array (ADR 0011).
type mcpRowWatch struct {
	isArmed  bool
	failures string
}

// mcpDialDoneMsg is one dial Cmd's return: the dial has stored its result on the host.
type mcpDialDoneMsg struct{}

// mcpAfterFold is Update's tail hook for the MCP reconnect, asked after every fold: while dials may
// start it takes the host's queued work and runs each dial as a Cmd under the program context, and
// while the engine is free for a tool swap it commits what has finished. Once no dial is in flight
// and nothing is left uncommitted — or the commit was refused — it resolves the watched
// `mcp-servers` row (settleMCPRow). Asking here rather than at
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
	isRefused := false
	if m.mcp.uncommitted && holds.canSwapTools() {
		isRefused = m.commitMCP()
	}
	if m.mcp.row.isArmed && m.mcp.inFlight == 0 && (!m.mcp.uncommitted || isRefused) {
		m.settleMCPRow()
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

// commitMCP lands the finished work and writes each outcome's note, verbatim and display-only, and
// hands each row failure to an armed row watch. A pending answer keeps the commit wanted, so the next
// fold that finds the engine free asks again; it reports whether the commit was refused.
func (m *Model) commitMCP() (pending bool) {
	outcomes, pending := m.opts.MCP.CommitMCP()
	m.mcp.uncommitted = pending
	for _, outcome := range outcomes {
		if outcome.Note != "" {
			m.transcript.addEphemeralNote(outcome.Note)
		}
		if outcome.RowFailure != "" && m.mcp.row.isArmed {
			m.mcp.row.failures = joinRowFailure(m.mcp.row.failures, outcome.RowFailure)
		}
	}
	return pending
}

// mcpRowFailureSeparator joins the failed servers' clauses on the row.
const mcpRowFailureSeparator = "; "

// joinRowFailure appends failure to the clauses already collected.
func joinRowFailure(collected, failure string) string {
	if collected == "" {
		return failure
	}
	return strings.Join([]string{collected, failure}, mcpRowFailureSeparator)
}

// armMCPRow starts watching the `mcp-servers` row afresh: a newer request's outcome is the one the
// row reports.
func (m *Model) armMCPRow() {
	m.mcp.row = mcpRowWatch{isArmed: true}
}

// settleMCPRow resolves the watched row with the dials' outcome. The in-progress note always clears
// — from the journaled edit and from the answer slot — so on success the row reads as the value and
// its ` *`, the pane's "saved". Any collected failure becomes the row's, while the settings surface
// is still open to show it.
func (m *Model) settleMCPRow() {
	failures := m.mcp.row.failures
	m.mcp.row = mcpRowWatch{}
	m.clearMCPReconnectingNote()
	if failures != "" && m.settings.open {
		m.settings.failure = settingFailure{
			path: settingKeyMCPServers,
			msg:  settingsApplyFailedNote + stripEscapes(failures),
		}
	}
}

// clearMCPReconnectingNote takes [MCPReconnectingNote] off the `mcp-servers` row wherever it was
// put: the journaled edit's note (rebuilt FRESH, the value-copied Model's rule, ADR 0011) and the
// answer slot.
func (m *Model) clearMCPReconnectingNote() {
	if m.settings.answer.path == settingKeyMCPServers && m.settings.answer.msg == MCPReconnectingNote {
		m.settings.answer = settingAnswer{}
	}
	edit, isEdited := m.settingEditOf(settingKeyMCPServers)
	if !isEdited || edit.note != MCPReconnectingNote {
		return
	}
	next := make([]settingEdit, 0, len(m.settingEdits))
	for _, e := range m.settingEdits {
		if e.path == settingKeyMCPServers {
			e.note = ""
		}
		next = append(next, e)
	}
	m.settingEdits = next
}
