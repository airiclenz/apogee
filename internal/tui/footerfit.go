package tui

import (
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/gitstatus"
)

// The footer's fit — one composer, spending the window in the order the row is read for
//
// The footer states seven facts about the session and one about the mode it runs in, and on a wide
// window it says all eight. On a narrow one it cannot, and the question this file answers is which
// of them the row gives up first.
//
// The old answer was "the mode marker": the moment the left run and the marker did not both fit,
// the marker dropped WHOLE and the left run truncated to the window. That is exactly backwards for
// the one fact a human checks the footer for — which blast radius the session is running in — and
// it is worst precisely where it matters, since a narrow window is not a rare state. The marker is
// now what the row never gives up.
//
// The fit is a LADDER over priorities. The session's spend goes first (priority 5 — the Spend
// (money) the session has run up, a running tally the /usage pane states in full), then the
// workspace's git state (priority 4 — the branch and its change counts, a fact about the working
// tree that a glance at `git status` restates), and then the row spends its columns outward-in,
// the same reading order the segments are laid out in: the effort word (priority 3 — how hard the
// model is asked to think), then the workdir (2), then the host (1). What is left is priority 0: the model, the `✦ offline` marker and the mode marker, and
// those three only give way to each other — the model truncates, then goes, then offline goes, and
// only where the mode marker cannot seat whole between its two margins does the row fall back to
// the old shape.
//
// The whole layout comes out of ONE call. The footer has two readers of its arithmetic — the
// painter and the pointer that addresses the marker's cells ([Model.handleFooterModeClick]) — and
// two arithmetics that agree today are two that can disagree tomorrow, so this returns the whole
// row and both read it. Measurement is the injected [widthAuthority] and nothing else (ADR 0030
// §5): a footer laid out in a measure the painter did not choose is off by a column per wide
// grapheme, and the marker is right-anchored, so that column lands on the mode word.

// footerInput is everything the fit needs, and all of it is plain text: no styles, no escape
// sequences, no arithmetic already done. The seven segment strings are the facts the left run is
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
	git     string // the workspace's git segment ([gitFooterSpans] joined): `<branch> NΔ N↓ N↑`
	spend   string
	offline string

	mode string

	width   int
	margin  int
	measure widthAuthority
	glyphs  glyphSet
}

// footerLayout is the composed row: the three plain runs the painter styles separately — info on
// the footer's own tone, offline in the error tone, mode in the mode's colour — plus col, the
// screen column the marker's first cell lands on, and hasMode, false only in the floor case where
// the marker cannot seat whole and the row keeps the older truncated shape.
//
// git is the git segment as the ladder KEPT it, and gitAt the byte offset in info its text begins
// at, so the painter can tone it span by span (non-zero counts brighter) without searching the run
// for it — a branch may well be spelled like the model. git is empty where the ladder dropped the
// segment, where there was none, and at the floor, whose truncated run is painted plain; gitAt is
// meaningful only while git is not empty.
//
// info and offline are already joined and already reduced: a segment the ladder dropped left WITH
// its separator, so neither run ever begins or ends on a dangling ✦. When hasMode is false, col is
// 0 and the two left runs are truncated to the window — there is no marker to address, which is
// why a click on the footer then names nothing.
type footerLayout struct {
	info    string
	offline string
	git     string
	gitAt   int
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
// the full run; the run without the spend; without the git segment; without the effort word;
// without the workdir; without the host; the model truncated to what is left; the model gone;
// offline gone. The last rung is the marker alone between its two margins, and there the blank
// column is not asked for — with no left run there is nothing for it to separate the marker from.
func footerFit(in footerInput) footerLayout {
	// The priority drops, richest first. A rung whose dropped segment was already absent composes
	// the same run as the one above it and fails the same way, so an absent spend, git segment or
	// effort word simply hands the next rung on — nothing has to know which segments are named.
	// keepsGit marks the rungs the git segment is still on, so the layout can say where it sits.
	for _, rung := range []struct {
		segments []string
		keepsGit bool
	}{
		{[]string{in.host, in.model, in.effort, in.workdir, in.git, in.spend}, true}, // everything the session knows
		{[]string{in.host, in.model, in.effort, in.workdir, in.git}, true},           // priority 5: the spend
		{[]string{in.host, in.model, in.effort, in.workdir}, false},                  // priority 4: the git segment
		{[]string{in.host, in.model, in.workdir}, false},                             // priority 3: the effort word
		{[]string{in.host, in.model}, false},                                         // priority 2: the workdir
		{[]string{in.model}, false},                                                  // priority 1: the host
	} {
		if layout, ok := in.seat(footerRun(in.glyphs, rung.segments...)); ok {
			if rung.keepsGit && in.git != "" {
				layout.git, layout.gitAt = in.git, in.gitOffset()
			}
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

// gitOffset is the byte offset the git segment's text begins at in a run that keeps it: past the
// segments laid out before it and the one separator that introduces it, or 0 where nothing
// precedes it. It restates no arithmetic of its own — the prefix is joined by [footerRun], the
// joiner the run itself was composed with.
func (in footerInput) gitOffset() int {
	prefix := footerRun(in.glyphs, in.host, in.model, in.effort, in.workdir)
	if prefix == "" {
		return 0
	}
	return len(prefix) + len(" "+in.glyphs.assistant+" ")
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
		return " " + in.glyphs.assistant + " " + in.offline
	}
}

// floor is the older shape, kept for the one window too narrow to seat the marker at all: the FULL
// left run — nothing is gained by dropping segments from a row that has no marker to make room for
// — truncated to the window with an ellipsis. The git segment rides in that run as plain text: the
// layout names no git span, so a cut that lands inside it is painted in the footer's own tone. The two runs are cut as a pair so offline keeps its
// own styled run and its error tone rather than being folded into the info run to be truncated
// with it.
func (in footerInput) floor() footerLayout {
	info := footerRun(in.glyphs, in.host, in.model, in.effort, in.workdir, in.git, in.spend)
	offline := in.offlineRun(info)
	budget := max(0, in.width-in.margin)
	info = in.measure.Truncate(info, budget, "…")
	offline = in.measure.Truncate(offline, max(0, budget-in.measure.Width(info)), "…")
	return footerLayout{info: info, offline: offline}
}

// footerRun joins the left run's segments the way the footer reads, ✦-separated, with every
// segment nothing has named leaving WITH its separator ([nonEmpty]).
func footerRun(glyphs glyphSet, segments ...string) string {
	return strings.Join(nonEmpty(segments...), " "+glyphs.assistant+" ")
}

// footerSpan is one piece of the footer's git segment and the tone it is read in: the branch and a
// zero count sit in the footer's own muted tone, and a non-zero count is bright — the brighter muted
// step ([theme.mutedBright]), never a role of its own — so a tree with something to say lifts out of
// the row while a clean one stays quiet.
type footerSpan struct {
	text   string
	bright bool
}

// gitFooterSpans words the workspace's git state as the footer reads it — `<branch> NΔ N↓ N↑` —
// one span per word: the branch, then the changed-path count, then how far the branch is behind
// and ahead of its upstream. The branch is absent on a detached HEAD, and the two upstream counts
// are absent where git reported no ahead/behind (no upstream, or one that is gone), since a zero
// there would claim a branch is level with an upstream nothing compared it to.
//
// The branch is the repository's text, and the footer is one of the view's escape-strip seams
// (doc.go): it goes through [stripEscapes] here, where it is worded, so the fit measures and the
// painter paints the same sanitised text.
func gitFooterSpans(snap gitstatus.Snapshot, glyphs glyphSet) []footerSpan {
	var spans []footerSpan
	if branch := stripEscapes(snap.Branch); branch != "" && !snap.Detached {
		spans = append(spans, footerSpan{text: branch})
	}
	count := func(n int, mark string) {
		spans = append(spans, footerSpan{text: strconv.Itoa(n) + mark, bright: n != 0})
	}
	count(snap.Dirty, glyphs.gitDirty)
	if snap.HasAheadBehind {
		count(snap.Behind, glyphs.gitBehind)
		count(snap.Ahead, glyphs.gitAhead)
	}
	return spans
}

// footerSpansText is the git segment's plain text — its spans space-joined — the one string the
// fit measures and the layout reports, and the one the painter's toned spans must re-join to.
func footerSpansText(spans []footerSpan) string {
	texts := make([]string, len(spans))
	for i, span := range spans {
		texts[i] = span.text
	}
	return strings.Join(texts, " ")
}
