package tui

import "testing"

// frameGeometry is what layout() owns about a frame: the viewport widget's height and scroll offset,
// the input box's height, the frame key the transcript lines were painted under, and how many lines
// that paint produced. Two View() calls are deliberately not compared: in stateRunning the status
// clock reads time.Now(), so two renders differ across a second boundary with nothing laid out.
type frameGeometry struct {
	viewportHeight int
	yOffset        int
	inputHeight    int
	painted        frameKey
	lines          int
}

// geometryOf reads the layout-owned state off m.
func geometryOf(m Model) frameGeometry {
	return frameGeometry{
		viewportHeight: m.viewport.Height(),
		yOffset:        m.viewport.YOffset(),
		inputHeight:    m.input.Height(),
		painted:        m.painted,
		lines:          len(m.lines),
	}
}

// assertSettled fails the test unless m, a model Update returned, already stands where an explicit
// layout() would put it: the tail already settled it (doc.go, "an arm mutates; Update's tail lays
// out and repaints"). The copy is laid out after m's own geometry is read, so nothing the extra
// layout touches can leak into what is compared.
func assertSettled(t *testing.T, m Model) {
	t.Helper()
	got := geometryOf(m)
	relaid := m
	relaid.layout()
	if want := geometryOf(relaid); got != want {
		t.Errorf("frame left unsettled by Update's tail:\n got %+v\nwant %+v (after an explicit layout)", got, want)
	}
}
