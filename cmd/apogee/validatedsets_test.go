package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/validated"
)

// The shipped gemma entry is the fixture for the wire-level tests: it is real curation
// data, pinned three ways (shipped.json, the catalogue table, shipped_test.go), so
// testing against it also exercises exactly what a user's startup exercises.
const gemmaKey = "gemma-4-e4b-it-qat"

func baseOpts(model string) options {
	return options{model: model, validatedSetsEnable: true}
}

func TestResolveValidatedSet_OffSwitches(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		opts options
	}{
		{"bypass suppresses everything", options{model: gemmaKey, validatedSetsEnable: true, bypass: true}},
		{"enable false suppresses everything", options{model: gemmaKey, validatedSetsEnable: false}},
		{"no model resolves nothing", baseOpts("")},
		{"unknown model matches nothing", baseOpts("some-unknown-model")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			set, notices, err := resolveValidatedSet(tt.opts, t.TempDir(), t.TempDir())
			if err != nil || set != nil || len(notices) != 0 {
				t.Fatalf("want silence, got set=%v notices=%v err=%v", set, notices, err)
			}
		})
	}
}

func TestResolveValidatedSet_DirectLowMatchOffers(t *testing.T) {
	t.Parallel()
	set, notices, err := resolveValidatedSet(baseOpts(gemmaKey), t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveValidatedSet: %v", err)
	}
	if set != nil {
		t.Fatalf("a low-confidence direct match must NOT apply; got %v", set)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "To apply it") {
		t.Fatalf("want one offer notice, got %v", notices)
	}
	// The offer names the exact alias to paste — the §3 explicit human decision.
	if !strings.Contains(notices[0], `"gemma-4-e4b-it-qat": "gemma-4-e4b-it-qat"`) {
		t.Fatalf("offer notice missing the paste-ready alias line: %q", notices[0])
	}
}

func TestResolveValidatedSet_IdentityAliasApplies(t *testing.T) {
	t.Parallel()
	opts := baseOpts(gemmaKey)
	opts.validatedSetsAlias = map[string]string{gemmaKey: gemmaKey}

	set, notices, err := resolveValidatedSet(opts, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveValidatedSet: %v", err)
	}
	if len(set) != 16 {
		t.Fatalf("want the gemma 16 applied, got %d: %v", len(set), set)
	}
	for i := 1; i < len(set); i++ {
		if set[i-1] >= set[i] {
			t.Fatalf("applied set not in sorted canonical order: %v", set)
		}
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "applied via alias") ||
		!strings.Contains(notices[0], "16 mechanisms on") || !strings.Contains(notices[0], validated.SourceShipped) {
		t.Fatalf("applied notice wrong: %v", notices)
	}
}

func TestResolveValidatedSet_ManualControlSuppresses(t *testing.T) {
	t.Parallel()
	opts := baseOpts(gemmaKey)
	opts.validatedSetsAlias = map[string]string{gemmaKey: gemmaKey}
	opts.mechanisms = map[string]bool{"validate": true} // any non-empty block = manual control

	set, notices, err := resolveValidatedSet(opts, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveValidatedSet: %v", err)
	}
	if set != nil {
		t.Fatalf("manual control must suppress the apply, got %v", set)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "mechanisms: config takes precedence") {
		t.Fatalf("want the suppressed notice, got %v", notices)
	}
}

func TestResolveValidatedSet_DanglingAliasIsLoud(t *testing.T) {
	t.Parallel()
	opts := baseOpts("my-model")
	opts.validatedSetsAlias = map[string]string{"my-model": "no-such-entry"}

	_, _, err := resolveValidatedSet(opts, t.TempDir(), t.TempDir())
	var dangling *validated.DanglingAliasError
	if !errors.As(err, &dangling) {
		t.Fatalf("want DanglingAliasError, got %v", err)
	}
}

func TestResolveValidatedSet_UserEntryWinsAndSorts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A user-local entry for the SAME key as the shipped gemma one: user wins, and its
	// (deliberately unsorted) set comes back in sorted canonical order.
	entry := `{"version":1,"key":"gemma-4-e4b-it-qat","set":["validate","autofix"],"evidence":{"campaign":"user-run-1"}}`
	if err := os.WriteFile(filepath.Join(dir, "gemma.json"), []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := baseOpts(gemmaKey)
	opts.validatedSetsAlias = map[string]string{gemmaKey: gemmaKey}

	set, notices, err := resolveValidatedSet(opts, dir, t.TempDir())
	if err != nil {
		t.Fatalf("resolveValidatedSet: %v", err)
	}
	want := []apogee.MechanismID{"autofix", "validate"}
	if len(set) != 2 || set[0] != want[0] || set[1] != want[1] {
		t.Fatalf("want the user entry's set sorted %v, got %v", want, set)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], validated.SourceUser) ||
		!strings.Contains(notices[0], "user-run-1") {
		t.Fatalf("applied notice should name the user-local source and campaign, got %v", notices)
	}
}

func TestResolveValidatedSet_DefectiveEntrySkipsSoft(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// An entry naming a mechanism this binary does not know: whole-set-or-nothing, so
	// the entry is skipped with a notice — never partially applied, never a startup error.
	entry := `{"version":1,"key":"mystery-model","set":["ghost_mechanism"],"evidence":{"campaign":"c"}}`
	if err := os.WriteFile(filepath.Join(dir, "mystery.json"), []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := baseOpts("mystery-model")
	opts.validatedSetsAlias = map[string]string{"mystery-model": "mystery-model"}

	set, notices, err := resolveValidatedSet(opts, dir, t.TempDir())
	if err != nil {
		t.Fatalf("a defective entry must stay soft, got %v", err)
	}
	if set != nil {
		t.Fatalf("defective entry must not apply, got %v", set)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "skipping validated-set entry") ||
		!strings.Contains(notices[0], "ghost_mechanism") {
		t.Fatalf("want one skip notice naming the defect, got %v", notices)
	}
}

func TestResolveValidatedSet_MalformedUserFileWarnsEvenUnmatched(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{"version":1,`), 0o600); err != nil {
		t.Fatal(err)
	}

	set, notices, err := resolveValidatedSet(baseOpts("some-unknown-model"), dir, t.TempDir())
	if err != nil || set != nil {
		t.Fatalf("want soft warning only, got set=%v err=%v", set, err)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "skipping validated-set entry") {
		t.Fatalf("want the load warning to surface, got %v", notices)
	}
}
