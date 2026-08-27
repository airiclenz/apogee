package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

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

// walkSchema recurses over a config struct's yaml tags, recording into described every path
// the registry accounts for and failing for every leaf it does not.
func walkSchema(t *testing.T, typ reflect.Type, prefix string, described map[string]bool) {
	t.Helper()
	for i := range typ.NumField() {
		field := typ.Field(i)
		name := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" {
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

// derefType strips pointer indirection so a *uiConfig is walked like a uiConfig.
func derefType(typ reflect.Type) reflect.Type {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ
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
		{"present.port", "0"},
		{"present.port", "8080"},
		{"mode", string(domain.ModeAuto)},
		{"ui.spinner", "glitter"},
		{"ui.stall-after", "90s"},
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
