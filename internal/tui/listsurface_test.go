package tui

import (
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
