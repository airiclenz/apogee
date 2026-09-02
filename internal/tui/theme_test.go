package tui

import (
	"context"
	"fmt"
	"image/color"
	"strings"
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
// role carries a DISTINCT value, so a swap between any two of the sampled roles fails here. What
// the SHIPPED scheme puts in those slots is the test below's business, and that scheme's structure
// the scheme package's own TestEmbeddedDarkIsTheCompleteDefault — its hex values are deliberately
// pinned nowhere, so retuning a color is never a test failure.
func TestNewThemeTakesItsColoursFromTheScheme(t *testing.T) {
	t.Parallel()

	// Every role a distinct value, so no assertion below can pass by borrowing another role's tone.
	s := scheme.Scheme{
		UserText: "#010101", Chrome: "#020202", Divider: "#030303", Surface: "#040404",
		Muted: "#050505", MutedBright: "#181818", Error: "#080808",
		Code: "#090909", ToolHeader: "#191919", ModePlan: "#0a0a0a", ModeAskBefore: "#0b0b0b",
		ModeAllowEdits: "#0c0c0c", ModeAuto: "#0d0d0d", Skill: "#0e0e0e", FileRef: "#0f0f0f",
		PromptToggle: "#101010", ToolMarker: "#111111", Gauge: "#121212", Selection: "#131313",
		Spinner1: "#141414", Spinner2: "#151515", Spinner3: "#161616", Spinner4: "#171717",
		ToolLeader: "#1a1a1a", ToolMarkerBright: "#1b1b1b", Warning: "#1c1c1c",
		DiffAddBg: "#1d1d1d", DiffDelBg: "#1e1e1e",
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
		// The status bar's two voices, sampled together: the fault token and the qualifier a rung
		// below it. They stand side by side on the same black field, so a crossed wire would paint
		// a "worth noticing" fact in the alarm tone and turn a qualifier into a failure.
		{"statusError fg", th.statusError.GetForeground(), s.Error},
		{"statusWarning fg", th.statusWarning.GetForeground(), s.Warning},
		{"statusWarning bg", th.statusWarning.GetBackground(), s.Surface},
		// A style carrying two roles at once — an accent on the field it stands on.
		{"skillToken fg", th.skillToken.GetForeground(), s.Skill},
		{"skillToken bg", th.skillToken.GetBackground(), s.Surface},
		{"fileToken fg", th.fileToken.GetForeground(), s.FileRef},
		// A border slot, which lipgloss keeps apart from the foreground.
		{"inputBorder border fg", th.inputBorder.GetBorderTopForeground(), s.Chrome},
		// The roles that are easiest to cross, because their dark values coincide today. They are
		// sampled on the BACKGROUND slot: a diff line's direction is a band under plain-tone text,
		// so a foreground here would be the old contract leaking back (detailStyle).
		{"diffAdded bg", th.diffAdded.GetBackground(), s.DiffAddBg},
		{"diffRemoved bg", th.diffRemoved.GetBackground(), s.DiffDelBg},
		// The tool header's own role and the code role it used to borrow: sampled together, because
		// keeping them apart is the whole reason `tool-header` exists.
		{"toolLabel fg", th.toolLabel.GetForeground(), s.ToolHeader},
		{"subRail fg", th.subRail.GetForeground(), s.ToolHeader},
		{"mdCode fg", th.mdCode.GetForeground(), s.Code},
		{"gaugeFill fg", th.gaugeFill.GetForeground(), s.Gauge},
		{"hairline fg", th.hairline.GetForeground(), s.Divider},
		// The marker role's own two steps — the collapsed slot and the open one. Crossing them
		// would leave an opened block's outcome sitting at the tone of the closed ones around it.
		{"toolMarker fg", th.toolMarker.GetForeground(), s.ToolMarker},
		{"toolMarkerBright fg", th.toolMarkerBright.GetForeground(), s.ToolMarkerBright},
		// The leader's own role, sampled beside the tone it is SEEDED from: a scheme damping the
		// dots must be able to move them without the ▶ they run up to, which is the whole reason
		// `tool-leader` exists as a role of its own rather than as another reader of `muted`.
		{"toolLeader fg", th.toolLeader.GetForeground(), s.ToolLeader},
		// The two steps of one ramp: a collapsed block's text and an open one's. Crossing these
		// would cost the open block the only cue that says it is open.
		{"toolDetail fg", th.toolDetail.GetForeground(), s.Muted},
		{"toolDetailBright fg", th.toolDetailBright.GetForeground(), s.MutedBright},
		{"promptToggle fg", th.promptToggle.GetForeground(), s.PromptToggle},
		{"userBlock fg", th.userBlock.GetForeground(), s.UserText},
		// The run view's header, sampled on BOTH slots: it keeps the prompt block's white and
		// stands on the `surface` field the input box and the status line share. A breadcrumb back
		// on `chrome` is exactly the crossed wire this field exists to keep fixed.
		{"breadcrumb fg", th.breadcrumb.GetForeground(), s.UserText},
		{"breadcrumb bg", th.breadcrumb.GetBackground(), s.Surface},
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

// TestDefaultThemeCarriesTheShippedScheme walks the PRODUCTION path — [scheme.Default] through
// [newTheme] — where the test above walks a synthetic one: the wiring test proves a role reaches
// the style it names, this one proves the slot a human actually looks at is fed by the SHIPPED
// scheme's own value for that role. A newTheme hard-coding a tone, or a Default() handing back a
// zero scheme, fails here.
//
// It asserts against `scheme.Default()` and never against a literal: the schemes stay under
// tuning, so changing a colour must never fail a test (owner call, 2026-08-11). The two things
// that must hold whatever the values are retuned to are asserted as the relations they are — the
// collapsed/open detail contrast step, and a spinner lap with stops in it.
func TestDefaultThemeCarriesTheShippedScheme(t *testing.T) {
	t.Parallel()

	def := scheme.Default()
	th := newTheme(def)

	// The stops arrive as a slice built from four roles at once rather than as a style, so the lap
	// is asserted to exist before the table indexes into it: an empty loop would crash the run
	// instead of failing it.
	if len(th.spinnerStops) == 0 {
		t.Fatal("the theme carries no spinner stops; the colour loop would have nothing to blend")
	}

	for _, tc := range []struct {
		name string
		got  color.Color
		want string
	}{
		{"errorText fg", th.errorText.GetForeground(), def.Error},
		{"selection bg", th.selection.GetBackground(), def.Selection},
		{"mode plan", th.modeColor(domain.ModePlan), def.ModePlan},
		{"mode ask-before", th.modeColor(domain.ModeAskBefore), def.ModeAskBefore},
		{"mode allow-edits", th.modeColor(domain.ModeAllowEdits), def.ModeAllowEdits},
		{"mode auto", th.modeColor(domain.ModeAuto), def.ModeAuto},
		{"raw surface", th.surface, def.Surface},
		{"raw chrome", th.chrome, def.Chrome},
		{"breadcrumb fg", th.breadcrumb.GetForeground(), def.UserText},
		{"breadcrumb bg", th.breadcrumb.GetBackground(), def.Surface},
		// The open-detail tone travels as the `muted-bright` role, sampled here because the pair it
		// forms with `muted` is checked below and a crossed wire would satisfy that check trivially.
		{"toolDetailBright fg", th.toolDetailBright.GetForeground(), def.MutedBright},
		// The one role that has to survive a conversion into the blend's own colour space on its way
		// onto the theme.
		{"first spinner stop", th.spinnerStops[0], def.Spinner1},
	} {
		// A scheme file may write its hex in either case (dark.yaml ships both) while hexOf always
		// renders lower, so the comparison is case-blind: re-casing a value is a retune too.
		if got := hexOf(tc.got); !strings.EqualFold(got, tc.want) {
			t.Errorf("%s = %s; want the default scheme's %s", tc.name, got, tc.want)
		}
	}

	// The collapsed and open detail tones are two steps of one ramp, read against each other — so
	// this holds whatever the scheme retunes them to. Let them meet and the open block loses the
	// only cue that says it is open.
	if hexOf(th.toolDetail.GetForeground()) == hexOf(th.toolDetailBright.GetForeground()) {
		t.Error("the collapsed and open detail tones resolve to the same colour; the contrast step is gone")
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
