package platform

import (
	"os"
	"path/filepath"
)

// evalSymlinks is the resolver RealPath tries first. It is a variable so a test can force the
// failure a Windows junction produces on any platform; nothing else assigns it.
var evalSymlinks = filepath.EvalSymlinks

// RealPath returns p with every link resolved. It starts from filepath.EvalSymlinks and turns to
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
// stands. On POSIX there is no fallback and RealPath is filepath.EvalSymlinks alone.
//
// The fallback's result is always absolute, where filepath.EvalSymlinks keeps a relative input
// relative; callers that compare or key on the result pass an absolute path.
//
// Errors: filepath.EvalSymlinks' own error, unchanged, when the path does not exist (so it stays
// fs.ErrNotExist-classed) or when the fallback cannot resolve it either.
func RealPath(p string) (string, error) {
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
