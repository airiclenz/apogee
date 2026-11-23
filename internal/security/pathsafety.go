package security

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/airiclenz/apogee/internal/realpath"
)

// ----------------------------------------------------------------------------
// Path-safety guard (consolidated from the Phase-1 per-tool path-safety, P3.6)
// ----------------------------------------------------------------------------

// ErrPathEscape is returned when a path argument resolves to a location outside the
// workspace root. Callers match it (errors.Is) to surface a uniform "outside the
// workspace" message rather than leaking the resolved absolute path's structure.
var ErrPathEscape = errors.New("security: path resolves outside the workspace root")

// ErrRootInaccessible is returned when the ROOT ITSELF cannot be opened — it was deleted or
// renamed, its permissions changed, or the name is not a directory. It is deliberately NOT
// ErrPathEscape: nothing escaped anything, and the caller's path may be perfectly fine, so
// reporting it as an escape tells the operator (and the model) to fix an argument that is not
// the problem and hides the one thing that is. Callers that distinguish the two match this
// first and render the root's own words; callers that only print the error say the true thing
// either way.
var ErrRootInaccessible = errors.New("security: workspace root is not accessible")

// ResolveInRoot resolves input (relative to root, or absolute) to a real path and
// confirms it stays within root, following symlinks so an in-workspace symlink to an
// outside target is rejected (a faithful port of the TS oracle's path-safety). A
// not-yet-existing target — the file a write is about to create — is validated against
// its nearest existing ancestor.
//
// It is the single reusable path-safety guard the file tools call (D6): consolidating
// the per-tool copies here means one symlink-aware, traversal-rejecting boundary every
// guarded tool inherits, in every mode.
func ResolveInRoot(input, root string) (string, error) {
	var resolved string
	if filepath.IsAbs(input) {
		resolved = filepath.Clean(input)
	} else {
		resolved = filepath.Join(root, input)
	}

	realResolved := EvalRealPath(resolved)
	realRoot := EvalRealPath(filepath.Clean(root))

	if realResolved == realRoot {
		return realResolved, nil
	}
	if strings.HasPrefix(realResolved, realRoot+string(filepath.Separator)) {
		return realResolved, nil
	}
	return "", fmt.Errorf("%w: %q", ErrPathEscape, input)
}

// WorkspaceRelative renders an already-resolved absolute path in its workspace-relative
// form — the short name a caller both DISPLAYS and OPENS the file by. It measures against the
// SYMLINK-RESOLVED root because ResolveInRoot returns a real path: on a box where the root is
// reached through a symlink (macOS /tmp) a plain Rel against the configured root would answer
// with a "../.."-laden path. Anything that still will not relativise falls back to the
// absolute path, which is longer but never wrong — and which a fenced open (SafeOpen and its
// siblings) then accepts only if it is genuinely inside the root, the safe direction.
func WorkspaceRelative(path, root string) string {
	rel, err := filepath.Rel(EvalRealPath(filepath.Clean(root)), path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return rel
}

// EvalRealPath returns p with every link resolved, never failing: a symlink and, on Windows, a
// junction are both crossed, and a path that does not exist yet resolves through its nearest
// existing ancestor. It is realpath.ResolveLenient under the name this package's callers know;
// both sides of a containment comparison must come from it, so they share one spelling.
func EvalRealPath(p string) string {
	return realpath.ResolveLenient(p)
}
