package tui

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/testtime"
)

// fakeMCPHost is a scripted MCPHost: the dials and the commit flag a test queues are handed out by
// the next TakeMCPWork, and every CommitMCP answers the scripted outcomes and pending flag.
type fakeMCPHost struct {
	mu           sync.Mutex
	dials        []MCPDial
	commitWanted bool
	outcomes     []MCPOutcome
	pending      bool
	commits      int
	cancels      int
	// retryNote is what every RetryFailedMCP answers, and retries counts the calls.
	retryNote string
	retries   int
	// states is the snapshot every MCPServerStates answers.
	states []domain.MCPServerEvent
}

func (f *fakeMCPHost) TakeMCPWork() ([]MCPDial, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	dials, commit := f.dials, f.commitWanted
	f.dials, f.commitWanted = nil, false
	return dials, commit
}

func (f *fakeMCPHost) CommitMCP() ([]MCPOutcome, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits++
	return f.outcomes, f.pending
}

// CancelMCPDials only counts the call: the dials these tests queue end through their own gates.
func (f *fakeMCPHost) CancelMCPDials() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels++
}

// RetryFailedMCP answers the scripted retryNote and counts the call.
func (f *fakeMCPHost) RetryFailedMCP() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retries++
	return f.retryNote, nil
}

// MCPServerStates answers the seeded snapshot, as a fresh slice.
func (f *fakeMCPHost) MCPServerStates() []domain.MCPServerEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.states)
}

func (f *fakeMCPHost) retryCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.retries
}

// queue adds dials for the next TakeMCPWork; with none, it queues removal-only work (a commit
// wanted with no dial behind it).
func (f *fakeMCPHost) queue(dials ...MCPDial) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dials = append(f.dials, dials...)
	f.commitWanted = f.commitWanted || len(dials) == 0
}

// script sets what every later CommitMCP answers.
func (f *fakeMCPHost) script(outcomes []MCPOutcome, pending bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outcomes, f.pending = outcomes, pending
}

func (f *fakeMCPHost) commitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.commits
}

func (f *fakeMCPHost) cancelCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cancels
}

// queuedCount is how many dials wait for the next TakeMCPWork.
func (f *fakeMCPHost) queuedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.dials)
}

// gatedDial is a dial that blocks until gate is closed or its ctx is cancelled.
func gatedDial(gate <-chan struct{}) MCPDial {
	return func(ctx context.Context) {
		select {
		case <-gate:
		case <-ctx.Done():
		}
	}
}

// instantDial is a dial that finishes at once.
func instantDial(context.Context) {}

// mcpModel builds a sized, idle Model over host.
func mcpModel(t *testing.T, host *fakeMCPHost) Model {
	t.Helper()
	opts := withTestUI(testOpts)
	opts.MCP = host
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	return step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
}

// startQueuedDials folds one keypress so Update's tail takes the queued work, returning the Model
// and the Cmd the dials ride on.
func startQueuedDials(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	return stepCmd(t, m, keyRune('x'))
}

// landDial runs cmd's tree, waits for its dial to return and folds the done Msg.
func landDial(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	return landDialFrom(t, m, runCmdTree(cmd))
}

// landDialFrom waits for a dial's done Msg on out, a Cmd tree already running, and folds it,
// failing the test when none lands within the budget.
func landDialFrom(t *testing.T, m Model, out <-chan tea.Msg) Model {
	t.Helper()
	expired := time.After(testtime.Budget(5 * time.Second))
	for {
		select {
		case msg := <-out:
			if _, isDone := msg.(mcpDialDoneMsg); isDone {
				return step(t, m, msg)
			}
		case <-expired:
			t.Fatal("no MCP dial landed")
			return m
		}
	}
}

// TestMCPDialKeysStillHandledWhileDialling: a dial blocked on its server leaves the Update loop
// free — typed keys reach the box — and nothing commits until the dial folds back.
func TestMCPDialKeysStillHandledWhileDialling(t *testing.T) {
	t.Parallel()
	host := &fakeMCPHost{}
	m := mcpModel(t, host)
	gate := make(chan struct{})
	host.queue(gatedDial(gate))

	m, cmd := startQueuedDials(t, m)
	done := runCmdTree(cmd)
	m = step(t, m, keyRune('y'))
	m = step(t, m, keyRune('z'))

	if got := m.input.Value(); got != "xyz" {
		t.Errorf("the box holds %q while dialling, want %q", got, "xyz")
	}
	if m.mcp.inFlight != 1 || host.commitCount() != 0 {
		t.Errorf("in flight %d, commits %d while dialling; want 1 and 0", m.mcp.inFlight, host.commitCount())
	}
	close(gate)
	m = landDialFrom(t, m, done)
	if m.mcp.inFlight != 0 || host.commitCount() != 1 {
		t.Errorf("in flight %d, commits %d after the dial landed; want 0 and 1", m.mcp.inFlight, host.commitCount())
	}
}

// TestMCPCommitWaitsForTheWorker: a dial that lands mid-Turn is committed only at the fold that ends
// the worker's Exchange.
func TestMCPCommitWaitsForTheWorker(t *testing.T) {
	t.Parallel()
	host := &fakeMCPHost{}
	m := mcpModel(t, host)
	startStubWorker(t, &m)
	host.queue(instantDial)

	m, cmd := startQueuedDials(t, m)
	m = landDial(t, m, cmd)
	if host.commitCount() != 0 || !m.mcp.uncommitted {
		t.Fatalf("commits %d, uncommitted %t mid-Turn; want 0 and true", host.commitCount(), m.mcp.uncommitted)
	}

	m = step(t, m, exchangeDoneMsg{})

	if host.commitCount() != 1 || m.mcp.uncommitted {
		t.Errorf("commits %d, uncommitted %t after the worker's done Msg; want 1 and false", host.commitCount(), m.mcp.uncommitted)
	}
}

// TestMCPCommitWritesEachOutcomeAsAnEphemeralNote: every outcome with a note writes it verbatim as
// a display-only note; an outcome with no note writes nothing.
func TestMCPCommitWritesEachOutcomeAsAnEphemeralNote(t *testing.T) {
	t.Parallel()
	host := &fakeMCPHost{}
	m := mcpModel(t, host)
	want := []string{"mcp docs reconnected", "mcp connect failed: web: connection refused"}
	host.script([]MCPOutcome{{Note: want[0]}, {RowFailure: "row only"}, {Note: want[1]}}, false)
	before := len(m.transcript.entries)
	host.queue()

	m = step(t, m, keyRune('x'))

	added := m.transcript.entries[before:]
	var notes []string
	for _, e := range added {
		if e.kind != entryNote {
			continue
		}
		notes = append(notes, e.text)
		if !e.ephemeral {
			t.Errorf("note %q is not ephemeral", e.text)
		}
	}
	if !slices.Equal(notes, want) {
		t.Errorf("the commit wrote notes %q, want %q", notes, want)
	}
}

// TestMCPPendingCommitRetriesAtTheNextIdlePoint: a commit the host answers pending keeps the commit
// wanted and writes no note; the next idle fold commits again and lands the result.
func TestMCPPendingCommitRetriesAtTheNextIdlePoint(t *testing.T) {
	t.Parallel()
	host := &fakeMCPHost{}
	m := mcpModel(t, host)
	host.script(nil, true)
	before := len(noteTexts(m))
	host.queue()

	m = step(t, m, keyRune('x'))
	if host.commitCount() != 1 || !m.mcp.uncommitted || len(noteTexts(m)) != before {
		t.Fatalf("after a pending commit: commits %d, uncommitted %t, notes %d (was %d); want 1, true and no new note",
			host.commitCount(), m.mcp.uncommitted, len(noteTexts(m)), before)
	}

	host.script([]MCPOutcome{{Note: "mcp docs reconnected"}}, false)
	m = step(t, m, keyRune('y'))
	m = step(t, m, keyRune('z'))

	if host.commitCount() != 2 || m.mcp.uncommitted {
		t.Errorf("commits %d, uncommitted %t after the retry; want 2 and false", host.commitCount(), m.mcp.uncommitted)
	}
	if got := lastNote(m); got != "mcp docs reconnected" {
		t.Errorf("last note %q, want the retried commit's note", got)
	}
}

// TestMCPCommitWaitsWhilePrebound: with no engine bound there is nothing to swap into, so the work
// is taken but the commit waits for a bound engine.
func TestMCPCommitWaitsWhilePrebound(t *testing.T) {
	t.Parallel()
	host := &fakeMCPHost{}
	m := mcpModel(t, host)
	m.opts.Prebound.Reason = domain.PreboundFirstBoot
	host.queue()

	m = step(t, m, keyRune('x'))
	if host.commitCount() != 0 || !m.mcp.uncommitted {
		t.Fatalf("commits %d, uncommitted %t while prebound; want 0 and true", host.commitCount(), m.mcp.uncommitted)
	}

	m.opts.Prebound = domain.PreboundStart{}
	m = step(t, m, keyRune('y'))

	if host.commitCount() != 1 {
		t.Errorf("commits %d once bound, want 1", host.commitCount())
	}
}

// TestMCPDialStartsWhileWatching: a watched save folded into the parked session starts its dial at
// once, but nothing commits into the inert engine; the commit lands once the watch ends.
func TestMCPDialStartsWhileWatching(t *testing.T) {
	t.Parallel()
	host := &fakeMCPHost{}
	f := newWatchFixture(t, "watched", recordHeadEntries(), 3)
	m := mcpModel(t, host)
	m, read := m.enterWatch(f.src)
	if !m.watch.isActive || read == nil {
		t.Fatal("setup: enterWatch did not enter watch mode")
	}
	m = step(t, m, read())
	host.queue(instantDial)

	m, cmd := stepCmd(t, m, configChangedMsg{alive: true})
	if m.mcp.inFlight != 1 {
		t.Fatalf("in flight %d after a watched save, want its dial started", m.mcp.inFlight)
	}
	m = landDial(t, m, cmd)
	if host.commitCount() != 0 || !m.mcp.uncommitted {
		t.Fatalf("commits %d, uncommitted %t while watching; want 0 and true", host.commitCount(), m.mcp.uncommitted)
	}

	m = step(t, m, keyEsc())

	if m.watch.isActive || host.commitCount() != 1 {
		t.Errorf("watching %t, commits %d after esc; want the watch left and one commit", m.watch.isActive, host.commitCount())
	}
}

// TestMCPRemovalOnlyWorkCommitsWithNoDial: work that only removes a server starts no dial and
// commits at the same fold.
func TestMCPRemovalOnlyWorkCommitsWithNoDial(t *testing.T) {
	t.Parallel()
	host := &fakeMCPHost{}
	m := mcpModel(t, host)
	host.queue()

	m = step(t, m, keyRune('x'))

	if m.mcp.inFlight != 0 || host.commitCount() != 1 || m.mcp.uncommitted {
		t.Errorf("in flight %d, commits %d, uncommitted %t; want 0, 1 and false",
			m.mcp.inFlight, host.commitCount(), m.mcp.uncommitted)
	}
}

// TestMCPNilHostIsInert: unwired, a stray done Msg and an ordinary fold leave the MCP state zero.
func TestMCPNilHostIsInert(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)

	m = step(t, m, mcpDialDoneMsg{})
	m = step(t, m, keyRune('x'))

	if m.mcp != (mcpState{}) {
		t.Errorf("MCP state %+v unwired, want zero", m.mcp)
	}
}

// TestMCPQuitCancelsDialsAndCommitsNothing: each of quitSession's exits — the busy stop, the
// deferred flush and the idle immediate one, which never sets quitting — cancels the dials in
// flight, and after it a dial's done Msg commits nothing and a later fold takes no queued work.
func TestMCPQuitCancelsDialsAndCommitsNothing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(t *testing.T, m *Model)
	}{
		{name: "busy stop", setup: func(t *testing.T, m *Model) { startStubWorker(t, m) }},
		{name: "deferred flush", setup: func(_ *testing.T, m *Model) { m.writeBusy = true }},
		{name: "idle immediate", setup: func(*testing.T, *Model) {}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host := &fakeMCPHost{}
			m := mcpModel(t, host)
			host.queue(instantDial)
			m, _ = startQueuedDials(t, m)
			if m.mcp.inFlight != 1 {
				t.Fatalf("setup: in flight %d, want the dial started", m.mcp.inFlight)
			}
			tc.setup(t, &m)

			m, _ = ctrlCQuit(t, m)

			if host.cancelCount() != 1 || !m.mcp.stopped {
				t.Fatalf("cancels %d, stopped %t after the quit; want 1 and true", host.cancelCount(), m.mcp.stopped)
			}
			m = step(t, m, mcpDialDoneMsg{})
			if host.commitCount() != 0 {
				t.Errorf("commits %d after a dial landed past the quit, want 0", host.commitCount())
			}
			host.queue(instantDial)
			m = step(t, m, keyRune('x'))
			if host.queuedCount() != 1 || host.commitCount() != 0 {
				t.Errorf("queued %d, commits %d after a later fold; want the work left untaken and no commit",
					host.queuedCount(), host.commitCount())
			}
		})
	}
}

// mcpServersRow is the `mcp-servers` row as the binary projects it: a structured key its own editor
// round trip edits.
func mcpServersRow() SettingRow {
	return SettingRow{
		Path: settingKeyMCPServers, Section: "Tools", Kind: SettingStructured, Value: "1 server",
		EditPointer: pointerForTest, ExternalEdit: true, Desc: "The MCP servers.",
	}
}

// mcpRowApplies records what the `mcp-servers` apply was asked: whether each ask was a watched save.
type mcpRowApplies struct {
	mu      sync.Mutex
	watched []bool
}

// apply answers every apply with the in-progress note, as the binary does for a request that dials.
func (a *mcpRowApplies) apply(_, _ string, watched bool) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.watched = append(a.watched, watched)
	return MCPReconnectingNote, nil
}

// mcpRowModel opens the settings pane over the `mcp-servers` row with host as the MCP seam; the
// row's round trip re-reads applied, and every apply goes to applies.
func mcpRowModel(t *testing.T, host *fakeMCPHost, applied []AppliedSetting, applies *mcpRowApplies) Model {
	t.Helper()
	rows := []SettingRow{mcpServersRow()}
	opts := testOpts
	opts.Settings = fakeSettingsHost{rows: func() []SettingRow { return rows }, applyWatched: applies.apply}
	edit := &externalEditLog{applied: applied}
	opts.Config = fakeConfigHost{externalEditSpec: edit.spec, reloadConfig: edit.reload}
	opts.MCP = host
	return openSettingsPane(t, newTestModelEng(t, &fakeEngine{}, opts))
}

// TestMCPRowShowsTheOutcomeWhenTheDialsLand: a changed round trip on the `mcp-servers` row applies
// as the row's own commit (not a watched save), the row reads `reconnecting MCP…` while the dial
// runs, and once it is committed the note clears — the value and its ` *` — or, on a failure, the
// row carries the joined failures while the pane is open, and nothing once it has closed.
func TestMCPRowShowsTheOutcomeWhenTheDialsLand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		outcomes    []MCPOutcome
		isPending   bool
		closesPane  bool
		wantFailure string
	}{
		{name: "success clears the note", outcomes: []MCPOutcome{{Note: "mcp docs reconnected"}}},
		{
			name: "failures join on the row",
			outcomes: []MCPOutcome{
				{Note: "mcp connect failed: docs: refused", RowFailure: "docs: refused"},
				{Note: "mcp connect failed: wiki: refused", RowFailure: "wiki: refused"},
			},
			wantFailure: settingsApplyFailedNote + "docs: refused; wiki: refused",
		},
		{
			name:        "a refused commit resolves on the refusal",
			outcomes:    []MCPOutcome{{RowFailure: "reconnect failed: busy — previous connections kept"}},
			isPending:   true,
			wantFailure: settingsApplyFailedNote + "reconnect failed: busy — previous connections kept",
		},
		{
			name:       "a closed pane shows no failure",
			outcomes:   []MCPOutcome{{Note: "mcp connect failed: docs: refused", RowFailure: "docs: refused"}},
			closesPane: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host := &fakeMCPHost{}
			applies := &mcpRowApplies{}
			m := mcpRowModel(t, host, []AppliedSetting{{Path: settingKeyMCPServers, Value: "2 servers"}}, applies)
			host.script(tc.outcomes, tc.isPending)
			host.queue(instantDial)

			m, cmd := stepCmd(t, m, settingsEditedMsg{path: settingKeyMCPServers})

			if got, want := m.settingsNote(mcpServersRow()), "· "+MCPReconnectingNote; got != want {
				t.Fatalf("row note while dialling = %q, want %q", got, want)
			}
			if !slices.Equal(applies.watched, []bool{false}) || host.retryCount() != 0 {
				t.Errorf("applies watched %v, retries %d; want one row commit and no retry", applies.watched, host.retryCount())
			}
			if tc.closesPane {
				m = step(t, m, keyEsc())
			}
			m = landDial(t, m, cmd)

			if m.mcp.row.isArmed {
				t.Error("the row watch is still armed after the dial was committed")
			}
			if edit, _ := m.settingEditOf(settingKeyMCPServers); edit.note != "" {
				t.Errorf("journaled note = %q, want it cleared", edit.note)
			}
			if got := m.settings.failure; got.msg != tc.wantFailure {
				t.Errorf("row failure = %q, want %q", got.msg, tc.wantFailure)
			}
			if tc.wantFailure == "" && !tc.closesPane {
				if got, want := m.settingsNote(mcpServersRow()), ""; got != want {
					t.Errorf("row note after the commit = %q, want the empty cell of a saved row", got)
				}
			}
		})
	}
}

// TestMCPRowUnchangedRoundTrip: a round trip that leaves `mcp-servers` unchanged applies nothing and
// asks RetryFailedMCP. A `reconnecting MCP…` answer arms the row and resolves once the dial lands;
// a "" answer leaves the row exactly as it was.
func TestMCPRowUnchangedRoundTrip(t *testing.T) {
	t.Parallel()
	t.Run("a retry arms the row and resolves", func(t *testing.T) {
		t.Parallel()
		host := &fakeMCPHost{retryNote: MCPReconnectingNote}
		applies := &mcpRowApplies{}
		m := mcpRowModel(t, host, nil, applies)
		host.script([]MCPOutcome{{Note: "mcp docs connected"}}, false)
		host.queue(instantDial)
		before := m.settingsNote(mcpServersRow())

		m, cmd := stepCmd(t, m, settingsEditedMsg{path: settingKeyMCPServers})

		if host.retryCount() != 1 || len(applies.watched) != 0 {
			t.Fatalf("retries %d, applies %d; want one retry and no apply", host.retryCount(), len(applies.watched))
		}
		if got, want := m.settingsNote(mcpServersRow()), "· "+MCPReconnectingNote; got != want {
			t.Fatalf("row note while retrying = %q, want %q", got, want)
		}
		m = landDial(t, m, cmd)
		if got := m.settingsNote(mcpServersRow()); got != before || m.mcp.row.isArmed {
			t.Errorf("row note after the retry = %q (armed %t), want %q and the watch resolved", got, m.mcp.row.isArmed, before)
		}
	})
	t.Run("nothing failed writes nothing", func(t *testing.T) {
		t.Parallel()
		host := &fakeMCPHost{}
		m := mcpRowModel(t, host, nil, &mcpRowApplies{})
		before := m.settingsNote(mcpServersRow())

		m = step(t, m, settingsEditedMsg{path: settingKeyMCPServers})

		if host.retryCount() != 1 {
			t.Errorf("retries %d, want 1", host.retryCount())
		}
		if got := m.settingsNote(mcpServersRow()); got != before || m.mcp.row.isArmed {
			t.Errorf("row note = %q (armed %t), want %q untouched", got, m.mcp.row.isArmed, before)
		}
	})
}

// TestMCPSeedReachesTheTUIFold: the host's snapshot is folded into the MCP fold at construction, in
// order — a connected server with its tool count, and a repo server startup skipped seeded failed
// with its cause — and a live Event then moves the seeded row as it would any other.
func TestMCPSeedReachesTheTUIFold(t *testing.T) {
	t.Parallel()
	host := &fakeMCPHost{states: []domain.MCPServerEvent{
		mcpConnected("files", 3),
		{Server: "repo", Transport: "stdio", Project: true, Phase: domain.MCPServerFailed,
			Cause: `server "repo" skipped: its pinned file moved`},
	}}
	m := mcpModel(t, host)

	rows := m.mcpServers.Rows()
	if len(rows) != 2 {
		t.Fatalf("seeded fold holds %+v, want files then repo", rows)
	}
	if rows[0].Server != "files" || rows[0].Phase != domain.MCPServerConnected || rows[0].Tools != 3 {
		t.Errorf("first row = %+v, want files connected with 3 tools", rows[0])
	}
	if rows[1].Server != "repo" || rows[1].Phase != domain.MCPServerFailed || !rows[1].Project ||
		rows[1].Cause != `server "repo" skipped: its pinned file moved` {
		t.Errorf("second row = %+v, want the skipped repo server failed with its cause", rows[1])
	}

	m = step(t, m, eventMsg{Event: mcpConnected("repo", 2)})

	if rows := m.mcpServers.Rows(); len(rows) != 2 || rows[1].Phase != domain.MCPServerConnected || rows[1].Tools != 2 {
		t.Errorf("after repo connected the fold holds %+v, want repo connected with 2 tools", rows)
	}
}

// TestMCPSeedNoHostSeedsNothing: with no MCP seam wired the fold starts empty.
func TestMCPSeedNoHostSeedsNothing(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	if rows := m.mcpServers.Rows(); len(rows) != 0 {
		t.Errorf("unwired fold holds %+v, want none", rows)
	}
}
