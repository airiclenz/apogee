package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestRegistryIsBijectionWithFileConfig is the anti-drift core of the key registry: it walks
// fileConfig's yaml tags with reflection and asserts the registry describes EXACTLY that
// schema — every leaf key has a row, and no row names a path the schema does not have. A row
// of kind structured terminates the descent, which is what makes "one row per block" a
// legitimate answer for a list, a map, a nested block or a multi-line text value rather than
// a hole in the coverage.
//
// Adding a key to fileConfig without describing it here therefore fails, which is the whole
// point: the /settings surface renders from the registry, so an undescribed key would be a
// key the user cannot see.
func TestRegistryIsBijectionWithFileConfig(t *testing.T) {
	t.Parallel()

	described := map[string]bool{}
	walkSchema(t, reflect.TypeOf(fileConfig{}), "", described)

	for _, k := range KeyRegistry {
		if !described[k.Path] {
			t.Errorf("registry row %q names a path fileConfig does not have (renamed or removed key?)", k.Path)
		}
	}
}

// walkSchema recurses over a config struct's yaml tags — read through schemaKeys, the reader the
// unknown-key walk shares (unknownkeys.go) — recording into described every path the registry
// accounts for and failing for every leaf it does not.
func walkSchema(t *testing.T, typ reflect.Type, prefix string, described map[string]bool) {
	t.Helper()
	for _, sk := range schemaKeys(typ) {
		field, name := sk.field, sk.key
		if name == "" {
			t.Errorf("%s.%s has no yaml tag, so its on-disk key cannot be described", typ.Name(), field.Name)
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		if row, ok := LookupKey(path); ok {
			described[path] = true
			if !kindMatchesType(row.Kind, field.Type) {
				t.Errorf("registry row %q is kind %q but %s.%s is a %s", path, row.Kind, typ.Name(), field.Name,
					field.Type)
			}
			continue // a described key terminates the descent, structured blocks included
		}
		if deref := derefType(field.Type); deref.Kind() == reflect.Struct {
			walkSchema(t, deref, path, described)
			continue
		}
		t.Errorf("config key %q (%s.%s) has no registry row — add one so /settings can show it",
			path, typ.Name(), field.Name)
	}
}

// kindMatchesType says whether a row's declared kind can honestly describe the Go type the
// schema holds the key in — the second half of the drift guard: a bool key retyped to a
// string is caught even though its path did not change. Pointers are transparent (a *bool is
// the schema's way of distinguishing an explicit `false` from an absent key), and kind
// structured accepts a plain string as well as the composite types, because the Go type of a
// value is not what makes it structured — what makes it structured is that no field edits it.
// KindText is the string whose value is multi-line prose: the same Go type as KindString and a
// different editor, which is the distinction the surface acts on. A string LIST is a slice, and
// the one kind whose Go type says nothing about its ELEMENTS — that a name list holds names and
// not blocks is what KindStringList asserts, and what the writer's own round-trip proves.
func kindMatchesType(kind Kind, typ reflect.Type) bool {
	typ = derefType(typ)
	switch kind {
	case KindBool:
		return typ.Kind() == reflect.Bool
	case KindInt:
		return typ.Kind() == reflect.Int
	case KindFloat:
		return typ.Kind() == reflect.Float64
	case KindString, KindEnum, KindServer, KindScheme, KindText:
		return typ.Kind() == reflect.String
	case KindStringList:
		return typ.Kind() == reflect.Slice && derefType(typ.Elem()).Kind() == reflect.String
	case KindStructured:
		switch typ.Kind() {
		case reflect.Slice, reflect.Map, reflect.Struct, reflect.String:
			return true
		}
	}
	return false
}

// TestRegistryEnumValuesMatchParseSites pins each enum row's vocabulary to the function that
// actually validates the key at startup — all three of them in internal/domain, which owns the
// words a config file is spelled with. Each subtest checks the bijection in both directions:
// every value the registry offers is accepted by the parse site, and every value the parse site
// knows is offered. A style added to the vocabulary and not to the table therefore fails here,
// instead of silently going unofferable in /settings.
func TestRegistryEnumValuesMatchParseSites(t *testing.T) {
	t.Parallel()

	t.Run("mode", func(t *testing.T) {
		t.Parallel()
		values := enumValues(t, "mode")
		for _, v := range values {
			if _, err := domain.ParseMode(v); err != nil {
				t.Errorf("registry offers mode %q but domain.ParseMode rejects it: %v", v, err)
			}
		}
		// Completeness against the ladder constants the parser switches on.
		for _, m := range []string{string(domain.ModePlan), string(domain.ModeAskBefore), string(domain.ModeAllowEdits), string(domain.ModeAuto)} {
			if !slices.Contains(values, m) {
				t.Errorf("mode %q is a known autonomy mode but the registry does not offer it", m)
			}
		}
	})

	t.Run("ui.spinner", func(t *testing.T) {
		t.Parallel()
		values := enumValues(t, "ui.spinner")
		for _, v := range values {
			if _, err := domain.ParseSpinnerStyle(v); err != nil {
				t.Errorf("registry offers spinner %q but domain.ParseSpinnerStyle rejects it: %v", v, err)
			}
		}
		for _, style := range domain.SpinnerStyleNames() {
			if !slices.Contains(values, string(style)) {
				t.Errorf("domain knows spinner style %q but the registry does not offer it", style)
			}
		}
	})

	t.Run("sub-agents-choice", func(t *testing.T) {
		t.Parallel()
		// The one enum whose parse site is in THIS package: who picks a delegation's seat is a fact
		// about the config rather than about the agent loop, so the vocabulary lives beside Options
		// and validateSubAgentsChoice is what a value is admitted by. Both directions, as above.
		values := enumValues(t, "sub-agents-choice")
		for _, v := range values {
			if err := validateSubAgentsChoice(v); err != nil {
				t.Errorf("registry offers sub-agents-choice %q but validateSubAgentsChoice rejects it: %v", v, err)
			}
		}
		for _, c := range []SubAgentsChoice{SubAgentsChoiceFixed, SubAgentsChoiceModel} {
			if !slices.Contains(values, string(c)) {
				t.Errorf("%q is a seat-choice the config knows but the registry does not offer it", c)
			}
		}
	})

	t.Run("cursor-shape", func(t *testing.T) {
		t.Parallel()
		values := enumValues(t, "cursor-shape")
		for _, v := range values {
			if !domain.ValidCursorShapeName(v) {
				t.Errorf("registry offers cursor shape %q but domain.ValidCursorShapeName rejects it", v)
			}
		}
		for _, name := range domain.CursorShapeNames() {
			if !slices.Contains(values, name) {
				t.Errorf("domain knows cursor shape %q but the registry does not offer it", name)
			}
		}
	})
}

// enumValues returns the registry row's vocabulary, failing when the row is missing or is
// not an enum at all — so a kind change is reported here rather than as an empty loop.
func enumValues(t *testing.T, path string) []string {
	t.Helper()
	row, ok := LookupKey(path)
	if !ok {
		t.Fatalf("no registry row for %q", path)
	}
	if row.Kind != KindEnum {
		t.Fatalf("registry row %q is kind %q, want %q", path, row.Kind, KindEnum)
	}
	if len(row.EnumValues) == 0 {
		t.Fatalf("registry row %q is an enum with no values", path)
	}
	return row.EnumValues
}

// TestRegistryRowInvariants pins the properties every surface reading the registry relies on,
// so a new row cannot half-describe a key: unique paths, a description for every row, editing
// only where an in-place editor exists, an enum vocabulary that includes the row's own
// default, and masking confined to the one secret the schema carries.
func TestRegistryRowInvariants(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, k := range KeyRegistry {
		if seen[k.Path] {
			t.Errorf("duplicate registry row for %q", k.Path)
		}
		seen[k.Path] = true

		if strings.TrimSpace(k.Desc) == "" {
			t.Errorf("registry row %q has no description — /settings would show a blank line", k.Path)
		}
		if k.Editable && k.Kind == KindStructured {
			t.Errorf("registry row %q is editable but structured — v1 has no editor for a block", k.Path)
		}
		// No row is masked since ADR 0036 retired the top-level `api-key:`: the schema's one secret
		// is a `servers:` entry's own key, nested inside a structured block the pane summarizes as a
		// count. A masked row appearing again means a secret has been given a surface of its own.
		if k.Masked {
			t.Errorf("registry row %q is masked; no top-level key carries a secret any more", k.Path)
		}
		switch k.Kind {
		case KindEnum:
			if len(k.EnumValues) == 0 {
				t.Errorf("registry row %q is an enum with no values", k.Path)
			}
			if k.Default != "" && !slices.Contains(k.EnumValues, k.Default) {
				t.Errorf("registry row %q defaults to %q, which is not one of its values %v", k.Path,
					k.Default, k.EnumValues)
			}
		default:
			if len(k.EnumValues) != 0 {
				t.Errorf("registry row %q is kind %q but carries enum values %v", k.Path, k.Kind, k.EnumValues)
			}
		}
		if k.FlagName != "" && k.EnvVar == "" {
			t.Errorf("registry row %q has a flag but no env var; every flag-settable key has both", k.Path)
		}
	}
}

// TestRegistryRowsProjectEveryValue is the projection half of the anti-drift guard, and it replaces
// the three per-key cover tests the display tables used to need (one per table, each walking the
// registry to prove a map had an entry for every row). With the projections ON the row there is no
// second table to cover: what is left to assert is that no row half-describes its value, which is
// one property of the registry rather than three properties of the binary.
//
// Every row reads. A KindText row carries its prose and no other row does — a text key with no
// prose would open its editor on an empty field and offer to overwrite the prompt with what was
// typed into it, and prose for a key whose row shows its whole value is a second answer to a
// question the row already answers. The same biconditional for a structured row's lossless value:
// without it a re-read would diff the block by its summary and miss every change that summarizes
// alike.
func TestRegistryRowsProjectEveryValue(t *testing.T) {
	t.Parallel()

	for _, k := range KeyRegistry {
		if k.Read == nil {
			t.Errorf("registry row %q does not read its value — a surface would show it blank", k.Path)
		}
		if (k.Text != nil) != (k.Kind == KindText) {
			t.Errorf("registry row %q is kind %q but carries prose = %v — the raw value is carried for "+
				"exactly the text keys", k.Path, k.Kind, k.Text != nil)
		}
		if (k.Structure != nil) != (k.Kind == KindStructured) {
			t.Errorf("registry row %q is kind %q but carries a structure = %v — the lossless value is "+
				"carried for exactly the structured keys", k.Path, k.Kind, k.Structure != nil)
		}
	}
}

// The suggestion band's row is a bool that defaults ON and is editable, which is the whole of what
// the key promises a surface: /settings offers it, an untouched config paints the band, and the row
// reads back what THIS session resolved rather than the declared default (ADR 0061). The row's
// live apply is the renderer's own (internal/tui's settingsApplyLocal), so what is asserted here is
// only the description a surface renders it from.
func TestSkillSuggestionsRowIsAnEditableBoolDefaultingOn(t *testing.T) {
	t.Parallel()

	row, ok := LookupKey("ui.skill-suggestions")
	if !ok {
		t.Fatal("no registry row for ui.skill-suggestions; /settings could not show the key at all")
	}
	if row.Kind != KindBool {
		t.Errorf("kind = %q, want %q", row.Kind, KindBool)
	}
	if row.Default != "true" {
		t.Errorf("default = %q, want \"true\" — a config that names nothing paints the band", row.Default)
	}
	if !row.Editable {
		t.Error("the row is not editable; the knob is live from /settings (ADR 0037)")
	}

	off := Options{UI: UISettings{SkillSuggestions: false}}
	if got := row.Read(off); got != "false" {
		t.Errorf("read of a session with the band off = %q, want \"false\"", got)
	}
	if got := row.Read(Options{UI: defaultUISettings()}); got != "true" {
		t.Errorf("read of an unconfigured session = %q, want \"true\"", got)
	}
}

// The task-list fold's row is a bool that defaults ON and is editable — the same promise the
// suggestion band's row makes, plus the one that makes this key different: the fold gesture in the
// transcript WRITES it (ADR 0035 addendum), so the row has to be one /settings can show and a
// hand-edit can flip, and it reads back what THIS session resolved rather than the declared
// default. The live apply and the write-back are the renderer's own (item 4 of the plan); what is
// asserted here is the row a surface renders it from.
func TestTaskListOpenRowIsAnEditableBoolDefaultingOn(t *testing.T) {
	t.Parallel()

	row, ok := LookupKey("ui.task-list-open")
	if !ok {
		t.Fatal("no registry row for ui.task-list-open; /settings could not show the key at all")
	}
	if row.Kind != KindBool {
		t.Errorf("kind = %q, want %q", row.Kind, KindBool)
	}
	if row.Default != "true" {
		t.Errorf("default = %q, want \"true\" — a config that names nothing starts the task-list cards open", row.Default)
	}
	if !row.Editable {
		t.Error("the row is not editable; the knob is live from /settings (ADR 0037)")
	}

	folded := Options{UI: UISettings{TaskListOpen: false}}
	if got := row.Read(folded); got != "false" {
		t.Errorf("read of a session with the cards folded = %q, want \"false\"", got)
	}
	if got := row.Read(Options{UI: defaultUISettings()}); got != "true" {
		t.Errorf("read of an unconfigured session = %q, want \"true\"", got)
	}
}

// The Tools umbrella fold's row is ui.task-list-open's twin with the default turned around: a bool
// that defaults OFF — a large umbrella starts folded out of the box — editable, written back by the
// fold gesture on a large umbrella (ADR 0035 addendum), and read back from what THIS session
// resolved rather than the declared default. The fold itself and the write-back are the renderer's
// (items 2 and 3 of the plan); what is asserted here is the row a surface renders it from.
func TestToolsOpenRowIsAnEditableBoolDefaultingOff(t *testing.T) {
	t.Parallel()

	row, ok := LookupKey("ui.tools-open")
	if !ok {
		t.Fatal("no registry row for ui.tools-open; /settings could not show the key at all")
	}
	if row.Kind != KindBool {
		t.Errorf("kind = %q, want %q", row.Kind, KindBool)
	}
	if row.Default != "false" {
		t.Errorf("default = %q, want \"false\" — a config that names nothing starts large Tools umbrellas folded", row.Default)
	}
	if !row.Editable {
		t.Error("the row is not editable; the knob is live from /settings (ADR 0037)")
	}

	open := Options{UI: UISettings{ToolsOpen: true}}
	if got := row.Read(open); got != "true" {
		t.Errorf("read of a session with the umbrellas open = %q, want \"true\"", got)
	}
	if got := row.Read(Options{UI: defaultUISettings()}); got != "false" {
		t.Errorf("read of an unconfigured session = %q, want \"false\"", got)
	}
}

// The fold threshold's row is an int that defaults to five type rows and is editable, with a
// validate hook — sessions.max-count's shape — so a value below zero is refused at the keystroke
// in the same sentence the startup check would use. The row reads back what this session
// resolved, including the documented `0` that never folds.
func TestToolsFoldOverRowIsAnEditableIntDefaultingFive(t *testing.T) {
	t.Parallel()

	row, ok := LookupKey("ui.tools-fold-over")
	if !ok {
		t.Fatal("no registry row for ui.tools-fold-over; /settings could not show the key at all")
	}
	if row.Kind != KindInt {
		t.Errorf("kind = %q, want %q", row.Kind, KindInt)
	}
	if row.Default != "5" {
		t.Errorf("default = %q, want \"5\" — a config that names nothing folds an umbrella past five type rows", row.Default)
	}
	if !row.Editable {
		t.Error("the row is not editable; the knob is live from /settings (ADR 0037)")
	}
	if row.Validate == nil {
		t.Error("the row has no validate hook; a negative threshold would reach the file before startup refused it")
	}

	never := Options{UI: UISettings{ToolsFoldOver: 0}}
	if got := row.Read(never); got != "0" {
		t.Errorf("read of a session that never folds = %q, want \"0\"", got)
	}
	if got := row.Read(Options{UI: defaultUISettings()}); got != "5" {
		t.Errorf("read of an unconfigured session = %q, want \"5\"", got)
	}
}

// A threshold below zero is meaningless — no umbrella has fewer than zero type rows — and both
// doors refuse it: the validate hook the pane runs on the typed text, and the block validator
// startup runs on the parsed `ui:` block. Zero and above pass both, zero being the documented
// "never folds". The hook is driven through the row rather than by name so the test asserts the
// door /settings actually knocks on.
func TestToolsFoldOverRejectsNegative(t *testing.T) {
	t.Parallel()

	row, ok := LookupKey("ui.tools-fold-over")
	if !ok {
		t.Fatal("no registry row for ui.tools-fold-over")
	}

	tests := []struct {
		text    string
		n       int
		refused bool
	}{
		{text: "-1", n: -1, refused: true},
		{text: "-50", n: -50, refused: true},
		{text: "0", n: 0, refused: false},
		{text: "5", n: 5, refused: false},
		{text: "12", n: 12, refused: false},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			t.Parallel()
			hookErr := row.Validate(tt.text)
			blockErr := UISettings{ToolsFoldOver: tt.n}.Validate()
			if (hookErr != nil) != tt.refused {
				t.Errorf("hook Validate(%q) = %v; want refused=%v", tt.text, hookErr, tt.refused)
			}
			if (blockErr != nil) != tt.refused {
				t.Errorf("UISettings{ToolsFoldOver: %d}.Validate() = %v; want refused=%v", tt.n, blockErr, tt.refused)
			}
			if tt.refused {
				if !strings.Contains(hookErr.Error(), "ui.tools-fold-over") {
					t.Errorf("hook refusal %q does not name the key", hookErr)
				}
				if hookErr.Error() != blockErr.Error() {
					t.Errorf("the two doors refuse in two sentences:\n  hook  %v\n  block %v", hookErr, blockErr)
				}
			}
		})
	}

	if err := row.Validate("five"); err == nil || !strings.Contains(err.Error(), "ui.tools-fold-over") {
		t.Errorf("hook Validate(\"five\") = %v; want a refusal naming the key", err)
	}
}

// The context-fill notice's row is a bool that defaults OFF (ADR 0077): the one top-level boolean
// beside the seven Floor keys that a config naming nothing leaves off, because the notice steers
// the model rather than correcting it and ships off until bench evidence turns it on. The row reads
// back what THIS session resolved rather than the declared default, and its description says out
// loud that the key is not a Floor guard — the condition ADR 0077 put on giving a builtin a
// top-level key at all.
func TestContextFillNoticeRowIsABoolDefaultingOff(t *testing.T) {
	t.Parallel()

	row, ok := LookupKey("context-fill-notice")
	if !ok {
		t.Fatal("no registry row for context-fill-notice; /settings could not show the key at all")
	}
	if row.Kind != KindBool {
		t.Errorf("kind = %q, want %q", row.Kind, KindBool)
	}
	if row.Default != "false" {
		t.Errorf("default = %q, want \"false\" — a model-facing behaviour above the Floor ships off", row.Default)
	}
	if !strings.Contains(row.Desc, "Not a Floor guard") {
		t.Errorf("desc = %q, want it to say the key is not a Floor guard (ADR 0077 D2)", row.Desc)
	}

	if got := row.Read(Options{ContextFillNotice: true}); got != "true" {
		t.Errorf("read of a session with the notice on = %q, want \"true\"", got)
	}
	if got := row.Read(Options{}); got != "false" {
		t.Errorf("read of an unconfigured session = %q, want \"false\"", got)
	}
}

// The `reactions` row summarises the human's FILE, and since ADR 0076 one entry in that file
// resolves to one Reaction per action key it carries — all sharing the entry's own id. So the row
// counts distinct ids: an entry that armed two classes is still one block the human wrote, and a
// row reading "2 reactions" over a one-entry file would read as a miscount of their own config.
// The description is pinned word for word beside it because it is the one sentence /settings gives
// a human about what the two lanes actually do.
func TestReactionsRowCountsEntriesNotResolvedReactions(t *testing.T) {
	t.Parallel()

	row, ok := LookupKey("reactions")
	if !ok {
		t.Fatal("no registry row for reactions; /settings could not show the key at all")
	}

	const wantDesc = "Commands run when a Moment closes or a seam fires; advise text reaches the " +
		"model fenced, a gate answers before the Approver does."
	if row.Desc != wantDesc {
		t.Errorf("reactions Desc = %q, want %q", row.Desc, wantDesc)
	}

	// One entry spelling two action keys: two resolved Reactions, one id, one block in the file.
	twoLanes := Options{Reactions: []domain.Reaction{
		{ID: "warden", Origin: domain.OriginUser, Class: domain.ClassObserve},
		{ID: "warden", Origin: domain.OriginUser, Class: domain.ClassGate},
	}}
	if got := row.Read(twoLanes); got != "1 reaction" {
		t.Errorf("read of a one-entry file arming two lanes = %q, want \"1 reaction\"", got)
	}

	twoEntries := Options{Reactions: []domain.Reaction{
		{ID: "warden", Origin: domain.OriginUser, Class: domain.ClassGate},
		{ID: "notify", Origin: domain.OriginUser, Class: domain.ClassObserve},
	}}
	if got := row.Read(twoEntries); got != "2 reactions" {
		t.Errorf("read of a two-entry file = %q, want \"2 reactions\"", got)
	}
	if got := row.Read(Options{}); got != "" {
		t.Errorf("read of a session with no reactions: block = %q, want the empty summary", got)
	}
}

// The `bypass` row's description is a CLAIM about what a session keeps when the flag is on, and it
// is the sentence /settings renders beside the toggle — the one statement of Bypass a human meets
// without reading a manual. Since ADR 0076 the answer has two halves: the advise and shape
// Reactions a user or the bench armed go, the Floor guards and the structural reducers stay. It is
// pinned word for word because a description that drifts into "Reactions off" would read as a way
// to take the floor away, which is exactly what the flag is not.
func TestBypassRowDescribesWhatStaysOn(t *testing.T) {
	t.Parallel()

	row, ok := LookupKey("bypass")
	if !ok {
		t.Fatal("no registry row for bypass; /settings could not show the key at all")
	}

	const want = "Run with advise and shape Reactions of user or bench origin off; " +
		"Floor guards and structural reducers stay on."
	if row.Desc != want {
		t.Errorf("bypass Desc = %q, want %q", row.Desc, want)
	}
}

// TestSettingKeyValidatorsRefuseWhatStartupWouldRefuse pins each row's validate hook (Key.Validate
// — the write path's guard) to one value it must refuse. It calls the hooks directly rather than through
// SaveConfigSetting because three of them cannot be reached from there: an enum's vocabulary is checked
// by the kind first, so the mode, spinner and cursor hooks only ever fire on DRIFT between this table's
// EnumValues and the parse site behind them — which is exactly the case worth having a test for.
func TestSettingKeyValidatorsRefuseWhatStartupWouldRefuse(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		path    string
		value   string
		wantMsg string
	}{
		{"web-search-endpoint", "%zz", "not a URL"},
		{"context-window", "-1", "0 or more"},
		{"working-window", "-1", "0 or more"},
		{"working-window", "lots", "0 or more"},
		{"delegate-max-steps", "-1", "0 or more"},
		{"delegate-max-steps", "eighty", "0 or more"},
		{"delegate-fanout-rounds", "-1", "0 or more"},
		{"delegate-fanout-rounds", "two", "0 or more"},
		{"delegate-max-depth", "0", "at least 1"},
		{"delegate-max-depth", "deep", "at least 1"},
		{"delegate-max-tokens", "-1", "0 or more"},
		{"delegate-timeout", "soon", "length of time"},
		{"delegate-timeout", "-5m", "0 or more"},
		{"stream-idle-timeout", "soon", "length of time"},
		{"stream-idle-timeout", "-5m", "0 or more"},
		{"re-stream-budget", "-1", "0 or more"},
		{"re-stream-budget", "lots", "0 or more"},
		{"present.port", "70000", "0-65535"},
		{"mode", "yolo", "invalid --mode"},
		{"ui.spinner", "twirl", "invalid ui.spinner"},
		{"cursor-shape", "sideways", "invalid cursor-shape"},
		{"ui.stall-after", "soonish", "invalid ui.stall-after"},
		{"ui.stall-after", "-5s", "invalid ui.stall-after"},
		{"ui.stall-after", "90", "invalid ui.stall-after"}, // a bare number that is not 0 has no unit
		{"ui.color-scheme", "", "name a scheme"},
		{"ui.color-scheme", "../../.ssh/config", "a scheme is named, not a path"},
		{"system-prompt-file", "", "name a file to read the prompt from"},
		{"system-prompt-text", "  ", "write the prompt inline"},
		{"system-prompt-text", "You are apogee in {{ workspace }}.", "unknown placeholder"},
		{"context-files.names", "[../secrets.md]", "climbs out of the workspace"},
		{"context-files.names", "[AGENTS.md, ./AGENTS.md]", "listed twice"},
		{"context-files.names", "[/etc/motd]", "not workspace-relative"},
	} {
		t.Run(tt.path+"="+tt.value, func(t *testing.T) {
			t.Parallel()
			k := mustKey(tt.path)
			if k.Validate == nil {
				t.Fatalf("registry row %q has no validate hook", tt.path)
			}
			err := k.Validate(tt.value)
			if err == nil {
				t.Fatalf("%s = %q was accepted", tt.path, tt.value)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %v, want it to mention %q", err, tt.wantMsg)
			}
		})
	}
}

// And the other side of it: every value the keys DO take is accepted, including the shapes that look
// like refusals — the sentinels and the empty values the search key documents, and the zeros that
// mean "decide for me". A validator that refused one of those would make a documented config
// unwritable from the settings surface.
func TestSettingKeyValidatorsAcceptTheirDocumentedShapes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ path, value string }{
		{"web-search-endpoint", ""},
		{"web-search-endpoint", "off"},
		{"web-search-endpoint", "search.example.com/s"}, // scheme-less: the tool heals it to https://
		{"web-search-endpoint", "https://search.example.com/s"},
		{"context-window", "0"},
		{"context-window", "32768"},
		// 0 is the documented spelling of "work in the whole advertised window", not a refusal — and
		// a bound ABOVE any window this machine serves is accepted too: the top-level key describes
		// no particular server, and the engine already takes the smaller of the two.
		{"working-window", "0"},
		{"working-window", "200000"},
		{"working-window", "2000000"},
		// 0 is the documented spelling of "unbounded" here, not a refusal — and 80 is the shipped
		// default, which the settings surface has to be able to write back.
		{"delegate-max-steps", "0"},
		{"delegate-max-steps", "80"},
		// 0 is the documented spelling of "no ceiling", and 2 is the shipped default.
		{"delegate-fanout-rounds", "0"},
		{"delegate-fanout-rounds", "2"},
		// 1 is the shipped default, and 2 is the one deeper bound a human is likely to write.
		{"delegate-max-depth", "1"},
		{"delegate-max-depth", "2"},
		// 0 is the documented "unbounded" on both; the other value is each key's shipped default.
		{"delegate-max-tokens", "0"},
		{"delegate-max-tokens", "20000000"},
		{"delegate-timeout", "0"},
		{"delegate-timeout", "2h"},
		// 0 is the documented spelling of "disabled", and 10m is the shipped default.
		{"stream-idle-timeout", "0"},
		{"stream-idle-timeout", "10m"},
		// 0 is the documented spelling of "never re-stream", and 3 is the shipped default.
		{"re-stream-budget", "0"},
		{"re-stream-budget", "3"},
		{"present.port", "0"},
		{"present.port", "8080"},
		{"mode", string(domain.ModeAuto)},
		{"ui.spinner", "glitter"},
		{"ui.stall-after", "120s"}, // the shipped default, which the settings surface has to be able to write back
		{"ui.stall-after", "2m"},
		{"ui.stall-after", "0"}, // the documented spelling of "off" — a zero that is not a refusal
		{"ui.stall-after", ""},  // and the empty field, which is the key's way of saying "the default"
		{"cursor-shape", "bar"},
		{"ui.color-scheme", "light"},
		// A scheme nothing has written yet is accepted on purpose: the loader answers an unresolvable
		// name with a warning and the default palette, so a pane that refused it would be stricter
		// than the thing it configures (ADR 0040 design call 8).
		{"ui.color-scheme", "solarized"},
		// A RELATIVE prompt file is resolved against the apogee home, which this pure check does not
		// hold, so it is accepted here and answered by the apply (validateSystemPromptFile).
		{"system-prompt-file", "prompts/apogee.md"},
		// Prose over several lines, carrying the placeholders the renderer substitutes per request.
		{"system-prompt-text", "You are apogee in {{workspace}}.\nToday is {{datetime}}, mode {{mode}}.\n"},
		{"context-files.names", "[AGENTS.md, docs/CLAUDE.md]"},
		{"context-files.names", "[]"}, // the second documented spelling of "off"
	} {
		t.Run(tt.path+"="+tt.value, func(t *testing.T) {
			t.Parallel()
			if err := mustKey(tt.path).Validate(tt.value); err != nil {
				t.Errorf("%s = %q was refused: %v", tt.path, tt.value, err)
			}
		})
	}
}

// The `system-prompt-file` hook's other half — the one that needs a filesystem: an ABSOLUTE path is
// checked for real, so a prompt file typed with a finger-slip is refused on the row instead of at the
// next launch, and a directory is refused as the not-a-prompt-file it is.
func TestSystemPromptFileValidatorChecksAnAbsolutePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	present := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(present, []byte("You are apogee.\n"), 0o600); err != nil {
		t.Fatalf("write the fixture prompt: %v", err)
	}
	validate := mustKey("system-prompt-file").Validate

	if err := validate(present); err != nil {
		t.Errorf("a readable prompt file was refused: %v", err)
	}
	for _, tt := range []struct{ name, value, wantMsg string }{
		{"a file that is not there", filepath.Join(dir, "absent.md"), "there is no such file"},
		{"a directory", dir, "it is a directory"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validate(tt.value)
			if err == nil {
				t.Fatalf("%s was accepted", tt.value)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %v, want it to mention %q", err, tt.wantMsg)
			}
		})
	}
}

// Every editable key whose kind is not the whole of its contract carries a hook, and every hook belongs
// to an editable key. The first half is the drift guard: a key added to the table with a range, a URL or
// a vocabulary behind it must be given its check deliberately rather than inheriting "anything goes"
// from the kind. The second is the honest converse — a hook on a key no surface can write is a check
// nothing runs.
func TestRegistryValidateHooksSitOnEditableKeys(t *testing.T) {
	t.Parallel()
	// The editable keys whose kind IS their whole contract: a plain name, a free-text template, an
	// address this process cannot verify. Listed here so adding a key cannot quietly join them.
	// `server` joins them for a reason of its own: its valid values are the names of THIS config's
	// `servers:` entries, which no per-value hook holding no list can know — so the name is checked
	// at selection, where the list is in hand, and any string is a writable value here.
	// `editor` joins them for present.command's reason: it is a command LINE, and whether this
	// machine has that program is not a fact a per-value hook can settle — it is answered at launch.
	// `tools.disabled` joins them because a name matching no tool is deliberately a NOTICE rather
	// than a refusal (unknownToolNotice): a hook here would make the settings surface stricter than
	// the file it writes, and refuse an edit the next launch would happily read.
	// The `url-safety` host pair joins them because an entry is normalised permissively where the
	// guard is built (trim, IDNA, lowercase, trailing root dot stripped), so a hook here would refuse
	// host spellings the guard itself accepts — and a host that resolves nowhere is not a fact a
	// per-value check holding no resolver can settle.
	unchecked := map[string]bool{
		"server": true, "present.command": true, "present.host": true, "editor": true,
		"tools.disabled": true, "url-safety.allow-hosts": true, "url-safety.deny-hosts": true,
	}
	for _, k := range KeyRegistry {
		switch {
		case k.Validate != nil && !k.Editable:
			t.Errorf("registry row %q has a validate hook but is not editable; nothing would run it", k.Path)
		case k.Validate == nil && k.Editable && k.Kind != KindBool && !unchecked[k.Path]:
			t.Errorf("registry row %q is editable and has no validate hook — give it one, or list it "+
				"in this test's unchecked set with the reason its kind is the whole contract", k.Path)
		case k.Validate != nil && unchecked[k.Path]:
			t.Errorf("registry row %q now has a validate hook; take it out of the unchecked set", k.Path)
		}
	}
}

// TestRegistryDefaultsReadBackFromAnEmptyFile pins every row's declared Default to the value a
// config that states nothing actually resolves to: LoadFileConfig over an ABSENT file, read back
// through the row's own Read, has to agree with the Default the row advertises — for the plain
// scalars, the block-mapped keys and the enums alike. The comparison runs through the row's own
// parse (renderSettingValue's canonical value for the kind; time.ParseDuration for the duration
// strings) rather than on the text, because Read spells a duration the way a resolved Duration
// prints itself — `2m0s` for `ui.stall-after`'s declared "120s", `2h0m0s` for `delegate-timeout`'s
// "2h" — and two spellings of one bound are not a drift.
//
// `cursor-shape` is exempt on purpose. Its row declares "block", but Read returns o.CursorShape,
// which an absent file leaves EMPTY (config_test.go: "cursor-shape and editor are file-only
// (default empty)"; wantDefaults sets no CursorShape): the renderer applies the default itself
// (internal/tui/prompteditor.go) and the settings pane's fallback rule — an empty value shows the
// declared Default — is what shows "block" (cmd/apogee/settingsrows.go, pinned in
// settingsrows_test.go). Defaulting it in fromFile would take that fallback rule's only subject
// away, so the gap stays and is named here rather than closed.
func TestRegistryDefaultsReadBackFromAnEmptyFile(t *testing.T) {
	t.Parallel()

	absent := func(string) ([]byte, error) { return nil, fs.ErrNotExist }
	resolved, err := LoadFileConfig("config.yaml", absent, noNotify)
	if err != nil {
		t.Fatalf("LoadFileConfig over an absent file: %v", err)
	}

	exempt := map[string]string{
		"cursor-shape": "Read returns the file's own (empty) value; the pane's fallback rule shows the default",
	}
	for _, k := range KeyRegistry {
		if k.Default == "" {
			continue
		}
		t.Run(k.Path, func(t *testing.T) {
			t.Parallel()
			if reason, ok := exempt[k.Path]; ok {
				t.Skipf("exempt: %s", reason)
			}
			got, want := k.Read(resolved), k.Default
			if !readsAlike(t, k, got, want) {
				t.Errorf("registry row %q reads %q from an absent file, want its declared default %q", k.Path, got, want)
			}
		})
	}
}

// TestRegistryDefaultsFollowTheStarterTemplate pins the two rows whose declared default the starter
// template used to override with an active line of its own — `remember-model: true` and
// `ui.stall-after: 120s` — to those very values (ADR 0048, amended 2026-09-16): the row's Default,
// the value an absent file resolves to (the test above), and the template's line are one value, so a
// change to any of the three without the others is what fails here. The template is read as text
// because that is what a first run seeds: an active line at column one for the top-level key, the
// indented one under `ui:` for the block key.
func TestRegistryDefaultsFollowTheStarterTemplate(t *testing.T) {
	t.Parallel()
	template := string(defaultConfigYAML)
	for _, tt := range []struct{ path, want, line string }{
		{"remember-model", "true", "\nremember-model: true\n"},
		{"ui.stall-after", "120s", "\n  stall-after: 120s"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			if got := mustKey(tt.path).Default; got != tt.want {
				t.Errorf("registry row %q declares Default %q; want %q, the starter template's value", tt.path, got, tt.want)
			}
			if !strings.Contains(template, tt.line) {
				t.Errorf("the starter template no longer ships the active line %q; the registry row %q declares %q", strings.TrimSpace(tt.line), tt.path, tt.want)
			}
		})
	}
}

// readsAlike compares two spellings of a row's value through the row's own parse: the canonical
// value renderSettingValue makes of each for the kind, and — for a string the row holds as a
// duration — the time.Duration both resolve to.
func readsAlike(t *testing.T, k Key, got, want string) bool {
	t.Helper()
	if k.Kind == KindString {
		if gotDuration, err := time.ParseDuration(got); err == nil {
			wantDuration, err := time.ParseDuration(want)
			if err != nil {
				t.Fatalf("registry row %q reads a duration (%s) but declares a default (%q) that is none", k.Path, gotDuration, want)
			}
			return gotDuration == wantDuration
		}
	}
	_, gotCanonical, err := renderSettingValue(k, got)
	if err != nil {
		t.Fatalf("registry row %q reads a value its own kind refuses: %v", k.Path, err)
	}
	_, wantCanonical, err := renderSettingValue(k, want)
	if err != nil {
		t.Fatalf("registry row %q declares a default its own kind refuses: %v", k.Path, err)
	}
	return gotCanonical == wantCanonical
}

// TestRegistrySetIsTheInverseOfRead pins the row's fourth projection to its first: for every row
// that has a Set, the value Read spells is one Set lands back unchanged, on the one Options field
// the row owns and no other. The fixture is the Options the file pass resolves for
// everyKeyFileConfig() — the projection LoadFileConfig runs (applyFile), over a file that states
// every key at a non-default value — and the defaults an empty file resolves to are the base the
// reach half lands on. Two halves per row, both on the typed Options fields and never on strings:
//
//   - exact: Set(Read(o)) on a copy of o leaves the copy equal to o — the spelling a row shows is
//     one the row takes back, and landing it touches nothing else;
//   - reach: Set(Read(o)) on the DEFAULTS moves exactly the field the row is known to own — so a
//     row whose Set writes its neighbour's field (two bools both false in the fixture would pass
//     the exact half) is named, as TestEveryConfigKeyReachesTheOptions names a fromFile that does.
//
// A KindText row round-trips through Text: its Read is a line-count summary, and the prose behind
// it is the value. Rows with no Set are exactly the ones with no spelled inverse — the structured
// rows, `context-files.enable` (its Read derives from the resolved list and owns no field) and
// `sub-agents-server` (its Read shows a word for the empty value) — and every editable row but the
// first of those has one, since the pane's apply lands through it.
func TestRegistrySetIsTheInverseOfRead(t *testing.T) {
	t.Parallel()

	var resolved, defaults Options
	mustApplyFile(t, &resolved, everyKeyFileConfig())
	mustApplyFile(t, &defaults, fileConfig{})

	// The Options field each row lands on, by name: the reach half's expectation, and the one
	// place the ownership is spelled out — the rows themselves write a closure, which no test can
	// read the field name off.
	owns := map[string]string{
		"server": "StartupServer", "sub-agents-choice": "SubAgentsChoice", "mode": "Mode",
		"system-prompt-text": "SystemPrompt", "system-prompt-file": "SystemPrompt",
		"use-default-prompt": "UseDefaultPrompt", "context-files.names": "ContextFiles",
		"confine-to-workspace": "ConfineToWorkspace", "web-search-endpoint": "WebSearchEndpoint",
		"tools.disabled": "ToolsDisabled", "tools.enabled": "ToolsEnabled",
		"url-safety.allow-hosts": "URLAllowHosts", "url-safety.deny-hosts": "URLDenyHosts",
		"use-project-skills": "UseProjectSkills", "use-shipped-skills": "UseShippedSkills",
		"auto-compact": "AutoCompact", "prune-tool-results": "PruneToolResults",
		"tool-use-enforcer": "ToolUseEnforcer", "empty-response-recovery": "EmptyResponseRecovery",
		"tool-call-repair": "ToolCallRepair", "tool-call-salvage": "ToolCallSalvage",
		"tool-loop-breaker": "ToolLoopBreaker", "tool-result-cap": "ToolResultCap",
		"read-cache": "ReadCache", "context-fill-notice": "ContextFillNotice",
		"delegate-max-steps": "DelegateMaxSteps", "delegate-fanout-rounds": "DelegateFanOutRounds",
		"delegate-max-depth": "DelegateMaxDepth", "delegate-max-tokens": "DelegateMaxTokens",
		"delegate-timeout": "DelegateTimeout", "stream-idle-timeout": "StreamIdleTimeout",
		"re-stream-budget": "RestreamBudget", "undo-snapshots": "UndoSnapshots",
		"auto-title": "AutoTitle", "remember-model": "RememberModel",
		"context-window": "ContextWindow", "working-window": "WorkingWindow",
		"response-reserve":  "ResponseReserve",
		"present.auto-open": "Present", "present.command": "Present", "present.port": "Present",
		"present.host": "Present",
		"ui.spinner":   "UI", "ui.spinner-color": "UI", "ui.show-scrollbar": "UI",
		"ui.color-scheme": "UI", "ui.stall-after": "UI", "ui.inspector": "UI",
		"ui.skill-suggestions": "UI", "ui.task-list-open": "UI",
		"ui.tools-open": "UI", "ui.tools-fold-over": "UI",
		"sessions.max-age": "Sessions", "sessions.max-count": "Sessions",
		"cursor-shape": "CursorShape", "editor": "Editor", "bypass": "Bypass",
	}
	noInverse := map[string]string{
		"context-files.enable": "Read derives from the resolved list; the key owns no Options field",
		"sub-agents-server":    "Read shows a word for the empty value, which is not a spelling",
	}

	for _, k := range KeyRegistry {
		t.Run(k.Path, func(t *testing.T) {
			t.Parallel()
			if k.Set == nil {
				switch {
				case k.Kind == KindStructured:
				case noInverse[k.Path] != "":
				default:
					t.Fatalf("registry row %q has no Set — every row with a spelled value has one; "+
						"a row with no spelled inverse is listed in this test's noInverse set with its reason", k.Path)
				}
				return
			}
			if _, listed := noInverse[k.Path]; listed || k.Kind == KindStructured {
				t.Fatalf("registry row %q now has a Set; take it out of this test's exemptions", k.Path)
			}
			field, known := owns[k.Path]
			if !known {
				t.Fatalf("registry row %q has a Set but this test does not know which Options field it "+
					"owns — add it to the owns table", k.Path)
			}
			spelled, spelledDefault := k.Read(resolved), k.Read(defaults)
			if k.Kind == KindText {
				spelled, spelledDefault = k.Text(resolved), k.Text(defaults)
			}

			exact := resolved
			if err := k.Set(spelled, &exact); err != nil {
				t.Fatalf("Set(Read(o)) = %q refused what the row itself reads: %v", spelled, err)
			}
			if diffs := structDiff(exact, resolved); len(diffs) != 0 {
				t.Errorf("Set(Read(o)) does not land what Read reads:\n%s", strings.Join(diffs, "\n"))
			}

			reached := defaults
			if err := k.Set(spelled, &reached); err != nil {
				t.Fatalf("Set(Read(o)) = %q on the defaults refused: %v", spelled, err)
			}
			moved := make([]string, 0, 1)
			for _, diff := range structDiff(reached, defaults) {
				moved = append(moved, strings.SplitN(diff, " ", 2)[0])
			}
			if spelled == spelledDefault {
				t.Fatalf("the fixture leaves %q at its default (%q); give everyKeyFileConfig a value "+
					"for it, or the reach half proves nothing", k.Path, spelled)
			}
			if !slices.Equal(moved, []string{field}) {
				t.Errorf("Set moved the Options fields %v, want exactly [%s] — the field the row reads", moved, field)
			}
		})
	}
}

// TestRegistrySetRefusesWhatValidateRefuses pins the admission half of Set to the row's own
// validate hook: whatever the hook refuses, Set refuses, in the hook's own sentence — so a site that
// lands a value through the row (the env pass, a Driver's live apply) reports the refusal the
// settings pane reports, with only its own lead in front. Whatever the hook ACCEPTS, Set accepts
// unless the kind's own parse refuses it (the writer's kind check — a bool that is not one, an enum
// outside its vocabulary), in which case the refusal is the writer's sentence for that. Either way
// a refused Set has touched nothing: the Options it was handed are what they were.
//
// The values are a common battery rather than a per-key table, on purpose: the claim is the SHAPE
// of Set's answer against the hook's for every row, and the hooks' per-key wordings are already
// pinned by TestSettingKeyValidatorsRefuseWhatStartupWouldRefuse.
func TestRegistrySetRefusesWhatValidateRefuses(t *testing.T) {
	t.Parallel()

	var resolved Options
	mustApplyFile(t, &resolved, everyKeyFileConfig())
	battery := []string{
		"", "   ", "nonsense", "-1", "0", "1", "70000", "1.5", "yes please", "off",
		"[../secrets.md]", "[AGENTS.md, ./AGENTS.md]", "%zz", "You are apogee in {{ workspace }}.",
		"-5m", "90", "soonish",
	}

	for _, k := range KeyRegistry {
		if k.Set == nil {
			continue
		}
		for _, value := range battery {
			t.Run(k.Path+"="+value, func(t *testing.T) {
				t.Parallel()
				admitted := value
				if k.Kind != KindText {
					admitted = strings.TrimSpace(value)
				}
				var hookErr error
				if k.Validate != nil {
					hookErr = k.Validate(admitted)
				}
				_, _, kindErr := renderSettingValue(k, admitted)

				got := resolved
				setErr := k.Set(value, &got)

				switch {
				case hookErr != nil:
					if setErr == nil {
						t.Fatalf("Validate refuses %q (%v) but Set accepted it", value, hookErr)
					}
					if setErr.Error() != hookErr.Error() {
						t.Errorf("Set refuses %q as\n  %v\nwant the hook's own sentence\n  %v", value, setErr, hookErr)
					}
				case kindErr != nil:
					if setErr == nil {
						t.Fatalf("the kind refuses %q (%v) but Set accepted it", value, kindErr)
					}
					if want := "apogee: " + kindErr.Error(); setErr.Error() != want {
						t.Errorf("Set refuses %q as\n  %v\nwant the writer's kind sentence\n  %v", value, setErr, want)
					}
				case setErr != nil:
					t.Fatalf("neither Validate nor the kind refuses %q, but Set did: %v", value, setErr)
				}
				if setErr != nil {
					if diffs := structDiff(got, resolved); len(diffs) != 0 {
						t.Errorf("a refused Set still wrote:\n%s", strings.Join(diffs, "\n"))
					}
				}
			})
		}
	}
}
