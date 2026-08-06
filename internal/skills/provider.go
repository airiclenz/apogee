package skills

import (
	"sync"
	"sync/atomic"

	"github.com/airiclenz/apogee/internal/domain"
)

// Provider is the live, reloadable view over the discovered skills. Load builds one immutable
// Catalog snapshot; a Provider holds the CURRENT snapshot behind an atomic pointer and can
// Reload it on demand — so a skill added or edited after launch is picked up without restarting
// apogee. The dirs it scans are live too (SetSources), because WHICH dirs count is itself a
// configuration the host can move mid-session.
//
// The point of the seam is that ONE *Provider feeds both skill consumers: the TUI's merged "/"
// menu (List/Get) and the agent loop's resolver (ResolveSkills, the domain.SkillResolver). A
// Reload the menu triggers is therefore the same fresh catalog the loop later resolves against,
// so a mid-session skill both SHOWS in the menu AND resolves when attached (rather than showing
// but failing at submit with "attached skill … is not known").
//
// Reload swaps a whole immutable *Catalog under an atomic pointer, never mutating one in place.
// Catalog's "read-only after Load" property (catalog.go) is preserved, so a concurrent reader —
// the loop goroutine calling ResolveSkills while the UI goroutine reloads — always observes a
// consistent snapshot with no lock and no torn state.
// The source dirs are guarded by their own mutex rather than riding the atomic pointer: they are
// written rarely (a `use-project-skills` edit) and read once per Reload, so the two live on
// different clocks — the catalog swap must stay lock-free for the loop goroutine that resolves
// against it, while a source change only has to be seen by the NEXT scan.
type Provider struct {
	mu  sync.Mutex
	src Sources
	cur atomic.Pointer[Catalog]
}

// NewProvider loads the initial catalog from src and returns a Provider ready to serve and
// reload. The initial load error is soft (a missing source dir is skipped, a malformed skill is
// skipped — Load's always-usable contract), so it is dropped here; the stored catalog is always
// non-nil and usable, possibly partial. Dropping it hides nothing: the same failures are on the
// catalog, reachable through Skipped.
func NewProvider(src Sources) *Provider {
	p := &Provider{src: src}
	cat, _ := Load(src)
	p.cur.Store(cat)
	return p
}

// Reload re-scans the source dirs and atomically swaps in the fresh catalog. The soft error is
// returned for a caller that wants to surface it, but the swap happens regardless: a partial
// catalog still replaces the old one, mirroring Load's "never signals unusable" contract.
//
// It scans whatever SetSources last installed, so a reload after a source change is a scan of the
// new layering — never a fresh catalog assembled from stale dirs.
func (p *Provider) Reload() error {
	cat, err := Load(p.sources())
	p.cur.Store(cat)
	return err
}

// SetSources re-points the Provider at another set of source dirs; the next Reload scans them. It
// exists because `use-project-skills:` is a setting a human can move mid-session (ADR 0037), and
// the flag is not something Load takes per call — it is part of WHICH dirs are sources at all.
//
// The catalog in force is deliberately left alone: this is a change of where the next scan looks,
// and the caller decides when that scan happens (Reload). Nothing is dropped in between, so a
// source change that is never reloaded costs the session nothing.
func (p *Provider) SetSources(src Sources) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.src = src
}

// sources reads the current source dirs under the lock, so a Reload racing a SetSources scans one
// coherent set rather than a torn one.
func (p *Provider) sources() Sources {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.src
}

// current returns the live catalog snapshot. It is always non-nil: NewProvider stores one and
// Reload only ever stores the non-nil result of Load.
func (p *Provider) current() *Catalog { return p.cur.Load() }

// List returns the current snapshot's skills, sorted for the merged "/" menu (see Catalog.List).
func (p *Provider) List() []Skill { return p.current().List() }

// Get looks up a skill by exact ID in the current snapshot (see Catalog.Get).
func (p *Provider) Get(id string) (Skill, bool) { return p.current().Get(id) }

// Skipped returns the SKILL.md files the last scan could not load (see Catalog.Skipped). It
// reads the SAME snapshot List does, so the /skills report's two halves always describe one
// scan — never a fresh listing paired with stale failures.
func (p *Provider) Skipped() []SkipError { return p.current().Skipped() }

// ResolveSkills satisfies domain.SkillResolver against the current snapshot, so the loop resolves
// attached IDs through whatever catalog the last Reload installed (see Catalog.ResolveSkills).
func (p *Provider) ResolveSkills(ids []string) []domain.ResolvedSkill {
	return p.current().ResolveSkills(ids)
}

// Compile-time proof the provider satisfies the loop's resolver seam, exactly as *Catalog does —
// so it is a drop-in for Config.Skills while adding the reload capability.
var _ domain.SkillResolver = (*Provider)(nil)
