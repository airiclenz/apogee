package tui

import (
	"context"
	"fmt"
	"image/color"
	"math"
	"strings"
	"testing"
	"time"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/charmbracelet/x/ansi"
	colorful "github.com/lucasb-eyer/go-colorful"
)

// ----------------------------------------------------------------------------
// The owned spinner animation (spinner.go)
// ----------------------------------------------------------------------------

// legacyClassicFrames are the eight glyphs the bubbles-widget spinner rendered, written out here
// rather than read off classicFrames: classic is a permanently supported style, so these literals
// are the guard that the look the owner has today cannot drift while the animation moves.
var legacyClassicFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// TestSpinnerClassicMatchesLegacyFrames proves the classic style is the pre-plan animation: the
// eight glyphs, in that order, cycling at eight, held for a tenth of a second each, one column
// wide.
func TestSpinnerClassicMatchesLegacyFrames(t *testing.T) {
	t.Parallel()

	s := newSpinnerAnim(SpinnerClassic, false)
	if got, want := s.interval(), time.Second/10; got != want {
		t.Errorf("classic interval = %v, want %v (the 10 fps it has always run at)", got, want)
	}

	// Two laps: the frame counter only ever grows, so the glyph must cycle rather than run off
	// the end of the frame list.
	for frame := 0; frame < 2*len(legacyClassicFrames); frame++ {
		s.frame = frame
		want := legacyClassicFrames[frame%len(legacyClassicFrames)]
		got := s.glyph()
		if got != want {
			t.Errorf("frame %d = %q, want %q", frame, got, want)
		}
		if width := ansi.StringWidth(got); width != 1 {
			t.Errorf("frame %d is %d columns wide, want 1", frame, width)
		}
	}
}

// TestSpinnerClassicUncolouredIsUnchanged is the regression guard for "the spinner shipping today
// stays available": an uncoloured classic frame is byte-identical to what the deleted bubbles
// widget rendered, which was its frame painted through a background-only style — no foreground,
// so the glyph keeps the terminal's own text colour.
func TestSpinnerClassicUncolouredIsUnchanged(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	legacy := lipgloss.NewStyle().Background(th.surface) // exactly how newModel styled the widget
	s := newSpinnerAnim(SpinnerClassic, false)
	for frame, glyph := range legacyClassicFrames {
		s.frame = frame
		if got, want := s.view(th), legacy.Render(glyph); got != want {
			t.Errorf("frame %d renders %q, want %q (the pre-plan spinner, byte for byte)", frame, got, want)
		}
	}
}

// TestSpinnerFrameWidth proves every implemented style paints exactly the number of terminal
// columns it declares, on every frame: the status line composes by concatenation, so a frame that
// is wider than its neighbours would shove the activity phrase sideways mid-run.
func TestSpinnerFrameWidth(t *testing.T) {
	t.Parallel()

	// Long enough to cover more than one lap of any style — and, once the breathing style lands,
	// more than one full breath.
	const sweep = 240

	for style, spec := range spinnerSpecs {
		t.Run(string(style), func(t *testing.T) {
			t.Parallel()

			s := newSpinnerAnim(style, false)
			for frame := 0; frame < sweep; frame++ {
				s.frame = frame
				if got := ansi.StringWidth(s.glyph()); got != spec.width {
					t.Errorf("frame %d of %s is %d columns wide, want the declared %d",
						frame, style, got, spec.width)
				}
			}
		})
	}
}

// TestSpinnerStyleFallsBackToClassic proves the renderer never shows a blank column: a style with
// no animation registered — the zero value, or a name this build parses but cannot yet paint —
// resolves to classic instead of an empty glyph.
func TestSpinnerStyleFallsBackToClassic(t *testing.T) {
	t.Parallel()

	for _, style := range []SpinnerStyle{"", SpinnerStyle("not-a-style")} {
		s := newSpinnerAnim(style, false)
		if got := s.glyph(); got != legacyClassicFrames[0] {
			t.Errorf("style %q frame 0 = %q, want classic's %q", style, got, legacyClassicFrames[0])
		}
		if got, want := s.interval(), classicInterval; got != want {
			t.Errorf("style %q interval = %v, want classic's %v", style, got, want)
		}
	}
}

// snakeFrames are the twelve glyphs the ring derivation must produce, in order. They live in the
// test rather than in spinner.go on purpose: snakeRing is the documentation of the shape and these
// literals are the guard that the shape actually renders as intended, so a slip in the ring table,
// the dot bits or the wrap would fail here instead of quietly changing the animation.
var snakeFrames = []string{
	"⠉⠹", "⠈⢹", "⠀⣹", "⢀⣸", "⣀⣰", "⣄⣠",
	"⣆⣀", "⣇⡀", "⣏⠀", "⡏⠁", "⠏⠉", "⠋⠙",
}

// snakeInterior is the four inner positions of the 4×4 dot grid — everything the ring is not. The
// grid holds sixteen positions and the ring is the other twelve, so "every lit dot is on the ring"
// is exactly "no lit dot is one of these". Written out here rather than derived from snakeRing so
// TestSnakeIsSixDotsOnTheRing checks the production table instead of restating it.
var snakeInterior = map[[2]int]bool{
	{1, 1}: true, {2, 1}: true, {1, 2}: true, {2, 2}: true,
}

// brailleBitPositions maps a cell mask's bits back onto the (column, row) each one lights, taken
// from the Unicode block's own dot numbering rather than read off brailleDotBits, so the tests that
// decode a rendered frame are independent of the table that encoded it.
var brailleBitPositions = map[rune][2]int{
	0x01: {0, 0}, 0x02: {0, 1}, 0x04: {0, 2}, 0x40: {0, 3},
	0x08: {1, 0}, 0x10: {1, 1}, 0x20: {1, 2}, 0x80: {1, 3},
}

// litDots decodes one rendered snake frame into the grid positions it lights, as global (col, row)
// with the left cell carrying cols 0–1 and the right cols 2–3.
func litDots(t *testing.T, glyph string) [][2]int {
	t.Helper()

	cells := []rune(glyph)
	if len(cells) != 2 {
		t.Fatalf("glyph %q is %d cells, want the two that form the 4x4 grid", glyph, len(cells))
	}
	var dots [][2]int
	for cell, r := range cells {
		mask := r - brailleBase
		if mask < 0 || mask > 0xFF {
			t.Fatalf("cell %d of %q is %U, outside the braille block", cell, glyph, r)
		}
		for bit, pos := range brailleBitPositions {
			if mask&bit != 0 {
				dots = append(dots, [2]int{cell*2 + pos[0], pos[1]})
			}
		}
	}
	return dots
}

// TestSnakeFrames pins the twelve glyphs the ring derivation produces, in order, and the rate that
// makes them one lap a second. It goes through newSpinnerAnim so the registry wiring is covered
// too: a style that derives its frames correctly but is registered at the wrong interval — or not
// registered at all, which falls back to classic — fails here.
func TestSnakeFrames(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	s := newSpinnerAnim(SpinnerSnake, false)
	if got, want := s.interval(), time.Second/12; got != want {
		t.Errorf("snake interval = %v, want %v (twelve positions, one lap a second)", got, want)
	}

	for frame, want := range snakeFrames {
		s.frame = frame
		got := s.glyph()
		if got != want {
			t.Errorf("frame %d = %q, want %q", frame, got, want)
		}
		if width := ansi.StringWidth(got); width != 2 {
			t.Errorf("frame %d is %d columns wide, want 2", frame, width)
		}
		if rendered := s.view(th); !strings.Contains(rendered, want) {
			t.Errorf("frame %d renders %q, which does not carry the glyph %q", frame, rendered, want)
		}
	}
}

// TestSnakeIsSixDotsOnTheRing proves the animation is the walk it claims to be rather than twelve
// glyphs that merely happen to look like one: every frame lights exactly six dots, and every one of
// them is on the ring. An interior dot would break the illusion of a single arc travelling the edge.
func TestSnakeIsSixDotsOnTheRing(t *testing.T) {
	t.Parallel()

	s := newSpinnerAnim(SpinnerSnake, false)
	// Two laps: the frame counter only grows, so the wrap has to hold as well as the first pass.
	for frame := 0; frame < 2*len(snakeRing); frame++ {
		s.frame = frame
		dots := litDots(t, s.glyph())
		if len(dots) != snakeLength {
			t.Errorf("frame %d lights %d dots, want the snake's %d", frame, len(dots), snakeLength)
		}
		for _, dot := range dots {
			if snakeInterior[dot] {
				t.Errorf("frame %d lights (%d,%d), which is inside the grid rather than on its ring",
					frame, dot[0], dot[1])
			}
		}
	}
}

// TestSnakeCycles proves the lap closes and never stalls: the twelve frames are all different — a
// repeat would read as the snake pausing — and frame 12 is frame 0 again.
func TestSnakeCycles(t *testing.T) {
	t.Parallel()

	s := newSpinnerAnim(SpinnerSnake, false)
	seen := make(map[string]int, len(snakeRing))
	for frame := 0; frame < len(snakeRing); frame++ {
		s.frame = frame
		glyph := s.glyph()
		if prev, dup := seen[glyph]; dup {
			t.Errorf("frame %d repeats frame %d's glyph %q — the snake would appear to stall",
				frame, prev, glyph)
		}
		seen[glyph] = frame
	}

	s.frame = 0
	first := s.glyph()
	s.frame = len(snakeRing)
	if got := s.glyph(); got != first {
		t.Errorf("frame %d = %q, want frame 0's %q — the lap must close", len(snakeRing), got, first)
	}
}

// glitterTroughFrame and glitterPeakFrame are where one breath bottoms out and tops out. The sine
// starts mid-rise at frame 0, so the peak is a quarter of a breath in and the trough three quarters —
// which is why reading the breath FROM the trough is what makes "swells, then falls" a statement
// about a whole period rather than about the arbitrary point the counter happens to start on.
const (
	glitterTroughFrame = 3 * glitterFramesPerBreath / 4
	glitterPeakFrame   = glitterFramesPerBreath / 4
)

// cellDots counts the dots one rendered braille cell lights, decoding the rune back through
// brailleBitPositions — the test's own copy of the block's dot numbering — so the count is
// independent of the population count production sorted the buckets with.
func cellDots(t *testing.T, r rune) int {
	t.Helper()

	mask := r - brailleBase
	if mask < 0 || mask > 0xFF {
		t.Fatalf("%U is outside the braille block", r)
	}
	dots := 0
	for bit := range brailleBitPositions {
		if mask&bit != 0 {
			dots++
		}
	}
	return dots
}

// TestGlitterDensityBreathes proves the breath is one clean swell and fall per period rather than
// jitter: read from its trough the density climbs to a solid eight dots and falls back to one, moving
// only in the direction of the arc it is on (rounding makes plateaus, never a reversal), and the next
// breath repeats it exactly. It also pins the floor at 1 — a 0-dot cell would blink the spinner out
// mid-run and read as a stall.
func TestGlitterDensityBreathes(t *testing.T) {
	t.Parallel()

	const breath = glitterFramesPerBreath
	if got := glitterDensity(glitterTroughFrame, breath); got != 1 {
		t.Errorf("density at the trough (frame %d) = %d, want 1 — the breath must not blank the cell",
			glitterTroughFrame, got)
	}
	if got := glitterDensity(glitterPeakFrame, breath); got != brailleDots {
		t.Errorf("density at the peak (frame %d) = %d, want the block's full %d",
			glitterPeakFrame, got, brailleDots)
	}

	peak := glitterTroughFrame + breath/2 // the same phase as glitterPeakFrame, one breath on
	for frame := glitterTroughFrame; frame < peak; frame++ {
		if next, cur := glitterDensity(frame+1, breath), glitterDensity(frame, breath); next < cur {
			t.Fatalf("density fell %d→%d over frames %d→%d, inside the swell", cur, next, frame, frame+1)
		}
	}
	for frame := peak; frame < glitterTroughFrame+breath; frame++ {
		if next, cur := glitterDensity(frame+1, breath), glitterDensity(frame, breath); next > cur {
			t.Fatalf("density rose %d→%d over frames %d→%d, inside the fall", cur, next, frame, frame+1)
		}
	}

	for frame := 0; frame < breath; frame++ {
		got, want := glitterDensity(frame+breath, breath), glitterDensity(frame, breath)
		if got != want {
			t.Errorf("frame %d of the next breath = %d, want frame %d's %d — the breath must close",
				frame+breath, got, frame, want)
		}
	}
}

// TestGlitterCellMatchesDensity proves the breath actually governs the glyph across several breaths:
// both cells of every frame light exactly the number of dots that frame's density calls for. A pick
// from the wrong bucket would still sparkle but would not breathe. It goes through newSpinnerAnim so
// the registry wiring is covered too — an unregistered style falls back to classic's single cell and
// fails the cell count.
func TestGlitterCellMatchesDensity(t *testing.T) {
	t.Parallel()

	s := newSpinnerAnim(SpinnerGlitter, false)
	for frame := 0; frame < 3*glitterFramesPerBreath; frame++ {
		s.frame = frame
		cells := []rune(s.glyph())
		if len(cells) != glitterCells {
			t.Fatalf("frame %d is %d cells, want the %d that glitter paints", frame, len(cells), glitterCells)
		}
		want := glitterDensity(frame, glitterFramesPerBreath)
		for cell, r := range cells {
			if got := cellDots(t, r); got != want {
				t.Errorf("frame %d cell %d (%U) lights %d dots, want the breath's %d", frame, cell, r, got, want)
			}
		}
	}

	// The peak's bucket holds only ⣿, so the swell tops out on a solid pair — the effect's visual
	// anchor, and the proof the density really does reach the top of the block.
	s.frame = glitterPeakFrame
	if got, want := s.glyph(), "⣿⣿"; got != want {
		t.Errorf("the peak frame %d = %q, want the solid %q", glitterPeakFrame, got, want)
	}
}

// TestGlitterIsPure proves the glyph is a pure function of the frame, which is what lets the
// animation ride on a value-copied Model (ADR 0011): rendering the same frame twice, or rendering it
// from a second independent copy, gives the same cells. A *rand.Rand here would advance per render
// and per copy, so the spinner would depend on how often View happened to run.
func TestGlitterIsPure(t *testing.T) {
	t.Parallel()

	first := newSpinnerAnim(SpinnerGlitter, false)
	second := newSpinnerAnim(SpinnerGlitter, false)
	for frame := 0; frame < glitterFramesPerBreath; frame++ {
		first.frame, second.frame = frame, frame
		once, twice := first.glyph(), first.glyph()
		if once != twice {
			t.Fatalf("frame %d rendered %q then %q — the glyph is not pure in the frame", frame, once, twice)
		}
		if other := second.glyph(); other != once {
			t.Fatalf("frame %d rendered %q on one copy and %q on another", frame, once, other)
		}
	}
}

// TestGlitterSparkles guards the requirement that separates glitter from a pulse: the glyph re-rolls
// every frame, so a short run of frames shows many different cells even while the density barely
// moves. This is the test that fails if the effect ever regresses to a static or slowly-changing
// pair — the specific thing the owner asked to avoid. It pins the slow half of the contrast too, so a
// breath cannot quietly shorten to the point where the two rates stop contrasting.
func TestGlitterSparkles(t *testing.T) {
	t.Parallel()

	const (
		window     = 20 // consecutive frames — one second at glitter's rate
		wantGlyphs = 10
	)

	s := newSpinnerAnim(SpinnerGlitter, false)
	if got, want := s.interval(), time.Second/20; got != want {
		t.Errorf("glitter interval = %v, want %v (the fast half of the contrast)", got, want)
	}
	if got, want := time.Duration(glitterFramesPerBreath)*s.interval(), 6*time.Second; got != want {
		t.Errorf("one breath spans %v at the registered rate, want %v (the slow half)", got, want)
	}

	seen := make(map[string]bool, window)
	for frame := 0; frame < window; frame++ {
		s.frame = frame
		if dots := glitterDensity(frame, glitterFramesPerBreath); dots < 2 {
			t.Fatalf("frame %d breathes at %d dots — too sparse a bucket to prove a re-roll", frame, dots)
		}
		seen[s.glyph()] = true
	}
	if len(seen) < wantGlyphs {
		t.Errorf("%d consecutive frames produced only %d distinct glyphs, want at least %d — the spinner would read as a pulse, not as glitter",
			window, len(seen), wantGlyphs)
	}
}

// oklabDistance measures how far apart two rendered colours are perceptually, decoding them back
// into Oklab — the space the loop is blended in — so "soft" and "visits its stops" are judged in the
// terms the gradient was built in rather than in raw sRGB, where equal steps do not look equal.
func oklabDistance(t *testing.T, first, second color.Color) float64 {
	t.Helper()

	from, ok := colorful.MakeColor(first)
	if !ok {
		t.Fatalf("%v is not a convertible colour — a transparent spinner would render as nothing", first)
	}
	to, ok := colorful.MakeColor(second)
	if !ok {
		t.Fatalf("%v is not a convertible colour — a transparent spinner would render as nothing", second)
	}
	l1, a1, b1 := from.OkLab()
	l2, a2, b2 := to.OkLab()
	return math.Sqrt((l1-l2)*(l1-l2) + (a1-a2)*(a1-a2) + (b1-b2)*(b1-b2))
}

const (
	// spinnerStopTolerance is how close the loop has to pass to each palette stop, in Oklab. It is
	// not zero because a stop lands exactly on a frame only when the lap's frame count divides by
	// the number of stops (120, 200 and 100 all do at the current period, but a retimed period need
	// not), and because every frame is quantised to a hex triple on its way out. The measured worst
	// case is 0.0002.
	spinnerStopTolerance = 0.01

	// spinnerSoftStep is the largest Oklab step allowed between consecutive frames — the "soft" in
	// the requirement. The measured worst case is 0.023, on classic's 100-frame lap (the coarsest of
	// the three rates); the stops themselves sit ~0.23 apart, so a stop list with a discontinuity or
	// a seam at the wrap overshoots this by an order of magnitude.
	spinnerSoftStep = 0.03
)

// TestSpinnerColorLoops proves the colour path is a closed loop through the palette rather than a
// sweep that snaps back: the frame after a full lap is the frame the lap started on, and the loop
// really does visit all three stops on the way round.
func TestSpinnerColorLoops(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	for _, style := range spinnerStyleNames {
		t.Run(string(style), func(t *testing.T) {
			t.Parallel()

			loop := newSpinnerAnim(style, true).framesPerColorLoop()
			if got, want := th.spinnerColor(loop, loop), th.spinnerColor(0, loop); got != want {
				t.Errorf("frame %d is %v, want frame 0's %v — the loop must close on itself", loop, got, want)
			}

			for i, stop := range th.spinnerStops {
				closest := math.MaxFloat64
				for frame := 0; frame < loop; frame++ {
					if d := oklabDistance(t, th.spinnerColor(frame, loop), stop); d < closest {
						closest = d
					}
				}
				if closest > spinnerStopTolerance {
					t.Errorf("the lap's closest approach to stop %d (%s) is %.4f, want within %.4f — the loop must actually reach its palette stops",
						i, stop.Hex(), closest, spinnerStopTolerance)
				}
			}
		})
	}
}

// TestSpinnerColorIsSoft is the "soft" in the requirement: no two consecutive frames are far enough
// apart to read as a change of colour rather than a drift. Sweeping one frame past the lap covers
// the step across the wrap, which is where a stop list that does not close would show a seam.
func TestSpinnerColorIsSoft(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	for _, style := range spinnerStyleNames {
		t.Run(string(style), func(t *testing.T) {
			t.Parallel()

			loop := newSpinnerAnim(style, true).framesPerColorLoop()
			for frame := 0; frame < loop; frame++ {
				step := oklabDistance(t, th.spinnerColor(frame, loop), th.spinnerColor(frame+1, loop))
				if step > spinnerSoftStep {
					t.Errorf("frames %d→%d jump %.4f in Oklab, want at most %.4f — the drift must be too slow to read as a step",
						frame, frame+1, step, spinnerSoftStep)
				}
			}
		})
	}
}

// TestSpinnerColorOffPaintsNoForeground proves the loop is genuinely off when it is off, for every
// style including classic: the glyph is painted on the bare spinner field, so it keeps the
// terminal's own text colour and emits no per-frame foreground of any kind. This is the escape
// hatch a 16-colour terminal relies on, and — with classic — the pre-plan look.
func TestSpinnerColorOffPaintsNoForeground(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	for _, style := range spinnerStyleNames {
		t.Run(string(style), func(t *testing.T) {
			t.Parallel()

			s := newSpinnerAnim(style, false)
			// Two laps of the colour loop: long enough that a foreground creeping in at some phase
			// of the (absent) gradient would show up.
			for frame := 0; frame < 2*s.framesPerColorLoop(); frame++ {
				s.frame = frame
				if got, want := s.view(th), th.spinnerBase.Render(s.glyph()); got != want {
					t.Fatalf("frame %d renders %q, want the bare field's %q — colour off must add no foreground",
						frame, got, want)
				}
			}
		})
	}
}

// TestSpinnerColorIsOrthogonalToStyle is the table over all six combinations — three styles ×
// colour on and off — and it is the test that fails if anyone later keys the colour off the style.
// Both axes are checked at once: the glyph is the style's own on every frame whatever the colour
// setting, and the colour is applied (on) or entirely absent (off) whatever the style.
func TestSpinnerColorIsOrthogonalToStyle(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	for _, style := range spinnerStyleNames {
		for _, colour := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/colour=%t", style, colour), func(t *testing.T) {
				t.Parallel()

				s := newSpinnerAnim(style, colour)
				bare := newSpinnerAnim(style, false)
				loop := s.framesPerColorLoop()
				// A third of a lap apart, so a working loop is on a different tone at each one.
				frames := [3]int{0, loop / 3, 2 * loop / 3}

				for _, frame := range frames {
					s.frame, bare.frame = frame, frame
					glyph := spinnerSpecs[style].glyph(frame)
					if got := s.glyph(); got != glyph {
						t.Errorf("frame %d glyph = %q, want %s's own %q — the colour setting must not touch the animation",
							frame, got, style, glyph)
					}
					rendered := s.view(th)
					if !strings.Contains(rendered, glyph) {
						t.Errorf("frame %d renders %q, which does not carry %s's glyph %q", frame, rendered, style, glyph)
					}
					if colour {
						if rendered == bare.view(th) {
							t.Errorf("frame %d renders identically with the loop on and off — %s never gets a colour",
								frame, style)
						}
						continue
					}
					if want := th.spinnerBase.Render(glyph); rendered != want {
						t.Errorf("frame %d renders %q, want the bare field's %q — %s must stay uncoloured",
							frame, rendered, want, style)
					}
				}

				if !colour {
					return
				}
				// With the loop on, three frames a third of a lap apart carry three tones, each
				// further from the others than a single soft step.
				for i := 0; i < len(frames); i++ {
					for j := i + 1; j < len(frames); j++ {
						apart := oklabDistance(t, th.spinnerColor(frames[i], loop), th.spinnerColor(frames[j], loop))
						if apart <= spinnerSoftStep {
							t.Errorf("frames %d and %d are only %.4f apart in Oklab — a third of a lap apart the loop must have moved",
								frames[i], frames[j], apart)
						}
					}
				}
			})
		}
	}
}

// spinnerLoopFrames is how many frames each style spends on one colour lap: the counts that make
// three different frame rates share one wall-clock period. They are written out rather than derived
// so a change to a style's interval that reshapes the loop fails here instead of quietly retiming it.
var spinnerLoopFrames = map[SpinnerStyle]int{
	SpinnerSnake:   120, // 12 fps
	SpinnerGlitter: 200, // 20 fps
	SpinnerClassic: 100, // 10 fps
}

// TestSpinnerColorPeriodIsTenSeconds proves the loop is timed in seconds rather than in frames:
// each style spends a different number of frames on a lap, and every lap still takes the same ten
// seconds, so selecting a faster animation does not speed the colour up with it.
func TestSpinnerColorPeriodIsTenSeconds(t *testing.T) {
	t.Parallel()

	if spinnerColorPeriod != 10*time.Second {
		t.Fatalf("spinnerColorPeriod = %v, want the ten seconds the design calls for", spinnerColorPeriod)
	}

	// The slack absorbs integer-nanosecond truncation in the intervals themselves and nothing more:
	// snake's time.Second/12 is 83.333333ms, a third of a nanosecond short, so its 120 frames land
	// 40ns under the period. A loop that actually drifted with the frame rate would miss by seconds.
	const slack = time.Microsecond

	for style, want := range spinnerLoopFrames {
		t.Run(string(style), func(t *testing.T) {
			t.Parallel()

			s := newSpinnerAnim(style, true)
			loop := s.framesPerColorLoop()
			if loop != want {
				t.Errorf("%s spends %d frames on a colour lap, want %d at its %v interval",
					style, loop, want, s.interval())
			}
			if diff := time.Duration(loop)*s.interval() - spinnerColorPeriod; diff < -slack || diff > slack {
				t.Errorf("%s's lap takes %v, want %v (±%v) — the styles' differing rates must not drift the period",
					style, time.Duration(loop)*s.interval(), spinnerColorPeriod, slack)
			}
		})
	}
}

// TestSpinnerTickChainGeneration proves the guard the bubbles widget's tag mechanism gave for
// free: only the live chain's ticks advance the spinner. Without it, re-arming while a tick is
// still in flight (an approval answered, an ask replied to) leaves two chains running and the
// spinner spins at double speed for the rest of the turn.
func TestSpinnerTickChainGeneration(t *testing.T) {
	t.Parallel()

	running := newTestModel(t)
	running.input.SetValue("hello")
	running = step(t, running, keyEnter()) // submit armed the chain
	if running.spin.gen == 0 {
		t.Fatal("submit did not arm the tick chain")
	}

	// The live generation's tick advances exactly one frame and keeps the chain alive.
	advanced, cmd := stepCmd(t, running, spinnerTickMsg{gen: running.spin.gen})
	if cmd == nil {
		t.Error("a live tick did not re-arm the chain — the spinner would freeze mid-turn")
	}
	if got, want := advanced.spin.frame, running.spin.frame+1; got != want {
		t.Errorf("frame after a live tick = %d, want %d", got, want)
	}

	// A re-arm opens a new generation, back at frame 0.
	rearmed := running
	if tick := rearmed.spin.arm(); tick == nil {
		t.Fatal("arm scheduled no tick")
	}
	if rearmed.spin.gen == running.spin.gen {
		t.Fatalf("arm reused generation %d instead of opening a new one", rearmed.spin.gen)
	}

	// A tick left over from the retired chain is inert: no advance, no re-arm.
	stale, cmd := stepCmd(t, rearmed, spinnerTickMsg{gen: running.spin.gen})
	if cmd != nil {
		t.Error("a tick from a retired generation re-armed the chain — two chains would run at 2x")
	}
	if got, want := stale.spin.frame, rearmed.spin.frame; got != want {
		t.Errorf("a retired generation's tick advanced the frame to %d, want %d", got, want)
	}

	// Off the running state the chain dies even for the live generation.
	idle := rearmed
	idle.state = stateIdle
	if _, cmd := stepCmd(t, idle, spinnerTickMsg{gen: idle.spin.gen}); cmd != nil {
		t.Error("the tick chain outlived the run instead of dying at idle")
	}
}

// ----------------------------------------------------------------------------
// The tick as the live star's clock (layout.md, "The live star")
// ----------------------------------------------------------------------------

// openCall folds a terminal call with no result — the open call that makes a block live.
func openCall(m *Model, id, command string) {
	m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{
		ID: id, Tool: "terminal", Arguments: []byte(`{"command":"` + command + `"}`)}})
}

// TestSpinnerTickRepaintsOnlyOnAFlipWhileACallIsOpen pins both halves of the tick's second job: it
// repaints the transcript so a live block's star can flip, and it does that ONLY on the tick that
// actually flips the phase and only while a call is open. The guard is not economy alone — a repaint
// is where a held drag-selection is judged (refreshViewport's keep-if-unchanged rule), so a tick that
// repainted unconditionally would put that judgment between the human and every selection for the
// whole of a turn, ten to twenty times a second, to draw a frame identical to the last one. The
// stash refreshViewport writes (m.lines) is the observable: a sentinel left standing is a repaint
// that did not happen.
func TestSpinnerTickRepaintsOnlyOnAFlipWhileACallIsOpen(t *testing.T) {
	const sentinel = "left standing by a tick that drew nothing"

	running := newTestModel(t)
	running.input.SetValue("run the tests")
	running = step(t, running, keyEnter()) // submit armed the chain
	if running.state != stateRunning {
		t.Fatalf("precondition: state = %v after a submit, want running", running.state)
	}
	// The tick that flips the phase is the last frame of a half-period; arm left the frame at 0, so
	// the boundary is one short of the half. A style whose half is a single frame would make the
	// "no flip" case unreachable, which no bundled style is (framesPerBlinkHalf is 5, 6 or 10).
	boundary := running.spin.framesPerBlinkHalf() - 1
	if boundary < 1 {
		t.Fatalf("precondition: framesPerBlinkHalf() = %d leaves no non-flipping tick to test",
			running.spin.framesPerBlinkHalf())
	}

	// Nothing open, on a tick that DOES flip: every block paints the same at either phase, so there
	// is nothing to redraw.
	idle := running
	idle.spin.frame = boundary
	idle.lines = []string{sentinel}
	idle = step(t, idle, spinnerTickMsg{gen: idle.spin.gen})
	if got := strings.Join(idle.lines, "\n"); got != sentinel {
		t.Errorf("a flipping tick with no call open repainted the transcript:\n%s", got)
	}

	// A call open, but on a tick INSIDE a half-period (frame 0 → 1): the star paints identically
	// either side of it, so this repaint would be work for its own sake.
	steady := running
	openCall(&steady, "c1", "go test ./...")
	steady.lines = []string{sentinel}
	steady = step(t, steady, spinnerTickMsg{gen: steady.spin.gen})
	if got := strings.Join(steady.lines, "\n"); got != sentinel {
		t.Errorf("a non-flipping tick repainted the transcript for an identical frame:\n%s", got)
	}

	// A call open on the tick that crosses the half-period, and the tick is the only clock its star
	// has.
	live := running
	openCall(&live, "c1", "go test ./...")
	live.spin.frame = boundary
	live.lines = []string{sentinel}
	live = step(t, live, spinnerTickMsg{gen: live.spin.gen})
	if got := strings.Join(live.lines, "\n"); got == sentinel {
		t.Error("the flipping tick with a call open did not repaint — the live star would never flip")
	}
}

// TestTickRepaintReachesADetachedViewport checks the flip reaches the rows a human who has SCROLLED
// UP is looking at — often the very block still working. refreshViewport's detached path returns
// early to leave the offset exactly where the human put it, but it swaps the content lines before it
// does, and that is what carries the new glyph onto the screen without re-attaching the view.
func TestTickRepaintReachesADetachedViewport(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("run the tests")
	m = step(t, m, keyEnter())
	m.transcript.reset() // the live block opens the scrollback…
	openCall(&m, "c1", "go test ./...")
	for i := 0; i < 30; i++ { // …and a screenful of history pushes it off the tail
		m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), 0)
	}
	m.refreshViewport()
	m.detached = true
	m.viewport.SetYOffset(0) // scrolled back up to the live block
	if m.viewport.AtBottom() {
		t.Fatal("precondition: the held offset is already at the bottom")
	}
	if before := plain(m.View()); !strings.Contains(before, "✦ Run") {
		t.Fatalf("precondition: the settled star is not on screen:\n%s", before)
	}

	m.spin.frame = m.spin.framesPerBlinkHalf() - 1 // the next tick is the one that crosses the phase
	m = step(t, m, spinnerTickMsg{gen: m.spin.gen})

	if !m.detached {
		t.Error("the tick's repaint re-attached a scrolled-up view")
	}
	if after := plain(m.View()); strings.Contains(after, "✦ Run") {
		t.Errorf("the flipped-out star never reached the detached viewport:\n%s", after)
	}
}

// TestBlinkingStarDropsOnlyTheSelectionsSpanningIt walks the star's one interaction with a held
// drag-selection, in both directions. The flip rewrites the header line, so a selection spanning it
// is dropped — the keep-if-unchanged rule doing its ordinary job on a line that changed, not a rule
// of the star's own — while a selection over text the flip did not touch lives through the same
// repaint.
func TestBlinkingStarDropsOnlyTheSelectionsSpanningIt(t *testing.T) {
	// live returns a running model whose transcript is exactly the given lead-in plus one open
	// tool-call block, repainted and with a drag-selection armed across the viewport's first row.
	live := func(t *testing.T, leadIn func(m *Model)) Model {
		t.Helper()
		m := newTestModel(t)
		m.input.SetValue("run the tests")
		m = step(t, m, keyEnter())
		m.transcript.reset() // drop the seeded start-up box and the send: the lead-in owns line 0
		leadIn(&m)
		openCall(&m, "c1", "go test ./...")
		m.refreshViewport()
		m.spin.frame = m.spin.framesPerBlinkHalf() - 1 // the next tick is the one that flips the phase
		return armTranscriptSelection(t, m, 0)
	}

	// Row 0 IS the live header: the flip rewrites the very line the span covers.
	spanning := live(t, func(*Model) {})
	spanning = step(t, spanning, spinnerTickMsg{gen: spanning.spin.gen})
	if spanning.transcriptSel.active {
		t.Error("a selection spanning the flipping header survived the flip")
	}

	// Row 0 is a settled user block and the live header sits below it: the flip is none of the
	// span's business.
	elsewhere := live(t, func(m *Model) { m.transcript.addUser("run the tests", nil) })
	elsewhere = step(t, elsewhere, spinnerTickMsg{gen: elsewhere.spin.gen})
	if !elsewhere.transcriptSel.active {
		t.Error("the star's flip dropped a selection over settled lines it never touched")
	}
}

// TestParseSpinnerStyle covers the config layer's single source of truth for the vocabulary: the
// three names, the empty default, and an unknown value whose error names the styles this build
// knows (the caller adds the config key).
func TestParseSpinnerStyle(t *testing.T) {
	t.Parallel()

	for _, want := range spinnerStyleNames {
		got, err := ParseSpinnerStyle(string(want))
		if err != nil {
			t.Errorf("ParseSpinnerStyle(%q) errored: %v", want, err)
		}
		if got != want {
			t.Errorf("ParseSpinnerStyle(%q) = %q, want %q", want, got, want)
		}
	}

	got, err := ParseSpinnerStyle("")
	if err != nil {
		t.Errorf(`ParseSpinnerStyle("") errored: %v`, err)
	}
	if got != defaultSpinnerStyle {
		t.Errorf(`ParseSpinnerStyle("") = %q, want the default %q`, got, defaultSpinnerStyle)
	}

	if _, err := ParseSpinnerStyle("sparkle"); err == nil {
		t.Error(`ParseSpinnerStyle("sparkle") accepted an unknown style`)
	} else {
		for _, style := range spinnerStyleNames {
			if !strings.Contains(err.Error(), string(style)) {
				t.Errorf("error %q does not name the known style %q", err, style)
			}
		}
	}
}

// TestNewModelSelectsTheConfiguredSpinner pins the construction seam the `ui:` config block feeds:
// newModel carries Options.Spinner and Options.SpinnerColor onto the Model's animation, both of
// them, independently — the table walks all six combinations, so folding the colour flag into the
// style (or reading one off the other) fails here rather than in a live run. The zero Options are
// covered too: cmd/apogee always resolves a real style, so the zero value reaches only hand-built
// test Options, where it must stay the one-column uncoloured classic cell the rest of this
// package's status-line tests are written against.
func TestNewModelSelectsTheConfiguredSpinner(t *testing.T) {
	t.Parallel()

	for _, style := range spinnerStyleNames {
		for _, colour := range []bool{false, true} {
			opts := testOpts
			opts.Spinner, opts.SpinnerColor = style, colour
			m := newModel(context.Background(), &fakeEngine{}, opts, nil)
			if m.spin.style != style {
				t.Errorf("newModel with Spinner %q built the animation for %q", style, m.spin.style)
			}
			if m.spin.color != colour {
				t.Errorf("newModel with SpinnerColor %v built the animation with color = %v (style %q)",
					colour, m.spin.color, style)
			}
			if got, want := m.spin.glyph(), spinnerSpecs[style].glyph(0); got != want {
				t.Errorf("newModel with Spinner %q renders %q at frame 0; want that style's own %q", style, got, want)
			}
		}
	}

	// Zero Options: no style named, no colour asked for — the still, uncoloured spinner the
	// hand-built test Options in this package rely on (classic, one column, no foreground).
	m := newModel(context.Background(), &fakeEngine{}, Options{}, nil)
	if m.spin.color {
		t.Error("zero Options built a coloured spinner; the colour loop must be opt-in")
	}
	if got, want := m.spin.view(m.th), m.th.spinnerBase.Render(legacyClassicFrames[0]); got != want {
		t.Errorf("zero Options render %q at frame 0; want the bare classic cell %q", got, want)
	}
}
