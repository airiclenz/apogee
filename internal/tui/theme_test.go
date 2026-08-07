package tui

import (
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
		Muted: "#050505", DiffAdd: "#060606", DiffDel: "#070707", Error: "#080808",
		Code: "#090909", ModePlan: "#0a0a0a", ModeAskBefore: "#0b0b0b", ModeAllowEdits: "#0c0c0c",
		ModeAuto: "#0d0d0d", Skill: "#0e0e0e", FileRef: "#0f0f0f", PromptToggle: "#101010",
		ToolMarker: "#111111", Gauge: "#121212", Selection: "#131313",
		Spinner1: "#141414", Spinner2: "#151515", Spinner3: "#161616", Spinner4: "#171717",
	}
	th := newTheme(s)

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

	// The open-detail tone has no scheme role yet (theme.go's openDetailTone): assert it is still
	// the step brighter the collapsed dim is read against, so the contrast cannot vanish silently.
	if got := hexOf(th.toolDetailBright.GetForeground()); got != openDetailTone {
		t.Errorf("toolDetailBright fg = %s; want %s", got, openDetailTone)
	}
	if hexOf(th.toolDetail.GetForeground()) == hexOf(th.toolDetailBright.GetForeground()) {
		t.Error("the collapsed and open detail tones resolve to the same colour; the contrast step is gone")
	}
}
