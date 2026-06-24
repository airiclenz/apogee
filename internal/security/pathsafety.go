package security

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ----------------------------------------------------------------------------
// Path-safety guard (consolidated from the Phase-1 per-tool path-safety, P3.6)
// ----------------------------------------------------------------------------

// ErrPathEscape is returned when a path argument resolves to a location outside the
// workspace root. Callers match it (errors.Is) to surface a uniform "outside the
// workspace" message rather than leaking the resolved absolute path's structure.
var ErrPathEscape = errors.New("security: path resolves outside the workspace root")

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

// EvalRealPath resolves p through symlinks. When p does not exist, it climbs to the
// nearest existing ancestor, resolves that, and re-joins the remainder — so a path to a
// file about to be created still resolves to a real, escape-checkable location even when
// the root itself is reached through a symlink (e.g. macOS /tmp).
func EvalRealPath(p string) string {
	p = filepath.Clean(p)
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}

	remaining := ""
	current := p
	for {
		parent := filepath.Dir(current)
		if parent == current {
			break // reached the filesystem root without finding an existing ancestor
		}
		remaining = filepath.Join(filepath.Base(current), remaining)
		if real, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(real, remaining)
		}
		current = parent
	}

	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}
