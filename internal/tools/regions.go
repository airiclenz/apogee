package tools

import (
	"slices"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// editRegionContext is how many unchanged lines a region carries each side of its change.
// Three is the ratified figure (ADR 0052): enough to place a change in its function, few
// enough that a body of several regions still fits a tool card.
//
// It also decides where two neighbouring changes stop reading as one. Their context ranges TILE
// the unchanged lines between them without overlapping: the earlier region takes up to three of
// those lines as its Trailing, the later one takes what is left over as its Leading. A gap of at
// most 2*editRegionContext lines is therefore covered end to end and the two regions come out
// ADJACENT in line numbering, which is what a renderer reads to decide whether an elision
// separator belongs between them — no separator when the next region starts where this one
// ended. A wider gap leaves lines uncovered, and the separator says so.
const editRegionContext = 3

// editRegions returns the changed regions of an edit that turned oldText into newText: the
// domain.EditRegions summary the four writing tools attach at apply time (ADR 0052). It is the ONE
// builder all of them call — a per-tool variant would let two edit blocks disagree about what a
// region is while painting into the same body — and it derives everything from the diff
// OPERATIONS, never from a rendered diff, the same counted-not-reparsed rule unifiedLineDiff's
// diffstat follows.
//
// One region per run of consecutive changed lines, its neighbours tiled as editRegionContext
// describes. Removed and Inserted hold CHANGED lines only — no unchanged line is ever folded
// into them to bridge a gap — which is what keeps domain.EditRegions.Stat() equal to
// unifiedLineDiff's diffstat over the same pair, line for line.
//
// The zero value comes back for the two cases the renderer treats alike, because both mean "no
// regions to paint" and both fall back to the argument-derived list: identical inputs, and a pair
// whose LCS table would exceed maxDiffTableCells. The budget is measured before either side is
// split, exactly as unifiedLineDiff measures it, so an over-budget pair allocates nothing
// proportional to the table it refuses.
func editRegions(oldText, newText string) domain.EditRegions {
	if oldText == newText {
		return domain.EditRegions{}
	}
	oldCount, newCount := lineCount(oldText), lineCount(newText)
	if int64(oldCount)*int64(newCount) > maxDiffTableCells {
		return domain.EditRegions{}
	}

	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")
	return domain.EditRegions{Regions: cutRegions(diffLines(oldLines, newLines))}
}

// okEditRegions returns an edit tool's success result: the prose sentence the model reads, with
// the Edit regions of the change just applied attached beside it as the structured half a host
// paints the Split diff from (ADR 0052). All four writing tools return through here — write_file
// only where it read an original, an absent before side going through okInsertedRegion instead —
// so the one question they share — does a summary ride along at all? — is answered in one place.
//
// A pair editRegions declines to cut, because the two texts are identical or because the pair is
// over the diff budget, attaches NO summary rather than an empty one. Absent is the signal the
// renderer reads to keep the argument-derived list (ratified call 9); a present EditRegions
// holding no region would instead claim the edit changed nothing.
func okEditRegions(callID, content, oldText, newText string) domain.ToolResult {
	regions := editRegions(oldText, newText)
	if len(regions.Regions) == 0 {
		return okResult(callID, content)
	}
	return okSummary(callID, content, regions)
}

// okInsertedRegion returns a write's success result for a target with NO readable before side:
// the whole content as one region of pure insertion, starting at line 1 of both files. write_file
// is its caller, on the ordinary create and on the rare original it could not read (write_file.go).
//
// It exists because an absent file is ZERO before lines, and okEditRegions cannot say that: an
// empty before text splits to [""], one empty line, which records a phantom removed blank line and
// reads a blank line in the content as unchanged context. Empty content changes nothing to show and
// attaches NO summary — the same prose floor okEditRegions falls back to for a pair it declines.
func okInsertedRegion(callID, content, newText string) domain.ToolResult {
	inserted := insertedLines(newText)
	if len(inserted) == 0 {
		return okResult(callID, content)
	}
	region := domain.EditRegion{BeforeStart: 1, AfterStart: 1, Inserted: inserted}
	return okSummary(callID, content, domain.EditRegions{Regions: []domain.EditRegion{region}})
}

// insertedLines cuts text into the lines a pure insertion inserts. A single trailing newline
// terminates the last line rather than opening an empty one, so the count matches what editRegions
// reports for the same content against a non-empty original — there the trailing empty split
// element pairs off as context on both sides instead of being inserted.
func insertedLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// cutRegions walks a line diff's operations in order and cuts them into regions, in file order.
// It returns nil when the operations carry no change at all.
func cutRegions(ops []diffOp) []domain.EditRegion {
	cutter := regionCutter{beforeLine: 1, afterLine: 1}
	for _, op := range ops {
		cutter.take(op)
	}
	return cutter.finish()
}

// regionCutter is the state of one walk over a diff's operations. It holds the line the walk has
// reached in each file, the region being built, and the unchanged lines seen since that region's
// last changed line — lines whose role is not yet decided, because how many of them arrive before
// the next change does is what says whether they are all this region's trailing context or only
// the first three of them are, with the rest falling to the next region's leading context.
type regionCutter struct {
	regions []domain.EditRegion

	// open is the region under construction, nil between regions.
	open *domain.EditRegion

	// beforeLine and afterLine are the 1-based lines the next operation of each kind sits on.
	beforeLine int
	afterLine  int

	unchanged []string
}

// take folds one operation into the walk, advancing the line counter of each file the operation
// consumes a line from: an unchanged line advances both, a removal only the before file, an
// insertion only the after file.
func (c *regionCutter) take(op diffOp) {
	if op.tag == tagContext {
		c.unchanged = append(c.unchanged, op.line)
		c.beforeLine++
		c.afterLine++
		return
	}

	c.reachChange()
	if op.tag == tagRemoved {
		c.open.Removed = append(c.open.Removed, op.line)
		c.beforeLine++
		return
	}
	c.open.Inserted = append(c.open.Inserted, op.line)
	c.afterLine++
}

// reachChange readies an open region for a changed line. A change that follows the previous one
// with no unchanged line between them continues the same region; any unchanged line at all ends
// that region and opens the next, the two sharing out the lines between them.
func (c *regionCutter) reachChange() {
	switch {
	case c.open == nil:
		c.begin()
	case len(c.unchanged) > 0:
		c.end()
		c.begin()
	}
}

// begin opens a region at the position the walk has reached, backing its start lines up over the
// leading context it takes — whatever the previous region left behind, or the run before the
// first change. Fewer than editRegionContext lines are available at the head of a file, and the
// start lines then stop where the file does.
func (c *regionCutter) begin() {
	leading := lastLines(c.unchanged, editRegionContext)
	c.open = &domain.EditRegion{
		BeforeStart: c.beforeLine - len(leading),
		AfterStart:  c.afterLine - len(leading),
		Leading:     leading,
	}
	c.unchanged = nil
}

// end closes the open region with the trailing context it takes from the unchanged lines that
// follow it, and files it. It CONSUMES the lines it takes: what remains is what a following
// region may claim as its leading context, so no line is ever context for two regions at once.
func (c *regionCutter) end() {
	trailing := firstLines(c.unchanged, editRegionContext)
	c.open.Trailing = trailing
	c.regions = append(c.regions, *c.open)
	c.open = nil
	c.unchanged = c.unchanged[len(trailing):]
}

// finish ends the walk, closing a region left open by the last operation, and returns the regions.
func (c *regionCutter) finish() []domain.EditRegion {
	if c.open != nil {
		c.end()
	}
	return c.regions
}

// firstLines returns a copy of the first count lines, or of all of them when there are fewer.
// The copy is the point: the region that keeps the lines must not share an array with the walk's
// own buffer, where a later append would reach them.
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
