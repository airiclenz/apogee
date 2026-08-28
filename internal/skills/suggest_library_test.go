package skills

import (
	"slices"
	"testing"
)

// libraryFixtureMinSkills is the floor the fixture catalog must clear before any ranking row runs:
// a fixture that lost its files would otherwise let every "nil" row pass for the wrong reason.
const libraryFixtureMinSkills = 20

// newLibraryCatalog loads testdata/library — a frontmatter-only copy of the owner's real skill
// library (see testdata/library/README.md) — through the ordinary Load so the test sees exactly
// the catalog a user with that library gets.
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

// TestSuggestOnTheRealLibrary pins what the band shows for the phrases people actually type,
// against the owner's library. Each row is binding: a row that stops holding means the matcher
// changed, not that the row should be relaxed.
func TestSuggestOnTheRealLibrary(t *testing.T) {
	t.Parallel()
	c := newLibraryCatalog(t)

	cases := []struct {
		name     string
		draft    string
		first    string   // the id that must rank first; empty when the result must be nil
		contains []string // ids that must appear anywhere in the result
		wantNil  bool
	}{
		{name: "grill me on this plan", draft: "grill me on this plan", first: "grill-me"},
		{name: "grill me about this design plan", draft: "grill me about this design plan", first: "grill-me", contains: []string{"grill-with-docs"}},
		{name: "audit the parser for security holes", draft: "audit the parser for security holes", contains: []string{"code-audit", "security-audit"}},
		{name: "cut a release for homebrew", draft: "cut a release for homebrew", first: "brew-release"},
		{name: "compact this conversation into a handoff", draft: "compact this conversation into a handoff", first: "handoff"},
		{name: "what changed since the last release and how do I test it", draft: "what changed since the last release and how do I test it", first: "test-checklist"},
		{name: "get me up to speed on this project", draft: "get me up to speed on this project", first: "refocus"},
		{name: "two words are below the gate", draft: "fix the parser", wantNil: true},
		{name: "pure stopwords hold no content term", draft: "the and of to", wantNil: true},
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
