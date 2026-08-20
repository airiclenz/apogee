package tui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// viewDiffBody is view_diff's body hook: the coloured diff read off the result's prose, which is
// where that tool's body lives (diffBody). The result-shaped signature is the registry's
// (toolPresenter.body) — read_file's body is read off its typed summary instead.
//
// It is now that tool's FLOOR rather than its usual reading: a body whose lines carry the diff's
// tags is walked into Edit regions and painted as their rows instead (viewDiffRegions), and what
// still renders from here is the output that carries none — the over-budget diffstat-only
// sentence, which is prose about a diff rather than one.
func viewDiffBody(res domain.ToolResult) []detailLine {
	return diffBody(res.Content)
}

// diffBody renders view_diff's unified output as the coloured body beneath the row — "+ "
// lines green, "- " lines red, context plain (layout.md, "A change is coloured the one way wherever
// a block shows one"; docs/layout/tool-layout.md's per-tool table). Tagging on the
// leading "+"/"-" is exact here because internal/tools' unifiedLineDiff tags every line "  ",
// "- " or "+ " and emits no "+++ b/…" / "--- a/…" file header, so a content line that itself
// starts with "+" always arrives behind a tag. It returns every line: the collapsed paint's cap
// and its remainder marker are the painter's (collapsedBodyRows, collapsedDetails, render.go).
//
// It counts NOTHING. The "+A −R" diffstat in the outcome slot of the branch above it comes from
// the tool's domain.DiffStat, counted from the diff operations themselves — which is why the
// stat still describes the whole diff when the collapsed paint stops at the cap, and why a "No
// changes detected" result (no diff, hence no stat) never reaches here at all. That last rule is also
// what makes the painter's kind-sniffing exact: a body from here always carries a tagged line.
func diffBody(content string) []detailLine {
	lines := splitLines(strings.TrimRight(content, "\n"))
	body := make([]detailLine, 0, len(lines))
	for _, ln := range lines {
		kind := detailPlain
		switch {
		case strings.HasPrefix(ln, "+"):
			kind = detailDiffAdded
		case strings.HasPrefix(ln, "-"):
			kind = detailDiffRemoved
		}
		body = append(body, detailLine{Kind: kind, Text: clipDetail(ln)})
	}
	return body
}

// diffRegionContext is how many unchanged lines a RECOVERED region carries each side of its
// change. Three is the ratified figure and necessarily the same three an edit tool records
// (internal/tools' editRegionContext, ADR 0052): the two are one layout — a recovered region
// paints through the very rows a recorded one does (stackedDiffLines) — so a reader must not be
// able to tell which kind of diff block they are looking at by counting its context.
const diffRegionContext = 3

// The two-cell tags a rendered line diff wears down its left edge: context, removal, addition.
// Reading them is exact because internal/tools' unifiedLineDiff puts EVERY line behind one of
// them and emits no "+++ b/…" file header, so a file line that itself begins with "+" always
// arrives behind a tag of its own.
//
// They are the same two cells the stacked reading paints as its marker column
// (stackedContextMarker and its pair) and are kept apart from them deliberately: these three are
// what a tool's output IS, those three are what this package DRAWS, and the day either side moves
// the other must not follow it silently.
const (
	diffContextTag = "  "
	diffRemovedTag = "- "
	diffAddedTag   = "+ "
)

// viewDiffRegions recovers the Edit regions of view_diff's body. That tool applies nothing, so
// there is no apply time for it to record them at (ADR 0052 §2) — but what it prints is a
// WHOLE-FILE diff, every line of both files tagged and none of them elided, so the numbers a
// region needs are simply the lines counted from 1. The result-shaped signature is the registry's
// (toolPresenter.regions).
//
// Expanded view_diff therefore stops painting the whole file: it shows the changed regions with
// three lines of context each side, exactly as an edit block shows them. That is the ratified
// behaviour change and not a truncation — the diff the MODEL reads is untouched, and the slot's
// diffstat still counts the whole of it (diffStatStat).
//
// It answers as ONE nameless section (diffFileRegions): view_diff diffs a single file and its
// output never names it, so there is nothing for a header row to say that the block's own row does
// not already say better.
func viewDiffRegions(res domain.ToolResult) []diffFileRegions {
	regions := taggedDiffRegions(res.Content)
	if len(regions) == 0 {
		return nil
	}
	return []diffFileRegions{{Regions: regions}}
}

// taggedDiffRegions cuts a tagged line diff into regions, walking it once and counting each file's
// lines from 1. It is the recovery half of the builder internal/tools runs over the two TEXTS at
// apply time (editRegions), and it answers exactly as that one does: one region per run of
// consecutive changed lines, up to diffRegionContext unchanged lines of context each side, and
// neighbouring changes left as SEPARATE regions whose context tiles the lines between them without
// overlap — the earlier region takes up to three of them as trailing context and the later takes
// what is left. Two regions that end up meeting are painted with no elision rule between them
// (regionsMeet), so the reading is the one a merge would have given.
//
// Removed and Inserted hold changed lines only, so the regions add up to the diffstat the tool
// counted over the same operations — this walk re-derives the POSITIONS the rendered diff dropped,
// never the counts, which stay the tool's (domain.DiffStat).
//
// It is total and all-or-nothing: a line carrying none of the three tags says this output is not a
// rendered diff at all — the "No changes detected" sentinel, the over-budget diffstat-only
// sentence — and NO regions come back, which leaves the whole body rendering plain (diffBody)
// rather than half-walked.
func taggedDiffRegions(content string) []domain.EditRegion {
	cutter := diffRegionCutter{beforeLine: 1, afterLine: 1}
	for _, line := range splitLines(strings.TrimRight(content, "\n")) {
		tag, text, ok := diffTagOf(line)
		if !ok {
			return nil
		}
		cutter.take(tag, text)
	}
	return cutter.finish()
}

// diffTagOf splits a rendered diff line into the tag it carries and the file line behind it. A
// line too short to carry one, or carrying anything else, is not a diff line — which includes the
// single empty line an empty body splits into, so prose and emptiness fail the same way.
func diffTagOf(line string) (tag, text string, ok bool) {
	const cells = len(diffContextTag) // one width for all three, which is what makes this a slice
	if len(line) < cells {
		return "", "", false
	}
	switch tag := line[:cells]; tag {
	case diffContextTag, diffRemovedTag, diffAddedTag:
		return tag, line[cells:], true
	}
	return "", "", false
}

// diffRegionCutter is the state of one walk down a tagged diff: the line the walk has reached in
// each file, the region being built, and the unchanged lines seen since that region's last changed
// line — lines whose role is not settled yet, because how many of them arrive before the next
// change is what says whether they are all this region's trailing context or only the first three
// are, with the rest falling to the next region's leading context.
type diffRegionCutter struct {
	regions []domain.EditRegion

	// open is the region under construction, nil between regions.
	open *domain.EditRegion

	// beforeLine and afterLine are the 1-based lines the next tagged line of each kind sits on.
	beforeLine int
	afterLine  int

	unchanged []string
}

// take folds one tagged line into the walk, advancing the counter of each file that line occupies
// a line of: a context line advances both, a removal only the before file, an insertion only the
// after file.
func (c *diffRegionCutter) take(tag, text string) {
	if tag == diffContextTag {
		c.unchanged = append(c.unchanged, text)
		c.beforeLine++
		c.afterLine++
		return
	}

	c.reachChange()
	if tag == diffRemovedTag {
		c.open.Removed = append(c.open.Removed, text)
		c.beforeLine++
		return
	}
	c.open.Inserted = append(c.open.Inserted, text)
	c.afterLine++
}

// reachChange readies an open region for a changed line. A change that follows the previous one
// with no unchanged line between them continues the same region; any unchanged line at all ends
// that region and opens the next, the two sharing out the lines between them.
func (c *diffRegionCutter) reachChange() {
	switch {
	case c.open == nil:
		c.begin()
	case len(c.unchanged) > 0:
		c.end()
		c.begin()
	}
}

// begin opens a region at the position the walk has reached, backing its start lines up over the
// leading context it takes — whatever the previous region left behind, or the run before the first
// change. Fewer lines are available at the head of a file, and the start lines then stop where the
// file does.
func (c *diffRegionCutter) begin() {
	leading := lastLines(c.unchanged, diffRegionContext)
	c.open = &domain.EditRegion{
		BeforeStart: c.beforeLine - len(leading),
		AfterStart:  c.afterLine - len(leading),
		Leading:     leading,
	}
	c.unchanged = nil
}

// end closes the open region with the trailing context it takes from the unchanged lines that
// follow it, and files it. It CONSUMES the lines it takes: what remains is what a following region
// may claim as its leading context, so no line is ever context for two regions at once.
func (c *diffRegionCutter) end() {
	trailing := firstLines(c.unchanged, diffRegionContext)
	c.open.Trailing = trailing
	c.regions = append(c.regions, *c.open)
	c.open = nil
	c.unchanged = c.unchanged[len(trailing):]
}

// hunk moves the walk to the lines a new hunk states each file resumes at, closing whatever region
// the previous hunk left open first. It is what a diff with ELISIONS in it needs and a whole-file
// one never asks for (taggedDiffRegions counts from 1 and never jumps).
//
// The unchanged lines it had not yet placed are DROPPED rather than carried over: git elided
// everything between the two hunks, so a line before that gap can be no region's leading context
// after it — the closing region takes its three as trailing context and the rest are simply lines
// this reading does not show.
func (c *diffRegionCutter) hunk(before, after int) {
	if c.open != nil {
		c.end()
	}
	c.unchanged = nil
	c.beforeLine, c.afterLine = before, after
}

// finish ends the walk, closing a region the last line left open, and returns the regions — none
// at all when the diff carried no changed line.
func (c *diffRegionCutter) finish() []domain.EditRegion {
	if c.open != nil {
		c.end()
	}
	return c.regions
}

// firstLines returns a copy of the first count lines, or of all of them when there are fewer. The
// copy is the point: a region outlives the walk — it is kept on the view for the session — and
// must not hold the walk's own buffer, where the lines it did not take would be retained with it.
func firstLines(lines []string, count int) []string {
	if len(lines) > count {
		lines = lines[:count]
	}
	return slices.Clone(lines)
}

// lastLines returns a copy of the last count lines, or of all of them when there are fewer — the
// mirror of firstLines, and a copy for the same reason.
func lastLines(lines []string, count int) []string {
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return slices.Clone(lines)
}

// editPair is one changed region an edit call ASKS FOR: the lines it removes and the lines it
// puts there instead. It is the shape all three edit tools reduce to — a single find-and-replace
// is one pair, a multi find-and-replace is its ordered list of them, a patch is one per hunk — so
// each tool's extractor has only to say what its own arguments mean and one renderer turns the
// answer into a body (changedLines). Either half may be empty: a pure insertion removes nothing,
// a deletion inserts nothing.
type editPair struct {
	removed  []string
	inserted []string
}

// replacedText builds the pair a find-and-replace argument names, splitting each side into the
// lines it changes (editLines).
func replacedText(removed, inserted string) editPair {
	return editPair{removed: editLines(removed), inserted: editLines(inserted)}
}

// editLines splits one side of an edit into the lines it changes. An empty side has none — there
// is no such thing as removing the empty string — and a single trailing newline is the last line's
// TERMINATOR rather than a line of its own, so a replacement written "a\nb\n" changes two lines and
// not two and a blank. Nothing else is dropped: what a body retains is every line it was given, and
// the compact shape is the painter's business (collapsedBodyRows, render.go).
func editLines(text string) []string {
	if text == "" {
		return nil
	}
	return splitLines(strings.TrimSuffix(text, "\n"))
}

// changedLines renders edit pairs as the display-only diff body an edit block hangs beneath its
// branch: per pair, the removed lines behind "- ", then the inserted lines behind "+ ", pairs in
// the order the call listed them. It is DERIVED FROM THE ARGUMENTS and goes nowhere near the wire
// — no tool result grows, no token is spent, and the model's own view of the call is untouched.
//
// The two tags are the ones diffBody emits, so the lines paint through the very red/green styles
// view_diff's hunks do; the house collapsed cap then holds an
// edit block to the same three rows as every other block (collapsedBodyRows, render.go). It
// truncates nothing — the entry keeps every line — and the per-line clip is the same 160-rune
// guard against a minified blob every other detail line carries.
//
// Pairs with nothing on either side yield NO body at all, which is what lets a call with absent or
// malformed arguments render exactly as it did before: a target, a summary, and nothing beneath.
func changedLines(pairs []editPair) []detailLine {
	n := 0
	for _, p := range pairs {
		n += len(p.removed) + len(p.inserted)
	}
	if n == 0 {
		return nil
	}
	body := make([]detailLine, 0, n)
	for _, p := range pairs {
		body = appendTagged(body, p.removed, "- ", detailDiffRemoved)
		body = appendTagged(body, p.inserted, "+ ", detailDiffAdded)
	}
	return body
}

// appendTagged appends one side of a pair, one detail line per line of text, each behind its diff
// tag and clipped like any other detail line.
func appendTagged(body []detailLine, lines []string, tag string, kind detailKind) []detailLine {
	for _, ln := range lines {
		body = append(body, detailLine{Kind: kind, Text: clipDetail(tag + ln)})
	}
	return body
}

// ----------------------------------------------------------------------------
// git_diff_range — the regions of git's own unified diff
// ----------------------------------------------------------------------------

// The line shapes git's unified diff is built out of. A file section opens on
// "diff --git a/<path> b/<path>", and a hunk within it opens on "@@ -a,b +c,d @@" — which is where
// this recovery gets its numbers, git having ELIDED everything between one hunk and the next. The
// no-newline marker is git's note ABOUT the line above it rather than a line of either file.
const (
	gitDiffFilePrefix = "diff --git "
	gitDiffHunkPrefix = "@@"
	gitDiffNoNewline  = `\ No newline at end of file`
)

// The two headers this recovery reads rather than skips: the file a section is about (the b-side
// path — the name the change left the file under) and the lines each side of a hunk resumes at. A
// count of 1 is written without its comma, which is why the counts are optional here; they are not
// read at all, because what a hunk holds is the lines it then prints.
var (
	gitDiffFilePattern = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)
	gitDiffHunkPattern = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
)

// gitDiffHeaderPrefixes are the extended-header lines a TEXTUAL diff carries between its file line
// and its first hunk: the blob index, the mode lines, and the two "---"/"+++" names. They say
// nothing this recovery needs — every number it wants is in a hunk header — so they are recognised
// and skipped.
//
// What is deliberately NOT here is any header saying the section carries no text to paint:
// "Binary files … differ", a rename's "similarity index" and "rename from"/"rename to", a "GIT
// binary patch". A line none of these prefixes claims stops the walk dead (gitDiffFileSections),
// so such a body keeps the plain output rendering it had before this existed — which is the honest
// answer for it: git said something about that file this reading cannot show.
var gitDiffHeaderPrefixes = []string{
	"index ", "--- ", "+++ ", "old mode ", "new mode ", "new file mode ", "deleted file mode ",
}

// gitDiffRangeRegions recovers the Edit regions of git_diff_range's output. That tool applies
// nothing either, so it has no apply time to record a change at (ADR 0052 §2) — and what it prints
// is git's own unified diff, which unlike view_diff's whole-file body shows only the neighbourhood
// of each change. The numbers therefore come from the "@@" header each hunk opens with, which is
// exactly what those headers are for. The result-shaped signature is the registry's
// (toolPresenter.regions).
//
// The answer is one section per file the range touched (ratified call 10): git's output spans
// files, each numbering its own lines, and the block paints a muted row naming the file over that
// file's regions.
func gitDiffRangeRegions(res domain.ToolResult) []diffFileRegions {
	return gitDiffFileSections(res.Content)
}

// gitDiffFileSections walks git's unified output into those sections. It shares its cutter with the
// walk down a whole-file diff (taggedDiffRegions), so a region recovered here and a region
// recovered there are the same thing and paint through the same rows: one region per run of
// consecutive changed lines, up to diffRegionContext unchanged lines of context each side, and
// neighbouring changes left as separate regions whose context tiles the lines between them.
//
// It is TOTAL and ALL-OR-NOTHING. A line no rule above claims — a binary section's "Binary files …
// differ", a rename's "similarity index", the columns of a `--stat` call, the "No differences
// found" sentinel, a warning that reached the output — returns NO sections at all, and the whole
// body then renders as the plain output it always did (outputDetail). So does a file section that
// yielded no region: a body painted with one of its files silently missing is exactly the
// half-parsed mix the fallback exists to avoid.
func gitDiffFileSections(content string) []diffFileRegions {
	var walk gitDiffWalk
	for _, line := range splitLines(strings.TrimRight(content, "\n")) {
		if !walk.take(line) {
			return nil
		}
	}
	if !walk.closeFile() {
		return nil
	}
	return walk.sections
}

// gitDiffWalk is the state of one walk down a printed git diff: the sections closed so far, the
// file the open one is about, and the region cutter filling it. inHunk is what tells a header line
// from a line of file content — the two are told apart by WHERE they stand, since a hunk's content
// lines can spell anything at all behind their one-cell tag.
type gitDiffWalk struct {
	sections []diffFileRegions
	file     string
	open     bool
	inHunk   bool
	cutter   diffRegionCutter
}

// take folds one line of the output into the walk and reports whether it belonged to a diff at all.
// A false answer is final: the caller abandons the whole reading rather than skipping the line.
func (w *gitDiffWalk) take(line string) bool {
	switch {
	case strings.HasPrefix(line, gitDiffFilePrefix):
		m := gitDiffFilePattern.FindStringSubmatch(line)
		return m != nil && w.startFile(m[2])
	case !w.open:
		return false
	case strings.HasPrefix(line, gitDiffHunkPrefix):
		return w.startHunk(line)
	case w.inHunk:
		return w.takeHunkLine(line)
	default:
		return gitDiffHeaderLine(line)
	}
}

// startFile closes the section the walk was in and opens one for path.
func (w *gitDiffWalk) startFile(path string) bool {
	if !w.closeFile() {
		return false
	}
	w.file, w.open, w.inHunk, w.cutter = path, true, false, diffRegionCutter{}
	return true
}

// closeFile files the open section and reports whether it held a change to paint. A section that
// yielded no region — a rename, a mode change, a binary file — fails the walk instead of being
// dropped, because dropping it would show a diff of fewer files than the tool printed.
func (w *gitDiffWalk) closeFile() bool {
	if !w.open {
		return true
	}
	regions := w.cutter.finish()
	if len(regions) == 0 {
		return false
	}
	w.sections = append(w.sections, diffFileRegions{File: w.file, Regions: regions})
	return true
}

// startHunk reads the two starting lines off a hunk header and moves the cutter to them. A "@@"
// line that is not a hunk header is not something this reading can place, so it fails the walk.
func (w *gitDiffWalk) startHunk(line string) bool {
	m := gitDiffHunkPattern.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	before, err := strconv.Atoi(m[1])
	if err != nil {
		return false
	}
	after, err := strconv.Atoi(m[2])
	if err != nil {
		return false
	}
	w.cutter.hunk(before, after)
	w.inHunk = true
	return true
}

// takeHunkLine folds one line of a hunk's body into the cutter. git tags such a line in ONE cell —
// a space, a "-" or a "+" — where the diff internal/tools renders tags in two (diffContextTag), so
// the tag is translated here and the cutter goes on reading a single shape of them.
//
// The no-newline marker is skipped rather than counted: it is a note about the line above it, and
// taking it for a line of the file would push every number after it one out.
func (w *gitDiffWalk) takeHunkLine(line string) bool {
	if line == gitDiffNoNewline {
		return true
	}
	switch {
	case strings.HasPrefix(line, " "):
		w.cutter.take(diffContextTag, line[1:])
	case strings.HasPrefix(line, "-"):
		w.cutter.take(diffRemovedTag, line[1:])
	case strings.HasPrefix(line, "+"):
		w.cutter.take(diffAddedTag, line[1:])
	default:
		return false
	}
	return true
}

// gitDiffHeaderLine reports whether a line standing between a file's "diff --git" line and its
// first hunk is one of the extended headers this reading skips (gitDiffHeaderPrefixes).
func gitDiffHeaderLine(line string) bool {
	for _, prefix := range gitDiffHeaderPrefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// The marker column of the stacked reading: two cells, the same width in every row, carrying `-` on
// a removed line, `+` on an inserted one and nothing on context (docs/layout/split-diff-layout.md).
// The glyphs are the change's palette-proof signal — colour never carries it alone (ratified call 6)
// — and they are the very tags the argument-derived body already writes (changedLines), so the two
// bodies an edit block can show read as one thing.
const (
	stackedRemovedMarker  = "- "
	stackedInsertedMarker = "+ "
	stackedContextMarker  = "  "
)

// stackedRegionRuleCells is how wide the damped `⋯` rule between two regions is drawn. The rule
// stands for the lines elided between them, and it is a fixed short run rather than one spanning
// the body: these are detail lines, built with no width in hand — the block's width is the
// painter's, settled at paint time — and a run long enough to fill a wide block would wrap onto a
// second row in a narrow one, which is a rule that reads as two.
const stackedRegionRuleCells = 8

// stackedDiffLines renders recorded Edit regions as the STACKED reading of a diff body: per region
// its leading context, its removed lines behind `-` at their before-file numbers, its inserted
// lines behind `+` at their after-file numbers, then its trailing context — the layout
// docs/layout/split-diff-layout.md sketches, and the reading a body falls back to at every width
// the split panes do not fit (ratified call 5).
//
// It is the ONE builder of those rows. Every block that has regions renders through it — the three
// edit tools, whose tools record them at apply time, and the two diff tools, whose renderers
// recover them — so the narrow reading of a diff cannot come to differ per tool.
//
// The number gutter is sized once for the whole body and right-aligned, so the numbers line up
// down the block however far apart the regions are. Context rows carry the BEFORE file's number:
// the column then reads as one file's numbering, with the inserted lines — which have no before
// line at all — marked as the exceptions they are. Wrapping is nobody's business here: a row too
// wide for the block wraps at paint time through the same machinery every other detail line does
// (hangingWrap), and the per-line clip is the 160-rune ceiling they all answer to (clipDetail).
//
// No regions is no body, which is what leaves a call with nothing recorded showing the
// argument-derived lines it was presented with (ratified call 9).
func stackedDiffLines(regions []domain.EditRegion) []detailLine {
	rows := stackedRows(regions)
	if len(rows) == 0 {
		return nil
	}
	gutter := stackedGutter(rows)
	out := make([]detailLine, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.line(gutter))
	}
	return out
}

// stackedRow is one row of the stacked reading before the body's gutter width is known: the line
// number it shows, the marker column it wears, the colour its kind gives it, and its text. A row
// with number 0 shows no number and no marker — the `⋯` rule between two regions is the only such
// row — which is what lets the sizing pass below ignore it without a second shape.
type stackedRow struct {
	number int
	marker string
	kind   detailKind
	text   string
}

// line is the row as a detail line, its number right-aligned into a gutter of the given width. The
// composed text is clipped like every other detail line, and clipping the whole row rather than its
// text alone is deliberate: the cut takes the tail, so the number and the marker always survive it.
func (r stackedRow) line(gutter int) detailLine {
	if r.number == 0 {
		return detailLine{Kind: r.kind, Text: r.text}
	}
	return detailLine{Kind: r.kind, Text: clipDetail(fmt.Sprintf("%*d %s%s", gutter, r.number, r.marker, r.text))}
}

// stackedRows lays the regions out as unsized rows, in file order, with the `⋯` rule laid between
// two regions that do NOT meet in the file's numbering (regionsMeet). Regions that DO meet are
// painted end to end with nothing between them: a tool records neighbouring changes as separate
// regions whose context tiles the lines between them without overlap (domain.EditRegion), so the
// rows already run continuously and a rule there would claim an elision that did not happen.
func stackedRows(regions []domain.EditRegion) []stackedRow {
	rows := make([]stackedRow, 0, len(regions)*4)
	for i, region := range regions {
		if i > 0 && !regionsMeet(regions[i-1], region) {
			rows = append(rows, stackedRow{text: strings.Repeat(glyphLeaderDot, stackedRegionRuleCells)})
		}
		before, after := region.BeforeStart, region.AfterStart
		for _, text := range region.Leading {
			rows = append(rows, stackedRow{number: before, marker: stackedContextMarker, text: text})
			before, after = before+1, after+1
		}
		for _, text := range region.Removed {
			rows = append(rows, stackedRow{number: before, marker: stackedRemovedMarker, kind: detailDiffRemoved, text: text})
			before++
		}
		for _, text := range region.Inserted {
			rows = append(rows, stackedRow{number: after, marker: stackedInsertedMarker, kind: detailDiffAdded, text: text})
			after++
		}
		for _, text := range region.Trailing {
			rows = append(rows, stackedRow{number: before, marker: stackedContextMarker, text: text})
			before++
		}
	}
	return rows
}

// regionsMeet reports whether the later of two regions starts on the very line the earlier one
// ends: its BeforeStart against the earlier region's own span in the before file, which is its
// context and its removed lines (an inserted line occupies no line of that file).
//
// It is the elision question and nothing else. Two regions that meet were cut apart only because
// each change gets its own record; painted end to end they read exactly as one region would, which
// is what the tiling rule was chosen to make true (domain.EditRegion).
func regionsMeet(prev, next domain.EditRegion) bool {
	return next.BeforeStart == prev.BeforeStart+len(prev.Leading)+len(prev.Removed)+len(prev.Trailing)
}

// stackedGutter is how many cells the body's number column takes: the digits of the widest number
// any of its rows shows. One width for the whole body is what makes the numbers a column rather
// than a ragged edge, and it is measured over the rows themselves so a region whose after-file
// numbers have drifted past its before-file ones widens it like any other.
func stackedGutter(rows []stackedRow) int {
	widest := 0
	for _, row := range rows {
		if row.number > widest {
			widest = row.number
		}
	}
	return len(strconv.Itoa(widest))
}

// singleReplacementBody derives single_find_and_replace's changed lines from its own arguments:
// the one oldText → newText pair the call asks for.
func singleReplacementBody(args map[string]any) []detailLine {
	removed, _ := args["oldText"].(string)
	inserted, _ := args["newText"].(string)
	return changedLines([]editPair{replacedText(removed, inserted)})
}

// multiReplacementBody derives multi_find_and_replace's changed lines: one pair per entry of the
// replacements array, in the order the tool applies them (sequentially, internal/tools), so the
// body reads in the order the edit happens. An entry that is not an object is skipped rather than
// guessed at — a malformed argument shows fewer pairs, never a wrong one.
func multiReplacementBody(args map[string]any) []detailLine {
	list, ok := args["replacements"].([]any)
	if !ok {
		return nil
	}
	pairs := make([]editPair, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		removed, _ := m["oldText"].(string)
		inserted, _ := m["newText"].(string)
		pairs = append(pairs, replacedText(removed, inserted))
	}
	return changedLines(pairs)
}

// fileEditBody derives edit_existing_file's changed lines from its content argument, which the
// tool reads in one of two ways and so does this: a "*** Begin Patch" block is a list of hunks,
// each of them a pair (patchEditPairs), and anything else is full replacement content — one pair
// that removes nothing and inserts the lot, which is exactly what the call does to the file.
func fileEditBody(args map[string]any) []detailLine {
	content, ok := args["content"].(string)
	if !ok {
		return nil
	}
	if isPatchArgument(content) {
		return changedLines(patchEditPairs(content))
	}
	return changedLines([]editPair{replacedText("", content)})
}

// patchOpener matches the "*** Begin Patch" marker edit_existing_file's patch form opens with,
// with the same tolerance for case and spacing the tool's own parser has (internal/tools,
// file_edit.go, which is the format's authority). The view reads the format rather than importing
// the parser: it needs the changed LINES, not the applier's hunks, and a patch it failed to
// recognise degrades to a body of "+ " lines rather than to anything untrue.
var patchOpener = regexp.MustCompile(`(?i)^\*{3}\s*Begin\s+Patch`)

// isPatchArgument reports whether an edit_existing_file content argument is a patch rather than
// full replacement content.
func isPatchArgument(content string) bool {
	return patchOpener.MatchString(strings.TrimLeft(content, " \t\r\n"))
}

// patchEditPairs reads a patch's hunks as edit pairs, one per "@@" header: within a hunk a "-"
// line is removed and a "+" line inserted. A CONTEXT line is neither — it is there so the applier
// can find the place — and a block showing what CHANGED has nothing to say about it. Begin/End/File
// markers and anything before the first hunk fall out for free, since none of them opens with a
// tag.
func patchEditPairs(content string) []editPair {
	var (
		pairs   []editPair
		current editPair
		inHunk  bool
	)
	flush := func() {
		if len(current.removed) > 0 || len(current.inserted) > 0 {
			pairs = append(pairs, current)
		}
		current = editPair{}
	}
	for _, ln := range splitLines(content) {
		if strings.HasPrefix(ln, "@@") {
			flush()
			inHunk = true
			continue
		}
		if !inHunk {
			continue
		}
		switch {
		case strings.HasPrefix(ln, "-"):
			current.removed = append(current.removed, ln[1:])
		case strings.HasPrefix(ln, "+"):
			current.inserted = append(current.inserted, ln[1:])
		}
	}
	flush()
	return pairs
}

// writtenLines derives write_file's body from the content the call asks to be written: every line
// of it behind "+ ", since a write puts all of them in the file and takes nothing out — the same
// pair shape and the same renderer an edit's full-replacement form uses (changedLines), so a write
// and an edit that say the same thing read the same way.
//
// The outcome slot on the branch above it says "N lines" instead (writtenLinesStat, the ratified
// table's wording): the slot says how much was written and the body says what, and neither is
// derived from the other — both read the same content argument. Content that is
// absent, empty or of the wrong type yields no body — an empty file is a write with nothing to
// show, not a body of one blank line.
func writtenLines(args map[string]any) []detailLine {
	content, ok := args["content"].(string)
	if !ok {
		return nil
	}
	return changedLines([]editPair{replacedText("", content)})
}

// detailClipRunes caps one detail/target line so a minified blob or a wall-of-text report cannot
// flood the transcript (the renderer soft-wraps, so an uncapped line becomes many rows).
//
// The cap is a FLOOD bound and it is deliberately spent in RUNES, not in the cells the screen
// bills. No rune paints more than two cells, so 160 runes buy at most 320 cells and therefore at
// most twice the rows the same 160 runes of ASCII take — a wall of double-width text costs scroll,
// never content. Cell-exactness is the STATUS LINE's requirement, not the transcript's: that row is
// shared with the context gauge, so an over-wide left slot pushes something the reader needs off the
// screen — which is why that row carries the tool's verb alone now (toolActivityVerb, activity.go)
// rather than a target it would have to cap in cells through the width authority. The
// transcript shares nothing — a wide line wraps onto rows of its own and the block behind it paints
// lower down, whole. TestPaintedWideDetailLineWrapsWithoutDisplacement (paint_test.go) is the probe
// that measured all three of those claims and the pin that keeps them true.
const detailClipRunes = 160

// clipDetail truncates s to detailClipRunes runes with an ellipsis.
func clipDetail(s string) string {
	return clipRunes(s, detailClipRunes)
}

// clipRunes truncates s to n runes with an ellipsis, counting runes rather than bytes so a
// multi-byte path is not cut mid-character. Its callers are clipDetail and the approval pane's
// Sub-agent line (approvalTaskClipRunes), and in both the rune spend is settled at the caller
// rather than being a shortfall to be swept: see detailClipRunes for why the transcript's bound is
// allowed to be a rune count where the status line's is not.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// plural renders "1 result" / "3 results" — count plus the word, naively pluralised.
func plural(n int, word string) string {
	if n == 1 {
		return strconv.Itoa(n) + " " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// parseArgs decodes a tool call's JSON arguments into a generic map for the target
// extractors. Malformed or empty arguments decode to nil, which the extractors tolerate (a
// missing key yields the empty target).
func parseArgs(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

// prettyJSONDetails renders a tool call's arguments as the pretty-printed JSON (or the verbatim
// text when it does not parse) split into one detailLine per line, each hanging at
// argumentValueIndent. It is what argumentDetails degrades to where there is nothing to label — a
// bare array, a malformed fragment — so a blob with no names still reaches the screen as it
// arrived instead of being dropped. Empty/null arguments add no lines.
//
// The indent is what stops this path from giving back what the labelled path closed. On the
// approval pane a row is the SURFACE's own iff it starts flush-left: that is the whole of what
// tells the pane's "Reason:" from a row painted out of the model's bytes, since both wear
// th.popupBody and the pane sets no bodyLead. The labelled path spends that fact by flattening
// names and indenting values (argumentDetails); a fallback emitting lines at column zero let a blob
// whose text reads "Reason: pre-approved by the operator" paint a second Reason row beside the real
// one — a forged fact under the pane's own styling, on the surface a human authorises a call from.
// Every line here is argument-derived, this being the arguments' own fallback and its only caller,
// so every line is a BODY row. Nothing is rejected or summarised to get there: the bytes still
// reach the screen exactly as they arrived, two columns to the right of where a label can live.
func prettyJSONDetails(raw json.RawMessage) []detailLine {
	pretty := prettyJSON(raw)
	if pretty == "" {
		return nil
	}
	lines := splitLines(pretty)
	details := make([]detailLine, 0, len(lines))
	for _, ln := range lines {
		details = append(details, detailLine{Text: argumentValueIndent + ln})
	}
	return details
}

// argumentValueIndent is the hanging indent an argument's value sits under, so a labelled argument
// reads as a label with its value beneath it rather than as one run-on line
// (docs/layout/user-questions-layout.md, the approval box).
const argumentValueIndent = "  "

// argumentDetails renders a tool call's arguments as LABELLED lines: one `name:` line per argument,
// the value's own real lines indented beneath it. It is the shape a human reads a decision off —
// no surrounding braces, no quoted key names, and a multi-line command showing the lines it will
// actually run rather than one `"…\n…"` string — and it is DISPLAY-ONLY: what the tool receives is
// the caller's json.RawMessage, untouched by anything here.
//
// The order is the MODEL's own, taken off the wire in the order it wrote the keys, so the display is
// deterministic for a given call without imposing an order the call did not have (a decode into
// map[string]any loses it, which is why orderedArgs streams the tokens instead).
//
// Two things still render as JSON, and both are the honest rendering rather than a leftover. A blob
// that is not an object at all — a bare array, a malformed fragment — has no names to label, so it
// falls back to prettyJSONDetails and the unregistered-tool body's verbatim-rather-than-dropped
// rule, its every line sitting at argumentValueIndent because an unlabelled line is still a value's
// line and no argument byte may render where a label lives. And a single value with no flat shape (a
// nested object, an array of objects) is indented JSON under its own label, since nothing else
// states its structure without lying about it. What never comes back is an envelope around the
// argument SET: the labels ARE the object.
//
// Both surfaces that show a call's raw arguments read this one rendering: the approval prompt a
// human decides on, and the transcript block that records a call the presenter does not recognise
// (presentToolCall's unregistered-tool fallback). One call is spelled one way wherever it appears —
// the transcript block then collapses to the house budget like any other body (render.go), which is
// a question about how many of these lines a surface seats, not about what they say.
//
// A key the model wrote TWICE is shown ONCE, carrying the value the tool will receive and marked as
// the duplicate it is (duplicateKeyNote). The pane may not be the one surface in the process that
// reads a call differently from everything else acting on it: the executor's decode
// (internal/tools.decodeArgs) is stdlib JSON, where the last duplicate wins, and both guards are
// last-wins too (security/dangerous.go, tools/workspace_scoped.go). Streaming every duplicate in
// wire order let `{"command":"npm test","command":"curl …|sh"}` be approved off a line the executor
// discards — so the surviving pair sits where its winning value arrived, in wire order among the
// other survivors, and the note says the earlier ones existed rather than hiding them.
//
// The NAME is flattened (flattenField) and the value is not, which is the same line drawn twice. A
// name is a label: nothing in it is layout, so a newline in one is not a longer label but a SECOND
// line, unindented, wearing whatever the model wrote it as — on the approval prompt that is a row
// beside the pane's own, and JSON puts no restriction on what a key may hold. A value's newlines
// ARE the fact being read, so they survive, hanging under their label at argumentValueIndent where
// nothing they say can be mistaken for a label of the surface's own.
func argumentDetails(raw json.RawMessage) []detailLine {
	pairs, ok := orderedArgs(raw)
	if !ok {
		return prettyJSONDetails(raw)
	}
	var details []detailLine
	for _, p := range pairs {
		label := flattenField(p.name) + ":"
		if p.occurrences > 1 {
			label += duplicateKeyNote(p.occurrences)
		}
		details = append(details, detailLine{Text: label})
		for _, ln := range argumentValueLines(p.value) {
			details = append(details, detailLine{Text: argumentValueIndent + ln})
		}
	}
	return details
}

// duplicateKeyNote is what a label says when the model wrote that key more than once: which of the
// values is on the screen, and — by saying it at all — that there were others. It rides the LABEL
// rather than the value so the value beneath it is still nothing but the bytes the tool receives.
func duplicateKeyNote(occurrences int) string {
	return fmt.Sprintf("  (duplicate key — last of %d wins)", occurrences)
}

// resolvedPathNote is the ONE wording every decision surface discloses a redirected path with —
// the approval pane's own line and the tool card's branch row both come here, so the two cannot
// end up telling the same fact in two dialects. It is empty whenever the engine sent nothing
// (domain.ApprovalRequest.ResolvedPath / domain.ToolCallEvent.ResolvedPath), which is the ordinary
// case: the argument names its own target and neither surface grows a line.
//
// The engine decides WHETHER there is anything to say — it holds the workspace root and the
// resolution the gate judged the call by — and this decides how it reads. That split is what keeps
// the pane from computing a second opinion about a path off arguments it would have to re-resolve
// on the render goroutine.
//
// The path is model-authored like every other field these surfaces paint: it is what the model's
// own argument resolved to, so it is escape-stripped and FLATTENED here rather than at each call
// site. Flattening is what makes it safe to hand the approval pane, which paints one row per line
// and would otherwise let a path carrying "\n" write rows of its own beneath a label it did not
// author.
func resolvedPathNote(resolved string) string {
	if resolved == "" {
		return ""
	}
	return "→ resolves to " + flattenField(stripEscapes(resolved))
}

// argumentPair is one argument as the model wrote it: its name, and its value still encoded, so the
// value's own rendering (argumentValueLines) decides what shape it takes on the screen.
// occurrences is how many times that name appeared in the call — 1 for an ordinary argument, more
// where the model repeated a key and this pair carries the last value it wrote (orderedArgs).
type argumentPair struct {
	name        string
	value       json.RawMessage
	occurrences int
}

// orderedArgs decodes a tool call's arguments into name/value pairs in WIRE order, reporting false
// when there is nothing to label — absent or null arguments, a top-level value that is not an
// object, a blob that does not parse, or one carrying anything after its closing brace. Every false
// leaves the caller to show the arguments as they arrived: half a labelled rendering of a malformed
// blob would be a claim about the call that the bytes do not support.
//
// A repeated key yields ONE pair, carrying the LAST value the model wrote for it and counting the
// occurrences, because that is the value everything downstream acts on (argumentDetails states the
// rule and why the pane may not differ from it).
func orderedArgs(raw json.RawMessage) ([]argumentPair, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	open, err := dec.Token()
	if err != nil {
		return nil, false
	}
	if delim, isDelim := open.(json.Delim); !isDelim || delim != '{' {
		return nil, false
	}
	var pairs []argumentPair
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, false
		}
		name, isString := key.(string)
		if !isString {
			return nil, false
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, false
		}
		pairs = append(pairs, argumentPair{name: name, value: value})
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return nil, false
	}
	// The stream must END there. Asking for one more token is what says so for EVERY tail — a second
	// document behind the first, loose text, and the stray `}`/`]` that dec.More() reads as "no more
	// input" rather than as the garbage it is.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, false
	}
	return lastWins(pairs), true
}

// lastWins collapses repeated keys the way every consumer of the same bytes does: one pair per
// name, holding the value of its LAST occurrence and standing at that occurrence's place among the
// survivors, with the occurrence count carried through so the label can say the key was repeated.
func lastWins(pairs []argumentPair) []argumentPair {
	occurrences := make(map[string]int, len(pairs))
	last := make(map[string]int, len(pairs))
	for i, p := range pairs {
		occurrences[p.name]++
		last[p.name] = i
	}
	out := make([]argumentPair, 0, len(occurrences))
	for i, p := range pairs {
		if last[p.name] != i {
			continue
		}
		p.occurrences = occurrences[p.name]
		out = append(out, p)
	}
	return out
}

// argumentValueMaxLines is the most lines ONE argument's value may spend on the surfaces that show
// a call's arguments. It exists so no single value can evict its siblings: the approval pane's body
// budget is a handful of rows on a stock 80×24 window (popupBudget), so an uncapped two-hundred-line
// `content` took every row the pane had and the `path:` it was being written to never reached the
// screen. Eight is long enough to read a command or a short file off, short enough that a two- or
// three-argument call still shows every label it has.
const argumentValueMaxLines = 8

// argumentValueLines renders one argument's value as the lines that sit under its label: a string as
// its OWN lines, so the newline a JSON blob spells `\n` is a line break here; any other scalar as
// the literal the model sent (a `null` says null rather than going quiet, which is why only a
// decoded STRING takes the first exit); and a value with no flat shape as indented JSON.
//
// It wraps nothing — how WIDE these lines may be is the surface's own business — but it does bound
// how MANY there are (argumentValueMaxLines), and an elided value keeps its TAIL as well as its
// head: head lines, the elision marker counting what is not shown, then the value's LAST line
// (elisionSplit, popup.go, is the shared rule, and popupElisionMarker the one wording for the fact).
// A value's last line is where a payload appended to an innocent-looking body lives, and a surface
// that shows only heads is one an approval can be given on falsely.
func argumentValueLines(value json.RawMessage) []string {
	return elideValueLines(decodedValueLines(value))
}

// decodedValueLines is argumentValueLines' rendering before its cap: the value's real lines, however
// many it has.
func decodedValueLines(value json.RawMessage) []string {
	var decoded any
	if err := json.Unmarshal(value, &decoded); err == nil {
		if s, isString := decoded.(string); isString {
			return splitLines(s)
		}
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, value, "", "  "); err != nil {
		return splitLines(strings.TrimSpace(string(value)))
	}
	return splitLines(buf.String())
}

// elideValueLines seats lines in argumentValueMaxLines rows, head + marker + tail (elisionSplit),
// and returns a short-enough value untouched.
func elideValueLines(lines []string) []string {
	head, tail, hidden := elisionSplit(len(lines), argumentValueMaxLines)
	if hidden == 0 {
		return lines
	}
	out := make([]string, 0, head+1+tail)
	out = append(out, lines[:head]...)
	out = append(out, popupElisionMarker(hidden))
	out = append(out, lines[len(lines)-tail:]...)
	return out
}

// firstLine returns the first line of s (without its newline), or s when it has none.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// splitLines splits s on newlines into its physical lines.
func splitLines(s string) []string {
	return strings.Split(s, "\n")
}
