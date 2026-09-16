package main

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Segment is one stretch of the take between two beats, with the framing the storyboard gives
// it: Start and End are source times, Frame is the beat's framing, and Region is the zoom's
// target as fractional x, y, w, h of the frame (nil when the framing has no zoom).
type Segment struct {
	Beat   int
	Start  time.Duration
	End    time.Duration
	Frame  Framing
	Region []float64
}

// Size is the take's pixel geometry, as ffprobe reports it. The zoom's crop is written in
// literal source pixels because crop's out_w/out_h are evaluated once, at configure time.
type Size struct {
	Width  int
	Height int
}

// segmentsFrom turns resolved beats into the segments the render keeps: beats sorted by
// resolved time, each running to the next beat's start and the last to the take's end. The head
// before the first beat is dropped, which is how the launch is cut. Each segment carries its
// beat's framing and, for a zoom, the region it names.
func segmentsFrom(board *Storyboard, times []BeatTime, duration time.Duration) []Segment {
	framing := make(map[int]Framing, len(board.Beats))
	for _, beat := range board.Beats {
		framing[beat.ID] = beat.Frame
	}
	sorted := slices.Clone(times)
	slices.SortStableFunc(sorted, func(a, b BeatTime) int { return cmp.Compare(a.At, b.At) })
	segments := make([]Segment, 0, len(sorted))
	for index, beat := range sorted {
		end := duration
		if index+1 < len(sorted) {
			end = sorted[index+1].At
		}
		segment := Segment{Beat: beat.ID, Start: beat.At, End: end, Frame: framing[beat.ID]}
		if zoom := segment.Frame.Zoom; zoom != nil {
			segment.Region = board.Regions[zoom.Region]
		}
		segments = append(segments, segment)
	}
	return segments
}

// The palette tail every render ends with, as the retired render.sh wrote it: a lanczos downscale to the
// shipped width, then a per-clip palette (much cleaner than ffmpeg's default 256-colour
// quantiser on flat terminal colours) applied with an ordered dither.
const (
	scaleFlags     = "lanczos"
	ditherMode     = "bayer"
	ditherScale    = 3
	sourceLabel    = "[0:v]"
	concatLabel    = "[v]"
	paletteLabel   = "[p]"
	splitLabelA    = "[a]"
	splitLabelB    = "[b]"
	pieceLabelBase = "s"
)

// Build writes the ffmpeg filtergraph that cuts the shipped GIF from the segments. Every
// segment becomes one or two `trim` pieces of the source — `hold` splits off its leading
// seconds at 1× and `speed` compresses the rest through `setpts` — a zoom pushes the pieces
// it spans through `scale…eval=frame` and a fixed-size `crop` about the region's centre, `cut`
// drops the segment, and the pieces `concat` into the palette tail. Segments must be in source
// order and disjoint; an empty segment (a beat on the take's last frame) contributes nothing.
func Build(segments []Segment, frame Frame, source Size) (string, error) {
	if err := checkSegments(segments); err != nil {
		return "", err
	}
	var pieces []string
	var labels []string
	for _, segment := range segments {
		if segment.Frame.Cut {
			continue
		}
		for _, piece := range segment.pieces() {
			label := fmt.Sprintf("[%s%d]", pieceLabelBase, len(labels))
			pieces = append(pieces, sourceLabel+piece.filters(segment, source)+label)
			labels = append(labels, label)
		}
	}
	if len(labels) == 0 {
		return "", errors.New("filtergraph: every segment is cut or empty")
	}
	var graph strings.Builder
	for _, piece := range pieces {
		graph.WriteString(piece)
		graph.WriteString(";")
	}
	fmt.Fprintf(&graph, "%sconcat=n=%d:v=1:a=0%s;", strings.Join(labels, ""), len(labels), concatLabel)
	fmt.Fprintf(&graph, "%sfps=%d,scale=%d:-1:flags=%s,split%s%s;",
		concatLabel, frame.FPS, frame.Width, scaleFlags, splitLabelA, splitLabelB)
	fmt.Fprintf(&graph, "%spalettegen=max_colors=%d%s;", splitLabelA, frame.MaxColors, paletteLabel)
	fmt.Fprintf(&graph, "%s%spaletteuse=dither=%s:bayer_scale=%d",
		splitLabelB, paletteLabel, ditherMode, ditherScale)
	return graph.String(), nil
}

// checkSegments rejects a segment that runs backwards or starts before the take, and any pair
// out of order or overlapping: the graph plays them back to back, so their order is the clip's.
func checkSegments(segments []Segment) error {
	for index, segment := range segments {
		if segment.Start < 0 {
			return fmt.Errorf("filtergraph: beat %d starts before the take (%s)", segment.Beat, segment.Start)
		}
		if segment.End < segment.Start {
			return fmt.Errorf("filtergraph: beat %d ends (%s) before it starts (%s)",
				segment.Beat, segment.End, segment.Start)
		}
		if index > 0 && segment.Start < segments[index-1].End {
			return fmt.Errorf("filtergraph: beat %d (%s) overlaps beat %d (ends %s)",
				segment.Beat, segment.Start, segments[index-1].Beat, segments[index-1].End)
		}
		if segment.Frame.Zoom != nil && len(segment.Region) != 4 {
			return fmt.Errorf("filtergraph: beat %d: zoom region %q is not a [x, y, w, h] rect",
				segment.Beat, segment.Frame.Zoom.Region)
		}
	}
	return nil
}

// piece is one trimmed stretch of a segment played at one rate.
type piece struct {
	Start time.Duration
	End   time.Duration
	Rate  float64
}

// pieces splits the segment at its hold: the leading `hold` seconds at 1×, the rest at the
// beat's speed. A segment at 1× throughout, or held for its whole length, is one piece. An
// empty segment yields none.
func (s Segment) pieces() []piece {
	length := s.End - s.Start
	if length <= 0 {
		return nil
	}
	hold := s.Frame.Hold
	rate := s.Frame.Rate()
	if rate == 1 || hold >= length {
		return []piece{{Start: s.Start, End: s.End, Rate: 1}}
	}
	if hold <= 0 {
		return []piece{{Start: s.Start, End: s.End, Rate: rate}}
	}
	return []piece{
		{Start: s.Start, End: s.Start + hold, Rate: 1},
		{Start: s.Start + hold, End: s.End, Rate: rate},
	}
}

// filters writes the piece's chain: the trim, the zoom when the piece overlaps the segment's
// zoom span, then the timestamp reset that concat needs, divided by the rate.
func (p piece) filters(segment Segment, source Size) string {
	chain := []string{fmt.Sprintf("trim=start=%s:end=%s", seconds(p.Start), seconds(p.End))}
	if zoom := segment.Frame.Zoom; zoom != nil && p.Start < segment.Start+zoom.span() {
		chain = append(chain, zoomFilters(segment, source)...)
	}
	if p.Rate == 1 {
		chain = append(chain, "setpts=PTS-STARTPTS")
	} else {
		chain = append(chain, fmt.Sprintf("setpts=(PTS-STARTPTS)/%s", number(p.Rate)))
	}
	return strings.Join(chain, ",")
}

// span is how long the zoom runs from the segment's start: in, hold and out together.
func (z Zoom) span() time.Duration { return z.In + z.Hold + z.Out }

// zoomFilters writes the push into the region: a per-frame scale by f(t), then a crop back to
// the source size whose origin follows the region's centre, so the centre stays put while the
// frame grows around it. The crop's size is fixed at configure time, which is why it is written
// in literal source pixels and only x and y move with t.
func zoomFilters(segment Segment, source Size) []string {
	factor := zoomFactor(segment)
	centreX := pixels((segment.Region[0] + segment.Region[2]/2) * float64(source.Width))
	centreY := pixels((segment.Region[1] + segment.Region[3]/2) * float64(source.Height))
	return []string{
		fmt.Sprintf("scale=w='iw*%s':h='ih*%s':eval=frame", factor, factor),
		fmt.Sprintf("crop=%d:%d:x='%s*(%s-1)':y='%s*(%s-1)'",
			source.Width, source.Height, number(centreX), factor, number(centreY), factor),
	}
}

// zoomFactor writes f(t), the magnification at source time t: 1 before the segment, ramping
// to the factor over `in`, held for `hold`, back to 1 over `out`, and 1 after. The ramps are the
// minimum of a rising and a falling line clipped to [0, 1]; a zero-length ramp is a step, so no
// expression ever divides by zero.
func zoomFactor(segment Segment) string {
	zoom := segment.Frame.Zoom
	start := seconds(segment.Start)
	rise := fmt.Sprintf("gte(t,%s)", start)
	if zoom.In > 0 {
		rise = fmt.Sprintf("(t-%s)/%s", start, seconds(zoom.In))
	}
	fallAt := seconds(segment.Start + zoom.In + zoom.Hold)
	fall := fmt.Sprintf("lt(t,%s)", fallAt)
	if zoom.Out > 0 {
		fall = fmt.Sprintf("1-(t-%s)/%s", fallAt, seconds(zoom.Out))
	}
	return fmt.Sprintf("(1+%s*clip(min(%s,%s),0,1))", number(zoom.Factor-1), rise, fall)
}

// pixels rounds a fractional pixel coordinate to a hundredth, below anything a frame can show
// and above the noise a product of fractions carries.
func pixels(f float64) float64 { return math.Round(f*100) / 100 }

// seconds spells a duration as ffmpeg reads it: decimal seconds, shortest exact form.
func seconds(d time.Duration) string { return number(d.Seconds()) }

// number spells a float in its shortest round-trip form.
func number(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
