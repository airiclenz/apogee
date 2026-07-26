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
