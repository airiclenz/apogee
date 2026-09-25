package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
)

// The zoom's auto-fit rule and its default ramps: with no factor given, the target box plus
// autoFitMarginCells on each side fills autoFitShare of the frame width, the factor clamped to
// [autoFitMin, autoFitMax]; a ramp the storyboard leaves at zero runs defaultZoomRamp.
const (
	autoFitMarginCells = 2
	autoFitShare       = 0.70
	autoFitMin         = 1.25
	autoFitMax         = 2.5
	defaultZoomRamp    = 1000 * time.Millisecond
)

// The cursor's look, in design pixels — pixels of a frame rasterized at designScale; a frame at
// another scale, and the downscale to the shipped width, multiply them through
// [Compositor.unit]. The dot is white at cursorDotOpacity inside a dark outline; each click grows
// a white ring from the dot's diameter to ringEndDiameter while it fades from ringStartOpacity.
const (
	designScale      = 2
	cursorDiameter   = 28.0
	cursorOutline    = 2.0
	cursorDotOpacity = 0.55
	ringEndDiameter  = 72.0
	ringStroke       = 3.0
	ringStartOpacity = 0.80
)

// The cursor's timing, on the output clock: it fades in over cursorFadeIn at the previous click
// point, glides to each click over cursorGlide ending at the press, rings for ringPulse after it,
// and starts fading out — over cursorFadeOut — cursorLinger after the beat's last click.
const (
	cursorFadeIn  = 400 * time.Millisecond
	cursorGlide   = 900 * time.Millisecond
	ringPulse     = 500 * time.Millisecond
	cursorLinger  = 1500 * time.Millisecond
	cursorFadeOut = 400 * time.Millisecond
)

// cursorOutlineColor is the dot's dark outline: Catppuccin Mocha's crust, darker than any
// background the TUI paints.
var cursorOutlineColor = color.RGBA{0x11, 0x11, 0x1b, 0xff}

// cursorColor is the dot's and the ring's fill.
var cursorColor = color.RGBA{0xff, 0xff, 0xff, 0xff}

// CellLayout is where the terminal's cells sit in a rasterized frame: the frame's pixel Size, the
// Padding around the grid, one cell's pixel size and the Scale the frame was rasterized at.
type CellLayout struct {
	Size       image.Point
	Padding    int
	CellWidth  int
	LineHeight int
	Scale      int
}

// pointF is a fractional pixel position.
type pointF struct{ X, Y float64 }

// rectF is a fractional pixel rectangle: its top-left corner and its size.
type rectF struct{ X, Y, W, H float64 }

// centre is the rectangle's middle.
func (r rectF) centre() pointF { return pointF{r.X + r.W/2, r.Y + r.H/2} }

// boxRect is the pixel rectangle a cell box covers.
func (l CellLayout) boxRect(b CellBox) rectF {
	return rectF{
		X: float64(l.Padding + b.X*l.CellWidth),
		Y: float64(l.Padding + b.Y*l.LineHeight),
		W: float64(b.W * l.CellWidth),
		H: float64(b.H * l.LineHeight),
	}
}

// cellCentre is the pixel centre of the cell at column x, row y.
func (l CellLayout) cellCentre(x, y int) pointF {
	return pointF{
		X: float64(l.Padding) + (float64(x)+0.5)*float64(l.CellWidth),
		Y: float64(l.Padding) + (float64(y)+0.5)*float64(l.LineHeight),
	}
}

// home is where the very first cursor appears: the frame's centre column, on the last row.
func (l CellLayout) home() pointF {
	return pointF{
		X: float64(l.Size.X) / 2,
		Y: float64(l.Size.Y-l.Padding) - float64(l.LineHeight)/2,
	}
}

// zoomPlan is one section's push into its target, on the output clock: the section's [Start,
// End), the ramps In and Out, the full Factor and the Centre the push aims at, in source pixels.
type zoomPlan struct {
	Start, End time.Duration
	In, Out    time.Duration
	Factor     float64
	Centre     pointF
}

// factorAt is the magnification at output time t: 1 outside the section, eased up over In from
// its start, held, and eased back to 1 over Out before its end. The ease runs on the factor's
// exponent, so each frame of a ramp scales by the same ratio at the same eased pace — a linear
// factor would rush the start of a push-in and crawl at its end.
func (z zoomPlan) factorAt(t time.Duration) float64 {
	if t < z.Start || t >= z.End {
		return 1
	}
	rise, fall := 1.0, 1.0
	if z.In > 0 {
		rise = easeInOut(float64(t-z.Start) / float64(z.In))
	}
	if z.Out > 0 {
		fall = easeInOut(float64(z.End-t) / float64(z.Out))
	}
	return math.Pow(z.Factor, min(rise, fall))
}

// clickMark is one click the cursor makes: its press on the output clock, the beat it belongs
// to and the clicked cell's centre in source pixels.
type clickMark struct {
	At    time.Duration
	Beat  int
	Point pointF
}

// cursorTrack is the cursor's whole path: every kept click in output order and the home point
// the first glide starts from.
type cursorTrack struct {
	Marks []clickMark
	Home  pointF
}

// position is where the cursor sits at output time t: at the previous click point (home before
// the first) until the glide to the next click starts, eased along the segment during it. A
// glide starts cursorGlide before its press, or at the previous press when they are closer.
func (c cursorTrack) position(t time.Duration) pointF {
	from := c.Home
	previous := time.Duration(math.MinInt64)
	for _, mark := range c.Marks {
		start := max(mark.At-cursorGlide, previous)
		if t < start {
			return from
		}
		if t < mark.At {
			return lerp(from, mark.Point, easeInOut(float64(t-start)/float64(mark.At-start)))
		}
		from, previous = mark.Point, mark.At
	}
	return from
}

// opacity is the cursor's visibility at output time t, 0 to 1: per beat that clicks, it rises
// over cursorFadeIn ending where the first glide starts, holds, and falls over cursorFadeOut
// starting cursorLinger after the beat's last press. Overlapping beats keep the brighter one.
func (c cursorTrack) opacity(t time.Duration) float64 {
	visible := 0.0
	for first := 0; first < len(c.Marks); {
		last := first
		for last+1 < len(c.Marks) && c.Marks[last+1].Beat == c.Marks[first].Beat {
			last++
		}
		shown := c.Marks[first].At - cursorGlide
		gone := c.Marks[last].At + cursorLinger
		rise := clamp01(float64(t-(shown-cursorFadeIn)) / float64(cursorFadeIn))
		fall := clamp01(float64(gone+cursorFadeOut-t) / float64(cursorFadeOut))
		visible = max(visible, min(rise, fall))
		first = last + 1
	}
	return visible
}

// ring is one click's pulse at output time t: its diameter in design pixels, its opacity and
// its centre in source pixels. ok is false outside every pulse; the latest press wins.
func (c cursorTrack) ring(t time.Duration) (diameter, opacity float64, centre pointF, ok bool) {
	for index := len(c.Marks) - 1; index >= 0; index-- {
		mark := c.Marks[index]
		if t < mark.At || t >= mark.At+ringPulse {
			continue
		}
		progress := float64(t-mark.At) / float64(ringPulse)
		diameter = cursorDiameter + (ringEndDiameter-cursorDiameter)*progress
		return diameter, ringStartOpacity * (1 - progress), mark.Point, true
	}
	return 0, 0, pointF{}, false
}

// Compositor turns each scheduled snapshot, rasterized, into a shipped frame: it pushes into
// the section's zoom target, scales to the shipped width and draws the click cursor on top at
// constant size. Everything it needs is fixed at construction, so composing is a pure function
// of the output time and the source frame.
type Compositor struct {
	layout CellLayout
	out    image.Point
	zooms  []zoomPlan
	cursor cursorTrack
}

// NewCompositor plans the zooms and the cursor of a clip: board's frame gives the shipped width
// and its beats the zooms, take the clicks and the snapshots targets resolve on, schedule the
// output clock and layout where cells sit in a source frame.
func NewCompositor(board *Storyboard, take *Take, schedule *Schedule, layout CellLayout) (*Compositor, error) {
	if layout.Size.X <= 0 || layout.Size.Y <= 0 || layout.CellWidth <= 0 || layout.LineHeight <= 0 {
		return nil, fmt.Errorf("compose: invalid cell layout %+v", layout)
	}
	width := board.Frame.Width
	if width <= 0 {
		return nil, fmt.Errorf("compose: frame.width: want >0, got %d", width)
	}
	c := &Compositor{
		layout: layout,
		out:    image.Pt(width, max(1, int(math.Round(float64(layout.Size.Y)*float64(width)/float64(layout.Size.X))))),
	}
	events, err := clickEvents(take)
	if err != nil {
		return nil, err
	}
	if c.zooms, err = planZooms(board, take, schedule, layout, events); err != nil {
		return nil, err
	}
	c.cursor = trackCursor(schedule, layout, events)
	return c, nil
}

// Size is the shipped frame's pixel size.
func (c *Compositor) Size() image.Point { return c.out }

// Compose builds the shipped frame at output time t from src, the rasterized snapshot the
// schedule shows then.
func (c *Compositor) Compose(t time.Duration, src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(image.Rectangle{Max: c.out})
	crop := c.crop(t)
	if crop.W >= float64(c.layout.Size.X) {
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	} else {
		draw.Draw(dst, dst.Bounds(), &image.Uniform{C: mochaBackground}, image.Point{}, draw.Src)
		sx, sy := float64(c.out.X)/crop.W, float64(c.out.Y)/crop.H
		transform := f64.Aff3{sx, 0, -crop.X * sx, 0, sy, -crop.Y * sy}
		draw.CatmullRom.Transform(dst, transform, src, src.Bounds(), draw.Src, nil)
	}
	c.drawCursor(dst, t, crop)
	return dst
}

// crop is the source rectangle shown at output time t: the whole frame when nothing zooms, else
// a frame-shaped rectangle 1/factor its size centred on the target and clamped inside the frame.
func (c *Compositor) crop(t time.Duration) rectF {
	full := rectF{W: float64(c.layout.Size.X), H: float64(c.layout.Size.Y)}
	for _, zoom := range c.zooms {
		factor := zoom.factorAt(t)
		if factor <= 1 {
			continue
		}
		w, h := full.W/factor, full.H/factor
		return rectF{
			X: math.Min(math.Max(zoom.Centre.X-w/2, 0), full.W-w),
			Y: math.Min(math.Max(zoom.Centre.Y-h/2, 0), full.H-h),
			W: w,
			H: h,
		}
	}
	return full
}

// toOutput maps a source pixel through the crop onto the shipped frame.
func (c *Compositor) toOutput(p pointF, crop rectF) pointF {
	return pointF{
		X: (p.X - crop.X) * float64(c.out.X) / crop.W,
		Y: (p.Y - crop.Y) * float64(c.out.Y) / crop.H,
	}
}

// unit is how many shipped pixels one design pixel spans: the source frame's scale against
// designScale, times the downscale to the shipped width. The zoom never enters it — the cursor
// keeps its size however far the frame pushes in.
func (c *Compositor) unit() float64 {
	scale := c.layout.Scale
	if scale <= 0 {
		scale = designScale
	}
	return float64(scale) / designScale * float64(c.out.X) / float64(c.layout.Size.X)
}

// drawCursor draws the click ring and then the dot at output time t, each positioned through
// the crop and sized in design pixels.
func (c *Compositor) drawCursor(dst *image.RGBA, t time.Duration, crop rectF) {
	unit := c.unit()
	if diameter, opacity, centre, ok := c.cursor.ring(t); ok {
		outer := diameter / 2 * unit
		paintAnnulus(dst, c.toOutput(centre, crop), outer-ringStroke*unit, outer, cursorColor, opacity)
	}
	opacity := c.cursor.opacity(t)
	if opacity <= 0 {
		return
	}
	centre := c.toOutput(c.cursor.position(t), crop)
	radius := cursorDiameter / 2 * unit
	inner := radius - cursorOutline*unit
	paintAnnulus(dst, centre, inner, radius, cursorOutlineColor, opacity)
	paintAnnulus(dst, centre, -1, inner, cursorColor, cursorDotOpacity*opacity)
}

// takeEngineEvent is one engine event of the take with its detail decoded.
type takeEngineEvent struct {
	At     time.Duration
	Kind   string
	Detail EngineEventDetail
}

// clickEvents decodes the take's target and click events, in take order.
func clickEvents(take *Take) ([]takeEngineEvent, error) {
	var events []takeEngineEvent
	for _, event := range take.Events {
		if event.Kind != EventTarget && event.Kind != EventClick {
			continue
		}
		var detail EngineEventDetail
		if err := json.Unmarshal([]byte(event.Detail), &detail); err != nil {
			return nil, fmt.Errorf("compose: %s event at %s: %w", event.Kind, event.At, err)
		}
		events = append(events, takeEngineEvent{At: event.At, Kind: event.Kind, Detail: detail})
	}
	return events, nil
}

// planZooms plans every kept section's zoom: its target box, the factor given or auto-fit, and
// its ramps — zero means defaultZoomRamp — shrunk in proportion when together they outrun the
// section.
func planZooms(board *Storyboard, take *Take, schedule *Schedule, layout CellLayout, events []takeEngineEvent) ([]zoomPlan, error) {
	sections := make(map[int]Section, len(schedule.Sections))
	for _, section := range schedule.Sections {
		sections[section.Beat] = section
	}
	var plans []zoomPlan
	for _, beat := range board.Beats {
		section, scheduled := sections[beat.ID]
		if beat.Zoom == nil || !scheduled || section.Cut {
			continue
		}
		box, err := zoomBox(beat, section, take, events)
		if err != nil {
			return nil, fmt.Errorf("compose: beat %d: zoom: %w", beat.ID, err)
		}
		rect := layout.boxRect(box)
		plan := zoomPlan{
			Start:  section.OutStart,
			End:    section.OutStart + section.Duration,
			In:     cmp.Or(beat.Zoom.In, defaultZoomRamp),
			Out:    cmp.Or(beat.Zoom.Out, defaultZoomRamp),
			Factor: beat.Zoom.Factor,
			Centre: rect.centre(),
		}
		if plan.Factor == 0 {
			plan.Factor = autoFitFactor(box, layout)
		}
		if ramps := plan.In + plan.Out; ramps > section.Duration {
			share := float64(section.Duration) / float64(ramps)
			plan.In = time.Duration(float64(plan.In) * share)
			plan.Out = time.Duration(float64(plan.Out) * share)
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

// zoomBox finds the beat's zoom target: the box a click of the same beat resolved on the same
// target, as the take logged it, else the target resolved on the beat's snapshots — the latest
// one it is on, so the push lands where the beat settles.
func zoomBox(beat Beat, section Section, take *Take, events []takeEngineEvent) (CellBox, error) {
	for _, event := range events {
		detail := event.Detail
		if event.Kind != EventTarget || detail.Beat != beat.ID || detail.Box == nil ||
			detail.Action < 0 || detail.Action >= len(beat.Do) {
			continue
		}
		if click := beat.Do[detail.Action].Click; click != nil && click.Target == beat.Zoom.Target {
			return *detail.Box, nil
		}
	}
	var lastErr error
	for index := len(take.Snapshots) - 1; index >= 0; index-- {
		snapshot := take.Snapshots[index]
		if snapshot.At >= section.End && section.End > section.Start {
			continue
		}
		box, err := resolveTarget(snapshot, beat.Zoom.Target)
		if err == nil {
			return box, nil
		}
		lastErr = err
		// The snapshot in effect at the beat's start is the earliest the beat shows.
		if snapshot.At <= section.Start {
			break
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("target /%s/: the take has no snapshot in the beat", beat.Zoom.Target.Text)
	}
	return CellBox{}, lastErr
}

// autoFitFactor is the magnification at which box, with autoFitMarginCells either side, spans
// autoFitShare of the frame width, clamped to [autoFitMin, autoFitMax].
func autoFitFactor(box CellBox, layout CellLayout) float64 {
	span := float64((box.W + 2*autoFitMarginCells) * layout.CellWidth)
	return math.Min(math.Max(autoFitShare*float64(layout.Size.X)/span, autoFitMin), autoFitMax)
}

// trackCursor collects the take's clicks onto the output clock. A click the schedule gives no
// output time — one inside a cut beat — is dropped: the clip never shows it.
func trackCursor(schedule *Schedule, layout CellLayout, events []takeEngineEvent) cursorTrack {
	track := cursorTrack{Home: layout.home()}
	for _, event := range events {
		if event.Kind != EventClick || event.Detail.Cell == nil {
			continue
		}
		at, ok := schedule.Output(event.At)
		if !ok {
			continue
		}
		cell := *event.Detail.Cell
		track.Marks = append(track.Marks, clickMark{At: at, Beat: event.Detail.Beat, Point: layout.cellCentre(cell[0], cell[1])})
	}
	return track
}

// paintAnnulus blends colour at opacity over every pixel between the radii inner and outer
// around centre, anti-aliased over one pixel at both edges. An inner radius below zero fills a
// disc.
func paintAnnulus(dst *image.RGBA, centre pointF, inner, outer float64, colour color.RGBA, opacity float64) {
	if opacity <= 0 || outer <= 0 {
		return
	}
	bounds := image.Rect(
		int(math.Floor(centre.X-outer-1)), int(math.Floor(centre.Y-outer-1)),
		int(math.Ceil(centre.X+outer+1)), int(math.Ceil(centre.Y+outer+1)),
	).Intersect(dst.Bounds())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			distance := math.Hypot(float64(x)+0.5-centre.X, float64(y)+0.5-centre.Y)
			coverage := clamp01(outer - distance + 0.5)
			if inner >= 0 {
				coverage = min(coverage, clamp01(distance-inner+0.5))
			}
			if coverage > 0 {
				blendPixel(dst, x, y, colour, coverage*opacity)
			}
		}
	}
}

// blendPixel lays colour over the pixel at (x, y) with the given alpha.
func blendPixel(dst *image.RGBA, x, y int, colour color.RGBA, alpha float64) {
	offset := dst.PixOffset(x, y)
	pixel := dst.Pix[offset : offset+4 : offset+4]
	for channel, value := range [3]uint8{colour.R, colour.G, colour.B} {
		pixel[channel] = uint8(math.Round(float64(pixel[channel])*(1-alpha) + float64(value)*alpha))
	}
	pixel[3] = 0xff
}

// easeInOut is the smootherstep ease over progress clamped to [0, 1]: speed and acceleration
// are both zero at either end, so a glide or a zoom gathers speed and settles rather than
// starting and stopping on a jolt.
func easeInOut(progress float64) float64 {
	p := clamp01(progress)
	return p * p * p * (p*(6*p-15) + 10)
}

// clamp01 clamps f to [0, 1].
func clamp01(f float64) float64 { return math.Min(math.Max(f, 0), 1) }

// lerp is the point share of the way from a to b.
func lerp(a, b pointF, share float64) pointF {
	return pointF{a.X + (b.X-a.X)*share, a.Y + (b.Y-a.Y)*share}
}
