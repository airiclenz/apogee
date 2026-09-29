package skills

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// libraryFixtureMinSkills is the floor the fixture catalog must clear before any ranking row runs:
// a fixture that lost its files would otherwise let every "nil" row pass for the wrong reason.
const libraryFixtureMinSkills = 20

// libraryFixtureBody is the one line every fixture skill carries as its body: the fixture is
// synthetic (see testdata/library/README.md), and a skill without this body was not written for it.
const libraryFixtureBody = "Synthetic fixture skill — see testdata/library/README.md."

// newLibraryCatalog loads testdata/library — a synthetic, library-sized catalog (see
// testdata/library/README.md) — through the ordinary Load so the test sees exactly the catalog a
// user with that library gets.
func newLibraryCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := Load(Sources{Home: "testdata/library"})
	if err != nil {
		t.Fatalf("Load(testdata/library): %v", err)
	}
	if c.Len() < libraryFixtureMinSkills {
		t.Fatalf("fixture catalog holds %d skills, want at least %d — is testdata/library intact?", c.Len(), libraryFixtureMinSkills)
	}
	return c
}

// TestSuggestOnTheLibraryFixture pins what the band shows for the phrases people actually type,
// against a library-sized catalog. Each row is binding: a row that stops holding means the matcher
// changed, not that the row should be relaxed.
func TestSuggestOnTheLibraryFixture(t *testing.T) {
	t.Parallel()
	c := newLibraryCatalog(t)

	cases := []struct {
		name     string
		draft    string
		first    string   // the id that must rank first; empty when the result must be nil
		contains []string // ids that must appear anywhere in the result
		wantNil  bool
	}{
		{name: "grill me on this plan", draft: "grill me on this plan", first: "plan-grill"},
		{name: "grill me about this design plan", draft: "grill me about this design plan", first: "plan-grill", contains: []string{"docs-grill"}},
		{name: "audit the parser for security holes", draft: "audit the parser for security holes", contains: []string{"quality-audit", "vuln-scan"}},
		{name: "cut a release for homebrew", draft: "cut a release for homebrew", first: "homebrew-publish"},
		{name: "compact this conversation into a handoff", draft: "compact this conversation into a handoff", first: "session-handoff"},
		{name: "what changed since the last release and how do I test it", draft: "what changed since the last release and how do I test it", first: "release-test-plan"},
		{name: "get me up to speed on this project", draft: "get me up to speed on this project", first: "project-briefing"},
		{name: "two words are below the gate", draft: "fix the parser", wantNil: true},
		{name: "pure stopwords hold no content term", draft: "the and of to", wantNil: true},
		// Generic dev chat names no skill. Each of these drafts earned at least one row from the
		// matcher before the relative cutoff and the dev-generic stopwords (file, files, add)
		// landed — "add" and "file" alone admitted a skill that merely mentions them — so a row
		// that starts returning something again means the precision regressed, not that the draft
		// grew a skill.
		{name: "generic edit: add a struct field", draft: "add a field to the config struct", wantNil: true},
		{name: "generic edit: add an import", draft: "add the missing import to this file", wantNil: true},
		{name: "generic edit: move files", draft: "move these files into a new package", wantNil: true},
		{name: "generic edit: add a cli flag", draft: "add a flag to the command line parser", wantNil: true},
		{name: "generic edit: split a file", draft: "split this file into two smaller files", wantNil: true},
		{name: "generic edit: changelog entry", draft: "please add a changelog entry for the fix", wantNil: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := c.Suggest(tc.draft, nil, 0)
			ids := suggestedIDs(t, got)
			t.Logf("Suggest(%q) top-%d = %v", tc.draft, len(ids), ids)

			if tc.wantNil {
				if got != nil {
					t.Fatalf("Suggest(%q) = %v, want nil", tc.draft, ids)
				}
				return
			}
			if tc.first != "" && (len(ids) == 0 || ids[0] != tc.first) {
				t.Errorf("Suggest(%q) first = %v, want %q", tc.draft, ids, tc.first)
			}
			for _, want := range tc.contains {
				if !slices.Contains(ids, want) {
					t.Errorf("Suggest(%q) = %v, want it to contain %q", tc.draft, ids, want)
				}
			}
		})
	}
}

// TestLibraryFixtureIsSynthetic keeps testdata/library made of skills written for it: every
// SKILL.md must carry libraryFixtureBody as its whole body, so a skill copied in from anywhere
// else fails here instead of joining the fixture.
func TestLibraryFixtureIsSynthetic(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob(filepath.Join("testdata", "library", "skills", "*", "SKILL.md"))
	if err != nil {
		t.Fatalf("glob testdata/library: %v", err)
	}
	if len(paths) < libraryFixtureMinSkills {
		t.Fatalf("fixture holds %d SKILL.md files, want at least %d", len(paths), libraryFixtureMinSkills)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		_, body, ok := strings.Cut(strings.TrimPrefix(string(raw), "---\n"), "\n---\n")
		if !ok {
			t.Errorf("%s: no closing frontmatter fence", path)
			continue
		}
		if got := strings.TrimSpace(body); got != libraryFixtureBody {
			t.Errorf("%s: body = %q, want %q", path, got, libraryFixtureBody)
		}
	}
}
