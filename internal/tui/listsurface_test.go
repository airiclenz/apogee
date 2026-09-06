package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// The shared list surface — one key contract behind every filtering overlay
// ----------------------------------------------------------------------------
//
// The assertions below drive the surface DIRECTLY rather than through the panes that embed it, which
// is the whole point of there being one: "what does ↓ do at the bottom of a filtered list" used to be
// reachable only by opening a real overlay and pressing keys into Update, once per pane. A claim
// proved for [Model.listKey] is proved for the picker and the /sessions browser at once — and, as the
// remaining panes adopt it, for them too.

// listTestRows is a one-cell row per label, the shape a list of plain choices has.
func listTestRows(labels ...string) []popupRow { return singleCellRows(labels) }

// pressList routes one key through the surface at l and returns the verdict it gave, discarding the
// Cmd the filter field may have asked for.
func pressList(m Model, l *listSurface, msg tea.KeyPressMsg, rows []popupRow, wrap listWrap) listVerdict {
	verdict, _ := m.listKey(l, msg, rows, wrap)
	return verdict
}

// What ↑/↓ do at the ENDS is the pane's own answer and the surface takes it as a parameter, so both
// answers are proved here rather than remembered at the call sites. Sub-tests are per (wrap, key)
// pair: a merge that dropped the flag would keep half of them passing.
func TestListSurfaceArrowsAnswerTheEndsByTheWrapFlag(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	rows := listTestRows("first", "second", "third")

	cases := []struct {
		name  string
		wrap  listWrap
		from  int
		key   tea.KeyPressMsg
		want  int
		since string
	}{
		{"wrapping ↓ at the bottom", listWrapsAround, 2, keyDown(), 0, "returns to the first row"},
		{"wrapping ↑ at the top", listWrapsAround, 0, keyUp(), 2, "returns to the last row"},
		{"stopping ↓ at the bottom", listStopsAtEnds, 2, keyDown(), 2, "stays on the last row"},
		{"stopping ↑ at the top", listStopsAtEnds, 0, keyUp(), 0, "stays on the first row"},
		{"↓ in the middle", listWrapsAround, 1, keyDown(), 2, "moves one row on"},
		{"↑ in the middle", listStopsAtEnds, 1, keyUp(), 0, "moves one row back"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := listSurface{listCursor: listCursor{selected: tc.from}}
			if got := pressList(m, &l, tc.key, rows, tc.wrap); got != listSwallowed {
				t.Fatalf("verdict = %v, want the modal to keep an arrow", got)
			}
			if l.selected != tc.want {
				t.Errorf("selected = %d, want %d — %s", l.selected, tc.want, tc.since)
			}
		})
	}
}

// The END of a list is where the FILTER put it, not where the offering ends. This is the claim the
// shared surface exists to make testable: the two rows a filter left are the whole list as far as
// ↑/↓ are concerned, so ↓ on the last of them wraps to the first of them (or stays) rather than
// stepping onto a row the pane never painted.
func TestListSurfaceArrowsAtTheBottomOfAFilteredList(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	// "alpha" prunes "beta" away and leaves rows 0 and 2 of the offering — so the filtered list's
	// bottom is its index 1, and the offering's is 2.
	rows := listTestRows("alpha", "beta", "alphabet")

	cases := []struct {
		name string
		wrap listWrap
		want int
	}{
		{"wrapping", listWrapsAround, 0},
		{"stopping", listStopsAtEnds, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := listSurface{listCursor: listCursor{selected: 1}, filter: typedFilter(m, "alpha")}
			if got := len(l.view(rows).rows); got != 2 {
				t.Fatalf("precondition: the filter leaves %d rows, want 2", got)
			}

			if got := pressList(m, &l, keyDown(), rows, tc.wrap); got != listSwallowed {
				t.Fatalf("verdict = %v, want the modal to keep ↓", got)
			}
			if l.selected != tc.want {
				t.Errorf("selected = %d, want %d — the last FILTERED row is the bottom", l.selected, tc.want)
			}
		})
	}
}

// The surface answers only for the keys every modal list shares and hands everything else back, so a
// pane's own verbs (the browser's ^r / ^d / ^a) are reachable and a key that is neither is swallowed
// by the modal above.
func TestListSurfaceVerdictsNameWhatThePaneMustDo(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	rows := listTestRows("first", "second")

	cases := []struct {
		name string
		key  tea.KeyPressMsg
		from int
		want listVerdict
	}{
		{"esc asks the pane to close", keyEsc(), 0, listCloses},
		{"⏎ on a seated row asks for the accept", keyEnter(), 1, listAccepts},
		{"↑ is the surface's own", keyUp(), 1, listSwallowed},
		{"^n is ↓ by another name", keyCtrl('n'), 0, listSwallowed},
		{"a printable key types", keyRune('s'), 0, listSwallowed},
		{"backspace edits the filter", keyBackspace(), 0, listSwallowed},
		{"a chord the surface has no use for goes back", keyCtrl('r'), 0, listUnclaimed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := listSurface{listCursor: listCursor{selected: tc.from}}
			if got := pressList(m, &l, tc.key, rows, listWrapsAround); got != tc.want {
				t.Errorf("verdict = %v, want %v", got, tc.want)
			}
		})
	}
}

// ⏎ over a list with no rows left is NOT an accept: there is nothing to take, so the key is spent by
// the modal and the pane is never asked to act on a highlight pointing at nothing.
func TestListSurfaceEnterOverAnEmptyListAcceptsNothing(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	rows := listTestRows("alpha", "beta")
	l := listSurface{filter: typedFilter(m, "no-such-row")}

	if got := pressList(m, &l, keyEnter(), rows, listWrapsAround); got != listSwallowed {
		t.Errorf("verdict = %v, want ⏎ over a zero-match list to take nothing", got)
	}
}

// A printable key extends the filter and the highlight is re-clamped to the rows the new filter
// leaves standing — the second clamp of the keypress, and the one that keeps the selection from
// pointing past the end of a list that just got shorter under it.
func TestListSurfaceTypingNarrowsTheListAndReclampsTheHighlight(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	rows := listTestRows("alpha", "beta", "gamma")
	l := listSurface{listCursor: listCursor{selected: 2}}

	if got := pressList(m, &l, keyRune('a'), rows, listWrapsAround); got != listSwallowed {
		t.Fatalf("verdict = %v, want a printable key to be typed", got)
	}
	if l.filter.value() != "a" {
		t.Fatalf("filter = %q, want the key typed into it", l.filter.value())
	}
	if got := len(l.view(rows).rows); got != 3 {
		t.Fatalf("precondition: %q leaves %d rows, want all three", "a", got)
	}

	// "al" leaves "alpha" alone, so the highlight has to come back from row 2 to row 0.
	if got := pressList(m, &l, keyRune('l'), rows, listWrapsAround); got != listSwallowed {
		t.Fatalf("verdict = %v, want a printable key to be typed", got)
	}
	if got := len(l.view(rows).rows); got != 1 {
		t.Fatalf("precondition: %q leaves %d rows, want one", "al", got)
	}
	if l.selected != 0 {
		t.Errorf("selected = %d, want 0 — the highlight is clamped into the narrowed list", l.selected)
	}

	// Backspace is the undo: the wider list comes back and the highlight stays where the clamp left it.
	if got := pressList(m, &l, keyBackspace(), rows, listWrapsAround); got != listSwallowed {
		t.Fatalf("verdict = %v, want backspace to edit the filter", got)
	}
	if l.filter.value() != "a" || l.selected != 0 {
		t.Errorf("filter = %q, selected = %d, want %q with the highlight left alone",
			l.filter.value(), l.selected, "a")
	}
}

// The selection is clamped BEFORE a key acts, so a list that shrank under the open pane — a beat
// carrying a shorter offering, a deleted session — cannot leave the highlight past its end.
func TestListSurfaceClampsTheHighlightBeforeActing(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	l := listSurface{listCursor: listCursor{selected: 7}}

	if got := pressList(m, &l, keyCtrl('r'), listTestRows("only"), listWrapsAround); got != listUnclaimed {
		t.Fatalf("verdict = %v, want the chord handed back", got)
	}
	if l.selected != 0 {
		t.Errorf("selected = %d, want 0 — the clamp runs on every key, claimed or not", l.selected)
	}

	l = listSurface{listCursor: listCursor{selected: 3}}
	if got := pressList(m, &l, keyEnter(), nil, listWrapsAround); got != listSwallowed {
		t.Fatalf("verdict = %v, want ⏎ over an empty list to take nothing", got)
	}
	if l.selected != 0 {
		t.Errorf("selected = %d, want an empty list to pin the highlight at zero", l.selected)
	}
}

// A list that does NOT filter hands the typing keys back, and that is the whole of the difference
// between the two shapes this module holds. Neither answer could be decided here: the /settings key
// list answers backspace with an armed reset, and the "/" | "@" dropdown gives every letter to the
// chat box it hangs over. Proved per key, because a merge that swallowed one of them would leave the
// other passing.
func TestListCursorHandsTheTypingKeysBack(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"a printable key", keyRune('s')},
		{"backspace", keyBackspace()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := listCursor{selected: 1}

			if got := l.key(tc.key, 3, listWrapsAround); got != listUnclaimed {
				t.Errorf("verdict = %v, want listUnclaimed — a list with no filter types nowhere", got)
			}
			if l.selected != 1 {
				t.Errorf("selected = %d, want 1 — a key the cursor did not claim moves no highlight", l.selected)
			}
		})
	}
}

// The keys the cursor hands back are the SAME keys a filtering surface claims, which is what makes
// the two one contract rather than two: the surface is asked last, of what the cursor found no use
// for. A chord neither of them wants still reaches the pane.
func TestListSurfaceClaimsTheTypingKeysTheCursorReturns(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	l := listSurface{listCursor: listCursor{selected: 1}}
	rows := listTestRows("first", "second", "third")

	if got := pressList(m, &l, keyRune('s'), rows, listWrapsAround); got != listSwallowed {
		t.Errorf("printable verdict = %v, want listSwallowed — the filter took it", got)
	}
	if got := l.filter.value(); got != "s" {
		t.Errorf("filter = %q, want %q", got, "s")
	}
	if got := pressList(m, &l, keyCtrl('r'), rows, listWrapsAround); got != listUnclaimed {
		t.Errorf("chord verdict = %v, want listUnclaimed — neither half of the contract wants it", got)
	}
}

// The highlight the painter is given is the clamped selection, and −1 where there is nothing to
// choose — the popup module's own convention for a pane with no cursor.
func TestListSurfaceHighlightIsOffAnEmptyList(t *testing.T) {
	t.Parallel()
	l := listCursor{selected: 5}

	if got := l.highlight(0); got != -1 {
		t.Errorf("highlight over no rows = %d, want −1 (no highlight)", got)
	}
	if got := l.highlight(2); got != 1 {
		t.Errorf("highlight = %d, want the selection clamped onto the last row", got)
	}
}

// seat is the setter beside highlight's getter — the one way the POINTER moves a list's cursor, since
// a click names a row outright instead of walking to it. It clamps like every other move, so a row the
// pane never painted can no more be highlighted by the mouse than by ↑/↓, and an empty list seats
// nothing at all: there is no row to put the highlight on.
func TestListCursorSeatClampsTheRowThePointerNames(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		row, count int
		want       int
	}{
		{"a painted row", 2, 5, 2},
		{"past the last row", 9, 5, 4},
		{"above the first", -3, 5, 0},
		{"an empty list seats nothing", 2, 0, 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			l := listCursor{selected: 7}

			l.seat(c.row, c.count)

			if l.selected != c.want {
				t.Errorf("seat(%d, %d) left the highlight on %d, want %d", c.row, c.count, l.selected, c.want)
			}
		})
	}
}

// An accept is resolved through the filter, never against the list underneath it: row 0 of a pruned
// list is row 1 of the offering, and taking row 0 of the offering would act on something the human
// never saw.
func TestListSurfaceAcceptResolvesThroughTheFilter(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	rows := listTestRows("first", "second", "third")
	l := listSurface{filter: typedFilter(m, "d")} // "second" and "third"

	offered, ok := l.view(rows).offeringIndex(l.selected)
	if !ok || offered != 1 {
		t.Errorf("offeringIndex(0) = (%d, %v), want the SECOND row of the offering", offered, ok)
	}

	l.selected = 5 // past the end: nothing to take rather than a wrong row taken
	if _, ok := l.view(rows).offeringIndex(l.selected); ok {
		t.Error("offeringIndex named a row for a highlight past the end of the filtered list")
	}
}

// wheelNotch is one wheel notch of the given button, the message a pane's wheel handler is offered
// once it has decided the pointer is inside its own rectangle.
func wheelNotch(button tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg{Button: button}
}

// The wheel contract, proved on the cursor itself rather than through the five panes that adopt it: a
// notch is one row, the two sideways buttons a trackpad sends are nothing, and a list with no rows has
// no highlight for either of them to walk.
func TestListCursorWheelWalksOneRowPerNotch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		from   int
		button tea.MouseButton
		rows   int
		want   int
		since  string
	}{
		{"a down notch", 0, tea.MouseWheelDown, 3, 1, "moves one row on"},
		{"an up notch", 2, tea.MouseWheelUp, 3, 1, "moves one row back"},
		{"a down notch on the last row", 2, tea.MouseWheelDown, 3, 2, "stays on the last row"},
		{"an up notch on the first row", 0, tea.MouseWheelUp, 3, 0, "stays on the first row"},
		{"a left notch", 1, tea.MouseWheelLeft, 3, 1, "is not a scroll of this list"},
		{"a right notch", 1, tea.MouseWheelRight, 3, 1, "is not a scroll of this list"},
		{"a notch over an empty list", 0, tea.MouseWheelDown, 0, 0, "has no row to walk"},
		{"a notch over a list that emptied", 7, tea.MouseWheelUp, 0, 7, "walks nothing and panics on nothing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := listCursor{selected: tc.from}

			l.wheel(wheelNotch(tc.button), tc.rows)

			if l.selected != tc.want {
				t.Errorf("selected = %d, want %d — %s", l.selected, tc.want, tc.since)
			}
		})
	}
}

// The wheel CLAMPS where the keys WRAP, and the two answers are pinned side by side rather than one at
// a time: the same cursor, on the same end of the same list, walked by a notch and by the arrow of a
// pane that wraps. ↑/↓ walk a list as a cycle; a wheel is a scroll, and rolling past the last row onto
// the first would move the human somewhere they did not aim.
func TestListCursorWheelClampsWhereTheKeysWrap(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		from      int
		button    tea.MouseButton
		key       tea.KeyPressMsg
		wantWheel int
		wantKey   int
	}{
		{"at the bottom", 2, tea.MouseWheelDown, keyDown(), 2, 0},
		{"at the top", 0, tea.MouseWheelUp, keyUp(), 0, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wheeled, keyed := listCursor{selected: tc.from}, listCursor{selected: tc.from}

			wheeled.wheel(wheelNotch(tc.button), 3)
			keyed.key(tc.key, 3, listWrapsAround)

			if wheeled.selected != tc.wantWheel {
				t.Errorf("wheel left selected = %d, want %d — a notch stops at the end", wheeled.selected, tc.wantWheel)
			}
			if keyed.selected != tc.wantKey {
				t.Errorf("key left selected = %d, want %d — this pane's arrows wrap around", keyed.selected, tc.wantKey)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// The row block's breathing rows
// ----------------------------------------------------------------------------

// breathingListModel is a model sized to one window with the overflow bar OFF, so the pane-shape
// assertions below read a row as its own text rather than as its text plus a bar cell. The bar is a
// contract of its own (popupRowScrollbar) and the one thing these assertions care about it — that it
// never runs down the blanks — is asserted separately, with it on.
func breathingListModel(t *testing.T, height int, bar bool) Model {
	t.Helper()
	opts := testOpts
	opts.HideScrollbar = !bar
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	return step(t, m, tea.WindowSizeMsg{Width: 80, Height: height})
}

// breathingList is the offering those assertions are made over: a titled, hinted list of ten plain
// one-cell choices in the picker's own slot, with the picker's own taste for how many of them to show
// at once. body is the caller's, because a pane WITH one is the case the upper blank is not spent on.
func breathingList(body string) listContent {
	c := listContent{
		pane:     panePicker,
		title:    "a list",
		hint:     "esc close",
		rowCap:   8,
		rows:     listTestRows("one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"),
		selected: 0,
	}
	if body != "" {
		// What renderFilterListPlaced composes for a list being narrowed, and the shape the /settings
		// sub-list's question arrives in too: a body block set off by its own two blanks.
		c.body, c.bodyLead, c.bodyPad = body, pickerFilterLead, true
	}
	return c
}

// listPaneLines is a rendered list pane as a human reads it: the two borders dropped and every line
// between them stripped of its styling and its border/padding chrome, so a blank arrives as "".
func listPaneLines(t *testing.T, pane string) []string {
	t.Helper()
	lines := popupLines(pane)
	if len(lines) < 3 {
		t.Fatalf("the pane is %d lines, want at least its two borders and a line between them: %q", len(lines), lines)
	}
	out := make([]string, 0, len(lines)-2)
	for _, ln := range lines[1 : len(lines)-1] {
		out = append(out, popupInterior(ln))
	}
	return out
}

// Every list pop-up keeps one blank line between whatever stands above its row block and the block,
// and one between the block and its key legend (popupSpec.rowPadAbove, popupRowStyle.padBelow) — the
// house rule renderList books for all four of its panes at once. The upper blank is NOT spent where
// the line above the block is already one: a pane with a body of its own closes it with the body's
// own lower pad, so a list being narrowed never opens a two-line gap under its filter. And the two
// are the FIRST lines the pane gives up: on a window that cannot seat a row beside them they are
// handed back together, because breathing room is not worth a decision.
func TestRenderListBreathes(t *testing.T) {
	cases := []struct {
		name   string
		height int
		body   string
		want   []string
	}{
		{
			// 26 rows is the first window that pays for the picker's whole taste and both blanks.
			name: "no body — the blank under the title", height: 26,
			want: []string{"a list", "", "❯ one", "two", "three", "four", "five", "six", "seven", "eight", "", "esc close"},
		},
		{
			name: "a body — its own blank is the block's", height: 26, body: pickerFilterLead + "t",
			want: []string{"a list", "", pickerFilterLead + "t", "", "❯ one", "two", "three", "four", "five", "six", "seven", "", "esc close"},
		},
		{
			// The floor: two lines of rows is all the grant has, so both blanks go back to them.
			name: "at the floor — the rows keep the lines", height: 18,
			want: []string{"a list", "❯ one", "two", "esc close"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := breathingListModel(t, tc.height, false)
			got := listPaneLines(t, m.renderList(breathingList(tc.body)))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("pane lines =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// The overflow bar is the rows' own stroke and stops where they do: the two blanks the block is set
// off by carry no cell of it, so the bar never says the list runs on into the breathing room.
func TestRenderListBreathingRowsCarryNoScrollbarCell(t *testing.T) {
	m := breathingListModel(t, 26, true)
	lines := listPaneLines(t, m.renderList(breathingList("")))
	if n := len(lines); lines[1] != "" || lines[n-2] != "" {
		t.Fatalf("the blanks are not at 1 and %d: %q", n-2, lines)
	}
	if !containsAny(lines, glyphScrollThumb) {
		t.Fatalf("precondition: the pane painted no bar at all, so there is nothing to hold off the blanks: %q", lines)
	}
}

// containsAny reports whether any of the lines carries s.
func containsAny(lines []string, s string) bool {
	for _, ln := range lines {
		if strings.Contains(ln, s) {
			return true
		}
	}
	return false
}
