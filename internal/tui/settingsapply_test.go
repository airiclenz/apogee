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
// applyInspector — the wire observer is installed while a provider client is constructed, so the
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
