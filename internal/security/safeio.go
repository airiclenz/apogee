package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ----------------------------------------------------------------------------
// TOCTOU-safe workspace file I/O (H1 — symlink-swap race closed)
// ----------------------------------------------------------------------------
//
// ResolveInRoot validates a path at CHECK time, but a plain os.WriteFile/os.ReadFile
// on the returned string re-walks the path at USE time, following symlinks. A confined
// (write-capable) subprocess can swap an intermediate workspace component to an
// outside-pointing symlink AFTER the check passes and BEFORE the write, redirecting the
// write outside the fence (the H1 finding). The not-yet-created tail of a write target
// widens the same window.
//
// These helpers close that race by performing the actual file operation through an
// os.Root anchored at the workspace root (Go 1.26 stdlib). os.Root pins the root
// directory's file descriptor and resolves every path component beneath it WITHOUT
// following a symlink out of the root: a symlink whose target escapes the root is
// REFUSED rather than followed, and there is no check/use gap because the path that is
// validated (relative to the pinned fd) is the path that is operated on. This is the
// "check-and-use-the-same-fd" fix the security review (H1) calls for, portable across
// all build targets (os.Root is stdlib, available on every GOOS).
//
// The workspace boundary stays TIGHTEN-ONLY: these helpers refuse strictly MORE than
// the old string-path I/O did (an escaping symlink that the old EvalSymlinks check could
// miss under a concurrent swap is now rejected), and they refuse the same traversal /
// out-of-root absolute paths via the rootRelative containment check below. They never
// widen the fence.

// SafeWriteFile writes data to input (relative to root, or absolute-inside-root),
// creating parent directories as needed, with the workspace fence enforced at WRITE time
// through an os.Root pinned at root. A path that escapes the root — by traversal, by an
// absolute target outside root, or by a symlinked component pointing outside (including
// one swapped in concurrently) — returns an error wrapping ErrPathEscape, and nothing is
// written. perm is the file mode for a newly-created file.
func SafeWriteFile(root, input string, data []byte, perm os.FileMode) error {
	rel, err := rootRelative(input, root)
	if err != nil {
		return err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPathEscape, err)
	}
	defer r.Close()

	if dir := filepath.Dir(rel); dir != "." {
		// Create parent directories within the fence. An ESCAPE error here (a parent
		// component symlinked outside the root) is fatal — refuse before writing. A
		// non-escape error (e.g. "file exists" when a parent component already exists,
		// including as a symlink) is NOT fatal here: the authoritative gate is the
		// WriteFile below, which os.Root refuses with "path escapes from parent" if the
		// final open would traverse out of the root. Deferring to it keeps WriteFile the
		// single source of truth for the fence and avoids a false failure on a pre-existing
		// parent.
		if err := r.MkdirAll(dir, 0o755); err != nil && isRootEscapeError(err) {
			return mapRootEscape(err)
		}
	}
	if err := r.WriteFile(rel, data, perm); err != nil {
		return mapRootEscape(err)
	}
	return nil
}

// SafeReadFile reads input (relative to root, or absolute-inside-root) with the workspace
// fence enforced at READ time through an os.Root pinned at root, so a symlinked component
// pointing outside the root is refused rather than followed. It returns the same
// (contents, error) contract as os.ReadFile; a path escape returns an error wrapping
// ErrPathEscape and the read is not performed.
func SafeReadFile(root, input string) ([]byte, error) {
	rel, err := rootRelative(input, root)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPathEscape, err)
	}
	defer r.Close()

	data, err := r.ReadFile(rel)
	if err != nil {
		return nil, mapRootEscape(err)
	}
	return data, nil
}

// SafeStat returns the fs metadata for input (relative to root, or absolute-inside-root)
// with the workspace fence enforced through an os.Root pinned at root, so a symlinked
// component pointing outside the root is refused rather than followed. It lets a caller BOUND
// a file by size before reading it — the stat-then-read discipline the read_file tool uses —
// so an oversized or hostile file is rejected without being materialized. A path escape
// returns an error wrapping ErrPathEscape and nothing is stat'd outside the root.
func SafeStat(root, input string) (os.FileInfo, error) {
	rel, err := rootRelative(input, root)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPathEscape, err)
	}
	defer r.Close()

	info, err := r.Stat(rel)
	if err != nil {
		return nil, mapRootEscape(err)
	}
	return info, nil
}

// rootRelative validates that input stays within root (the same containment property
// ResolveInRoot enforces) and returns the path RELATIVE to root, suitable for an os.Root
// operation. It rejects traversal and out-of-root absolute paths up front (wrapping
// ErrPathEscape) so the caller gets the uniform "outside the workspace" error before any
// fd is opened; os.Root then enforces the symlink-component half of the fence at use time.
func rootRelative(input, root string) (string, error) {
	var abs string
	if filepath.IsAbs(input) {
		abs = filepath.Clean(input)
	} else {
		abs = filepath.Join(root, input)
	}
	cleanRoot := filepath.Clean(root)

	rel, err := filepath.Rel(cleanRoot, abs)
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrPathEscape, input)
	}
	// A ".." prefix (or exactly "..") means abs climbs above root: out of the fence.
	if rel == ".." || hasParentPrefix(rel) {
		return "", fmt.Errorf("%w: %q", ErrPathEscape, input)
	}
	if rel == "" {
		rel = "."
	}
	return rel, nil
}

// hasParentPrefix reports whether rel begins with a parent-directory hop ("../"), the
// signal that the resolved path escapes the root it was made relative to.
func hasParentPrefix(rel string) bool {
	return len(rel) >= 3 && rel[0] == '.' && rel[1] == '.' && os.IsPathSeparator(rel[2])
}

// mapRootEscape normalises an os.Root I/O error so a symlink-escape / traversal denial
// surfaces as ErrPathEscape (the uniform "outside the workspace" sentinel callers match),
// while a genuine I/O error (missing file, permission) passes through unchanged. Whatever
// the classification, an os.Root error means the operation did NOT touch the filesystem
// outside the root — the fence holds regardless of how the error is reported.
func mapRootEscape(err error) error {
	if err == nil {
		return nil
	}
	if isRootEscapeError(err) {
		return fmt.Errorf("%w: %v", ErrPathEscape, err)
	}
	return err
}

// rootEscapeMessage is the stable text os.Root uses when a path component escapes the
// pinned root (the unexported os.errPathEscapes sentinel, "path escapes from parent").
// os exports no matchable sentinel for it, so the escape denial is recognised by this
// message — security does not depend on this match (any os.Root error means the op did
// not escape); it only selects the uniform ErrPathEscape model-facing message.
const rootEscapeMessage = "path escapes from parent"

// isRootEscapeError reports whether err is an os.Root containment denial (an escaping
// symlink component or a traversal out of the pinned root), as opposed to an ordinary
// I/O error.
func isRootEscapeError(err error) bool {
	return err != nil && strings.Contains(err.Error(), rootEscapeMessage)
}
