package tui

import (
	"context"
	"fmt"
	"image/color"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
)

// hexOf renders a resolved colour back as the "#rrggbb" a scheme role is written in, so an
// assertion below reads in the vocabulary of the scheme file rather than in lipgloss' RGBA tuple.
// The 16-bit channels colour.Color reports are premultiplied; every tone in play here is opaque, so
// taking the high byte of each is exact.
func hexOf(c color.Color) string {
	if c == nil {
		return "<none>"
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// TestNewThemeTakesItsColoursFromTheScheme is the wiring guard between a [scheme.Scheme] and the
// styles [newTheme] builds from it. The palette used to be package-level vars this file could not
// mis-address; now every colour travels through a named role, and a crossed wire — `error` reaching
// the selection bar, `mode-auto` reaching the plan marker — would render perfectly and be wrong.
//
// It samples rather than enumerates: one style per SHAPE a role can arrive in (a foreground, a
// background, a border slot, a per-value colour, a raw field), asserted against a scheme whose every
// role carries a DISTINCT value, so a swap between any two of the sampled roles fails here. The
// pixel-identity of the shipped dark scheme is the whole existing suite's job, and the scheme
// package's own TestEmbeddedDarkMatchesPinnedPalette pins the hex values themselves.
func TestNewThemeTakesItsColoursFromTheScheme(t *testing.T) {
	t.Parallel()

	// Every role a distinct value, so no assertion below can pass by borrowing another role's tone.
	s := scheme.Scheme{
		UserText: "#010101", Chrome: "#020202", Divider: "#030303", Surface: "#040404",
		Muted: "#050505", MutedBright: "#181818", DiffAdd: "#060606", DiffDel: "#070707", Error: "#080808",
		Code: "#090909", ModePlan: "#0a0a0a", ModeAskBefore: "#0b0b0b", ModeAllowEdits: "#0c0c0c",
		ModeAuto: "#0d0d0d", Skill: "#0e0e0e", FileRef: "#0f0f0f", PromptToggle: "#101010",
		ToolMarker: "#111111", Gauge: "#121212", Selection: "#131313",
		Spinner1: "#141414", Spinner2: "#151515", Spinner3: "#161616", Spinner4: "#171717",
	}
	th := newTheme(s)

	// The colour loop's stops arrive in a SHAPE of their own — a slice built from four roles at once
	// (buildSpinnerStops), not a style — so the count is asserted before the table indexes into it: a
	// loop wired from three roles would otherwise crash the run instead of failing it.
	if len(th.spinnerStops) != 4 {
		t.Fatalf("the theme carries %d spinner stops; want the scheme's four", len(th.spinnerStops))
	}

	for _, tc := range []struct {
		name string
		got  color.Color
		want string
	}{
		// A foreground role, and a background role.
		{"errorText fg", th.errorText.GetForeground(), s.Error},
		{"selection bg", th.selection.GetBackground(), s.Selection},
		// A style carrying two roles at once — an accent on the field it stands on.
		{"skillToken fg", th.skillToken.GetForeground(), s.Skill},
		{"skillToken bg", th.skillToken.GetBackground(), s.Surface},
		{"fileToken fg", th.fileToken.GetForeground(), s.FileRef},
		// A border slot, which lipgloss keeps apart from the foreground.
		{"inputBorder border fg", th.inputBorder.GetBorderTopForeground(), s.Chrome},
		// The roles that are easiest to cross, because their dark values coincide today.
		{"diffAdded fg", th.diffAdded.GetForeground(), s.DiffAdd},
		{"diffRemoved fg", th.diffRemoved.GetForeground(), s.DiffDel},
		{"toolLabel fg", th.toolLabel.GetForeground(), s.Code},
		{"gaugeFill fg", th.gaugeFill.GetForeground(), s.Gauge},
		{"hairline fg", th.hairline.GetForeground(), s.Divider},
		{"toolMarker fg", th.toolMarker.GetForeground(), s.ToolMarker},
		// The two steps of one ramp: a collapsed block's text and an open one's. Crossing these
		// would cost the open block the only cue that says it is open.
		{"toolDetail fg", th.toolDetail.GetForeground(), s.Muted},
		{"toolDetailBright fg", th.toolDetailBright.GetForeground(), s.MutedBright},
		{"promptToggle fg", th.promptToggle.GetForeground(), s.PromptToggle},
		{"userBlock fg", th.userBlock.GetForeground(), s.UserText},
		// The raw fields the call sites that paint without a style reach for.
		{"raw surface", th.surface, s.Surface},
		{"raw chrome", th.chrome, s.Chrome},
		{"raw muted", th.muted, s.Muted},
		{"raw errorFg", th.errorFg, s.Error},
		// The per-VALUE colour: one rung of the ladder must not answer for another.
		{"modeColor plan", th.modeColor(domain.ModePlan), s.ModePlan},
		{"modeColor ask-before", th.modeColor(domain.ModeAskBefore), s.ModeAskBefore},
		{"modeColor allow-edits", th.modeColor(domain.ModeAllowEdits), s.ModeAllowEdits},
		{"modeColor auto", th.modeColor(domain.ModeAuto), s.ModeAuto},
		{"modeColor off-ladder", th.modeColor(domain.Mode("nonesuch")), s.Muted},
		// The loop's first waypoint: the one role that has to survive a conversion into the blend's
		// own colour space on its way onto the theme, and the ORDER the loop visits its stops in.
		{"first spinner stop", th.spinnerStops[0], s.Spinner1},
	} {
		if got := hexOf(tc.got); got != tc.want {
			t.Errorf("%s = %s; want the scheme's %s", tc.name, got, tc.want)
		}
	}
}

// TestDefaultThemeKeepsTheDarkPalette pins the production path — [scheme.Default] through
// [newTheme] — to the hex values apogee has always drawn with, on the sample of roles the plan
// names. It is the "nothing moved" half of the guard above: the wiring test proves a role reaches
// the style it names, this one proves the DEFAULT scheme still carries the tones the transcript,
// the footer and the prompt were designed around.
func TestDefaultThemeKeepsTheDarkPalette(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())

	for _, tc := range []struct {
		name string
		got  color.Color
		want string
	}{
		{"errorText fg", th.errorText.GetForeground(), "#f85149"},
		{"selection bg", th.selection.GetBackground(), "#3a5fcd"},
		{"mode plan", th.modeColor(domain.ModePlan), "#2afefa"},
		{"mode ask-before", th.modeColor(domain.ModeAskBefore), "#3fb950"},
		{"mode allow-edits", th.modeColor(domain.ModeAllowEdits), "#58a6ff"},
		{"mode auto", th.modeColor(domain.ModeAuto), "#f0883e"},
		{"raw surface", th.surface, "#000000"},
		{"raw chrome", th.chrome, "#4a4a4a"},
	} {
		if got := hexOf(tc.got); got != tc.want {
			t.Errorf("%s = %s; want the dark scheme's %s", tc.name, got, tc.want)
		}
	}

	// The open-detail tone now travels as the `muted-bright` role, and the dark scheme must keep
	// paying it the same hex the literal used to: assert it is still the step brighter the collapsed
	// dim is read against, so the contrast cannot vanish silently.
	if got := hexOf(th.toolDetailBright.GetForeground()); got != "#b2b2b2" {
		t.Errorf("toolDetailBright fg = %s; want the dark scheme's #b2b2b2", got)
	}
	if hexOf(th.toolDetail.GetForeground()) == hexOf(th.toolDetailBright.GetForeground()) {
		t.Error("the collapsed and open detail tones resolve to the same colour; the contrast step is gone")
	}

	// The spinner's loop is the scheme's too, now that the last four palette vars are gone: pin the
	// violet the lap has always opened on, so the tones the animation was designed around cannot
	// drift out of the dark scheme unnoticed.
	if len(th.spinnerStops) == 0 {
		t.Fatal("the theme carries no spinner stops; the colour loop would have nothing to blend")
	}
	if got := hexOf(th.spinnerStops[0]); got != "#8668ff" {
		t.Errorf("first spinner stop = %s; want the dark scheme's #8668ff", got)
	}
}

// The boot seam: [newModel] builds its theme from the palette the binary RESOLVED and handed over
// ([Options.ColorScheme]), rather than from the built-in default. Without this the whole
// `ui.color-scheme` key would resolve correctly at the composition root and change nothing on
// screen.
//
// The second half is the zero value's answer. Every hand-built Options in this package — and any
// future Driver that does not care about colour — leaves the field zero, and the zero Scheme is not
// a palette at all (every role the empty string), so it must resolve to the shipped default rather
// than to a screen with no colours in it.
func TestNewModelTakesItsThemeFromTheWiredScheme(t *testing.T) {
	t.Parallel()

	wired := scheme.Default()
	wired.Error = "#010203"
	m := newModel(context.Background(), &fakeEngine{}, Options{ColorScheme: wired}, nil)
	if got := hexOf(m.th.errorFg); got != "#010203" {
		t.Errorf("the model's error tone = %s; want the wired scheme's #010203", got)
	}

	bare := newModel(context.Background(), &fakeEngine{}, Options{}, nil)
	if got, want := hexOf(bare.th.errorFg), hexOf(newTheme(scheme.Default()).errorFg); got != want {
		t.Errorf("unwired Options gave the error tone %s; want the default scheme's %s", got, want)
	}
}

// What resolving the scheme cost reaches the human: each warning the binary rendered becomes one
// transcript note at construction (ADR 0040 design call 11). The load is forgiving — an unknown
// name, an unreadable file and a bad hex all keep the session running on default colours — so the
// note is the only thing that tells the human their scheme is not the one they are looking at.
//
// They are EPHEMERAL: re-derived from the config and the schemes folder at every launch, so a
// resume must not replay a warning about a file it did not read.
func TestNewModelNotesTheColorSchemeWarnings(t *testing.T) {
	t.Parallel()

	const warning = `color-scheme "mine.yaml": key "error": bad hex "#zz0000" — using default`
	m := newModel(context.Background(), &fakeEngine{},
		Options{ColorSchemeWarnings: []string{warning}}, nil)

	if !hasEntry(m, entryNote, warning) {
		t.Errorf("no note carries the colour-scheme warning; entries = %+v", m.transcript.entries)
	}
	for _, e := range m.transcript.entries {
		if e.kind == entryNote && e.text == warning && !e.ephemeral {
			t.Error("the colour-scheme warning is persisted; it is re-derived at every launch")
		}
	}

	clean := newModel(context.Background(), &fakeEngine{}, Options{}, nil)
	if hasEntry(clean, entryNote, warning) {
		t.Error("a run with no warnings still noted one")
	}
}
