package skills

import (
	"io/fs"
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

// Sources reports the source set the next Reload would scan — the value SetSources last installed,
// or the one NewProvider started from. It exists for the host that changes ONE field of a live
// Sources (a `use-project-skills` or `use-shipped-skills` flip is one key each, applied
// independently): read this, replace the one field, hand the whole value back to SetSources. A
// caller that rebuilt the literal from scratch instead would silently reset every field the key it
// is applying does not own.
func (p *Provider) Sources() Sources { return p.sources() }

// sources reads the current source dirs under the lock, so a Reload racing a SetSources scans one
// coherent set rather than a torn one.
func (p *Provider) sources() Sources {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.src
}

// SourceDirs lists the dirs the NEXT scan would look in — the same layered list Load walks, in
// the same order (load.go's sourceDirs). It reads the CURRENT sources rather than a set captured
// at construction, so a SetSources is reflected immediately and a caller holding this method value
// is holding a live view with no plumbing of its own.
//
// It is the DISPLAY view — the /skills report names each source as configured, symlink and all;
// the host that MOUNTS these dirs for the model's read tools takes ReadRoots instead. Neither
// lists the embedded shipped source: it is compiled in rather than installed, so it has no host
// path to name (load.go's sourceDirs).
func (p *Provider) SourceDirs() []string { return sourceDirs(p.sources()) }

// ReadRoots lists the same dirs as the paths a host may MOUNT — each one symlink-resolved, and a
// workspace anchor that resolves outside the workspace dropped altogether (load.go's readRoots,
// F-13). It exists for the host that mounts the skill library as a read-only root for the model's
// read tools (domain.Config.ExtraReadRoots): the bundled files of a skill live beside its SKILL.md,
// so the dirs discovery scans are exactly the dirs those files are under. A dir that does not exist
// is still listed — this reports where skills COME FROM, and the mount side skips an unusable root
// of its own accord, exactly as loadDir skips a missing source dir.
//
// Like SourceDirs it reads the CURRENT sources rather than a set captured at construction, so a
// SetSources — a `use-project-skills` flip — moves the mount with no re-wiring, and a caller
// holding this method value is holding a live view.
func (p *Provider) ReadRoots() []string { return readRoots(p.sources()) }

// VirtualReadRoots lists the read-only trees the host mounts under a NAME rather than a host path,
// keyed by the prefix their addresses are spelled with — today the embedded shipped source alone,
// under `shipped:` (load.go's virtualReadRoots). It is ReadRoots' counterpart for the source that
// has no host path, and the host hands it to the read tools through the same live seam
// (domain.Config.VirtualReadRoots), so a shipped skill's announced `files: shipped:<id>` line names
// a folder the model can actually read.
//
// Like SourceDirs and ReadRoots it reads the CURRENT sources, so a `use-shipped-skills` flip moves
// the mount with no re-wiring, and a caller holding this method value is holding a live view.
func (p *Provider) VirtualReadRoots() map[string]fs.FS { return virtualReadRoots(p.sources()) }

// current returns the live catalog snapshot. It is always non-nil: NewProvider stores one and
// Reload only ever stores the non-nil result of Load.
func (p *Provider) current() *Catalog { return p.cur.Load() }

// List returns the current snapshot's skills, sorted for the merged "/" menu (see Catalog.List).
func (p *Provider) List() []Skill { return p.current().List() }

// Get looks up a skill by exact ID in the current snapshot (see Catalog.Get).
func (p *Provider) Get(id string) (Skill, bool) { return p.current().Get(id) }

// Skipped returns the SKILL.md files the last scan could not load (see Catalog.Skipped). It reads
// the snapshot in force at the moment it is called — which is NOT necessarily the one a preceding
// List read, because a Reload can swap the pointer between two accessor calls. A reader that wants
// both halves of one scan takes Report instead.
func (p *Provider) Skipped() []SkipError { return p.current().Skipped() }

// Report returns the current snapshot's skills and the SKILL.md files that scan could not load,
// off ONE p.current() load (see Catalog.Report) — the /skills report's two halves, guaranteed to
// describe a single scan. List and Skipped each take their own load, so calling them in sequence
// lets a Reload land in between and pair a fresh listing with stale failures (or the reverse);
// this is the accessor that closes that window, and the one the report path uses.
func (p *Provider) Report() (list []Skill, skipped []SkipError) { return p.current().Report() }

// Suggest ranks the skills of the snapshot in force at the moment it is called against a draft
// (see Catalog.Suggest). Like every accessor here it takes its OWN p.current() load, so it is NOT
// paired with a preceding List: a Reload landing between the two lets the "/" menu list one
// snapshot while the band ranks the next (Report is the accessor that pairs two halves of one
// scan; no caller has needed a List+Suggest pair). The guarantee is per call — every row the band
// paints names a skill of the snapshot that ranked it, and the loop resolves an attached ID
// through ResolveSkills against whatever catalog the last Reload installed, exactly as it
// resolves one the "/" menu attached.
func (p *Provider) Suggest(draft string, exclude func(id string) bool, limit int) []Suggestion {
	return p.current().Suggest(draft, exclude, limit)
}

// Lookup answers a model's load_skill query against the snapshot in force at the moment it is
// called (see Catalog.Lookup). Like every accessor here it takes its OWN p.current() load, which is
// the property that matters for this one: a skill added or edited mid-session is reachable through
// the door as soon as the next Reload lands, with no re-wiring of the tool that asks.
func (p *Provider) Lookup(query string) LookupResult { return p.current().Lookup(query) }

// LookupSkill satisfies domain.SkillLookup against the current snapshot, so the load_skill tool
// searches whatever catalog the last Reload installed — the same snapshot ResolveSkills answers
// the user's attached "/id" from (see Catalog.LookupSkill).
func (p *Provider) LookupSkill(query string) domain.SkillLookupResult {
	return p.current().LookupSkill(query)
}

// ResolveSkills satisfies domain.SkillResolver against the current snapshot, so the loop resolves
// attached IDs through whatever catalog the last Reload installed (see Catalog.ResolveSkills).
func (p *Provider) ResolveSkills(ids []string) []domain.ResolvedSkill {
	return p.current().ResolveSkills(ids)
}

// Compile-time proof the provider satisfies the loop's resolver seam, exactly as *Catalog does —
// so it is a drop-in for Config.Skills while adding the reload capability.
var _ domain.SkillResolver = (*Provider)(nil)

// And the model-facing door onto the same catalog (ADR 0065), so one *Provider is all a host
// injects for both seams.
var _ domain.SkillLookup = (*Provider)(nil)
