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

// CancelMCPDials is a no-op: the dials these tests queue end through their own gates.
func (f *fakeMCPHost) CancelMCPDials() {}

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
