package tui

import (
	"bytes"
	"context"
	"image/color"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/airiclenz/apogee/internal/gitstatus"
	"github.com/airiclenz/apogee/internal/scheme"
)

// lightScheme is the built-in light palette, resolved the way the binary resolves a configured name.
func lightScheme(t *testing.T) scheme.Scheme {
	t.Helper()
	s, warnings := scheme.Resolve("light", "")
	if len(warnings) > 0 {
		t.Fatalf("resolving the built-in light scheme warned: %v", warnings)
	}
	return s
}

// schemeModel returns a ready 80×24 model drawn in s, with a scheme host that resolves any name to
// switchTo — the seam applyColorScheme reads.
func schemeModel(t *testing.T, s, switchTo scheme.Scheme) Model {
	t.Helper()
	opts := colorSchemeOpts(nil, []string{"dark", "light"},
		func(string) (scheme.Scheme, []string) { return switchTo, nil }, nil)
	opts.ColorScheme = s
	opts.Workspace = "/home/user/project"
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	return step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
}

// sameColor reports whether two colours paint the same RGBA, whatever concrete type carries them.
func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// apogee never sets the terminal's default background: the transcript stands on the terminal's own
// background under either built-in scheme and after a live switch, and only the bottom chrome paints
// `surface` (TestBottomChromePaintsOneBackground).
func TestViewKeepsTheTerminalBackground(t *testing.T) {
	t.Parallel()
	dark, light := scheme.Default(), lightScheme(t)
	for _, tc := range []struct {
		name string
		s    scheme.Scheme
	}{{"dark", dark}, {"light", light}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := schemeModel(t, tc.s, tc.s)
			if got := m.View().BackgroundColor; got != nil {
				t.Errorf("View().BackgroundColor = %v, want nil (the terminal's own background)", got)
			}
		})
	}

	t.Run("after a scheme switch", func(t *testing.T) {
		t.Parallel()
		m := schemeModel(t, dark, light)
		if _, _, err := m.applyColorScheme("light"); err != nil {
			t.Fatalf("applyColorScheme: %v", err)
		}
		if got := m.View().BackgroundColor; got != nil {
			t.Errorf("View().BackgroundColor = %v after the switch, want nil (the terminal's own background)", got)
		}
	})
}

// The gap row above the chrome stays on the terminal's own background, like the transcript, and
// every cell from the ▔ rule down to the closing ▁ rule is painted on `surface` to the full width — the cells past each row's text included (footer row 2 holding only the
// workdir, footer row 1 after the mode label, the status line's margin after the gauge) — so the
// bottom of the screen is one band rather than chrome bands over the terminal's own background.
// The one exception is the gauge's own groove: its empty track and partial cell are painted on
// `chrome` by design (renderGaugeBar), so the status line may carry one run of at most gaugeWidth
// chrome cells; the gauge's filled cells are on `surface` like the rest.
func TestBottomChromePaintsOneBackground(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		s    scheme.Scheme
	}{{"dark", scheme.Default()}, {"light", lightScheme(t)}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := schemeModel(t, tc.s, tc.s)
			m.ctxUsed = 10000 // a third of the 32768 window: full cells, a partial cell and a track
			buf := uv.NewScreenBuffer(m.width, m.height)
			buf.Clear()
			uv.NewStyledString(m.View().Content).Draw(buf, buf.Bounds())

			// With no overlay open the gap row sits directly under the transcript and the chrome
			// runs from the ▔ rule under it to the last row of the window; the status line is under
			// the ▔ rule.
			gap := m.transcriptRows()
			for x := range m.width {
				if c := buf.CellAt(x, gap); c == nil || c.Style.Bg != nil {
					t.Fatalf("gap row %d column %d: background set, want the terminal's own", gap, x)
				}
			}
			status := gap + gapHeight + topRuleHeight
			groove := 0 // the status line's chrome cells: the gauge's track and partial cell
			for y := gap + gapHeight; y < m.height; y++ {
				grooveEnd := -1
				for x := range m.width {
					c := buf.CellAt(x, y)
					if c == nil {
						t.Fatalf("row %d: no cell at column %d", y, x)
					}
					if y == status && sameColor(c.Style.Bg, m.th.chrome) && (grooveEnd < 0 || grooveEnd == x) {
						groove++
						grooveEnd = x + 1
						continue
					}
					if !sameColor(c.Style.Bg, m.th.surface) {
						t.Errorf("row %d (%q) column %d: background %v, want surface %v",
							y, buf.Lines[y].String(), x, c.Style.Bg, m.th.surface)
						break
					}
				}
			}
			if groove == 0 || groove > gaugeWidth {
				t.Errorf("the status line carries %d chrome cells, want the gauge's groove: 1 to %d", groove, gaugeWidth)
			}
		})
	}
}

// paintedFrame draws m's View into a screen buffer of the window's size, in the measure the frame
// was laid out in — the parse bubbletea's renderer runs on every frame.
func paintedFrame(t *testing.T, m Model) uv.ScreenBuffer {
	t.Helper()
	buf := uv.NewScreenBuffer(m.width, m.height)
	buf.Method = m.th.measure.Method()
	uv.NewStyledString(m.View().Content).Draw(buf, buf.Bounds())
	return buf
}

// isConcealed reports whether c carries the conceal attribute.
func isConcealed(c *uv.Cell) bool {
	return c != nil && c.Style.Attrs&uv.AttrConceal != 0
}

// isPaintedSpace reports whether c is a width-1 space with a background set, concealed or not.
func isPaintedSpace(c *uv.Cell) bool {
	return c != nil && c.Width == 1 && c.Content == " " && c.Style.Bg != nil
}

// statusRow is the row the status line paints on while no transcript-side pane is open: under the
// gap row and the ▔ rule (TestBottomChromePaintsOneBackground).
func statusRow(m Model) int {
	return m.transcriptRows() + gapHeight + topRuleHeight
}

// footerFrameRows is the rows the footer paints on: directly above the ▁ rule (mouse.go's
// footerRowY).
func footerFrameRows(m Model) []int {
	rows := make([]int, 0, m.footerRows())
	for row := range m.footerRows() {
		rows = append(rows, m.height-bottomRuleHeight-m.footerRows()+row)
	}
	return rows
}

// unsealedChromeRows returns the bottom-chrome rows of m's frame as they are squared BEFORE their
// edges are sealed, keyed by frame row: the footer's rows always, and the status line's while the
// model is idle (a running status line states an elapsed time a second call could read a second
// later).
func unsealedChromeRows(m Model) map[int]string {
	unsealed := make(map[int]string, m.footerRows()+1)
	for i, ln := range strings.Split(m.footerView(), "\n") {
		unsealed[footerFrameRows(m)[i]] = squareLine(m.th.measure, ln, m.width)
	}
	if m.state == stateIdle {
		unsealed[statusRow(m)] = squareLine(m.th.measure, m.statusLine(), m.width)
	}
	return unsealed
}

// sealedFrames are the frames the sealed-edge claims are judged on. rows names the rows the frame's
// field paints, which must include at least one row ending (or starting) in painted blanks; a frame
// whose field paints none — a sent prompt block ends a column short of the edge, in the unpainted
// scrollbar column, and a popup's rows end in its border glyphs — carries no rows and counts for the
// whole-frame claims only, never as coverage of its field.
var sealedFrames = []struct {
	name    string
	arrange func(t *testing.T) Model
	rows    func(m Model) []int
}{{
	name: "idle status line",
	arrange: func(t *testing.T) Model {
		t.Helper()
		m := schemeModel(t, scheme.Default(), scheme.Default())
		m.ctxUsed = 0
		return m
	},
	rows: func(m Model) []int { return []int{statusRow(m)} },
}, {
	name: "two-row footer with a git snapshot",
	arrange: func(t *testing.T) Model {
		t.Helper()
		return withGitSnapshot(footerFactsModelIn(t, footerTwoRows, "/ws/proj"), footerGitTestSnapshot)
	},
	rows: footerFrameRows,
}, {
	name: "sent prompt block",
	arrange: func(t *testing.T) Model {
		t.Helper()
		m := newTestModel(t)
		m.input.SetValue("hello world")
		m = step(t, m, keyEnter())
		if e := lastEntry(t, m); e.kind != entryUser {
			t.Fatalf("tail entry = %+v, want the sent prompt block — test premise broken", e)
		}
		return m
	},
}, {
	name: "open popup",
	arrange: func(t *testing.T) Model {
		t.Helper()
		m := modelWithOverlayRoomAt(t, 80, 24, testOpts)
		m.input.SetValue("/")
		m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
		m.layout()
		if !m.autocomplete.active || len(m.autocomplete.items) == 0 {
			t.Fatal(`the "/" menu did not open — test premise broken`)
		}
		return m
	},
}}

// No row of a frame ends — or starts — in a painted blank the renderer could leave to an erase
// (sealRowEdges): every edge cell that is a space with a background set carries the conceal
// attribute, and nothing else in the frame does. Sealing writes the edge cell and nothing else:
// every other cell of a sealed row keeps the content and style it had before, and the edge cell its
// own, conceal aside.
func TestPaintedRowEdgesAreSealed(t *testing.T) {
	t.Parallel()
	for _, tc := range sealedFrames {
		runParallelCase(t, tc.name, func(t *testing.T) {
			m := tc.arrange(t)

			buf := paintedFrame(t, m)

			if tc.rows != nil {
				assertFieldPaintsAnEdge(t, buf, tc.rows(m))
			}
			last := m.width - 1
			for y := range m.height {
				for _, x := range []int{0, last} {
					if c := buf.CellAt(x, y); isPaintedSpace(c) && !isConcealed(c) {
						t.Errorf("row %d (%q) column %d: a painted blank left unsealed", y, buf.Lines[y].String(), x)
					}
				}
				for x := 1; x < last; x++ {
					if isConcealed(buf.CellAt(x, y)) {
						t.Errorf("row %d column %d: concealed, but only a row's edge cells are sealed", y, x)
					}
				}
			}
			for y, ln := range unsealedChromeRows(m) {
				assertSealedOnlyAtTheEdges(t, m, buf.Lines[y], ln)
			}
		})
	}
}

// assertFieldPaintsAnEdge fails unless one of rows starts or ends in a painted space: the frame's
// field reaches a row's edge in painted blanks, so the frame covers that field.
func assertFieldPaintsAnEdge(t *testing.T, buf uv.ScreenBuffer, rows []int) {
	t.Helper()
	for _, y := range rows {
		if isPaintedSpace(buf.CellAt(0, y)) || isPaintedSpace(buf.CellAt(buf.Width()-1, y)) {
			return
		}
	}
	t.Fatalf("rows %v: none starts or ends in a painted blank — the frame does not cover its field", rows)
}

// assertSealedOnlyAtTheEdges compares a painted row against the parse of the line it was sealed
// from: every cell's content equal, every style equal, the two edge cells' save for conceal.
func assertSealedOnlyAtTheEdges(t *testing.T, m Model, sealed uv.Line, unsealedLine string) {
	t.Helper()
	ref := uv.NewScreenBuffer(m.width, 1)
	ref.Method = m.th.measure.Method()
	uv.NewStyledString(unsealedLine).Draw(ref, ref.Bounds())
	for x := range m.width {
		got, want := sealed.At(x), ref.CellAt(x, 0)
		style := got.Style
		if x == 0 || x == m.width-1 {
			style.Attrs &^= uv.AttrConceal
		}
		if got.Content != want.Content || !style.Equal(&want.Style) {
			t.Errorf("row %q column %d: cell {%q %+v}, want the unsealed parse's {%q %+v}",
				sealed.String(), x, got.Content, got.Style, want.Content, want.Style)
		}
	}
}

// eraseSequence matches an erase in line (EL 0, 1, 2) or in display (ED 0–3), the bare form
// included.
var eraseSequence = regexp.MustCompile(`\x1b\[[0-3]?[JK]`)

// Ultraviolet's renderer writes every painted row out rather than erasing it: rendering the frame's
// painted rows through a TerminalRenderer for an xterm-compatible terminal (where clearing to the
// end of a line is free and would be its first choice) emits no erase but its own initial screen
// clear on a first paint, and none at all on a repaint that shortens the chrome — the gauge going
// idle, a shorter git snapshot — which is exactly where it would clear a row's emptied tail.
// Terminal.app draws erased cells in its own background, so any erase there would show through
// the field.
func TestPaintedRowsAreNeverErased(t *testing.T) {
	t.Parallel()
	m := withGitSnapshot(footerFactsModelIn(t, footerTwoRows, "/ws/proj"), footerGitTestSnapshot)
	m.ctxUsed = 10000
	var out bytes.Buffer
	r := uv.NewTerminalRenderer(&out, []string{"TERM=xterm-256color", "TERM_PROGRAM=Apple_Terminal"})
	r.SetFullscreen(true)
	r.Erase()
	first := paintedRows(t, m)
	buf := uv.NewScreenBuffer(m.width, len(first))
	buf.Method = m.th.measure.Method()

	renderRows(t, r, buf, first)

	if got := eraseSequence.FindAllString(out.String(), -1); len(got) != 1 || got[0] != "\x1b[2J" {
		t.Errorf("first paint erased %q, want the renderer's one initial screen clear", got)
	}

	out.Reset()
	m.ctxUsed = 0
	m = withGitSnapshot(m, gitstatus.Snapshot{Branch: "main"})
	second := paintedRows(t, m)
	if len(second) != len(first) || strings.Join(second, "\n") == strings.Join(first, "\n") {
		t.Fatalf("repaint rows = %d (changed: %v), want the same %d painted rows changed — test premise broken",
			len(second), strings.Join(second, "\n") != strings.Join(first, "\n"), len(first))
	}

	renderRows(t, r, buf, second)

	if got := eraseSequence.FindAllString(out.String(), -1); len(got) != 0 {
		t.Errorf("repaint erased %q, want every painted row written out", got)
	}
}

// paintedRows returns the lines of m's frame whose last cell has a background set.
func paintedRows(t *testing.T, m Model) []string {
	t.Helper()
	buf := paintedFrame(t, m)
	lines := strings.Split(m.View().Content, "\n")
	rows := make([]string, 0, len(lines))
	for y, ln := range lines {
		if c := buf.CellAt(m.width-1, y); c != nil && c.Style.Bg != nil {
			rows = append(rows, ln)
		}
	}
	if len(rows) == 0 {
		t.Fatal("the frame paints no row to its edge — test premise broken")
	}
	return rows
}

// renderRows draws rows into buf and renders it through r, as bubbletea's cursed renderer does each
// frame (its cell buffer drawn in place, then its RenderBuffer rendered and flushed).
func renderRows(t *testing.T, r *uv.TerminalRenderer, buf uv.ScreenBuffer, rows []string) {
	t.Helper()
	uv.NewStyledString(strings.Join(rows, "\n")).Draw(buf, buf.Bounds())
	r.Render(buf.RenderBuffer)
	if err := r.Flush(); err != nil {
		t.Fatalf("flushing the renderer: %v", err)
	}
}
