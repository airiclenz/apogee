package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

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
	// The fast path: with no pane up, none can answer, and the walk would only ask each to say so.
	if pre.openPanes() != 0 {
		for _, p := range pointerPanes {
			if shape, ok := paneSpecs[p].hover(pre, x, y); ok {
				return shape
			}
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

// hoverPointerShape answers one button-less motion — a hover, which only all-motion tracking reports
// (pointerprobe.go) — with the pointer shape a click at (x, y) would imply. While the pointer-shape
// gate is open ([Model.pointerShapesLive]) and that shape differs from the last one written, it keeps
// the new shape, marks a shape as in force on the terminal, and returns the one OSC 22 write that
// shows it; an unchanged shape, or the gate shut, writes nothing, so a hover across one region costs a
// classification per cell and a single write.
//
// It never repaints: it reads the frame through a [Model.withFrameSpans] snapshot that stays here,
// and the motion it answers is the one Msg the Update tail leaves unsettled ([settleFrame]). A drag
// never reaches it — a held button reports its own motion — so a drag keeps the shape it started on.
func (m Model) hoverPointerShape(x, y int) (Model, tea.Cmd) {
	if !m.pointerShapesLive() {
		return m, nil
	}
	shape := m.pointerShapeAt(m.withFrameSpans(), x, y)
	if shape == m.pointerShape {
		return m, nil
	}
	m.pointerShape = shape
	m.pointerShapeWritten = true
	return m, tea.Raw(ansi.SetPointerShape(shape.name()))
}

// pointerShapeSuffix is what a mouse re-assert (reassertMouse, mousereassert.go) appends to the
// tracking escapes: the shape last written, said again, because the child that may have reset the
// terminal's mouse reporting may have reset its pointer too. It is empty while the gate is shut and
// before any shape has been written — the terminal is still on its own default then, and a write of
// the zero shapeDefault would be an OSC 22 byte the session never asked for.
func (m Model) pointerShapeSuffix() string {
	if !m.pointerShapesLive() || !m.pointerShapeWritten {
		return ""
	}
	return ansi.SetPointerShape(m.pointerShape.name())
}

// forgetPointerShape is the model's half of a pointer reset: it drops the last written shape, so the
// next hover writes afresh, and reports whether a shape was in force — the caller writes the reset
// only then, so a session that never wrote a shape never writes a reset either.
func (m Model) forgetPointerShape() (Model, bool) {
	written := m.pointerShapeWritten
	m.pointerShape, m.pointerShapeWritten = shapeDefault, false
	return m, written
}

// pointerShapeResetBothStacks is the reset written when apogee lets go of the terminal — the program's
// exit (claimTerminalScreen's release, tui.go) and the foreground-editor handoff
// (settingsExternalEdit, settingswatcher.go): the empty-name OSC 22 reset, then the same again on the
// alternate screen, ending on the primary one. A terminal keeps one pointer stack per screen (the
// kitty spec), and the reset can land on either — after the renderer has already left the alternate
// screen, or before — so saying it on both leaves both at the default arrow whichever was active.
func pointerShapeResetBothStacks() string {
	reset := ansi.SetPointerShape("")
	return reset + ansiEnterAltScreen + reset + ansiExitAltScreen
}

// pointerShapeWrittenBy reports whether final — the model the program returned from its run — left
// a pointer shape in force on the terminal, which is what decides whether the exit release writes
// the reset. Anything but a Model reads as no shape: nothing apogee knows of was written.
func pointerShapeWrittenBy(final tea.Model) bool {
	m, ok := final.(Model)
	return ok && m.pointerShapeWritten
}

// The panes' hovers, one per row of the pane table (panes.go), each the shape its pane's click hit
// implies ([paneHit.shape]) — asked of the same pure predicate the pane's click handler acts on, so
// the hover and the click cannot come apart. pre is the frame the point is aimed at, and it stands in
// for the live model too: nothing has been dismissed on a hover's way down the chain.
//
// Outside its box a pane answers only where its click claims there: the modal lists (the /sessions
// browser, the picker, the /workflows view) dismiss AND claim, so their outside is the arrow — the
// click only closes them — while the decision prompt, the /settings pane, the reports and the dropdown
// leave the point to the next region, as their clicks leave it to the chain (handleMouseClick's
// outside-click rule).

// settingsHover is the /settings pane's: the I-beam on the open field's text, the hand on a key the
// key list would select, the arrow on the rest of the pane's rows.
func settingsHover(pre Model, x, y int) (pointerShape, bool) {
	return pre.settingsHit(pre, x, y).shape()
}

// reportHover is a report's: the arrow anywhere inside its box, where a click is claimed for nothing.
func reportHover(r reportKind) func(pre Model, x, y int) (pointerShape, bool) {
	return func(pre Model, _, y int) (pointerShape, bool) {
		return reportHit(r, pre, y).shape()
	}
}

// browserHover is the /sessions browser's: the hand on a session row, the arrow on its chrome, in a
// rename or a delete confirm, and outside the box.
func browserHover(pre Model, _, y int) (pointerShape, bool) {
	hit, _ := pre.browserHit(pre, y)
	return hit.shape()
}

// pickerHover is the picker's: the hand on an offered row, the arrow elsewhere and outside the box.
func pickerHover(pre Model, _, y int) (pointerShape, bool) {
	return pre.pickerHit(pre, y).shape()
}

// workflowsHover is the /workflows view's: the hand on a row, the arrow on its chrome and prose, while
// the name row is open, and outside the box.
func workflowsHover(pre Model, _, y int) (pointerShape, bool) {
	return pre.workflowsHit(pre, y).shape()
}

// promptHover is the decision prompt's: the ask pane, then the approval pane, in promptPointerClick's
// order — only one is ever up, and the other answers nothing.
func promptHover(pre Model, _, y int) (pointerShape, bool) {
	if shape, ok := pre.askHit(pre, y).shape(); ok {
		return shape, true
	}
	return pre.approvalHit(pre, y).shape()
}

// dropdownHover is the "/" | "@" menu's: the hand on a row, the arrow on its chrome.
func dropdownHover(pre Model, _, y int) (pointerShape, bool) {
	return pre.dropdownHit(pre, y).shape()
}
