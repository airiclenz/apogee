package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// skillFileName is the marker file that makes a folder a skill. The match is case-insensitive
// (SKILL.md / skill.md), mirroring the oracle.
const skillFileName = "SKILL.md"

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
// (possibly partial) catalog. The error, when non-nil, joins the per-skill soft failures for a
// caller that wants to surface them; it never signals "the catalog is unusable".
func Load(src Sources) (*Catalog, error) {
	cat := newCatalog()
	var softErrs []error
	for _, dir := range sourceDirs(src) {
		softErrs = append(softErrs, loadDir(cat, dir)...)
	}
	return cat, errors.Join(softErrs...)
}

// sourceDirs lists the skill dirs in increasing priority (later overrides earlier on an id
// collision), mirroring the oracle's order: the global library, the project's .apogee/skills,
// then the project's bare skills/ (gated by UseProjectSkills). An empty Home/Workspace drops
// its dirs rather than producing a bogus relative path.
func sourceDirs(src Sources) []string {
	var dirs []string
	if src.Home != "" {
		dirs = append(dirs, filepath.Join(src.Home, "skills"))
	}
	if src.Workspace != "" {
		dirs = append(dirs, filepath.Join(src.Workspace, ".apogee", "skills"))
		if src.UseProjectSkills {
			dirs = append(dirs, filepath.Join(src.Workspace, "skills"))
		}
	}
	return dirs
}

// loadDir walks one source dir through os.Root and loads every SKILL.md it finds, returning a
// soft error per unreadable/malformed skill (a missing or unopenable dir yields none — it is
// simply skipped). The os.Root fence is the same idiom as the TUI's workspace file walk: a
// symlink that escapes the dir cannot be followed, so a workspace skills/ symlinked at host
// files reads nothing out of bounds. Dotted subdirs are skipped (no .git, no hidden folders).
func loadDir(cat *Catalog, dir string) []error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil // a missing/unreadable source dir is fine — there just are no skills here
	}
	defer root.Close()
	fsys := root.FS()

	var errs []error
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || p == "." {
			return nil // skip an unreadable entry (incl. an escaping symlink) / the root itself
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir // never descend into .git or other dotted dirs
			}
			return nil
		}
		if !strings.EqualFold(d.Name(), skillFileName) {
			return nil
		}
		if err := loadSkillFile(cat, fsys, dir, p); err != nil {
			errs = append(errs, err)
		}
		return nil
	})
	return errs
}

// loadSkillFile reads and parses one SKILL.md at the dir-relative path p (read through the
// os.Root FS, so the fence still holds) and inserts the parsed Skill, stamping its absolute Dir.
// A read or parse failure is returned as a soft error so the walk continues past one bad file.
func loadSkillFile(cat *Catalog, fsys fs.FS, dir, p string) error {
	abs := filepath.Join(dir, filepath.FromSlash(p))
	data, err := fs.ReadFile(fsys, p)
	if err != nil {
		return fmt.Errorf("skills: read %s: %w", abs, err)
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
		return fmt.Errorf("skills: skip %s: %w", abs, err)
	}
	sk.Dir = filepath.Join(dir, filepath.FromSlash(skillDirRel))
	cat.set(sk)
	return nil
}
