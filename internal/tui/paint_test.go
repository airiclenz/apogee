package tui

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Frame-level paint harness
// ----------------------------------------------------------------------------

// Every other test in this package asserts the string the Model BUILT (plain(m.View())), which
// is measured in GraphemeWidth throughout — the very measure under suspicion. This harness
// asserts the grid the terminal PAINTS instead, by replaying bubbletea's own draw path: the
// composed view string goes through uv.NewStyledString and Draw into a screen buffer whose
// width method the caller picks, exactly as cursedRenderer.flush does
// (bubbletea/v2@v2.0.7/cursed_renderer.go:268-311). The method is a parameter because that is
// the whole point: the painter defaults to ansi.WcWidth (ultraviolet/buffer.go:612-617) and
// only switches to ansi.GraphemeWidth when a terminal answers mode 2027 (tea.go:794-797), so a
// frame that is correct under one measure can be a column short under the other.

// vs16Warning is U+26A0 WARNING SIGN followed by U+FE0F VARIATION SELECTOR-16 — the grapheme the
// two measures disagree about. GraphemeWidth promotes any cluster carrying VS16 to two cells;
// WcWidth takes the first non-zero-width rune of the cluster, and U+26A0 is one cell wide.
const vs16Warning = "\u26a0\ufe0f"

// paintFrame renders m the way bubbletea's renderer paints it and returns the painted grid, one
// string per terminal row. Trailing blank cells are not part of a painted row (a terminal shows
// nothing there), and the placeholder cell that follows a wide grapheme contributes nothing of
// its own, so a returned row holds exactly the glyphs the terminal would show, in paint order.
//
// The path mirrors cursedRenderer.flush: an alt-screen frame is drawn into a screen buffer the
// size of the terminal, so the "frame taller than the screen, drop the top rows" branch that
// flush has for the inline renderer cannot apply here and is deliberately not reproduced.
func paintFrame(t *testing.T, m Model, method ansi.Method) []string {
	t.Helper()
	if m.width < 1 || m.height < 1 {
		t.Fatalf("model is %dx%d: send a tea.WindowSizeMsg before painting it", m.width, m.height)
	}

	buf := uv.NewScreenBuffer(m.width, m.height)
	buf.Method = method
	buf.Clear()
	uv.NewStyledString(m.View().Content).Draw(buf, buf.Bounds())

	rows := make([]string, buf.Height())
	for y := range rows {
		rows[y] = buf.Lines[y].String()
	}
	return rows
}

// paintedWidth reports how many terminal columns row occupies when painted with method.
//
// The method is an explicit argument rather than a fixed measure because a painted width is
// only meaningful against the measure it was painted with — a helper that picked one would be
// the same silent assumption this plan exists to remove.
func paintedWidth(row string, method ansi.Method) int {
	return method.StringWidth(row)
}

// paintedColumn reports the column the first occurrence of glyph is painted in on row, or -1
// when the row does not contain it. Rows come from paintFrame, so the answer is a real screen
// coordinate: the same space the terminal reports a mouse click in.
func paintedColumn(row, glyph string, method ansi.Method) int {
	i := strings.Index(row, glyph)
	if i < 0 {
		return -1
	}
	return method.StringWidth(row[:i])
}

// paintTestModel returns a ready 80×24 model whose transcript overflows the viewport — so the
// scroll bar has a thumb to paint — with tail appended as the last paragraphs. The filler is
// deliberately plain ASCII with no box-drawing glyphs, so the only │/█ on a transcript row is
// the scroll bar itself.
func paintTestModel(t *testing.T, tail ...string) Model {
	t.Helper()
	var b strings.Builder
	for i := range 40 {
		b.WriteString("filler paragraph ")
		b.WriteByte(byte('a' + i%26))
		b.WriteString("\n\n")
	}
	for _, p := range tail {
		b.WriteString(p)
		b.WriteString("\n\n")
	}
	m := newTestModel(t)
	return step(t, m, eventMsg{Event: domain.MessageEvent{Text: b.String()}})
}

// transcriptPaintRows returns the painted rows the transcript viewport owns — the scroll-bar
// gutter included, the bottom chrome excluded.
func transcriptPaintRows(t *testing.T, m Model, method ansi.Method) []string {
	t.Helper()
	rows := paintFrame(t, m, method)
	if len(rows) < m.viewport.Height() {
		t.Fatalf("painted %d rows, fewer than the viewport's %d", len(rows), m.viewport.Height())
	}
	return rows[:m.viewport.Height()]
}

// The harness reproduces the real paint path rather than inventing one: on a transcript the two
// measures agree about (plain ASCII), every painted row is the row the Model built, under both
// width methods. Only the trailing blanks differ — the composed view pads rows out to the block
// width, and a terminal paints nothing in a blank cell at the end of a line.
func TestPaintFrameMatchesTheViewOnASCII(t *testing.T) {
	m := paintTestModel(t, "a plain ascii tail paragraph")

	want := strings.Split(plain(m.View()), "\n")

	for _, method := range []ansi.Method{ansi.WcWidth, ansi.GraphemeWidth} {
		got := paintFrame(t, m, method)
		if len(got) != len(want) {
			t.Fatalf("method %v painted %d rows, want %d", method, len(got), len(want))
		}
		for i := range want {
			if trimRight(got[i]) != trimRight(want[i]) {
				t.Errorf("method %v row %d:\n painted %q\n    view %q", method, i, got[i], want[i])
			}
		}
	}
}

// trimRight drops the trailing blanks a painted row never shows.
func trimRight(s string) string { return strings.TrimRight(s, " ") }

// CHARACTERIZATION TEST — it pins a BUG, not an invariant. Item 3 of
// "docs/plans/2026-07-31 - 03 - width-authority-plan.md" fixes the drift and inverts this test
// into the invariant "the bar lands in the same painted column on every row, under both
// methods"; until then this is the failing behaviour held still so the fix can be shown to work.
//
// The scroll bar's column is set by the lipgloss.JoinHorizontal at model.go:2119, which pads
// each transcript row to the viewport width in GraphemeWidth. A terminal that has not answered
// mode 2027 paints in WcWidth, advances one column short over the ⚠️ cluster, and drops that
// row's bar glyph one column to the left of every other row's.
func TestPaintedScrollbarDriftsOnVS16_CharacterizesBug(t *testing.T) {
	m := paintTestModel(t, "danger "+vs16Warning+" here", "a plain ascii tail paragraph")

	barColumns := func(method ansi.Method) (vs16 int, others map[int]int) {
		t.Helper()
		others = map[int]int{}
		vs16 = -1
		for _, row := range transcriptPaintRows(t, m, method) {
			col := paintedColumn(row, "│", method)
			if col < 0 {
				col = paintedColumn(row, "█", method) // the thumb rows carry the other glyph
			}
			if col < 0 {
				t.Fatalf("method %v: no scroll-bar glyph on painted row %q", method, row)
			}
			if strings.Contains(row, vs16Warning) {
				vs16 = col
				continue
			}
			others[col]++
		}
		if vs16 < 0 {
			t.Fatalf("method %v: the ⚠️ row is not on screen", method)
		}
		if len(others) != 1 {
			t.Fatalf("method %v: the ascii rows disagree about the bar column: %v", method, others)
		}
		return vs16, others
	}

	t.Run("WcWidth drifts one column left", func(t *testing.T) {
		vs16, others := barColumns(ansi.WcWidth)

		var ascii int
		for col := range others {
			ascii = col
		}
		if want := ascii - 1; vs16 != want {
			t.Errorf("⚠️ row paints its scroll bar in column %d; the recorded bug is column %d, "+
				"one left of the ascii rows' %d (has item 3 landed? invert this test)", vs16, want, ascii)
		}
	})

	t.Run("GraphemeWidth does not", func(t *testing.T) {
		vs16, others := barColumns(ansi.GraphemeWidth)

		var ascii int
		for col := range others {
			ascii = col
		}
		if vs16 != ascii {
			t.Errorf("⚠️ row paints its scroll bar in column %d, want %d — under the measure the "+
				"layout code itself uses the bar is not supposed to drift", vs16, ascii)
		}
	})
}

// The two measures in play, pinned side by side. apogee's whole layout side measures in
// GraphemeWidth (lipgloss.Width and ansi.StringWidth both resolve to it); the painter measures
// in WcWidth unless a terminal answers mode 2027. A dependency bump that moves either number
// changes what the TUI paints, so it should fail here first and loudly.
func TestPaintedWidthMeasuresDisagreeOnVS16(t *testing.T) {
	tests := []struct {
		name              string
		s                 string
		grapheme, wcWidth int
	}{
		{"ascii", "abc", 3, 3},
		{"cjk", "日本語", 6, 6},
		{"emoji presentation", "🙂", 2, 2},
		{"warning sign, bare", "\u26a0", 1, 1},
		{"warning sign, vs16", vs16Warning, 2, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := paintedWidth(tc.s, ansi.GraphemeWidth); got != tc.grapheme {
				t.Errorf("GraphemeWidth(%q) = %d, want %d", tc.s, got, tc.grapheme)
			}
			if got := paintedWidth(tc.s, ansi.WcWidth); got != tc.wcWidth {
				t.Errorf("WcWidth(%q) = %d, want %d", tc.s, got, tc.wcWidth)
			}
			// ansi.StringWidth is the spelling every apogee measurement site reaches for
			// (directly, or through lipgloss.Width): it must stay the GraphemeWidth column.
			if got := ansi.StringWidth(tc.s); got != tc.grapheme {
				t.Errorf("ansi.StringWidth(%q) = %d, want the GraphemeWidth %d", tc.s, got, tc.grapheme)
			}
		})
	}
}
