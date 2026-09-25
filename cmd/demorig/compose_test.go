package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"
	"time"
)

// composeLayout is a small source frame: 49 × 14 cells of 20 × 40 pixels inside a 10-pixel
// padding, rasterized at the design scale and shipped at half its width.
var composeLayout = CellLayout{Size: image.Pt(1000, 600), Padding: 10, CellWidth: 20, LineHeight: 40, Scale: 2}

// composeWidth is the shipped width the compose tests use.
const composeWidth = 500

// engineEvent is an engine event of kind carrying detail, at source time at.
func engineEvent(t *testing.T, at time.Duration, kind string, detail EngineEventDetail) TakeEvent {
	t.Helper()
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return TakeEvent{At: at, Kind: kind, Detail: string(encoded)}
}

// clickAt is the target and click events of a one-click action on box in beat.
func clickAt(t *testing.T, at time.Duration, beat int, box CellBox) []TakeEvent {
	t.Helper()
	x, y := box.Center()
	return []TakeEvent{
		engineEvent(t, at, EventTarget, EngineEventDetail{Beat: beat, Form: "click", Box: &box}),
		engineEvent(t, at, EventClick, EngineEventDetail{Beat: beat, Form: "click", Cell: &[2]int{x, y}}),
	}
}

// newTestCompositor schedules the take over the board's beats and builds its compositor.
func newTestCompositor(t *testing.T, board *Storyboard, take *Take) (*Compositor, *Schedule) {
	t.Helper()
	board.Frame.Width = composeWidth
	spans, err := SpansFrom(board, take)
	if err != nil {
		t.Fatalf("SpansFrom: %v", err)
	}
	schedule := mustSchedule(t, spans...)
	compositor, err := NewCompositor(board, take, schedule, composeLayout)
	if err != nil {
		t.Fatalf("NewCompositor: %v", err)
	}
	return compositor, schedule
}

// zoomClickTake is a two-beat take: beat 1 idles for 2 s, beat 2 clicks box at 3 s and runs to
// 6 s. Every beat fits its duration, so source and output clocks agree.
func zoomClickTake(t *testing.T, box CellBox) (*Storyboard, *Take) {
	t.Helper()
	target := Target{Text: "target"}
	board := &Storyboard{Beats: []Beat{
		{ID: 1, Duration: 2 * sec},
		{
			ID: 2, Duration: 4 * sec,
			Do:   []Action{{Click: &ClickAction{Target: target}}},
			Zoom: &Zoom{Target: target, Factor: 2},
		},
	}}
	events := []TakeEvent{beatStart(t, 0, 1), beatStart(t, 2*sec, 2)}
	take := &Take{
		Snapshots: []Snapshot{{At: 0}, {At: 6 * sec}},
		Events:    append(events, clickAt(t, 3*sec, 2, box)...),
	}
	return board, take
}

// solidFrame is a source frame filled with colour.
func solidFrame(colour color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rectangle{Max: composeLayout.Size})
	draw.Draw(img, img.Bounds(), &image.Uniform{C: colour}, image.Point{}, draw.Src)
	return img
}

// centroidWhere is the mean position of the pixels (centres) matching keep, and how many did.
func centroidWhere(img *image.RGBA, keep func(color.RGBA) bool) (pointF, int) {
	var sum pointF
	count := 0
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			if keep(img.RGBAAt(x, y)) {
				sum.X += float64(x) + 0.5
				sum.Y += float64(y) + 0.5
				count++
			}
		}
	}
	if count == 0 {
		return pointF{}, 0
	}
	return pointF{sum.X / float64(count), sum.Y / float64(count)}, count
}

func TestComposeZoomCropStaysInsideTheFrame(t *testing.T) {
	t.Parallel()
	for _, box := range []CellBox{{X: 0, Y: 0, W: 3, H: 1}, {X: 45, Y: 13, W: 4, H: 1}} {
		t.Run(fmt.Sprintf("box at %d,%d", box.X, box.Y), func(t *testing.T) {
			t.Parallel()
			board, take := zoomClickTake(t, box)
			compositor, _ := newTestCompositor(t, board, take)

			crop := compositor.crop(4 * sec)
			if crop.W != 500 || crop.H != 300 {
				t.Errorf("crop at full zoom: want 500×300, got %g×%g", crop.W, crop.H)
			}
			if crop.X < 0 || crop.Y < 0 || crop.X+crop.W > 1000 || crop.Y+crop.H > 600 {
				t.Errorf("crop %+v leaves the 1000×600 frame", crop)
			}
			for _, at := range []time.Duration{2*sec + 200*time.Millisecond, 5*sec + 800*time.Millisecond} {
				ramp := compositor.crop(at)
				if ramp.W <= 500 || ramp.W >= 1000 || ramp.X < 0 || ramp.X+ramp.W > 1000 {
					t.Errorf("crop mid-ramp at %s: want inside the frame and between the factors, got %+v", at, ramp)
				}
			}
			if full := compositor.crop(1 * sec); full != (rectF{W: 1000, H: 600}) {
				t.Errorf("crop before the zoomed beat: want the whole frame, got %+v", full)
			}
			if got := compositor.Compose(4*sec, solidFrame(mochaBackground)).Bounds().Size(); got != image.Pt(500, 300) {
				t.Errorf("composed frame: want 500×300, got %v", got)
			}
		})
	}
}

func TestComposeCursorGlidesAlongTheSegment(t *testing.T) {
	t.Parallel()
	board := &Storyboard{Beats: []Beat{{ID: 1, Duration: 10 * sec}}}
	events := []TakeEvent{beatStart(t, 0, 1)}
	events = append(events, clickAt(t, 2*sec, 1, CellBox{X: 4, Y: 2, W: 1, H: 1})...)
	events = append(events, clickAt(t, 5*sec, 1, CellBox{X: 40, Y: 11, W: 1, H: 1})...)
	take := &Take{Snapshots: []Snapshot{{At: 0}, {At: 10 * sec}}, Events: events}
	compositor, _ := newTestCompositor(t, board, take)
	track := compositor.cursor

	from, to := composeLayout.cellCentre(4, 2), composeLayout.cellCentre(40, 11)
	mid := track.position(5*sec - cursorGlide/2)
	cross := (to.X-from.X)*(mid.Y-from.Y) - (to.Y-from.Y)*(mid.X-from.X)
	if math.Abs(cross) > 1e-6 {
		t.Errorf("mid-glide %+v: want it on the segment %+v → %+v", mid, from, to)
	}
	share := (mid.X - from.X) / (to.X - from.X)
	if math.Abs(share-0.5) > 1e-9 {
		t.Errorf("mid-glide share: want the eased half-way point 0.5, got %g", share)
	}
	if got := track.position(5 * sec); got != to {
		t.Errorf("at the press: want %+v, got %+v", to, got)
	}
	if got := track.position(2*sec - cursorGlide); got != composeLayout.home() {
		t.Errorf("before the first glide: want home %+v, got %+v", composeLayout.home(), got)
	}
	if got := track.opacity(2*sec - cursorGlide - cursorFadeIn); got != 0 {
		t.Errorf("before the fade-in: want invisible, got %g", got)
	}
	if got := track.opacity(2*sec - cursorGlide); got != 1 {
		t.Errorf("at the glide's start: want fully shown, got %g", got)
	}
	if got := track.opacity(5*sec + cursorLinger + cursorFadeOut); got != 0 {
		t.Errorf("after the fade-out: want invisible, got %g", got)
	}
}

func TestComposeRingGrowsLinearly(t *testing.T) {
	t.Parallel()
	board, take := zoomClickTake(t, CellBox{X: 20, Y: 6, W: 3, H: 1})
	compositor, _ := newTestCompositor(t, board, take)

	diameter, opacity, _, ok := compositor.cursor.ring(3*sec + ringPulse/2)
	if !ok || math.Abs(diameter-50) > 0.5 || math.Abs(opacity-0.4) > 0.01 {
		t.Errorf("ring half-way through its pulse: want ≈50 px at 40%%, got %g px at %g (shown %v)", diameter, opacity, ok)
	}
	if diameter, _, _, _ := compositor.cursor.ring(3 * sec); diameter != cursorDiameter {
		t.Errorf("ring at the press: want %g px, got %g", cursorDiameter, diameter)
	}
	if _, _, _, ok := compositor.cursor.ring(3*sec + ringPulse); ok {
		t.Error("ring after its pulse: want none")
	}
}

func TestComposeZoomedCursorMapsToTheZoomedTarget(t *testing.T) {
	t.Parallel()
	box := CellBox{X: 30, Y: 7, W: 1, H: 1}
	board, take := zoomClickTake(t, box)
	compositor, _ := newTestCompositor(t, board, take)
	press := 3 * sec

	// Where the zoom puts the target cell: paint it red and find it in the output, drawn by a
	// compositor with the same zoom and no clicks.
	marked := solidFrame(mochaBackground)
	rect := composeLayout.boxRect(box)
	draw.Draw(marked, image.Rect(int(rect.X), int(rect.Y), int(rect.X+rect.W), int(rect.Y+rect.H)),
		&image.Uniform{C: color.RGBA{0xff, 0, 0, 0xff}}, image.Point{}, draw.Src)
	noClicks := *compositor
	noClicks.cursor = cursorTrack{Home: composeLayout.home()}
	red := func(c color.RGBA) bool { return c.R > 0x80 && c.G < 0x60 }
	cell, count := centroidWhere(noClicks.Compose(press, marked), red)
	if count == 0 {
		t.Fatal("the zoomed target cell is not in the composed frame")
	}

	// Where the cursor lands at the press, over a blank frame.
	bright := func(c color.RGBA) bool { return c.G > mochaBackground.G+0x30 }
	cursor, count := centroidWhere(compositor.Compose(press, solidFrame(mochaBackground)), bright)
	if count == 0 {
		t.Fatal("no cursor drawn at the press")
	}
	if math.Hypot(cursor.X-cell.X, cursor.Y-cell.Y) > 1 {
		t.Errorf("cursor at the press: want it on the zoomed target cell %+v, got %+v", cell, cursor)
	}

	// Constant size: the dot's footprint does not grow with the zoom.
	wantRadius := cursorDiameter / 2 * compositor.unit()
	if limit := math.Pi * wantRadius * wantRadius * 1.3; float64(count) > limit {
		t.Errorf("cursor footprint: want at most %.0f pixels at constant size, got %d", limit, count)
	}
}

func TestComposeClickInsideACutBeatDrawsNoCursor(t *testing.T) {
	t.Parallel()
	board := &Storyboard{Beats: []Beat{
		{ID: 1, Duration: 2 * sec},
		{ID: 2, Cut: true, Do: []Action{{Click: &ClickAction{Target: Target{Text: "x"}}}}},
		{ID: 3, Duration: 2 * sec},
	}}
	events := []TakeEvent{beatStart(t, 0, 1), beatStart(t, 2*sec, 2)}
	events = append(events, clickAt(t, 3*sec, 2, CellBox{X: 20, Y: 6, W: 1, H: 1})...)
	events = append(events, beatStart(t, 4*sec, 3))
	take := &Take{Snapshots: []Snapshot{{At: 0}, {At: 6 * sec}}, Events: events}
	compositor, schedule := newTestCompositor(t, board, take)

	if len(compositor.cursor.Marks) != 0 {
		t.Fatalf("cursor marks: want the cut click dropped, got %+v", compositor.cursor.Marks)
	}
	blank := solidFrame(mochaBackground)
	for _, frame := range schedule.Frames(10) {
		composed := compositor.Compose(frame.Out, blank)
		if _, count := centroidWhere(composed, func(c color.RGBA) bool { return c != mochaBackground }); count != 0 {
			t.Fatalf("frame at %s: want nothing drawn over the background, got %d pixels", frame.Out, count)
		}
	}
}

func TestComposeZoomResolvesItsTargetOnTheSnapshot(t *testing.T) {
	t.Parallel()
	row := func(text string) []TakeCell {
		cells := make([]TakeCell, 49)
		for x := range cells {
			cells[x] = TakeCell{Rune: " ", Width: 1}
		}
		for x, r := range text {
			cells[x] = TakeCell{Rune: string(r), Width: 1}
		}
		return cells
	}
	grid := make([][]TakeCell, 14)
	for y := range grid {
		grid[y] = row("")
	}
	grid[3] = row("     ctx 42%")
	board := &Storyboard{Beats: []Beat{
		{ID: 1, Duration: 4 * sec, Zoom: &Zoom{Target: Target{Text: `ctx \d+%`}}},
	}}
	take := &Take{
		Snapshots: []Snapshot{{At: 0, Cells: grid}, {At: 4 * sec, Cells: grid}},
		Events:    []TakeEvent{beatStart(t, 0, 1)},
	}
	compositor, _ := newTestCompositor(t, board, take)

	if len(compositor.zooms) != 1 {
		t.Fatalf("zooms: want one, got %+v", compositor.zooms)
	}
	zoom := compositor.zooms[0]
	if want := composeLayout.boxRect(CellBox{X: 5, Y: 3, W: 7, H: 1}).centre(); zoom.Centre != want {
		t.Errorf("zoom centre: want the resolved box's %+v, got %+v", want, zoom.Centre)
	}
	if zoom.In != defaultZoomRamp || zoom.Out != defaultZoomRamp {
		t.Errorf("ramps: want the %s default, got in %s out %s", defaultZoomRamp, zoom.In, zoom.Out)
	}
	// Auto-fit: (7 + 4) cells × 20 px = 220 px filling 70 % of 1000 px is 3.18×, clamped to 2.5.
	if zoom.Factor != autoFitMax {
		t.Errorf("auto-fit factor: want %g, got %g", autoFitMax, zoom.Factor)
	}
}

func TestComposeAutoFitFactor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		width int
		want  float64
	}{
		{width: 6, want: 2.5},  // 700 / 200 = 3.5, clamped
		{width: 21, want: 1.4}, // 700 / 500
		{width: 36, want: 1.25},
	} {
		if got := autoFitFactor(CellBox{W: tc.width, H: 1}, composeLayout); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("auto-fit for %d cells: want %g, got %g", tc.width, tc.want, got)
		}
	}
	var problems []string
	Zoom{Target: Target{Text: "gauge"}}.validate(func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	})
	if len(problems) != 0 {
		t.Errorf("a zoom with no factor: want it accepted for auto-fit, got %v", problems)
	}
}

func TestEaseInOutSettlesAtBothEnds(t *testing.T) {
	t.Parallel()
	const h = 1e-4
	for _, edge := range []float64{0, 1} {
		if got := easeInOut(edge); got != edge {
			t.Errorf("ease(%g): want %g, got %g", edge, edge, got)
		}
	}
	if got := easeInOut(0.5); got != 0.5 {
		t.Errorf("ease(0.5): want the half-way point 0.5, got %g", got)
	}
	// Speed and acceleration both vanish at the ends: the second difference is O(h³), not O(h²).
	if got := easeInOut(2*h) - 2*easeInOut(h) + easeInOut(0); math.Abs(got) > h*h*1e-2 {
		t.Errorf("ease at its start: want no acceleration, got a second difference of %g", got)
	}
}

func TestZoomRampScalesGeometrically(t *testing.T) {
	t.Parallel()
	zoom := zoomPlan{Start: 0, End: 10 * sec, In: 2 * sec, Out: 2 * sec, Factor: 4}
	if got := zoom.factorAt(sec); math.Abs(got-2) > 1e-9 {
		t.Errorf("half-way up a 4× push: want the geometric middle 2, got %g", got)
	}
	if got := zoom.factorAt(5 * sec); got != 4 {
		t.Errorf("held: want 4, got %g", got)
	}
	if got := zoom.factorAt(0); got != 1 {
		t.Errorf("at the section's start: want 1, got %g", got)
	}
}
