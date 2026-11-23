package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
)

// geometryFacts is the projection of a panelGeometry the gate-and-clamp table asserts.
type geometryFacts struct {
	isShown       bool
	isOverlayable bool
	isOverlaid    bool
	dockedWidth   int
	paintWidth    int
	slotWidth     int
	inset         int
	isGripLive    bool
}

// factsOf projects geometry onto the facts the gate-and-clamp table names. The grip is read at the
// separator's own column, and only on a painted column: gripAt answers a press already on it.
func factsOf(geometry panelGeometry) geometryFacts {
	facts := geometryFacts{
		isShown:       geometry.isShown,
		isOverlayable: geometry.isOverlayable,
		isOverlaid:    geometry.isOverlaid,
		dockedWidth:   geometry.dockedWidth,
		paintWidth:    geometry.paintWidth,
		slotWidth:     geometry.slotWidth,
		inset:         geometry.chrome.inset,
	}
	if geometry.paintWidth > 0 {
		_, facts.isGripLive = geometry.gripAt(geometry.slotWidth)
	}
	return facts
}

// TestPanelGeometryGateAndClamp pins the show gate, the overlay gate and the width clamp: the column
// docks from the show mode's least window up, is overlayable below it, is overlaid only where it is
// overlayable — an overlay asked for above the gate keeps the docked paint width, no inset and a live
// grip — and its docked width is the configured one clamped to the transcript it leaves.
func TestPanelGeometryGateAndClamp(t *testing.T) {
	t.Parallel()

	autoLeast, _ := sidePanelLeastWindow(domain.SidePanelShowAuto)
	onLeast, _ := sidePanelLeastWindow(domain.SidePanelShowOn)
	unset := domain.SidePanelWidth{}
	stated := domain.StatedSidePanelWidth
	cases := []struct {
		name string
		in   panelGeometryInput
		want geometryFacts
	}{
		{
			name: "auto on a roomy window docks the default width",
			in:   panelGeometryInput{window: 160, show: domain.SidePanelShowAuto, width: unset},
			want: geometryFacts{isShown: true, dockedWidth: 44, paintWidth: 44, slotWidth: 116, isGripLive: true},
		},
		{
			name: "auto at its least window clamps an unset width to the gate floor",
			in:   panelGeometryInput{window: autoLeast, show: domain.SidePanelShowAuto, width: unset},
			want: geometryFacts{isShown: true, dockedWidth: 36, paintWidth: 36, slotWidth: autoLeast - 36, isGripLive: true},
		},
		{
			name: "on at its least window clamps an unset width to its own floor",
			in:   panelGeometryInput{window: onLeast, show: domain.SidePanelShowOn, width: unset},
			want: geometryFacts{isShown: true, dockedWidth: 36, paintWidth: 36, slotWidth: onLeast - 36, isGripLive: true},
		},
		{
			name: "a set width is kept where the transcript least allows it",
			in:   panelGeometryInput{window: autoLeast, show: domain.SidePanelShowAuto, width: stated(30)},
			want: geometryFacts{isShown: true, dockedWidth: 30, paintWidth: 30, slotWidth: autoLeast - 30, isGripLive: true},
		},
		{
			name: "a set width wider than the window allows clamps to the transcript least",
			in:   panelGeometryInput{window: 160, show: domain.SidePanelShowAuto, width: stated(100)},
			want: geometryFacts{isShown: true, dockedWidth: 99, paintWidth: 99, slotWidth: 61, isGripLive: true},
		},
		{
			name: "a set width under the minimum clamps up to it",
			in:   panelGeometryInput{window: 160, show: domain.SidePanelShowAuto, width: stated(10)},
			want: geometryFacts{
				isShown: true, dockedWidth: domain.MinSidePanelWidth, paintWidth: domain.MinSidePanelWidth,
				slotWidth: 160 - domain.MinSidePanelWidth, isGripLive: true,
			},
		},
		{
			name: "under the gate the column is overlayable and paints nothing",
			in:   panelGeometryInput{window: autoLeast - 1, show: domain.SidePanelShowAuto, width: unset},
			want: geometryFacts{isOverlayable: true, slotWidth: autoLeast - 1},
		},
		{
			name: "under the gate an overlay paints the configured width and keeps the slot whole",
			in:   panelGeometryInput{window: autoLeast - 1, show: domain.SidePanelShowAuto, width: unset, isOverlaid: true},
			want: geometryFacts{isOverlayable: true, isOverlaid: true, paintWidth: 44, slotWidth: autoLeast - 1},
		},
		{
			name: "an overlay wider than the window is clamped to it alone",
			in:   panelGeometryInput{window: 30, show: domain.SidePanelShowAuto, width: unset, isOverlaid: true},
			want: geometryFacts{isOverlayable: true, isOverlaid: true, paintWidth: 30, slotWidth: 30, inset: 1},
		},
		{
			name: "an overlay asked for above the gate keeps the docked column",
			in:   panelGeometryInput{window: 160, show: domain.SidePanelShowAuto, width: unset, isOverlaid: true},
			want: geometryFacts{isShown: true, dockedWidth: 44, paintWidth: 44, slotWidth: 116, isGripLive: true},
		},
		{
			name: "off neither docks nor overlays",
			in:   panelGeometryInput{window: 200, show: domain.SidePanelShowOff, width: unset, isOverlaid: true},
			want: geometryFacts{slotWidth: 200},
		},
		{
			name: "off on a narrow window neither docks nor overlays",
			in:   panelGeometryInput{window: 60, show: domain.SidePanelShowOff, width: unset, isOverlaid: true},
			want: geometryFacts{slotWidth: 60},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := factsOf(newPanelGeometry(tc.in))

			if got != tc.want {
				t.Errorf("geometry = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestPanelGeometryChromeYields pins the chrome's yield order on a column too narrow for a full
// sub-agent row: the right margin first, then the gutter down to its least cell, then the body
// indent, down to nothing — and the content width is always what the chrome leaves.
func TestPanelGeometryChromeYields(t *testing.T) {
	t.Parallel()

	const columnWidth = 40
	full := panelChrome{gutter: sidePanelGutterWidth, margin: sidePanelMarginWidth, indent: sidePanelBodyIndentWidth}
	roomyContent := columnWidth - full.left() - full.margin
	gutterYield := sidePanelGutterWidth - sidePanelGutterLeast
	cases := []struct {
		name  string
		short int
		want  panelChrome
	}{
		{"a full row fits: nothing yields", 0, full},
		{"one cell short: the margin yields one", 1, panelChrome{
			gutter: sidePanelGutterWidth, margin: sidePanelMarginWidth - 1, indent: sidePanelBodyIndentWidth,
		}},
		{"the margin's width short: the margin is gone", sidePanelMarginWidth, panelChrome{
			gutter: sidePanelGutterWidth, indent: sidePanelBodyIndentWidth,
		}},
		{"then the gutter yields to its least", sidePanelMarginWidth + gutterYield, panelChrome{
			gutter: sidePanelGutterLeast, indent: sidePanelBodyIndentWidth,
		}},
		{"then the body indent yields", sidePanelMarginWidth + gutterYield + 1, panelChrome{
			gutter: sidePanelGutterLeast, indent: sidePanelBodyIndentWidth - 1,
		}},
		{"far short: only the gutter's least is left", columnWidth, panelChrome{gutter: sidePanelGutterLeast}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			geometry := newPanelGeometry(panelGeometryInput{
				window:       160,
				show:         domain.SidePanelShowAuto,
				width:        domain.StatedSidePanelWidth(columnWidth),
				fullRowWidth: roomyContent + tc.short,
			})

			if geometry.chrome != tc.want {
				t.Errorf("chrome = %+v, want %+v", geometry.chrome, tc.want)
			}
			if want := columnWidth - tc.want.left() - tc.want.margin; geometry.contentWidth != want {
				t.Errorf("content width = %d, want %d", geometry.contentWidth, want)
			}
		})
	}
}

// TestPanelGeometryOverlayGapAndInset pins where the overlay's blank column stands: hung left of the
// painted column where the window has a column to spare, out of the column's own width (inset 1, no
// column cell) where the overlay is as wide as the window, and nowhere on the docked column.
func TestPanelGeometryOverlayGapAndInset(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		in            panelGeometryInput
		wantHung      bool
		wantInset     int
		wantFirstCell int
	}{
		{
			name:          "an overlay narrower than the window hangs its gap",
			in:            panelGeometryInput{window: 100, show: domain.SidePanelShowAuto, isOverlaid: true},
			wantHung:      true,
			wantFirstCell: 100 - domain.DefaultSidePanelWidth,
		},
		{
			name:          "an overlay as wide as the window insets its gap",
			in:            panelGeometryInput{window: 30, show: domain.SidePanelShowAuto, isOverlaid: true},
			wantInset:     1,
			wantFirstCell: 1,
		},
		{
			name:          "the docked column hangs no gap",
			in:            panelGeometryInput{window: 160, show: domain.SidePanelShowAuto},
			wantFirstCell: 160 - domain.DefaultSidePanelWidth,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			geometry := newPanelGeometry(tc.in)

			if geometry.isOverlayGapHung != tc.wantHung {
				t.Errorf("gap hung = %v, want %v", geometry.isOverlayGapHung, tc.wantHung)
			}
			if geometry.chrome.inset != tc.wantInset {
				t.Errorf("inset = %d, want %d", geometry.chrome.inset, tc.wantInset)
			}
			if geometry.onColumn(tc.wantFirstCell-1) || !geometry.onColumn(tc.wantFirstCell) {
				t.Errorf("the column's first cell is not %d", tc.wantFirstCell)
			}
		})
	}
}

// TestPanelGeometryGripAndPin pins the cells a press lands on: the grip is the separator and the
// cell right of it on the docked column and nothing on the overlay, the pin glyph stands at the
// painted column's first content cell, and a column cell is bounded by the window's right edge and
// the caller's row bound.
func TestPanelGeometryGripAndPin(t *testing.T) {
	t.Parallel()

	const slotBottom = 20
	docked := newPanelGeometry(panelGeometryInput{window: 160, show: domain.SidePanelShowAuto, fullRowWidth: 1})
	overlaid := newPanelGeometry(panelGeometryInput{
		window: 100, show: domain.SidePanelShowAuto, isOverlaid: true, fullRowWidth: 1,
	})
	separator := docked.slotWidth

	t.Run("the docked grip is the separator and the cell beside it", func(t *testing.T) {
		t.Parallel()

		for x, want := range map[int]bool{separator - 1: false, separator: true, separator + 1: true, separator + 2: false} {
			if _, got := docked.gripAt(x); got != want {
				t.Errorf("gripAt(%d) = %v, want %v", x, got, want)
			}
		}
		if grab, _ := docked.gripAt(separator + 1); grab != 1 {
			t.Errorf("grab right of the separator = %d, want 1", grab)
		}
	})
	t.Run("the overlay has no grip", func(t *testing.T) {
		t.Parallel()

		left := overlaid.window - overlaid.paintWidth
		for x := left; x < overlaid.window; x++ {
			if _, isGrip := overlaid.gripAt(x); isGrip {
				t.Errorf("gripAt(%d) on the overlay is a grip", x)
			}
		}
	})
	t.Run("the pin stands at the first content cell", func(t *testing.T) {
		t.Parallel()

		if want := separator + docked.chrome.left(); docked.pinX != want {
			t.Errorf("docked pin x = %d, want %d", docked.pinX, want)
		}
		left := overlaid.window - overlaid.paintWidth
		if want := left + overlaid.chrome.left(); overlaid.pinX != want {
			t.Errorf("overlay pin x = %d, want %d", overlaid.pinX, want)
		}
	})
	t.Run("a cell is on the column and above the row bound", func(t *testing.T) {
		t.Parallel()

		cells := []struct {
			x, y int
			want bool
		}{
			{separator, 0, true},
			{separator, slotBottom - 1, true},
			{separator, slotBottom, false},
			{separator, -1, false},
			{separator - 1, 0, false},
			{docked.window, 0, false},
		}
		for _, c := range cells {
			if got := docked.cell(c.x, c.y, slotBottom); got != c.want {
				t.Errorf("cell(%d, %d) = %v, want %v", c.x, c.y, got, c.want)
			}
		}
	})
}

// TestPanelWidthNeverComposesTheFrame pins the width reads as frame-free: the docked and painted
// widths, the gate, the slot's width and the frame key are read with a boxed pane up without
// rendering it — only the frame's own spans compose it. It reads the process-wide paneRenders
// counter, so it does not run in parallel.
func TestPanelWidthNeverComposesTheFrame(t *testing.T) {
	m := approvalPaneModel(t)
	m.opts.UI.SidePanelShow = domain.SidePanelShowAuto
	m = step(t, m, tea.WindowSizeMsg{Width: 160, Height: 40})
	if !m.panelShown() || !paneSpecs[panePrompt].open(m) {
		t.Fatal("the fixture does not show the side panel beside an open approval pane")
	}
	rendersSince := func() int64 {
		var total int64
		for p := range paneRenders {
			total += paneRenders[p].Swap(0)
		}
		return total
	}
	rendersSince()

	_ = m.panelShown()
	_ = m.panelWidth()
	_ = m.panelPaintWidth()
	_ = m.slotWidth()
	_ = m.frameKey()

	if got := rendersSince(); got != 0 {
		t.Errorf("the width reads rendered %d panes, want none", got)
	}
	_ = m.frameSpans()
	if rendersSince() == 0 {
		t.Error("frameSpans rendered no pane: the counter cannot see a composed frame")
	}
}

// TestPanelFrameStopsLandOnPaintedRows pins the frame's stops to the rows View paints: every stop of
// a frame built over View's own row bound sits on the painted row of the section it names — a
// header's two stops on the row its title paints on, a body stop on the row its body text paints on
// — and the panelStops delegate answers the same stops.
func TestPanelFrameStopsLandOnPaintedRows(t *testing.T) {
	t.Parallel()
	pre := panelModel(t, domain.DefaultSidePanelSections, 30, panelStopEvents(t)...).withFrameSpans()
	rows := pre.frameSpans().slotBottom

	frame := pre.newPanelFrame(pre.panelGeometry(), rows)

	if len(frame.stops) == 0 {
		t.Fatal("the frame lays out no stops")
	}
	painted := panelText(t, pre)
	for _, stop := range frame.stops {
		if stop.y >= len(painted) || stop.y >= len(frame.layout) {
			t.Fatalf("stop %+v sits on row %d, below the %d painted rows", stop.id, stop.y, len(painted))
		}
		line := frame.layout[stop.y]
		section := frame.sections[line.section]
		if section.kind != stop.id.section {
			t.Errorf("stop %+v sits on a row of the %s section", stop.id, section.kind)
		}
		want := section.title
		if !stop.id.isHeader {
			want = strings.TrimSpace(ansi.Strip(section.body[line.body]))
		}
		if !strings.Contains(painted[stop.y], want) {
			t.Errorf("stop %+v sits on row %d, painted %q, want it to paint %q", stop.id, stop.y, painted[stop.y], want)
		}
	}
	if delegated := pre.panelStops(rows); !slices.Equal(delegated, frame.stops) {
		t.Errorf("panelStops answers\n%+v\nwant the frame's\n%+v", delegated, frame.stops)
	}
}

// TestPanelFrameOverlaidPaintsAtOverlayWidth pins a frame built as overlaid on a window under the
// gate to the overlay's width: its sections and its column are painted across the overlay's content
// and paint widths, not the hidden docked column's nothing, and every header keeps its title.
func TestPanelFrameOverlaidPaintsAtOverlayWidth(t *testing.T) {
	t.Parallel()
	m := overlayModel(t, domain.SidePanelShowAuto, 100, domain.SidePanelWidth{}, panelStopEvents(t)...)
	if m.panelGeometry().contentWidth != 0 {
		t.Fatal("setup: the column is painted before the overlay is asked for")
	}

	frame := m.newPanelFrame(m.panelGeometryAs(true), 20)

	geometry := frame.geometry
	if !geometry.isOverlaid || geometry.contentWidth <= 0 {
		t.Fatalf("the frame is not overlaid (overlaid %v, content width %d)", geometry.isOverlaid, geometry.contentWidth)
	}
	content := m.sidePanelContentIn(frame)
	headers := 0
	for y, line := range frame.layout {
		if line.section < 0 || line.body >= 0 {
			continue
		}
		headers++
		if got := m.th.measure.Width(content[y]); got != geometry.contentWidth {
			t.Errorf("row %d: the header is %d cells wide, want the overlay's content width %d", y, got, geometry.contentWidth)
		}
		if title := frame.sections[line.section].title; !strings.Contains(ansi.Strip(content[y]), title) {
			t.Errorf("row %d: the header %q lost its title %q", y, ansi.Strip(content[y]), title)
		}
	}
	if headers == 0 {
		t.Fatal("the overlaid frame paints no header")
	}
	for y, row := range m.sidePanelColumnIn(frame) {
		if got := m.th.measure.Width(row); got != geometry.paintWidth {
			t.Errorf("column row %d is %d cells wide, want the overlay's paint width %d", y, got, geometry.paintWidth)
		}
	}
}

// TestPanelSectionsPaintOncePerView pins the sections to one paint per View — the paint and the
// cursor's row read one frame — whether the column docks with or without the navigation cursor in
// it, or is painted as the overlay. It reads the process-wide panelSectionPaints counter, so it does
// not run in parallel.
func TestPanelSectionsPaintOncePerView(t *testing.T) {
	docked := panelModel(t, domain.DefaultSidePanelSections, 30, panelStopEvents(t)...)
	inPanel := step(t, step(t, docked, keyAltUp()), keyRight())
	if !inPanel.cursor.inPanel() || !inPanel.blockCursorOwnsKeys() {
		t.Fatal("setup: → does not stand the navigation cursor in the docked column")
	}
	cases := []struct {
		name string
		m    Model
	}{
		{"docked", docked},
		{"docked with the cursor in the column", inPanel},
		{"overlaid", overlaidModel(t)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			panelSectionPaints.Store(0)

			_ = tc.m.View()

			if got := panelSectionPaints.Load(); got != 1 {
				t.Errorf("one View painted the sections %d times, want once", got)
			}
		})
	}
}
