package scheme

import (
	"slices"
	"testing"
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
