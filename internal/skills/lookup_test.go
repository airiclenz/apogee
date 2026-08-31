package skills

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestLookupExactIDReturnsTheBody pins the first rung: a query that NAMES a skill is answered with
// that skill's body and nothing is ranked, so a model that already knows the id never pays for a
// second call — and never gets a differently-spelled neighbour instead.
func TestLookupExactIDReturnsTheBody(t *testing.T) {
	t.Parallel()
	c := newFixtureCatalog(t, lookupFixture())

	for _, query := range []string{"grill-me", "  grill-me  ", "/grill-me", "Grill-Me"} {
		got := c.Lookup(query)
		if !got.Found {
			t.Fatalf("Lookup(%q) found nothing; an exact id must return its body", query)
		}
		if got.Skill.ID != "grill-me" {
			t.Errorf("Lookup(%q) returned %q, want grill-me", query, got.Skill.ID)
		}
		if got.Skill.Body == "" {
			t.Errorf("Lookup(%q) returned an empty body", query)
		}
		if got.Also != nil {
			t.Errorf("Lookup(%q) named also-matched %v; an exact id ranks nothing", query, got.Also)
		}
	}
}

// TestLookupConfidentMatchReturnsBodyAndAlsoMatched pins the second rung: a query no id spells but
// one skill clearly owns comes back as a body, with the ids it beat named beside it so the model
// can see what it did not get.
func TestLookupConfidentMatchReturnsBodyAndAlsoMatched(t *testing.T) {
	t.Parallel()
	c := newFixtureCatalog(t, lookupFixture())

	got := c.Lookup("cut a release")
	if !got.Found {
		t.Fatalf("Lookup(\"cut a release\") found nothing; the authored trigger must win outright")
	}
	if got.Skill.ID != "brew-release" {
		t.Fatalf("Lookup(\"cut a release\") = %q, want brew-release", got.Skill.ID)
	}
	if len(got.Also) == 0 {
		t.Error("a confident match over other gate-passers must name them; Also is empty")
	}
	for _, id := range got.Also {
		if id == got.Skill.ID {
			t.Errorf("Also names the winner %q back", id)
		}
	}
	if len(got.Also) > maxAlsoMatched {
		t.Errorf("Also names %d ids, capped at %d", len(got.Also), maxAlsoMatched)
	}
	if got.Candidates != nil {
		t.Error("a found lookup must not also return candidates")
	}
}

// TestLookupSoleGatePasserIsConfident pins the other half of the confident rung: with nothing to
// compare against, one gate-passer IS the answer — the margin rule has no runner-up to measure.
func TestLookupSoleGatePasserIsConfident(t *testing.T) {
	t.Parallel()
	c := newFixtureCatalog(t, lookupFixture())

	got := c.Lookup("handoff document conversation")
	if !got.Found || got.Skill.ID != "handoff" {
		t.Fatalf("Lookup = %+v, want the sole gate-passer handoff returned as a body", got)
	}
	if got.Also != nil {
		t.Errorf("a sole gate-passer has nothing to also-match; got %v", got.Also)
	}
}

// TestLookupAmbiguousQueryReturnsCandidates pins the third rung: when two skills share the query's
// vocabulary closely enough that neither leads, NO body is spent — the model gets ids and summaries
// and calls again with one it can now spell.
func TestLookupAmbiguousQueryReturnsCandidates(t *testing.T) {
	t.Parallel()
	c := newFixtureCatalog(t, lookupFixture())

	got := c.Lookup("audit review")
	if got.Found {
		t.Fatalf("Lookup(\"audit review\") returned %q as a confident body; two audit skills share this query", got.Skill.ID)
	}
	if len(got.Candidates) < 2 {
		t.Fatalf("Lookup(\"audit review\") returned %d candidates, want the ambiguous set", len(got.Candidates))
	}
	if len(got.Candidates) > maxLookupCandidates {
		t.Errorf("returned %d candidates, capped at %d", len(got.Candidates), maxLookupCandidates)
	}
	for _, cand := range got.Candidates {
		if cand.Summary == "" {
			t.Errorf("candidate %q carries no summary; the rung is id + summary", cand.ID)
		}
	}
}

// TestLookupMissReturnsNothing pins the fourth outcome: a query nothing matches is answered with
// neither a body nor a guess. A wrong body is worse than no body.
func TestLookupMissReturnsNothing(t *testing.T) {
	t.Parallel()
	c := newFixtureCatalog(t, lookupFixture())

	for _, query := range []string{"", "   ", "quantum chromodynamics"} {
		got := c.Lookup(query)
		if got.Found || len(got.Candidates) > 0 {
			t.Errorf("Lookup(%q) = %+v, want no match at all", query, got)
		}
	}
}

// TestLookupAnswersASingleWordQuery is the reason rank does not carry Suggest's minDraftWords
// floor: that floor keeps the suggestion band from flickering at a half-typed draft, while a lookup
// argument is deliberate and usually short. A door that answered "no match" to "debugging" would be
// wrong on the queries most likely to be right.
func TestLookupAnswersASingleWordQuery(t *testing.T) {
	t.Parallel()
	c := newFixtureCatalog(t, lookupFixture())

	if got := c.Suggest("homebrew", nil, 0); got != nil {
		t.Fatalf("Suggest(%q) = %v; the band's word floor must stay where it is", "homebrew", suggestedIDs(t, got))
	}
	got := c.Lookup("homebrew")
	if !got.Found || got.Skill.ID != "brew-release" {
		t.Fatalf("Lookup(\"homebrew\") = %+v, want brew-release's body", got)
	}
}

// TestLookupSkillReturnsDomainTypes pins the domain-facing adapter: the winner arrives as the same
// four fields the loop injects an ATTACHED skill through, so a body means the same thing whichever
// door it came through.
func TestLookupSkillReturnsDomainTypes(t *testing.T) {
	t.Parallel()
	c := newFixtureCatalog(t, lookupFixture())

	got := c.LookupSkill("grill-me")
	want := domain.SkillLookupResult{
		Found: true,
		Skill: domain.ResolvedSkill{
			ID:          "grill-me",
			DisplayName: "Grill Me",
			Body:        "Interview the user until the plan holds.",
			Dir:         "/library/grill-me",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LookupSkill(\"grill-me\") = %+v, want %+v", got, want)
	}

	amb := c.LookupSkill("audit review")
	if amb.Found || len(amb.Candidates) < 2 {
		t.Fatalf("LookupSkill(\"audit review\") = %+v, want the candidate rung", amb)
	}
	for _, cand := range amb.Candidates {
		if cand.ID == "" || cand.Summary == "" {
			t.Errorf("candidate %+v is missing half its rung", cand)
		}
	}
}

// TestLookupOnAnUnfinalizedCatalogStillAnswersExactIDs guards the one path that does not depend on
// the index: a catalog that never finalized has no ranking to do, but a query naming a skill
// outright is answered off the id map — and a nil catalog answers nothing rather than panicking.
func TestLookupOnAnUnfinalizedCatalogStillAnswersExactIDs(t *testing.T) {
	t.Parallel()

	c := newCatalog()
	for _, s := range lookupFixture() {
		c.set(s, "/library/"+s.ID+"/SKILL.md")
	}
	if got := c.Lookup("grill-me"); !got.Found {
		t.Error("an unfinalized catalog must still answer an exact id")
	}
	if got := c.Lookup("cut a release"); got.Found || len(got.Candidates) > 0 {
		t.Errorf("an unfinalized catalog has no index to rank with; got %+v", got)
	}

	var nilCatalog *Catalog
	if got := nilCatalog.Lookup("grill-me"); got.Found {
		t.Error("a nil catalog must answer nothing")
	}
}

// TestProviderLookupReadsTheLiveSnapshot pins the seam the tool actually holds: the Provider
// answers off whatever catalog the last Reload installed, so a skill added mid-session is reachable
// through the door with no re-wiring.
func TestProviderLookupReadsTheLiveSnapshot(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	p := NewProvider(Sources{Home: home})
	if got := p.Lookup("debugging"); got.Found {
		t.Fatalf("an empty library answered %q", got.Skill.ID)
	}

	writeSkill(t, filepath.Join(home, "skills"), "debugging", "---\nid: debugging\nname: Debugging\nsummary: Chase a bug down.\n---\n\nReproduce it first.\n")
	if err := p.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	got := p.LookupSkill("debugging")
	if !got.Found || got.Skill.ID != "debugging" {
		t.Fatalf("LookupSkill after Reload = %+v, want the freshly added skill", got)
	}
	if !strings.Contains(got.Skill.Body, "Reproduce it first.") {
		t.Errorf("body %q does not carry the SKILL.md's instructions", got.Skill.Body)
	}
}

// lookupFixture is the library the lookup tests draw on: two skills that overlap on "audit" and
// "review" (so no query about either can be confident), one with authored triggers that wins
// outright OVER a skill whose summary happens to hold the same words (the also-matched line's
// case), and two unrelated ones. Every skill carries a Body and a Dir, which suggestFixture's
// ranking-only skills do not need.
func lookupFixture() []Skill {
	return []Skill{
		{
			ID:          "code-audit",
			DisplayName: "Code Audit",
			Summary:     "High-signal code review of real bugs and audit findings.",
			Body:        "Review the code. Report only verified findings.",
			Dir:         "/library/code-audit",
		},
		{
			ID:          "security-audit",
			DisplayName: "Security Audit",
			Summary:     "Security review and audit of latent vulnerabilities.",
			Body:        "Audit for injection, secrets and access control.",
			Dir:         "/library/security-audit",
		},
		{
			ID:          "brew-release",
			DisplayName: "Brew Release",
			Summary:     "Publish a new version and update the Homebrew tap formula.",
			Triggers:    []string{"cut a release", "homebrew"},
			Body:        "Bump, tag, publish, then update the tap.",
			Dir:         "/library/brew-release",
		},
		{
			ID:          "grill-me",
			DisplayName: "Grill Me",
			Summary:     "Interview the user relentlessly about a plan or design.",
			Body:        "Interview the user until the plan holds.",
			Dir:         "/library/grill-me",
		},
		{
			ID:          "test-checklist",
			DisplayName: "Test Checklist",
			Summary:     "Compile a release test checklist for everything implemented since the last cut release.",
			Body:        "List what changed, then say how to test each of it.",
			Dir:         "/library/test-checklist",
		},
		{
			ID:          "handoff",
			DisplayName: "Handoff",
			Summary:     "Compact the current conversation into a handoff document.",
			Body:        "Write what the next agent needs and nothing else.",
			Dir:         "/library/handoff",
		},
	}
}
