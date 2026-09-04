package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/validated"
)

// gemmaKey is the entry key apogee SHIPPED a Validated set under until v0.20.0, when every one of
// the fifteen Mechanisms it named retired and the roster went empty (ADR 0071). It is the fixture
// for the retired-key path: a config carrying the alias apogee itself printed must still start.
const gemmaKey = "gemma-4-e4b-it-qat"

// labKey is the model label the synthetic entries below are filed under. The shipped roster is
// empty, so every fixture that needs an entry writes its own — which is also the only kind of
// entry a user has now, and exactly how a saved record reaches startup.
const labKey = "lab-model"

// labSet is what those synthetic entries enable: one ID no build carries, standing in for the
// experimental row a bench build registers into the (otherwise empty) catalogue. It is on no
// retired roll, so DropRetired leaves it alone and the entry reaches the catalogue check whole.
var labSet = []domain.MechanismID{"lab_row"}

func baseOpts(model string) config.Options {
	return config.Options{Model: model, ValidatedSetsEnable: true}
}

func TestResolveValidatedSet_OffSwitches(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		opts config.Options
	}{
		{"bypass suppresses everything", config.Options{Model: labKey, ValidatedSetsEnable: true, Bypass: true}},
		{"enable false suppresses everything", config.Options{Model: labKey, ValidatedSetsEnable: false}},
		{"no model resolves nothing", baseOpts("")},
		{"unknown model matches nothing", baseOpts("some-unknown-model")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeLabEntry(t, dir, labKey, labSet)
			set, notices, err := resolveValidatedSet(tt.opts, dir, t.TempDir())
			if err != nil || set != nil || len(notices) != 0 {
				t.Fatalf("want silence, got set=%v notices=%v err=%v", set, notices, err)
			}
		})
	}
}

func TestResolveValidatedSet_DirectLowMatchOffers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLabEntry(t, dir, labKey, labSet)

	set, notices, err := resolveValidatedSet(baseOpts(labKey), dir, t.TempDir())
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
	if !strings.Contains(notices[0], `"lab-model": "lab-model"`) {
		t.Fatalf("offer notice missing the paste-ready alias line: %q", notices[0])
	}
}

// The alias replaces the confidence gate: a low-confidence label that would only be OFFERED is
// carried past the gate to the entry itself. What happens NEXT is the catalogue's business — this
// build's catalogue is empty (ADR 0071), so the entry is then skipped whole against it — but the
// proof here is that the alias reached the entry rather than leaving the surface offering it.
func TestResolveValidatedSet_IdentityAliasReplacesTheConfidenceGate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLabEntry(t, dir, labKey, labSet)
	opts := baseOpts(labKey)
	opts.ValidatedSetsAlias = map[string]string{labKey: labKey}

	set, notices, err := resolveValidatedSet(opts, dir, t.TempDir())
	if err != nil {
		t.Fatalf("resolveValidatedSet: %v", err)
	}
	if set != nil {
		t.Fatalf("an entry the empty catalogue cannot assemble must not apply; got %v", set)
	}
	if len(notices) != 1 || strings.Contains(notices[0], "To apply it") {
		t.Fatalf("the alias must carry past the low-confidence offer; got %v", notices)
	}
	if !strings.Contains(notices[0], "skipping validated-set entry") ||
		!strings.Contains(notices[0], string(labSet[0])) {
		t.Fatalf("want the skip notice naming the entry's live member, got %v", notices)
	}
}

func TestResolveValidatedSet_ManualControlSuppresses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLabEntry(t, dir, labKey, labSet)
	opts := baseOpts(labKey)
	opts.ValidatedSetsAlias = map[string]string{labKey: labKey}
	opts.Mechanisms = map[string]bool{"lab_row": true} // any non-empty block = manual control

	set, notices, err := resolveValidatedSet(opts, dir, t.TempDir())
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
	opts.ValidatedSetsAlias = map[string]string{"my-model": "no-such-entry"}

	_, _, err := resolveValidatedSet(opts, t.TempDir(), t.TempDir())
	var dangling *validated.DanglingAliasError
	if !errors.As(err, &dangling) {
		t.Fatalf("want DanglingAliasError, got %v", err)
	}
}

// The retired ENTRY KEY, at the surface that has to tolerate it: the alias line in the config is
// the one apogee itself printed at the release that shipped the entry, so removing the entry must
// cost the user a notice, never a start and never the *DanglingAliasError a target on no roll
// earns. One line, naming the release and the alias to delete.
func TestResolveValidatedSet_RetiredEntryKeyStartsWithOneNotice(t *testing.T) {
	t.Parallel()
	opts := baseOpts("my-quant")
	opts.ValidatedSetsAlias = map[string]string{"my-quant": gemmaKey}

	set, notices, err := resolveValidatedSet(opts, t.TempDir(), t.TempDir())

	var dangling *validated.DanglingAliasError
	if errors.As(err, &dangling) {
		t.Fatalf("the alias apogee itself offered must not refuse the start: %v", err)
	}
	if err != nil {
		t.Fatalf("resolveValidatedSet: %v", err)
	}
	if set != nil {
		t.Fatalf("a retired entry arms nothing, got %v", set)
	}
	if len(notices) != 1 {
		t.Fatalf("want exactly one retired-entry notice, got %v", notices)
	}
	for _, want := range []string{gemmaKey, "was retired in v0.20.0", "remove the alias"} {
		if !strings.Contains(notices[0], want) {
			t.Errorf("notice = %q, want it to name %q", notices[0], want)
		}
	}
}

// The other half of the same guard, ratified by the owner on 2026-09-03: the retired-key roll is
// read only where the entry lookup MISSED. A user's own entry filed under the retired key is their
// measured evidence and still resolves exactly as it did before the removal — a curation change of
// ours never costs a live entry its start (ADR 0016's 2026-08-29 amendment).
func TestResolveValidatedSet_UserEntryUnderARetiredKeyStillResolves(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLabEntry(t, dir, gemmaKey, labSet)

	set, notices, err := resolveValidatedSet(baseOpts(gemmaKey), dir, t.TempDir())
	if err != nil {
		t.Fatalf("resolveValidatedSet: %v", err)
	}
	if set != nil {
		t.Fatalf("a low-confidence direct match must NOT apply; got %v", set)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "To apply it") {
		t.Fatalf("the user's own entry must be OFFERED, not answered by the retired-key roll: %v", notices)
	}
	if strings.Contains(notices[0], "was retired in") {
		t.Fatalf("the roll answered over a live entry: %q", notices[0])
	}
}

// An entry whose every member retired sheds to nothing. It must not reach the applying rung — an
// empty set passes the catalogue check vacuously, so it would arm nothing behind a banner claiming
// a validated stack — and it earns ONE line, not one per shed member: on a fifteen-member record
// the per-member lines would bury the only fact that matters.
func TestResolveValidatedSet_AllRetiredEntryNoLongerApplies(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	retired := mechanisms.RetiredIDs()
	if len(retired) < 2 {
		t.Fatalf("the retired roll carries %d ids; this fixture needs two", len(retired))
	}
	writeLabEntry(t, dir, labKey, retired[:2])
	opts := baseOpts(labKey)
	opts.ValidatedSetsAlias = map[string]string{labKey: labKey}

	set, notices, err := resolveValidatedSet(opts, dir, t.TempDir())
	if err != nil {
		t.Fatalf("an all-retired entry must stay soft, got %v", err)
	}
	if len(set) != 0 {
		t.Fatalf("nothing may arm from an entry with no live members, got %v", set)
	}
	if len(notices) != 1 {
		t.Fatalf("want exactly one notice for the whole entry, got %v", notices)
	}
	for _, want := range []string{labKey, "names only retired mechanisms", "remove it"} {
		if !strings.Contains(notices[0], want) {
			t.Errorf("notice = %q, want it to name %q", notices[0], want)
		}
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
	opts.ValidatedSetsAlias = map[string]string{"mystery-model": "mystery-model"}

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

// A user's saved record written before a Mechanism was RETIRED keeps the rest of its set instead of
// being skipped whole as an unknown id would be (ADR 0016's 2026-08-29 amendment). The shed happens
// on the ladder itself, before any renderer reads the entry, so startup and `probe model` count the
// same members (ADR 0021 §4); the line it earns names the entry, the id and the id's OWN release.
func TestResolveValidatedSetDropsARetiredIDWithANotice(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// The record as it stood before the retirement: a live member with `grammar` written in the
	// middle, so the shed cannot be passing by position.
	legacy := []domain.MechanismID{labSet[0], "grammar", "lab_row_two"}
	writeLabEntry(t, dir, labKey, legacy)
	opts := baseOpts(labKey)
	opts.ValidatedSetsAlias = map[string]string{labKey: labKey}

	d := startupSetDecision(opts, dir, t.TempDir())

	if len(d.droppedRetired) != 1 || d.droppedRetired[0] != "grammar" {
		t.Fatalf("droppedRetired = %v, want only the retired member shed", d.droppedRetired)
	}
	want := []domain.MechanismID{labSet[0], "lab_row_two"}
	if len(d.match.Entry.Set) != len(want) || d.match.Entry.Set[0] != want[0] || d.match.Entry.Set[1] != want[1] {
		t.Fatalf("surviving set = %v, want the live members in order %v", d.match.Entry.Set, want)
	}

	// The wording, at the pure renderer that owns it: the release is the shed id's OWN, looked up
	// on the roll — not one release string shared by every removal, which would misname the
	// changelog entry a user goes looking for.
	notice := retiredSetMemberNotice(d.match.Entry, "grammar")
	for _, w := range []string{labKey, "grammar", "retired", "v0.18.7", "the rest of the set applies"} {
		if !strings.Contains(notice, w) {
			t.Errorf("shed notice = %q, want it to name %q", notice, w)
		}
	}

	// A member shed because it was PROMOTED to a Floor guard reads the other way: the catalogue row
	// is gone but the behaviour is engine default now, so the line names the successor key — telling
	// this user only "retired" would report a loss their measured stack did not take.
	promoted := retiredSetMemberNotice(d.match.Entry, "tool_loop_interceptor")
	for _, w := range []string{labKey, "tool_loop_interceptor", `"tool-loop-breaker" floor guard since v0.20.0`, "on by default"} {
		if !strings.Contains(promoted, w) {
			t.Errorf("promoted-member notice = %q, want it to name %q", promoted, w)
		}
	}
	if strings.Contains(promoted, "retired in") {
		t.Errorf("promoted-member notice = %q, want the retired-outright sentence gone", promoted)
	}
}

// writeLabEntry drops one synthetic entry into a user-local validated-sets directory, the way a
// saved record reaches startup. The shipped roster is empty since v0.20.0, so this is where every
// entry these tests match against comes from.
func writeLabEntry(t *testing.T, dir, key string, set []domain.MechanismID) {
	t.Helper()
	blob, err := json.Marshal(validated.Entry{
		Version:  validated.EntryVersion,
		Key:      key,
		Set:      set,
		Evidence: validated.Evidence{Campaign: "lab-run-1"},
	})
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir validated dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, key+".json"), blob, 0o600); err != nil {
		t.Fatal(err)
	}
}

// labEntryJSON is writeLabEntry's on-disk twin for the tests that seed an entry through
// writeUserValidatedEntry's raw-body helper.
func labEntryJSON(key string) string {
	return `{"version":1,"key":"` + key + `","set":["` + string(labSet[0]) + `"],"evidence":{"campaign":"lab-run-1"}}`
}
