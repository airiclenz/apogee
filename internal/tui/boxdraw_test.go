package tui

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/scheme"
)

// joinTestModel is the bare Model the frame joiners read: the theme in measure, the window width,
// the viewport width the bar hangs at, and a seal memo when memo is set.
func joinTestModel(t *testing.T, method ansi.Method, width, viewportWidth int, memo bool) Model {
	t.Helper()
	m := Model{th: newTheme(scheme.Default()), width: width}
	m.th.measure = widthAuthority{method: method}
	m.viewport.SetWidth(viewportWidth)
	if memo {
		m.seals = newSealMemo()
	}
	return m
}

// referenceFrame is the frame as the joiners composed it before they handed widths on: every
// joiner squared every line it was handed by measuring it, and joinFrame measured them all again.
func referenceFrame(m Model, body, bar string, blocks []string, left int, column []string) string {
	measure := m.th.measure
	rows := append([]string{body}, blocks...)
	if bar != "" {
		bodyRows, barRows := strings.Split(body, "\n"), strings.Split(bar, "\n")
		if n := len(barRows); n > len(bodyRows) {
			bodyRows = append(bodyRows, make([]string, n-len(bodyRows))...)
		}
		for i, row := range bodyRows {
			row = squareLine(measure, row, m.viewport.Width())
			if i < len(barRows) {
				row += barRows[i]
			}
			bodyRows[i] = row
		}
		rows = append([]string{strings.Join(bodyRows, "\n")}, blocks...)
	}
	if column != nil {
		var lines []string
		for _, block := range rows {
			lines = append(lines, strings.Split(block, "\n")...)
		}
		for i, ln := range lines {
			ln = squareLine(measure, ln, left)
			if i < len(column) {
				ln += column[i]
			}
			lines[i] = ln
		}
		rows = []string{strings.Join(lines, "\n")}
	}
	var out []string
	for _, block := range rows {
		for _, ln := range strings.Split(block, "\n") {
			out = append(out, sealRowEdges(measure, squareLine(measure, ln, m.width), m.width))
		}
	}
	return strings.Join(out, "\n")
}

// TestJoinFrameSkipsSquaredRows: the frame composed from the widths the joiners hand on is the frame
// composed by measuring every line at every join, byte for byte — over rows that paint differently
// in the two measures (VS16), wide glyphs a cut straddles, rows that end on a non-ASCII cell exactly
// at the width (where the seam is measured, not summed), and painted rows the frame seals.
func TestJoinFrameSkipsSquaredRows(t *testing.T) {
	t.Parallel()

	const w = 10 // the viewport's width; the bar hangs in column w
	field := lipgloss.NewStyle().Background(lipgloss.Color("#202020"))
	body := strings.Join([]string{
		"short",
		"exactly10!",
		"over the width by far",
		"warn " + vs16Warning + " x",
		"abcdefghi漢",            // the wide glyph straddles column w: the cut stops a cell short
		"abcdef漢字",              // exactly w, ending on a wide cell
		"abcdefghi؀",            // a Prepend code point closing the row: the seam is measured
		"\x1b[1mbold\x1b[m",     // SGR around text
		field.Render("painted"), // a background the frame seals
		"",
	}, "\n")
	th := newTheme(scheme.Default())
	bars := map[string]string{
		"track and thumb":    strings.Join(scrollbarCells(th, 3, 4, 20, 10), "\n"),
		"bar shorter":        strings.Join(scrollbarCells(th, 0, 4, 20, 6), "\n"),
		"bar taller":         strings.Join(scrollbarCells(th, 0, 4, 20, 13), "\n"),
		"blank gutter":       strings.Repeat(" \n", 9) + " ",
		"no bar":             "",
		"bar of wide glyphs": strings.Repeat("漢\n", 9) + "漢",
	}
	blocks := []string{"", field.Render("status line " + vs16Warning), "input box\n" + field.Render("footer")}
	columns := map[string][]string{
		"no column":     nil,
		"panel column":  {"│ " + vs16Warning + " a", field.Render("│ files"), "│ 漢字", "│"},
		"column longer": strings.Split(strings.Repeat("│ row\n", 20), "\n"),
	}

	for _, method := range []ansi.Method{ansi.WcWidth, ansi.GraphemeWidth} {
		for _, frameWidth := range []int{w + 1, w + 4, w - 2} {
			for barName, bar := range bars {
				for columnName, column := range columns {
					name := fmt.Sprintf("method %d, width %d, %s, %s", method, frameWidth, barName, columnName)
					t.Run(name, func(t *testing.T) {
						t.Parallel()

						m := joinTestModel(t, method, frameWidth, w, true)
						want := referenceFrame(m, body, bar, blocks, w+1, column)

						var head squaredRows
						rows := append([]string(nil), blocks...)
						if bar != "" {
							head = m.joinScrollbar(body, bar)
						} else {
							rows = append([]string{body}, rows...)
						}
						if column != nil {
							head, rows = joinColumn(m.th.measure, head, rows, w+1, column), nil
						}
						for i, row := range head.rows {
							if got := m.th.measure.Width(row); got != head.widths[i] {
								t.Errorf("head row %d %q carries width %d, paints %d", i, row, head.widths[i], got)
							}
						}
						if got := m.joinFrame(head, rows); got != want {
							t.Errorf("frame differs from the measured-at-every-join frame\n got: %q\nwant: %q", got, want)
						}
					})
				}
			}
		}
	}
}

// TestScrollbarGlyphsRenderedOnce: a bar's cells are each rendered once per bar, not once per line —
// every thumb line holds the one thumb render and every track line the one track render — and every
// cell is the render it stands for.
func TestScrollbarGlyphsRenderedOnce(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	thumb, track := th.scrollThumb.Render(glyphScrollThumb), th.scrollTrack.Render(glyphScrollTrack)
	cells := scrollbarCells(th, 60, 100, 400, 200)
	pos, size := scrollbarThumb(60, 100, 400, 200)
	renders := map[*byte]bool{}
	for i, cell := range cells {
		want := track
		if i >= pos && i < pos+size {
			want = thumb
		}
		if cell != want {
			t.Errorf("cell %d = %q, want %q", i, cell, want)
		}
		renders[unsafe.StringData(cell)] = true
	}
	if len(renders) != 2 {
		t.Errorf("a 200-line bar holds %d distinct renders, want 2 (the thumb and the track)", len(renders))
	}
}

// TestSealMemoHoldsOnlyTheLastFrame: frame after frame with a different status line each time, the
// seal memo holds one slot per row it sealed in the last frame and nothing from the frames before;
// a shorter frame drops the slots past its last row; and every frame is the one sealed cold.
func TestSealMemoHoldsOnlyTheLastFrame(t *testing.T) {
	t.Parallel()

	field := lipgloss.NewStyle().Background(lipgloss.Color("#202020"))
	m := joinTestModel(t, ansi.WcWidth, 30, 29, true)
	cold := joinTestModel(t, ansi.WcWidth, 30, 29, false)
	frame := func(i int) []string {
		return []string{"transcript row", field.Render(fmt.Sprintf("status %d", i)), field.Render("footer")}
	}
	for i := range 200 {
		blocks := frame(i)
		if got, want := m.joinFrame(squaredRows{}, blocks), cold.joinFrame(squaredRows{}, blocks); got != want {
			t.Fatalf("frame %d: memo frame %q, cold frame %q", i, got, want)
		}
	}
	if got := sealMemoEntries(m.seals); got != 2 {
		t.Errorf("after 200 status lines the memo holds %d sealed rows, want 2 (status, footer)", got)
	}
	if got := len(m.seals.rows); got > 3 {
		t.Errorf("the memo holds %d slots for a 3-row frame", got)
	}
	if got, want := m.joinFrame(squaredRows{}, frame(199)), cold.joinFrame(squaredRows{}, frame(199)); got != want {
		t.Errorf("a repeated frame: memo %q, cold %q", got, want)
	}

	m.joinFrame(squaredRows{}, []string{field.Render("only")})
	if got := len(m.seals.rows); got > 1 {
		t.Errorf("a 1-row frame left %d slots", got)
	}
	if got := sealMemoEntries(m.seals); got != 1 {
		t.Errorf("a 1-row painted frame left %d sealed rows, want 1", got)
	}
}

// sealMemoEntries counts the slots of s holding a sealed row.
func sealMemoEntries(s *sealMemo) int {
	n := 0
	for _, slot := range s.rows {
		if slot.isSet {
			n++
		}
	}
	return n
}
