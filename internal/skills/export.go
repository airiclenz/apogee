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

// ----------------------------------------------------------------------------
// Exporting a shipped skill — the one way a copy of one reaches disk
// ----------------------------------------------------------------------------
//
// The shipped skills are embedded and never installed (doc.go, ADR 0065 §2), which is what keeps
// every user's four current across an upgrade — and also what leaves nothing to open in an editor.
// Export is the answer to that: it writes ONE shipped skill's whole folder into the user's global
// library, where the ordinary layering makes the copy win the id from then on (ADR 0032). It is
// the skills counterpart of scheme.Export, down to the two rules that matter — the bytes go out
// verbatim, and nothing already on disk is ever overwritten.

// exportedDirPerm and exportedFilePerm are the modes an exported skill is written under: the same
// owner-only pair every apogee-home writer uses, because a skill body is instruction text the agent
// will follow and a world-writable copy of one is a way to steer this user's agent.
const (
	exportedDirPerm  fs.FileMode = 0o700
	exportedFilePerm fs.FileMode = 0o600
)

// ShippedIDs names every skill compiled into this binary, in the embedded tree's own (sorted)
// order. It is what an export refusal lists back, so a mistyped id teaches the vocabulary instead
// of only reporting a miss.
//
// A broken embed yields no ids rather than a panic, matching loadShipped and virtualReadRoots: a
// build that lost its shipped tree still runs, and an export against it is refused by the same
// "no such shipped skill" path a typo takes.
func ShippedIDs() []string {
	entries, err := fs.ReadDir(shippedFiles, shippedDir)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	return ids
}

// ExportShipped writes an editable copy of the shipped skill named by id — its SKILL.md and every
// file bundled beside it, bytes verbatim — into <libraryDir>/<id>/, and returns the directory it
// wrote. libraryDir is the caller's global skills folder (`<home>/skills`); it is created if it is
// not there, which is the creation-deferred convention (doc.go): the writer makes what it needs,
// and nothing was auto-created for the shipped skills that stayed embedded.
//
// Only a SHIPPED id is exportable. A disk skill is already a folder the user can open, so there is
// nothing to obtain — and confining the source to the embedded tree is also what keeps id from
// being able to name anything else: it is refused as a name before it is looked up, and past that
// only an exact shipped folder name gets through (scheme.Export's rule, for scheme.Export's
// reason).
//
// An existing <libraryDir>/<id> is never overwritten: the directory is created with os.Mkdir, so
// asking whether it is there and claiming it are ONE operation and no answer can go stale between
// them. That refusal is the whole protection for a skill the user has been editing — the copy they
// exported yesterday and rewrote is exactly what a silent overwrite would destroy.
func ExportShipped(id, libraryDir string) (string, error) {
	if libraryDir == "" {
		// A relative write out of the process's working directory is never what a caller means by
		// "the user's library"; it means the library root was not resolved.
		return "", errors.New("apogee: no skills directory to export into")
	}
	if !validShippedID(id) {
		return "", fmt.Errorf("apogee: %q is not a skill id — a skill is named, not a path; shipped skills are: %s",
			id, strings.Join(ShippedIDs(), ", "))
	}
	src, err := fs.Sub(shippedFiles, path.Join(shippedDir, id))
	if err != nil || !shippedDirExists(id) {
		return "", fmt.Errorf("apogee: no shipped skill %q — shipped skills are: %s", id, strings.Join(ShippedIDs(), ", "))
	}
	if err := os.MkdirAll(libraryDir, exportedDirPerm); err != nil {
		return "", fmt.Errorf("apogee: create skills directory %q: %w", libraryDir, err)
	}
	dest := filepath.Join(libraryDir, id)
	if err := os.Mkdir(dest, exportedDirPerm); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("apogee: skill %q already exists — edit it or delete it first", dest)
		}
		return "", fmt.Errorf("apogee: create skill directory %q: %w", dest, err)
	}
	if err := copyTree(src, dest); err != nil {
		// A half-copied skill would load with pieces missing AND block the retry that fixes it, so
		// the failed attempt takes its own folder with it (scheme.Export's cleanup).
		_ = os.RemoveAll(dest)
		return "", fmt.Errorf("apogee: write skill %q: %w", dest, err)
	}
	return dest, nil
}

// validShippedID reports whether id may be looked up in the embedded tree at all. It is the
// name-not-a-path rule stated once: an id is a single folder name, so a separator, a `..`, a
// volume, a leading dot or anything badIDRune refuses is turned away HERE rather than as a side
// effect of the lookup below happening to miss — the refusal must not depend on what the tree
// happens to contain.
func validShippedID(id string) bool {
	if id == "" || id == "." || id == ".." || strings.HasPrefix(id, ".") {
		return false
	}
	if _, bad := badIDRune(id); bad {
		return false
	}
	return id == path.Base(id) && id == filepath.Base(id) &&
		!strings.ContainsAny(id, `/\:`)
}

// shippedDirExists reports whether the embedded tree carries a folder named id. fs.Sub does not
// check that its subtree is there, so this is the existence half of the lookup: without it an
// unknown id would export an empty folder rather than being refused.
func shippedDirExists(id string) bool {
	info, err := fs.Stat(shippedFiles, path.Join(shippedDir, id))
	return err == nil && info.IsDir()
}

// copyTree writes every file of src under dest, creating directories as it walks. Bytes go out
// verbatim — a bundled resource may be anything a skill author put beside their SKILL.md, and the
// point of the copy is that it is the same file.
func copyTree(src fs.FS, dest string) error {
	return fs.WalkDir(src, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if d.IsDir() {
			if rel == "." {
				return nil // dest itself, already created as the export's claim on the id
			}
			return os.Mkdir(target, exportedDirPerm)
		}
		if !d.Type().IsRegular() {
			// Nothing but regular files is embeddable, so this is unreachable in practice; skipping
			// beats writing whatever a future non-regular entry would turn into.
			return nil
		}
		data, err := fs.ReadFile(src, rel)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, exportedFilePerm)
	})
}
