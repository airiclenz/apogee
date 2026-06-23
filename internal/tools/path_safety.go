package tools

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrPathEscape is returned when a tool argument resolves to a path outside the
// sandbox root. Tools match it to surface a uniform "outside the workspace" message
// rather than leaking the resolved absolute path's structure.
var ErrPathEscape = errors.New("tools: path resolves outside the workspace root")

// resolveInRoot resolves input (relative to root, or absolute) to a real path and
// confirms it stays within root, following symlinks so an in-workspace symlink to an
// outside target is rejected (a faithful port of the TS oracle's path-safety). A
// not-yet-existing target — the file a write is about to create — is validated
// against its nearest existing ancestor.
func resolveInRoot(input, root string) (string, error) {
	var resolved string
	if filepath.IsAbs(input) {
		resolved = filepath.Clean(input)
	} else {
		resolved = filepath.Join(root, input)
	}

	realResolved := evalRealPath(resolved)
	realRoot := evalRealPath(filepath.Clean(root))

	if realResolved == realRoot {
		return realResolved, nil
	}
	if strings.HasPrefix(realResolved, realRoot+string(filepath.Separator)) {
		return realResolved, nil
	}
	return "", fmt.Errorf("%w: %q", ErrPathEscape, input)
}

// evalRealPath resolves p through symlinks. When p does not exist, it climbs to the
// nearest existing ancestor, resolves that, and re-joins the remainder — so a path
// to a file about to be created still resolves to a real, escape-checkable location
// even when the root itself is reached through a symlink (e.g. macOS /tmp).
func evalRealPath(p string) string {
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
