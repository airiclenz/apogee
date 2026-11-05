package tui

import (
	"context"
	"image/color"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

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
