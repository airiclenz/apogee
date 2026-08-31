package skills

import (
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// confidentMargin is how far ahead of the runner-up a top hit must score before a lookup spends a
// BODY on it rather than handing back candidates. One and a half times is the smallest margin that
// is unmistakably a lead rather than two skills sharing the query's vocabulary: the scores here are
// BM25 sums over a handful of terms, where two genuinely-similar skills land within a few percent
// of each other and a real winner — the one whose id or display name IS the query — carries the
// name bonus or a trigger boost on top and clears this easily.
//
// It is a threshold on a score that "is not a probability and carries no threshold a caller should
// test against" (Suggestion.Score) for exactly one caller and exactly one purpose: deciding whether
// to spend tokens now or ask the model to spell an id. Getting it wrong costs one extra round trip,
// never a wrong answer — the candidates rung names the same skills the confident rung would have
// picked from.
const confidentMargin = 1.5

// maxLookupCandidates caps the id+summary list a non-confident lookup returns. Five is enough for
// the model to recognise the one it meant and short enough that the reply stays a menu rather than
// a catalog dump — which is the property ADR 0065 §7 protects: the model asked a question and gets
// an answer, not a listing of everything apogee knows.
const maxLookupCandidates = 5

// maxAlsoMatched caps the "also matched" ids named beside a confident body for the same reason,
// with less room: the body is already spent and this line exists only so the model can see what it
// did not get.
const maxAlsoMatched = 3

// LookupResult is one Lookup's answer in package types — the sibling of the domain-typed
// SkillLookupResult, exactly as Resolve is the sibling of ResolveSkills.
//
// Found reports that a body is being handed back, in Skill; Also names the other skills the query
// matched and is empty when the query was an exact id or when only one skill cleared the gate.
// Candidates carries the id+summary rung, populated only when nothing was confident enough for a
// body. Neither set ⇒ nothing matched.
type LookupResult struct {
	Found      bool
	Skill      Skill
	Also       []string
	Candidates []Suggestion
}

// Lookup answers the model's load_skill query against this catalog (ADR 0065 §6), in the three
// shapes the tool can return:
//
//   - an EXACT id — the query is the name of a skill, give or take surrounding space, a leading
//     "/" (the spelling the user's chat token uses, so the one a model is most likely to copy) and
//     letter case — returns that skill's body with nothing ranked at all;
//   - a CONFIDENT match — the ADR 0061 matcher's ranking and evidence gate (rank), where either
//     exactly one skill cleared the gate or the top one leads the runner-up by confidentMargin —
//     returns the winner's body and names up to maxAlsoMatched other ids that matched;
//   - anything less returns up to maxLookupCandidates id+summary Candidates and no body, leaving
//     the model to call again with an id it can now spell.
//
// Nothing clearing the gate is the fourth outcome and the zero value: no body, no candidates. The
// caller reports the miss naming the query back — a lookup never guesses, because a wrong body is
// worse than no body, and the model can always ask again in words closer to what it wants.
//
// An unfinalized catalog (one that never went through finalize) still answers the EXACT rung: the
// id map is what Get reads, and a query that names a skill outright should not depend on an index
// the scan may not have built.
func (c *Catalog) Lookup(query string) LookupResult {
	if c == nil {
		return LookupResult{}
	}
	if s, ok := c.Get(normalizeLookupID(query)); ok {
		return LookupResult{Found: true, Skill: s}
	}
	if c.idx == nil {
		return LookupResult{}
	}

	ranked := rank(c, query, nil)
	switch {
	case len(ranked) == 0:
		return LookupResult{}
	case len(ranked) == 1 || ranked[0].Score >= confidentMargin*ranked[1].Score:
		s, ok := c.Get(ranked[0].ID)
		if !ok {
			// Unreachable in practice — rank only ever names ids of this same snapshot — but a
			// missing body must degrade to the candidates rung rather than to an empty "found".
			break
		}
		return LookupResult{Found: true, Skill: s, Also: candidateIDs(ranked[1:], maxAlsoMatched)}
	}

	if len(ranked) > maxLookupCandidates {
		ranked = ranked[:maxLookupCandidates]
	}
	return LookupResult{Candidates: ranked}
}

// LookupSkill satisfies domain.SkillLookup: the same answer in the loop-facing types, with the
// winner reduced to a domain.ResolvedSkill so the tool renders a looked-up body through exactly the
// fields the loop renders an attached one through (ID, DisplayName, Body, Dir — the folder address
// the model reads the skill's bundled files from).
func (c *Catalog) LookupSkill(query string) domain.SkillLookupResult {
	res := c.Lookup(query)
	out := domain.SkillLookupResult{Found: res.Found, Also: res.Also}
	if res.Found {
		out.Skill = domain.ResolvedSkill{
			ID:          res.Skill.ID,
			DisplayName: res.Skill.DisplayName,
			Body:        res.Skill.Body,
			Dir:         res.Skill.Dir,
		}
	}
	for _, cand := range res.Candidates {
		out.Candidates = append(out.Candidates, domain.SkillCandidate{ID: cand.ID, Summary: cand.Summary})
	}
	return out
}

// normalizeLookupID reduces a query to the id it would be if it were one: trimmed, stripped of the
// leading "/" the chat token carries, and lowercased, because ids come from folder names and a
// model that reads "debugging" in a summary may well write "Debugging" back.
func normalizeLookupID(query string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(query), "/"))
}

// candidateIDs lists the ids of at most limit suggestions — the "also matched" line's contents.
// Nil for an empty set, so a single-gate-passer lookup carries no line at all rather than an empty
// one.
func candidateIDs(from []Suggestion, limit int) []string {
	if len(from) == 0 {
		return nil
	}
	if len(from) > limit {
		from = from[:limit]
	}
	out := make([]string, 0, len(from))
	for _, s := range from {
		out = append(out, s.ID)
	}
	return out
}

// Compile-time proof the catalog satisfies the model-facing lookup seam, exactly as it satisfies
// the loop's resolver seam (ADR 0010: skills depends on domain, never the reverse).
var _ domain.SkillLookup = (*Catalog)(nil)
