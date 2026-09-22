package security

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The Fence — the workspace root and the one approved escape, as a value
// ----------------------------------------------------------------------------
//
// Every mutation this package performs is bounded by two facts: the workspace root it is pinned
// to, and the single out-of-workspace path an approval may have authorised (ADR 0049). Until the
// Fence existed the two travelled separately — the root as one string argument, the permit as
// another, empty for "none" — and every caller had to carry both and keep them paired. A Fence is
// that pair as one value: the permit is TYPED (domain.WriteEscapePermit, never a bare string that
// could be an unrelated path), and the ADR 0049 question — does this argument mean the approved
// target? — is asked in one place, Governs, that every verb below routes through.
//
// The verbs are the mutating primitives of safeio.go, moved here: WriteFile, Remove, CopyFile,
// CopyFileFrom and Rename. The read primitives (SafeReadFile, SafeOpen) are NOT verbs of the
// Fence and take no permit — an approval authorises the write the operator read, not a wider
// view of the host (ADR 0049 D2). The rule an approved escape lands under — re-resolution, the
// deepest-existing-ancestor root, the symlinked-target refusal — is stated once in writepermit.go
// and inherited here by construction.

// Fence is the bound a mutation runs under: the workspace Root every write is pinned to, and the
// Permit that names the one resolved path outside it an approval authorised. A zero Permit is the
// ordinary case — every call that never met a Gate — and with it the Fence is the workspace root
// alone, byte-for-byte the behaviour before permits existed.
//
// A Fence is a value: copy it freely, and never mutate one a caller handed you. Its verbs are
// value-receiver methods so a Fence recorded beside a mutation (an undo record, a journal entry)
// states exactly the bound that mutation ran under.
type Fence struct {
	// Root is the workspace root the fence is pinned to — the directory every relative argument
	// joins and every os.Root a verb opens is anchored at.
	Root string
	// Permit is the one approved escape target (ADR 0049), or the zero value for none.
	Permit domain.WriteEscapePermit
}

// WorkspaceFence returns the Fence for root with no permit: the workspace fence and nothing
// else, which is what every mutation that never met a Gate runs under.
func WorkspaceFence(root string) Fence {
	return Fence{Root: root}
}

// Governs reports whether input means exactly the permitted target — the single question that
// decides whether a mutation runs through the permitted root instead of the workspace fence
// (the ADR 0049 rule, and its 2026-08-14 amendment: an argument spelled INSIDE the workspace
// that leaves it through a symlink is judged by where it resolves, not by how it is spelled).
//
// The answer is reproduced, not trusted: input is rendered absolute exactly as every mutation
// resolves it (a relative argument joins Root) and re-resolved through EvalRealPath, the same
// resolution dispatch classified the write with, so an argument that has come to mean a
// different path since the disclosure answers no. A Fence with no permit governs nothing, and
// says so without touching the filesystem.
//
// It is asked twice on purpose. openMutationRoot asks it to ROUTE, ahead of the lexical branch;
// openPermittedRoot asks it again to REFUSE, with the specific message an operator whose approval
// stopped executing needs. Both ask it of the same string through the same resolution, so a path
// can never route one way and be judged another.
func (f Fence) Governs(input string) bool {
	if f.Permit.Real == "" {
		return false
	}
	return f.Permit.Names(EvalRealPath(permittedName(f.Root, input)))
}

// WriteFile writes data to input (relative to Root, or absolute-inside-Root), creating parent
// directories as needed, with the workspace fence enforced at WRITE time through an os.Root
// pinned at Root. A path that escapes the root — by traversal, by an absolute target outside
// root, or by a symlinked component pointing outside (including one swapped in concurrently) —
// returns an error wrapping ErrPathEscape, and nothing is written.
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
//
// The PARENT CHAIN is the opposite: every directory component leading to the target must be
// a real directory. A parent that is a symlink — even one pointing inside the root, which the
// workspace fence has no reason to refuse — returns an error wrapping ErrSymlinkedParent
// before anything is created, staged or mkdir'd, because following it would land the write
// somewhere the argument never named (the SYMLINK POLICY note in safeio.go). So the
// guarantee is: the write touches EXACTLY the name the caller passed, resolved through real
// directories, or it touches nothing.
//
// The Fence's Permit is the one out-of-workspace path an approval authorised — the resolved
// target the approval pane disclosed (ADR 0049) — or zero for the ordinary call, which is every
// call that never met a Gate. A permit changes nothing for a target inside Root; for an input
// that Governs (re-resolves to exactly that path) it pins the write to the target's own deepest
// existing ancestor instead of refusing it. The rule is stated once, in writepermit.go.
func (f Fence) WriteFile(input string, data []byte, perm os.FileMode) error {
	r, rel, err := openMutationRoot(f, input)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	// Before ANY mutation: a symlinked parent would redirect the MkdirAll below as readily
	// as it would redirect the write itself.
	if err := refuseSymlinkedParents(r, rel); err != nil {
		return err
	}

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
	staged, file, err := createStagingFile(r, dir, mode)
	if err != nil {
		return mapRootEscape(err)
	}
	if err := stageAndClose(file, data, mode, existed); err != nil {
		_ = r.Remove(staged)
		return err
	}
	if err := r.Rename(staged, rel); err != nil {
		_ = r.Remove(staged)
		return mapRootEscape(err)
	}
	return nil
}

// CopyFileFrom copies the regular file at srcInput — relative to srcRoot, or
// absolute-inside-srcRoot — to dstInput under the Fence, with EACH END fenced at COPY time by its
// OWN os.Root: the source is read through a root pinned at srcRoot, the destination written
// through a root pinned at Root. A path that escapes ITS OWN root (by traversal, by an
// absolute target outside it, or by a symlinked component pointing outside, including one
// swapped in concurrently) returns an error wrapping ErrPathEscape, and nothing is written. The
// two fences are independent: a source escaping srcRoot is refused even when it happens to lie
// inside Root, and vice versa.
//
// Two roots exist for the one case where a read boundary and a write boundary legitimately
// differ — a copy's source is a READ, so it may come from a configured read-only root (the
// skills library) while the destination stays workspace-fenced. The write half is not widened
// by any of this: Root bounds the only thing this call creates, exactly as the one-root form
// does. The source root is the READ side's and is UNGOVERNED: the Fence's Permit bounds the
// DESTINATION alone, because a read is never widened by an approval to write (ADR 0049 D2).
//
// The destination is written with WriteFile's guarantees: parent directories are created
// inside the fence, the bytes are staged in the destination's own parent and renamed over it,
// and the staging file is removed on any failure after it is created. So the destination name
// is never seen half-copied, and — the rename being the last step — an in-root symlink AT that
// name is replaced by a regular file rather than written through. The destination's PARENT chain
// gets WriteFile's refusal too: a parent that is a symlink, even one staying inside Root,
// returns an error wrapping ErrSymlinkedParent before the source is opened and before anything is
// staged or mkdir'd. The SOURCE chain is exempt by design — a copy's source is a read, and reads
// follow in-root links.
//
// Mode: the destination lands with the SOURCE's mode, whether or not it already existed. That
// is what distinguishes a copy from a write: the point of copying a 0755 script is to end up
// with a 0755 script. A source that is not a regular file (a directory, a device) is refused:
// this primitive copies ONE file, and a caller copying a tree (copy_file's directory branch)
// enumerates it through its own fence and calls this once per file — the caller's own "not a
// file" wording is the model-facing one, this is the backstop.
//
// The content is streamed, so a copy costs no more memory than its buffer however large the
// file is; there is no fsync, for the same reason WriteFile has none.
func (f Fence) CopyFileFrom(srcRoot, srcInput, dstInput string) error {
	srcRel, err := rootRelative(srcInput, srcRoot)
	if err != nil {
		return err
	}
	dr, dstRel, err := openMutationRoot(f, dstInput)
	if err != nil {
		return err
	}
	defer func() { _ = dr.Close() }()
	sr, err := os.OpenRoot(srcRoot)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRootInaccessible, err)
	}
	defer func() { _ = sr.Close() }()

	// Before ANY mutation, and before the source is even opened: a symlinked parent on the
	// DESTINATION chain would land the copy somewhere the argument never named, exactly as it
	// would redirect a WriteFile. The SOURCE chain is deliberately not checked — a copy's
	// source is a read, and reads follow (the SYMLINK POLICY note in safeio.go).
	if err := refuseSymlinkedParents(dr, dstRel); err != nil {
		return err
	}

	src, mode, err := openCopySource(sr, srcRel, srcInput)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	return stageCopy(dr, dstRel, src, mode)
}

// CopyFile copies the regular file at srcInput to dstInput — both relative to Root, or
// absolute-inside-Root — with the workspace fence enforced at COPY time. It is CopyFileFrom's
// EQUAL-ROOTS special case: the one root pins both ends, which is what every caller whose source
// and destination are both workspace paths wants. Every guarantee documented on CopyFileFrom —
// staged-and-renamed destination, the source's mode, a non-regular source refused, ErrPathEscape
// at either end with nothing written, the Permit bounding the destination alone — holds here
// unchanged.
func (f Fence) CopyFile(srcInput, dstInput string) error {
	return f.CopyFileFrom(f.Root, srcInput, dstInput)
}

// Rename renames oldInput to newInput — both relative to Root, or absolute-inside-Root —
// through a single os.Root pinned at Root, so BOTH ends of the rename are fenced: a path that
// escapes the root at either end returns an error wrapping ErrPathEscape and nothing moves.
// Parent directories of the destination are created inside the fence, as WriteFile creates
// them for its target.
//
// The rename is the filesystem's own: atomic, and it replaces an existing destination NAME
// (including a symlink at that name) without following it. Deciding whether replacing that name
// is allowed is the caller's policy, not this primitive's.
//
// BOTH parent chains must be real directories. A rename mutates both ends — the old name is
// unlinked, the new one created — so a symlinked parent on either chain would move a file the
// operator never named, and returns an error wrapping ErrSymlinkedParent with nothing created and
// nothing moved. The check runs before the destination's parents are created, which also means a
// caller that retries a FAILED rename as copy-then-remove (move_file does) knows any error other
// than this one came from two chains that already passed the gate.
//
// It is NEVER PERMITTED: the Fence's Permit is not consulted, whatever it names (ADR 0049), and
// that is not an omission. A rename is one syscall through one pinned root, so it cannot span the
// workspace fence and an approved target outside it — the kernel would refuse the cross-device
// move even if a root could express it. An approved escape MOVE is therefore the copy-then-remove
// pair: CopyFileFrom carries the permit to the destination, Remove unlinks the in-workspace source
// under the fence. That fallback is the designed route, not a workaround.
func (f Fence) Rename(oldInput, newInput string) error {
	oldRel, err := rootRelative(oldInput, f.Root)
	if err != nil {
		return err
	}
	newRel, err := rootRelative(newInput, f.Root)
	if err != nil {
		return err
	}
	r, err := os.OpenRoot(f.Root)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRootInaccessible, err)
	}
	defer func() { _ = r.Close() }()

	// Before ANY mutation: BOTH ends of a rename are mutated — the old name is unlinked, the new
	// one created — so a symlinked parent on either chain would move a file the operator never
	// named. Both chains are validated before the MkdirAll below, so a refused rename creates
	// nothing, and a caller that falls back to copy-then-remove on some OTHER failure knows both
	// ends already passed this gate.
	if err := refuseSymlinkedParents(r, oldRel); err != nil {
		return err
	}
	if err := refuseSymlinkedParents(r, newRel); err != nil {
		return err
	}

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

// Remove removes the name input (relative to Root, or absolute-inside-Root) through an
// os.Root pinned at Root, so a path escaping the root is refused rather than followed. It
// removes THE NAME: a symlink is unlinked, never the file it points at.
//
// The name's PARENT chain must be real directories, as it must for a write: an unlink lands
// wherever the chain leads, so a symlinked parent — even one staying inside the root — returns an
// error wrapping ErrSymlinkedParent and nothing is removed.
//
// It is os.Remove's contract otherwise — a non-empty directory is refused by the filesystem —
// and a missing name returns an error satisfying errors.Is(err, os.ErrNotExist).
//
// The Fence's Permit is WriteFile's approved escape target (ADR 0049), zero for the ordinary
// call. A deletion is the most destructive member of the approved family and is deliberately
// included: its Gate disclosed the same resolved path a write's would, so the same permit — and
// only the exact path it names — reaches the same primitive.
func (f Fence) Remove(input string) error {
	r, rel, err := openMutationRoot(f, input)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	// Before ANY mutation: the unlink lands wherever the parent chain leads, so a symlinked
	// parent would remove a file under a name the operator never approved — `docs/config` erasing
	// `.git/config` through a `docs → .git` link. The target's OWN name is not examined: removing
	// a symlink unlinks the link, which is this primitive's contract.
	if err := refuseSymlinkedParents(r, rel); err != nil {
		return err
	}

	if err := r.Remove(rel); err != nil {
		return mapRootEscape(err)
	}
	return nil
}
