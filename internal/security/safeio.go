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
//
// A ROOT THAT WILL NOT OPEN is a different answer from a path that escaped, and every
// primitive here gives it: pinning the root can fail on its own (the root deleted, its
// permissions changed, a name that is not a directory), and that failure returns an error
// wrapping ErrRootInaccessible (pathsafety.go), never ErrPathEscape. The argument was never
// judged in that case, so calling it an escape would send the caller after a path that is fine.
//
// SYMLINK POLICY — what os.Root does NOT decide. os.Root judges one question only: does
// resolution leave the root. A symlink pointing INSIDE the root is therefore followed, and
// following it silently moves the operation somewhere the argument never named — an in-root
// directory link `docs → .git` makes a write to "docs/config" land on ".git/config" without
// ever leaving the workspace, so the workspace fence has nothing to say about it and the
// operator approved a path that is not the path touched. The two directions are answered
// differently, per the hostile-bytes ratified call:
//
//   - WRITES REFUSE a parent chain that crosses a symlink (refuseSymlinkedParents, applied
//     by Fence.WriteFile): a write must reach its target through real directories, so the name
//     on the approval pane is the name that changes. The final component is exempt because
//     the write REPLACES that name rather than writing through it (see Fence.WriteFile).
//   - READS FOLLOW an in-root symlink, and the tools that read disclose where the name
//     resolved (internal/tools' `→ resolves to …` note). Refusing reads would break the
//     ordinary linked-file layouts a repo legitimately has; disclosing is what closes the gap
//     between what the operator reads and what the model got.
//
// The refusal is a POLICY CHECK, not a second fence: it is decided from the chain as it
// exists at check time, so an in-root component swapped to a symlink after the check can
// still redirect a write WITHIN the root (never outside it — that half is os.Root's, decided
// at use time). This layer is a guard, not a boundary (see doc.go); the boundary is the
// Confiner, which is what bounds who can plant the link in the first place.

// ErrSymlinkedParent is returned when a write's path reaches its target THROUGH a symlinked
// directory. It is deliberately NOT ErrPathEscape: nothing escaped the workspace — the write
// was refused because the operator was shown one path and the filesystem would have changed
// another. Callers that distinguish the two report each in its own words; callers that only
// print the error say the true thing either way.
var ErrSymlinkedParent = errors.New("write path crosses a symlinked directory")

// refuseSymlinkedParents refuses a MUTATION whose path — rel, relative to the pinned root r —
// reaches its target through anything other than real directories, returning an error wrapping
// ErrSymlinkedParent as soon as a parent component is a symlink, and nil when the whole chain is
// real. Every chain a Fence verb mutates goes through it: WriteFile's target, Rename's two
// ends, Remove's target and CopyFileFrom's destination. A chain that is
// only READ — a copy's source — does not, because reads follow in-root links and disclose where
// they landed.
//
// The TARGET's own name is not examined — these primitives replace or unlink that name rather
// than following it (Fence.WriteFile's replace-the-name contract, Fence.Remove's unlink-the-name
// one), which is what makes a symlinked final name a disclosure question for the read side rather
// than a redirect on the write side.
//
// The walk is top-down through the pinned root, so the OUTERMOST offending component is the one
// named: `docs → .git` is reported as "docs", the link the operator can actually look at, not as
// some deeper component that only exists because of it. A component that does not exist ends the
// walk successfully — nothing below an absent directory can exist either, and Fence.WriteFile's
// MkdirAll then creates real directories the whole rest of the way.
func refuseSymlinkedParents(r *os.Root, rel string) error {
	dir := filepath.Dir(rel)
	if dir == "." || dir == string(filepath.Separator) {
		return nil
	}

	walked := ""
	for _, part := range strings.Split(dir, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		walked = filepath.Join(walked, part)

		info, err := r.Lstat(walked)
		switch {
		case err == nil:
		case errors.Is(err, os.ErrNotExist):
			return nil
		default:
			return mapRootEscape(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return symlinkedParentError(r, walked)
		}
	}
	return nil
}

// symlinkedParentError renders the refusal so an operator can act on it: the component that is a
// symlink and, when it can be read, where that symlink points — the pair that turns "refused"
// into "this directory is not the directory you think it is". An unreadable link degrades to the
// component alone rather than to a bare failure.
//
// A link that leaves the ROOT keeps the fence's own uniform escape wording instead: that refusal
// predates this policy, every caller matches ErrPathEscape for it, and "outside the workspace" is
// the more urgent half of the truth. Only a link that stays inside — the case the fence has no
// reason to refuse — is reported as ErrSymlinkedParent.
func symlinkedParentError(r *os.Root, component string) error {
	if _, err := r.Stat(component); isRootEscapeError(err) {
		return mapRootEscape(err)
	}
	return inRootSymlinkedParentError(r, component)
}

// inRootSymlinkedParentError is symlinkedParentError's in-root half, split out so the escape
// classification above reads as the one decision it makes.
func inRootSymlinkedParentError(r *os.Root, component string) error {
	target, err := r.Readlink(component)
	if err != nil {
		return fmt.Errorf("%w: %q (a write must reach its target through real directories)",
			ErrSymlinkedParent, component)
	}
	return fmt.Errorf("%w: %q -> %q (a write must reach its target through real directories)",
		ErrSymlinkedParent, component, target)
}

// stagingPrefix is the basename prefix of Fence.WriteFile's staging file. The leading dot
// keeps it out of ordinary listings, and the fixed prefix makes a leftover from a killed
// process recognisable as ours.
const stagingPrefix = ".apogee-tmp-"

// stagingNameAttempts bounds the retries on a staging-name collision. Names carry 64 bits
// of randomness, so a collision means a stale leftover with that exact name; a handful of
// attempts is generous.
const stagingNameAttempts = 5

// targetMode reports the mode Fence.WriteFile's staged write must land with, and whether the
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

// createStagingFile creates Fence.WriteFile's staging file inside dir — the target's own
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
		_ = f.Close()
		return err
	}
	if applyMode {
		if err := f.Chmod(mode); err != nil {
			_ = f.Close()
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
// A symlink pointing INSIDE the root is FOLLOWED, deliberately: the contents are whatever the
// name resolves to within the fence, which is what makes ordinary linked-file layouts keep
// working. That is also why "the file this call read" and "the file the caller named" can be
// two different paths — a caller that shows the operator (or the model) which file it read must
// disclose the resolved name rather than echo its argument (the SYMLINK POLICY note in the
// package header; internal/tools renders it as `→ resolves to …`).
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
		return nil, fmt.Errorf("%w: %v", ErrRootInaccessible, err)
	}
	defer func() { _ = r.Close() }()

	data, err := r.ReadFile(rel)
	if err != nil {
		return nil, mapRootEscape(err)
	}
	return data, nil
}

// ErrNotRegular is returned when SafeOpen reaches something that is neither a regular file nor a
// directory — a named pipe, a socket, a device. It is deliberately NOT ErrPathEscape: nothing left
// the workspace, the name simply does not lead to bytes a read tool can bound and return. Callers
// that render errors for the model pass it through verbatim, so the refusal is never disguised as
// absence.
var ErrNotRegular = errors.New("not a regular file")

// SafeOpen opens input (relative to root, or absolute-inside-root) for reading, with the
// workspace fence enforced at OPEN time through an os.Root pinned at root, so a symlinked
// component pointing outside the root is refused rather than followed. A path escape
// returns an error wrapping ErrPathEscape and nothing is opened.
//
// Only a regular file or a directory comes back open. The name is opened O_NONBLOCK, because a
// blocking open of a FIFO with no writer never returns and would wedge the calling tool before
// any fstat could see what it was — a planted pipe in the workspace is the case (2026-09-20
// audit). The opened descriptor is then fstatted, and a named pipe, a socket or a device is closed
// again and refused with an error wrapping ErrNotRegular. A UNIX socket never reaches that gate:
// open(2) itself refuses it (ENXIO) and the error passes through as an ordinary I/O failure.
// The flag stays on the descriptor: read(2) and getdents ignore O_NONBLOCK on a regular file and
// a directory, which TestSafeOpen_RegularFileStillReadsBlocking and TestSafeOpen_DirectoryStillOpens
// prove, so the handle reads exactly as a blocking one would.
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
		return nil, fmt.Errorf("%w: %v", ErrRootInaccessible, err)
	}
	defer func() { _ = r.Close() }()

	f, err := r.OpenFile(rel, os.O_RDONLY|openNonblock, 0)
	if err != nil {
		return nil, mapRootEscape(err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if info.Mode()&(os.ModeNamedPipe|os.ModeSocket|os.ModeDevice|os.ModeCharDevice) != 0 {
		_ = f.Close()
		return nil, fmt.Errorf("%w: %s", ErrNotRegular, input)
	}
	return f, nil
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
		_ = f.Close()
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, 0, fmt.Errorf("not a regular file: %s", input)
	}
	return f, info.Mode().Perm(), nil
}

// stageCopy lands src's bytes at dstRel through the root pinned at the DESTINATION's end, with
// Fence.WriteFile's staging discipline: parents created inside the fence, the bytes staged in the
// destination's own parent, the rename last, and the staging file removed on any failure after
// it is created.
func stageCopy(dr *os.Root, dstRel string, src io.Reader, mode os.FileMode) error {
	dir := filepath.Dir(dstRel)
	if dir != "." {
		// Same reasoning as Fence.WriteFile's parent creation: an ESCAPE here is fatal, any other
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
		_ = dst.Close()
		return err
	}
	if err := dst.Chmod(mode); err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
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
