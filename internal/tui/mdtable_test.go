package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/airiclenz/apogee/internal/scheme"
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
	th := newTheme(scheme.Default())
	source := strings.Join([]string{
		"| Tool | Calls | Notes |",
		"|:--|--:|:-:|",
		"| Read File | 12 | fast |",
		"| Run | 3 | `go test ./...` |",
	}, "\n")
	want := []string{
		"Tool      │ Calls │     Notes",
		"──────────┼───────┼──────────────",
		"Read File │    12 │     fast",
		"──────────┼───────┼──────────────",
		"Run       │     3 │ go test ./...",
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
// two-column step the shape reported against the first table release. At this width the middle
// column is shrunk by two cells and the second body row wraps, so the block is one line taller than
// its row count — the filler line under the shorter cells beside it is held to the width too, as are
// the two rules between the three rows.
func TestTableRowsShareOneWidth(t *testing.T) {
	th := newTheme(scheme.Default())
	source := strings.Join([]string{
		"| File | Description of the change that was made | Status |",
		"| --- | --- | --- |",
		"| internal/tui/markdown.go | dispatch the table matcher before lists | done |",
		"| internal/tui/mdtable.go | the parser and the renderer both live here | done |",
		"| layout.md | spec the block | fail |",
	}, "\n")
	const width = 76 // narrower than the table's natural width, so it is fitted to exactly this

	got := renderMarkdownBody(th, source, width)

	if len(got) != 8 {
		t.Fatalf("got %d lines, want 8 (header, rule, three rows — one wrapped to two — and a rule between each pair): %#v",
			len(got), visible(got))
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
	th := newTheme(scheme.Default())
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
	if v, want := strip(got[1]), "──┼───┼──"; v != want {
		t.Errorf("rule row = %q; want %q (ruled through the dividers, crossing each one)", v, want)
	}
	if colorActive(th) && !strings.Contains(got[0], "\x1b") {
		t.Errorf("header row emitted no styling: %q", got[0])
	}
	if colorActive(th) && !strings.Contains(got[1], "\x1b") {
		t.Errorf("rule row emitted no styling: %q", got[1])
	}
}

// A cell is rendered through renderInline like any other text, so the inline subset works inside a
// table for free: an <u>…</u> cell has its tags consumed and its text underlined.
func TestTableCellUnderline(t *testing.T) {
	th := newTheme(scheme.Default())
	source := "| a | b |\n| --- | --- |\n| <u>x</u> | y |"

	got := renderMarkdownBody(th, source, 40)

	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3 (header, rule, one row): %#v", len(got), visible(got))
	}
	if v, want := strip(got[2]), "x │ y"; v != want {
		t.Errorf("body row = %q; want %q (the <u> tags consumed, the column no wider for them)", v, want)
	}
	if sgr := underlineSGR(th); sgr != "" && !strings.Contains(got[2], sgr) {
		t.Errorf("underlined cell emitted no underline SGR %q: %q", sgr, got[2])
	}
}

// EVERY rule the table draws — the one under the header and the one between each pair of adjacent
// body rows — runs the whole width of the table: it is ruled THROUGH the dividers rather than
// stopping at each one, so it reads as a single horizontal rule across the block rather than a dash
// per column interrupted at every column division. A space anywhere inside one of those lines is the
// bare-gutter regression. Where a rule meets a divider it crosses it with a ┼ in exactly the cell the
// other rows draw their │ in: a crossing one cell off would show as a kink in the vertical, and it
// has to hold for the inter-row rules as much as for the header's, since they are the same stroke
// continued down the block.
func TestTableRuleIsContinuous(t *testing.T) {
	th := newTheme(scheme.Default())
	source := strings.Join([]string{
		"| Tool | Calls | Notes |",
		"|:--|--:|:-:|",
		"| Read File | 12 | fast |",
		"| Run | 3 | `go test ./...` |",
	}, "\n")

	got := renderMarkdownBody(th, source, 40) // wider than the table, so no column is shrunk

	if len(got) != 5 {
		t.Fatalf("got %d lines, want 5 (header, rule, row, rule, row): %#v", len(got), visible(got))
	}
	if rules := tableRuleLines(visible(got)); !reflect.DeepEqual(rules, []int{1, 3}) {
		t.Fatalf("rules on lines %v; want the header's on line 1 and one between the two rows on line 3: %#v",
			rules, visible(got))
	}

	// The header's crossings are the reference every other line of the block is held to.
	crossings := glyphColumns(strip(got[1]), glyphTableCross)
	if len(crossings) != 2 {
		t.Errorf("rule row = %q; want a crossing at each of the two dividers, got %d", strip(got[1]), len(crossings))
	}
	for i, ln := range got {
		v := strip(ln)
		if i == 1 || i == 3 {
			if strings.Contains(v, " ") {
				t.Errorf("rule row %d = %q; want one unbroken run — the dividers are ruled through, not bare", i, v)
			}
			for _, r := range v {
				if s := string(r); s != glyphTableRule && s != glyphTableCross {
					t.Errorf("rule row %d = %q; want nothing but %q and %q, found %q",
						i, v, glyphTableRule, glyphTableCross, s)
					break
				}
			}
			if cols := glyphColumns(v, glyphTableCross); !reflect.DeepEqual(cols, crossings) {
				t.Errorf("rule row %d crosses in columns %v but the header's rule crosses at %v: %q",
					i, cols, crossings, v)
			}
		} else if cols := glyphColumns(v, glyphTableColumn); !reflect.DeepEqual(cols, crossings) {
			t.Errorf("line %d draws its dividers in columns %v but the rules cross at %v: %q",
				i, cols, crossings, v)
		}
		if w := lipgloss.Width(ln); w != lipgloss.Width(got[1]) {
			t.Errorf("line %d is %d cells wide but the rule is %d — the rule spans the table exactly: %q",
				i, w, lipgloss.Width(got[1]), v)
		}
	}
}

// tableRuleLines reports the indexes of the lines of an ANSI-stripped block that are horizontal
// rules — nothing but the rule glyph and its crossings — which is how a test tells "this line is a
// rule" apart from "this line is a row" without counting on where the rules fall.
func tableRuleLines(lines []string) []int {
	var out []int
	for i, v := range lines {
		if v == "" {
			continue
		}
		rule := true
		for _, r := range v {
			if s := string(r); s != glyphTableRule && s != glyphTableCross {
				rule = false
				break
			}
		}
		if rule {
			out = append(out, i)
		}
	}
	return out
}

// A rule sits between every pair of ADJACENT body rows, and nowhere else: not above the first body
// row, where the header's own rule already is, and not below the last, because the table is ruled
// and not boxed — it has no bottom frame to close. Three body rows therefore draw three rules in
// all, and the block's last line is a row rather than a stroke hanging under one.
func TestTableRulesBetweenBodyRows(t *testing.T) {
	th := newTheme(scheme.Default())
	source := strings.Join([]string{
		"| a | b |",
		"| --- | --- |",
		"| 1 | one |",
		"| 2 | two |",
		"| 3 | three |",
	}, "\n")

	got := visible(renderMarkdownBody(th, source, 40))
	want := []string{
		"a │ b    ",
		"──┼──────",
		"1 │ one  ",
		"──┼──────",
		"2 │ two  ",
		"──┼──────",
		"3 │ three",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ruled body rows:\n--- got ---\n%s\n--- want ---\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if rules := tableRuleLines(got); !reflect.DeepEqual(rules, []int{1, 3, 5}) {
		t.Errorf("rules on lines %v; want one under the header and one between each adjacent pair", rules)
	}
}

// One body row draws exactly one rule — the header's. A rule under the last row would be a bottom
// frame, which the block does not have however many rows it holds.
func TestTableLastRowHasNoRuleUnderIt(t *testing.T) {
	th := newTheme(scheme.Default())
	source := "| a | b |\n| --- | --- |\n| 1 | one |"

	got := visible(renderMarkdownBody(th, source, 40))

	if rules := tableRuleLines(got); !reflect.DeepEqual(rules, []int{1}) {
		t.Errorf("rules on lines %v of %#v; want only the header's", rules, got)
	}
}

// A wrapped row is still ONE row: the rule separates rows, never the lines a single row spills onto,
// so the reader can tell "more of the same row" from "the next row". Both rows here are two lines
// tall, which puts the only inter-row rule between the second line of the first row and the first
// line of the second.
func TestTableWrappedRowIsNotRuledInside(t *testing.T) {
	th := newTheme(scheme.Default())
	source := strings.Join([]string{
		"| short | long |",
		"| --- | --- |",
		"| ab | one two three four |",
		"| cd | five six seven eight |",
	}, "\n")

	got := visible(renderMarkdownBody(th, source, 20))
	want := []string{
		"short │ long        ",
		"──────┼─────────────",
		"ab    │ one two     ",
		"      │ three four  ",
		"──────┼─────────────",
		"cd    │ five six    ",
		"      │ seven eight ",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("wrapped rows:\n--- got ---\n%s\n--- want ---\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if rules := tableRuleLines(got); !reflect.DeepEqual(rules, []int{1, 4}) {
		t.Errorf("rules on lines %v; want the header's and one between the two rows — a wrapped row is not ruled inside",
			rules)
	}
}

// glyphColumns reports the display column of every occurrence of glyph in the ANSI-stripped line
// s — the positions a reader sees it in, which is what "the crossing sits under the divider" has
// to mean: a byte offset would answer a different question on any line carrying a wide cell.
func glyphColumns(s, glyph string) []int {
	var cols []int
	for i := 0; i < len(s); {
		j := strings.Index(s[i:], glyph)
		if j < 0 {
			break
		}
		cols = append(cols, lipgloss.Width(s[:i+j]))
		i += j + len(glyph)
	}
	return cols
}

// Each column is padded on the side its delimiter cell names, header cells included.
func TestTableAlignsColumns(t *testing.T) {
	th := newTheme(scheme.Default())
	source := strings.Join([]string{
		"| left | right | mid |",
		"| :--- | ----: | :-: |",
		"| x | y | z |",
	}, "\n")

	got := visible(renderMarkdownBody(th, source, 40))

	if len(got) != 3 {
		t.Fatalf("got %#v; want three lines", got)
	}
	// Columns are 4, 5 and 3 cells wide, so the row is "x   " + " │ " + "    y" + " │ " + " z " —
	// the centred cell's own trailing space included, since a row is padded out to the table.
	if want := "x    │     y │  z "; got[2] != want {
		t.Errorf("row = %q; want %q (left, right, centred)", got[2], want)
	}
	if want := "left │ right │ mid"; got[0] != want {
		t.Errorf("header = %q; want %q (the header aligns with its column too)", got[0], want)
	}
}

// A centred cell with an odd remainder takes the extra space on its right (layout.md).
func TestTableCentreOddRemainder(t *testing.T) {
	th := newTheme(scheme.Default())
	source := "| head |\n| :-: |\n| ab |"

	got := visible(renderMarkdownBody(th, source, 20))

	if want := " ab "; got[2] != want {
		t.Errorf("centred row = %q; want %q (one space left, the odd one right)", got[2], want)
	}
}

// Inline markup inside a cell styles as it does in a paragraph, and it is the rendered width that
// sets the column: the ** and ` markers must not push the column open.
func TestTableInlineMarkupInCells(t *testing.T) {
	th := newTheme(scheme.Default())
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
	// four wide and the divider — and so "note" behind it — lands in the same place on every row.
	rules := tableRuleLines(text)
	if !reflect.DeepEqual(rules, []int{1, 3}) {
		t.Fatalf("rules on lines %v; want the header's and one between the two rows: %#v", rules, text)
	}
	for i, v := range text {
		if slices.Contains(rules, i) {
			continue // a rule row crosses the divider rather than drawing it
		}
		if len(v) > 4 && !strings.HasPrefix(v[4:], tableDivider) {
			t.Errorf("line %d = %q; want the divider straight after the four-cell first column", i, v)
		}
	}
	if colorActive(th) && !strings.Contains(got[4], "\x1b") {
		t.Errorf("a `code` cell emitted no styling: %q", got[4])
	}
}

// The width cap is absolute and its endpoint is a wrap: an over-wide table still shrinks its widest
// column — the whole overflow coming off that one, the narrow column keeping its natural width — but
// the cell that no longer fits now wraps onto further lines inside its column instead of being cut
// with a … tail, and every word of it survives in order.
func TestTableWrapsToWidth(t *testing.T) {
	th := newTheme(scheme.Default())
	const cell = "very long very long very long very long very long very long"
	source := strings.Join([]string{
		"| id | description |",
		"| --- | --- |",
		"| 1 | " + cell + " |",
	}, "\n")
	const width = 24

	got := renderMarkdownBody(th, source, width)
	text := visible(got)

	for _, ln := range got {
		if w := lipgloss.Width(ln); w > width {
			t.Errorf("line %q has visible width %d > %d", strip(ln), w, width)
		}
	}
	if len(got) <= 3 {
		t.Fatalf("got %d lines, want the over-wide cell wrapped onto further lines: %#v", len(got), text)
	}
	// The "id" column is two cells wide naturally and keeps both: the shrink comes off the widest
	// column only, so the divider sits in the same display column on every line of the block.
	for i, v := range text {
		if strings.Contains(v, "…") {
			t.Errorf("line %d = %q; want a wrap — no cell is cut any more", i, v)
		}
		if i == 1 {
			continue // the rule row crosses the divider rather than drawing it
		}
		if !strings.HasPrefix(v[2:], tableDivider) {
			t.Errorf("line %d = %q; want the divider straight after the two-cell id column", i, v)
		}
	}
	var wrapped []string
	for _, v := range text[2:] {
		wrapped = append(wrapped, strings.TrimSpace(strings.SplitN(v, glyphTableColumn, 2)[1]))
	}
	if got, want := strings.Join(wrapped, " "), cell; got != want {
		t.Errorf("wrapped cell reads %q across its lines; want the whole of %q", got, want)
	}
}

// A table shrinks as far as the readable floor before it gives up, and every line it draws stays
// inside the width all the way down. Eighteen cells is the narrowest these three columns can be
// drawn in: minTableColumnWidth cells each, plus the three the divider between them costs twice.
// Below its natural width the block is still a table — the squeezed columns wrap rather than fall
// back — so its height grows with the wrapping instead of holding at one line per row.
func TestTableShrinksToTheWidthItIsGiven(t *testing.T) {
	th := newTheme(scheme.Default())
	source := "| alpha | beta | gamma |\n| --- | --- | --- |\n| 1 | 2 | 3 |"

	for _, width := range []int{18, 20, 24} {
		got := renderMarkdownBody(th, source, width)
		lines := visible(got)
		if len(got) < 3 {
			t.Errorf("width %d: got %d lines, want at least 3: %#v", width, len(got), lines)
		}
		if !strings.Contains(strings.Join(lines, "\n"), glyphTableRule) {
			t.Errorf("width %d: block drew no rule; want it still rendered as a table: %#v", width, lines)
		}
		for _, ln := range got {
			if w := lipgloss.Width(ln); w > width {
				t.Errorf("width %d: line %q has visible width %d", width, strip(ln), w)
			}
		}
	}
}

// The endpoint of an over-wide cell is a wrap, not a cut: it spills onto further lines inside its
// own column and every word of it is still on screen. This is the issue the wave closes — a cell
// cut with a … lost information the model had put in the table.
func TestTableWrapsInsteadOfTruncating(t *testing.T) {
	th := newTheme(scheme.Default())
	const note = "the quick brown fox jumps over the lazy dog"
	source := strings.Join([]string{
		"| id | note |",
		"| --- | --- |",
		"| 1 | " + note + " |",
	}, "\n")

	got := visible(renderMarkdownBody(th, source, 30))

	if len(got) < 4 {
		t.Fatalf("got %d lines, want the over-wide cell wrapped onto further lines: %#v", len(got), got)
	}
	block := strings.Join(got, "\n")
	if strings.Contains(block, "…") {
		t.Errorf("a cell was cut with a … tail; want it wrapped:\n%s", block)
	}
	for _, word := range strings.Fields(note) {
		if !strings.Contains(block, word) {
			t.Errorf("the wrap dropped %q:\n%s", word, block)
		}
	}
}

// A row is as many physical lines as its TALLEST cell needs, and the shorter cells beside it are
// blank-filled BELOW their content — cells are top-aligned, so a one-line cell reads on the row's
// first line rather than floating in its middle. The filler line is padded out like any other, so
// the block's right edge stays straight through it.
func TestTableRowHeightIsItsTallestCell(t *testing.T) {
	th := newTheme(scheme.Default())
	source := strings.Join([]string{
		"| short | long |",
		"| --- | --- |",
		"| ab | one two three four |",
	}, "\n")
	const width = 20

	got := renderMarkdownBody(th, source, width)
	want := []string{
		"short │ long        ",
		"──────┼─────────────",
		"ab    │ one two     ",
		"      │ three four  ",
	}

	if v := visible(got); !reflect.DeepEqual(v, want) {
		t.Errorf("ragged row:\n--- got ---\n%s\n--- want ---\n%s",
			strings.Join(v, "\n"), strings.Join(want, "\n"))
	}
	for i, ln := range got {
		if w := lipgloss.Width(ln); w != width {
			t.Errorf("line %d is %d cells wide, want %d — filler lines are padded too: %q",
				i, w, width, strip(ln))
		}
	}
}

// A right- or centre-aligned column aligns EVERY one of its wrapped lines, not merely the first:
// the padding is applied per line, so a continuation line that fell back to the left would show as
// a step in an otherwise straight column.
func TestTableWrappedLinesKeepAlignment(t *testing.T) {
	th := newTheme(scheme.Default())
	source := strings.Join([]string{
		"| right | mid |",
		"| ----: | :-: |",
		"| alpha beta gamma | one two three |",
	}, "\n")

	got := visibleTrimmed(renderMarkdownBody(th, source, 23))
	want := []string{
		"     right │    mid",
		"───────────┼───────────",
		"alpha beta │  one two",
		"     gamma │   three",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("wrapped alignment:\n--- got ---\n%s\n--- want ---\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Every line of the block is exactly the table's width in the width authority's measure — the
// header (which wraps here too), the rule, the body rows, and the continuation and filler lines a
// wrapped row adds. That straight right edge is what the transcript's right-hand chrome is laid
// out against (mouse.go, model.go).
func TestTableEveryLineIsTheTableWidth(t *testing.T) {
	th := newTheme(scheme.Default())
	source := strings.Join([]string{
		"| a header that is much too long for its column | b |",
		"| --- | ---: |",
		"| one two three four five six seven | 42 |",
		"| x | a value that does not fit either |",
	}, "\n")
	const width = 32

	got := renderMarkdownBody(th, source, width)

	if len(got) <= 5 {
		t.Fatalf("got %d lines, want a block taller than its four source rows: %#v", len(got), visible(got))
	}
	for i, ln := range got {
		if w := th.measure.Width(ln); w != width {
			t.Errorf("line %d is %d cells wide, want %d: %q", i, w, width, strip(ln))
		}
	}
}

// Inline style survives a wrap: a bold or code span broken across a column's line boundary re-emits
// its SGR run on the continuation line and resets at its end, so the second half of a **bold** cell
// is bold too rather than the style bleeding out of the table (wrapText, render.go).
func TestTableWrapKeepsInlineStyle(t *testing.T) {
	th := newTheme(scheme.Default())
	if !colorActive(th) {
		t.Skip("profile emits no colour, so there is no SGR run to carry across the break")
	}
	source := strings.Join([]string{
		"| id | note |",
		"| --- | --- |",
		"| 1 | **bold across the wrap boundary** |",
	}, "\n")

	got := renderMarkdownBody(th, source, 20)

	if len(got) < 4 {
		t.Fatalf("got %d lines, want the bold cell wrapped: %#v", len(got), visible(got))
	}
	for i, ln := range got[3:] {
		if !strings.Contains(ln, "\x1b") {
			t.Errorf("continuation line %d carries no SGR; the bold run was dropped at the break: %q",
				i+3, strip(ln))
		}
	}
	last := got[len(got)-1]
	if !strings.Contains(last, "\x1b[m") && !strings.Contains(last, "\x1b[0m") {
		t.Errorf("the last wrapped line never resets its style, so bold would bleed past it: %q", last)
	}
}

// Row height is unbounded: a cell of several hundred characters in a narrow column gets every line
// its content needs and nothing is dropped. A cap would only put the truncation back at a different
// threshold, which is the thing this wave removes.
func TestTableWrapIsUnbounded(t *testing.T) {
	th := newTheme(scheme.Default())
	const words = 120
	cell := strings.TrimSpace(strings.Repeat("word ", words))
	source := strings.Join([]string{
		"| id | text |",
		"| --- | --- |",
		"| 1 | " + cell + " |",
	}, "\n")
	const width = 14 // id keeps two cells, the divider three, the text column the other nine

	got := visible(renderMarkdownBody(th, source, width))

	var rebuilt []string
	for _, v := range got[2:] {
		rebuilt = append(rebuilt, strings.TrimSpace(strings.SplitN(v, glyphTableColumn, 2)[1]))
	}
	// Nine cells hold exactly "word word", so the row is half as tall as the cell has words — and
	// it is that many lines tall however far past a screenful that runs.
	if want := words / 2; len(rebuilt) != want {
		t.Errorf("got %d body lines for a %d-word cell; want %d, with nothing dropped",
			len(rebuilt), words, want)
	}
	if joined := strings.Join(rebuilt, " "); joined != cell {
		t.Errorf("wrapped cell reads %q; want the whole of the original", joined)
	}
}

// BenchmarkRenderTable pins the cost of the path a streamed token actually pays: a table whose
// cells all fit at a width that needs no shrinking, so every cell takes wrapTableCell's fast path.
// The markdown walk re-runs over the whole transcript on every token (model.go), so this render is
// on the per-keystroke path and its allocation count is the number that matters.
func BenchmarkRenderTable(b *testing.B) {
	th := newTheme(scheme.Default())
	tbl, _, ok := matchTableBlock([]string{
		"| Tool | Calls | Notes |",
		"|:--|--:|:-:|",
		"| Read File | 12 | fast |",
		"| Run | 3 | go test ./... |",
		"| Write File | 7 | ok |",
	}, 0)
	if !ok {
		b.Fatal("benchmark source is not a table")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, fits := renderTable(th, tbl, 60); !fits {
			b.Fatal("table did not fit at 60 columns")
		}
	}
}

// Below the readable floor the block is not a table at all: it falls back to the plain paragraphs
// it rendered before tables existed, delimiter row and pipes visible — and those paragraphs stay
// inside the width like every other line the TUI draws. At these widths a row of one- and
// three-cell tokens no longer fits on one line, so the delimiter's dashes are matched across the
// breaks the wrapper makes rather than as one run (they are still all there, in order: wrapText
// caps the line without dropping anything — TestWrapTextHoldsTheWidthCap).
func TestTableUnfittableFallsBack(t *testing.T) {
	th := newTheme(scheme.Default())
	source := "| alpha | beta | gamma |\n| --- | --- | --- |\n| 1 | 2 | 3 |"

	// Seventeen cells is the widest of these: three columns are not drawn until each has
	// minTableColumnWidth cells of content and both dividers between them are paid for, which is
	// eighteen — and at the narrow end there is not even room for the dividers themselves.
	for _, width := range []int{1, 3, 6, 8, 12, 17} {
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

// The floor is the fallback's threshold, asserted at the layout boundary itself: renderTable turns
// away every width that cannot give each of its three columns minTableColumnWidth cells once the
// two dividers are paid for, renderMarkdownBody draws the block as plain paragraphs there — source
// text visible, neither table glyph anywhere in it — and the very next cell up is a table again.
func TestTableNarrowerThanTheFloorFallsBack(t *testing.T) {
	th := newTheme(scheme.Default())
	lines := []string{"| alpha | beta | gamma |", "| --- | --- | --- |", "| 1 | 2 | 3 |"}
	tbl, _, ok := matchTableBlock(lines, 0)
	if !ok {
		t.Fatal("the fixture is not a table")
	}
	source := strings.Join(lines, "\n")
	const floorWidth = 3*minTableColumnWidth + 2*tableDividerWidth // 18: every column at the floor

	for width := 1; width < floorWidth; width++ {
		if _, fits := renderTable(th, tbl, width); fits {
			t.Errorf("width %d: fitted a table that cannot give every column %d cells", width, minTableColumnWidth)
		}
		got := visible(renderMarkdownBody(th, source, width))
		block := strings.Join(got, "\n")
		if strings.Contains(block, glyphTableColumn) || strings.Contains(block, glyphTableRule) {
			t.Errorf("width %d: the block drew table rules; want the plain-paragraph fallback:\n%s", width, block)
		}
		if !strings.Contains(strings.Join(got, ""), "---") {
			t.Errorf("width %d: the delimiter row was consumed; want it left as source text:\n%s", width, block)
		}
	}
	if _, fits := renderTable(th, tbl, floorWidth); !fits {
		t.Errorf("width %d pays every column its floor exactly; want the table drawn", floorWidth)
	}
}

// A column narrower than the floor by nature is never charged it: three columns of two cells each
// are drawn as a table at twelve cells, where charging one floor per column — 3×4 plus the two
// dividers, eighteen — would have thrown the whole block down to paragraphs for want of a width
// none of its columns would ever have used.
func TestTableOfNarrowColumnsIsNotRejected(t *testing.T) {
	th := newTheme(scheme.Default())
	source := "| id | ab | xy |\n| --- | --- | --- |\n| 1 | 2 | 3 |"
	const width = 3*2 + 2*tableDividerWidth // 12: the three natural widths and their dividers

	got := renderMarkdownBody(th, source, width)

	want := []string{"id │ ab │ xy", "───┼────┼───", "1  │ 2  │ 3 "}
	if !reflect.DeepEqual(visible(got), want) {
		t.Errorf("narrow-column table:\n--- got ---\n%s\n--- want ---\n%s",
			strings.Join(visible(got), "\n"), strings.Join(want, "\n"))
	}
	for i, ln := range got {
		if w := th.measure.Width(ln); w != width {
			t.Errorf("line %d is %d cells wide, want the table's %d: %q", i, w, width, strip(ln))
		}
	}
}

// While a table streams in, the header row that has no delimiter under it yet is an ordinary
// paragraph — the same contract every other half-typed construct keeps.
func TestTableStreamingDegradesToParagraphs(t *testing.T) {
	th := newTheme(scheme.Default())
	const header = "| Tool | Calls |"

	got := renderMarkdownBody(th, header, 40)

	if len(got) != 1 || got[0] != header {
		t.Errorf("partial table = %#v; want the source line unchanged", visible(got))
	}
}

// A table ends where its block ends: whatever follows renders as its own block, and the table is
// not re-parsed line by line into it.
func TestTableFollowedByOtherBlocks(t *testing.T) {
	th := newTheme(scheme.Default())
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

	want := []string{"a │ b", "──┼──", "1 │ 2", "", "Title", "• item", "  code()"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mixed blocks:\n--- got ---\n%s\n--- want ---\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A table that opens partway down a message leaves the prose above and below it alone.
func TestTableInsideProse(t *testing.T) {
	th := newTheme(scheme.Default())
	source := "Here it is:\n| a | b |\n| - | - |\n| 1 | 2 |\ndone"

	got := visible(renderMarkdownBody(th, source, 40))

	want := []string{"Here it is:", "a │ b", "──┼──", "1 │ 2", "done"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("prose around a table:\n--- got ---\n%s\n--- want ---\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// fitColumns takes space from the widest column, and from the leftmost of equally wide ones, so a
// table lays out identically on every repaint — and it stops at the floor it is given rather than
// shredding a column into one letter per line. The width a table REQUIRES is sum(min(natural,
// floor)), asserted here in both directions: a column narrower than the floor by nature is never
// widened to it and so is never charged it, which is what keeps a table of naturally narrow columns
// from being turned away over a floor none of its columns would ever occupy.
func TestTableFitColumns(t *testing.T) {
	cases := []struct {
		name   string
		widths []int
		budget int
		floor  int
		want   []int
		ok     bool
	}{
		{"already fits", []int{3, 4}, 10, 1, []int{3, 4}, true},
		{"widest gives way first", []int{2, 9}, 8, 1, []int{2, 6}, true},
		{"equal columns: the leftmost gives way first", []int{5, 5, 3}, 10, 1, []int{3, 4, 3}, true},
		{"levels down to the runner-up before touching it", []int{9, 4, 2}, 10, 1, []int{4, 4, 2}, true},
		{"all the way to one cell each", []int{9, 9}, 2, 1, []int{1, 1}, true},
		{"one cell each still overflows", []int{9, 9}, 1, 1, nil, false},
		{"no budget at all", []int{4}, 0, 1, nil, false},
		{"the floor stops the shrink", []int{9, 9}, 8, 4, []int{4, 4}, true},
		{"a column already at the floor gives nothing up", []int{4, 12}, 12, 4, []int{4, 8}, true},
		{"one cell under the floor turns the table away", []int{9, 9}, 7, 4, nil, false},
		{"naturally narrow columns are not charged the floor", []int{2, 2, 2}, 6, 4, []int{2, 2, 2}, true},
		{"a narrow column pays only its own width", []int{2, 12}, 6, 4, []int{2, 4}, true},
		{"and is never shrunk to pay for a wide one", []int{2, 12}, 5, 4, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			required := 0
			for _, w := range tc.widths {
				required += min(w, tc.floor)
			}

			widths := append([]int(nil), tc.widths...)
			ok := fitColumns(widths, tc.budget, tc.floor)
			if ok != tc.ok {
				t.Fatalf("fitColumns(%v, %d, floor %d) = %v; want %v", tc.widths, tc.budget, tc.floor, ok, tc.ok)
			}
			if !ok {
				if required <= tc.budget {
					t.Errorf("turned away though the required width %d fits the %d budget", required, tc.budget)
				}
				return
			}
			if required > tc.budget {
				t.Errorf("fitted though the required width %d is over the %d budget", required, tc.budget)
			}
			if !reflect.DeepEqual(widths, tc.want) {
				t.Errorf("fitColumns(%v, %d, floor %d) = %v; want %v", tc.widths, tc.budget, tc.floor, widths, tc.want)
			}
			total := 0
			for i, w := range widths {
				total += w
				if floor := min(tc.widths[i], tc.floor); w < floor {
					t.Errorf("column %d shrunk to %d; want no less than its floor of %d", i, w, floor)
				}
			}
			if total > tc.budget {
				t.Errorf("fitted widths %v sum to %d, over the %d budget", widths, total, tc.budget)
			}
		})
	}
}
