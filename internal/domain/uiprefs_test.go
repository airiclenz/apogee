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
}

// TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase pins the sweep the other tests run on: the
// ten keys in the order the starter template presents them, each with a case above, and a slice
// a caller cannot reorder under this package.
func TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase(t *testing.T) {
	t.Parallel()

	want := []string{
		"ui.spinner", "ui.spinner-color", "ui.show-scrollbar", "ui.color-scheme", "ui.stall-after",
		"ui.inspector", "ui.skill-suggestions", "ui.task-list-open", "ui.tools-open", "ui.tools-fold-over",
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
// value read through Set would be — the same three sentences.
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
	} {
		err := tt.u.Validate()
		if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
			t.Errorf("%s: Validate() = %v; want it to start with %q", tt.name, err, tt.want)
		}
	}
}
