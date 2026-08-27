package skills

import (
	"errors"
	"slices"
	"sort"

	"github.com/airiclenz/apogee/internal/domain"
)

// Catalog is the outcome of one discovery scan: the skills that loaded, keyed by ID, AND the
// SKILL.md files that did not (Skipped). It is built by Load and read by two consumers over
// different seams: the TUI's merged "/" menu (List/Get/Skipped) and the agent loop (ResolveSkills,
// the domain.SkillResolver it satisfies). The same *Catalog is injected into both — it is
// read-only after Load, so sharing it across the UI and the loop goroutine is safe (no method
// mutates byID, pathByID or skipped).
//
// The failures ride along WITH the loaded skills rather than beside them so a reader can never
// pair a fresh listing with a stale set of failures: Provider swaps the whole *Catalog under one
// atomic pointer, which makes "19 loaded, 1 skipped" always one scan's answer.
type Catalog struct {
	byID map[string]Skill
	// pathByID is the SKILL.md each live skill was loaded from, kept only so a later collision
	// can name the file it is displacing. It is deliberately not exposed: Skill.Dir already
	// carries the folder for consumers, and this map exists for the shadow record alone.
	pathByID map[string]string
	skipped  []SkipError
	// idx is the suggestion matcher's BM25 index over byID, built once by finalize at the end of
	// the scan (suggest.go). It is nil until then, which is exactly what Suggest tests before it
	// answers — an unfinalized catalog suggests nothing rather than half a corpus.
	idx *index
}

// newCatalog returns an empty catalog ready for set. Load always returns a non-nil *Catalog —
// even an empty one — so a nil pointer never reaches a domain.SkillResolver field (a typed-nil
// interface there would pass a `!= nil` guard yet panic on call).
func newCatalog() *Catalog {
	return &Catalog{byID: map[string]Skill{}, pathByID: map[string]string{}}
}

// set inserts a skill by ID, remembering the absolute SKILL.md path it came from. An id already
// present is KEPT: load.go walks the source dirs in decreasing priority, so the FIRST writer of an
// id — the highest-priority source — wins. Under ADR 0032 that is the user's global library over
// either workspace source.
//
// Keep-first rather than last-write-wins is what lets the global cap (maxSkills) agree with that
// precedence instead of undoing it (audit 2026-08-25 F-06): a skill that lost the cap was never
// read, so no write-order rule could hand it the id back. With the highest-priority source walked
// first, the copy already in the map is by construction the one that outranks the newcomer.
//
// The displaced skill is never dropped silently. Its SKILL.md is recorded through the same skip
// channel as a malformed file, carrying a ShadowedError that names the winner, so /skills can
// report both which copy is live and which was shadowed. This also closes the same-source case —
// two folders in one dir with colliding ids — which used to lose one without a word, against this
// package's own "soft must not mean silent" contract (doc.go); there the walk's lexical order
// decides, so the folder reached first is the live copy.
func (c *Catalog) set(s Skill, path string) {
	if prev, ok := c.pathByID[s.ID]; ok {
		c.addSkip(SkipError{Path: path, Err: ShadowedError{By: prev}})
		return
	}
	c.byID[s.ID] = s
	c.pathByID[s.ID] = path
}

// finalize builds the suggestion index over everything the scan loaded. It is the LAST step of a
// scan and the point the catalog becomes immutable: after it, no method mutates any field, so the
// Provider can hand ONE snapshot to a matcher running on the UI goroutine and a resolver running
// on the loop goroutine with no lock and no lazy build. Load calls it exactly once; a Catalog that
// never went through it serves everything else normally and simply suggests nothing.
func (c *Catalog) finalize() { c.idx = buildIndex(c.byID) }

// addSkip records one SKILL.md discovery could not load, in walk order.
func (c *Catalog) addSkip(e SkipError) {
	c.skipped = append(c.skipped, e)
}

// Skipped returns the SKILL.md files this scan found but could not load, in discovery order —
// the "why is my skill missing?" half of the /skills report. It returns a copy, keeping the
// catalog read-only after Load exactly as List does.
func (c *Catalog) Skipped() []SkipError {
	return slices.Clone(c.skipped)
}

// skipError joins the recorded skips into the single soft error Load returns, so a caller that
// wants one error value gets the same set Skipped exposes structurally. Nil when nothing was
// skipped (errors.Join of nothing).
func (c *Catalog) skipError() error {
	errs := make([]error, 0, len(c.skipped))
	for _, e := range c.skipped {
		errs = append(errs, e)
	}
	return errors.Join(errs...)
}

// List returns every skill sorted by DisplayName (then ID, so the order is total and stable
// across equal display names) — the order the merged "/" menu shows.
func (c *Catalog) List() []Skill {
	out := make([]Skill, 0, len(c.byID))
	for _, s := range c.byID {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DisplayName != out[j].DisplayName {
			return out[i].DisplayName < out[j].DisplayName
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Len reports how many distinct skills the catalog holds. Discovery reads it to enforce a
// global count cap so a hostile repo cannot grow the catalog without bound (load.go).
func (c *Catalog) Len() int { return len(c.byID) }

// Get looks up a skill by exact ID — the by-id lookup the TUI uses to label an attached chip.
func (c *Catalog) Get(id string) (Skill, bool) {
	s, ok := c.byID[id]
	return s, ok
}

// Resolve returns the skills for ids in the order given, skipping any unknown ID (the caller
// decides whether a miss is worth reporting). It is the package-typed sibling of ResolveSkills.
func (c *Catalog) Resolve(ids []string) []Skill {
	out := make([]Skill, 0, len(ids))
	for _, id := range ids {
		if s, ok := c.byID[id]; ok {
			out = append(out, s)
		}
	}
	return out
}

// ResolveSkills satisfies domain.SkillResolver: it maps attached IDs to the loop-facing
// domain.ResolvedSkill (ID, DisplayName, Body, Dir), in id order, skipping unknowns. The loop
// compares the returned set against what it asked for to report any miss (loop.go), keeping
// the "never silently ignored" property without this package knowing about events. Dir carries
// the skill's folder through so the loop can name it in the injected block — the address of the
// files bundled beside the SKILL.md.
func (c *Catalog) ResolveSkills(ids []string) []domain.ResolvedSkill {
	out := make([]domain.ResolvedSkill, 0, len(ids))
	for _, s := range c.Resolve(ids) {
		out = append(out, domain.ResolvedSkill{
			ID:          s.ID,
			DisplayName: s.DisplayName,
			Body:        s.Body,
			Dir:         s.Dir,
		})
	}
	return out
}

// Compile-time proof the catalog satisfies the loop's resolver seam (ADR 0010: skills depends
// on domain, never the reverse — domain defines the interface, this package implements it).
var _ domain.SkillResolver = (*Catalog)(nil)
