package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// unmarkedLine returns the index of the first on-screen rendered line no block marked as a click
// surface that still holds text, failing when the paint carries none — the fixture no longer has a
// plain, drag-selectable line to aim at.
func unmarkedLine(t *testing.T, m Model) int {
	t.Helper()
	for i := m.viewport.YOffset(); i < len(m.lines) && i < len(m.lineTargets); i++ {
		if m.lineTargets[i].kind == targetNone && strings.TrimSpace(ansi.Strip(m.lines[i])) != "" {
			return i
		}
	}
	t.Fatal("no rendered line is unmarked and non-blank")
	return -1
}

// rowBelowTheTranscript returns a viewport row past the last rendered line that is still one of
// the transcript's own rows, failing when the content fills the transcript to its foot.
func rowBelowTheTranscript(t *testing.T, m Model) int {
	t.Helper()
	row := len(m.lines) - m.viewport.YOffset()
	if row < 0 || row >= m.transcriptRows() {
		t.Fatalf("row %d is not a transcript row past the content (%d rows)", row, m.transcriptRows())
	}
	return row
}

// TestPointerShapeAtAnswersEachRegionOutsideThePanes is the classifier's per-region contract: the
// shape for a point is the one a click there implies, decided by the same hit predicate the click
// handler uses, state gate included.
func TestPointerShapeAtAnswersEachRegionOutsideThePanes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		model func(*testing.T) Model
		point func(*testing.T, Model) (x, y int)
		want  pointerShape
	}{
		{
			name:  "the prompt's text area while editable",
			model: newTestModel,
			point: func(_ *testing.T, m Model) (int, int) {
				x0, y0, _, _ := m.inputContentRect()
				return x0, y0
			},
			want: shapeText,
		},
		{
			name: "the prompt's text area while not editable",
			model: func(t *testing.T) Model {
				m := newTestModel(t)
				m.input.Blur()
				return m
			},
			point: func(_ *testing.T, m Model) (int, int) {
				x0, y0, _, _ := m.inputContentRect()
				return x0, y0
			},
			want: shapeDefault,
		},
		{
			name:  "the footer's mode marker",
			model: newTestModel,
			point: footerMarkerMiddle,
			want:  shapePointer,
		},
		{
			name: "the footer's mode marker while a call awaits approval",
			model: func(t *testing.T) Model {
				m := newTestModel(t)
				m.state = stateAwaitingApproval
				return m
			},
			point: footerMarkerMiddle,
			want:  shapeDefault,
		},
		{
			name: "the footer's mode marker under the /settings pane",
			model: func(t *testing.T) Model {
				m := newTestModel(t)
				m.settings.open = true
				m.layout()
				return m
			},
			point: footerMarkerMiddle,
			want:  shapeDefault,
		},
		{
			name: "the footer's mode marker under the /sessions browser",
			model: func(t *testing.T) Model {
				m := newTestModel(t)
				m.sessionBrowser = sessionBrowser{open: true}
				m.layout()
				return m
			},
			point: footerMarkerMiddle,
			want:  shapeDefault,
		},
		{
			name:  "a tool block's header line",
			model: func(t *testing.T) Model { return modelWithToolBlock(t, "one\ntwo\nthree\nfour") },
			point: func(t *testing.T, m Model) (int, int) {
				return 2, screenRow(t, m, markedLine(t, m, targetHeader))
			},
			want: shapePointer,
		},
		{
			name:  "a plain transcript line",
			model: func(t *testing.T) Model { return modelWithTranscript(t, "hello") },
			point: func(t *testing.T, m Model) (int, int) {
				return 2, screenRow(t, m, unmarkedLine(t, m))
			},
			want: shapeText,
		},
		{
			name:  "a blank row below the transcript",
			model: func(t *testing.T) Model { return modelWithTranscript(t, "hello") },
			point: func(t *testing.T, m Model) (int, int) {
				return 2, rowBelowTheTranscript(t, m)
			},
			want: shapeDefault,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := tc.model(t)
			x, y := tc.point(t, m)

			got := m.pointerShapeAt(m.withFrameSpans(), x, y)

			if got != tc.want {
				t.Fatalf("pointerShapeAt(%d, %d) = %s, want %s", x, y, got.name(), tc.want.name())
			}
		})
	}
}

// TestPointerShapeAtNamesTheCSSShapes pins each shape to the CSS cursor name OSC 22 carries.
func TestPointerShapeAtNamesTheCSSShapes(t *testing.T) {
	t.Parallel()

	for shape, want := range map[pointerShape]string{
		shapeDefault: "default",
		shapeText:    "text",
		shapePointer: "pointer",
	} {
		if got := shape.name(); got != want {
			t.Errorf("pointerShape(%d).name() = %q, want %q", shape, got, want)
		}
	}
}

// footerMarkerMiddle aims at the middle cell of the footer's mode marker, through footerMarkerCells
// (mouse_test.go) so the point is the painter's own.
func footerMarkerMiddle(t *testing.T, m Model) (int, int) {
	t.Helper()
	y, _, middle, _ := footerMarkerCells(t, m)
	return middle, y
}
