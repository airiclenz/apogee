package platform

import (
	"os"
	"path/filepath"
)

// evalSymlinks is the resolver RealPath tries first. It is a variable so a test can force the
// failure a Windows junction produces on any platform; nothing else assigns it.
var evalSymlinks = filepath.EvalSymlinks

// RealPath returns p with every link resolved: the result of filepath.EvalSymlinks whenever that
// succeeds. When it fails on a path that exists, the per-OS fallback resolves it instead — on
// Windows the final path name the OS reports for an open handle (GetFinalPathNameByHandle, the
// \\?\ prefix stripped), because filepath.EvalSymlinks reports an error for a path that passes
// under an NTFS junction (Go >= 1.23, winsymlink=1; golang/go#82447, golang/go#63703) although
// the path is there. A junction is resolved like a symbolic link, never treated as opaque. On
// POSIX there is no fallback and RealPath is filepath.EvalSymlinks alone.
//
// The fallback's result is always absolute, where filepath.EvalSymlinks keeps a relative input
// relative; callers that compare or key on the result pass an absolute path.
//
// Errors: filepath.EvalSymlinks' own error, unchanged, when the path does not exist (so it stays
// fs.ErrNotExist-classed) or when the fallback cannot resolve it either.
func RealPath(p string) (string, error) {
	resolved, resolveErr := evalSymlinks(p)
	if resolveErr == nil || realPathFallback == nil {
		return resolved, resolveErr
	}
	if _, err := os.Lstat(p); err != nil {
		return "", resolveErr
	}
	if final, ok := realPathFallback(p); ok {
		return final, nil
	}
	return "", resolveErr
}
