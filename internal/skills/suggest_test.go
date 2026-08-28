package skills

import (
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// suggestFixture is the eight-skill library every ranking test in this file draws on: two
// overlapping audit skills, one with authored triggers, one whose summary reuses the corpus's
// common words, and four unrelated skills that must stay out of the results.
func suggestFixture() []Skill {
	return []Skill{
		{
			ID:          "code-audit",
			DisplayName: "Code Audit",
			Summary:     "High-signal code review of real bugs, security holes and architectural drift.",
		},
		{
			ID:          "security-audit",
			DisplayName: "Security Audit",
			Summary:     "Find and triage latent security vulnerabilities and holes in the workspace, ranked by exploitability.",
		},
		{
			ID:          "brew-release",
			DisplayName: "Brew Release",
			Summary:     "Publish a new version of the CLI and update the Homebrew tap formula.",
			Triggers:    []string{"cut a release", "homebrew"},
		},
		{
			ID:          "grill-me",
			DisplayName: "Grill Me",
			Summary:     "Interview the user relentlessly about a plan or design until reaching shared understanding.",
		},
		{
			ID:          "handoff",
			DisplayName: "Handoff",
			Summary:     "Compact the current conversation into a handoff document for another agent to pick up.",
		},
		{
			ID:          "refocus",
			DisplayName: "Refocus",
			Summary:     "Brief the user on the workspace state: what works, what is in flight, what is planned.",
		},
		{
			ID:          "test-checklist",
			DisplayName: "Test Checklist",
			Summary:     "Compile a release test checklist for everything implemented since the last cut release.",
		},
		{
			ID:          "note-taker",
			DisplayName: "Note Taker",
			Summary:     "Take notes on the parser and the audit trail of a workspace file.",
		},
	}
}

// newFixtureCatalog assembles a finalized catalog from the given skills, exactly as Load does —
// the index is built once at the end, so Suggest sees the same immutable snapshot production does.
func newFixtureCatalog(t *testing.T, list []Skill) *Catalog {
	t.Helper()
	c := newCatalog()
	for _, s := range list {
		c.set(s, fmt.Sprintf("/library/%s/SKILL.md", s.ID))
	}
	c.finalize()
	return c
}

// suggestedIDs reduces a result to the ids in rank order — what every ordering assertion compares.
func suggestedIDs(t *testing.T, got []Suggestion) []string {
	t.Helper()
	ids := make([]string, 0, len(got))
	for _, s := range got {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestSuggestBelowTheRawWordFloorReturnsNothing(t *testing.T) {
	t.Parallel()
	c := newFixtureCatalog(t, suggestFixture())

	// "security audit" would clear the evidence gate twice over; it is the word count alone that
	// keeps the band dark.
	for _, draft := range []string{"", "audit", "please audit", "security audit", "a b"} {
		if got := c.Suggest(draft, nil, 0); got != nil {
			t.Errorf("Suggest(%q) = %v, want nil below %d words", draft, suggestedIDs(t, got), minDraftWords)
		}
	}
}

func TestSuggestGateCountsRawWords(t *testing.T) {
	t.Parallel()
	c := newFixtureCatalog(t, suggestFixture())

	cases := []struct {
		name  string
		draft string
		want  []string
	}{
		{"five words with two content terms clear the gate", "grill me on this plan", []string{"grill-me"}},
		{"the same content terms in two words do not", "grill plan", nil},
		{"four words of pure stopwords score nothing", "the and of to", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := suggestedIDs(t, c.Suggest(tc.draft, nil, 0))
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Suggest(%q) = %v, want %v", tc.draft, got, tc.want)
			}
		})
	}
}

func TestSuggestRanksTheStrongerLexicalMatchFirst(t *testing.T) {
	t.Parallel()
	c := newFixtureCatalog(t, suggestFixture())

	got := c.Suggest("please audit the parser for security holes", nil, 0)

	want := []string{"security-audit", "code-audit", "note-taker"}
	if ids := suggestedIDs(t, got); !reflect.DeepEqual(ids, want) {
		t.Fatalf("Suggest = %v, want %v", ids, want)
	}
	for _, s := range got {
		if s.TriggerHit {
			t.Errorf("%s reported a trigger hit; the fixture declares none for it", s.ID)
		}
	}
}

func TestSuggestPutsATriggerHitFirst(t *testing.T) {
	t.Parallel()
	c := newFixtureCatalog(t, suggestFixture())

	got := c.Suggest("cut a release for homebrew", nil, 0)

	if len(got) < 2 {
		t.Fatalf("Suggest = %v, want the triggered skill and at least one lexical match", suggestedIDs(t, got))
	}
	if got[0].ID != "brew-release" || !got[0].TriggerHit {
		t.Fatalf("top suggestion = %+v, want brew-release with TriggerHit", got[0])
	}
	// The boost must lift the trigger above a skill that shares two draft terms lexically, which
	// is what test-checklist ("the last cut release") is in the fixture for.
	if got[1].ID != "test-checklist" || got[1].TriggerHit {
		t.Fatalf("second suggestion = %+v, want test-checklist without a trigger hit", got[1])
	}
	if got[0].Score <= got[1].Score {
		t.Errorf("trigger score %v did not beat the lexical score %v", got[0].Score, got[1].Score)
	}
}

func TestSuggestPrefixMatchesAStemEitherWay(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// skills is the catalog for the case; nil means suggestFixture.
		skills []Skill
		draft  string
		// admits must appear in the result; nil means the result must be empty.
		admits string
	}{
		{
			// "releas" stems to "relea", a prefix of the indexed "release": the query side is
			// the shorter one.
			name:   "a truncated draft term finds the longer document term",
			draft:  "cut a releas for the tap",
			admits: "brew-release",
		},
		{
			// refocus indexes "planned" as "plann"; the draft's "plan" is its prefix and the
			// second matched term the evidence gate needs — "workspace" alone would not admit it.
			name:   "a short draft term finds the longer document stem",
			draft:  "plan the workspace now",
			admits: "refocus",
		},
		{
			// "checklists" stems to "checklist" exactly; prefix matching must not disturb a
			// document that already matches every term outright.
			name:   "exact matches still land as before",
			draft:  "compile release checklists now",
			admits: "test-checklist",
		},
		{
			name: "a three-rune term never matches by prefix",
			skills: []Skill{
				{ID: "branch-cutter", DisplayName: "Branch Cutter", Summary: "Cutting a branch off the tap."},
			},
			draft:  "cut the tap now",
			admits: "",
		},
		{
			name: "the same document is admitted once the term reaches the floor",
			skills: []Skill{
				{ID: "branch-cutter", DisplayName: "Branch Cutter", Summary: "Cutting a branch off the tap."},
			},
			draft:  "cutting the tap now",
			admits: "branch-cutter",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			list := tc.skills
			if list == nil {
				list = suggestFixture()
			}
			c := newFixtureCatalog(t, list)

			got := suggestedIDs(t, c.Suggest(tc.draft, nil, 0))

			if tc.admits == "" {
				if len(got) != 0 {
					t.Errorf("Suggest(%q) = %v, want nothing admitted", tc.draft, got)
				}
				return
			}
			if !slices.Contains(got, tc.admits) {
				t.Errorf("Suggest(%q) = %v, want it to admit %s", tc.draft, got, tc.admits)
			}
		})
	}
}

func TestSuggestNameHitsOutrankSummaryRepeats(t *testing.T) {
	t.Parallel()
	// untriggeredFixture is suggestFixture with brew-release's triggers gone, so nothing but the
	// lexical score separates it from test-checklist, whose summary repeats "release" and holds
	// "cut" outright.
	untriggeredFixture := suggestFixture()
	for i := range untriggeredFixture {
		if untriggeredFixture[i].ID == "brew-release" {
			untriggeredFixture[i].Triggers = nil
		}
	}
	// equalBags holds two documents with the SAME term bag and length, differing only in which
	// terms sit in the id and display name — so BM25 ties them, the id tiebreak would put
	// formula-tap first, and only the name bonus can lift release-notes above it.
	equalBags := []Skill{
		{ID: "release-notes", DisplayName: "Release Notes", Summary: "Publish them to the tap formula, then the tap formula."},
		{ID: "formula-tap", DisplayName: "Formula Tap", Summary: "Publish the release notes and the release notes."},
	}

	cases := []struct {
		name   string
		skills []Skill
		draft  string
		want   []string
	}{
		{"an id hit beats a summary repeat", untriggeredFixture, "cut a release for homebrew", []string{"brew-release", "test-checklist"}},
		{"the bonus alone decides an otherwise exact tie", equalBags, "publish the release notes", []string{"release-notes", "formula-tap"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := newFixtureCatalog(t, tc.skills)

			got := c.Suggest(tc.draft, nil, 0)

			if ids := suggestedIDs(t, got); !reflect.DeepEqual(ids, tc.want) {
				t.Fatalf("Suggest(%q) = %v, want %v", tc.draft, ids, tc.want)
			}
			if got[0].TriggerHit {
				t.Errorf("%s reported a trigger hit; the fixture declares none for it", got[0].ID)
			}
		})
	}
}

func TestSuggestASingleSharedTermAdmitsNothing(t *testing.T) {
	t.Parallel()
	c := newFixtureCatalog(t, suggestFixture())

	// "compile" appears in exactly one summary and nothing else in the draft matches anywhere, so
	// the evidence gate must keep the band dark rather than offer a one-word coincidence.
	if got := c.Suggest("compile the daemon binary", nil, 0); got != nil {
		t.Fatalf("Suggest = %v, want nil below %d matched terms", suggestedIDs(t, got), minMatchedTerms)
	}
}

func TestSuggestExcludeDropsASkillAndTheNextFillsIn(t *testing.T) {
	t.Parallel()
	c := newFixtureCatalog(t, suggestFixture())
	const draft = "please audit the parser for security holes"

	before := suggestedIDs(t, c.Suggest(draft, nil, 2))
	after := suggestedIDs(t, c.Suggest(draft, func(id string) bool { return id == "security-audit" }, 2))

	if want := []string{"security-audit", "code-audit"}; !reflect.DeepEqual(before, want) {
		t.Fatalf("Suggest without exclude = %v, want %v", before, want)
	}
	if want := []string{"code-audit", "note-taker"}; !reflect.DeepEqual(after, want) {
		t.Fatalf("Suggest with exclude = %v, want %v", after, want)
	}
}

func TestSuggestLimitCapsTheResult(t *testing.T) {
	t.Parallel()
	c := newFixtureCatalog(t, suggestFixture())
	const draft = "please audit the parser for security holes"

	if got := c.Suggest(draft, nil, 1); len(got) != 1 {
		t.Errorf("Suggest(limit 1) returned %d results, want 1", len(got))
	}
	if got := c.Suggest(draft, nil, 0); len(got) != defaultSuggestLimit {
		t.Errorf("Suggest(limit 0) returned %d results, want the default %d", len(got), defaultSuggestLimit)
	}
	if got := c.Suggest(draft, nil, -5); len(got) != defaultSuggestLimit {
		t.Errorf("Suggest(limit -5) returned %d results, want the default %d", len(got), defaultSuggestLimit)
	}
	if got := c.Suggest(draft, nil, 50); len(got) != 3 {
		t.Errorf("Suggest(limit 50) returned %d results, want the 3 admitted", len(got))
	}
}

func TestSuggestBreaksScoreTiesByIDAscending(t *testing.T) {
	t.Parallel()
	// Two skills with identical summaries and equal-length ids score identically on a draft that
	// only touches the shared words, so ONLY the id tiebreak can decide the order.
	c := newFixtureCatalog(t, []Skill{
		{ID: "twin-beta", DisplayName: "Twin Beta", Summary: "A duplicated fixture entry for tie ordering."},
		{ID: "twin-alpha", DisplayName: "Twin Alpha", Summary: "A duplicated fixture entry for tie ordering."},
	})

	got := c.Suggest("duplicated fixture entry ordering", nil, 0)

	want := []string{"twin-alpha", "twin-beta"}
	if ids := suggestedIDs(t, got); !reflect.DeepEqual(ids, want) {
		t.Fatalf("Suggest = %v, want %v (id ascending on a score tie)", ids, want)
	}
	if got[0].Score != got[1].Score {
		t.Fatalf("fixture no longer ties: %v vs %v", got[0].Score, got[1].Score)
	}
}

func TestSuggestCarriesTheRowFields(t *testing.T) {
	t.Parallel()
	c := newFixtureCatalog(t, suggestFixture())

	got := c.Suggest("cut a release for homebrew", nil, 1)

	if len(got) != 1 {
		t.Fatalf("Suggest returned %d results, want 1", len(got))
	}
	if got[0].DisplayName != "Brew Release" {
		t.Errorf("DisplayName = %q, want %q", got[0].DisplayName, "Brew Release")
	}
	if got[0].Summary == "" {
		t.Error("Summary is empty; the band has nothing to paint")
	}
}

// TestSuggestIndexesTheDescriptionPastTheMenuClamp pins that the matcher sees a skill's whole
// description: a phrase the author placed after the 200-rune Summary clamp still finds the skill.
func TestSuggestIndexesTheDescriptionPastTheMenuClamp(t *testing.T) {
	t.Parallel()
	filler := strings.Repeat("filler ", maxSummaryLen/len("filler ")+1)
	sk, err := parseSkill("---\nid: checklist\ndescription: "+filler+" compile the release checklist\n---\nbody", "checklist")
	if err != nil {
		t.Fatalf("parseSkill: %v", err)
	}
	if strings.Contains(sk.Summary, "compile") {
		t.Fatalf("Summary %q still holds the phrase — the clamp must sit before it", sk.Summary)
	}
	c := newFixtureCatalog(t, []Skill{sk})

	got := suggestedIDs(t, c.Suggest("compile the release checklist now", nil, 0))

	if len(got) != 1 || got[0] != "checklist" {
		t.Errorf("Suggest = %v, want [checklist] — the phrase past the clamp must be indexed", got)
	}
}

func TestSuggestOnAnUnfinalizedOrEmptyCatalogReturnsNothing(t *testing.T) {
	t.Parallel()

	if got := newCatalog().Suggest("audit the parser for security", nil, 0); got != nil {
		t.Errorf("unfinalized catalog suggested %v, want nil", suggestedIDs(t, got))
	}
	if got := newFixtureCatalog(t, nil).Suggest("audit the parser for security", nil, 0); got != nil {
		t.Errorf("empty catalog suggested %v, want nil", suggestedIDs(t, got))
	}
}

func TestSuggestExcludesEveryAdmittedSkill(t *testing.T) {
	t.Parallel()
	c := newFixtureCatalog(t, suggestFixture())

	got := c.Suggest("please audit the parser for security holes", func(string) bool { return true }, 0)

	if got != nil {
		t.Fatalf("Suggest = %v, want nil when everything is excluded", suggestedIDs(t, got))
	}
}

func TestProviderSuggestServesTheCurrentSnapshot(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, "skills"), "brew-release",
		"---\nid: brew-release\ndisplayName: Brew Release\nsummary: Publish a new version and update the Homebrew tap.\ntriggers:\n  - cut a release\n---\nbody\n")

	p := NewProvider(Sources{Home: home})

	got := p.Suggest("time to cut a release now", nil, 0)
	if len(got) != 1 || got[0].ID != "brew-release" || !got[0].TriggerHit {
		t.Fatalf("Provider.Suggest = %+v, want the triggered brew-release", got)
	}
}

func TestTokenize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"hyphenated id splits into words", "code-audit", []string{"code", "audit"}},
		{"stopwords and one-rune fragments drop", "please review the a I diffs", []string{"review", "diff"}},
		{"plural in ies stems to y", "utilities", []string{"utility"}},
		{"plural in es keeps its e outside a sibilant", "holes releases changes", []string{"hole", "release", "change"}},
		{"plural in es after a sibilant drops the es", "boxes wishes classes", []string{"box", "wish", "class"}},
		{"words ending in ss or us are not plurals", "stress process status focus", []string{"stress", "process", "status", "focus"}},
		{"gerund and past tense stem", "running planned planning", []string{"runn", "plann", "plann"}},
		{"words ending in eed keep their ed", "speed feed agreed", []string{"speed", "feed", "agreed"}},
		{"a stem that would get too short keeps the word", "goes ties", []string{"goes", "ties"}},
		{"digits survive as terms", "release v2 builds 1024", []string{"release", "v2", "build", "1024"}},
		{"case and punctuation are normalised", "Security: HOLES, found!", []string{"security", "hole", "found"}},
		{"nothing but stopwords yields nothing", "the and of to", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tokenize(tc.in)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("tokenize(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestTokenizeDropsTheStopwordsBetweenTriggerWords(t *testing.T) {
	t.Parallel()
	// The contiguity check runs over tokenized sequences, which is why an author's "cut a release"
	// still hits a user's "cut the release".
	if !containsSequence(tokenize("cut the release tonight"), tokenize("cut a release")) {
		t.Error("a trigger phrase did not survive differing stopwords between its words")
	}
	if containsSequence(tokenize("release the cut branch"), tokenize("cut a release")) {
		t.Error("a trigger phrase matched out of order; the check must be contiguous")
	}
	if containsSequence(tokenize("anything at all"), tokenize("the and of")) {
		t.Error("an all-stopword trigger matched; an empty phrase must never hit")
	}
}

// BenchmarkSuggest documents the per-keystroke cost over a library far larger than any real one.
// It asserts nothing — it exists so a future change to the matcher shows its price.
func BenchmarkSuggest(b *testing.B) {
	list := make([]Skill, 0, 1000)
	for i := range 1000 {
		list = append(list, Skill{
			ID:          fmt.Sprintf("synthetic-skill-%d", i),
			DisplayName: fmt.Sprintf("Synthetic Skill %d", i),
			Summary:     fmt.Sprintf("Audit the parser and review the workspace for security holes in module %d.", i),
			Triggers:    []string{fmt.Sprintf("audit module %d", i)},
		})
	}
	c := newCatalog()
	for _, s := range list {
		c.set(s, fmt.Sprintf("/library/%s/SKILL.md", s.ID))
	}
	c.finalize()

	b.ResetTimer()
	for range b.N {
		_ = c.Suggest("please audit the parser for security holes", nil, 0)
	}
}
