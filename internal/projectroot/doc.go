// Package projectroot resolves the Project root (ADR 0096 §1, the glossary's "Project root"): the
// folder whose `.apogee/` holds the project's Project config and its `.apogee/skills` library.
//
// The rule is a filesystem walk and nothing else. From the link-resolved workspace upward (a
// Windows junction is resolved like a symlink, through platform.RealPath), the nearest folder
// holding a `.apogee` directory is the Project root — but only inside a git repository, and never
// past its top-level: the walk stops at the first folder holding a `.git` entry, a directory or a
// file alike, so a worktree or a submodule stops at its own top rather than at the superproject's.
// Outside a repository, or inside one with no `.apogee/` up to its top, the Project root is the
// workspace itself.
//
// The user's home is never a Project root: `~/.apogee` is the global config, not a project's, so the
// walk stops before the home folder whether or not that folder is a repository, and a workspace
// that IS the home folder resolves to the empty Project root — "no project layer". A caller reads
// the empty answer as "use the workspace" wherever a folder is still needed (the skills anchor).
//
// No git is ever spawned: a `.git` entry is detected by Lstat, so resolving the root costs a
// handful of stats and runs before any process the repository could configure (internal/gitexec
// owns that hazard). The package sits just above the standard library — it imports only
// internal/platform (its RealPath resolver), which imports only internal/domain and its own
// winlabel — so internal/config, which needs the root before the composition root's state roots
// exist, can import it without a cycle.
//
// Files:
//
//	projectroot.go  Resolve: the upward walk, its two stop conditions and the home guard
package projectroot
