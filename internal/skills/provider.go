package skills

import (
	"sync/atomic"

	"github.com/airiclenz/apogee/internal/domain"
)

// Provider is the live, reloadable view over the discovered skills. Load builds one immutable
// Catalog snapshot; a Provider holds the CURRENT snapshot behind an atomic pointer and can
// Reload it from the same Sources on demand — so a skill added or edited after launch is picked
// up without restarting apogee.
//
// The point of the seam is that ONE *Provider feeds both skill consumers: the TUI's /skill
// picker (List/Get) and the agent loop's resolver (ResolveSkills, the domain.SkillResolver). A
// Reload the picker triggers is therefore the same fresh catalog the loop later resolves against,
// so a mid-session skill both SHOWS in the picker AND resolves when attached (rather than showing
// but failing at submit with "attached skill … is not known").
//
// Reload swaps a whole immutable *Catalog under an atomic pointer, never mutating one in place.
// Catalog's "read-only after Load" property (catalog.go) is preserved, so a concurrent reader —
// the loop goroutine calling ResolveSkills while the UI goroutine reloads — always observes a
// consistent snapshot with no lock and no torn state.
type Provider struct {
	src Sources
	cur atomic.Pointer[Catalog]
}

// NewProvider loads the initial catalog from src and returns a Provider ready to serve and
// reload. The initial load error is soft (a missing source dir is skipped, a malformed skill is
// skipped — Load's always-usable contract), so it is dropped here; the stored catalog is always
// non-nil and usable, possibly partial.
func NewProvider(src Sources) *Provider {
	p := &Provider{src: src}
	cat, _ := Load(src)
	p.cur.Store(cat)
	return p
}

// Reload re-scans the source dirs and atomically swaps in the fresh catalog. The soft error is
// returned for a caller that wants to surface it, but the swap happens regardless: a partial
// catalog still replaces the old one, mirroring Load's "never signals unusable" contract.
func (p *Provider) Reload() error {
	cat, err := Load(p.src)
	p.cur.Store(cat)
	return err
}

// current returns the live catalog snapshot. It is always non-nil: NewProvider stores one and
// Reload only ever stores the non-nil result of Load.
func (p *Provider) current() *Catalog { return p.cur.Load() }

// List returns the current snapshot's skills, sorted for the /skill picker (see Catalog.List).
func (p *Provider) List() []Skill { return p.current().List() }

// Get looks up a skill by exact ID in the current snapshot (see Catalog.Get).
func (p *Provider) Get(id string) (Skill, bool) { return p.current().Get(id) }

// ResolveSkills satisfies domain.SkillResolver against the current snapshot, so the loop resolves
// attached IDs through whatever catalog the last Reload installed (see Catalog.ResolveSkills).
func (p *Provider) ResolveSkills(ids []string) []domain.ResolvedSkill {
	return p.current().ResolveSkills(ids)
}

// Compile-time proof the provider satisfies the loop's resolver seam, exactly as *Catalog does —
// so it is a drop-in for Config.Skills while adding the reload capability.
var _ domain.SkillResolver = (*Provider)(nil)
