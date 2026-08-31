package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// stubLookup answers with a canned result and records the query it was asked, so a test can assert
// both what the tool rendered and what it passed through.
type stubLookup struct {
	res  domain.SkillLookupResult
	seen string
}

func (s *stubLookup) LookupSkill(query string) domain.SkillLookupResult {
	s.seen = query
	return s.res
}

// callLoadSkill runs the tool with a query and returns the result, failing the test on the Go error
// that only a cancelled context produces.
func callLoadSkill(t *testing.T, tool *LoadSkill, query string) domain.ToolResult {
	t.Helper()
	res, err := tool.Execute(context.Background(), domain.ToolCall{
		ID:        "c1",
		Tool:      "load_skill",
		Arguments: json.RawMessage(`{"query":` + quoteJSON(query) + `}`),
	})
	if err != nil {
		t.Fatalf("Execute(%q) returned a Go error: %v", query, err)
	}
	return res
}

// quoteJSON is a minimal string quoter for the two-field args above — enough for the queries these
// tests use, and clearer at the call site than building a struct and marshalling it.
func quoteJSON(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

// TestLoadSkillReturnsTheBody pins the found rung: the body comes back inside the same <skill: …>
// wrapper the loop injects an attached skill through, with the files: line naming the folder — so a
// body means the same thing whichever door it came through.
func TestLoadSkillReturnsTheBody(t *testing.T) {
	t.Parallel()

	lookup := &stubLookup{res: domain.SkillLookupResult{
		Found: true,
		Skill: domain.ResolvedSkill{
			ID: "debugging", DisplayName: "Debugging",
			Body: "Reproduce it first. Then read " + domain.SkillDirToken + "/checklist.md.",
			Dir:  "shipped:debugging",
		},
	}}
	res := callLoadSkill(t, NewLoadSkill(lookup), "debugging")

	if res.IsError {
		t.Fatalf("a found skill must not be an error result: %q", res.Content)
	}
	if lookup.seen != "debugging" {
		t.Errorf("the tool searched for %q, want the query verbatim", lookup.seen)
	}
	for _, want := range []string{"<skill: Debugging>", "files: shipped:debugging", "Reproduce it first.", "</skill>"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("result does not contain %q:\n%s", want, res.Content)
		}
	}
	if strings.Contains(res.Content, domain.SkillDirToken) {
		t.Errorf("the {{SKILL_DIR}} placeholder survived into the result; the read tools refuse it:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "shipped:debugging/checklist.md") {
		t.Errorf("the placeholder did not expand to the skill's folder:\n%s", res.Content)
	}
}

// TestLoadSkillNamesTheAlsoMatchedIDs pins the confident rung's second half: the ids the winner beat
// are named OUTSIDE the <skill> wrapper, so a model that treats the wrapper's contents as
// instructions does not read a list of ids as one.
func TestLoadSkillNamesTheAlsoMatchedIDs(t *testing.T) {
	t.Parallel()

	lookup := &stubLookup{res: domain.SkillLookupResult{
		Found: true,
		Skill: domain.ResolvedSkill{ID: "brew-release", DisplayName: "Brew Release", Body: "Bump, tag, publish."},
		Also:  []string{"test-checklist", "handoff"},
	}}
	res := callLoadSkill(t, NewLoadSkill(lookup), "cut a release")

	if !strings.Contains(res.Content, "test-checklist, handoff") {
		t.Errorf("result does not name the also-matched ids:\n%s", res.Content)
	}
	body, after, found := strings.Cut(res.Content, "</skill>")
	if !found {
		t.Fatalf("result has no </skill> close:\n%s", res.Content)
	}
	if strings.Contains(body, "test-checklist") {
		t.Error("the also-matched line is inside the skill wrapper; it must sit outside it")
	}
	if !strings.Contains(after, "test-checklist") {
		t.Error("the also-matched line is missing after the wrapper")
	}
	// A skill with no folder announces none, exactly as the loop's injected block does.
	if strings.Contains(res.Content, "files:") {
		t.Errorf("a skill with no Dir must announce no files: line:\n%s", res.Content)
	}
}

// TestLoadSkillReturnsCandidates pins the ambiguous rung: no body, ids and summaries, and an
// explicit instruction to call again — the round trip that is cheaper than a wrong body.
func TestLoadSkillReturnsCandidates(t *testing.T) {
	t.Parallel()

	lookup := &stubLookup{res: domain.SkillLookupResult{Candidates: []domain.SkillCandidate{
		{ID: "code-audit", Summary: "Review code for real bugs."},
		{ID: "security-audit", Summary: "Find latent vulnerabilities."},
	}}}
	res := callLoadSkill(t, NewLoadSkill(lookup), "audit review")

	if res.IsError {
		t.Fatalf("candidates are an answer, not an error: %q", res.Content)
	}
	for _, want := range []string{`"audit review"`, "load_skill", "- code-audit: Review code for real bugs.", "- security-audit: Find latent vulnerabilities."} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("result does not contain %q:\n%s", want, res.Content)
		}
	}
	if strings.Contains(res.Content, "<skill:") {
		t.Errorf("no body may be spent on the candidates rung:\n%s", res.Content)
	}
}

// TestLoadSkillReportsAMiss pins the fourth outcome: the query is named back so the model can see
// what it asked for, and nothing is guessed.
func TestLoadSkillReportsAMiss(t *testing.T) {
	t.Parallel()

	res := callLoadSkill(t, NewLoadSkill(&stubLookup{}), "quantum chromodynamics")
	if res.IsError {
		t.Fatalf("a miss is information, not an error result: %q", res.Content)
	}
	if !strings.Contains(res.Content, `"quantum chromodynamics"`) {
		t.Errorf("the miss does not name the query back:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "<skill:") {
		t.Errorf("a miss must carry no body:\n%s", res.Content)
	}
}

// TestLoadSkillRefusesAnEmptyQuery keeps the tool from spending a lookup on nothing, and tells the
// model how to call it properly instead.
func TestLoadSkillRefusesAnEmptyQuery(t *testing.T) {
	t.Parallel()

	lookup := &stubLookup{}
	res := callLoadSkill(t, NewLoadSkill(lookup), "   ")
	if !res.IsError {
		t.Errorf("an empty query must be a result-level error, got %q", res.Content)
	}
	if lookup.seen != "" {
		t.Errorf("the catalog was searched for %q; an empty query must not reach it", lookup.seen)
	}
}

// TestLoadSkillWithoutACatalogDegrades guards the hand-built registry that registers the tool with a
// nil lookup: it reports the catalog is unavailable rather than panicking on the first call.
func TestLoadSkillWithoutACatalogDegrades(t *testing.T) {
	t.Parallel()

	res := callLoadSkill(t, NewLoadSkill(nil), "debugging")
	if !res.IsError || !strings.Contains(res.Content, "unavailable") {
		t.Errorf("a nil lookup must degrade to an unavailable result, got %+v", res)
	}
}

// TestLoadSkillIsReadOnly pins the disposition: fetching instructions writes nothing, so the tool
// runs in every mode — Plan included, which is where a model most wants a procedure before it
// touches anything.
func TestLoadSkillIsReadOnly(t *testing.T) {
	t.Parallel()

	if !NewLoadSkill(&stubLookup{}).ReadOnly() {
		t.Error("load_skill must be ReadOnly")
	}
}

// TestLoadSkillDescriptionNamesNoSkill pins ADR 0065 §7 at the one place it could quietly break:
// the description travels in EVERY request, so a skill id or summary in it would put the catalog in
// front of the model unbidden — exactly what the door exists to avoid.
func TestLoadSkillDescriptionNamesNoSkill(t *testing.T) {
	t.Parallel()

	desc := NewLoadSkill(&stubLookup{}).Description()
	for _, shipped := range []string{"debugging", "planning", "code-review", "commit-hygiene"} {
		if strings.Contains(desc, shipped) {
			t.Errorf("the tool description names the shipped skill %q; no catalog contents may ride in it", shipped)
		}
	}
	if strings.Contains(desc, "\n") {
		t.Error("the tool description must be one line")
	}
}

// TestRegistryRegistersLoadSkillOnlyWithACatalog pins the registration rule: a Driver with no skill
// catalog never offers the model a door onto nothing, and one with a catalog always does.
func TestRegistryRegistersLoadSkillOnlyWithACatalog(t *testing.T) {
	t.Parallel()

	if _, ok := NewDefaultRegistryWithHost(t.TempDir(), HostTools{}).Lookup("load_skill"); ok {
		t.Error("load_skill must be absent without a SkillLookup")
	}
	reg := NewDefaultRegistryWithHost(t.TempDir(), HostTools{SkillLookup: &stubLookup{}})
	if _, ok := reg.Lookup("load_skill"); !ok {
		t.Fatal("load_skill must be present with a SkillLookup")
	}

	// It is default-ON, so it reaches the menu with no `tools.enabled:` lift — and the ordinary
	// roster lever still closes the door.
	off := NewDefaultRegistryWithHost(t.TempDir(), HostTools{
		SkillLookup: &stubLookup{}, Disabled: []string{"load_skill"},
	})
	if _, ok := off.Lookup("load_skill"); ok {
		t.Error("`tools.disabled: [load_skill]` must take the tool off the menu")
	}
}

// TestKnownToolNamesListsLoadSkill keeps the name a host checks a configured roster entry against:
// without it `tools.disabled: [load_skill]` would be reported to the user as a typo.
func TestKnownToolNamesListsLoadSkill(t *testing.T) {
	t.Parallel()

	for _, name := range KnownToolNames() {
		if name == "load_skill" {
			return
		}
	}
	t.Error("KnownToolNames does not list load_skill; disabling it would be reported as a typo")
}
