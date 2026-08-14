package config

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The hand-edited config the block cases start from: a paragraph of documentation, the commented
// example the template ships the block as, and a setting below it — so a splice that moved a
// comment or joined the wrong block shows up as a changed line rather than as a passing test.
const mechanismsExampleConfig = "server: testbox\n" +
	"\n" +
	"# Mechanisms keep a small model on track; every one of them defaults off.\n" +
	"# mechanisms:\n" +
	"#   validate: true              # tool-call validation + auto-retry\n" +
	"#   syntax: true                # write-content syntax check\n" +
	"\n" +
	"ui:\n" +
	"  spinner: snake\n"

// The catalogue ids the cases toggle. They are real ones (internal/mechanisms/catalogue.go), but
// nothing here asks the catalogue: which ids exist is the caller's question, and this writer's is
// only where the line goes.
const (
	validateID = "validate"
	syntaxID   = "syntax"
)

// mechanismsIn parses a written config the way apogee itself does and returns the block's map —
// the property that matters more than the bytes, since it is what a reader takes back out.
func mechanismsIn(t *testing.T, content string) map[string]bool {
	t.Helper()
	var fc fileConfig
	if err := yaml.Unmarshal([]byte(content), &fc); err != nil {
		t.Fatalf("the written config does not parse: %v\n%s", err, content)
	}
	return fc.Mechanisms
}

// The four shapes a mechanism line can be inserted into, each against a file whose other lines must
// come back untouched: a block the file only documents, a file documenting it nowhere, a bare
// `mechanisms:` key, and a block already open.
func TestSaveMechanismSettingInsertsIntoEveryParentState(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		before  string
		id      string
		enabled bool
		want    []string
		wantMap map[string]bool
	}{
		{
			name:    "the block is created directly below its commented example",
			before:  mechanismsExampleConfig,
			id:      validateID,
			enabled: true,
			want:    []string{"mechanisms:", "  validate: true"},
			wantMap: map[string]bool{validateID: true},
		},
		{
			name:    "a file that documents the block nowhere gets it at the end",
			before:  "server: testbox\nui:\n  spinner: snake\n",
			id:      validateID,
			enabled: true,
			want:    []string{"", "mechanisms:", "  validate: true"},
			wantMap: map[string]bool{validateID: true},
		},
		{
			name:    "a bare mechanisms: key gains its first child",
			before:  "server: testbox\nmechanisms:\nui:\n  spinner: snake\n",
			id:      validateID,
			enabled: true,
			want:    []string{"  validate: true"},
			wantMap: map[string]bool{validateID: true},
		},
		{
			name:    "an open block gains a sibling at its own indentation",
			before:  "server: testbox\nmechanisms:\n  validate: true\nui:\n  spinner: snake\n",
			id:      syntaxID,
			enabled: true,
			want:    []string{"  syntax: true"},
			wantMap: map[string]bool{validateID: true, syntaxID: true},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeTestConfig(t, tt.before)

			if err := SaveMechanismSetting(path, tt.id, tt.enabled); err != nil {
				t.Fatalf("write the mechanism: %v", err)
			}

			written := readTestConfig(t, path)
			at, inserted := splicedInsertion(t, tt.before, written)
			if !slices.Equal(inserted, tt.want) {
				t.Errorf("inserted %q at line %d, want %q", inserted, at, tt.want)
			}
			if got := mechanismsIn(t, written); !maps.Equal(got, tt.wantMap) {
				t.Errorf("mechanisms = %v, want %v", got, tt.wantMap)
			}
		})
	}
}

// Toggling a Mechanism off rewrites the line the file already carries — keeping the user's own
// indentation and their end-of-line note — rather than removing it (ADR 0016: the block's emptiness
// is what decides whether a Validated set applies, so an "off" has to stay visible).
func TestSaveMechanismSettingWritesFalseOverAnActiveLine(t *testing.T) {
	t.Parallel()
	before := "server: testbox\n" +
		"mechanisms:\n" +
		"  validate: true    # tool-call validation\n" +
		"  syntax: true\n" +
		"ui:\n" +
		"  spinner: snake\n"
	path := writeTestConfig(t, before)

	if err := SaveMechanismSetting(path, validateID, false); err != nil {
		t.Fatalf("turn the mechanism off: %v", err)
	}

	written := readTestConfig(t, path)
	if !strings.Contains(written, "  validate: false    # tool-call validation\n") {
		t.Errorf("the off line did not keep its shape and note:\n%s", written)
	}
	b, a := SplitConfigLines([]byte(before)), SplitConfigLines([]byte(written))
	if len(a) != len(b) {
		t.Fatalf("a rewrite changed the line count: %d -> %d", len(b), len(a))
	}
	want := map[string]bool{validateID: false, syntaxID: true}
	if got := mechanismsIn(t, written); !maps.Equal(got, want) {
		t.Errorf("mechanisms = %v, want %v", got, want)
	}
}

// An id the block does not name is written even when it is being written OFF: an absent key and an
// explicit `false` leave the Mechanism in the same state but not the file — a block with a line in
// it is manual control (ADR 0016), which is a decision the user made and the writer records.
func TestSaveMechanismSettingWritesFalseForAnIdTheBlockDoesNotName(t *testing.T) {
	t.Parallel()
	before := mechanismsExampleConfig
	path := writeTestConfig(t, before)

	if err := SaveMechanismSetting(path, validateID, false); err != nil {
		t.Fatalf("write the mechanism: %v", err)
	}

	written := readTestConfig(t, path)
	if _, inserted := splicedInsertion(t, before, written); !slices.Equal(
		inserted, []string{"mechanisms:", "  validate: false"}) {
		t.Errorf("inserted %q, want the block with an explicit off line", inserted)
	}
	want := map[string]bool{validateID: false}
	if got := mechanismsIn(t, written); !maps.Equal(got, want) {
		t.Errorf("mechanisms = %v, want %v", got, want)
	}
}

// A re-write of what the file already says is a confirmation, not a rewrite: the bytes are left
// exactly as they were, so a surface that re-applies a toggle cannot churn the user's file.
func TestSaveMechanismSettingIsANoOpWhenTheFileAlreadySaysIt(t *testing.T) {
	t.Parallel()
	before := "server: testbox\nmechanisms:\n  validate: true\n"
	path := writeTestConfig(t, before)

	if err := SaveMechanismSetting(path, validateID, true); err != nil {
		t.Fatalf("re-write the mechanism: %v", err)
	}

	if written := readTestConfig(t, path); written != before {
		t.Errorf("the file was rewritten:\n--- got ---\n%s--- want ---\n%s", written, before)
	}
}

// An absent config is seeded from the embedded template and the line lands under the template's own
// commented example — the placement ADR 0035 asks for, and the sharpest test that the write is a
// splice: the template is documentation apart from one active key, and a round trip through
// unmarshal→marshal would have deleted every word of it.
func TestSaveMechanismSettingSeedsAnAbsentConfigAndLandsBelowTheExample(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")

	if err := SaveMechanismSetting(path, validateID, true); err != nil {
		t.Fatalf("write the mechanism into a seeded config: %v", err)
	}

	written := readTestConfig(t, path)
	at, inserted := splicedInsertion(t, string(defaultConfigYAML), written)
	if !slices.Equal(inserted, []string{"mechanisms:", "  validate: true"}) {
		t.Fatalf("inserted %q at line %d, want the block and its one line", inserted, at)
	}
	lines := SplitConfigLines([]byte(written))
	if above := lines[at-2]; !strings.HasPrefix(above, "#") {
		t.Errorf("the block landed under %q, want the last line of its commented example", above)
	}
	want := map[string]bool{validateID: true}
	if got := mechanismsIn(t, written); !maps.Equal(got, want) {
		t.Errorf("mechanisms = %v, want %v", got, want)
	}
}

// The round trip that matters: what the writer put in the file is what the loader every launch runs
// takes back out of it.
func TestSaveMechanismSettingRoundTripsThroughLoadFileConfig(t *testing.T) {
	t.Parallel()
	path := writeTestConfig(t, mechanismsExampleConfig)

	if err := SaveMechanismSetting(path, validateID, true); err != nil {
		t.Fatalf("write the mechanism: %v", err)
	}
	if err := SaveMechanismSetting(path, syntaxID, false); err != nil {
		t.Fatalf("write the second mechanism: %v", err)
	}

	layer, err := LoadFileConfig(path, os.ReadFile, noNotify)
	if err != nil {
		t.Fatalf("load the written config: %v", err)
	}
	want := map[string]bool{validateID: true, syntaxID: false}
	if !maps.Equal(layer.Mechanisms, want) {
		t.Errorf("loaded mechanisms = %v, want %v", layer.Mechanisms, want)
	}
}

// An id the file cannot carry as a plain key is refused before the config is even opened — the
// catalogue's own ids are identifiers, and anything a reader would take back out under a different
// name would leave the block naming a Mechanism nobody enabled.
func TestSaveMechanismSettingRefusesAnIdItCannotSpellAsAKey(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, id string }{
		{"empty", ""},
		{"whitespace only", "   "},
		{"a word yaml reads as a bool", "on"},
		{"one carrying a colon", "validate: true"},
		{"one spanning lines", "validate\nsyntax"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			before := mechanismsExampleConfig
			path := writeTestConfig(t, before)

			if err := SaveMechanismSetting(path, tt.id, true); err == nil {
				t.Fatalf("writing %q was accepted; want a refusal", tt.id)
			}
			if written := readTestConfig(t, path); written != before {
				t.Errorf("the refused write touched the file:\n%s", written)
			}
		})
	}
}

// The gate every splice passes: a result that moved anything but this one id is refused rather than
// written, which is what stands between a file shape the line arithmetic mis-read and a config the
// user quietly cannot get back.
func TestVerifiedMechanismSpliceRefusesAnEditThatChangedAnythingElse(t *testing.T) {
	t.Parallel()
	before := "server: testbox\nmechanisms:\n  validate: true\n  syntax: true\n"
	var parsed fileConfig
	if err := yaml.Unmarshal([]byte(before), &parsed); err != nil {
		t.Fatalf("parse the starting config: %v", err)
	}

	for _, tt := range []struct{ name, updated string }{
		{"a sibling mechanism flipped", "server: testbox\nmechanisms:\n  validate: false\n  syntax: false\n"},
		{"a setting outside the block moved", "server: other\nmechanisms:\n  validate: false\n  syntax: true\n"},
		{"the line landed nowhere", before},
		{"the result no longer parses", "server: testbox\nmechanisms:\n  validate: false\n   syntax: true\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := verifiedMechanismSplice([]byte(tt.updated), parsed, validateID, false)
			if err == nil {
				t.Fatalf("the edit was accepted; want a refusal")
			}
			if got != nil {
				t.Errorf("a refused edit returned %d bytes to write", len(got))
			}
		})
	}
}

// A verified edit comes back whole: the gate returns the bytes, so a caller cannot write an
// unchecked splice by forgetting to ask.
func TestVerifiedMechanismSpliceReturnsTheCheckedBytes(t *testing.T) {
	t.Parallel()
	before := "server: testbox\nmechanisms:\n  validate: true\n"
	var parsed fileConfig
	if err := yaml.Unmarshal([]byte(before), &parsed); err != nil {
		t.Fatalf("parse the starting config: %v", err)
	}
	updated := "server: testbox\nmechanisms:\n  validate: false\n"

	got, err := verifiedMechanismSplice([]byte(updated), parsed, validateID, false)
	if err != nil {
		t.Fatalf("the edit was refused: %v", err)
	}
	if string(got) != updated {
		t.Errorf("returned %q, want the checked bytes back", got)
	}
}

// A block the writer cannot address surgically is refused with the "by hand" idiom, and the file is
// left as it was — the shape check every writer here makes before it splices.
func TestSaveMechanismSettingRefusesAFlowStyleBlock(t *testing.T) {
	t.Parallel()
	before := "server: testbox\nmechanisms: {validate: true}\n"
	path := writeTestConfig(t, before)

	err := SaveMechanismSetting(path, syntaxID, true)
	if err == nil {
		t.Fatalf("a flow-style block was accepted; want a refusal")
	}
	if !strings.Contains(err.Error(), "by hand") {
		t.Errorf("refusal = %v, want the by-hand idiom", err)
	}
	if written := readTestConfig(t, path); written != before {
		t.Errorf("the refused write touched the file:\n%s", written)
	}
}
