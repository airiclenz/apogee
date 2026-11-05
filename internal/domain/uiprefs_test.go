package domain

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// UIPrefs is the `ui:` block as one value with one parser, so what these tests hold is the
// parser's contract for every key the block carries: a documented value lands its typed field and
// nothing else, `""` lands the key's default, a bad value is refused in the sentence every surface
// already refuses it in and leaves the receiver untouched, and a key the block does not carry is
// refused. Sweeping UIKeys() rather than a hand-written list is what keeps a key added to the
// block from landing unpinned.

// uiKeyCases is one row per key: a value that lands, the field it lands on (read back off the
// edited block), and the values the key refuses with the sentence each refusal must carry.
type uiKeyCase struct {
	value string
	read  func(UIPrefs) any
	want  any
	bad   map[string]string
}

var uiKeyCases = map[string]uiKeyCase{
	UIKeySpinner: {
		value: "glitter",
		read:  func(u UIPrefs) any { return u.Spinner },
		want:  SpinnerGlitter,
		bad:   map[string]string{"twirl": `apogee: invalid ui.spinner: unknown spinner style "twirl"`},
	},
	UIKeySpinnerColor: {
		value: "false",
		read:  func(u UIPrefs) any { return u.SpinnerColor },
		want:  false,
		bad:   map[string]string{"maybe": `apogee: invalid ui.spinner-color "maybe": want true or false`},
	},
	UIKeyGlyphs: {
		value: "safe",
		read:  func(u UIPrefs) any { return u.Glyphs },
		want:  GlyphsSafe,
		bad:   map[string]string{"ascii": `apogee: invalid ui.glyphs: unknown glyph set "ascii"`},
	},
	UIKeyShowScrollbar: {
		value: "false",
		read:  func(u UIPrefs) any { return u.ShowScrollbar },
		want:  false,
		bad:   map[string]string{"maybe": `apogee: invalid ui.show-scrollbar "maybe": want true or false`},
	},
	UIKeyColorScheme: {
		value: "light",
		read:  func(u UIPrefs) any { return u.ColorScheme },
		want:  "light",
	},
	UIKeyStallAfter: {
		value: "2m",
		read:  func(u UIPrefs) any { return u.StallAfter },
		want:  2 * time.Minute,
		bad: map[string]string{
			"soonish": `apogee: invalid ui.stall-after "soonish": want a length of time like 90s or 2m, or 0 to turn the quiet qualifier off`,
			"90":      `apogee: invalid ui.stall-after "90": want a length of time like 90s or 2m, or 0 to turn the quiet qualifier off`,
			"-5s":     `apogee: invalid ui.stall-after -5s: want 0 or more, where 0 turns the quiet qualifier off`,
		},
	},
	UIKeyInspector: {
		value: "true",
		read:  func(u UIPrefs) any { return u.Inspector },
		want:  true,
		bad:   map[string]string{"maybe": `apogee: invalid ui.inspector "maybe": want true or false`},
	},
	UIKeySkillSuggestions: {
		value: "false",
		read:  func(u UIPrefs) any { return u.SkillSuggestions },
		want:  false,
		bad:   map[string]string{"maybe": `apogee: invalid ui.skill-suggestions "maybe": want true or false`},
	},
	UIKeyTaskListOpen: {
		value: "false",
		read:  func(u UIPrefs) any { return u.TaskListOpen },
		want:  false,
		bad:   map[string]string{"maybe": `apogee: invalid ui.task-list-open "maybe": want true or false`},
	},
	UIKeyToolsOpen: {
		value: "true",
		read:  func(u UIPrefs) any { return u.ToolsOpen },
		want:  true,
		bad:   map[string]string{"maybe": `apogee: invalid ui.tools-open "maybe": want true or false`},
	},
	UIKeyToolsFoldOver: {
		value: "12",
		read:  func(u UIPrefs) any { return u.ToolsFoldOver },
		want:  12,
		bad: map[string]string{
			"five": `apogee: invalid ui.tools-fold-over "five": want a number of type rows (0 never folds)`,
			"-1":   `apogee: invalid ui.tools-fold-over -1: want 0 or more, where 0 never folds a Tools umbrella`,
		},
	},
	UIKeyNotify: {
		value: "osc9",
		read:  func(u UIPrefs) any { return u.Notify },
		want:  NotifyOSC9,
		bad:   map[string]string{"beep": `apogee: invalid ui.notify: unknown notify mode "beep"`},
	},
	UIKeyGitFetch: {
		value: "false",
		read:  func(u UIPrefs) any { return u.GitFetch },
		want:  false,
		bad:   map[string]string{"maybe": `apogee: invalid ui.git-fetch "maybe": want true or false`},
	},
	UIKeyPointerShapes: {
		value: "true",
		read:  func(u UIPrefs) any { return u.PointerShapes },
		want:  true,
		bad:   map[string]string{"maybe": `apogee: invalid ui.pointer-shapes "maybe": want true or false`},
	},
}

// TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase pins the sweep the other tests run on: the
// fourteen keys in the order the starter template presents them, each with a case above, and a slice
// a caller cannot reorder under this package.
func TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase(t *testing.T) {
	t.Parallel()

	want := []string{
		"ui.spinner", "ui.spinner-color", "ui.glyphs", "ui.show-scrollbar", "ui.color-scheme",
		"ui.stall-after", "ui.inspector", "ui.skill-suggestions", "ui.task-list-open", "ui.tools-open",
		"ui.tools-fold-over", "ui.pointer-shapes", "ui.notify", "ui.git-fetch",
	}
	if got := UIKeys(); !slices.Equal(got, want) {
		t.Errorf("UIKeys() = %v; want the template order %v", got, want)
	}
	for _, key := range UIKeys() {
		if _, ok := uiKeyCases[key]; !ok {
			t.Errorf("no test case for %s", key)
		}
	}
	if len(uiKeyCases) != len(UIKeys()) {
		t.Errorf("%d cases for %d keys", len(uiKeyCases), len(UIKeys()))
	}

	handed := UIKeys()
	handed[0] = "ui.nothing"
	if got := UIKeys()[0]; got != UIKeySpinner {
		t.Errorf("reordering the handed-out slice changed UIKeys()[0] to %q", got)
	}
}

// TestUIPrefsSetLandsEveryKey: for every key, a documented value lands its typed field, moves no
// other field, and `""` lands the key's default back.
func TestUIPrefsSetLandsEveryKey(t *testing.T) {
	t.Parallel()
	for _, key := range UIKeys() {
		tc := uiKeyCases[key]
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			u := DefaultUIPrefs()
			if err := u.Set(key, "  "+tc.value+"  "); err != nil {
				t.Fatalf("Set(%s, %q): %v", key, tc.value, err)
			}
			if got := tc.read(u); got != tc.want {
				t.Errorf("Set(%s, %q) landed %v; want %v", key, tc.value, got, tc.want)
			}
			restored := u
			if err := restored.Set(key, ""); err != nil {
				t.Fatalf("Set(%s, \"\"): %v", key, err)
			}
			if restored != DefaultUIPrefs() {
				t.Errorf("Set(%s, %q) then Set(%s, \"\") = %+v; want the defaults %+v — the value moved a neighbour, or \"\" is not the default",
					key, tc.value, key, restored, DefaultUIPrefs())
			}
		})
	}
}

// TestUIPrefsSetRefusesInThePinnedSentences: every bad value is refused in the sentence the
// surfaces already refuse it in, and a refused Set leaves the receiver exactly as it was.
func TestUIPrefsSetRefusesInThePinnedSentences(t *testing.T) {
	t.Parallel()
	for _, key := range UIKeys() {
		for value, want := range uiKeyCases[key].bad {
			t.Run(key+"="+value, func(t *testing.T) {
				t.Parallel()

				u := DefaultUIPrefs()
				u.ColorScheme = "light" // a non-default block, so "untouched" is not "still zero"
				before := u
				err := u.Set(key, value)

				if err == nil {
					t.Fatalf("Set(%s, %q) accepted; want %s", key, value, want)
				}
				if !strings.HasPrefix(err.Error(), want) {
					t.Errorf("Set(%s, %q) = %q; want it to start with %q", key, value, err, want)
				}
				if u != before {
					t.Errorf("a refused Set moved the block: %+v; want %+v", u, before)
				}
			})
		}
	}
}

// TestUIPrefsSetRefusesAnUnknownKey: a key the block does not carry is refused, and the receiver
// is untouched.
func TestUIPrefsSetRefusesAnUnknownKey(t *testing.T) {
	t.Parallel()

	u := DefaultUIPrefs()
	err := u.Set("ui.nothing", "true")

	if err == nil || !strings.Contains(err.Error(), `unknown ui key "ui.nothing"`) {
		t.Errorf("Set(ui.nothing) = %v; want the unknown-key refusal", err)
	}
	if u != DefaultUIPrefs() {
		t.Errorf("an unknown key moved the block: %+v", u)
	}
}

// TestParseStallAfterAndToolsFoldOverDefaults pins the two parsers' "" and 0 answers: the seam's
// rule (an empty value is an absent key, an explicit 0 is "off"/"never") moved here with them.
func TestParseStallAfterAndToolsFoldOverDefaults(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		text string
		want time.Duration
	}{{"", DefaultStallAfter}, {"  ", DefaultStallAfter}, {"0", 0}, {"120s", 120 * time.Second}, {" 1m30s ", 90 * time.Second}} {
		got, err := ParseStallAfter(tt.text)
		if err != nil || got != tt.want {
			t.Errorf("ParseStallAfter(%q) = %s, %v; want %s", tt.text, got, err, tt.want)
		}
	}
	for _, tt := range []struct {
		text string
		want int
	}{{"", DefaultToolsFoldOver}, {"0", 0}, {" 7 ", 7}} {
		got, err := ParseToolsFoldOver(tt.text)
		if err != nil || got != tt.want {
			t.Errorf("ParseToolsFoldOver(%q) = %d, %v; want %d", tt.text, got, err, tt.want)
		}
	}
}

// TestUIPrefsValidateSpeaksTheParsersSentences: a block assembled field by field is refused as a
// value read through Set would be — the same five sentences.
func TestUIPrefsValidateSpeaksTheParsersSentences(t *testing.T) {
	t.Parallel()

	if err := DefaultUIPrefs().Validate(); err != nil {
		t.Errorf("the defaults do not validate: %v", err)
	}
	for _, tt := range []struct {
		name string
		u    UIPrefs
		want string
	}{
		{"an unknown spinner", UIPrefs{Spinner: "twirl"}, `apogee: invalid ui.spinner: unknown spinner style "twirl"`},
		{"a negative wait", UIPrefs{StallAfter: -5 * time.Second}, `apogee: invalid ui.stall-after -5s: want 0 or more`},
		{"a negative count", UIPrefs{ToolsFoldOver: -1}, `apogee: invalid ui.tools-fold-over -1: want 0 or more`},
		{"an unknown glyph set", UIPrefs{Glyphs: "ascii"}, `apogee: invalid ui.glyphs: unknown glyph set "ascii" (known sets: unicode, safe)`},
		{"an unknown notify mode", UIPrefs{Notify: "beep"}, `apogee: invalid ui.notify: unknown notify mode "beep" (known modes: off, bell, osc9, both)`},
	} {
		err := tt.u.Validate()
		if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
			t.Errorf("%s: Validate() = %v; want it to start with %q", tt.name, err, tt.want)
		}
	}
}

// TestUIPrefsGlyphsDefaultsToUnicodeAndKnowsTwoSets: the block's glyph set is unicode by default,
// `""` reads as unicode (a block validated one key at a time leaves Glyphs zero), safe is the other
// set, and any other name is refused naming the key.
func TestUIPrefsGlyphsDefaultsToUnicodeAndKnowsTwoSets(t *testing.T) {
	t.Parallel()

	if got := DefaultUIPrefs().Glyphs; got != GlyphsUnicode {
		t.Errorf("DefaultUIPrefs().Glyphs = %q; want %q", got, GlyphsUnicode)
	}
	for _, tt := range []struct {
		text string
		want GlyphSet
	}{
		{"", GlyphsUnicode},
		{"unicode", GlyphsUnicode},
		{"safe", GlyphsSafe},
	} {
		got, err := ParseGlyphSet(tt.text)
		if err != nil || got != tt.want {
			t.Errorf("ParseGlyphSet(%q) = %q, %v; want %q", tt.text, got, err, tt.want)
		}
		u := DefaultUIPrefs()
		u.Glyphs = GlyphSet(tt.text)
		if err := u.Validate(); err != nil {
			t.Errorf("Validate() with Glyphs %q = %v; want accepted", tt.text, err)
		}
	}
	for _, bad := range []string{"ascii", "Safe", "UNICODE", " safe"} {
		u := DefaultUIPrefs()
		u.Glyphs = GlyphSet(bad)
		err := u.Validate()
		if err == nil || !strings.HasPrefix(err.Error(), "apogee: invalid ui.glyphs: ") {
			t.Errorf("Validate() with Glyphs %q = %v; want refused naming ui.glyphs", bad, err)
		}
	}
}

// TestUIPrefsNotifyDefaultsToBellAndReadsEmptyAsOff: the shipped default is the bell, carried by
// DefaultUIPrefs alone; `""` parses as off, so a zero block (a test harness's, a hand-built
// Driver's) stays silent and still validates; Set's `""` lands the shipped default, the contract
// every key keeps; each of the four modes parses to itself; and any other name is refused naming
// the key.
func TestUIPrefsNotifyDefaultsToBellAndReadsEmptyAsOff(t *testing.T) {
	t.Parallel()

	if got := DefaultUIPrefs().Notify; got != NotifyBell {
		t.Errorf("DefaultUIPrefs().Notify = %q; want %q", got, NotifyBell)
	}
	if err := (UIPrefs{}).Validate(); err != nil {
		t.Errorf("the zero block does not validate: %v", err)
	}
	for _, tt := range []struct {
		text string
		want NotifyMode
	}{
		{"", NotifyOff},
		{"off", NotifyOff},
		{"bell", NotifyBell},
		{"osc9", NotifyOSC9},
		{"both", NotifyBoth},
	} {
		got, err := ParseNotifyMode(tt.text)
		if err != nil || got != tt.want {
			t.Errorf("ParseNotifyMode(%q) = %q, %v; want %q", tt.text, got, err, tt.want)
		}
		u := DefaultUIPrefs()
		u.Notify = NotifyMode(tt.text)
		if err := u.Validate(); err != nil {
			t.Errorf("Validate() with Notify %q = %v; want accepted", tt.text, err)
		}
	}
	if got := NotifyModeNames(); !slices.Equal(got, []NotifyMode{NotifyOff, NotifyBell, NotifyOSC9, NotifyBoth}) {
		t.Errorf("NotifyModeNames() = %v; want off, bell, osc9, both", got)
	}

	u := UIPrefs{Notify: NotifyBoth}
	if err := u.Set(UIKeyNotify, ""); err != nil || u.Notify != NotifyBell {
		t.Errorf(`Set(ui.notify, "") = %v, landing %q; want the shipped default %q`, err, u.Notify, NotifyBell)
	}

	for _, bad := range []string{"beep", "Bell", "OFF", "osc 9"} {
		u := DefaultUIPrefs()
		u.Notify = NotifyMode(bad)
		err := u.Validate()
		if err == nil || !strings.HasPrefix(err.Error(), "apogee: invalid ui.notify: ") {
			t.Errorf("Validate() with Notify %q = %v; want refused naming ui.notify", bad, err)
		}
	}
}

// TestUIPrefsFooterRowsDefaultToTwoRows: the shipped default is two rows — the upstream with the
// mode marker at its right edge, above the workspace — carried by DefaultUIPrefs; `""` reads as that
// default, so the zero block still validates; and Set's `""` lands it, the contract every key keeps.
func TestUIPrefsFooterRowsDefaultToTwoRows(t *testing.T) {
	t.Parallel()
	want := [][]FooterSegment{
		{FooterHost, FooterModel, FooterEffort, FooterMode},
		{FooterWorkdir, FooterGit, FooterSpend},
	}

	got, err := ParseFooterRows(string(DefaultUIPrefs().FooterRows))

	if err != nil || !slices.EqualFunc(got, want, slices.Equal[[]FooterSegment]) {
		t.Errorf("ParseFooterRows(DefaultUIPrefs().FooterRows) = %v, %v; want %v", got, err, want)
	}
	if got, err := ParseFooterRows(""); err != nil || !slices.EqualFunc(got, want, slices.Equal[[]FooterSegment]) {
		t.Errorf(`ParseFooterRows("") = %v, %v; want the default %v`, got, err, want)
	}
	if err := (UIPrefs{}).Validate(); err != nil {
		t.Errorf("the zero block does not validate: %v", err)
	}
	u := UIPrefs{FooterRows: "[[mode]]"}
	if err := u.Set(UIKeyFooterRows, ""); err != nil || u.FooterRows != DefaultFooterRows {
		t.Errorf(`Set(ui.footer.rows, "") = %v, landing %q; want the default %q`, err, u.FooterRows, DefaultFooterRows)
	}
}

// TestUIPrefsSetLandsFooterRowsInTheirCanonicalSpelling: the registry row's Read text is the
// canonical spelling, and Set lands it back to the same value — and a looser spelling of the same
// rows lands as that one canonical text, so two spellings never compare unequal.
func TestUIPrefsSetLandsFooterRowsInTheirCanonicalSpelling(t *testing.T) {
	t.Parallel()
	const twoRows FooterRows = "[[host, model, effort, mode], [workdir, git, spend]]"

	for _, text := range []string{string(twoRows), " [ [host,model,effort,mode] ,[ workdir , git,spend ] ] "} {
		u := DefaultUIPrefs()

		err := u.Set(UIKeyFooterRows, text)

		if err != nil || u.FooterRows != twoRows {
			t.Errorf("Set(ui.footer.rows, %q) = %v, landing %q; want %q", text, err, u.FooterRows, twoRows)
		}
	}
	if got := SpellFooterRows([][]string{{"host", "mode"}, {"git"}}); got != "[[host, mode], [git]]" {
		t.Errorf("SpellFooterRows = %q; want [[host, mode], [git]]", got)
	}
}

// TestUIPrefsValidateRefusesEveryBadFooterShape: each shape the key refuses is refused by Validate
// in a sentence naming the key and quoting the value, and Set refusing it leaves the block as it was.
func TestUIPrefsValidateRefusesEveryBadFooterShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, rows, reason string
	}{
		{"no rows", "[]", "want 1 or 2 rows, not 0"},
		{"three rows", "[[host], [model], [mode]]", "want 1 or 2 rows, not 3"},
		{"empty row", "[[host, mode], []]", "row 2 is empty"},
		{"unknown segment", "[[host, clock, mode]]", `unknown segment "clock"`},
		{"repeated segment", "[[host, mode], [host]]", `segment "host" is named twice`},
		{"two modes", "[[mode], [mode]]", `segment "mode" is named twice`},
		{"no mode", "[[host, model]]", `want "mode" in exactly one row`},
		{"not a list of rows", "host, mode", "want a list of 1 or 2 rows"},
		{"flat list", "[host, mode]", "want a list of 1 or 2 rows"},
		{"nested too deep", "[[[host], mode]]", "want a list of 1 or 2 rows"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u := DefaultUIPrefs()
			u.FooterRows = FooterRows(tt.rows)

			err := u.Validate()

			wantPrefix := "apogee: invalid ui.footer.rows " + `"` + tt.rows + `": `
			if err == nil || !strings.HasPrefix(err.Error(), wantPrefix) || !strings.Contains(err.Error(), tt.reason) {
				t.Errorf("Validate() with FooterRows %q = %v; want %q naming %q", tt.rows, err, wantPrefix, tt.reason)
			}
			held := DefaultUIPrefs()
			if err := held.Set(UIKeyFooterRows, tt.rows); err == nil || held != DefaultUIPrefs() {
				t.Errorf("Set(ui.footer.rows, %q) = %v, leaving %+v; want refused and untouched", tt.rows, err, held)
			}
		})
	}
}

// TestUIPrefsFooterColorsDefaultToNoOverrides: the zero overrides are the default — every segment
// keeps its own tone, the canonical spelling is `{}`, and both `""` and `{}` read back to it.
func TestUIPrefsFooterColorsDefaultToNoOverrides(t *testing.T) {
	t.Parallel()

	colors := DefaultUIPrefs().FooterColors

	if colors != (FooterColors{}) || colors.String() != "{}" {
		t.Errorf("DefaultUIPrefs().FooterColors = %q; want no overrides, spelled {}", colors)
	}
	for _, text := range []string{"", " {} ", "{ }"} {
		if got, err := ParseFooterColors(text); err != nil || got != (FooterColors{}) {
			t.Errorf("ParseFooterColors(%q) = %q, %v; want no overrides", text, got, err)
		}
	}
	if got := colors.Role(FooterModel); got != "" {
		t.Errorf("Role(model) with no overrides = %q; want \"\"", got)
	}
}

// TestUIPrefsSetLandsFooterColorsInTheirCanonicalSpelling: the registry row's Read text is the
// canonical spelling — the overridden targets in vocabulary order — and Set lands it back to the
// same value; a looser spelling in another order lands as that one value, and each target answers
// the role it names.
func TestUIPrefsSetLandsFooterColorsInTheirCanonicalSpelling(t *testing.T) {
	t.Parallel()
	const canonical = "{host: muted-bright, model: success, branch: warning, spend: error}"

	for _, text := range []string{canonical, "{ spend:error,branch : warning, host: muted-bright, model:success }"} {
		u := DefaultUIPrefs()

		err := u.Set(UIKeyFooterColors, text)

		if err != nil || u.FooterColors.String() != canonical {
			t.Errorf("Set(ui.footer.colors, %q) = %v, landing %q; want %q", text, err, u.FooterColors, canonical)
		}
		roles := map[FooterSegment]string{FooterHost: "muted-bright", FooterModel: "success",
			FooterEffort: "", FooterWorkdir: "", FooterBranch: "warning", FooterSpend: "error", FooterMode: ""}
		for segment, want := range roles {
			if got := u.FooterColors.Role(segment); got != want {
				t.Errorf("Role(%s) = %q; want %q", segment, got, want)
			}
		}
		if err := u.Set(UIKeyFooterColors, ""); err != nil || u != DefaultUIPrefs() {
			t.Errorf(`Set(ui.footer.colors, "") = %v, leaving %+v; want the defaults`, err, u)
		}
	}
}

// TestUIPrefsSetRefusesEveryBadFooterColors: each value the key refuses is refused in a sentence
// naming the key and quoting the value, and a refused Set leaves the block as it was.
func TestUIPrefsSetRefusesEveryBadFooterColors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, colors, reason string
	}{
		{"the mode marker", "{mode: success}", `segment "mode" cannot be overridden`},
		{"the offline marker", "{offline: success}", `segment "offline" cannot be overridden`},
		{"an unknown segment", "{clock: success}", `unknown segment "clock"`},
		{"the whole git segment", "{git: success}", `unknown segment "git"`},
		{"a segment twice", "{model: success, model: error}", `segment "model" is named twice`},
		{"no role", "{model: }", `segment "model" names no role`},
		{"no braces", "model: success", "want a map of segment names to scheme role names"},
		{"no colon", "{model success}", "want a map of segment names to scheme role names"},
		{"nested", "{model: {fg: success}}", "want a map of segment names to scheme role names"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u := DefaultUIPrefs()

			err := u.Set(UIKeyFooterColors, tt.colors)

			wantPrefix := "apogee: invalid ui.footer.colors " + `"` + tt.colors + `": `
			if err == nil || !strings.HasPrefix(err.Error(), wantPrefix) || !strings.Contains(err.Error(), tt.reason) {
				t.Errorf("Set(ui.footer.colors, %q) = %v; want %q naming %q", tt.colors, err, wantPrefix, tt.reason)
			}
			if u != DefaultUIPrefs() {
				t.Errorf("a refused Set moved the block to %+v", u)
			}
		})
	}
}
