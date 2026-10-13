package tui

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
)

// `auto-title:` names one switch and gates two namers: the session titles this Model fires itself,
// and the host's delegation namer, which sits behind the engine's Config and can be reached only by
// being told (ADR 0068). The key is applied LOCALLY — it never reaches SettingsHost.Apply — so
// without this hook a `/settings` flip would move one namer and leave the other running.
func TestSettingsApplyLocalTellsTheHostAboutAutoTitle(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "switched off", value: settingFalse},
		{name: "switched on", value: settingTrue, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var told []bool
			opts := testOpts
			opts.AutoTitle = !tc.want
			opts.OnAutoTitle = func(enabled bool) { told = append(told, enabled) }
			m := newTestModelEng(t, &fakeEngine{}, opts)

			m, note, cmd, handled, err := m.settingsApplyLocal(settingKeyAutoTitle, tc.value)
			if err != nil || !handled {
				t.Fatalf("settingsApplyLocal(%q, %q) = handled %v, err %v", settingKeyAutoTitle, tc.value, handled, err)
			}
			if note != "" || cmd != nil {
				t.Errorf("apply produced note %q and cmd %v; want the key to simply take effect", note, cmd)
			}
			if m.opts.AutoTitle != tc.want {
				t.Errorf("opts.AutoTitle = %v; want %v — the renderer's own half of the key", m.opts.AutoTitle, tc.want)
			}
			if len(told) != 1 || told[0] != tc.want {
				t.Fatalf("the host was told %v; want exactly one call carrying %v — the value the renderer parsed",
					told, tc.want)
			}
		})
	}
}

// A host that wired no hook is the ordinary case, not a degrade: every hand-built Options and every
// Driver with no delegation namer leaves it nil, and the local apply must still take effect.
func TestSettingsApplyLocalSurvivesNoAutoTitleHook(t *testing.T) {
	t.Parallel()

	opts := testOpts
	opts.AutoTitle = true
	m := newTestModelEng(t, &fakeEngine{}, opts)

	m, _, _, handled, err := m.settingsApplyLocal(settingKeyAutoTitle, settingFalse)
	if err != nil || !handled {
		t.Fatalf("settingsApplyLocal(%q, false) = handled %v, err %v", settingKeyAutoTitle, handled, err)
	}
	if m.opts.AutoTitle {
		t.Error("opts.AutoTitle survived the apply; a nil hook must not cost the renderer its own half")
	}
}

// Every `ui.*` key the renderer applies lands on the Options' block through [domain.UIPrefs.Set] —
// the one parser the config file, the registry rows and this pane share (ADR 0043) — so an apply
// moves m.opts.UI exactly as Set would, and a value Set refuses moves nothing and is reported on the
// row as the apply's own failure. The sweep is domain's key list, so a key added to the block fails
// here until the pane has an arm (and a value pair) for it.
//
// `ui.inspector` is the one key of the block left out, on purpose: settingsApplyLocal has no arm
// for it, because the key routes OUT through [SettingsHost.Apply] (cmd/apogee/wire_settings.go,
// applyMirror — the wire observer is installed while a provider client is constructed, so the
// flip is mirrored onto the host's live Options for the next Firing to read) and the renderer's own
// Inspector deliberately keeps the start-up state (the registry's `ui.inspector` row: "takes effect
// at the next start"). Its Set lands in the seam only, never in m.opts.UI — which the last check
// below pins by asking the local apply and being told the key is not its own.
func TestSettingsApplyLocalLandsEveryUIKeyThroughSet(t *testing.T) {
	t.Parallel()

	// accepted is a value off the harness's seed (testUIPrefs), so the apply has a move to make;
	// refused is one the block cannot read. The colour scheme's refusal is the resolver's, not the
	// parser's — Set takes any name — so its refused arm runs over a model with no resolve wired.
	values := map[string]struct{ accepted, refused string }{
		domain.UIKeySpinner:          {string(domain.SpinnerGlitter), "nope"},
		domain.UIKeySpinnerColor:     {settingTrue, "maybe"},
		domain.UIKeyGlyphs:           {string(domain.GlyphsSafe), "ascii"},
		domain.UIKeyShowScrollbar:    {settingFalse, "maybe"},
		domain.UIKeyColorScheme:      {"light", "light"},
		domain.UIKeyStallAfter:       {"2m", "soonish"},
		domain.UIKeySkillSuggestions: {settingTrue, "maybe"},
		domain.UIKeyTaskListOpen:     {settingFalse, "maybe"},
		domain.UIKeyToolsOpen:        {settingTrue, "maybe"},
		domain.UIKeyToolsFoldOver:    {"8", "many"},
	}
	model := func(t *testing.T, resolves bool) Model {
		t.Helper()
		opts := testOpts
		schemes := fakeSchemeHost{}
		if resolves {
			schemes.resolve = func(string) (scheme.Scheme, []string) { return stubScheme("#123456"), nil }
		}
		opts.Schemes = schemes
		return newTestModelEng(t, &fakeEngine{}, opts)
	}

	for _, key := range domain.UIKeys() {
		if key == domain.UIKeyInspector {
			continue
		}
		pair, ok := values[key]
		if !ok {
			t.Errorf("%s: the block carries a key this test has no value pair for", key)
			continue
		}
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			m := model(t, true)
			before := m.opts.UI
			want := before
			if err := want.Set(key, pair.accepted); err != nil {
				t.Fatalf("UIPrefs.Set(%q, %q) = %v; the accepted value must be one the block reads", key, pair.accepted, err)
			}
			if want == before {
				t.Fatalf("UIPrefs.Set(%q, %q) moved nothing; the accepted value must be off the seed", key, pair.accepted)
			}

			applied, _, _, handled, err := m.settingsApplyLocal(key, pair.accepted)
			if err != nil || !handled {
				t.Fatalf("settingsApplyLocal(%q, %q) = handled %v, err %v", key, pair.accepted, handled, err)
			}
			if applied.opts.UI != want {
				t.Errorf("opts.UI = %+v after the apply, want %+v — exactly what UIPrefs.Set lands", applied.opts.UI, want)
			}

			m = model(t, false)
			before = m.opts.UI
			refused, _ := m.settingsApplied(SettingRow{Path: key}, settingEdit{path: key, value: pair.refused})
			if refused.opts.UI != before {
				t.Errorf("opts.UI = %+v after a refused apply, want the seed %+v untouched", refused.opts.UI, before)
			}
			if got := refused.settings.failure.msg; !strings.HasPrefix(got, settingsApplyFailedNote) {
				t.Errorf("failure = %q, want it to open with %q — the row carries the apply's own refusal", got, settingsApplyFailedNote)
			}
		})
	}

	if _, _, _, handled, _ := (model(t, true)).settingsApplyLocal(domain.UIKeyInspector, settingTrue); handled {
		t.Error("settingsApplyLocal handled ui.inspector; the key is the seam's, not the renderer's")
	}
}

// TestGlyphsApplyRepaintsThePaintedBlocks: a `ui.glyphs` edit applied live swaps the theme's set and
// the blocks already on screen repaint in it — the memoised paints of the outgoing marks are
// dropped (paintcache.go) and the set is in the frame key, so a transcript painted with `▶` and the
// `⋯` leader comes back with `►` and `·`, and back again.
func TestGlyphsApplyRepaintsThePaintedBlocks(t *testing.T) {
	t.Parallel()

	m := glyphFrameModel(t, domain.GlyphsUnicode, domain.ModeAskBefore)
	if frame := plain(m.View()); !strings.Contains(frame, "⋯") || !strings.Contains(frame, "▶") {
		t.Fatalf("the unicode fixture paints no ⋯ leader or ▶ indicator to repaint:\n%s", frame)
	}

	for _, tc := range []struct {
		value      string
		want       glyphSet
		gone, seen []string
	}{
		{string(domain.GlyphsSafe), safeGlyphs, []string{"⋯", "▶", "✦"}, []string{"··", "►", "♦ Tools"}},
		{string(domain.GlyphsUnicode), defaultGlyphs, []string{"··", "►", "♦"}, []string{"⋯", "▶", "✦ Tools"}},
	} {
		applied, _, _, handled, err := m.settingsApplyLocal(domain.UIKeyGlyphs, tc.value)
		if err != nil || !handled {
			t.Fatalf("settingsApplyLocal(ui.glyphs, %q) = handled %v, err %v", tc.value, handled, err)
		}
		if applied.th.glyphs != tc.want {
			t.Errorf("ui.glyphs=%s seated %+v; want %+v", tc.value, applied.th.glyphs, tc.want)
		}
		applied.settle()
		frame := plain(applied.View())
		for _, mark := range tc.gone {
			if strings.Contains(frame, mark) {
				t.Errorf("after ui.glyphs=%s the frame still draws %q — a stale paint:\n%s", tc.value, mark, frame)
			}
		}
		for _, mark := range tc.seen {
			if !strings.Contains(frame, mark) {
				t.Errorf("after ui.glyphs=%s the frame carries no %q:\n%s", tc.value, mark, frame)
			}
		}
		m = applied
	}
}

// TestColorSchemeSwitchKeepsTheGlyphSet: a scheme switch rebuilds the theme (applyColorScheme), and
// the glyph set is the human's `ui.glyphs`, not the palette's — so `safe` survives the rebuild.
func TestColorSchemeSwitchKeepsTheGlyphSet(t *testing.T) {
	t.Parallel()

	opts := testOpts
	opts.UI.Glyphs = domain.GlyphsSafe
	opts.Schemes = fakeSchemeHost{resolve: func(string) (scheme.Scheme, []string) { return stubScheme("#123456"), nil }}
	m := newTestModelEng(t, &fakeEngine{}, opts)
	if m.th.glyphs != safeGlyphs {
		t.Fatalf("the fixture is not drawn in the safe set: %+v", m.th.glyphs)
	}
	applied, _, _, handled, err := m.settingsApplyLocal(domain.UIKeyColorScheme, "light")
	if err != nil || !handled {
		t.Fatalf("settingsApplyLocal(ui.color-scheme, light) = handled %v, err %v", handled, err)
	}
	if applied.th.glyphs != safeGlyphs {
		t.Errorf("a scheme switch under ui.glyphs: safe left the theme drawing %+v; want the safe set", applied.th.glyphs)
	}
}
