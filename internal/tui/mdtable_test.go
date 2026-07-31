package tui

import (
	"reflect"
	"testing"
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
