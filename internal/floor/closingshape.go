package floor

import (
	"regexp"
	"slices"
	"strings"
)

// ClosingShape is what ClosingShapeOf judged a delegate's closing text to be. ShapeReport is the
// ordinary case — a report, which internal/agent forwards under its closing-report sub-head beneath
// the plain cap head. The other four are the non-report shapes: each value is the phrase the
// variant cap head's `reads as %s` slot carries (internal/agent's stepCapNonReportFormat and
// siblings), so the head is worded by shape rather than by one blanket "not a report". The
// classifier lives here, beside HasToolCallMarkup, so internal/agent's capped path and the TUI read
// one judgment of the same text.
type ClosingShape string

// The five closing shapes: the report, then the four non-report shapes in the order ClosingShapeOf
// tests for them.
const (
	ShapeReport         ClosingShape = ""
	ShapeToolCallMarkup ClosingShape = "tool-call markup"
	ShapeFileDump       ClosingShape = "a file dump"
	ShapeGrepDump       ClosingShape = "a grep dump"
	ShapeNarration      ClosingShape = "narration of its next step"
)

// IsNonReport reports whether the shape is one of the four non-report shapes.
func (s ClosingShape) IsNonReport() bool { return s != ShapeReport }

// The line shapes ClosingShapeOf reads, each pinned by a session-mining fixture (2026-09-18,
// session 20260918T143011Z-9788d447; plan 2026-09-18 - 00, item 3).
var (
	// readFileHeaderLine is the header read_file opens every result with —
	// `[File: <path>, <n> lines total, showing lines <a>-<b>]` (internal/tools/read_file.go) — the
	// line a capped delegate pastes when its closing reply is the file it last read rather than a
	// report on it. Read over the first nonReportHeadLines non-blank lines only: a report that
	// merely CITES a path never opens with this header, and one that quotes it deep in its body is
	// still a report.
	readFileHeaderLine = regexp.MustCompile(`^\[File: .+, \d+ lines total`)
	// grepHitLine is a grep hit — `path:line:` — the line shape a pasted search dump is made of.
	// Only a MAJORITY of such lines makes the text a dump: a report cites `path:line` freely, and
	// three hits quoted in a longer report are evidence, not the reply.
	grepHitLine = regexp.MustCompile(`^[\w./-]+:\d+:`)
	// intentAtSentenceStart is a stated next step — "Let me …", "I'll …", "Now I …" — at ANY
	// sentence start of a line, mid-line included: a closing reply that ENDS on one is a delegate
	// narrating what it would do next, not reporting what it did.
	intentAtSentenceStart = regexp.MustCompile(`(?i)(^|[.!?]\s+)(let me|i'll|i will|now i|next i|next, i)\b`)
	// receiptLine is a structured receipt field — `PHASE: `, `STATUS: `, `OUT: `, `SUMMARY: `,
	// `COUNTS: `, `FLAG: ` — the shape a skill's report format prescribes. A trailing intent AFTER a
	// receipt is a report that closes with what remains, so the intent rule yields to it.
	receiptLine = regexp.MustCompile(`^[A-Z][A-Z_-]*: `)
)

// nonReportHeadLines is how many leading non-blank lines readFileHeaderLine is read over: a pasted
// file may follow one line of lead-in ("Now claims 17-20:" was the fixture), not a whole report.
const nonReportHeadLines = 3

// ClosingShapeOf judges a delegate's closing text: ShapeReport for blank text — the wordless path
// is the caller's (internal/agent's stepCapNoTextMarker), never a non-report — and for anything
// that reads as a report; otherwise the first non-report shape that fires, in this order: unparsed
// tool-call markup (HasToolCallMarkup), a read_file header among the first nonReportHeadLines
// non-blank lines, grep hits as the majority of non-blank lines, and a trailing stated intent with
// no receipt line before it. No shape FAULTS the text: the capped result stays non-error and
// forwards it whole — internal/agent's closed acknowledgement list stays the only wording rule
// that withholds one, and a one-line report opening "I'll note …" is demoted to narration, never
// dropped.
func ClosingShapeOf(text string) ClosingShape {
	lines := nonBlankLines(text)
	if len(lines) == 0 {
		return ShapeReport
	}
	switch {
	case HasToolCallMarkup(text):
		return ShapeToolCallMarkup
	case hasReadFileHeader(lines):
		return ShapeFileDump
	case isGrepMajority(lines):
		return ShapeGrepDump
	case endsOnIntent(lines):
		return ShapeNarration
	}
	return ShapeReport
}

// IsNonReport reports whether ClosingShapeOf judges text a non-report; false for blank text.
func IsNonReport(text string) bool {
	return ClosingShapeOf(text).IsNonReport()
}

// nonBlankLines splits text into its non-blank lines, each trimmed of surrounding space.
func nonBlankLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// hasReadFileHeader reports whether any of the first nonReportHeadLines lines is a read_file header.
func hasReadFileHeader(lines []string) bool {
	head := lines[:min(len(lines), nonReportHeadLines)]
	return slices.ContainsFunc(head, readFileHeaderLine.MatchString)
}

// isGrepMajority reports whether more than half of lines are grep hits.
func isGrepMajority(lines []string) bool {
	hits := 0
	for _, line := range lines {
		if grepHitLine.MatchString(line) {
			hits++
		}
	}
	return hits*2 > len(lines)
}

// endsOnIntent reports whether the last line carries a stated intent and no receipt line precedes it.
func endsOnIntent(lines []string) bool {
	last := lines[len(lines)-1]
	if !intentAtSentenceStart.MatchString(last) {
		return false
	}
	return !slices.ContainsFunc(lines[:len(lines)-1], receiptLine.MatchString)
}
