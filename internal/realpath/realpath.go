package realpath

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// evalSymlinks is the resolver Resolve tries first. It is a variable so a test can force the
// failure a Windows junction produces on any platform; nothing else assigns it.
var evalSymlinks = filepath.EvalSymlinks

// Resolve returns p with every link resolved. It starts from filepath.EvalSymlinks and turns to
// the per-OS fallback in two cases, on Windows the final path name the OS reports for an open
// handle (GetFinalPathNameByHandle, the \\?\ prefix stripped):
//
//   - filepath.EvalSymlinks fails on a path that exists, as it does for a path that passes under
//     an NTFS junction (Go >= 1.23, winsymlink=1; golang/go#82447, golang/go#63703);
//   - filepath.EvalSymlinks succeeds but its answer still passes through a junction, as it does
//     for a path that IS a junction: Go >= 1.23 reports a junction as a plain directory, so
//     EvalSymlinks returns that spelling unchanged.
//
// A junction is resolved like a symbolic link, never treated as opaque. Only a junction triggers
// the second case, so a path with none keeps EvalSymlinks' spelling (a subst or mapped drive
// letter included). When the fallback cannot answer in that case, the EvalSymlinks answer
// stands. On POSIX there is no fallback and Resolve is filepath.EvalSymlinks alone.
//
// The fallback's result is always absolute, where filepath.EvalSymlinks keeps a relative input
// relative; callers that compare or key on the result pass an absolute path.
//
// Errors: filepath.EvalSymlinks' own error, unchanged, when the path does not exist (so it stays
// fs.ErrNotExist-classed) or when the fallback cannot resolve it either.
func Resolve(p string) (string, error) {
	resolved, resolveErr := evalSymlinks(p)
	if realPathFallback == nil {
		return resolved, resolveErr
	}
	if resolveErr == nil {
		if crossesJunction == nil || !crossesJunction(resolved) {
			return resolved, nil
		}
		if final, ok := realPathFallback(resolved); ok {
			return final, nil
		}
		return resolved, nil
	}
	if _, err := os.Lstat(p); err != nil {
		return "", resolveErr
	}
	if final, ok := realPathFallback(p); ok {
		return final, nil
	}
	return "", resolveErr
}

// ResolveLenient returns p with every link resolved and never fails. When p does not exist it
// climbs to the nearest existing ancestor, resolves that, and re-joins the missing rest, so a
// path to a file about to be created still resolves to a real location, even when its root is
// reached through a link (macOS /tmp).
//
// On Windows the nearest existing ancestor is resolved through the final path name of an open
// handle whether or not a junction is crossed, so two separately resolved paths always share one
// spelling: a junction's target, and the underlying location of a subst or mapped drive. When
// that call answers for no ancestor at all (a volume it fails on for every path), the result is
// the Resolve-based climb below, the same as on POSIX.
//
// On POSIX, and in that Windows failure case, each ancestor goes through Resolve. When no
// ancestor resolves, the result is p made absolute, and p cleaned when even that fails.
func ResolveLenient(p string) string {
	p = filepath.Clean(p)
	if realPathFallback != nil {
		if final, ok := climb(p, realPathFallback); ok {
			return final
		}
	}
	if resolved, ok := climb(p, resolveOK); ok {
		return resolved
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// IsLink reports whether p is itself a link: a symbolic link or, on Windows, any reparse point
// (a junction included). It reads p's own directory entry and follows nothing, so a plain file
// beneath a link is not one.
//
// Errors: os.Lstat's own error, unchanged, when p cannot be read.
func IsLink(p string) (bool, error) {
	info, err := os.Lstat(p)
	if err != nil {
		return false, err
	}
	return info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0, nil
}

// StripLongPathPrefix removes the Windows \\?\ extended-length prefix, mapping the UNC
// form (`\\?\UNC\server\share`) back to its ordinary spelling so the two forms of one path
// compare equal. It reports whether a prefix was present. It is pure string work and compiled on
// every OS, so Windows path rules stay table-testable from any host.
func StripLongPathPrefix(p string) (string, bool) {
	const prefix = `\\?\`
	if !strings.HasPrefix(p, prefix) {
		return p, false
	}
	rest := p[len(prefix):]
	if unc := `UNC\`; len(rest) >= len(unc) && strings.EqualFold(rest[:len(unc)], unc) {
		return `\\` + rest[len(unc):], true
	}
	return rest, true
}

// climb resolves the nearest ancestor of the cleaned path p that resolve answers for, p itself
// first, and re-joins the components below it. It reports false when resolve answers for no
// ancestor up to the filesystem root (or, for a relative p, up to ".").
func climb(p string, resolve func(string) (string, bool)) (string, bool) {
	if resolved, ok := resolve(p); ok {
		return resolved, true
	}
	remaining := ""
	for current := p; ; {
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		remaining = filepath.Join(filepath.Base(current), remaining)
		if resolved, ok := resolve(parent); ok {
			return filepath.Join(resolved, remaining), true
		}
		current = parent
	}
}

// resolveOK is Resolve in the shape climb takes: the error folded into ok.
func resolveOK(p string) (string, bool) {
	resolved, err := Resolve(p)
	return resolved, err == nil
}
