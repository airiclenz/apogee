package tui

import "strings"

// The footer's fit — one composer, spending the window in the order the row is read for
//
// The footer states five facts about the session and one about the mode it runs in, and on a wide
// window it says all six. On a narrow one it cannot, and the question this file answers is which
// of them the row gives up first.
//
// The old answer was "the mode marker": the moment the left run and the marker did not both fit,
// the marker dropped WHOLE and the left run truncated to the window. That is exactly backwards for
// the one fact a human checks the footer for — which blast radius the session is running in — and
// it is worst precisely where it matters, since a narrow window is not a rare state. The marker is
// now what the row never gives up.
//
// The fit is a LADDER over priorities, and the row spends its columns outward-in, the same reading
// order the segments are laid out in: the effort word goes first (priority 3 — how hard the model
// is asked to think), then the workdir (2), then the host (1). What is left is priority 0: the
// model, the `✦ offline` marker and the mode marker, and those three only give way to each other —
// the model truncates, then goes, then offline goes, and only where the mode marker cannot seat
// whole between its two margins does the row fall back to the old shape.
//
// The whole layout comes out of ONE call. The footer has two readers of its arithmetic — the
// painter and the pointer that addresses the marker's cells ([Model.handleFooterModeClick]) — and
// two arithmetics that agree today are two that can disagree tomorrow, so this returns the whole
// row and both read it. Measurement is the injected [widthAuthority] and nothing else (ADR 0030
// §5): a footer laid out in a measure the painter did not choose is off by a column per wide
// grapheme, and the marker is right-anchored, so that column lands on the mode word.

// footerInput is everything the fit needs, and all of it is plain text: no styles, no escape
// sequences, no arithmetic already done. The five segment strings are the facts the left run is
// composed from, in reading order, each one empty where nothing has named it; mode is the
// already-worded mode marker (its symbol, its word, and in Auto the blast radius that word runs
// with), which the fit treats as ONE atom and never splits.
//
// width is the window, margin the width of the row's own lead and trailing margins ([bodyIndent]
// in the painter's measure), and measure the authority every width in this file is taken with.
type footerInput struct {
	host    string
	model   string
	effort  string
	workdir string
	offline string

	mode string

	width   int
	margin  int
	measure widthAuthority
}

// footerLayout is the composed row: the three plain runs the painter styles separately — info on
// the footer's own tone, offline in the error tone, mode in the mode's colour — plus col, the
// screen column the marker's first cell lands on, and hasMode, false only in the floor case where
// the marker cannot seat whole and the row keeps the older truncated shape.
//
// info and offline are already joined and already reduced: a segment the ladder dropped left WITH
// its separator, so neither run ever begins or ends on a dangling ✦. When hasMode is false, col is
// 0 and the two left runs are truncated to the window — there is no marker to address, which is
// why a click on the footer then names nothing.
type footerLayout struct {
	info    string
	offline string
	mode    string
	col     int
	hasMode bool
}

// footerFit composes the footer's row to the width it has, dropping segments in priority order
// until the left run and the mode marker both fit. It is pure — the same input always yields the
// same layout — and TOTAL: every width, including 0 and a negative one, yields a layout the
// painter can spend.
//
// "Fits" is the painter's own test, unchanged: at least one blank column between the left run and
// the marker, so the two ends never touch. The rungs, in order, stopping at the first that fits:
// the full run; the run without the effort word; without the workdir; without the host; the model
// truncated to what is left; the model gone; offline gone. The last rung is the marker alone
// between its two margins, and there the blank column is not asked for — with no left run there is
// nothing for it to separate the marker from.
func footerFit(in footerInput) footerLayout {
	// The priority drops, richest first. A rung whose dropped segment was already absent composes
	// the same run as the one above it and fails the same way, so an absent effort word simply
	// hands the next rung the workdir — nothing has to know which segments are named.
	for _, segments := range [][]string{
		{in.host, in.model, in.effort, in.workdir}, // everything the session knows
		{in.host, in.model, in.workdir},            // priority 3: the effort word
		{in.host, in.model},                        // priority 2: the workdir
		{in.model},                                 // priority 1: the host
	} {
		if layout, ok := in.seat(footerRun(segments...)); ok {
			return layout
		}
	}

	// Priority 0. The model is the last fact of the four to go, and it gives way in halves: an
	// ellipsis first, so a wide-enough window still names which model is answering, and only then
	// the segment whole.
	if budget := in.modelBudget(); budget > 0 {
		if layout, ok := in.seat(in.measure.Truncate(in.model, budget, "…")); ok {
			return layout
		}
	}
	if layout, ok := in.seat(""); ok {
		return layout
	}

	// Nothing left but the marker. It seats whole between the two margins or it does not seat at
	// all — a clipped mode word would name a blast radius the session is not in.
	if in.margin+in.measure.Width(in.mode)+in.margin <= in.width {
		return footerLayout{mode: in.mode, col: in.width - in.margin - in.measure.Width(in.mode), hasMode: true}
	}
	return in.floor()
}

// seat places the marker against a candidate left run and reports whether the two ends fit. col is
// the marker's right-anchored first column — the slot's own trailing margin sits between its last
// cell and the window edge, the very column the status line's right slot ends in.
func (in footerInput) seat(info string) (footerLayout, bool) {
	offline := in.offlineRun(info)
	left := in.margin + in.measure.Width(info) + in.measure.Width(offline)
	col := in.width - in.margin - in.measure.Width(in.mode)
	if col-left < 1 {
		return footerLayout{}, false
	}
	return footerLayout{info: info, offline: offline, mode: in.mode, col: col, hasMode: true}, true
}

// modelBudget is the width the model may still spend once every other segment has gone: the
// window less both margins, the offline run, the marker, and the one blank column that separates
// the two ends. It can be zero or negative, which is the fit's own signal that truncating the
// model cannot help and the segment has to go whole.
func (in footerInput) modelBudget() int {
	return in.width - in.margin - in.margin -
		in.measure.Width(in.offlineRun(in.model)) - in.measure.Width(in.mode) - 1
}

// offlineRun words the offline segment the way the painter draws it: ✦-led while a left run
// precedes it, and the bare word once the ladder has left it alone on the row, so the separator
// travels with the segment rather than stranding a dangling ✦ at the row's start.
func (in footerInput) offlineRun(info string) string {
	switch {
	case in.offline == "":
		return ""
	case info == "":
		return in.offline
	default:
		return " " + glyphAssistant + " " + in.offline
	}
}

// floor is the older shape, kept for the one window too narrow to seat the marker at all: the FULL
// left run — nothing is gained by dropping segments from a row that has no marker to make room for
// — truncated to the window with an ellipsis. The two runs are cut as a pair so offline keeps its
// own styled run and its error tone rather than being folded into the info run to be truncated
// with it.
func (in footerInput) floor() footerLayout {
	info := footerRun(in.host, in.model, in.effort, in.workdir)
	offline := in.offlineRun(info)
	budget := max(0, in.width-in.margin)
	info = in.measure.Truncate(info, budget, "…")
	offline = in.measure.Truncate(offline, max(0, budget-in.measure.Width(info)), "…")
	return footerLayout{info: info, offline: offline}
}

// footerRun joins the left run's segments the way the footer reads, ✦-separated, with every
// segment nothing has named leaving WITH its separator ([nonEmpty]).
func footerRun(segments ...string) string {
	return strings.Join(nonEmpty(segments...), " "+glyphAssistant+" ")
}
