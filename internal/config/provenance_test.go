package config

import (
	"maps"
	"testing"
)

// Every override names what won, in the spelling the user typed: a flag as `--<name>`, a variable
// by its own name — and APOGEE_NO_UPDATE_CHECK, whose row names no variable, by that switch. The
// files and the default name nothing.
func TestProvenanceOfNamesEveryOverride(t *testing.T) {
	t.Parallel()
	noFlag := func(string) bool { return false }
	noEnv := func(string) string { return "" }

	flags, envs := 0, 0
	for _, k := range KeyRegistry {
		if k.fromFlag != nil && k.FlagName != "" {
			flags++
			o := Options{Overrides: overrideSources(func(n string) bool { return n == k.FlagName }, noEnv)}
			want := Provenance{Source: SourceFlag, Name: "--" + k.FlagName}
			if got := o.ProvenanceOf(k); got != want {
				t.Errorf("ProvenanceOf(%q) with --%s set = %+v; want %+v", k.Path, k.FlagName, got, want)
			}
		}
		if k.fromEnv != nil && k.EnvVar != "" {
			envs++
			o := Options{Overrides: overrideSources(noFlag, func(n string) string {
				if n == k.EnvVar {
					return "1"
				}
				return ""
			})}
			want := Provenance{Source: SourceEnv, Name: k.EnvVar}
			if got := o.ProvenanceOf(k); got != want {
				t.Errorf("ProvenanceOf(%q) with %s set = %+v; want %+v", k.Path, k.EnvVar, got, want)
			}
		}
	}
	if flags == 0 || envs == 0 {
		t.Fatalf("the registry offered %d flag and %d env overrides; want some of each", flags, envs)
	}

	updateCheck, ok := LookupKey(updateCheckPath)
	if !ok {
		t.Fatalf("no %q row in the registry", updateCheckPath)
	}
	o := Options{Overrides: overrideSources(noFlag, func(n string) string {
		if n == EnvNoUpdateCheck {
			return "1"
		}
		return ""
	})}
	if got, want := o.ProvenanceOf(updateCheck), (Provenance{Source: SourceEnv, Name: EnvNoUpdateCheck}); got != want {
		t.Errorf("ProvenanceOf(%q) with %s set = %+v; want %+v", updateCheckPath, EnvNoUpdateCheck, got, want)
	}

	files := Options{
		ProjectKeys: map[string]bool{"context-files.names": true},
		GlobalKeys:  map[string]bool{"workflow-retries": true},
	}
	for path, want := range map[string]Source{
		"context-files.names": SourceProject,
		"workflow-retries":    SourceGlobal,
		"auto-title":          SourceDefault,
	} {
		k, _ := LookupKey(path)
		if got := files.ProvenanceOf(k); got != (Provenance{Source: want}) {
			t.Errorf("ProvenanceOf(%q) = %+v; want source %q and no name", path, got, want)
		}
	}
}

// A tighten-only list the Project config added entries to is the project's in part whichever
// source the mark names — an override outranks both files, but the value is still a union — and
// a project-param key the project states is not a union at all.
func TestProvenanceOfMarksAProjectUnion(t *testing.T) {
	t.Parallel()
	o := Options{
		Overrides:   map[string]Source{"tools.disabled": SourceEnv},
		ProjectKeys: map[string]bool{"tools.disabled": true, "dangerous-rules": true, "context-files.names": true},
	}
	for path, want := range map[string]bool{
		"tools.disabled":        true,
		"dangerous-rules":       true,
		"context-files.names":   false,
		"url-safety.deny-hosts": false,
	} {
		k, ok := LookupKey(path)
		if !ok {
			t.Fatalf("no %q row in the registry", path)
		}
		if got := o.ProvenanceOf(k).ProjectUnion; got != want {
			t.Errorf("ProvenanceOf(%q).ProjectUnion = %v; want %v", path, got, want)
		}
	}
}

// The rank order is the documented precedence — flag > env > project > global > default — and a
// source a resolution never reports (the zero value) ranks below all of them.
func TestSourceOutranksFollowsThePrecedence(t *testing.T) {
	t.Parallel()
	order := []Source{"", SourceDefault, SourceGlobal, SourceProject, SourceEnv, SourceFlag}
	for i, low := range order {
		if low.Outranks(low) {
			t.Errorf("%q outranks itself", low)
		}
		for _, high := range order[i+1:] {
			if !high.Outranks(low) {
				t.Errorf("%q does not outrank %q", high, low)
			}
			if low.Outranks(high) {
				t.Errorf("%q outranks %q", low, high)
			}
		}
	}
}

// WithFileLayers takes both file layers from the later reading except for the keys keep names,
// which keep the receiver's entries — present or absent — and leaves the values and overrides
// alone. Its maps are its own: writing into them moves neither input.
func TestWithFileLayersKeepsTheNamedKeys(t *testing.T) {
	t.Parallel()
	boot := Options{
		Model:       "boot-model",
		Overrides:   map[string]Source{"mode": SourceFlag},
		GlobalKeys:  map[string]bool{"server": true, "workflow-retries": true},
		ProjectKeys: map[string]bool{"context-files.names": true},
	}
	files := Options{
		Model:       "files-model",
		GlobalKeys:  map[string]bool{"auto-title": true, "sub-agents-server": true},
		ProjectKeys: map[string]bool{"tools.disabled": true, "server": true},
	}
	keep := func(k Key) bool { return k.Path == "server" || k.Path == "sub-agents-server" }

	got := boot.WithFileLayers(files, keep)

	wantGlobal := map[string]bool{"auto-title": true, "server": true}
	wantProject := map[string]bool{"tools.disabled": true}
	if !maps.Equal(got.GlobalKeys, wantGlobal) || !maps.Equal(got.ProjectKeys, wantProject) {
		t.Errorf("layers = global %v, project %v; want global %v, project %v",
			got.GlobalKeys, got.ProjectKeys, wantGlobal, wantProject)
	}
	if got.Model != "boot-model" || got.Overrides["mode"] != SourceFlag {
		t.Errorf("WithFileLayers moved the values: model %q, overrides %v", got.Model, got.Overrides)
	}

	got.GlobalKeys["mode"] = true
	got.ProjectKeys["mode"] = true
	if boot.GlobalKeys["mode"] || boot.ProjectKeys["mode"] || files.GlobalKeys["mode"] || files.ProjectKeys["mode"] {
		t.Error("a write into the result reached an input's map")
	}

	if all := boot.WithFileLayers(files, nil); !maps.Equal(all.GlobalKeys, files.GlobalKeys) ||
		!maps.Equal(all.ProjectKeys, files.ProjectKeys) {
		t.Errorf("a nil keep kept something: global %v, project %v", all.GlobalKeys, all.ProjectKeys)
	}
}

// A project may state exactly the project-param and tighten-only keys: every registry row answers
// by its class, and the Project config writer refuses the rest.
func TestProjectSettableIsTheParamAndTightenOnlyClasses(t *testing.T) {
	t.Parallel()
	for class, want := range map[KeyClass]bool{
		ClassGlobalOnly:   false,
		ClassProjectParam: true,
		ClassTightenOnly:  true,
		ClassGranting:     false,
	} {
		if got := (Key{Class: class}).ProjectSettable(); got != want {
			t.Errorf("a %s key: ProjectSettable = %v; want %v", class, got, want)
		}
	}
	for _, k := range KeyRegistry {
		want := k.Class == ClassProjectParam || k.Class == ClassTightenOnly
		if got := k.ProjectSettable(); got != want {
			t.Errorf("%q (%s): ProjectSettable = %v; want %v", k.Path, k.Class, got, want)
		}
		if _, err := projectWritableKey(k.Path); err == nil && !k.ProjectSettable() {
			t.Errorf("projectWritableKey(%q) accepted a %s key", k.Path, k.Class)
		}
	}
}
