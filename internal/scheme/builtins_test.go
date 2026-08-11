package scheme

import (
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// The guards in this file run over every shipped scheme rather than over a named one, so
// a scheme added to schemes/ later inherits them without anyone remembering to.

// builtinScheme loads one shipped scheme onto a zero Scheme, so a role the file forgets
// stays empty instead of being quietly filled in by the default — which is what lets the
// completeness guard below see an omission at all.
func builtinScheme(t *testing.T, name string) Scheme {
	t.Helper()
	data, ok := builtinBytes(name)
	if !ok {
		t.Fatalf("built-in scheme %q is not embedded", name)
	}
	got, warnings := parseInto(Scheme{}, name+fileExt, data)
	if len(warnings) != 0 {
		t.Fatalf("built-in %q warned: %v — a shipped scheme must load clean", name, warnings)
	}
	return got
}

func TestLightSchemeIsBuiltIn(t *testing.T) {
	t.Parallel()
	if names := builtinNames(); !slices.Contains(names, "light") {
		t.Fatalf("built-ins are %v, want the shipped light scheme among them", names)
	}
}

func TestBuiltinSchemesStateEveryRole(t *testing.T) {
	t.Parallel()
	for _, name := range builtinNames() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := builtinScheme(t, name)
			for _, role := range roleTable {
				if role.get(got) == "" {
					t.Errorf("key %q is missing — a shipped scheme states all %d roles, so exporting one hands the user a complete file to edit", role.key, len(roleTable))
				}
			}
		})
	}
}

func TestBuiltinSchemesStateEveryRoleOnce(t *testing.T) {
	t.Parallel()
	for _, name := range builtinNames() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Read the raw file, not the parsed Scheme: YAML keeps a repeated key and lets the
			// last one win, so a scheme stating a role twice parses clean while one of the two
			// lines quietly does nothing — invisible in the struct, and handed to the user
			// verbatim by `/color-scheme export`.
			data, ok := builtinBytes(name)
			if !ok {
				t.Fatalf("built-in scheme %q is not embedded", name)
			}
			var doc yaml.Node
			if err := yaml.Unmarshal(data, &doc); err != nil {
				t.Fatalf("built-in %q is unreadable YAML: %v", name, err)
			}
			if len(doc.Content) == 0 {
				t.Fatalf("built-in %q states no roles at all", name)
			}
			root := doc.Content[0]
			if root.Kind != yaml.MappingNode {
				t.Fatalf("built-in %q is not a mapping of color roles", name)
			}
			firstLine := make(map[string]int, len(roleKeys))
			for i := 0; i+1 < len(root.Content); i += 2 {
				key := root.Content[i]
				if line, dup := firstLine[key.Value]; dup {
					t.Errorf("key %q is stated twice, on lines %d and %d — the later value silently wins and the earlier line is dead", key.Value, line, key.Line)
					continue
				}
				firstLine[key.Value] = key.Line
			}
		})
	}
}

func TestBuiltinSchemesKeepSkillAndFileRefDistinct(t *testing.T) {
	t.Parallel()
	for _, name := range builtinNames() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := builtinScheme(t, name)
			if got.Skill == got.FileRef {
				t.Errorf("skill and file-ref are both %q — ADR 0027: the two prompt tokens must stay tellable apart at a glance", got.Skill)
			}
		})
	}
}

func TestBuiltinSchemesKeepBothMutedStepsDistinct(t *testing.T) {
	t.Parallel()
	for _, name := range builtinNames() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := builtinScheme(t, name)
			// The two steps are how an EXPANDED tool block reads out of the scrollback of
			// collapsed ones around it (internal/tui, theme.toolDetail vs toolDetailBright).
			// One value for both is a scheme in which opening a block changes nothing.
			if got.Muted == got.MutedBright {
				t.Errorf("muted and muted-bright are both %q — an open block must read a step out of the collapsed dim", got.Muted)
			}
		})
	}
}

func TestBuiltinSchemesKeepBothMarkerStepsDistinct(t *testing.T) {
	t.Parallel()
	for _, name := range builtinNames() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := builtinScheme(t, name)
			// The marker pair carries the same open/closed step as the muted pair, one column
			// over: a scheme giving both roles one value is a scheme in which opening a block
			// leaves its marker tone exactly where it was.
			if got.ToolMarker == got.ToolMarkerBright {
				t.Errorf("tool-marker and tool-marker-bright are both %q — an open block's marker must read a step out of the collapsed tone", got.ToolMarker)
			}
		})
	}
}

func TestBuiltinSchemesKeepToolHeaderAndCodeDistinct(t *testing.T) {
	t.Parallel()
	for _, name := range builtinNames() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := builtinScheme(t, name)
			// Splitting tool-header off code is the whole point of the role: a shipped
			// scheme that gives both one value paints tool headers in the code tone
			// again and hands the separation back.
			if got.ToolHeader == got.Code {
				t.Errorf("tool-header and code are both %q — a tool block's header must not read as the code it prints", got.Code)
			}
		})
	}
}

func TestBuiltinSchemesKeepModeColorsDistinct(t *testing.T) {
	t.Parallel()
	for _, name := range builtinNames() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := builtinScheme(t, name)
			// The footer marker is the only place the mode shows as color; two modes
			// sharing one would make the privilege ladder unreadable exactly where it
			// matters most.
			modes := []struct{ key, value string }{
				{"mode-plan", got.ModePlan},
				{"mode-ask-before", got.ModeAskBefore},
				{"mode-allow-edits", got.ModeAllowEdits},
				{"mode-auto", got.ModeAuto},
			}
			for i, a := range modes {
				for _, b := range modes[i+1:] {
					if a.value == b.value {
						t.Errorf("%s and %s are both %q — each autonomy mode needs its own color", a.key, b.key, a.value)
					}
				}
			}
		})
	}
}
