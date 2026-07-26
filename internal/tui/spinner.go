package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
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
	SpinnerClassic: {interval: classicInterval, width: 1, glyph: classicGlyph},
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

// view paints this frame's glyph for the status line: the theme's spinner field — the status
// bar's black background with no foreground of its own, so the glyph keeps the terminal's own
// text colour. This is the single place the glyph animation and its colour compose, so no style
// carries a colour of its own and nothing branches on the style to decide one.
func (s spinnerAnim) view(th theme) string {
	return th.spinnerBase.Render(s.glyph())
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
