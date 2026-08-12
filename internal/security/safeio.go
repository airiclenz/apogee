package security

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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
// REFUSED rather than followed, and WITHIN ONE CALL there is no check/use gap because the
// path that is validated (relative to the pinned fd) is the path that is operated on. This
// is the "check-and-use-the-same-fd" fix the security review (H1) calls for, portable
// across all build targets (os.Root is stdlib, available on every GOOS).
//
// SCOPE — what is closed is PATH RESOLUTION, per call, and — for a bounded read taken
// through SafeOpen — the size bound's check/use gap too: the caller fstats the very
// descriptor it then reads through an io.LimitReader, so the check and the use cannot
// disagree about which file they describe, and no more than the caller's cap+1 bytes are
// ever materialised even if the file grows mid-call. SafeReadFile remains UNBOUNDED by
// contract (its callers read files they are about to rewrite whole); a caller needing a
// bound must use SafeOpen. A bound decided from a SEPARATE stat call is no defence
// against an adversary who can rename inside the workspace: the stat-first pattern this
// package used to offer had exactly that window — an in-root name flipped between the
// two calls was stat'd as one file and read as another, measured (2026-07-25, Linux
// probe) and probe-dependent — which is why the stat primitive is gone and the one
// primitive that serves a bounded read returns the pinned handle itself.
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
// written.
//
// The write is ATOMIC at the target name: data goes to a staging file in the target's own
// parent directory (same directory, hence same filesystem — what rename atomicity
// requires) and is then renamed over the target through the pinned root. A crash or a
// failing write mid-call therefore never leaves a truncated target: readers see either the
// old file or the new one, never a half-written one. The staging file is removed on any
// failure after it is created. There is no fsync — the guarantee is "no torn file visible
// at the target name", not power-loss durability.
//
// Mode: an existing target keeps its own mode across the rename; a newly-created target
// takes perm (subject to the process umask, as an ordinary create is).
//
// Because the last step is a rename, the contract at the target NAME is REPLACE-THE-NAME:
// when the name is a symlink pointing inside the root, the rename replaces the symlink
// itself with a regular file rather than writing through to its target. A name (or
// component) symlinked OUTSIDE the root is still refused with ErrPathEscape, and nothing —
// not even a staging file — is created outside the fence.
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

	dir := filepath.Dir(rel)
	if dir != "." {
		// Create parent directories within the fence. An ESCAPE error here (a parent
		// component symlinked outside the root) is fatal — refuse before writing. A
		// non-escape error (e.g. "file exists" when a parent component already exists,
		// including as a symlink) is NOT fatal here: the authoritative gates are the
		// targetMode stat and the staging open below, which os.Root refuses with "path
		// escapes from parent" if resolution would traverse out of the root. Deferring to
		// them keeps the fence decided at use time and avoids a false failure on a
		// pre-existing parent.
		if err := r.MkdirAll(dir, 0o755); err != nil && isRootEscapeError(err) {
			return mapRootEscape(err)
		}
	}

	mode, existed, err := targetMode(r, rel, perm)
	if err != nil {
		return err
	}
	staged, f, err := createStagingFile(r, dir, mode)
	if err != nil {
		return mapRootEscape(err)
	}
	if err := stageAndClose(f, data, mode, existed); err != nil {
		_ = r.Remove(staged)
		return err
	}
	if err := r.Rename(staged, rel); err != nil {
		_ = r.Remove(staged)
		return mapRootEscape(err)
	}
	return nil
}

// stagingPrefix is the basename prefix of SafeWriteFile's staging file. The leading dot
// keeps it out of ordinary listings, and the fixed prefix makes a leftover from a killed
// process recognisable as ours.
const stagingPrefix = ".apogee-tmp-"

// stagingNameAttempts bounds the retries on a staging-name collision. Names carry 64 bits
// of randomness, so a collision means a stale leftover with that exact name; a handful of
// attempts is generous.
const stagingNameAttempts = 5

// targetMode reports the mode SafeWriteFile's staged write must land with, and whether the
// target already exists — an existing target keeps its own mode, a new one takes perm.
// Statting through the root is also the fence gate the direct WriteFile used to be: a
// component symlinked outside the root, or a target NAME symlinked outside it, is refused
// here, before anything is staged.
func targetMode(r *os.Root, rel string, perm os.FileMode) (mode os.FileMode, existed bool, err error) {
	info, err := r.Stat(rel)
	switch {
	case err == nil:
		return info.Mode().Perm(), true, nil
	case isRootEscapeError(err):
		return 0, false, mapRootEscape(err)
	case errors.Is(err, os.ErrNotExist):
		return perm, false, nil
	default:
		return 0, false, err
	}
}

// createStagingFile creates SafeWriteFile's staging file inside dir — the target's own
// parent, so the rename that follows stays within one filesystem — and returns its
// root-relative name alongside the open handle. The exclusive create means an existing
// name is never clobbered.
func createStagingFile(r *os.Root, dir string, perm os.FileMode) (string, *os.File, error) {
	var lastErr error
	for range stagingNameAttempts {
		name, err := stagingName()
		if err != nil {
			return "", nil, err
		}
		rel := filepath.Join(dir, name)
		f, err := r.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if err == nil {
			return rel, f, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", nil, err
		}
		lastErr = err
	}
	return "", nil, lastErr
}

// stagingName builds a collision-resistant staging basename.
func stagingName() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("staging name: %w", err)
	}
	return stagingPrefix + hex.EncodeToString(buf[:]), nil
}

// stageAndClose writes data to the staging handle, applies mode when the target already
// existed (an explicit fchmod, because the creating open is subject to the process umask
// and would otherwise lose bits the target had), and closes the handle — always, including
// on error, so a failed write never leaks a descriptor.
func stageAndClose(f *os.File, data []byte, mode os.FileMode, applyMode bool) error {
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if applyMode {
		if err := f.Chmod(mode); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

// SafeReadFile reads input (relative to root, or absolute-inside-root) with the workspace
// fence enforced at READ time through an os.Root pinned at root, so a symlinked component
// pointing outside the root is refused rather than followed. It returns the same
// (contents, error) contract as os.ReadFile; a path escape returns an error wrapping
// ErrPathEscape and the read is not performed.
//
// It applies no size bound of its own: the contents are whatever the name resolves to inside
// the root at read time, however large. A caller that needs a bound must use SafeOpen and
// decide it from an fstat of the returned handle — see the SCOPE note in the package header.
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

// SafeOpen opens input (relative to root, or absolute-inside-root) for reading, with the
// workspace fence enforced at OPEN time through an os.Root pinned at root, so a symlinked
// component pointing outside the root is refused rather than followed. A path escape
// returns an error wrapping ErrPathEscape and nothing is opened.
//
// The returned handle PINS THE FILE'S IDENTITY: what is statted and read through it is the
// file that was opened, regardless of any rename after — which is what makes a size bound
// decided from an fstat of this descriptor race-free (see the SCOPE note in the package
// header). The caller owns Close and any size policy. The pinning root is closed before
// return; a file opened through an os.Root stays valid after the root closes.
func SafeOpen(root, input string) (*os.File, error) {
	rel, err := rootRelative(input, root)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPathEscape, err)
	}
	defer r.Close()

	f, err := r.Open(rel)
	if err != nil {
		return nil, mapRootEscape(err)
	}
	return f, nil
}

// SafeCopyFileFrom copies the regular file at srcInput — relative to srcRoot, or
// absolute-inside-srcRoot — to dstInput under dstRoot, with EACH END fenced at COPY time by its
// OWN os.Root: the source is read through a root pinned at srcRoot, the destination written
// through a root pinned at dstRoot. A path that escapes ITS OWN root (by traversal, by an
// absolute target outside it, or by a symlinked component pointing outside, including one
// swapped in concurrently) returns an error wrapping ErrPathEscape, and nothing is written. The
// two fences are independent: a source escaping srcRoot is refused even when it happens to lie
// inside dstRoot, and vice versa.
//
// Two roots exist for the one case where a read boundary and a write boundary legitimately
// differ — a copy's source is a READ, so it may come from a configured read-only root (the
// skills library) while the destination stays workspace-fenced. The write half is not widened
// by any of this: dstRoot bounds the only thing this call creates, exactly as the one-root form
// does.
//
// The destination is written with SafeWriteFile's guarantees: parent directories are created
// inside the fence, the bytes are staged in the destination's own parent and renamed over it,
// and the staging file is removed on any failure after it is created. So the destination name
// is never seen half-copied, and — the rename being the last step — an in-root symlink AT that
// name is replaced by a regular file rather than written through.
//
// Mode: the destination lands with the SOURCE's mode, whether or not it already existed. That
// is what distinguishes a copy from a write: the point of copying a 0755 script is to end up
// with a 0755 script. A source that is not a regular file (a directory, a device) is refused —
// the caller's own "not a file" wording is the model-facing one, this is the backstop.
//
// The content is streamed, so a copy costs no more memory than its buffer however large the
// file is; there is no fsync, for the same reason SafeWriteFile has none.
func SafeCopyFileFrom(srcRoot, srcInput, dstRoot, dstInput string) error {
	srcRel, err := rootRelative(srcInput, srcRoot)
	if err != nil {
		return err
	}
	dstRel, err := rootRelative(dstInput, dstRoot)
	if err != nil {
		return err
	}
	sr, err := os.OpenRoot(srcRoot)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPathEscape, err)
	}
	defer sr.Close()
	dr, err := os.OpenRoot(dstRoot)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPathEscape, err)
	}
	defer dr.Close()

	src, mode, err := openCopySource(sr, srcRel, srcInput)
	if err != nil {
		return err
	}
	defer src.Close()

	return stageCopy(dr, dstRel, src, mode)
}

// SafeCopyFile copies the regular file at srcInput to dstInput — both relative to root, or
// absolute-inside-root — with the workspace fence enforced at COPY time. It is
// SafeCopyFileFrom's EQUAL-ROOTS special case: the one root pins both ends, which is what every
// caller whose source and destination are both workspace paths wants. Every guarantee documented
// on SafeCopyFileFrom — staged-and-renamed destination, the source's mode, a non-regular source
// refused, ErrPathEscape at either end with nothing written — holds here unchanged.
func SafeCopyFile(root, srcInput, dstInput string) error {
	return SafeCopyFileFrom(root, srcInput, root, dstInput)
}

// openCopySource opens the copy's source through the root pinned at the SOURCE's end and reports
// the mode the destination must land with. The mode comes from the very descriptor the bytes are
// read through, so the copy cannot take its permissions from one file and its content from
// another. A non-regular source is refused here, before anything is staged at the destination;
// input is the source as the caller wrote it, for that refusal's message.
func openCopySource(sr *os.Root, rel, input string) (*os.File, os.FileMode, error) {
	f, err := sr.Open(rel)
	if err != nil {
		return nil, 0, mapRootEscape(err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, 0, fmt.Errorf("not a regular file: %s", input)
	}
	return f, info.Mode().Perm(), nil
}

// stageCopy lands src's bytes at dstRel through the root pinned at the DESTINATION's end, with
// SafeWriteFile's staging discipline: parents created inside the fence, the bytes staged in the
// destination's own parent, the rename last, and the staging file removed on any failure after
// it is created.
func stageCopy(dr *os.Root, dstRel string, src io.Reader, mode os.FileMode) error {
	dir := filepath.Dir(dstRel)
	if dir != "." {
		// Same reasoning as SafeWriteFile's parent creation: an ESCAPE here is fatal, any other
		// error defers to the staging open, which os.Root refuses if resolution would leave the
		// root.
		if err := dr.MkdirAll(dir, 0o755); err != nil && isRootEscapeError(err) {
			return mapRootEscape(err)
		}
	}
	staged, dst, err := createStagingFile(dr, dir, mode)
	if err != nil {
		return mapRootEscape(err)
	}
	if err := copyAndClose(dst, src, mode); err != nil {
		_ = dr.Remove(staged)
		return err
	}
	if err := dr.Rename(staged, dstRel); err != nil {
		_ = dr.Remove(staged)
		return mapRootEscape(err)
	}
	return nil
}

// copyAndClose streams src into the staging handle, applies mode explicitly (the creating open
// is subject to the process umask, which would otherwise drop bits the source had), and closes
// the handle — always, including on error, so a failed copy never leaks a descriptor.
func copyAndClose(dst *os.File, src io.Reader, mode os.FileMode) error {
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	if err := dst.Chmod(mode); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

// SafeRename renames oldInput to newInput — both relative to root, or absolute-inside-root —
// through a single os.Root pinned at root, so BOTH ends of the rename are fenced: a path that
// escapes the root at either end returns an error wrapping ErrPathEscape and nothing moves.
// Parent directories of the destination are created inside the fence, as SafeWriteFile creates
// them for its target.
//
// The rename is the filesystem's own: atomic, and it replaces an existing destination NAME
// (including a symlink at that name) without following it. Deciding whether replacing that name
// is allowed is the caller's policy, not this primitive's.
func SafeRename(root, oldInput, newInput string) error {
	oldRel, err := rootRelative(oldInput, root)
	if err != nil {
		return err
	}
	newRel, err := rootRelative(newInput, root)
	if err != nil {
		return err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPathEscape, err)
	}
	defer r.Close()

	if dir := filepath.Dir(newRel); dir != "." {
		if err := r.MkdirAll(dir, 0o755); err != nil && isRootEscapeError(err) {
			return mapRootEscape(err)
		}
	}
	if err := r.Rename(oldRel, newRel); err != nil {
		return mapRootEscape(err)
	}
	return nil
}

// SafeRemove removes the name input (relative to root, or absolute-inside-root) through an
// os.Root pinned at root, so a path escaping the root is refused rather than followed. It
// removes THE NAME: a symlink is unlinked, never the file it points at.
//
// It is os.Remove's contract otherwise — a non-empty directory is refused by the filesystem —
// and a missing name returns an error satisfying errors.Is(err, os.ErrNotExist).
func SafeRemove(root, input string) error {
	rel, err := rootRelative(input, root)
	if err != nil {
		return err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPathEscape, err)
	}
	defer r.Close()

	if err := r.Remove(rel); err != nil {
		return mapRootEscape(err)
	}
	return nil
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
