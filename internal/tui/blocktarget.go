package tui

// ----------------------------------------------------------------------------
// The click map — what one rendered line is to a pointer
// ----------------------------------------------------------------------------
//
// The transcript's fold levels are reachable by pointer and by keyboard, and neither reach
// re-derives what is foldable: the painter that emits a line states, in the same act, what a click
// on it means. This file holds the vocabulary that statement is made in — the kinds ([targetKind]),
// the painter's own relative form ([lineMark]) and the absolute form [transcript.renderView]
// resolves it to ([lineTarget]) — so the one accounting the mouse (mouse.go) and the block cursor
// (blockcursor.go) both read has a file of its own rather than living inside the renderer that
// happens to build it. blocktarget_test.go is the suite named for it.
// targetKind says what one rendered line is to a motionless click (layout.md, "Collapsed and
// expanded blocks"): nothing at all — the overwhelmingly common case, and the zero value — or a
// TOGGLE line, which flips the state of the entry the mark names. Every line a block paints carries
// the one meaning: the collapsed paint's "+N more lines" used to be a line of its own with an
// open-only click, and it now rides the leader row's outcome slot instead (collapsedRemainder), so
// there is no longer a row whose click could only ever open.
//
// targetHeader is named for the line it started on and is no longer only that line: a single tool
// block wears it on EVERY row it paints — its header, its leader row, its body — and a grouped
// block's MEMBER rows wear it too, each naming its own call rather than the block's head, which is
// how a group of ten opens one of them (renderToolBlock, renderToolGroup). What the kind means has
// not moved: it is the toggle, whatever line it lands on.
//
// A super-group adds the two kinds its extra LEVEL needs (renderSuperGroup). targetType is a type
// row: it toggles the run's own second state (transcript.toggleTypeExpanded) rather than the
// expanded flag every other target flips, which is what lets a reader open a run to its member rows
// and then open a member to its body. targetUmbrella is the umbrella header, whose click is not a
// toggle at all — its floor is the type rows, so it never folds to one line and instead closes every
// open child beneath it (design call 9, transcript.closeSuperGroup).
type targetKind int

const (
	targetNone targetKind = iota
	targetHeader
	targetType
	targetUmbrella
)

// lineTarget is one rendered line's click surface: what the line is, and the index into
// transcript.entries of the entry whose expanded state a click there flips
// (transcript.toggleExpanded) — the block's head for every shape but a grouped run, where it is the
// member the row belongs to. The zero value is "no target", which is what every line outside a
// toggleable block carries, so a lookup needs no second sentinel.
type lineTarget struct {
	kind  targetKind
	entry int
}

// lineMark is what one painted line is to a click as the block's OWN painter states it: the kind,
// and which of the block's entries a click there flips, said as an OFFSET from the block's head. A
// single block marks everything 0 — it has one entry and the head is it — and a grouped block marks
// each member row with the member's index, which is that call's offset by construction
// (toolCallRun walks adjacent entries forward, so views[n] is entries[head+n]).
//
// The offset is relative for the reason the kinds carry no entry index at all: a painter knows the
// shape it is drawing and not where in the scrollback it sits, and [transcript.renderView] alone
// turns the pair into an absolute entry. The zero value is "the head, no target", which is what
// every line outside a click surface carries.
type lineMark struct {
	kind   targetKind
	member int
}
