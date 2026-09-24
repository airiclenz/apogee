package floor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// closingShapeFixture reads one of the session-mining fixtures under testdata/closingshape: the
// closing texts capped delegates actually handed their parents on 2026-09-18 (session
// 20260918T143011Z-9788d447, transcript entries 277, 442, 580, 892 and 1106), copied verbatim
// rather than paraphrased, because the shapes they carry — a header on the SECOND line, an intent
// MID-line — are exactly what a one-line quote would have flattened away. internal/agent keeps its
// own copy for the capped-path test that drives [580] end to end.
func closingShapeFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "closingshape", name+".txt"))
	if err != nil {
		t.Fatalf("closing-shape fixture %s: %v", name, err)
	}
	return string(data)
}

// TestClosingShapeOf_ReadsTheFourNonReportShapes is the table over ClosingShapeOf (plan 2026-09-18 -
// 00, item 3): each non-report shape on the fixture that motivated it and on the report it must NOT
// fire for, and blank text as a report — the wordless path is internal/agent's stepCapNoTextMarker,
// never a non-report. The intent case pairs [580]'s trailing "Let me …" against the same line
// appended to [277]'s receipt block: the receipt exempts it. A DelegateReportBlock-shaped report —
// found, changed, unfinished, with path:line references mid-line — is the ordinary report.
func TestClosingShapeOf_ReadsTheFourNonReportShapes(t *testing.T) {
	t.Parallel()

	const reportCitingAPath = "The cap head is written once (internal/agent/subagent.go:128).\n" +
		"Its shape is read by the TUI at internal/tui/toolregistry.go:844.\n" +
		"Nothing was changed.\n" +
		"The read_file header `[File: x.go, 3 lines total, showing lines 1-3]` is what the TUI hides.\n" +
		"Unfinished: the token-bound head is unchecked."
	const reportQuotingThreeHits = "Found three callers of capResultHead:\n" +
		"internal/agent/subagent.go:852:\t\tContent: a.capResultHead()\n" +
		"internal/agent/subagent_test.go:1490:\tif want := cappedResult(\n" +
		"internal/agent/seat_test.go:176:\t\t{\"step capped\"\n" +
		"All three read the step-cap head.\n" +
		"Nothing was changed.\n" +
		"Unfinished: the TUI recogniser was not re-read."
	const delegateReportShaped = "Found: the cap head is written in capResultHead (internal/agent/subagent.go:128); the TUI reads it at internal/tui/toolregistry.go:844.\n" +
		"Changed: nothing.\n" +
		"Unfinished: the token-bound head was not checked."
	const oneLineIntent = "I'll note that the survey is unfinished."
	intentLine := strings.TrimSpace(closingShapeFixture(t, "580-trailing-intent"))
	intentLine = intentLine[strings.LastIndex(intentLine, "\n")+1:]

	cases := []struct {
		name string
		text string
		want ClosingShape
	}{
		{"blank", "", ShapeReport},
		{"whitespace only", " \n\t\n", ShapeReport},
		{"tool-call markup", `<tool_call>{"name": "shell", "arguments": {"cmd": "ls"}}</tool_call>`, ShapeToolCallMarkup},
		{"[892] read_file header on the first line", closingShapeFixture(t, "892-file-dump"), ShapeFileDump},
		{"[442] read_file header on the second line", closingShapeFixture(t, "442-file-dump-after-lead-in"), ShapeFileDump},
		{"a report that cites a path and quotes the header deep in its body", reportCitingAPath, ShapeReport},
		{"[1106] grep hits as the majority of lines", closingShapeFixture(t, "1106-grep-dump"), ShapeGrepDump},
		{"three grep hits in a longer report", reportQuotingThreeHits, ShapeReport},
		{"[580] a mid-line trailing intent", closingShapeFixture(t, "580-trailing-intent"), ShapeNarration},
		{"[277] a receipt block", closingShapeFixture(t, "277-receipt-report"), ShapeReport},
		{"[277] a receipt block followed by [580]'s intent line", closingShapeFixture(t, "277-receipt-report") + "\n\n" + intentLine, ShapeReport},
		{"a one-line report opening on an intent", oneLineIntent, ShapeNarration},
		{"a DelegateReportBlock-shaped report", delegateReportShaped, ShapeReport},
		{"the scripted closing report", "I read two files; the third is unread and the survey is unfinished.", ShapeReport},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := ClosingShapeOf(tc.text); got != tc.want {
				t.Errorf("ClosingShapeOf = %q, want %q", got, tc.want)
			}
			if got := IsNonReport(tc.text); got != tc.want.IsNonReport() {
				t.Errorf("IsNonReport = %v, want %v", got, tc.want.IsNonReport())
			}
		})
	}
}
