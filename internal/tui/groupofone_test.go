package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// groupOfOneEntry is where the lone Read of a.go stands in every model below: entries[0] is the
// prompt that asked for it.
const groupOfOneEntry = 1

// groupOfOneModel is a Model holding one prompt and one lone Read of a.go — a group of one — in the
// state named: settled under auto-collapse (folded), settled with auto-collapse off (open), or live
// under auto-collapse while its Turn runs and its result is still out (open).
func groupOfOneModel(t *testing.T, state string) Model {
	t.Helper()
	m := newTestModel(t)
	m.transcript.reset()
	m.transcript.addUser("read the file", nil)
	switch state {
	case "live":
		m.transcript.setToolsAutoCollapse(true)
		m.transcript.setTurnLive(true)
		m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{
			ID: "lone", Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`)}})
	case "auto-collapse off":
		readCall(&m.transcript, "lone", "a.go", 1, 5, 0)
	default:
		m.transcript.setToolsAutoCollapse(true)
		readCall(&m.transcript, "lone", "a.go", 1, 5, 0)
		m.transcript.setTurnLive(false)
	}
	m.refreshViewport()
	return m
}

// groupOfOneLines is the plain paint of m's lines from the group of one's header down.
func groupOfOneLines(t *testing.T, m Model) []string {
	t.Helper()
	at := umbrellaHeaderLineOf(t, m, groupOfOneEntry)
	lines := make([]string, 0, len(m.lines)-at)
	for _, line := range m.lines[at:] {
		lines = append(lines, strings.TrimRight(collapseLeader(strip(line)), " "))
	}
	return lines
}

// assertGroupOfOneOpen fails unless the group of one paints open: its header wearing ▼, the call's
// own block directly beneath it — its ✦ header, then its `┕ a.go` row — and no type row.
func assertGroupOfOneOpen(t *testing.T, m Model) {
	t.Helper()
	lines := groupOfOneLines(t, m)
	frame := strings.Join(m.lines, "\n")
	if !strings.HasSuffix(lines[0], glyphExpanded) {
		t.Fatalf("the group header %q is not open:\n%s", lines[0], frame)
	}
	if len(lines) < 3 || !strings.HasSuffix(lines[1], "Read") || !strings.HasPrefix(lines[2], "  ┕ a.go") {
		t.Errorf("the open group of one does not paint the call's own block under its header:\n%s", frame)
	}
	if slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, "┕ Read") }) {
		t.Errorf("the open group of one paints a type row:\n%s", frame)
	}
}

// assertGroupOfOneFolded fails unless the group of one paints folded: its header wearing ▶ and none
// of the call's own block.
func assertGroupOfOneFolded(t *testing.T, m Model) {
	t.Helper()
	lines := groupOfOneLines(t, m)
	frame := strings.Join(m.lines, "\n")
	if !strings.HasSuffix(lines[0], defaultGlyphs.collapsed) {
		t.Errorf("the group header %q is not folded:\n%s", lines[0], frame)
	}
	if slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, "┕ a.go") }) {
		t.Errorf("the folded group of one still paints the call's block:\n%s", frame)
	}
}

// foldGroupOfOne makes the reader's fold gesture on the group of one's header — a click, or ⏎ with
// the block cursor standing on it — and returns the model after it.
func foldGroupOfOne(t *testing.T, m Model, way string) Model {
	t.Helper()
	if way == "mouse" {
		return clickCell(t, m, 4, screenRow(t, m, umbrellaHeaderLineOf(t, m, groupOfOneEntry)))
	}
	m = step(t, m, keyEsc())
	m = cursorOnTarget(t, m, targetUmbrella, groupOfOneEntry)
	return step(t, m, keyEnter())
}

// TestGroupOfOneFoldsOpensAndRefolds is the lone-call-no-refold defect's fix in every state a lone
// call stands in: its header folds and opens it on every gesture, by mouse and by ⏎, settled under
// auto-collapse (where it starts folded), with auto-collapse off and while its call is live (where
// it starts open) — the touch the first gesture leaves never strands it in either shape. The
// gestures end on an open group, which then settles under auto-collapse: still open, it carries the
// reader's touch, where an untouched one folds.
func TestGroupOfOneFoldsOpensAndRefolds(t *testing.T) {
	t.Parallel()

	for _, state := range []string{"settled", "auto-collapse off", "live"} {
		for _, way := range []string{"mouse", "enter"} {
			t.Run(state+" by "+way, func(t *testing.T) {
				t.Parallel()

				m := groupOfOneModel(t, state)
				startsFolded := state == "settled"
				gestures := 4
				if startsFolded {
					assertGroupOfOneFolded(t, m)
					gestures = 3
				} else {
					assertGroupOfOneOpen(t, m)
				}

				for gesture := 1; gesture <= gestures; gesture++ {
					m = foldGroupOfOne(t, m, way)
					if folded := startsFolded != (gesture%2 == 1); folded {
						assertGroupOfOneFolded(t, m)
					} else {
						assertGroupOfOneOpen(t, m)
					}
				}

				if state == "live" { // the lone call's result lands before its Turn ends
					m.transcript.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "lone",
						Content: "[File: a.go, 5 lines total, showing lines 1-5]\n…",
						Summary: domain.ReadSpan{Start: 1, End: 5, Total: 5}}})
				}
				m.transcript.setToolsAutoCollapse(true)
				m.transcript.setTurnLive(false)
				m.refreshViewport()
				if !settledAt(&m.transcript, groupOfOneEntry) {
					t.Fatal("setup: the group of one is not settled under auto-collapse")
				}
				assertGroupOfOneOpen(t, m)
			})
		}
	}
}

// TestGroupOfOneLiveHeaderSaysTheLiveVerb pins the live half of the header: while the lone call's
// result is out its Group summary reads in the live verb, over the call's own block whose target row
// has no outcome yet; the landed result turns it to the settled verb.
func TestGroupOfOneLiveHeaderSaysTheLiveVerb(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
		ID: "c1", Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`)}})

	want := strings.Join([]string{
		umbrellaHeaderRow("Reading 1 file", glyphExpanded),
		"✦ Read",
		"  ┕ a.go ⋯",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("live group of one:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1",
		Content: "[File: a.go, 5 lines total, showing lines 1-5]\n…",
		Summary: domain.ReadSpan{Start: 1, End: 5, Total: 5}}})
	if got, want := strings.Split(renderPlain(tr, 80), "\n")[0], umbrellaHeaderRow("Read 1 file", glyphExpanded); got != want {
		t.Errorf("settled group header = %q, want %q", got, want)
	}
}

// TestGroupOfOneGrowsIntoTypeRows pins the growth: the next Turn's call joins the group of one,
// which becomes an umbrella of two — one type row for the run, the call's own block gone from under
// the header — with no membership recorded anywhere that had to move.
func TestGroupOfOneGrowsIntoTypeRows(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	tr.setTurnLive(true)
	readCall(tr, "c1", "a.go", 1, 5, 0)
	tr.apply(turnBoundary(domain.EventBase{}))
	readCall(tr, "c2", "b.go", 1, 9, 0)

	want := strings.Join([]string{
		umbrellaHeaderRow("Read 2 files", glyphExpanded),
		groupMemberLine("  ┕ Read (2) ⋯ 14 lines"),
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("grown group:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
