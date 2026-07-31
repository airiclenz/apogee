package tui

import (
	"reflect"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
)

// ----------------------------------------------------------------------------
// Markdown tables: detection and parsing
// ----------------------------------------------------------------------------
//
// These assertions are pure data — cells, alignments and block spans — because that is all the
// parsing half produces; the rendered shape (widths, gutters, the rule row) is asserted against
// visible text elsewhere. Every case here is a question the walk asks once per line of every
// streamed message, so the "not a table" cases matter as much as the happy ones.

func TestTableSplitRow(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"pipes on both ends", "| a | b |", []string{"a", "b"}},
		{"no outer pipes", "a | b", []string{"a", "b"}},
		{"leading pipe only", "| a | b", []string{"a", "b"}},
		{"trailing pipe only", "a | b |", []string{"a", "b"}},
		{"single column", "| a |", []string{"a"}},
		{"empty interior cell kept", "| a |  | c |", []string{"a", "", "c"}},
		{"source padding trimmed", "|  a    |   b |", []string{"a", "b"}},
		{"row indented", "   | a | b |   ", []string{"a", "b"}},
		{"escaped pipe is cell content", `| a \| b | c |`, []string{"a | b", "c"}},
		{"escaped pipe at row end", `| a | b \|`, []string{"a", "b |"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := splitTableRow(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("splitTableRow(%q) = %#v; want %#v", tc.in, got, tc.want)
			}
		})
	}
}

func TestTableDelimiterAlignment(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []mdAlign
	}{
		{"plain is left", "| --- |", []mdAlign{mdAlignLeft}},
		{"leading colon is left", "| :-- |", []mdAlign{mdAlignLeft}},
		{"trailing colon is right", "| --: |", []mdAlign{mdAlignRight}},
		{"both colons are centred", "| :-: |", []mdAlign{mdAlignCenter}},
		{"all four forms in one row", "|---|:--|--:|:-:|", []mdAlign{mdAlignLeft, mdAlignLeft, mdAlignRight, mdAlignCenter}},
		{"long hyphen runs", "| :--------: | ---- |", []mdAlign{mdAlignCenter, mdAlignLeft}},
		{"no outer pipes", "--- | --:", []mdAlign{mdAlignLeft, mdAlignRight}},
		{"single hyphen cell", "| - |", []mdAlign{mdAlignLeft}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseDelimiterRow(tc.in)
			if !ok {
				t.Fatalf("parseDelimiterRow(%q) rejected the row; want it accepted", tc.in)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseDelimiterRow(%q) = %#v; want %#v", tc.in, got, tc.want)
			}
		})
	}
}

// A row that is not built purely from hyphen cells is not a delimiter row — the two-line lookahead
// leans on this to keep ordinary prose out of the table path.
func TestTableDelimiterRejected(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"| |",
		"| : |",
		"| :: |",
		"| --- | abc |",
		"| -x- |",
		"| - - |",
		"| --- | |",
		"| ---: :--- |",
		"a | b",
	} {
		if got, ok := parseDelimiterRow(in); ok {
			t.Errorf("parseDelimiterRow(%q) = %#v, accepted; want it rejected", in, got)
		}
	}
}

func TestTableMatchBlock(t *testing.T) {
	lines := []string{
		"| Tool | Calls |",
		"|:--|--:|",
		"| Read File | 12 |",
		"| Run | 3 |",
	}

	table, span, ok := matchTableBlock(lines, 0)

	if !ok {
		t.Fatalf("matchTableBlock did not detect the table")
	}
	if span != 4 {
		t.Errorf("span = %d; want 4 (header + delimiter + two body rows)", span)
	}
	if want := []string{"Tool", "Calls"}; !reflect.DeepEqual(table.header, want) {
		t.Errorf("header = %#v; want %#v", table.header, want)
	}
	if want := []mdAlign{mdAlignLeft, mdAlignRight}; !reflect.DeepEqual(table.align, want) {
		t.Errorf("align = %#v; want %#v", table.align, want)
	}
	if want := [][]string{{"Read File", "12"}, {"Run", "3"}}; !reflect.DeepEqual(table.rows, want) {
		t.Errorf("rows = %#v; want %#v", table.rows, want)
	}
}

// A table need not start at line 0: the walk offers every line in turn.
func TestTableMatchBlockAtOffset(t *testing.T) {
	lines := []string{"intro", "", "| a |", "| - |", "| x |", "", "tail"}

	table, span, ok := matchTableBlock(lines, 2)

	if !ok {
		t.Fatalf("matchTableBlock(lines, 2) did not detect the table")
	}
	if span != 3 {
		t.Errorf("span = %d; want 3 (the blank line closes the block)", span)
	}
	if want := []string{"a"}; !reflect.DeepEqual(table.header, want) {
		t.Errorf("header = %#v; want %#v (single column)", table.header, want)
	}
	if want := [][]string{{"x"}}; !reflect.DeepEqual(table.rows, want) {
		t.Errorf("rows = %#v; want %#v", table.rows, want)
	}
}

// The header sets the column count: a short row is padded with empty cells, a long one loses its
// excess, and neither reshapes the table.
func TestTableRowCellCountFitted(t *testing.T) {
	lines := []string{
		"| a | b | c |",
		"| - | - | - |",
		"| short |",
		"| 1 | 2 | 3 | 4 | 5 |",
	}

	table, span, ok := matchTableBlock(lines, 0)

	if !ok {
		t.Fatalf("matchTableBlock did not detect the table")
	}
	if span != 4 {
		t.Errorf("span = %d; want 4", span)
	}
	want := [][]string{{"short", "", ""}, {"1", "2", "3"}}
	if !reflect.DeepEqual(table.rows, want) {
		t.Errorf("rows = %#v; want %#v (padded short, dropped excess)", table.rows, want)
	}
}

// The block ends at the first blank line and at the first line carrying no pipe; what follows is
// left for the walk to render as its own block.
func TestTableBlockTerminates(t *testing.T) {
	cases := []struct {
		name     string
		lines    []string
		wantSpan int
		wantRows int
	}{
		{"blank line", []string{"| a |", "| - |", "| x |", "", "| y |"}, 3, 1},
		{"pipe-free line", []string{"| a |", "| - |", "| x |", "after", "| y |"}, 3, 1},
		{"heading", []string{"| a |", "| - |", "# Title"}, 2, 0},
		{"end of input", []string{"| a |", "| - |", "| x |"}, 3, 1},
		{"no body rows", []string{"| a |", "| - |"}, 2, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			table, span, ok := matchTableBlock(tc.lines, 0)
			if !ok {
				t.Fatalf("matchTableBlock(%#v) did not detect the table", tc.lines)
			}
			if span != tc.wantSpan {
				t.Errorf("span = %d; want %d", span, tc.wantSpan)
			}
			if len(table.rows) != tc.wantRows {
				t.Errorf("rows = %#v; want %d row(s)", table.rows, tc.wantRows)
			}
		})
	}
}

// The cases that must keep today's paragraph behaviour: no delimiter, a mismatched delimiter, a
// delimiter with nothing above it, and a header still waiting for the rest of its table to stream
// in.
func TestTableNotATable(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
	}{
		{"delimiter cell count differs", []string{"| a | b |", "| --- |", "| 1 | 2 |"}},
		{"more delimiter cells than header", []string{"| a |", "| --- | --- |"}},
		{"header with no delimiter under it", []string{"| a | b |", "plain text"}},
		{"header is the last line (still streaming)", []string{"| a | b |"}},
		{"delimiter-shaped line standing alone", []string{"--- | ---", "plain text"}},
		{"delimiter row with no header above it", []string{"| --- | --- |", "| a | b |"}},
		{"no pipe in the header row", []string{"a b", "| --- |"}},
		{"escaped pipes only", []string{`a \| b`, "| --- |"}},
		{"blank header row", []string{"", "| --- |"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if table, span, ok := matchTableBlock(tc.lines, 0); ok {
				t.Errorf("matchTableBlock(%#v) = %#v, span %d, detected; want no table", tc.lines, table, span)
			}
		})
	}
}

// Out-of-range starts are asked for by no caller today, but the walk indexes the slice it is
// given: matchTableBlock answers "no table" rather than panicking.
func TestTableMatchBlockOutOfRange(t *testing.T) {
	lines := []string{"| a |", "| - |"}
	for _, start := range []int{-1, 1, 2, 7} {
		if _, _, ok := matchTableBlock(lines, start); ok && start != 0 {
			t.Errorf("matchTableBlock(lines, %d) detected a table; want none", start)
		}
	}
}

// ----------------------------------------------------------------------------
// Markdown tables: layout and rendering
// ----------------------------------------------------------------------------
//
// These assertions are written against the visible text (strip) and the visible width
// (lipgloss.Width) like markdown_test.go's, because a column position is exactly what the reader
// sees: the styling may or may not be emitted, but the cells must land in the same columns
// either way.

// The worked example in layout.md's "Markdown tables in assistant text", rendered at the width
// that section states — the spec of record, asserted line for line. The comparison drops each
// line's trailing blanks because the doc's fenced block cannot show them: a row IS padded out to
// the table's width (TestTableRowsShareOneWidth pins that), and those blanks are invisible in
// print. Everything a reader can see in the example is pinned here exactly.
func TestTableRendersLayoutExample(t *testing.T) {
	th := newTheme()
	source := strings.Join([]string{
		"| Tool | Calls | Notes |",
		"|:--|--:|:-:|",
		"| Read File | 12 | fast |",
		"| Run | 3 | `go test ./...` |",
	}, "\n")
	want := []string{
		"Tool       Calls      Notes",
		"───────────────────────────────",
		"Read File     12      fast",
		"Run            3  go test ./...",
	}

	got := visibleTrimmed(renderMarkdownBody(th, source, 34))

	if !reflect.DeepEqual(got, want) {
		t.Errorf("table render mismatch:\n--- got ---\n%s\n--- want ---\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Every line of a table is the same display width — header, rule and every body row — so the
// block presents one straight right edge to whatever sits beside it. It is the invariant the
// transcript's right-hand chrome is measured against: the scroll-bar gutter beside a short row
// would read as the bar stepping inward, and the mouse addresses columns the row no longer holds
// (mouse.go). The regression it guards is a body row whose last cell is narrower than its column —
// here the "Status" header is two cells wider than every value under it, which is exactly the
// two-column step the shape reported against the first table release.
func TestTableRowsShareOneWidth(t *testing.T) {
	th := newTheme()
	source := strings.Join([]string{
		"| File | Description of the change that was made | Status |",
		"| --- | --- | --- |",
		"| internal/tui/markdown.go | dispatch the table matcher before lists | done |",
		"| internal/tui/mdtable.go | the parser and the renderer both live here | done |",
		"| layout.md | spec the block | fail |",
	}, "\n")
	const width = 76 // narrower than the table's natural width, so it is fitted to exactly this

	got := renderMarkdownBody(th, source, width)

	if len(got) != 5 {
		t.Fatalf("got %d lines, want 5 (header, rule, three rows): %#v", len(got), visible(got))
	}
	for i, ln := range got {
		if w := lipgloss.Width(ln); w != width {
			t.Errorf("line %d is %d cells wide, want %d (every table line ends in the same column): %q",
				i, w, width, strip(ln))
		}
	}
}

// The table's own syntax is consumed: no pipe and no delimiter hyphens survive into the rendered
// block, and the header is styled where the profile emits colour.
func TestTableConsumesItsSyntax(t *testing.T) {
	th := newTheme()
	source := "| a | b | c |\n| --- | --- | --- |\n| 1 | 2 | 3 |"

	got := renderMarkdownBody(th, source, 40)

	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3 (header, rule, one row): %#v", len(got), visible(got))
	}
	for i, ln := range got {
		v := strip(ln)
		if strings.Contains(v, "|") {
			t.Errorf("line %d still shows a pipe: %q", i, v)
		}
		if strings.Contains(v, "---") {
			t.Errorf("line %d still shows the delimiter row: %q", i, v)
		}
	}
	if v := strip(got[1]); v != "───────" {
		t.Errorf("rule row = %q; want %q (one unbroken run, gutters ruled too)", v, "───────")
	}
	if colorActive(th) && !strings.Contains(got[0], "\x1b") {
		t.Errorf("header row emitted no styling: %q", got[0])
	}
	if colorActive(th) && !strings.Contains(got[1], "\x1b") {
		t.Errorf("rule row emitted no styling: %q", got[1])
	}
}

// The rule under the header is ONE unbroken line: the gutters between the columns are ruled like
// the columns themselves, so it reads as a single horizontal rule under the whole table rather than
// a dash per column interrupted at every column division. A space anywhere inside that line is the
// bare-gutter regression. The run is also exactly as wide as every other line of the block — the
// gutters are now filled rather than blank, so the width the rule spans is the width it draws.
func TestTableRuleIsContinuous(t *testing.T) {
	th := newTheme()
	source := strings.Join([]string{
		"| Tool | Calls | Notes |",
		"|:--|--:|:-:|",
		"| Read File | 12 | fast |",
		"| Run | 3 | `go test ./...` |",
	}, "\n")

	got := renderMarkdownBody(th, source, 40) // wider than the table, so no column is shrunk

	if len(got) != 4 {
		t.Fatalf("got %d lines, want 4 (header, rule, two rows): %#v", len(got), visible(got))
	}
	rule := strip(got[1])
	if strings.Contains(rule, " ") {
		t.Errorf("rule row = %q; want one unbroken run — the gutters are ruled too, not bare", rule)
	}
	if want := strings.Repeat(glyphTableRule, lipgloss.Width(rule)); rule != want {
		t.Errorf("rule row = %q; want nothing but %q", rule, glyphTableRule)
	}
	for i, ln := range got {
		if w := lipgloss.Width(ln); w != lipgloss.Width(got[1]) {
			t.Errorf("line %d is %d cells wide but the rule is %d — the rule spans the table exactly: %q",
				i, w, lipgloss.Width(got[1]), strip(ln))
		}
	}
}

// Each column is padded on the side its delimiter cell names, header cells included.
func TestTableAlignsColumns(t *testing.T) {
	th := newTheme()
	source := strings.Join([]string{
		"| left | right | mid |",
		"| :--- | ----: | :-: |",
		"| x | y | z |",
	}, "\n")

	got := visible(renderMarkdownBody(th, source, 40))

	if len(got) != 3 {
		t.Fatalf("got %#v; want three lines", got)
	}
	// Columns are 4, 5 and 3 cells wide, so the row is "x   " + "  " + "    y" + "  " + " z " —
	// the centred cell's own trailing space included, since a row is padded out to the table.
	if want := "x         y   z "; got[2] != want {
		t.Errorf("row = %q; want %q (left, right, centred)", got[2], want)
	}
	if want := "left  right  mid"; got[0] != want {
		t.Errorf("header = %q; want %q (the header aligns with its column too)", got[0], want)
	}
}

// A centred cell with an odd remainder takes the extra space on its right (layout.md).
func TestTableCentreOddRemainder(t *testing.T) {
	th := newTheme()
	source := "| head |\n| :-: |\n| ab |"

	got := visible(renderMarkdownBody(th, source, 20))

	if want := " ab "; got[2] != want {
		t.Errorf("centred row = %q; want %q (one space left, the odd one right)", got[2], want)
	}
}

// Inline markup inside a cell styles as it does in a paragraph, and it is the rendered width that
// sets the column: the ** and ` markers must not push the column open.
func TestTableInlineMarkupInCells(t *testing.T) {
	th := newTheme()
	source := strings.Join([]string{
		"| name | note |",
		"| --- | --- |",
		"| **bold** | plain |",
		"| `code` | x |",
	}, "\n")

	got := renderMarkdownBody(th, source, 40)
	text := visible(got)

	for i, v := range text {
		if strings.Contains(v, "**") || strings.Contains(v, "`") {
			t.Errorf("line %d still carries inline markers: %q", i, v)
		}
	}
	// "bold" and "code" are four cells wide once rendered, the same as "name": the column stays
	// four wide and "note" starts in the same place on every row.
	for i, v := range text {
		if i == 1 {
			continue // the rule row has no gutter text to line up
		}
		if len(v) > 4 && !strings.HasPrefix(v[4:], "  ") {
			t.Errorf("line %d = %q; want the second column to start at column 6", i, v)
		}
	}
	if colorActive(th) && !strings.Contains(got[3], "\x1b") {
		t.Errorf("a `code` cell emitted no styling: %q", got[3])
	}
}

// The width cap is absolute: an over-wide table shrinks its widest column and cuts the cells that
// no longer fit with a … tail, and no rendered line exceeds the width.
func TestTableTruncatesToWidth(t *testing.T) {
	th := newTheme()
	source := strings.Join([]string{
		"| id | description |",
		"| --- | --- |",
		"| 1 | " + strings.Repeat("very long ", 6) + "|",
	}, "\n")
	const width = 24

	got := renderMarkdownBody(th, source, width)

	for _, ln := range got {
		if w := lipgloss.Width(ln); w > width {
			t.Errorf("line %q has visible width %d > %d", strip(ln), w, width)
		}
	}
	if v := strip(got[2]); !strings.HasSuffix(v, "…") {
		t.Errorf("over-wide cell = %q; want it cut with a … tail", v)
	}
	if len(got) != 3 {
		t.Errorf("got %d lines, want 3 — a row is one physical line however wide its cells", len(got))
	}
}

// A table shrinks as far as one cell per column before it gives up, and every line it draws stays
// inside the width all the way down.
func TestTableShrinksToTheWidthItIsGiven(t *testing.T) {
	th := newTheme()
	source := "| alpha | beta | gamma |\n| --- | --- | --- |\n| 1 | 2 | 3 |"

	for _, width := range []int{8, 9, 12, 20} {
		got := renderMarkdownBody(th, source, width)
		if len(got) != 3 {
			t.Errorf("width %d: got %d lines, want 3: %#v", width, len(got), visible(got))
		}
		for _, ln := range got {
			if w := lipgloss.Width(ln); w > width {
				t.Errorf("width %d: line %q has visible width %d", width, strip(ln), w)
			}
		}
	}
}

// Below that floor the block is not a table at all: it falls back to the plain paragraphs it
// rendered before tables existed, delimiter row and pipes visible — and those paragraphs stay
// inside the width like every other line the TUI draws. At these widths a row of one- and
// three-cell tokens no longer fits on one line, so the delimiter's dashes are matched across the
// breaks the wrapper makes rather than as one run (they are still all there, in order: wrapText
// caps the line without dropping anything — TestWrapTextHoldsTheWidthCap).
func TestTableUnfittableFallsBack(t *testing.T) {
	th := newTheme()
	source := "| alpha | beta | gamma |\n| --- | --- | --- |\n| 1 | 2 | 3 |"

	for _, width := range []int{1, 3, 6} {
		lines := visible(renderMarkdownBody(th, source, width))
		got := strings.Join(lines, "\n")
		if strings.Contains(got, "─") {
			t.Errorf("width %d: block drew a rule; want the plain-paragraph fallback:\n%s", width, got)
		}
		if !strings.Contains(strings.Join(lines, ""), "---") {
			t.Errorf("width %d: delimiter row was consumed; want it left as source text:\n%s", width, got)
		}
		for _, ln := range lines {
			if w := th.measure.Width(ln); w > width {
				t.Errorf("width %d: fallback line %q is %d cells wide, over the cap", width, ln, w)
			}
		}
	}
}

// While a table streams in, the header row that has no delimiter under it yet is an ordinary
// paragraph — the same contract every other half-typed construct keeps.
func TestTableStreamingDegradesToParagraphs(t *testing.T) {
	th := newTheme()
	const header = "| Tool | Calls |"

	got := renderMarkdownBody(th, header, 40)

	if len(got) != 1 || got[0] != header {
		t.Errorf("partial table = %#v; want the source line unchanged", visible(got))
	}
}

// A table ends where its block ends: whatever follows renders as its own block, and the table is
// not re-parsed line by line into it.
func TestTableFollowedByOtherBlocks(t *testing.T) {
	th := newTheme()
	source := strings.Join([]string{
		"| a | b |",
		"| --- | --- |",
		"| 1 | 2 |",
		"",
		"# Title",
		"- item",
		"```",
		"code()",
		"```",
	}, "\n")

	got := visible(renderMarkdownBody(th, source, 40))

	want := []string{"a  b", "────", "1  2", "", "Title", "• item", "  code()"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mixed blocks:\n--- got ---\n%s\n--- want ---\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A table that opens partway down a message leaves the prose above and below it alone.
func TestTableInsideProse(t *testing.T) {
	th := newTheme()
	source := "Here it is:\n| a | b |\n| - | - |\n| 1 | 2 |\ndone"

	got := visible(renderMarkdownBody(th, source, 40))

	want := []string{"Here it is:", "a  b", "────", "1  2", "done"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("prose around a table:\n--- got ---\n%s\n--- want ---\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// fitColumns takes space from the widest column, and from the leftmost of equally wide ones, so a
// table lays out identically on every repaint.
func TestTableFitColumns(t *testing.T) {
	cases := []struct {
		name   string
		widths []int
		budget int
		want   []int
		ok     bool
	}{
		{"already fits", []int{3, 4}, 10, []int{3, 4}, true},
		{"widest gives way first", []int{2, 9}, 8, []int{2, 6}, true},
		{"equal columns: the leftmost gives way first", []int{5, 5, 3}, 10, []int{3, 4, 3}, true},
		{"levels down to the runner-up before touching it", []int{9, 4, 2}, 10, []int{4, 4, 2}, true},
		{"all the way to one cell each", []int{9, 9}, 2, []int{1, 1}, true},
		{"one cell each still overflows", []int{9, 9}, 1, nil, false},
		{"no budget at all", []int{4}, 0, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			widths := append([]int(nil), tc.widths...)
			ok := fitColumns(widths, tc.budget)
			if ok != tc.ok {
				t.Fatalf("fitColumns(%v, %d) = %v; want %v", tc.widths, tc.budget, ok, tc.ok)
			}
			if !ok {
				return
			}
			if !reflect.DeepEqual(widths, tc.want) {
				t.Errorf("fitColumns(%v, %d) = %v; want %v", tc.widths, tc.budget, widths, tc.want)
			}
			total := len(widths) - 1
			for _, w := range widths {
				total += w
				if w < 1 {
					t.Errorf("column shrunk to %d; want a floor of one cell", w)
				}
			}
		})
	}
}
