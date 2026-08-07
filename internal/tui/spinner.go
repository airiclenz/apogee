package tui

import (
	"fmt"
	"image/color"
	"math"
	"math/bits"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	colorful "github.com/lucasb-eyer/go-colorful"
)

// ----------------------------------------------------------------------------
// The status-line spinner (the running state's animation)
// ----------------------------------------------------------------------------
//
// The spinner is this package's own animation rather than a charm.land/bubbles/v2/spinner
// widget. The widget renders frames[i] through one fixed lipgloss.Style, which leaves no room
// for a glyph CHOSEN per frame or a colour COMPUTED per frame — both of which the spinner
// styles need. Here the frame index is the input to a pure function instead of an index into a
// literal, so a style can derive its glyph and a colour can be blended per frame.
//
// Every frame is a pure function of [spinnerAnim.frame]. That is not a stylistic preference: the
// Model is a value type copied on every Update (ADR 0011; doc.go), so animation state may hold
// no RNG handle and no self-referential no-copy type — a *rand.Rand would be shared across
// copies and advance from the ones Update discards. Plain ints in, glyph out: identical no
// matter how often View runs, and testable without a clock.
//
// What the widget gave for free and this file must replace: its TickMsg carried an id and a tag,
// so re-arming while a tick was still in flight could not leave two chains running. [spinnerAnim]
// reproduces that as a generation counter — [spinnerAnim.arm] opens a new generation and the
// Update loop drops a tick from any older one, which is what keeps the frame rate from doubling
// after an approval prompt or an ask_user question re-arms the chain.

// SpinnerStyle names a status-line animation. internal/tui owns the vocabulary so the config
// layer validates against one source of truth rather than duplicating the name list.
type SpinnerStyle string

const (
	SpinnerSnake   SpinnerStyle = "snake"
	SpinnerGlitter SpinnerStyle = "glitter"
	SpinnerClassic SpinnerStyle = "classic"
)

// spinnerStyleNames is the vocabulary [ParseSpinnerStyle] accepts, in the order its error lists
// them: the names this build knows, declared once so no caller re-types the set.
var spinnerStyleNames = []SpinnerStyle{SpinnerSnake, SpinnerGlitter, SpinnerClassic}

// defaultSpinnerStyle is what an unset config value resolves to.
const defaultSpinnerStyle = SpinnerSnake

// ParseSpinnerStyle maps a config value onto a style. "" ⇒ the default; an unknown value is an
// error naming the styles this build knows. The caller names the key it read the value from —
// this package does not know the config schema.
func ParseSpinnerStyle(s string) (SpinnerStyle, error) {
	if s == "" {
		return defaultSpinnerStyle, nil
	}
	for _, style := range spinnerStyleNames {
		if SpinnerStyle(s) == style {
			return style, nil
		}
	}
	names := make([]string, 0, len(spinnerStyleNames))
	for _, style := range spinnerStyleNames {
		names = append(names, string(style))
	}
	return "", fmt.Errorf("unknown spinner style %q (known styles: %s)", s, strings.Join(names, ", "))
}

// classicFrames are the eight braille cells of the original status-line spinner — a single cell
// that appears to rotate. They are pinned, in this order, by
// TestSpinnerClassicMatchesLegacyFrames: classic is a permanently supported style, not a
// transitional fallback, so its look is a promise rather than an implementation detail.
var classicFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// classicInterval holds each classic frame for a tenth of a second — the 10 fps the bundled
// bubbles spinners run at, and the rate this style has always run at.
const classicInterval = time.Second / 10

// classicGlyph paints classic's frame n: the eight-cell rotation, cycling. frame is never
// negative (arm zeroes it and a tick only increments it), so the modulo needs no guard.
func classicGlyph(frame int) string {
	return classicFrames[frame%len(classicFrames)]
}

// brailleBase is the braille block's origin: a cell is U+2800 plus the mask of the dots it lights,
// so U+2800 on its own is the blank cell — a PAINTED cell with no dots, not a space, which is what
// keeps a partly-empty frame from being trimmed as padding.
const brailleBase = 0x2800

// brailleDotBits maps a dot's place inside one braille cell — the cell's own column, then the row —
// onto its bit in that cell's mask. The block numbers its dots down the left column and then down
// the right (1,2,3 then 4,5,6) with the two low-vision dots 7 and 8 appended afterwards, which is
// why the fourth row jumps to 0x40/0x80 instead of continuing the run.
var brailleDotBits = [2][4]rune{
	{0x01, 0x02, 0x04, 0x40}, // the cell's left column:  dots 1, 2, 3, 7
	{0x08, 0x10, 0x20, 0x80}, // the cell's right column: dots 4, 5, 6, 8
}

// brailleDots is how many dots one cell of the block can light: eight, in its 8-dot form. It is both
// the width of a cell's mask in bits and the top of the density scale the glitter style breathes
// along, which is why the block spans 2^brailleDots cells.
const brailleDots = 8

// snakeRing walks the outer ring of the 4-column × 4-row dot grid that two braille cells form side
// by side, clockwise from the top-left, as (col, row) with cols 0–1 in the left cell and cols 2–3
// in the right. The frames are DERIVED from this table rather than hand-written — the table is the
// documentation of the shape — and TestSnakeFrames pins the twelve glyphs it derives, so the
// literals are the guard and this is the explanation.
var snakeRing = [12]struct{ col, row int }{
	{0, 0}, {1, 0}, {2, 0}, {3, 0}, // across the top, left to right
	{3, 1}, {3, 2}, {3, 3}, // down the right edge
	{2, 3}, {1, 3}, {0, 3}, // back across the bottom, right to left
	{0, 2}, {0, 1}, // up the left edge, closing the lap
}

// snakeLength is how much of the ring is lit at once: six of its twelve positions, so the snake is
// exactly half the ring and reads as one continuous arc with a visible gap behind its tail.
const snakeLength = 6

// snakeInterval gives the ring one lap a second — twelve positions, one position per frame.
const snakeInterval = time.Second / time.Duration(len(snakeRing))

// snakeGlyph paints snake's frame n: the six-position run of the ring starting at position n,
// wrapping, folded back into the two braille cells that carry it. frame is never negative (arm
// zeroes it and a tick only increments it), so the modulo needs no guard.
func snakeGlyph(frame int) string {
	cells := [2]rune{brailleBase, brailleBase}
	for i := 0; i < snakeLength; i++ {
		dot := snakeRing[(frame+i)%len(snakeRing)]
		cells[dot.col/2] |= brailleDotBits[dot.col%2][dot.row]
	}
	return string(cells[:])
}

// glitterBreath is one full swell-and-fall of the glitter style's density. It is deliberately slow
// against the rate the glyph itself re-rolls at: a slow breath with a slow glyph is a pulse, and the
// CONTRAST between the two — a six-second swell under a cell that changes every frame — is what
// makes this style read as glitter.
const glitterBreath = 6 * time.Second

// glitterInterval re-rolls the glyph twenty times a second: the fast half of that contrast, up from
// classic's ten. View composes pre-rendered viewport rows, so the extra repaints cost little — but
// the rate lives here as one named constant so backing it off is a one-line edit.
const glitterInterval = time.Second / 20

// glitterFramesPerBreath is how many frames one breath spans at this style's rate — 120, so the
// density creeps through the whole swell while the pair of cells is re-rolled 120 times.
const glitterFramesPerBreath = int(glitterBreath / glitterInterval)

// glitterCells is how many braille cells a glitter frame paints: two, side by side, each rolled
// independently. It is also the style's width in terminal columns, one per cell.
const glitterCells = 2

// brailleByDots buckets the braille block, U+2800 through U+28FF, by how many of a cell's dots are
// lit — the density sort. brailleByDots[n] holds every cell of density n, so the animation asks for a
// density and never needs to know a glyph. The bucket sizes are the binomials 1, 8, 28, 56, 70, 56,
// 28, 8, 1; the last holds only ⣿, which is why the breath tops out on a solid pair of cells.
var brailleByDots = buildBrailleByDots()

// buildBrailleByDots sorts the block by density, once, at package init. A cell is brailleBase plus
// the mask of the dots it lights, so a population count over the mask IS the cell's density.
func buildBrailleByDots() [brailleDots + 1][]rune {
	var buckets [brailleDots + 1][]rune
	for mask := 0; mask < 1<<brailleDots; mask++ {
		density := bits.OnesCount8(uint8(mask))
		buckets[density] = append(buckets[density], rune(brailleBase+mask))
	}
	return buckets
}

// glitterDensityScale maps the sine's 0..2 swing onto the block's density range: half the eight-dot
// span, so 1 + round(scale × (1 + sin)) is 1 at the trough and brailleDots at the peak.
const glitterDensityScale = float64(brailleDots-1) / 2

// glitterDensity is the breath: a sine over one framesPerBreath period, mapped onto 1..brailleDots
// dots. It never reaches 0 — a blank cell mid-run reads as a stalled spinner, not as a dim one. The
// period is a parameter rather than the constant so a test can breathe in its own number of frames.
func glitterDensity(frame, framesPerBreath int) int {
	phase := 2 * math.Pi * float64(frame%framesPerBreath) / float64(framesPerBreath)
	return 1 + int(math.Round(glitterDensityScale*(1+math.Sin(phase))))
}

// glitterFrameStride and glitterCellStride spread the (frame, cell) pair across the 64-bit space
// before the finaliser mixes it — the golden-ratio odd constant for the frame, a second odd constant
// for the cell — so consecutive frames, and the two cells of one frame, do not pick neighbouring
// members of the same bucket.
const (
	glitterFrameStride = 0x9E3779B97F4A7C15
	glitterCellStride  = 0xBF58476D1CE4E5B9
)

// glitterCell picks this frame's glyph for one cell out of the bucket the breath currently calls for.
// The pick is a HASH of (frame, cell), not a draw from a *rand.Rand: the Model is value-copied on
// every Update (ADR 0011), so an RNG handle would be shared across the copies and advance from the
// ones Update discards — the same frame would then paint differently depending on how often View
// ran. A hash keeps every frame a pure function of frame: reproducible in a test, stable under the
// copy. dots must be a density the block has cells for, 0..brailleDots, which is all glitterDensity
// ever yields; frame is never negative (arm zeroes it and a tick only increments it).
func glitterCell(frame, cell, dots int) rune {
	bucket := brailleByDots[dots]
	pick := splitmix64(uint64(frame)*glitterFrameStride + uint64(cell)*glitterCellStride)
	return bucket[pick%uint64(len(bucket))]
}

// splitmix64 is SplitMix64's finaliser — the avalanche step that turns a strided counter into a
// well-spread 64-bit value. Only the mixing function, not the generator: it holds no state, so it
// stays a pure function of its input, which is what the value-copied Model needs. The shift and
// multiply constants are the published ones and mean nothing on their own.
func splitmix64(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 27
	x *= 0x94D049BB133111EB
	x ^= x >> 31
	return x
}

// glitterGlyph paints glitter's frame n: both cells at the density the breath is on, each rolled
// independently, so the pair sparkles while the density swells and falls underneath it.
func glitterGlyph(frame int) string {
	dots := glitterDensity(frame, glitterFramesPerBreath)
	var cells [glitterCells]rune
	for cell := range cells {
		cells[cell] = glitterCell(frame, cell, dots)
	}
	return string(cells[:])
}

// spinnerSpec is one style's animation, as data: how long a single frame is held, how many
// terminal columns its glyph occupies, and the pure function that paints frame n.
type spinnerSpec struct {
	interval time.Duration
	width    int
	glyph    func(frame int) string
}

// spinnerSpecs is the registry of implemented styles — the set the renderer resolves against and
// the set TestSpinnerFrameWidth sweeps. A style declared above but absent here is a name this
// build parses without yet having an animation for it; [spinnerAnim.spec] resolves it to classic.
var spinnerSpecs = map[SpinnerStyle]spinnerSpec{
	SpinnerSnake:   {interval: snakeInterval, width: 2, glyph: snakeGlyph},
	SpinnerGlitter: {interval: glitterInterval, width: glitterCells, glyph: glitterGlyph},
	SpinnerClassic: {interval: classicInterval, width: 1, glyph: classicGlyph},
}

// The colour loop is ORTHOGONAL to the glyph animation: it is a flag on [spinnerAnim], never a
// property of a style, so all three styles × colour on/off are valid combinations and every one of
// them renders. [spinnerAnim.view] is the single place the two compose — nothing below branches on
// the style to decide a colour, and no style carries one of its own.

// spinnerColorPeriod is one full lap of the colour loop in wall-clock time. It is the same ten
// seconds under every style: the period is the constant and the frame count is derived from the
// style's own interval (framesPerColorLoop), so a faster style takes more, smaller colour steps
// rather than a shorter lap.
const spinnerColorPeriod = 10 * time.Second

// The loop's waypoints are the scheme's four `spinner-*` roles, visited in order — violet → green →
// amber → pink and back to violet under the dark scheme. They live on the theme ([theme.spinnerStops],
// built in newTheme) rather than in a package var of this file, because a package var is fixed at init
// and the active scheme is not: a runtime scheme switch has to move the loop with it (ADR 0039). Named
// stops rather than a raw hue circle is the point: the footer's autonomy-mode markers and the orange
// code accent are already on screen, and a full hue sweep would collide with them.

// buildSpinnerStops converts a scheme's spinner colours into the space the blend works in, once per
// theme.
func buildSpinnerStops(palette ...color.Color) []colorful.Color {
	stops := make([]colorful.Color, 0, len(palette))
	for _, c := range palette {
		stop, ok := colorful.MakeColor(c)
		if !ok {
			// Unreachable: MakeColor refuses only a fully transparent colour, and every stop arrives
			// as an opaque "#rrggbb" the scheme package has already validated. It degrades to black
			// instead of panicking because a spinner in the wrong tone is a cosmetic fault, while a
			// panic while building the theme would take the binary down before it draws anything.
			stop = colorful.Color{}
		}
		stops = append(stops, stop)
	}
	return stops
}

// spinnerColor is the frame's foreground: the closed loop through the theme's stops, blended in Oklch.
// BlendOkLch keeps chroma up across the arcs where an sRGB lerp desaturates the midpoints into mud.
// The loop closes — the last stop blends back into the first — so there is no seam at the wrap.
//
// framesPerLoop is how many of the caller's frames one lap spans (framesPerColorLoop) and must be
// positive; frame is never negative (arm zeroes it and a tick only increments it).
//
// A deliberate caveat: this codebase does no terminal-profile detection — quantisation happens
// downstream in colorprofile — so on a 256-colour terminal the gradient steps visibly and on a
// 16-colour one it collapses to a couple of tones. Turning the loop off is the answer for those
// terminals, not a narrower gradient here.
func (th theme) spinnerColor(frame, framesPerLoop int) color.Color {
	// Where this frame sits on the loop, measured in arcs: 0 at the first stop, 1 at the second,
	// len(stops) back at the first. Taking the frame modulo the lap keeps pos below the stop count,
	// so arc indexes a real stop and the remainder is the fraction along the arc leaving it.
	stops := th.spinnerStops
	pos := float64(frame%framesPerLoop) / float64(framesPerLoop) * float64(len(stops))
	arc := int(pos)
	from, to := stops[arc], stops[(arc+1)%len(stops)]
	return lipgloss.Color(from.BlendOkLch(to, pos-float64(arc)).Hex())
}

// spinnerAnim is the animation state carried on the value-copied Model (ADR 0011): plain ints,
// no RNG handle, no self-referential type. It is the whole spinner — the widget it replaced held
// its frame counter and its chain bookkeeping the same way.
type spinnerAnim struct {
	style SpinnerStyle // which animation paints the glyph
	color bool         // the colour loop runs; false renders the glyph on the bare status field
	frame int          // frames elapsed since the chain was armed
	gen   int          // chain generation — drops a tick left over from a previous arm
}

// newSpinnerAnim builds the still, unarmed animation for a style. It schedules nothing: the
// chain starts when a run does (arm).
func newSpinnerAnim(style SpinnerStyle, color bool) spinnerAnim {
	return spinnerAnim{style: style, color: color}
}

// spec resolves this style's animation. An unregistered style — the zero value, or a name this
// build parses but has no animation for — falls back to classic, so the status line always shows
// something moving rather than a blank column where the spinner should be.
func (s spinnerAnim) spec() spinnerSpec {
	if spec, ok := spinnerSpecs[s.style]; ok {
		return spec
	}
	return spinnerSpecs[SpinnerClassic]
}

// interval is how long this style holds one frame — its frame rate, inverted. The tick chain
// re-schedules itself at this period, so a style's rate is its own business.
func (s spinnerAnim) interval() time.Duration { return s.spec().interval }

// glyph is this frame's braille cell(s), pure in [spinnerAnim.frame].
func (s spinnerAnim) glyph() string { return s.spec().glyph(s.frame) }

// starBlinkHalfPeriod is how long the transcript's LIVE STAR holds one phase: ✦ for half a second,
// then a bare cell for half a second (layout.md, "The live star"). It is a wall-clock duration
// rather than a frame count so the blink reads the same under every spinner style — the frame count
// a phase spans is DERIVED from the style's own interval ([spinnerAnim.framesPerBlinkHalf]) instead
// of being re-typed per style.
const starBlinkHalfPeriod = 500 * time.Millisecond

// framesPerBlinkHalf is how many of THIS style's frames one blink phase spans: 5 at classic's
// 10 fps, 6 at snake's 12, 10 at glitter's 20. The floor of one keeps a hypothetical style slower
// than the phase itself blinking — without it the division would round to zero and the star would
// freeze on one glyph — which is the same reasoning framesPerColorLoop's derivation carries.
func (s spinnerAnim) framesPerBlinkHalf() int {
	return max(1, int(starBlinkHalfPeriod/s.interval()))
}

// blink is this frame's phase of the transcript's LIVE STAR — the alternation between ✦ and a bare
// cell that a tool block still holding an open call paints its header glyph from (layout.md, "The
// live star"; [blockState.star]). It is derived from the frame count and nothing else, which is the
// whole point: the transcript carries no timer of its own, so the tick that advances this animation
// is the same tick that flips the star, and a run that is not spinning has a star that is not
// blinking either. A fresh chain starts at the ✦-showing phase — [spinnerAnim.arm] zeroes the
// frame, and the first half-period of frames is the false phase.
func (s spinnerAnim) blink() bool { return (s.frame/s.framesPerBlinkHalf())%2 == 1 }

// framesPerColorLoop is how many of THIS style's frames one colour lap spans: 100 at classic's
// 10 fps, 120 at snake's 12, 200 at glitter's 20. Deriving the count from the style's interval is
// what keeps the loop's wall-clock period at spinnerColorPeriod under every style, so selecting a
// faster animation does not speed the colour up with it.
func (s spinnerAnim) framesPerColorLoop() int { return int(spinnerColorPeriod / s.interval()) }

// view paints this frame's glyph for the status line, on the theme's spinner field — the status
// bar's black background. This is the single place the glyph animation and the colour loop compose:
// the two are orthogonal settings, so no style carries a colour of its own and nothing here
// branches on the style to decide one. Uncoloured, the field adds no foreground at all and the
// glyph keeps the terminal's own text colour — byte for byte what the pre-plan spinner rendered
// (TestSpinnerClassicUncolouredIsUnchanged), which is why this branch paints on spinnerBase rather
// than through the status bar's faint grey.
func (s spinnerAnim) view(th theme) string {
	if !s.color {
		return th.spinnerBase.Render(s.glyph())
	}
	return th.spinnerBase.Foreground(th.spinnerColor(s.frame, s.framesPerColorLoop())).Render(s.glyph())
}

// arm opens a fresh tick chain: a new generation, back at frame 0, with the first tick
// scheduled. Every return to stateRunning calls it — a submit, an approval decision, an answered
// question — and the new generation is what makes a tick still in flight from the previous chain
// inert, so the frame rate cannot double. It takes a pointer because the generation bump must
// land on the Model copy the caller returns — which is also why every caller arms in a statement
// of its own: in `return m, m.spin.arm()` the order of the bump and the copy of m is unspecified.
func (s *spinnerAnim) arm() tea.Cmd {
	s.gen++
	s.frame = 0
	return s.tick()
}

// tick schedules the next frame of the CURRENT generation, snapshotting it into the Msg so a
// chain the Update loop has since retired identifies itself.
func (s spinnerAnim) tick() tea.Cmd {
	gen := s.gen
	return tea.Tick(s.interval(), func(time.Time) tea.Msg { return spinnerTickMsg{gen: gen} })
}

// spinnerTickMsg advances the spinner one frame. gen names the chain that scheduled it; the
// Update loop drops any tick that does not carry the live generation.
type spinnerTickMsg struct{ gen int }
