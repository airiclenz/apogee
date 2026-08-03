package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
// deliberately plain ASCII with no box-drawing glyphs, so the only vertical stroke on a transcript
// row is the scroll bar itself.
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

// paintedAs returns m with its width authority moved to method — the state the real program is in
// once the terminal has answered mode 2027, or has not. The authority MIRRORS the painter
// (width.go), so a frame is only ever composed and painted in ONE measure; a test that wants the
// GraphemeWidth painter therefore has to hand the model the same report bubbletea hands it, rather
// than paint a WcWidth-composed frame with the other measure — which is the mismatch the authority
// exists to prevent, not a case it has to survive.
func paintedAs(t *testing.T, m Model, method ansi.Method) Model {
	t.Helper()
	if method == ansi.GraphemeWidth {
		m = step(t, m, tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet})
	}
	if got := m.th.measure.Method(); got != method {
		t.Fatalf("model measures with %v, want the painter's %v", got, method)
	}
	return m
}

// paintMethods is the pair every painted assertion runs against: the measure a terminal that never
// answers mode 2027 paints in, and the one bubbletea switches to when a terminal does.
var paintMethods = []struct {
	name   string
	method ansi.Method
}{
	{"WcWidth — the painter's default", ansi.WcWidth},
	{"GraphemeWidth — the terminal answered mode 2027", ansi.GraphemeWidth},
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

// THE INVARIANT — this is the inverted form of the characterization test item 1 left here (it
// pinned the bar drifting one column left on the ⚠️ row under WcWidth). The scroll bar is a
// straight column: every transcript row paints its track/thumb glyph in the window's last column,
// the ⚠️ row included, whichever measure the painter is using.
//
// What used to break it was the join, not the bar: lipgloss.JoinHorizontal padded each transcript
// row to the block's GraphemeWidth, so on a WcWidth painter the ⚠️ row arrived at the gutter a
// column short. joinScrollbar (model.go) squares every row off in the authority's measure instead,
// which is by construction the measure the frame is painted in.
func TestPaintedScrollbarHoldsOneColumn(t *testing.T) {
	for _, tc := range paintMethods {
		t.Run(tc.name, func(t *testing.T) {
			m := paintedAs(t, paintTestModel(t, "danger "+vs16Warning+" here", "a plain ascii tail paragraph"), tc.method)

			columns := map[int]int{}
			sawVS16 := false
			for _, row := range transcriptPaintRows(t, m, tc.method) {
				col := paintedColumn(row, glyphScrollTrack, tc.method)
				if col < 0 {
					col = paintedColumn(row, glyphScrollThumb, tc.method) // the thumb rows carry the other glyph
				}
				if col < 0 {
					t.Fatalf("no scroll-bar glyph on painted row %q", row)
				}
				columns[col]++
				sawVS16 = sawVS16 || strings.Contains(row, vs16Warning)
			}
			if !sawVS16 {
				t.Fatal("the ⚠️ row is not on screen — the fixture no longer exercises the drift")
			}
			if len(columns) != 1 {
				t.Fatalf("the scroll bar is painted in %d different columns, want exactly one: %v", len(columns), columns)
			}
			for col := range columns {
				if want := m.width - 1; col != want {
					t.Errorf("the scroll bar paints in column %d, want the window's last column %d", col, want)
				}
			}
		})
	}
}

// A markdown table's frame is one terminal cell wide in EITHER measure — the same property the
// scroll bar's two glyphs have, and for the same reason: mdtable.go spends tableDividerWidth as a
// constant instead of measuring the divider it draws, and a two-cell glyph under one of the
// painter's measures would make every column arithmetic in the file a cell short there.
//
// The second half is the invariant that constant buys: every line of a rendered table — header,
// rule and body rows alike — paints exactly as many columns as every other, in the measure the
// painter is using. The fixture carries the disputed ⚠️ grapheme in a cell, so a cell padded with
// a hard-wired GraphemeWidth rather than the authority (ADR 0030) comes out a column long here on
// the WcWidth painter, which is the default one.
func TestTableDividerHoldsOneColumn(t *testing.T) {
	source := strings.Join([]string{
		"| Tool | Calls | Notes |",
		"|:--|--:|:-:|",
		"| Read File | 12 | danger " + vs16Warning + " |",
		"| Run | 3 | `go test ./...` |",
	}, "\n")

	for _, tc := range paintMethods {
		t.Run(tc.name, func(t *testing.T) {
			for _, glyph := range []string{glyphTableColumn, glyphTableCross} {
				if got := paintedWidth(glyph, tc.method); got != 1 {
					t.Errorf("%q paints %d columns, want the one cell tableDividerWidth assumes", glyph, got)
				}
			}

			th := newTheme()
			th.measure = widthAuthority{method: tc.method}

			lines := renderMarkdownBody(th, source, 40)
			if len(lines) != 4 {
				t.Fatalf("rendered %d lines, want 4 (header, rule, two rows): %#v", len(lines), visible(lines))
			}
			if !strings.Contains(strip(lines[1]), glyphTableCross) {
				t.Fatalf("the rule row carries no crossing — the fixture is not a ruled table: %q", strip(lines[1]))
			}
			want := paintedWidth(lines[0], tc.method)
			for i, ln := range lines {
				if got := paintedWidth(ln, tc.method); got != want {
					t.Errorf("table line %d paints %d columns, want the header's %d: %q", i, got, want, strip(ln))
				}
			}
		})
	}
}

// The absolute width cap of layout.md ("no rendered line ever exceeds the width the block was
// given"), asserted in the measure the frame is PAINTED in rather than the one it happened to be
// measured in — which is the whole of what this plan changes.
//
// The composed rows are the oracle, not paintFrame's grid: the screen buffer is exactly m.width
// cells wide, so it can only ever show a cap violation as silently lost content. Measuring the
// row the Model hands bubbletea, with the painter's own method, catches the overrun itself.
func TestComposedFrameHoldsTheWidthCapWhenPainted(t *testing.T) {
	for _, tc := range paintMethods {
		t.Run(tc.name, func(t *testing.T) {
			m := paintedAs(t, paintTestModel(t,
				"a plain ascii tail paragraph",
				"日本語のテキストと ascii が混ざった段落",
				"danger "+vs16Warning+" here, and "+vs16Warning+vs16Warning+" twice more",
				strings.Repeat("⚠️ ", 60), // a paragraph that has to wrap on the disputed grapheme
			), tc.method)

			for i, row := range strings.Split(m.View().Content, "\n") {
				if w := paintedWidth(row, tc.method); w > m.width {
					t.Errorf("composed row %d paints %d columns wide, past the %d-column window: %q",
						i, w, m.width, row)
				}
			}
		})
	}
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

// A pop-up's columns are the scroll bar's invariant one surface over: every row opens its second
// column in the same PAINTED cell, the row carrying the disputed ⚠️ grapheme included.
//
// popupColumnWidths measures a cell and layoutPopupRow pads it back out to that measure, and both
// used to be hard-wired to ansi.StringWidth. On the WcWidth painter — the DEFAULT one — a cell
// carrying VS16 was therefore charged two cells for a glyph the terminal draws in one, so it was
// padded a cell short and the column after it opened a cell early on that row alone: the pop-up's
// own version of the bar drifting left. Routing the measure and the pad through th.measure
// (ADR 0030) is what squares them, and the fixture's second row is the straight edge that proves it.
func TestPaintedPopupColumnsHoldOneOffset(t *testing.T) {
	rows := []popupRow{
		{"danger " + vs16Warning, "— first"},
		{"eight_ok", "— second"}, // eight cells in EITHER measure
	}

	for _, tc := range paintMethods {
		t.Run(tc.name, func(t *testing.T) {
			th := newTheme()
			th.measure = widthAuthority{method: tc.method}

			widest := 0
			for _, row := range rows {
				widest = max(widest, paintedWidth(row[0], tc.method))
			}
			want := widest + paintedWidth(popupGutter, tc.method)

			for i, ln := range layoutPopupRows(th, rows) {
				if got := paintedColumn(ln, "—", tc.method); got != want {
					t.Errorf("row %d opens its second column in painted cell %d, want %d: %q",
						i, got, want, ln)
				}
			}
		})
	}
}

// truncateToWidth clips in the measure the painter is on, which cuts both ways and both are
// defects. Clipping in GraphemeWidth on a WcWidth painter sheds text the terminal had room for —
// the pane's width is scarce enough that a needlessly dropped word is a real loss — while the
// mirror-image mistake would let a row spill past the border the pane composes every line to.
// Measuring and cutting must also agree with EACH OTHER (ADR 0030 §3), which is why the function
// takes both operations off th.measure rather than pairing one with ansi.Truncate.
func TestPopupTruncationFollowsThePainter(t *testing.T) {
	const title = "danger " + vs16Warning // eight painted cells under WcWidth, nine under GraphemeWidth

	for _, tc := range paintMethods {
		t.Run(tc.name, func(t *testing.T) {
			th := newTheme()
			th.measure = widthAuthority{method: tc.method}

			const room = 8
			got := truncateToWidth(th, title, room)
			if w := paintedWidth(got, tc.method); w > room {
				t.Errorf("clipped title paints %d columns, past the pane's %d: %q", w, room, got)
			}
			switch fits := paintedWidth(title, tc.method) <= room; {
			case fits && got != title:
				t.Errorf("clipped %q to %q, but the painter seats it whole in %d columns", title, got, room)
			case !fits && got == title:
				t.Errorf("kept %q whole at %d columns, which the painter cannot seat", title, room)
			}
		})
	}
}

// The staged-row band is a solid bar: every one of its rows paints edge to edge across the window,
// so no seam shows the terminal's own background through the black fill. queuedRow composes that
// row by clipping to the window and padding back out to it, and a pad computed in a measure the
// painter is not on leaves the bar a column short on exactly the row carrying ⚠️ — the row a human
// would read as a rendering glitch rather than as a width bug.
func TestPaintedQueuedBandFillsTheWindow(t *testing.T) {
	for _, tc := range paintMethods {
		t.Run(tc.name, func(t *testing.T) {
			m := stageRow(t, paintedAs(t, runningModel(t), tc.method), "danger "+vs16Warning+" queued")

			lines := strings.Split(strip(m.renderPendingInterjections()), "\n")
			if len(lines) != 3 {
				t.Fatalf("band lines = %d (%q), want one staged row framed by a blank row each side",
					len(lines), lines)
			}
			for i, ln := range lines {
				if got := paintedWidth(ln, tc.method); got != m.width {
					t.Errorf("band row %d paints %d columns, want the window's %d: %q",
						i, got, m.width, ln)
				}
			}
		})
	}
}

// A boxed surface paints exactly as many rows as it composed, and every one of them reaches the
// box's right border.
//
// Both boxes used to hand their total width to lipgloss — th.popupBorder.Width(width) for the pane
// and th.startupBorder.Width(width) for the start-up card — and a lipgloss Width does not merely
// pad: past its width it WRAPS, in GraphemeWidth whatever the painter is doing. A row the authority
// measures as exactly the box's inner width can be wider than that to lipgloss, because a VARIATION
// SELECTOR-16 cluster is one cell to the WcWidth painter and two to lipgloss. So ONE composed row
// came back as TWO painted rows, splitting the row's tail onto a line of its own: the pane below
// painted six rows where it composed five, and the start-up card seven where it composed six, the
// fold leaving rows that stop short of the box's right border. A pane taller than the frame budgeted for it
// (popupBudget) is the version of that defect a human sees. drawBox draws the rows instead, in the
// painter's own measure (ADR 0030 §5).
//
// The GraphemeWidth arm is the case the two measures agree about, so it passed before the fix too;
// it is here to pin that the fix left the agreeing case exactly where it was.
func TestPaintedBoxRowsAreNotFolded(t *testing.T) {
	// One row of ⚠️ , six times: twelve cells to the WcWidth painter and eighteen to lipgloss, which
	// is the disagreement — the row fits the pane's inner width in the measure the terminal paints
	// in, and overflows it in the measure lipgloss would have wrapped it at.
	pane := popupSpec{
		title:       "pick one",
		rows:        []popupRow{{strings.Repeat(vs16Warning+" ", 6)}},
		selected:    0,
		hint:        "esc close",
		maxBodyRows: -1,
		maxRows:     -1,
	}
	card := startupView{
		Logo:    "AAAA\nBBBB\nCCCC",
		Host:    "host",
		Model:   "danger " + vs16Warning, // the info block's widest value, so it sets the card's right edge
		Context: "32k",
		Version: "0.10.13",
	}

	for _, box := range []struct {
		name  string
		width int
		rows  int // the rows the surface composes: its two borders plus its content lines
		draw  func(th theme, width int) []string
	}{
		{
			name: "pop-up pane", width: 20, rows: 5, // ╭─╮, title, one row, hint, ╰─╯
			draw: func(th theme, width int) []string {
				return strings.Split(renderPopup(th, pane, width), "\n")
			},
		},
		{
			// The wide layout pairs logo line i with info row i and blank-fills the shorter side, so
			// the card's four info rows set its height however many lines the logo has.
			name: "start-up card", width: 30, rows: 6, // ╭─╮, four logo/info lines, ╰─╯
			draw: func(th theme, width int) []string { return renderStartupBox(th, card, width) },
		},
	} {
		for _, tc := range paintMethods {
			t.Run(box.name+"/"+tc.name, func(t *testing.T) {
				th := newTheme()
				th.measure = widthAuthority{method: tc.method}

				lines := box.draw(th, box.width)
				if len(lines) != box.rows {
					t.Errorf("box paints %d rows, want the %d it composed:\n%s",
						len(lines), box.rows, strings.Join(mapStrip(lines), "\n"))
				}
				for i, ln := range lines {
					plain := strip(ln)
					if got := paintedWidth(plain, tc.method); got != box.width {
						t.Errorf("row %d paints %d columns, want the box's %d: %q",
							i, got, box.width, plain)
					}
					if edge := lastGlyph(plain); edge != "╮" && edge != "│" && edge != "╯" {
						t.Errorf("row %d ends in %q, not the box's right border: %q", i, edge, plain)
					}
				}
			})
		}
	}
}

// mapStrip strips the SGR from every line of a block, for a failure message that shows the rows a
// box painted rather than the escape sequences it painted them with.
func mapStrip(lines []string) []string {
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = strip(ln)
	}
	return out
}

// lastGlyph is the final grapheme cluster of a plain line, or "" when it has none — the cell a boxed
// row's right border has to occupy.
func lastGlyph(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[len(runes)-1])
}
