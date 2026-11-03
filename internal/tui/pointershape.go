package tui

// pointerShape is the pointer the terminal should show over one point of the frame: what a click
// there would do, said in the three shapes apogee speaks (ADR 0106). The zero value is the default
// arrow, which is what every point no click acts on shows.
type pointerShape int

const (
	shapeDefault pointerShape = iota // the arrow: a click here does nothing
	shapeText                        // the I-beam: a click here seats a caret or starts a drag-select
	shapePointer                     // the hand: a click here activates a row, a block or a control
)

// name is the shape's CSS cursor name, the vocabulary the OSC 22 set sequence carries.
func (s pointerShape) name() string {
	switch s {
	case shapeText:
		return "text"
	case shapePointer:
		return "pointer"
	default:
		return "default"
	}
}

// pointerShapeAt answers which shape a click at (x, y) implies, for the frame pre was composed for
// (pre is m.withFrameSpans(), handleMouseClick's snapshot). It walks the click chain's own order —
// the panes ([pointerPanes], each asked through its row's hover), the footer's mode marker, the
// editable prompt, the transcript — and every region answers through the hit predicate its click
// handler decides by, geometry and state gate together, so the pointer can never promise a click the
// handler would refuse:
//
//   - a pane whose hover answers for the point: that pane's shape;
//   - the mode marker, where [Model.footerModeHit] holds: pointer;
//   - the prompt's text rows, while [Model.inputEditable]: text;
//   - a transcript line that is a block's click surface ([Model.blockTargetAt]): pointer;
//   - any other transcript line — drag-selectable: text;
//   - everywhere else, rows past the transcript's content included: default.
//
// It has no side effects: the Model is a value (ADR 0011) and nothing here writes to it.
func (m Model) pointerShapeAt(pre Model, x, y int) pointerShape {
	for _, p := range pointerPanes {
		hover := paneSpecs[p].hover
		if hover == nil {
			continue
		}
		if shape, ok := hover(pre, x, y); ok {
			return shape
		}
	}
	if m.footerModeHit(pre, x, y) {
		return shapePointer
	}
	if m.inputEditable() {
		if _, _, ok := pre.pointInputRow(x, y); ok {
			return shapeText
		}
	}
	line, _, ok := pre.pointTranscriptRow(x, y)
	if !ok {
		return shapeDefault
	}
	if _, toggles := pre.blockTargetAt(line); toggles {
		return shapePointer
	}
	return shapeText
}
