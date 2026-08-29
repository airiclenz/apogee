package tui

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Mouse-tracking re-assert (mousereassert.go)
// ----------------------------------------------------------------------------
//
// The renderer writes the tracking escapes only on a MouseMode DIFF, so once a tool child has
// reset the terminal's mouse reporting nothing in bubbletea ever says it again. These tests pin
// the two moments apogee says it itself — a tool result and a resize — the event that must NOT
// trigger one, the pre-ready floor, and the --tui-diag line that makes the whole thing visible in
// a bug report.

// reassertCount reports how many of the Msgs cmd delivers are the mouse-tracking re-assert,
// looking inside a tea.BatchMsg the way the runtime does (expandBatch, model_test.go): a re-assert
// batched with a record write is still a re-assert. It RUNS what it is given, so keep it away from
// a Cmd whose side effect a test also drives (a record write): counting it would perform it.
func reassertCount(t *testing.T, cmd tea.Cmd) int {
	t.Helper()
	n := 0
	for _, msg := range expandBatch(cmd) {
		if raw, isRaw := msg.(tea.RawMsg); isRaw && raw.Msg == mouseTrackingSeq {
			n++
		}
	}
	return n
}

// toolResult is a plain depth-0 tool result: one child finished with the terminal.
func toolResult() eventMsg {
	return eventMsg{Event: domain.ToolResultEvent{
		Result: domain.ToolResult{CallID: "t1", Content: "read 42 lines"},
	}}
}

// The exact bytes matter more than the fact of a write: what apogee re-asserts has to be the same
// sequence the renderer emits for MouseModeCellMotion, or the terminal is left in a mode the
// frame's MouseMode no longer describes.
func TestResizeReassertsMouseTrackingVerbatim(t *testing.T) {
	m := newModel(context.Background(), &fakeEngine{}, testOpts, nil)

	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	raw, isRaw := cmdMsg(cmd).(tea.RawMsg)
	if !isRaw {
		t.Fatalf("the resize answered %T, want a tea.RawMsg carrying the tracking sequence", cmdMsg(cmd))
	}
	if raw.Msg != "\x1b[?1002h\x1b[?1006h" {
		t.Errorf("re-asserted %q, want the cell-motion sequence \"\\x1b[?1002h\\x1b[?1006h\"", raw.Msg)
	}
}

// The tool result is the case the whole file exists for: the child that may have reset tracking
// has just reported back, so the sequence goes out on the spot.
func TestToolResultReassertsMouseTracking(t *testing.T) {
	m := newTestModel(t)

	_, cmd := m.Update(toolResult())

	if n := reassertCount(t, cmd); n != 1 {
		t.Errorf("a tool result produced %d re-asserts, want 1", n)
	}
}

// Everything else leaves the terminal alone. A streamed token arrives thousands of times a Turn
// and no child touched the tty between them, so a re-assert there would be pure noise on the wire.
func TestNonToolEventDoesNotReassertMouseTracking(t *testing.T) {
	m := newTestModel(t)

	_, cmd := m.Update(eventMsg{Event: domain.TokenEvent{Text: "still working"}})

	if n := reassertCount(t, cmd); n != 0 {
		t.Errorf("a token event produced %d re-asserts, want 0", n)
	}
}

// Before the first WindowSizeMsg the frame carries no mouse mode at all (View's placeholder), so a
// re-assert would enable reporting the renderer never turned on — and the escapes would arrive
// ahead of the alternate screen the first laid-out frame opens.
func TestPreReadyToolResultDoesNotReassertMouseTracking(t *testing.T) {
	m := newModel(context.Background(), &fakeEngine{}, testOpts, nil) // no WindowSizeMsg yet

	_, cmd := m.Update(toolResult())

	if n := reassertCount(t, cmd); n != 0 {
		t.Errorf("a pre-ready tool result produced %d re-asserts, want 0", n)
	}
}

// The diagnostic is the only way a bug report can say whether the escapes went out: --tui-diag
// records a running count, so each re-assert is a line of its own (the log's change suppression
// would collapse a constant value).
func TestDiagLogRecordsEveryMouseReassert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diag.txt")
	diag, err := newDiagLog(path)
	if err != nil {
		t.Fatalf("newDiagLog: %v", err)
	}
	m := newModel(context.Background(), &fakeEngine{}, testOpts, nil)
	m.diag = diag // Run wires it between newModel and the program; a test wires it the same way

	m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m = step(t, m, toolResult())

	if err := diag.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	var recorded []string
	for _, line := range strings.Split(readFileString(t, path), "\n") {
		if strings.HasPrefix(line, diagMouseReassert+":") {
			recorded = append(recorded, line)
		}
	}
	want := []string{"mouse-reassert: 1", "mouse-reassert: 2"}
	if len(recorded) != len(want) || recorded[0] != want[0] || recorded[1] != want[1] {
		t.Errorf("diag log recorded %q, want %q", recorded, want)
	}
}

// A depth-1 result carries BOTH duties: the progress save that re-persists the running delegation
// and the re-assert. They travel as one tea.Batch, which the runtime unpacks — so the drivers that
// stand in for it must too, or the write inside the batch is silently lost.
func TestRunWritesLandsARecordWriteBatchedWithTheReassert(t *testing.T) {
	host := &fakeSessionHost{}
	m := newBrowserModel(t, &fakeEngine{}, host, "/ws/a")
	seedConversation(&m)
	boundary := domain.Session{Version: domain.SessionVersion, State: json.RawMessage(`{"turn":1}`)}
	m, turnSave := stepCmd(t, m, turnSnapshotMsg{Sess: boundary})
	m = runWrites(t, m, turnSave) // the per-Turn save lands, so the progress save dispatches

	m, batched := stepCmd(t, m, delegateChildResult())

	// Only the SHAPE is read here: running a member to identify it would run the write, and this
	// test exists to prove that runWrites is what runs it. The re-assert's identity is pinned by
	// TestToolResultReassertsMouseTracking and by the coalescing test in sessionsave_test.go.
	batch, isBatch := cmdMsg(batched).(tea.BatchMsg)
	if !isBatch {
		t.Fatalf("the child's result answered %T, want a tea.BatchMsg of the progress save and the re-assert", cmdMsg(batched))
	}
	if len(batch) != 2 {
		t.Fatalf("the child's result batched %d Cmds, want 2 (the progress save and the re-assert)", len(batch))
	}
	m = runWrites(t, m, batched)
	if n := len(host.savedCalls()); n != 2 {
		t.Errorf("Save calls = %d, want 2 (the per-Turn save and the batched progress save)", n)
	}
}
