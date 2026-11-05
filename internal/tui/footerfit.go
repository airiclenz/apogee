package tui

import (
	"path/filepath"
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
// Before the ladder gives anything up, the row SHORTENS: the workdir middle-elides stepwise, its
// head and its last components kept and the middle given to an ellipsis (`~/repos/…/internal/tui`,
// then `~/…/tui`), down to its floor `…/<last>`, and only then the branch middle-elides, to the
// width the row has left, down to its own floor ([footerBranchFloorCells]). Both are the long,
// free-form facts of the row, and a shortened path or branch still names which one it is, where a
// dropped git segment or effort word says nothing at all. The shortening belongs to the full run
// alone: once a segment has dropped, the rung composes the full workdir and branch again, since the
// drop is what bought the room.
//
// The whole layout comes out of ONE call. The footer has two readers of its arithmetic — the
// painter and the pointer that addresses the marker's cells and the git segment's
// ([Model.handleFooterModeClick], [Model.handleFooterGitClick]) — and
// two arithmetics that agree today are two that can disagree tomorrow, so this returns the whole
// row and both read it. Measurement is the injected [widthAuthority] and nothing else (ADR 0030
// §5): a footer laid out in a measure the painter did not choose is off by a column per wide
// grapheme, and the marker is right-anchored, so that column lands on the mode word.

// footerSegmentID names one of the footer's segments — the word a row layout lists it by. Six of
// them are left-run facts the fit lays out in the order the row names them and drops by priority
// ([footerSegmentID.priority]); footerSegMode is where the mode marker sits, and the fit carries
// the marker as its own atom ([footerInput.mode]) rather than as a left-run segment, because it is
// the one thing the row never gives up. `offline` is not a segment at all: it is the state a send
// is refused in, worded beside the marker ([footerInput.offlineRun]).
type footerSegmentID string

// The footer's segment names, in today's reading order: the upstream facts outward-in, then the
// local ones, then the session's running tally.
const (
	footerSegHost    footerSegmentID = "host"
	footerSegModel   footerSegmentID = "model"
	footerSegEffort  footerSegmentID = "effort"
	footerSegMode    footerSegmentID = "mode"
	footerSegWorkdir footerSegmentID = "workdir"
	footerSegGit     footerSegmentID = "git"
	footerSegSpend   footerSegmentID = "spend"
)

// footerTopPriority is the highest priority a segment carries — the first one a narrowing row gives
// up — and the ladder's starting rung keeps every segment below it plus one.
const footerTopPriority = 5

// priority is the rung a segment leaves the row on: the higher, the sooner. The session's spend
// goes first (5), then the workspace's git state (4), then the row spends its columns outward-in —
// the effort word (3), the workdir (2), the host (1). The model is priority 0, the floor the ladder
// never drops ([footerFit] spends it in halves instead), and so is any name the fit does not
// know, which keeps the fit total rather than silently losing a segment.
func (id footerSegmentID) priority() int {
	switch id {
	case footerSegSpend:
		return 5
	case footerSegGit:
		return 4
	case footerSegEffort:
		return 3
	case footerSegWorkdir:
		return 2
	case footerSegHost:
		return 1
	default:
		return 0
	}
}

// footerSegment is one named fact of the left run: its name and its plain text, empty where
// nothing has named it. pieces is the git segment's toned words ([gitFooterSpans]) and nil for
// every other segment; where it is set, text is exactly those pieces re-joined
// ([footerSpansText]), so a fit that elides the branch rewrites both together ([gitFooterSegment]).
type footerSegment struct {
	id     footerSegmentID
	text   string
	pieces []footerSpan
}

// The ellipsis the footer's middle-elision puts where the text it gave up stood.
const footerEllipsis = "…"

// footerBranchFloorCells is the fewest cells of the branch's own name a middle-elided branch keeps
// — split around the ellipsis, the extra cell to the head — so the shortest branch the row states
// still reads as its own (`feat…name`) rather than as two letters. A branch no wider than this
// floor and its ellipsis is never elided.
const footerBranchFloorCells = 8

// gitFooterSegment is the git segment worded from its pieces: the pieces themselves, for the
// painter to tone one by one, and their plain re-join, for the fit to measure. No pieces is no
// segment — empty, so it leaves the row with its separator.
func gitFooterSegment(pieces []footerSpan) footerSegment {
	return footerSegment{id: footerSegGit, text: footerSpansText(pieces), pieces: pieces}
}

// footerInput is everything the fit needs, and all of it is plain text: no styles, no escape
// sequences, no arithmetic already done. segments are the facts the left run is composed from, in
// the order the row lays them out, each one empty where nothing has named it; mode is the
// already-worded mode marker (its symbol, its word, and in Auto the blast radius that word runs
// with), which the fit treats as ONE atom and never splits, and offline the bare word for the state
// a send is refused in.
//
// width is the window, margin the width of the row's own lead and trailing margins ([bodyIndent]
// in the painter's measure), and measure the authority every width in this file is taken with.
type footerInput struct {
	segments []footerSegment
	offline  string

	mode string

	width   int
	margin  int
	measure widthAuthority
	glyphs  glyphSet
}

// segment is the text of the named segment, or "" where the row carries none by that name.
func (in footerInput) segment(id footerSegmentID) string {
	for _, seg := range in.segments {
		if seg.id == id {
			return seg.text
		}
	}
	return ""
}

// footerSegmentSpan is where one kept segment sits on the composed row: its text as the row shows
// it, at the byte offset in the layout's info run that text begins at, and its cell span — the
// screen column of its first cell, the row's lead margin included, and the cells it covers. The
// text is the segment's own — middle-elided where the fit shortened it — unless the priority-0
// rung cut it, in which case it is the cut text, ellipsis and all.
//
// pieces is the git segment's toned words as the row shows them, an elided branch included, so
// the painter tones the very words the fit measured ([Model.footerGit]); it is nil for every other
// segment and for a segment the cut reached.
type footerSegmentSpan struct {
	id     footerSegmentID
	text   string
	pieces []footerSpan
	at     int
	col    int
	cells  int
}

// footerLayout is the composed row: the three plain runs the painter styles separately — info on
// the footer's own tone, offline in the error tone, mode in the mode's colour — plus col, the
// screen column the marker's first cell lands on, and hasMode, false only in the floor case where
// the marker cannot seat whole and the row keeps the older truncated shape.
//
// spans is the per-segment table of the run the ladder KEPT, in row order: one entry per segment
// that shows, naming where it sits ([footerSegmentSpan]), so the painter can tone a segment span
// by span (the git counts) without searching the run for it — a branch may well be spelled like
// the model. A segment the ladder dropped, or that nothing named, has no entry, and the floor —
// whose truncated run is painted plain — has none at all.
//
// info and offline are already joined and already reduced: a segment the ladder dropped left WITH
// its separator, so neither run ever begins or ends on a dangling ✦. When hasMode is false, col is
// 0 and the two left runs are truncated to the window — there is no marker to address, which is
// why a click on the footer then names nothing.
type footerLayout struct {
	info    string
	offline string
	spans   []footerSegmentSpan
	mode    string
	col     int
	hasMode bool
}

// span is the named segment's entry in the layout's span table, and false where the row does not
// show it.
func (l footerLayout) span(id footerSegmentID) (footerSegmentSpan, bool) {
	for _, span := range l.spans {
		if span.id == id {
			return span, true
		}
	}
	return footerSegmentSpan{}, false
}

// footerFit composes one footer row to the width it has, dropping segments in priority order
// until the left run and the mode marker both fit. It is pure — the same input always yields the
// same layout — and TOTAL: every width, including 0 and a negative one, yields a layout the
// painter can spend.
//
// "Fits" is the painter's own test, unchanged: at least one blank column between the left run and
// the marker, so the two ends never touch. The rungs, in order, stopping at the first that fits:
// the full run; the full run with the workdir middle-elided one step at a time down to its floor,
// and then with the branch middle-elided too ([footerInput.shortenings]); and then the run without
// every segment of priority 5, of 4 and above, and so on
// down to 1 ([footerSegmentID.priority]) — today the spend, the git segment, the effort word, the
// workdir and the host, in that order; then the priority-0 run (the model) truncated to what is
// left; that run gone; offline gone. The last rung is the marker alone between its two margins,
// and there the blank column is not asked for — with no left run there is nothing for it to
// separate the marker from.
func footerFit(in footerInput) footerLayout {
	full := in.keptBelow(footerTopPriority + 1)
	if layout, ok := in.fit(full); ok {
		return layout
	}
	for _, kept := range in.shortenings(full) {
		if layout, ok := in.fit(kept); ok {
			return layout
		}
	}

	// The priority drops, richest first: each rung keeps the segments below its limit, in row
	// order, at their full text. A rung whose dropped segments were already absent composes the
	// same run as the one above it and fails the same way, so an absent spend, git segment or
	// effort word simply hands the next rung on — nothing has to know which segments are named.
	for limit := footerTopPriority; limit > 0; limit-- {
		if layout, ok := in.fit(in.keptBelow(limit)); ok {
			return layout
		}
	}

	// Priority 0. The model is the last fact of the four to go, and it gives way in halves: an
	// ellipsis first, so a wide-enough window still names which model is answering, and only then
	// the segment whole.
	floor := in.keptBelow(1)
	run := footerRun(in.glyphs, segmentTexts(floor)...)
	if budget := in.floorBudget(run); budget > 0 {
		cut := in.measure.Truncate(run, budget, "…")
		if layout, ok := in.seat(cut); ok {
			layout.spans = in.spansOf(floor, cut)
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

// fit composes the run of kept and seats the marker against it, naming every kept segment's span
// when the two ends fit.
func (in footerInput) fit(kept []footerSegment) (footerLayout, bool) {
	info := footerRun(in.glyphs, segmentTexts(kept)...)
	layout, ok := in.seat(info)
	if ok {
		layout.spans = in.spansOf(kept, info)
	}
	return layout, ok
}

// shortenings is the full run's rungs between "everything" and the first drop, narrowest last:
// the workdir middle-elided one step after another ([elidePath]), and then, with the workdir at
// its floor, the branch middle-elided to exactly the width the row still lacks
// ([elideMiddle]). A branch that would have to go below its floor to fit adds no rung — the row
// does not fit that way, and the ladder's first drop is what comes next.
func (in footerInput) shortenings(full []footerSegment) [][]footerSegment {
	var rungs [][]footerSegment
	kept := full
	for i, seg := range kept {
		if seg.id != footerSegWorkdir {
			continue
		}
		for _, shorter := range elidePath(seg.text, in.measure) {
			kept = withSegmentAt(kept, i, footerSegment{id: seg.id, text: shorter})
			rungs = append(rungs, kept)
		}
	}
	for i, seg := range kept {
		branchAt := footerBranchPiece(seg.pieces)
		if seg.id != footerSegGit || branchAt < 0 {
			continue
		}
		name := seg.pieces[branchAt].text
		target := in.measure.Width(name) + in.slack(footerRun(in.glyphs, segmentTexts(kept)...))
		if target < in.measure.Width(name) && target >= footerBranchFloorCells+in.measure.Width(footerEllipsis) {
			pieces := append([]footerSpan(nil), seg.pieces...)
			pieces[branchAt].text = elideMiddle(name, target, in.measure)
			rungs = append(rungs, withSegmentAt(kept, i, gitFooterSegment(pieces)))
		}
	}
	return rungs
}

// withSegmentAt is segments with the i-th replaced by seg — on a copy, so a rung never rewrites
// the run of the rung before it.
func withSegmentAt(segments []footerSegment, i int, seg footerSegment) []footerSegment {
	out := append([]footerSegment(nil), segments...)
	out[i] = seg
	return out
}

// footerBranchPiece is the index of the branch among the git segment's pieces, or -1 where it
// names none (a detached HEAD, or no git segment at all).
func footerBranchPiece(pieces []footerSpan) int {
	for i, piece := range pieces {
		if piece.isBranch {
			return i
		}
	}
	return -1
}

// elidePath is a path's middle-elided forms, each strictly narrower than the one before: the path's
// head and its tail kept k components deep and the middle given to an ellipsis, k shrinking to 1
// (`~/repos/apogee/internal/tui` → `~/repos/…/internal/tui` → `~/…/tui`), and last the floor, the
// ellipsis and the final component alone (`…/tui`). A rooted path keeps its root ahead of its head
// (`/home/…/tui`). The forms are measured in the authority the row is, so a form only counts as a
// step where it saves a cell; a path too shallow to save one has no forms at all.
func elidePath(path string, measure widthAuthority) []string {
	sep := "/"
	if filepath.Separator != '/' && strings.ContainsRune(path, filepath.Separator) {
		sep = string(filepath.Separator)
	}
	root := ""
	if strings.HasPrefix(path, sep) {
		root = sep
	}
	parts := strings.Split(strings.Trim(path, sep), sep)
	if len(parts) < 2 {
		return nil
	}
	var forms []string
	width := measure.Width(path)
	add := func(form string) {
		if w := measure.Width(form); w < width {
			forms, width = append(forms, form), w
		}
	}
	for keep := (len(parts) - 1) / 2; keep >= 1; keep-- {
		head := strings.Join(parts[:keep], sep)
		tail := strings.Join(parts[len(parts)-keep:], sep)
		add(root + head + sep + footerEllipsis + sep + tail)
	}
	add(footerEllipsis + sep + parts[len(parts)-1])
	return forms
}

// elideMiddle cuts text to at most width cells by giving its middle to an ellipsis: the head keeps
// half of what remains, the extra cell to it, and the tail the rest. A wide glyph that would
// straddle either cut goes whole, so the result may fall a cell short of width but never past it.
func elideMiddle(text string, width int, measure widthAuthority) string {
	total := measure.Width(text)
	if total <= width {
		return text
	}
	room := max(0, width-measure.Width(footerEllipsis))
	headCells := (room + 1) / 2
	tailCells := room - headCells
	// A cut from the left keeps a glyph it lands inside of, so the tail starts a cell later until
	// that glyph has gone whole.
	tail := ""
	for from := total - tailCells; from < total; from++ {
		if tail = measure.Cut(text, from, total); measure.Width(tail) <= tailCells {
			break
		}
	}
	return measure.Truncate(text, headCells, "") + footerEllipsis + tail
}

// keptBelow is the named segments whose priority is below limit, in row order — the run one rung
// of the ladder composes.
func (in footerInput) keptBelow(limit int) []footerSegment {
	var kept []footerSegment
	for _, seg := range in.segments {
		if seg.text != "" && seg.id.priority() < limit {
			kept = append(kept, seg)
		}
	}
	return kept
}

// segmentTexts is the segments' texts, in order — what [footerRun] joins.
func segmentTexts(segments []footerSegment) []string {
	texts := make([]string, len(segments))
	for i, seg := range segments {
		texts[i] = seg.text
	}
	return texts
}

// spansOf is the span table of info, a run composed from kept: each segment placed past the ones
// before it and the separator that introduces it. It restates no arithmetic of its own — the
// prefix is joined by [footerRun], the joiner the run itself was composed with — and it stops at
// the first segment the run does not hold whole, naming that one by the cut text it shows, so a
// run the priority-0 rung truncated still names its model.
func (in footerInput) spansOf(kept []footerSegment, info string) []footerSegmentSpan {
	sep := " " + in.glyphs.assistant + " "
	spans := make([]footerSegmentSpan, 0, len(kept))
	prefix := ""
	for i, seg := range kept {
		if i > 0 {
			prefix += sep
		}
		if !strings.HasPrefix(info, prefix) || len(info) == len(prefix) {
			break
		}
		text := seg.text
		cut := !strings.HasPrefix(info[len(prefix):], text)
		if cut {
			text = info[len(prefix):]
		}
		span := footerSegmentSpan{
			id:    seg.id,
			text:  text,
			at:    len(prefix),
			col:   in.margin + in.measure.Width(prefix),
			cells: in.measure.Width(text),
		}
		if !cut {
			span.pieces = seg.pieces
		}
		spans = append(spans, span)
		if cut {
			break
		}
		prefix += seg.text
	}
	return spans
}

// seat places the marker against a candidate left run and reports whether the two ends fit. col is
// the marker's right-anchored first column — the slot's own trailing margin sits between its last
// cell and the window edge, the very column the status line's right slot ends in.
func (in footerInput) seat(info string) (footerLayout, bool) {
	if in.slack(info) < 0 {
		return footerLayout{}, false
	}
	offline := in.offlineRun(info)
	col := in.width - in.margin - in.measure.Width(in.mode)
	return footerLayout{info: info, offline: offline, mode: in.mode, col: col, hasMode: true}, true
}

// slack is how many columns a candidate left run leaves spare once the marker is seated against it
// with the one blank column between the two ends: zero is a run that just fits, and a negative
// slack is the cells the run must still give up.
func (in footerInput) slack(info string) int {
	left := in.margin + in.measure.Width(info) + in.measure.Width(in.offlineRun(info))
	col := in.width - in.margin - in.measure.Width(in.mode)
	return col - left - 1
}

// floorBudget is the width the priority-0 run (the model) may still spend once every other
// segment has gone: the window less both margins, the offline run, the marker, and the one blank
// column that separates the two ends. It can be zero or negative, which is the fit's own signal
// that truncating the run cannot help and it has to go whole.
func (in footerInput) floorBudget(run string) int {
	return in.width - in.margin - in.margin -
		in.measure.Width(in.offlineRun(run)) - in.measure.Width(in.mode) - 1
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
// — truncated to the window with an ellipsis. Every segment rides in that run as plain text: the
// layout names no spans, so a cut that lands inside the git segment is painted in the footer's own tone. The two runs are cut as a pair so offline keeps its
// own styled run and its error tone rather than being folded into the info run to be truncated
// with it.
func (in footerInput) floor() footerLayout {
	info := footerRun(in.glyphs, segmentTexts(in.segments)...)
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

// footerTone is the meaning a piece of the footer's git segment is read in, and through it the
// scheme role it is painted in ([theme.footerToneStyle]). The zero value is the footer's own muted
// tone, so a piece nothing has toned stays quiet.
type footerTone int

const (
	// footerToneMuted is the footer's own tone: the branch, and a count that is zero.
	footerToneMuted footerTone = iota
	// footerToneWarning is the `warning` role: work not yet where it belongs — changed paths not
	// committed, upstream commits not yet merged.
	footerToneWarning
	// footerToneSuccess is the `success` role: commits made here and ready to push.
	footerToneSuccess
)

// footerSpan is one piece of the footer's git segment and the tone it is read in
// ([footerTone]): the branch and a zero count sit in the footer's own muted tone, a non-zero `Δ`
// or `↓` in the warning tone and a non-zero `↑` in the success tone, so a tree with something to
// say lifts out of the row in the colour of what it says while a clean one stays quiet. isBranch
// marks the one piece the fit may middle-elide ([footerInput.shortenings]): a count is never
// shortened.
type footerSpan struct {
	text     string
	tone     footerTone
	isBranch bool
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
		spans = append(spans, footerSpan{text: branch, isBranch: true})
	}
	count := func(n int, mark string, tone footerTone) {
		if n == 0 {
			tone = footerToneMuted
		}
		spans = append(spans, footerSpan{text: strconv.Itoa(n) + mark, tone: tone})
	}
	count(snap.Dirty, glyphs.gitDirty, footerToneWarning)
	if snap.HasAheadBehind {
		count(snap.Behind, glyphs.gitBehind, footerToneWarning)
		count(snap.Ahead, glyphs.gitAhead, footerToneSuccess)
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
