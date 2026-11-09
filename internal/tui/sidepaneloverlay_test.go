package tui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/sidepanel"
)

// overlayHeader is a section title the side panel paints and the transcript of panelStopEvents never
// does, so finding it on a frame row finds the column.
const overlayHeader = "Changed files"

// overlayModel builds a ready model under show at width × 30 with `ui.side-panel.width` set to
// panelWidth (0 leaves it unset, the default), its panel on a clock fixed at panelNow and events
// folded through the view's own Event fold — navModel's fixture at a window the caller picks, where
// the column may be hidden under its gate.
func overlayModel(
	t *testing.T,
	show domain.SidePanelShow,
	width, panelWidth int,
	events ...domain.Event,
) Model {
	t.Helper()

	opts := testOpts
	opts.UI.SidePanelShow = show
	opts.UI.SidePanelSections = domain.DefaultSidePanelSections
	opts.UI.SidePanelWidth = panelWidth
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	m = step(t, m, tea.WindowSizeMsg{Width: width, Height: 30})
	m.panel = sidepanel.New(func() time.Time { return panelNow })
	for _, e := range events {
		m = m.foldEvent(e)
	}
	m.refreshViewport()
	if m.panelShown() {
		t.Fatalf("setup: the column docks at %d columns under %q", width, show)
	}
	return m
}

// frameColumnOf is the cell column a frame row above the session-name rule first paints text at, or
// −1 where no such row paints it.
func frameColumnOf(m Model, text string) int {
	for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if strings.HasPrefix(line, "▔") {
			return -1
		}
		if at := strings.Index(line, text); at >= 0 {
			return ansi.StringWidth(line[:at])
		}
	}
	return -1
}

// assertOverlayPainted fails the test unless m paints the overlay — overlaid, with a panel header
// on the frame at or right of the overlay's left edge — and returns the overlay's width.
func assertOverlayPainted(t *testing.T, m Model) int {
	t.Helper()
	width := m.panelPaintWidth()
	if !m.panelOverlaid() || width <= 0 {
		t.Fatalf("the overlay is not painted (overlaid %v, width %d)", m.panelOverlaid(), width)
	}
	if x := frameColumnOf(m, overlayHeader); x < m.width-width {
		t.Fatalf("the %q header paints at column %d, want at or right of the overlay's edge %d",
			overlayHeader, x, m.width-width)
	}
	return width
}

// overlaidModel is overlayModel's 100-column `auto` frame over panelStopEvents with navigation mode
// entered in the transcript and moved into the panel by →, the overlay asserted painted.
func overlaidModel(t *testing.T) Model {
	t.Helper()
	m := overlayModel(t, domain.SidePanelShowAuto, 100, 0, panelStopEvents(t)...)
	m = step(t, step(t, m, keyAltUp()), keyRight())
	assertOverlayPainted(t, m)
	return m
}

// TestOverlayOpensOnRightAtNarrowWidth is ADR 0111 D7's way in: on a window under the column's gate,
// → in navigation mode paints the panel over the transcript's right edge at its configured width
// clamped to the window — the transcript floor the docked column keeps is no input — with the cursor
// on a panel stop, and the slot and the docked width it reads stay the window's.
func TestOverlayOpensOnRightAtNarrowWidth(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		show       domain.SidePanelShow
		width      int
		panelWidth int
		want       int
	}{
		{"auto at 100 columns", domain.SidePanelShowAuto, 100, 0, domain.DefaultSidePanelWidth},
		{"on at 100 columns", domain.SidePanelShowOn, 100, 0, domain.DefaultSidePanelWidth},
		{"wider than the window: clamped to it", domain.SidePanelShowAuto, 40, 60, 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := overlayModel(t, tc.show, tc.width, tc.panelWidth, panelStopEvents(t)...)
			m = step(t, m, keyAltUp())
			transcriptCursorLine(t, m)
			if m.panelPaintWidth() != 0 || frameColumnOf(m, overlayHeader) >= 0 {
				t.Fatal("setup: the column is painted before →")
			}

			m = step(t, m, keyRight())

			panelCursorStop(t, m)
			if got := assertOverlayPainted(t, m); got != tc.want {
				t.Errorf("the overlay is %d columns wide, want %d", got, tc.want)
			}
			if m.panelWidth() != 0 || m.slotWidth() != m.width {
				t.Errorf("the docked width %d and slot %d moved under the overlay; want 0 and %d",
					m.panelWidth(), m.slotWidth(), m.width)
			}
		})
	}
}

// TestOverlayLeavesTranscriptLinesByteIdentical pins ADR 0110 D6 under the overlay: opening it
// re-wraps nothing — the transcript's wrapped lines and its viewport's width are what they were.
func TestOverlayLeavesTranscriptLinesByteIdentical(t *testing.T) {
	t.Parallel()
	m := overlayModel(t, domain.SidePanelShowAuto, 100, 0, panelStopEvents(t)...)
	m = step(t, m, keyAltUp())
	lines, width := slices.Clone(m.lines), m.viewport.Width()

	m = step(t, m, keyRight())

	assertOverlayPainted(t, m)
	if !slices.Equal(m.lines, lines) {
		t.Errorf("the transcript's wrapped lines changed under the overlay:\n%s\nwant\n%s",
			strings.Join(m.lines, "\n"), strings.Join(lines, "\n"))
	}
	if got := m.viewport.Width(); got != width {
		t.Errorf("the viewport is %d columns under the overlay, want %d", got, width)
	}
}

// TestOverlayHasNoGrip pins the overlay as unresizable (ADR 0111 D7): neither its separator nor the
// gutter beside it is a grip, and a press there followed by a drag starts no drag, moves nothing and
// writes no width.
func TestOverlayHasNoGrip(t *testing.T) {
	t.Parallel()
	m := overlaidModel(t)
	width := m.panelPaintWidth()
	separator := m.width - width
	for x := separator; x < separator+sidePanelChromeWidth; x++ {
		if _, isGrip := m.panelGripAt(x); isGrip {
			t.Errorf("column %d of the overlay is a grip", x)
		}
	}

	m = step(t, m, leftClick(separator, 0))
	isDragging := m.panelDrag.active
	m = step(t, m, leftDrag(separator-10, 0))
	m = step(t, m, leftRelease(separator-10, 0))

	if isDragging {
		t.Error("a press on the overlay's separator armed a drag")
	}
	if got := m.panelPaintWidth(); got != width || m.opts.UI.SidePanelWidth != 0 {
		t.Errorf("the drag left the overlay %d wide and the setting %d; want %d and unset",
			got, m.opts.UI.SidePanelWidth, width)
	}
}

// TestOverlayOffDoesNothing pins `off` (ADR 0111 D7): → in navigation mode is claimed, leaves the
// cursor on its transcript row and leaves the screen unchanged.
func TestOverlayOffDoesNothing(t *testing.T) {
	t.Parallel()
	m := overlayModel(t, domain.SidePanelShowOff, 100, 0, panelStopEvents(t)...)
	m = step(t, m, keyAltUp())
	line := transcriptCursorLine(t, m)
	before := m.View().Content

	next, _, claimed := m.blockCursorKey(keyRight())

	if !claimed {
		t.Error("→ was left to the prompt's caret")
	}
	if got := transcriptCursorLine(t, next); got != line {
		t.Errorf("→ moved the cursor to line %d, want it left on %d", got, line)
	}
	if next.panelPaintWidth() != 0 || next.View().Content != before {
		t.Error("→ under `off` changed the screen")
	}
}

// TestOverlayStopsMatchClickTargets pins the overlay's stops to its pointer (ADR 0111 D7: a click
// inside acts as on the docked column): every stop's target is the click at every row of its span —
// a header's pin stop's on the pin glyph's cell, its title stop's and a body stop's inside the
// overlay's content (adr-0111-amend-1109b) — a row no stop spans clicks nothing, and the transcript
// cell left of the overlay is no column cell.
func TestOverlayStopsMatchClickTargets(t *testing.T) {
	t.Parallel()
	pre := overlaidModel(t).withFrameSpans()
	rows := pre.frameSpans().slotBottom
	left := pre.width - pre.panelPaintWidth()
	bodyX := left + sidePanelChromeWidth + 2

	stops := pre.navPanelStops()

	if len(stops) == 0 {
		t.Fatal("the overlay offers no stops")
	}
	isSpanned := make([]bool, rows)
	for _, stop := range stops {
		x := bodyX
		if stop.id.isPin {
			x = pre.panelPinX()
		}
		for y := stop.y; y < stop.y+stop.height; y++ {
			isSpanned[y] = true
			if got, isPanel := pre.panelTargetAt(x, y); !isPanel || got != stop.target {
				t.Errorf("stop %+v: the click at (%d, %d) is %+v (column %v), want %+v", stop.id, x, y, got, isPanel, stop.target)
			}
		}
	}
	for y, spanned := range isSpanned {
		if got, _ := pre.panelTargetAt(bodyX, y); !spanned && got.kind != panelTargetNone {
			t.Errorf("row %d is no stop, yet a click there follows %+v", y, got)
		}
		if _, isPanel := pre.panelTargetAt(left-1, y); isPanel {
			t.Errorf("row %d: the transcript cell left of the overlay is claimed as a column cell", y)
		}
	}
}

// TestOverlayNotOpenedByAltArrows pins the overlay's only ways in as → and bare `/panel`: on a
// window under the gate, with no transcript stop and a column that would offer stops, ⌥↑ and ⌥↓ do
// not enter navigation mode in the hidden panel — they stay no-ops and paint nothing.
func TestOverlayNotOpenedByAltArrows(t *testing.T) {
	t.Parallel()
	m := overlayModel(t, domain.SidePanelShowAuto, 80, 0, taskList(0, "one", "two"), panelUsageReading(panelUsed))
	if stops := cursorStops(m.lineTargets, m.pads); len(stops) != 0 {
		t.Fatalf("setup: the transcript offers stops %v; want none", stops)
	}
	if !m.panelOverlayable() || len(m.panelStops(m.frameSpans().slotBottom)) == 0 {
		t.Fatal("setup: the hidden column could not overlay, or would offer no stop")
	}
	before := m.View().Content

	for _, key := range []tea.KeyPressMsg{keyAltUp(), keyAltDown()} {
		next := step(t, m, key)

		if next.cursor.active {
			t.Errorf("%s entered navigation mode in the hidden panel", key.String())
		}
		if next.panelPaintWidth() != 0 || next.View().Content != before {
			t.Errorf("%s painted the overlay", key.String())
		}
	}
}

// assertOverlayClosed fails the test unless m paints no overlay and navigation mode has left the
// panel region — ended, or (wantTranscript) standing on a transcript row.
func assertOverlayClosed(t *testing.T, m Model, wantTranscript bool) {
	t.Helper()
	if m.panelOverlaid() || m.panelPaintWidth() != 0 || frameColumnOf(m, overlayHeader) >= 0 {
		t.Fatalf("the overlay is still painted (overlaid %v, width %d)", m.panelOverlaid(), m.panelPaintWidth())
	}
	if m.cursor.inTranscript() != wantTranscript || (!wantTranscript && m.cursor.active) {
		t.Errorf("the cursor is active %v in region %v; want it in the transcript %v",
			m.cursor.active, m.cursor.region, wantTranscript)
	}
}

// TestOverlayClosesOnLeftEscAndOutsideClick pins the overlay's ways out (ADR 0111 D7): ← returns the
// cursor to the transcript row it left, or ends the mode where the transcript has no stop to return
// to; esc ends the mode; and a click outside the overlay ends it and goes on to what it landed on —
// here the transcript, whose selection it seats.
func TestOverlayClosesOnLeftEscAndOutsideClick(t *testing.T) {
	t.Parallel()

	t.Run("←", func(t *testing.T) {
		t.Parallel()
		m := overlayModel(t, domain.SidePanelShowAuto, 100, 0, panelStopEvents(t)...)
		m = step(t, m, keyAltUp())
		line := transcriptCursorLine(t, m)
		m = step(t, m, keyRight())
		assertOverlayPainted(t, m)

		m = step(t, m, keyLeft())

		assertOverlayClosed(t, m, true)
		if got := transcriptCursorLine(t, m); got != line {
			t.Errorf("← returned to line %d, want %d", got, line)
		}
	})
	t.Run("← with no transcript stop", func(t *testing.T) {
		t.Parallel()
		m := overlayModel(t, domain.SidePanelShowAuto, 100, 0, taskList(0, "one", "two"), panelUsageReading(panelUsed))
		if stops := cursorStops(m.lineTargets, m.pads); len(stops) != 0 {
			t.Fatalf("setup: the transcript offers stops %v; want none", stops)
		}
		m = sendLine(t, m, "/panel")
		panelCursorStop(t, m)
		if !m.panelOverlaid() {
			t.Fatal("setup: bare /panel did not open the overlay")
		}

		m = step(t, m, keyLeft())

		if m.cursor.active || m.panelOverlaid() {
			t.Errorf("← left the overlay open (active %v, overlaid %v)", m.cursor.active, m.panelOverlaid())
		}
	})
	t.Run("esc", func(t *testing.T) {
		t.Parallel()
		m := overlaidModel(t)

		m = step(t, m, keyEsc())

		assertOverlayClosed(t, m, false)
	})
	t.Run("a click outside", func(t *testing.T) {
		t.Parallel()
		m := overlaidModel(t)
		x := m.width - m.panelPaintWidth() - 1
		y := transcriptRowY(t, m)

		m = step(t, m, leftClick(x, y))

		assertOverlayClosed(t, m, false)
		if !m.transcriptSel.active {
			t.Error("the click outside was spent on closing the overlay; want it to seat the transcript's selection")
		}
	})
}

// transcriptRowY is the first screen row m's frame draws a transcript line on — the row a click
// outside the overlay lands on the transcript at.
func transcriptRowY(t *testing.T, m Model) int {
	t.Helper()
	pre := m.withFrameSpans()
	for y := range pre.frameSpans().slotBottom {
		if _, _, ok := pre.pointTranscriptRow(0, y); ok {
			return y
		}
	}
	t.Fatal("no screen row draws a transcript line")
	return -1
}

// TestOverlayInsideClickActsAsDocked pins a click inside the overlay to the docked column's (ADR 0111
// D7): a header's pin glyph pins its section and the overlay stays, the cursor in it; a body stop
// follows its target — the usage report opens /usage.
func TestOverlayInsideClickActsAsDocked(t *testing.T) {
	t.Parallel()

	t.Run("a pin glyph", func(t *testing.T) {
		t.Parallel()
		m := overlaidModel(t)
		stops := m.navPanelStops()
		at := slices.IndexFunc(stops, func(s panelStop) bool { return s.id.isPin && !s.target.isPinned })
		if at < 0 {
			t.Fatalf("setup: the overlay has no unpinned header among %+v", stops)
		}
		section := stops[at].id.section

		m = clickCell(t, m, m.panelPinX(), stops[at].y)

		if want := string(section) + ": pinned"; !strings.Contains(string(m.opts.UI.SidePanelSections), want) {
			t.Errorf("sections %q after the click; want %q", m.opts.UI.SidePanelSections, want)
		}
		assertOverlayPainted(t, m)
		panelCursorStop(t, m)
	})
	t.Run("the usage report", func(t *testing.T) {
		t.Parallel()
		m := overlaidModel(t)
		stops := m.navPanelStops()
		at := slices.IndexFunc(stops, func(s panelStop) bool { return s.target.kind == panelTargetUsage })
		if at < 0 {
			t.Fatalf("setup: the overlay has no usage stop among %+v", stops)
		}

		m = clickCell(t, m, m.width-m.panelPaintWidth()+sidePanelChromeWidth+2, stops[at].y)

		if !m.usagePane.open {
			t.Error("a click on the overlay's usage report did not open /usage")
		}
	})
}
