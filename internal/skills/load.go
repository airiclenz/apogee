package skills

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/airiclenz/apogee/internal/security"
)

// skillFileName is the marker file that makes a folder a skill. The match is case-insensitive
// (SKILL.md / skill.md), mirroring the oracle.
const skillFileName = "SKILL.md"

// maxSkillFileBytes bounds a single SKILL.md read so a hostile repo cannot OOM discovery with
// a giant marker file — the `.apogee/skills` dir is always scanned. Skills are prose
// instructions; 1 MiB is far past any real one. Mirrors the read_file tool's one-handle
// discipline — open, then read through a limit, never materialising past the cap
// (internal/tools' readWorkspaceFileBounded).
const maxSkillFileBytes = 1 << 20 // 1 MiB

// maxSkills caps how many skills discovery loads across all source dirs, so a repo that plants
// thousands of skill folders cannot make the in-memory catalog unbounded. Well past any real
// library; the merged "/" menu only ever surfaces a handful at once.
const maxSkills = 1024

// maxSkillDirs caps how many directories ONE source dir's walk descends into. maxSkills bounds the
// catalog, not the walk: a tree of a million skill-less folders loads zero skills and still costs a
// million readdirs, so a hostile repo could stall discovery — which runs on every catalog reload —
// without ever growing the catalog. A real library is a flat list of skill folders with the odd
// bundled resource tree, so this is orders of magnitude past any honest layout.
const maxSkillDirs = 4096

// maxSkillDirDepth caps how deep below a source dir the walk descends, the other half of the same
// bound: a single chain of nested folders is narrow enough to slip under maxSkillDirs but can still
// be arbitrarily long. A skill folder sits ONE level down and its bundled files a level or two
// below that (references/, scripts/), so eight levels is far past any real skill.
const maxSkillDirDepth = 8

// Sources are the injected roots Load discovers skills under (ADR 0001 — no implicit ~/.apogee).
// Home is the apogee home (its skills/ subdir is the global library); Workspace is the project
// root (its .apogee/skills and, when UseProjectSkills, its skills/ folder). An empty Home or
// Workspace simply contributes no dirs.
type Sources struct {
	Home             string
	Workspace        string
	UseProjectSkills bool
}

// Load discovers skills from the layered source dirs and returns the assembled Catalog. The
// returned *Catalog is always non-nil and usable — a missing source dir is skipped and a
// malformed skill is skipped — so a caller may safely ignore the error and still get a working
// (possibly partial) catalog. The error, when non-nil, joins everything the scan recorded for a
// caller that wants one error value; it never signals "the catalog is unusable". Not every record
// is a failure: a skill that lost an id collision (ShadowedError) parsed fine and is simply not the
// live copy, which is why the /skills report partitions on the cause rather than on the channel.
//
// Dropping that error loses nothing: the same failures are recorded ON the catalog
// (Catalog.Skipped), so the caller that ignores the error can still tell the human WHICH skill
// did not load and why.
func Load(src Sources) (*Catalog, error) {
	cat := newCatalog()
	for _, a := range sourceAnchors(src) {
		loadDir(cat, a)
	}
	return cat, cat.skipError()
}

// skillAnchor is one source dir kept in two halves: the BASE it belongs to (the workspace root, or
// the apogee home — both operator-chosen) and the path of the source dir below it. The split is
// what lets loadDir pin its fence at the base and reach the source dir THROUGH it, so every
// component below the base — `.apogee`, `skills` — is resolved inside that fence and an untrusted
// repo cannot relocate the walk by shipping any of them as a symlink. trusted marks the one anchor
// exempt from that containment because the path naming it is operator-authored rather than
// repo-authored — the global library under the apogee home; openAnchor carries the rationale.
type skillAnchor struct {
	base    string // the operator-chosen root the walk may not leave
	rel     string // slash-separated path of the source dir below base
	trusted bool   // the anchor's own path is the operator's: follow it, fence at what it resolves to
}

// dir renders the anchor as the single host path a human sees — what skip records name, what
// Skill.Dir is stamped from, and what the /skills report lists as a source.
func (a skillAnchor) dir() string { return filepath.Join(a.base, filepath.FromSlash(a.rel)) }

// sourceAnchors lists the skill dirs in DECREASING priority (an earlier one wins an id collision
// with a later one): the user's global library FIRST, then the project's bare skills/ (gated by
// UseProjectSkills), then the project's .apogee/skills. Home going first is the ADR 0032 rule —
// the user's own library wins any cross-source id collision, so a cloned repo can contribute a
// NEW skill id but can never silently replace a skill the user invokes by muscle memory. The
// workspace dirs keep their relative order among themselves.
//
// Highest-priority-first is what makes the global cap (maxSkills) agree with that precedence
// instead of undoing it (audit 2026-08-25 F-06). The cap is first-come and the walk is shared
// across every source, so whichever source is walked LAST is the one the cap can evict: with the
// library walked last, a repo shipping maxSkills folders filled the catalog and the user's own
// library never loaded — priority decided by last-write could not save a skill that was never
// read. Walking highest-priority first inverts both halves at once: the cap can only ever cut
// into the lowest-priority source, and a collision keeps the FIRST copy (Catalog.set), which
// reaches the identical "home wins, bare skills/ beats .apogee/skills" outcome from the other end.
//
// An empty Home/Workspace drops its dirs rather than producing a bogus relative path. The home
// anchor is the trusted one: the apogee home is the operator's control plane, so the path naming
// the library may be a symlink the operator placed and discovery follows it (openAnchor).
//
// This is a deliberate, documented deviation from the apogee-code oracle's order, which this
// function used to mirror: a SKILL.md written for either tool still loads in both — only
// collision RESOLUTION differs (ADR 0032). Every displaced skill is recorded rather than dropped
// (Catalog.set), so the trade is visible in the /skills report instead of silent.
func sourceAnchors(src Sources) []skillAnchor {
	var anchors []skillAnchor
	if src.Home != "" {
		anchors = append(anchors, skillAnchor{base: src.Home, rel: "skills", trusted: true})
	}
	if src.Workspace != "" {
		if src.UseProjectSkills {
			anchors = append(anchors, skillAnchor{base: src.Workspace, rel: "skills"})
		}
		anchors = append(anchors, skillAnchor{base: src.Workspace, rel: ".apogee/skills"})
	}
	return anchors
}

// sourceDirs renders the same list as plain host paths, for the callers that only DISPLAY the
// sources (Provider.SourceDirs, the /skills report). Discovery itself walks the anchors, which
// carry the base each dir must stay inside.
func sourceDirs(src Sources) []string {
	anchors := sourceAnchors(src)
	dirs := make([]string, 0, len(anchors))
	for _, a := range anchors {
		dirs = append(dirs, a.dir())
	}
	return dirs
}

// readRoots renders the same anchors as the MOUNT view: the host hands these to the read tools as
// extra read-only roots (domain.Config.ExtraReadRoots), and every entry is the anchor's
// symlink-RESOLVED real path rather than the path as configured. It is openAnchor's two-way rule
// restated for the mount (audit 2026-08-25 F-13). An untrusted anchor — the workspace's
// .apogee/skills and its bare skills/ — is resolved THROUGH its base, so a repo that ships any
// component of it as a symlink leaving the workspace has that anchor DROPPED from the list: the
// walk already refuses to scan it, and mounting it anyway would have let grep, read_file, list_dir
// and find_files read the very tree discovery would not. The trusted home anchor is followed and
// the mount pinned at what it resolves to, because the operator's dotfiles symlink is exactly how
// a managed library is named and openAnchor re-pins the walk there for the same reason.
//
// The trust decision lives in this package and nowhere else: only here are the anchors paired with
// the base a "does this still belong to the workspace" judgement can be made against. internal/
// tools receives resolved paths and merely refuses to mount one that is not its own real path
// (path_read.go's matchRoot) — the two layers must agree, and neither can take the other's
// decision.
//
// A missing dir is still listed, exactly as sourceDirs lists it: this reports where skills come
// from, and the mount side skips an unusable root of its own accord. Beyond resolving symlinks the
// function does no I/O.
func readRoots(src Sources) []string {
	anchors := sourceAnchors(src)
	roots := make([]string, 0, len(anchors))
	for _, a := range anchors {
		if a.trusted {
			roots = append(roots, security.EvalRealPath(a.dir()))
			continue
		}
		resolved, err := security.ResolveInRoot(filepath.FromSlash(a.rel), a.base)
		if err != nil {
			// security.ErrPathEscape, the only error this returns: the anchor — or a component
			// of it — is a symlink leaving the base. Drop it rather than mount a relocated fence.
			continue
		}
		roots = append(roots, resolved)
	}
	return roots
}

// loadDir walks one source dir through os.Root and loads every SKILL.md it finds, recording a
// SkipError on the catalog per unreadable/malformed skill (a missing source dir records none — it
// is simply skipped). The fence is pinned by openAnchor. For a workspace source dir it covers the
// ANCHOR as well as the walk below it: neither a symlinked `.apogee`, `.apogee/skills` or `skills`
// nor a symlink deeper in the tree can move the walk out of the workspace root. For the operator's
// global library it is pinned at the dir the home's `skills` RESOLVES to, so the walk below it is
// contained against that target and a symlink leaving it is still refused. Dotted subdirs are
// skipped (no .git, no hidden folders), and the walk is bounded by maxSkillDirs and
// maxSkillDirDepth so an unloadably deep or wide tree terminates instead of touring the disk.
func loadDir(cat *Catalog, a skillAnchor) {
	dir := a.dir()
	root, err := openAnchor(a)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			// An absent source dir is the normal case and stays silent; anything else — an anchor
			// that resolves outside its base, an unreadable one — is a source the human expected
			// to be scanned and was not, and this package does not let soft mean silent (doc.go).
			cat.addSkip(SkipError{
				Path: dir,
				Err:  fmt.Errorf("skill source dir %s was not scanned: %w", dir, err),
			})
		}
		return
	}
	defer root.Close()
	fsys := root.FS()

	dirsSeen, deepBranchNoted := 0, false
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || p == "." {
			return nil // skip an unreadable entry (incl. an escaping symlink) / the root itself
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir // never descend into .git or other dotted dirs
			}
			dirsSeen++
			if dirsSeen > maxSkillDirs {
				// Cap reached: stop this dir's walk rather than let a planted tree own discovery's
				// running time. Noted once, like the skill cap below.
				cat.addSkip(SkipError{
					Path: absSkillPath(dir, p),
					Err: fmt.Errorf("directory cap (%d) reached; this and any later directories under %s were not scanned",
						maxSkillDirs, dir),
				})
				return fs.SkipAll
			}
			if walkDepth(p) >= maxSkillDirDepth {
				if !deepBranchNoted {
					deepBranchNoted = true
					cat.addSkip(SkipError{
						Path: absSkillPath(dir, p),
						Err: fmt.Errorf("depth cap (%d) reached; nothing below this directory under %s was scanned",
							maxSkillDirDepth, dir),
					})
				}
				return fs.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(d.Name(), skillFileName) {
			return nil
		}
		if cat.Len() >= maxSkills {
			// Cap reached: a hostile repo cannot grow the catalog without bound. Stop this dir's
			// walk and note the skip once rather than per remaining file.
			cat.addSkip(SkipError{
				Path: absSkillPath(dir, p),
				Err: fmt.Errorf("skill cap (%d) reached; this and any later skills under %s were not loaded",
					maxSkills, dir),
			})
			return fs.SkipAll
		}
		loadSkillFile(cat, fsys, dir, p)
		return nil
	})
}

// openAnchor pins the walk's os.Root at the source dir, and how much it trusts the path naming
// that dir turns on WHO wrote it. os.OpenRoot resolves its own argument like any other open — it
// follows symlinks in every component of the anchor, including the last — so opening
// `<workspace>/.apogee/skills` directly hands the fence to whoever authored the workspace: a repo
// that ships `.apogee`, `.apogee/skills` or `skills` as a symlink relocates the root and the walk
// below it reads a tree apogee never meant to scan. Those anchors are repo-authored bytes, so they
// are anchored at the base and reached through Root.OpenRoot, which resolves every component
// INSIDE the fence instead and refuses exactly that relocation, while still following a symlink
// whose target stays within the base. The derived Root owns its own descriptor, so the base handle
// is released immediately.
//
// A trusted anchor — only the global library below the apogee home — is opened directly. The
// apogee home is not a repo's territory but the operator's control plane, and the dangerous-action
// floor gates model writes to it, so a symlink found there was placed by the human: naming the
// library that way is exactly how a dotfiles-managed library is wired, and refusing it would take
// the library away from the operator to defend against the operator. The fence is not given up,
// only re-pinned — os.OpenRoot fixes the root at what the symlink RESOLVES to, so the walk stays
// contained against the real library, a symlink below it that leaves is refused like any other
// escape, and both walk caps apply unchanged.
func openAnchor(a skillAnchor) (*os.Root, error) {
	if a.trusted {
		return os.OpenRoot(a.dir())
	}
	base, err := os.OpenRoot(a.base)
	if err != nil {
		return nil, err
	}
	defer base.Close()
	return base.OpenRoot(a.rel)
}

// walkDepth counts how many path elements below the source dir a walk entry sits at: a skill
// folder in the root of a source dir is 1, its references/ subfolder 2. The path is the
// slash-separated fs.FS form the walk yields, never a host path.
func walkDepth(p string) int { return strings.Count(p, "/") + 1 }

// loadSkillFile reads and parses one SKILL.md at the dir-relative path p (read through the
// os.Root FS, so the fence still holds) and inserts the parsed Skill, stamping its absolute Dir.
// A read or parse failure is recorded as a SkipError on the catalog rather than returned, so the
// walk continues past one bad file AND the human can still be told that file was passed over.
func loadSkillFile(cat *Catalog, fsys fs.FS, dir, p string) {
	abs := absSkillPath(dir, p)
	data, err := readBounded(fsys, p, maxSkillFileBytes)
	if err != nil {
		cat.addSkip(SkipError{Path: abs, Err: err})
		return
	}
	skillDirRel := path.Dir(p)
	dirName := path.Base(skillDirRel)
	if skillDirRel == "." {
		// A SKILL.md sitting directly in the source root has no enclosing skill folder; name it
		// from the source dir itself so the degenerate layout still yields a usable id.
		dirName = filepath.Base(dir)
	}
	sk, err := parseSkill(string(data), dirName)
	if err != nil {
		cat.addSkip(SkipError{Path: abs, Err: err})
		return
	}
	sk.Dir = filepath.Join(dir, filepath.FromSlash(skillDirRel))
	cat.set(sk, abs)
}

// absSkillPath resolves a walk-relative SKILL.md path back to a host path under dir, so a
// reported skip names a file the human can open.
func absSkillPath(dir, p string) string {
	return filepath.Join(dir, filepath.FromSlash(p))
}

// readBounded reads at most max bytes of the file at p through fsys, REFUSING (rather than
// materializing) a file larger than the cap. It opens and LimitReads instead of fs.ReadFile so
// a hostile oversized marker file is never slurped whole into memory before being rejected —
// the untrusted-file discipline the read_file tool applies at its own ceiling. The reader is
// bounded to max+1 so a file exactly at the cap still reads fully while an over-cap one is
// caught without reading it all.
func readBounded(fsys fs.FS, p string, max int64) ([]byte, error) {
	f, err := fsys.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("file exceeds the %d-byte skill limit", max)
	}
	return data, nil
}
