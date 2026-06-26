package skills

import (
	"sort"

	"github.com/airiclenz/apogee/internal/domain"
)

// Catalog is the resolved set of discovered skills keyed by ID. It is built by Load and read
// by two consumers over different seams: the TUI's /skill picker (List/Get) and the agent loop
// (ResolveSkills, the domain.SkillResolver it satisfies). The same *Catalog is injected into
// both — it is read-only after Load, so sharing it across the UI and the loop goroutine is safe
// (no method mutates byID).
type Catalog struct {
	byID map[string]Skill
}

// newCatalog returns an empty catalog ready for set. Load always returns a non-nil *Catalog —
// even an empty one — so a nil pointer never reaches a domain.SkillResolver field (a typed-nil
// interface there would pass a `!= nil` guard yet panic on call).
func newCatalog() *Catalog {
	return &Catalog{byID: map[string]Skill{}}
}

// set inserts (or replaces) a skill by ID. A replacement is the layering rule: load.go walks
// the source dirs in priority order, so the last writer of an ID — the highest-priority source
// — wins (a workspace skill overrides a global one).
func (c *Catalog) set(s Skill) {
	c.byID[s.ID] = s
}

// List returns every skill sorted by DisplayName (then ID, so the order is total and stable
// across equal display names) — the order the /skill dropdown shows.
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
// domain.ResolvedSkill (ID, DisplayName, Body), in id order, skipping unknowns. The loop
// compares the returned set against what it asked for to report any miss (loop.go), keeping
// the "never silently ignored" property without this package knowing about events.
func (c *Catalog) ResolveSkills(ids []string) []domain.ResolvedSkill {
	out := make([]domain.ResolvedSkill, 0, len(ids))
	for _, s := range c.Resolve(ids) {
		out = append(out, domain.ResolvedSkill{ID: s.ID, DisplayName: s.DisplayName, Body: s.Body})
	}
	return out
}

// Compile-time proof the catalog satisfies the loop's resolver seam (ADR 0010: skills depends
// on domain, never the reverse — domain defines the interface, this package implements it).
var _ domain.SkillResolver = (*Catalog)(nil)
