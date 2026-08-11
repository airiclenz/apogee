package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The chord that opens the walk. Alt rather than a bare key because ↑/↓ are already the prompt's
// (recall, the caret), which is the whole reason the cursor is modal.
func keyAltUp() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModAlt} }
func keyAltDown() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt} }

// cursorLine is the content line the highlight is standing on, with the mode asserted on the way —
// every claim below is about WHERE the cursor is, so a test that lost the mode must say that rather
// than compare a zero line against one.
func cursorLine(t *testing.T, m Model) int {
	t.Helper()
	if !m.cursor.active {
		t.Fatal("the block cursor is not active")
	}
	return m.cursor.line
}

// TestBlockCursorEntersAtTheEndItsKeyPointsAwayFrom is the way in (design call 7): the mode is
// entered by ⌥↑/⌥↓ alone, and because the cursor arrives from OUTSIDE the transcript each key lands
// on the end it is pointing away from — ⌥↑ on the newest block, ⌥↓ on the oldest.
func TestBlockCursorEntersAtTheEndItsKeyPointsAwayFrom(t *testing.T) {
	t.Parallel()

	base := modelWithToolGroup(t)
	stops := cursorStops(base.lineTargets)
	if len(stops) < 2 {
		t.Fatalf("setup: the fixture offers %d stops, too few to tell the two ends apart", len(stops))
	}

	if m := step(t, base, keyAltUp()); cursorLine(t, m) != stops[len(stops)-1] {
		t.Errorf("⌥↑ entered at line %d, want the last stop %d", m.cursor.line, stops[len(stops)-1])
	}
	if m := step(t, base, keyAltDown()); cursorLine(t, m) != stops[0] {
		t.Errorf("⌥↓ entered at line %d, want the first stop %d", m.cursor.line, stops[0])
	}
	if base.cursor.active {
		t.Error("the fixture itself came back with the mode on: a test above mutated shared state")
	}
}

// TestBlockCursorWalksOneStopPerSurface is the difference between a pointer and a cursor: a block
// marks every line it paints for its own entry, so a click lands anywhere on it, but the walk stops
// ONCE per surface rather than crawling line by line through it. A single tool block IS one surface
// — its header, its leader row and, open, every body row and the see-less footer closing them — so
// it offers exactly one stop in either state, and movement clamps there instead of wrapping. The
// collapsed paint no longer offers a second one: the `+N more lines` count rides the leader row's
// outcome slot now rather than a marker line of its own (collapsedRemainder).
func TestBlockCursorWalksOneStopPerSurface(t *testing.T) {
	t.Parallel()

	m := modelWithToolBlock(t, "ok   a\nok   b\nok   c\nPASS")
	header := markedLine(t, m, targetHeader)
	if got := cursorStops(m.lineTargets); len(got) != 1 || got[0] != header {
		t.Fatalf("the collapsed block offers stops %v, want the block's header at %d alone", got, header)
	}

	m = step(t, m, keyAltUp())
	if got := cursorLine(t, m); got != header {
		t.Fatalf("⌥↑ entered at line %d, want the block's only stop at %d", got, header)
	}

	m = step(t, m, keyEnter())
	if !blockExpanded(t, m, header) {
		t.Fatal("⏎ on the block's stop did not open it")
	}
	if got := cursorStops(m.lineTargets); len(got) != 1 || got[0] != header {
		t.Errorf("the open block offers stops %v, want its header at %d alone — its body rows are the same surface",
			got, header)
	}

	m = step(t, m, keyUp())
	if got := cursorLine(t, m); got != header {
		t.Errorf("↑ at the only stop moved to line %d, want it clamped at %d", got, header)
	}

	m = step(t, m, keyDown())
	if got := cursorLine(t, m); got != header {
		t.Errorf("↓ at the only stop moved to line %d, want it clamped at %d", got, header)
	}
}

// TestBlockCursorWalksTheDeepestVisibleLevel is the umbrella's own claim (design call 7): the cursor
// walks the same surfaces the mouse has, at whatever level the painter marked them, so a folded
// super-group offers its type rows and nothing else — and opening one adds ITS members to the walk,
// where a level that is folded away paints no lines and so has no stops at all.
func TestBlockCursorWalksTheDeepestVisibleLevel(t *testing.T) {
	t.Parallel()

	m := modelWithSuperGroup(t)
	for _, line := range cursorStops(m.lineTargets) {
		if got := m.lineTargets[line].kind; got != targetType {
			t.Fatalf("a folded umbrella offers a %v stop at line %d, want its type rows alone", got, line)
		}
	}

	// The last type row is the Terminal run (entry 3, the second of the two runs the fixture folds).
	m = step(t, m, keyAltUp())
	run := m.lineTargets[cursorLine(t, m)]
	if run.kind != targetType {
		t.Fatalf("⌥↑ entered on a %v stop, want the umbrella's last type row", run.kind)
	}

	m = step(t, m, keyEnter())
	if !m.transcript.entries[run.entry].typeExpanded {
		t.Fatal("⏎ on a type row did not open the run it heads")
	}

	m = step(t, m, keyDown())
	deeper := m.lineTargets[cursorLine(t, m)]
	if deeper.kind != targetHeader || deeper.entry != run.entry {
		t.Errorf("↓ from the opened type row reached %+v, want the run's first member row (entry %d)", deeper, run.entry)
	}
}

// TestBlockCursorScrollsTheViewToFollowIt is the other half of a walk that leaves the screen: the
// transcript scrolls to wherever the cursor has gone, so every step of the walk is a step the human
// can see — including under the sticky header, whose frozen prompt rows the cursor must land below
// rather than behind.
func TestBlockCursorScrollsTheViewToFollowIt(t *testing.T) {
	t.Parallel()

	m := modelWithLongToolGroup(t, 30)
	stops := cursorStops(m.lineTargets)
	if len(stops) < m.transcriptRows() {
		t.Fatalf("setup: %d stops fit inside %d rows — the walk never leaves the screen", len(stops), m.transcriptRows())
	}

	m = step(t, m, keyAltUp())
	bottom := m.viewport.YOffset()
	for i := 1; i < len(stops); i++ {
		m = step(t, m, keyUp())
		row := m.blockCursorRow()
		if row < 0 {
			t.Fatalf("step %d walked the cursor to line %d, which the view is not showing (offset %d)",
				i, m.cursor.line, m.viewport.YOffset())
		}
		if got := m.drawnLineAt(row); got != m.cursor.line {
			t.Fatalf("step %d put the cursor on row %d, which draws line %d, not %d", i, row, got, m.cursor.line)
		}
	}
	if m.viewport.YOffset() >= bottom {
		t.Errorf("the view never scrolled: offset %d after walking to the top, %d at the bottom", m.viewport.YOffset(), bottom)
	}
}

// modelWithLongToolGroup builds a ready idle model holding one prompt and count consecutive Runs,
// each with a body of its own — a group taller than the transcript's own row budget, which is what a
// walk needs to leave the screen.
func modelWithLongToolGroup(t *testing.T, count int) Model {
	t.Helper()
	m := newTestModel(t) // 80x24
	m.transcript.reset()
	m.transcript.addUser("check every package", nil)
	for i := range count {
		runCall(&m.transcript, fmt.Sprintf("c%d", i+1), fmt.Sprintf("go test ./pkg%d/...", i+1), "ok\nPASS", 0)
	}
	m.refreshViewport()
	return m
}

// TestBlockCursorEnterTogglesWhatItStandsOn is ⏎'s whole meaning: it flips the surface under the
// highlight exactly as a click on that surface would, and the highlight stays on it — so a block
// opened by keyboard is closed by pressing ⏎ again, with no second gesture to find it.
func TestBlockCursorEnterTogglesWhatItStandsOn(t *testing.T) {
	t.Parallel()

	m := modelWithToolBlock(t, "ok   a\nok   b\nok   c\nPASS")
	header := markedLine(t, m, targetHeader)

	m = step(t, m, keyAltUp())
	m = step(t, m, keyUp())
	if cursorLine(t, m) != header {
		t.Fatalf("setup: the cursor is at line %d, not on the block's header at %d", m.cursor.line, header)
	}

	m = step(t, m, keyEnter())
	if !blockExpanded(t, m, header) {
		t.Fatal("⏎ on the block's header did not expand it")
	}
	if body := strings.Join(m.lines, "\n"); !strings.Contains(body, "PASS") {
		t.Fatalf("the expanded paint did not reach the viewport lines:\n%s", body)
	}
	if got := cursorLine(t, m); got != header {
		t.Errorf("the cursor moved to line %d over the toggle, want it still on the header at %d", got, header)
	}

	m = step(t, m, keyEnter())
	if blockExpanded(t, m, header) {
		t.Error("a second ⏎ did not close the block again")
	}
}

// TestBlockCursorKeysReturnToThePrompt is the way out, both of them (design call 7). esc leaves the
// walk and types nothing; a printable key leaves it AND lands in the box, so a human who starts
// writing mid-walk loses neither the letter nor a keystroke to the mode they were in.
func TestBlockCursorKeysReturnToThePrompt(t *testing.T) {
	t.Parallel()

	base := modelWithToolBlock(t, "ok   a\nok   b\nok   c\nPASS")

	t.Run("esc leaves the walk and types nothing", func(t *testing.T) {
		m := step(t, base, keyAltUp())
		m = step(t, m, keyEsc())
		if m.cursor.active {
			t.Error("esc did not leave the block-cursor mode")
		}
		if got := m.input.Value(); got != "" {
			t.Errorf("esc typed %q into the prompt", got)
		}
	})

	t.Run("a printable key leaves the walk and lands in the prompt", func(t *testing.T) {
		m := step(t, base, keyAltUp())
		m = step(t, m, keyRune('x'))
		if m.cursor.active {
			t.Error("a printable key did not leave the block-cursor mode")
		}
		if got := m.input.Value(); got != "x" {
			t.Errorf("the prompt holds %q — the key that ended the mode was swallowed by it", got)
		}
	})

	t.Run("⏎ inside the walk sends nothing", func(t *testing.T) {
		m := step(t, base, keyAltUp())
		m.input.SetValue("a message")
		m = step(t, m, keyEnter())
		if got := m.input.Value(); got != "a message" {
			t.Errorf("⏎ inside the walk submitted the draft (the box now holds %q)", got)
		}
	})
}

// TestBlockCursorHighlightsTheRowItStandsOn pins the paint: the cursor's row — and only it — carries
// the theme's selection field, the one tone the frame spends on "this is the thing you are pointing
// at" (design call 7), and the text under the bar is unchanged.
func TestBlockCursorHighlightsTheRowItStandsOn(t *testing.T) {
	t.Parallel()

	m := modelWithToolBlock(t, "ok   a\nok   b\nok   c\nPASS")
	m = step(t, m, keyAltUp())
	m = step(t, m, keyUp())
	row := m.blockCursorRow()
	if row < 0 {
		t.Fatal("the cursor's line is not drawn on any transcript row")
	}

	view := m.View()
	lines := strings.Split(view.Content, "\n")
	if !strings.Contains(strip(lines[row]), "✦ Terminal") {
		t.Fatalf("row %d reads %q, not the block header the cursor stands on", row, strip(lines[row]))
	}
	if !colorActive(m.th) {
		return // the styling is the rest of the claim, and this profile emits none
	}
	bar := styleSGR(m.th.selection)
	if !strings.Contains(lines[row], bar) {
		t.Errorf("the cursor's row carries no selection bar: %q", lines[row])
	}
	for i, line := range lines {
		if i != row && strings.Contains(line, bar) {
			t.Errorf("row %d carries the bar as well as the cursor's row %d: %q", i, row, line)
		}
	}
}

// TestBlockCursorYieldsItsKeysToAPendingDecision is the lockout this mode must never cause: an
// approval menu owns ↑/↓ and ⏎ for its own rows, so while one is up the walk holds neither the keys
// nor the highlight — and gets both back, on the line it was standing on, once the decision is taken.
func TestBlockCursorYieldsItsKeysToAPendingDecision(t *testing.T) {
	t.Parallel()

	m := modelWithToolBlock(t, "ok   a\nok   b\nok   c\nPASS")
	m = step(t, m, keyAltUp())
	line := cursorLine(t, m)
	painted := m.View().Content

	m.state = stateAwaitingApproval
	if _, _, handled := m.blockCursorKey(keyEnter()); handled {
		t.Error("the block cursor claimed ⏎ while an approval decision was pending")
	}
	if got := m.View().Content; strings.Contains(got, styleSGR(m.th.selection)) && colorActive(m.th) {
		t.Error("the cursor's bar is still painted while an approval decision is pending")
	}

	m.state = stateIdle
	if got := cursorLine(t, m); got != line {
		t.Errorf("the walk resumed at line %d, want the line %d it was suspended on", got, line)
	}
	if got := m.View().Content; got != painted {
		t.Error("the frame did not come back as it was before the decision interrupted the walk")
	}
}
