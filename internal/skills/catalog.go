package skills

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/workflow"
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
// atomic pointer, which makes "19 loaded, 1 skipped" always one scan's answer. Report is the
// accessor that keeps that promise across the seam — a reader wanting BOTH halves takes it in one
// call rather than pairing a List with a Skipped a Reload may land between.
type Catalog struct {
	byID map[string]Skill
	// pathByID is the SKILL.md each live skill was loaded from, kept only so a later collision
	// can name the file it is displacing. It is deliberately not exposed: Skill.Dir already
	// carries the folder for consumers, and this map exists for the shadow record alone.
	pathByID map[string]string
	skipped  []SkipError
	// bytes is the SKILL.md bytes discovery has read into this catalog so far, across every
	// source dir in walk order. load.go charges each read to it and stops a walk once the next
	// file would push it past maxSkillCatalogBytes; it sits here rather than on one walk so the
	// cap bounds the catalog as a whole, not each source separately.
	bytes int64
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

// Report returns both halves of the /skills report — the sorted skills List returns and a copy of
// the failures Skipped returns — read off THIS catalog in one call. A *Catalog is immutable after
// Load, so the pairing is trivially consistent here; the method exists for the seam above it
// (Provider.Report, SkillCatalog.Report), where taking List and Skipped separately would let a
// Reload land between the two and report half of one scan beside half of another.
func (c *Catalog) Report() (list []Skill, skipped []SkipError) {
	return c.List(), c.Skipped()
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

// Recipe satisfies workflow.RecipeSource: the recipe the skill id carries — its own copy of the
// plan, the declared inputs, the folder's address and the folder itself — and false for an unknown
// id or a skill without a `recipe:` block.
func (c *Catalog) Recipe(id string) (workflow.Recipe, bool) {
	s, ok := c.byID[id]
	if !ok || s.Recipe == nil {
		return workflow.Recipe{}, false
	}
	return workflow.Recipe{
		ID:     s.ID,
		Plan:   clonePlan(*s.Recipe),
		Inputs: slices.Clone(s.Inputs),
		Dir:    s.Dir,
		Files:  skillFiles(s.Dir),
	}, true
}

// RecipeIDs satisfies workflow.RecipeSource: the ids of every skill carrying a recipe, sorted.
func (c *Catalog) RecipeIDs() []string {
	var ids []string
	for id, s := range c.byID {
		if s.Recipe != nil {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// clonePlan copies plan deeply enough that a caller rewriting a stage's fields — the agent binds
// inputs into them — never reaches the catalog's own, which Reload's readers share lock-free.
func clonePlan(plan workflow.Plan) workflow.Plan {
	stages := make([]workflow.Stage, len(plan.Stages))
	for index, stage := range plan.Stages {
		stage.Returns = maps.Clone(stage.Returns)
		stage.Context = slices.Clone(stage.Context)
		stage.Tools = slices.Clone(stage.Tools)
		stage.Options = slices.Clone(stage.Options)
		if stage.Over != nil {
			over := *stage.Over
			over.List = slices.Clone(over.List)
			stage.Over = &over
		}
		stages[index] = stage
	}
	return workflow.Plan{Name: plan.Name, Stages: stages}
}

// skillFiles opens a skill's folder by the address its Dir announces: the embedded tree below
// `shipped:` for one of apogee's own, the host folder for one found on disk. It is nil when the
// address opens nothing, which only a relative Dir or an unknown shipped folder is.
//
// A folder on disk is served through an os.Root, never os.DirFS: a repo can ship a recipe's
// `prompt:` file as a symlink to a host file, and os.DirFS follows it out of the folder, where the
// root refuses every path that resolves outside it (and every absolute symlink target). The root
// stays open for as long as the returned FS is reachable — the recipe run holding it — and is
// closed by os.Root's own finalizer after that, so no caller has a handle to release. A folder
// that will not open (removed since discovery) still yields a non-nil FS whose every Open returns
// that error: nil would send the prompt reader to its workspace fallback and read a same-named
// file from the wrong tree.
func skillFiles(dir string) fs.FS {
	if rel, shipped := strings.CutPrefix(dir, ShippedMountPrefix); shipped {
		files, err := fs.Sub(shippedFiles, path.Join(shippedDir, rel))
		if err != nil {
			return nil
		}
		return files
	}
	if !filepath.IsAbs(dir) {
		return nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return unopenedFS{err: err}
	}
	return root.FS()
}

// unopenedFS is the FS of a skill folder that did not open: every Open fails with that error.
type unopenedFS struct{ err error }

// Open satisfies fs.FS, refusing name with the folder's open error.
func (u unopenedFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: u.err}
}

// Compile-time proof the catalog satisfies the loop's resolver seam (ADR 0010: skills depends
// on domain, never the reverse — domain defines the interface, this package implements it).
var _ domain.SkillResolver = (*Catalog)(nil)

// And the recipe port the agent starts a recipe through (ADR 0087 D6), declared beside the Plan
// it carries so the loop never imports this package.
var _ workflow.RecipeSource = (*Catalog)(nil)
