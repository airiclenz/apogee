package projectroot

import (
	"os"
	"path/filepath"
)

// configDirName is the per-project folder that marks a Project root: it holds the Project config
// (`config.yaml`) and the project's skills (`skills/`).
const configDirName = ".apogee"

// gitEntryName is the entry that marks a git top-level. It is a directory in an ordinary clone and
// a file in a worktree or a submodule; either one ends the walk.
const gitEntryName = ".git"

// Resolve returns the Project root for workspace (package doc): the nearest folder holding a
// `.apogee/` directory from the workspace up to its git top-level, or the workspace itself when
// there is no repository or no such folder in it.
//
// home is the user's home folder (os.UserHomeDir), the one folder that is never a Project root: the
// walk stops before reaching it, and a workspace that is the home folder itself resolves to the
// empty string — no project layer. An empty home disables only that guard.
//
// When the answer is the workspace, it comes back in the workspace's own spelling, symlinks and
// all, so a caller that composes paths from both sees one folder under one name; a folder above the
// workspace comes back symlink-resolved, which is the only spelling the walk can vouch for.
//
// Resolve never fails. A workspace that cannot be resolved — a missing `--workspace` folder — is
// returned as given, and an entry that cannot be stat'ed counts as absent, so the worst a broken
// tree can do is make the answer the workspace.
func Resolve(workspace, home string) string {
	if workspace == "" {
		return ""
	}
	start, err := realPath(workspace)
	if err != nil {
		return workspace
	}
	realHome := ""
	if home != "" {
		realHome = realPathOrClean(home)
	}
	if start == realHome {
		return ""
	}

	root, found := walkToProjectRoot(start, realHome)
	if !found || root == start {
		return workspace
	}
	return root
}

// walkToProjectRoot climbs from start, an absolute symlink-resolved folder, and reports the nearest
// folder holding a `.apogee/` directory at or below the git top-level. found is false when the walk
// ends without meeting a `.git` entry — at the filesystem root, or at the home folder when that is
// not itself the git top — because outside a repository no `.apogee/` above the workspace counts;
// it is also false inside a repository that holds none.
//
// The home folder ends the walk unexamined for `.apogee` (its `~/.apogee` is the global config):
// a dotfiles repository at the home folder is still the git top for a project below it, so a
// `.apogee/` between the two counts, while nothing above the home folder is ever reached.
func walkToProjectRoot(start, realHome string) (root string, found bool) {
	nearest := ""
	for dir := start; ; {
		if dir == realHome {
			return nearest, nearest != "" && exists(filepath.Join(dir, gitEntryName))
		}
		if nearest == "" && isDir(filepath.Join(dir, configDirName)) {
			nearest = dir
		}
		if exists(filepath.Join(dir, gitEntryName)) {
			return nearest, nearest != ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// realPath renders path absolute and symlink-resolved; it fails when any component is missing.
func realPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// realPathOrClean is realPath for the home guard, falling back to the cleaned absolute spelling so
// a home that cannot be resolved still stops a walk that reaches it by that spelling.
func realPathOrClean(path string) string {
	if resolved, err := realPath(path); err == nil {
		return resolved
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// isDir reports whether path names a directory, following a final symlink: a `.apogee` that is a
// symlink to a folder still marks the root, and what is read through it is fenced by its reader.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// exists reports whether an entry of any kind is at path, without following it: a `.git` file, a
// `.git` directory and a dangling `.git` symlink all mark a git top-level.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
