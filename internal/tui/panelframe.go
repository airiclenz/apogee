package tui

import "github.com/airiclenz/apogee/internal/domain"

// The side panel's horizontal geometry (ADR 0110, ADR 0111 D7) as one pure value: whether the column
// shows, docks or overlays this frame, how wide it is docked and painted, the chrome around its
// content, and the cells a click or a drag lands on. It is built from the window, the show mode, the
// width setting, whether navigation mode asks for the overlay and the width a full sub-agent row
// needs — never from the Model, and never from the frame's composed rows ([Model.frameSpans]), so a
// width read renders no pane. [Model.panelGeometry] is the one place that asks the cursor whether the
// overlay is wanted; the sidepanel.go and model.go width helpers are delegates of it.

// panelGeometryInput is everything the side panel's horizontal geometry reads. It carries no row
// bound: the rows above the session-name rule are the frame's, and [panelGeometry.cell] takes that
// bound from its caller.
type panelGeometryInput struct {
	// window is the window's width in cells.
	window int
	// show is `ui.side-panel.show`: the mode whose gate floor decides docked or overlayable.
	show domain.SidePanelShow
	// width is `ui.side-panel.width` as configured: resolved for its cells, stated or not for its clamp.
	width domain.SidePanelWidth
	// isOverlaid is whether navigation mode asks for the overlay — the cursor stands in the panel
	// region with the keys its own. The geometry grants it only where the column is overlayable.
	isOverlaid bool
	// fullRowWidth is the content width a full sub-agent row needs ([Model.panelFullSubAgentRowWidth]):
	// the chrome yields cells below it.
	fullRowWidth int
}

// panelGeometry is the side panel's horizontal geometry for one window, setting and overlay state
// ([newPanelGeometry]). Docked, the column takes dockedWidth cells right of the transcript slot;
// overlaid, it is painted paintWidth cells over the slot's right edge while dockedWidth — the one
// width the slot and the frame key read (ADR 0110 D6) — stays 0.
type panelGeometry struct {
	// window is the window's width in cells.
	window int
	// show is the show mode the clamp reads its gate floor from.
	show domain.SidePanelShow
	// isShown reports whether the column docks: the window pays for the show mode's gate floor, the
	// gate's column width and the scroll-bar column ([Model.panelShown]).
	isShown bool
	// isOverlayable reports whether the column can open as the narrow-window overlay: the window is
	// under the gate, and the mode draws the column at all — `off` and the empty mode never do.
	isOverlayable bool
	// isOverlaid reports whether the column is painted as the overlay: asked for and overlayable.
	isOverlaid bool
	// dockedWidth is the docked column's width, its separator included — the configured width clamped
	// to the window ([panelGeometry.clamp]) while it shows, 0 otherwise, the overlay included.
	dockedWidth int
	// paintWidth is the columns the column is painted across, its separator included: the docked
	// width, or under the overlay the configured width clamped to the window alone.
	paintWidth int
	// chrome is the painted column's horizontal chrome around its content ([panelChrome]).
	chrome panelChrome
	// isOverlayGapHung reports whether the overlay's blank column hangs left of the painted column —
	// the window has a column to spare beside it — rather than coming out of its own width.
	isOverlayGapHung bool
	// contentWidth is the painted column less its chrome, 0 while it is not painted.
	contentWidth int
	// pinX is the window column of a header's pin glyph: the painted column's first content cell.
	pinX int
	// slotWidth is the transcript slot's width: the window less the docked width.
	slotWidth int
}

// newPanelGeometry builds the side panel's geometry from in. The overlay is gated here: an
// overlay asked for on a window the column docks in (or a mode that never draws it) is not granted,
// so the docked column keeps its paint width, no inset and a live grip.
func newPanelGeometry(in panelGeometryInput) panelGeometry {
	least, drawsColumn := sidePanelLeastWindow(in.show)
	geometry := panelGeometry{
		window:        in.window,
		show:          in.show,
		isShown:       drawsColumn && in.window >= least,
		isOverlayable: drawsColumn && in.window < least,
	}
	geometry.isOverlaid = in.isOverlaid && geometry.isOverlayable
	if geometry.isShown {
		geometry.dockedWidth = geometry.clamp(in.width.Resolve(), in.width.IsStated())
	}
	geometry.slotWidth = in.window - geometry.dockedWidth
	geometry.paintWidth = geometry.dockedWidth
	if geometry.isOverlaid {
		geometry.paintWidth = min(in.width.Resolve(), in.window)
	}
	geometry.isOverlayGapHung = geometry.isOverlaid && in.window-geometry.paintWidth >= 1
	geometry.chrome = newPanelChrome(
		geometry.paintWidth,
		geometry.isOverlaid && !geometry.isOverlayGapHung,
		in.fullRowWidth,
	)
	geometry.contentWidth = max(0, geometry.paintWidth-geometry.chrome.left()-geometry.chrome.margin)
	geometry.pinX = in.window - geometry.paintWidth + geometry.chrome.left()
	return geometry
}

// newPanelChrome is the chrome of a column painted width cells wide, with the overlay's blank
// column inside it where hasInset. Where the content holds a full sub-agent row (fullRowWidth) the
// gutter, the margin and the body indent are their full width; narrower, they yield a cell at a
// time before any fact is cut: the right margin first, then the gutter's extra cell, down to the one
// cell the cursor's ▌ stands in, then the body indent, down to nothing.
func newPanelChrome(width int, hasInset bool, fullRowWidth int) panelChrome {
	chrome := panelChrome{gutter: sidePanelGutterWidth, margin: sidePanelMarginWidth, indent: sidePanelBodyIndentWidth}
	if hasInset {
		chrome.inset = 1
	}
	short := fullRowWidth - (width - chrome.left() - chrome.margin)
	marginYield := min(max(0, short), sidePanelMarginWidth)
	chrome.margin -= marginYield
	gutterYield := min(max(0, short-marginYield), sidePanelGutterWidth-sidePanelGutterLeast)
	chrome.gutter -= gutterYield
	chrome.indent -= min(max(0, short-marginYield-gutterYield), sidePanelBodyIndentWidth)
	return chrome
}

// clamp is width held to the docked column's limits in this window (adr-0110-amend-1108 (a)): at
// least [domain.MinSidePanelWidth], at most the window less the transcript it leaves and the
// scroll-bar column. A set width — one the user dragged or wrote — leaves the transcript
// [sidePanelTranscriptLeast]; an unset one leaves the show mode's gate floor
// ([sidePanelTranscriptFloor]), so the default width docks as it did before a width could be set
// narrower than the gate. The docked width and a separator drag, which always clamps as set, read
// it, so the drag stops exactly where the paint does.
func (g panelGeometry) clamp(width int, isSet bool) int {
	least := sidePanelTranscriptLeast
	if !isSet {
		least, _ = sidePanelTranscriptFloor(g.show)
	}
	return min(max(width, domain.MinSidePanelWidth), g.window-least-scrollbarWidth)
}

// onColumn reports whether window column x is within the painted column: right of its left edge,
// the overlay's inset blank excluded, and left of the window's right edge.
func (g panelGeometry) onColumn(x int) bool {
	left := g.window - g.paintWidth + g.chrome.inset
	return g.paintWidth > 0 && x >= left && x < g.window
}

// cell reports whether (x, y) is one of the column's cells: on the painted column ([panelGeometry.onColumn])
// and on a row above slotBottom — the session-name rule's row of the frame the caller reads.
func (g panelGeometry) cell(x, y, slotBottom int) bool {
	return g.onColumn(x) && y >= 0 && y < slotBottom
}

// gripAt answers whether window column x of a press already on the column is its grip — the
// separator or the one cell right of it ([sidePanelGripWidth]), whatever the gutter yields, so the
// lead pin cell is never one — and how far right of the separator it is. The overlay has no grip
// (ADR 0111 D7): the separator belongs to the docked column, and an overlay the window cannot dock
// has no width a drag could set.
func (g panelGeometry) gripAt(x int) (grab int, isGrip bool) {
	grab = x - g.slotWidth
	if g.isOverlaid || grab < 0 || grab >= sidePanelGripWidth {
		return 0, false
	}
	return grab, true
}

// panelFrame is the side panel as one frame paints and walks it: its geometry, the sections painted
// once at the geometry's content width, their layout over the rows above the session-name rule, and
// the stops laid out over that same layout. View builds one per frame and paints the column and the
// cursor's row from it ([Model.sidePanelColumnIn], [Model.panelCursorRowIn]), so the sections are
// painted once a frame and the stops land on the rows the paint put them on. It is built on a
// per-Update copy and never held across Updates (ADR 0011), and its slices are never written after
// [Model.newPanelFrame] returns.
type panelFrame struct {
	// geometry is the horizontal geometry the sections were painted at.
	geometry panelGeometry
	// sections is the shown sections, painted at geometry's content width ([Model.panelSectionsIn]).
	sections []panelSection
	// layout is sections laid out over rows by the overflow rule ([panelLayout]); nil when no section
	// is shown.
	layout []panelLine
	// rows is the column's height: the rows above the session-name rule.
	rows int
	// stops is the column's stops over layout, in paint order ([Model.layPanelStops]).
	stops []panelStop
}

// newPanelFrame builds the side panel's frame at geometry over rows rows — the row bound the caller
// read off the frame it composes (View's belowSlot, or the spans a click chain snapshots). It never
// composes a frame of its own ([Model.frameSpans]), so a width read builds none.
func (m Model) newPanelFrame(geometry panelGeometry, rows int) panelFrame {
	frame := panelFrame{geometry: geometry, sections: m.panelSectionsIn(geometry), rows: rows}
	if len(frame.sections) == 0 {
		return frame
	}
	frame.layout = panelLayout(frame.sections, rows)
	frame.stops = m.layPanelStops(frame.sections, frame.layout)
	return frame
}
