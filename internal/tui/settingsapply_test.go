package tui

import (
	"testing"
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
