package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/tui"
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

	for _, k := range keyRegistry {
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
		if row, ok := lookupKey(path); ok {
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
// structured accepts a plain string as well as the composite types, because
// system-prompt-text/-file are single strings whose value is multi-line prose — no text field
// edits those, so the surface treats them as blocks.
func kindMatchesType(kind configKind, typ reflect.Type) bool {
	typ = derefType(typ)
	switch kind {
	case kindBool:
		return typ.Kind() == reflect.Bool
	case kindInt:
		return typ.Kind() == reflect.Int
	case kindString, kindEnum:
		return typ.Kind() == reflect.String
	case kindStructured:
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
// actually validates the key at startup. Two of the three sets live in internal/tui as
// unexported slices, so the vocabulary is recovered from the exported parser's own error
// message — which is the parse site's published list — rather than duplicated by hand: a
// style added there and not here fails this test instead of silently going unofferable in
// /settings.
func TestRegistryEnumValuesMatchParseSites(t *testing.T) {
	t.Parallel()

	// A value no vocabulary will ever contain, used to make each parser list what it knows.
	const unknown = "apogee-registry-test-not-a-value"

	t.Run("mode", func(t *testing.T) {
		t.Parallel()
		values := enumValues(t, "mode")
		for _, v := range values {
			if _, err := parseMode(v); err != nil {
				t.Errorf("registry offers mode %q but parseMode rejects it: %v", v, err)
			}
		}
		// Completeness against the ladder constants the parser switches on.
		for _, m := range []string{string(modePlan), string(modeAskBefore), string(modeAllowEdits), string(modeAuto)} {
			if !slices.Contains(values, m) {
				t.Errorf("mode %q is a known autonomy mode but the registry does not offer it", m)
			}
		}
	})

	t.Run("ui.spinner", func(t *testing.T) {
		t.Parallel()
		values := enumValues(t, "ui.spinner")
		for _, v := range values {
			if _, err := tui.ParseSpinnerStyle(v); err != nil {
				t.Errorf("registry offers spinner %q but ParseSpinnerStyle rejects it: %v", v, err)
			}
		}
		_, err := tui.ParseSpinnerStyle(unknown)
		for _, name := range vocabularyFromError(t, err) {
			if !slices.Contains(values, name) {
				t.Errorf("ParseSpinnerStyle knows style %q but the registry does not offer it", name)
			}
		}
	})

	t.Run("cursor-shape", func(t *testing.T) {
		t.Parallel()
		values := enumValues(t, "cursor-shape")
		for _, v := range values {
			if _, err := tui.ParseCursorShape(v); err != nil {
				t.Errorf("registry offers cursor shape %q but ParseCursorShape rejects it: %v", v, err)
			}
		}
		_, err := tui.ParseCursorShape(unknown)
		for _, name := range vocabularyFromError(t, err) {
			if !slices.Contains(values, name) {
				t.Errorf("ParseCursorShape knows shape %q but the registry does not offer it", name)
			}
		}
	})
}

// enumValues returns the registry row's vocabulary, failing when the row is missing or is
// not an enum at all — so a kind change is reported here rather than as an empty loop.
func enumValues(t *testing.T, path string) []string {
	t.Helper()
	row, ok := lookupKey(path)
	if !ok {
		t.Fatalf("no registry row for %q", path)
	}
	if row.Kind != kindEnum {
		t.Fatalf("registry row %q is kind %q, want %q", path, row.Kind, kindEnum)
	}
	if len(row.EnumValues) == 0 {
		t.Fatalf("registry row %q is an enum with no values", path)
	}
	return row.EnumValues
}

// vocabularyFromError recovers the list a parse site names in its rejection message — the
// "(known styles: a, b, c)" tail. It fails loudly rather than quietly returning nothing when
// the format changes, because a silent empty list would turn the completeness check above
// into a test that asserts nothing.
func vocabularyFromError(t *testing.T, err error) []string {
	t.Helper()
	if err == nil {
		t.Fatal("the parse site accepted a value no vocabulary contains")
	}
	const marker = "(known "
	msg := err.Error()
	i := strings.Index(msg, marker)
	if i < 0 {
		t.Fatalf("cannot read the vocabulary out of %q: no %q section — the parse site's error "+
			"format changed, so update this test", msg, marker)
	}
	rest := msg[i+len(marker):]
	j := strings.Index(rest, ": ")
	if j < 0 {
		t.Fatalf("cannot read the vocabulary out of %q: no list follows %q", msg, marker)
	}
	return strings.Split(strings.TrimSuffix(rest[j+2:], ")"), ", ")
}

// TestRegistryRowInvariants pins the properties every surface reading the registry relies on,
// so a new row cannot half-describe a key: unique paths, a description for every row, editing
// only where an in-place editor exists, an enum vocabulary that includes the row's own
// default, and masking confined to the one secret the schema carries.
func TestRegistryRowInvariants(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, k := range keyRegistry {
		if seen[k.Path] {
			t.Errorf("duplicate registry row for %q", k.Path)
		}
		seen[k.Path] = true

		if strings.TrimSpace(k.Desc) == "" {
			t.Errorf("registry row %q has no description — /settings would show a blank line", k.Path)
		}
		if k.Editable && k.Kind == kindStructured {
			t.Errorf("registry row %q is editable but structured — v1 has no editor for a block", k.Path)
		}
		// No row is masked since ADR 0036 retired the top-level `api-key:`: the schema's one secret
		// is a `servers:` entry's own key, nested inside a structured block the pane summarizes as a
		// count. A masked row appearing again means a secret has been given a surface of its own.
		if k.Masked {
			t.Errorf("registry row %q is masked; no top-level key carries a secret any more", k.Path)
		}
		switch k.Kind {
		case kindEnum:
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

// TestSettingKeyValidatorsRefuseWhatStartupWouldRefuse pins each row's validate hook (configKey.Validate
// — the write path's guard) to one value it must refuse. It calls the hooks directly rather than through
// saveConfigSetting because three of them cannot be reached from there: an enum's vocabulary is checked
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
		{"present.port", "70000", "0-65535"},
		{"llama-launcher", "http://box:7331", "looks like a URL"},
		{"llama-launcher", "   ", "only whitespace"},
		{"mode", "yolo", "invalid --mode"},
		{"ui.spinner", "twirl", "invalid ui.spinner"},
		{"cursor-shape", "sideways", "invalid cursor-shape"},
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
// like refusals — the sentinels and the empty values the launcher and search keys document, and the
// zeros that mean "decide for me". A validator that refused one of those would make a documented
// config unwritable from the settings surface.
func TestSettingKeyValidatorsAcceptTheirDocumentedShapes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ path, value string }{
		{"web-search-endpoint", ""},
		{"web-search-endpoint", "off"},
		{"web-search-endpoint", "search.example.com/s"}, // scheme-less: the tool heals it to https://
		{"web-search-endpoint", "https://search.example.com/s"},
		{"context-window", "0"},
		{"context-window", "32768"},
		{"present.port", "0"},
		{"present.port", "8080"},
		{"llama-launcher", ""},
		{"llama-launcher", "off"},
		{"llama-launcher", "/home/me/.llama-launcher/config.yaml"},
		{"mode", string(modeAuto)},
		{"ui.spinner", "glitter"},
		{"cursor-shape", "bar"},
	} {
		t.Run(tt.path+"="+tt.value, func(t *testing.T) {
			t.Parallel()
			if err := mustKey(tt.path).Validate(tt.value); err != nil {
				t.Errorf("%s = %q was refused: %v", tt.path, tt.value, err)
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
	unchecked := map[string]bool{
		"server": true, "present.command": true, "present.host": true,
	}
	for _, k := range keyRegistry {
		switch {
		case k.Validate != nil && !k.Editable:
			t.Errorf("registry row %q has a validate hook but is not editable; nothing would run it", k.Path)
		case k.Validate == nil && k.Editable && k.Kind != kindBool && !unchecked[k.Path]:
			t.Errorf("registry row %q is editable and has no validate hook — give it one, or list it "+
				"in this test's unchecked set with the reason its kind is the whole contract", k.Path)
		case k.Validate != nil && unchecked[k.Path]:
			t.Errorf("registry row %q now has a validate hook; take it out of the unchecked set", k.Path)
		}
	}
}

// TestModeIsTheOnlyKeyAppliedWithoutARestart pins the equivalence the /settings pane's two live-apply
// decisions rest on. The pane names `mode` when it decides what to APPLY (settings.go's settingsModeKey —
// the seam Engine.SetMode exists for) but asks RestartRequired when it decides what to SHOW: a row with
// no restart flag renders its edited value as the value the session is running, and a row with one
// renders a "(next launch)" marker instead. Both are right today because those are the same one key; a
// second RestartRequired:false row added without a live seam would silently make the pane claim an edit
// had taken effect when nothing had applied it, and this test is what names that.
func TestModeIsTheOnlyKeyAppliedWithoutARestart(t *testing.T) {
	t.Parallel()
	for _, k := range keyRegistry {
		if !k.RestartRequired && k.Path != "mode" {
			t.Errorf("registry row %q takes effect without a restart; give it a live seam in the "+
				"/settings pane (internal/tui/settings.go: settingsModeKey) or mark it RestartRequired",
				k.Path)
		}
	}
	if mustKey("mode").RestartRequired {
		t.Error("mode is now restart-required, so the pane's live apply would render a marker instead")
	}
}
