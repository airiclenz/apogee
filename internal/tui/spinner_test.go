package tui

import (
	"strings"
	"testing"
	"time"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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

	th := newTheme()
	legacy := lipgloss.NewStyle().Background(colBlack) // exactly how newModel styled the widget
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

	th := newTheme()
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
