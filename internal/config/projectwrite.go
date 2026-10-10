package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/airiclenz/apogee/internal/adoption"
)

// The Project config writer (ADR 0096 §4). apogee writes `<Project root>/.apogee/config.yaml` only
// on the user's answer, and it runs the same transaction every global write runs (configedit.go) —
// with three differences, each because the file lives in a repository the user may commit:
//
//   - An absent file starts as an EMPTY document, never the global starter template: a project
//     states only what it sets, and a template's commented-out global keys would read, to a
//     teammate, as settings the project chose.
//   - Nothing but the config lands in the repository. The transaction's lock lives under the
//     apogee home beside the root's adoption record ([adoption.ConfigLockPath]), no `.gitignore`
//     is written, and an edit that turns out to have nothing to write takes back the `.apogee/`
//     and the empty file it made for it.
//   - The file is written only where it is. A `.apogee/` or a `config.yaml` that is a symlink, or a
//     file that resolves outside the Project root, is refused rather than followed: a committed link
//     could otherwise steer apogee's write at any file the user can write.

const (
	// projectDirPerm and projectFilePerm are the modes a first write creates `.apogee/` and its
	// config with — an ordinary repository folder and file (the umask applies). The file holds no
	// secret: servers and API keys are global-only and never read from a project. A later write
	// preserves whatever mode the file has (writeConfigAtomically).
	projectDirPerm  os.FileMode = 0o755
	projectFilePerm os.FileMode = 0o644
)

// editProject runs one config-write transaction against the Project config of projectRoot, holding
// a lock kept under workspacesDir (the binary's `~/.apogee/workspaces`). The file and its folder are
// created on the first write; a splice that finds nothing to do, and every refusal, leaves the
// project exactly as it was.
//
// Errors: a refusal when projectRoot is empty or is the user's home, when `.apogee/` or the config
// is a symlink, not a folder or regular file, or resolves outside the root; a refusal naming the
// config when the lock is still held after its wait; the transaction's own errors (editHeld).
func editProject(projectRoot, workspacesDir string, splice editSplice, verify editVerify) error {
	path := projectFilePath(projectRoot)
	if path == "" {
		return errors.New("apogee: cannot write the project config: no Project root is known")
	}
	lockPath, err := adoption.ConfigLockPath(workspacesDir, projectRoot)
	if err != nil {
		return err
	}
	release, err := lockConfigAt(lockPath, path, configLockTimeout)
	if err != nil {
		return err
	}
	defer release()

	made, err := prepareProjectFile(projectRoot, path)
	if err != nil {
		return err
	}
	wrote, err := editHeld(path, readProjectFile, splice, verify)
	if !wrote {
		made.undo()
	}
	return err
}

// readProjectFile is the Project config's read step: the file prepareProjectFile has already
// checked and, when absent, created empty — so the edit starts from the bytes on disk, or from an
// empty document, and never from a seeded template.
func readProjectFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("apogee: read project config %q: %w", path, err)
	}
	return data, nil
}

// projectCreated is what prepareProjectFile made, so an edit that wrote nothing can take it back.
type projectCreated struct {
	dir  string // `.apogee/`, when this write created it
	file string // the empty config, when this write created it
}

// undo removes what the preparation made, file first, and leaves anything else alone: a folder
// that has gained another entry meanwhile is not empty and stays.
func (c projectCreated) undo() {
	if c.file != "" {
		_ = os.Remove(c.file)
	}
	if c.dir != "" {
		_ = os.Remove(c.dir)
	}
}

// prepareProjectFile makes the Project config at path ready for the transaction, under its lock:
// `.apogee/` and an empty config are created when absent, and both are checked to be what they
// claim — a real folder and a regular file, neither a symlink — inside the Project root, which must
// not be the user's home (whose `.apogee/` is the global config).
func prepareProjectFile(projectRoot, path string) (projectCreated, error) {
	root, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		return projectCreated{}, fmt.Errorf("apogee: cannot write the project config: resolve Project root %q: %w",
			projectRoot, err)
	}
	if isHome(root) {
		return projectCreated{}, fmt.Errorf("apogee: cannot write the project config: %q is the home folder, "+
			"whose .apogee/ is the global config", projectRoot)
	}
	var made projectCreated
	dir := filepath.Dir(path)
	created, err := ensureProjectEntry(dir, func() error { return os.Mkdir(dir, projectDirPerm) }, fs.ModeDir)
	if err != nil {
		return projectCreated{}, err
	}
	if created {
		made.dir = dir
	}
	created, err = ensureProjectEntry(path, func() error { return createEmptyFile(path) }, 0)
	if err != nil {
		made.undo()
		return projectCreated{}, err
	}
	if created {
		made.file = path
	}
	if err := insideRoot(root, path); err != nil {
		made.undo()
		return projectCreated{}, err
	}
	return made, nil
}

// ensureProjectEntry makes sure the entry at path exists as the type want names — a folder
// (fs.ModeDir) or a regular file (0) — creating it with create when it is absent, and reports
// whether it did. An entry that is a symlink, or of another type, is refused: the writer follows no
// link the repository carries.
func ensureProjectEntry(path string, create func() error, want fs.FileMode) (bool, error) {
	created := false
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		switch createErr := create(); {
		case createErr == nil:
			created = true
		case !errors.Is(createErr, fs.ErrExist): // another writer making it first is not a failure
			return false, fmt.Errorf("apogee: cannot write the project config: create %q: %w", path, createErr)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return false, fmt.Errorf("apogee: cannot write the project config: stat %q: %w", path, err)
	}
	switch mode := info.Mode(); {
	case mode&fs.ModeSymlink != 0:
		return false, fmt.Errorf("apogee: cannot write the project config: %q is a symlink; "+
			"apogee writes the project config only where it is", path)
	case mode.Type() != want:
		return false, fmt.Errorf("apogee: cannot write the project config: %q is not a %s", path, entryNoun(want))
	}
	return created, nil
}

// entryNoun names the type ensureProjectEntry wanted, for its refusal.
func entryNoun(want fs.FileMode) string {
	if want == fs.ModeDir {
		return "folder"
	}
	return "regular file"
}

// createEmptyFile creates path empty with projectFilePerm, failing when it already exists.
func createEmptyFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, projectFilePerm)
	if err != nil {
		return err
	}
	return f.Close()
}

// insideRoot refuses a config whose resolved location is not under the resolved Project root — the
// belt to the per-entry symlink checks' braces, should the folders above `.apogee/` move under the
// write.
func insideRoot(root, path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("apogee: cannot write the project config: resolve %q: %w", path, err)
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("apogee: cannot write the project config: %q resolves to %q, outside the Project root %q",
			path, resolved, root)
	}
	return nil
}

// isHome reports whether the resolved folder root is the user's home. A home that cannot be found
// guards nothing; the Project root resolver never answers it either (internal/projectroot).
func isHome(root string) bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	return filepath.Clean(home) == root
}
