package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/undo"
)

// Path-safety is consolidated into the shared internal/security guard (P3.6 / D6):
// one symlink-aware, traversal-rejecting boundary every guarded tool inherits, in
// every mode. These package-local aliases keep the built-in tools (and their tests)
// calling the same names while the implementation lives in one place. Behaviour is
// unchanged — security.ResolveInRoot is the verbatim move of the former local code.

// ErrPathEscape is returned when a tool argument resolves to a path outside the
// sandbox root. It is the security guard's sentinel, re-exported here so existing
// errors.Is(err, ErrPathEscape) checks in the tools and their tests keep matching.
// Its counterpart security.ErrRootInaccessible — the ROOT itself would not open, which
// says nothing about the argument — is matched by its own qualified name at the few
// sites that distinguish the two, always ahead of this one.
var ErrPathEscape = security.ErrPathEscape

// resolveInRoot resolves input within root via the shared path-safety guard, returning
// ErrPathEscape for a path that escapes the workspace (symlinks followed).
func resolveInRoot(input, root string) (string, error) {
	return security.ResolveInRoot(input, root)
}

// refuseExecFromWritablePath refuses a program that resolves inside the writable confinement
// box through the shared exec fence (security.RefuseExecFromWritablePath): every tool that
// resolves an executable on PATH — git, python_exec, run_tests, diagnostics — calls it at the
// resolution site, so bytes the model was allowed to write can never become argv[0]. The error
// names the RESOLVED path, which is what tells an operator their in-repo virtualenv is the
// cause rather than a missing install.
func refuseExecFromWritablePath(argv0, root string, box *domain.ConfinementBox) error {
	return security.RefuseExecFromWritablePath(argv0, root, box)
}

// confinementBox returns the box a confined call runs inside, or nil when no Confinement
// handle rides on ctx — the gated/unconfined case, where the workspace root is the whole
// fence. It is the small read the exec sites need to pass the box on to the fence above.
func confinementBox(ctx context.Context) *domain.ConfinementBox {
	if conf, ok := domain.ConfinementFromContext(ctx); ok {
		return &conf.Box
	}
	return nil
}

// safeWriteFile writes data to input within root through the shared TOCTOU-safe guard:
// the workspace fence is enforced at WRITE time (os.Root-pinned), so a symlinked path
// component swapped to point outside the root — including a concurrent swap by a confined
// subprocess — is refused rather than followed (security review H1). It replaces the
// former resolveInRoot+os.WriteFile pair, which re-walked the path with a check/use gap.
//
// The final argument is security's approved-escape permit (ADR 0049), read off ctx: empty for
// every ordinary call, so the workspace root alone bounds the write exactly as it always did,
// and otherwise the ONE resolved path the operator was shown and approved — which security
// re-resolves the argument against before anything lands.
//
// It is also where the undo journal takes its pre-image (ADR 0051): the bytes this write is
// about to replace are read BEFORE the mutation and recorded only AFTER it succeeded, so a
// refused write journals nothing. Because every content-writing verb — write_file,
// edit_existing_file, single_find_and_replace, multi_find_and_replace — reaches the filesystem
// through here, that is one capture site rather than four.
func safeWriteFile(ctx context.Context, input, root string, data []byte, perm os.FileMode) error {
	pre := capturePreImage(ctx, input, root)
	if err := security.SafeWriteFile(root, input, data, perm, writeEscapeTarget(ctx)); err != nil {
		return err
	}
	pre.commit(data, true, perm)
	return nil
}

// safeReadFile reads input within root through the shared TOCTOU-safe guard, with the
// workspace fence enforced at READ time so an escaping symlink component is refused
// rather than followed (security review H1). It replaces the former resolveInRoot+
// os.ReadFile pair for the write tools' read-modify-write step.
func safeReadFile(input, root string) ([]byte, error) {
	return security.SafeReadFile(root, input)
}

// safeOpen opens input for reading within root through the shared TOCTOU-safe guard, with
// the workspace fence enforced at OPEN time (os.Root-pinned). The returned handle pins the
// file's identity: what is statted and read through it is the file that was opened,
// regardless of any rename after. The caller owns Close and any size policy.
func safeOpen(input, root string) (*os.File, error) {
	return security.SafeOpen(root, input)
}

// statInRoot stats path within root through ONE pinned descriptor: the file is opened
// through the workspace fence (os.Root-pinned) and the FileInfo is an fstat of THAT
// descriptor, so what is described is what was opened. It replaces the resolveInRoot +
// os.Stat pair, whose second half re-walked the path string and would follow a component
// swapped to point outside the workspace after the check passed (the H1 check-then-use gap).
// A directory opens successfully and is reported by its FileInfo, so each caller keeps its
// own "not a file" / "not a directory" wording.
func statInRoot(path, root string) (os.FileInfo, error) {
	f, err := safeOpen(path, root)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

// ----------------------------------------------------------------------------
// The approved escape (ADR 0049)
// ----------------------------------------------------------------------------
//
// A workspace-scoped write whose target lies OUTSIDE the workspace is GATED, and the approval
// pane shows the operator the resolved path before they answer (confinement-execution-contract
// §4). When the answer is yes — or the mode is the one whose contract is that the VM is the box —
// dispatch stamps the execution context with a domain.WriteEscapePermit naming exactly that
// resolved path. The three helpers below are all this package does with it: read the permitted
// target, and pin the write family's OWN read-back and pre-flight stat to it. There is no
// per-tool logic and no per-tool decision — the permit either governs this call's target or it
// does not, and every verb asks the same question in the same place.
//
// The floor is unconditional: a call with no permit passes "" and behaves byte-for-byte as it did
// before ADR 0049, and no READ tool takes a permit at all.

// writeEscapeTarget answers the ONE resolved absolute path this execution may write outside the
// workspace fence, or "" when there is none — the string internal/security's mutating primitives
// take as their permitted target. It sits beside confinementBox because it answers the same shape
// of question: what did the engine authorise for THIS execution, read at the site that needs it
// rather than threaded through every tool.
func writeEscapeTarget(ctx context.Context) string {
	permit, ok := domain.WriteEscapePermitFrom(ctx)
	if !ok {
		return ""
	}
	return permit.Real
}

// readWriteTarget reads the file a write tool is about to replace, through the fence THAT CALL is
// entitled to: the workspace root for an ordinary edit, and — for an approved escape — an os.Root
// pinned at the permitted target's own parent directory, which is where the write's own root is
// pinned (security's permitted branch). The read-modify-write verbs need this half: a patch or a
// find-and-replace has to see the bytes it is about to rewrite, so refusing the read would make
// "an approved gate executes" false for exactly the verbs a model edits with.
//
// It widens nothing else. The permit names one fully-resolved path and this pins to that path
// alone; the READ tools keep calling safeReadFile and are handed no permit ever, which is ADR
// 0049's write-side-only rule where it is enforceable. And the bytes cannot part company with the
// write: the write re-resolves the argument against the same permitted target, so an argument that
// has come to mean something else is refused and nothing lands.
func readWriteTarget(ctx context.Context, input, root string) ([]byte, error) {
	pinInput, pinRoot, absent := escapeTargetPin(ctx, input, root)
	if absent {
		return nil, os.ErrNotExist
	}
	return safeReadFile(pinInput, pinRoot)
}

// statWriteTarget stats the file a write tool is about to create, replace or remove, through the
// same fence readWriteTarget reads it through. It serves the file-operation tools' friendly
// pre-flight refusals (checkFileOpsPathsFrom, checkDeletePath), which have to look where the
// operation itself will look or they would describe a different file than the one that gets
// touched. The safety is still the fenced primitive's: it re-decides containment at operation
// time, so a name swapped after this returns can only turn a friendly refusal into a blunt one.
func statWriteTarget(ctx context.Context, path, root string) (os.FileInfo, error) {
	pinPath, pinRoot, absent := escapeTargetPin(ctx, path, root)
	if absent {
		return nil, os.ErrNotExist
	}
	return statInRoot(pinPath, pinRoot)
}

// escapeTargetPin answers the (input, root) pair a fenced read or stat of THIS CALL'S OWN write
// target must use, and whether that target is knowably absent.
//
// The workspace branch is checked FIRST and is unconditional: a permit never moves an in-workspace
// read, so a call carrying one behaves identically to one that does not for every path inside the
// fence. "Inside" is decided by RESOLUTION, which is why a workspace-spelled path that leaves the
// fence through a symlink is not inside it. Outside, the pair is repointed only when the argument
// re-resolves to EXACTLY the permitted target — the same equality security's mutation root routes
// on (internal/security/writepermit.go) — and then only to that target's own parent directory, so
// the one name reachable through the returned root is the approved one.
//
// absent is true when that parent is not an openable directory. The target cannot exist then, and
// the caller reports ordinary absence: pinning a root that cannot be opened would surface a fence
// refusal instead, which for a not-yet-created destination is both wrong and unexplainable.
func escapeTargetPin(ctx context.Context, input, root string) (pinInput, pinRoot string, absent bool) {
	permitted := writeEscapeTarget(ctx)
	if permitted == "" {
		return input, root, false
	}
	if _, err := resolveInRoot(input, root); err == nil {
		return input, root, false
	}
	target, ok := resolveTargetUnbounded(input, root)
	if !ok || target.Real != filepath.Clean(permitted) {
		return input, root, false
	}
	parent := filepath.Dir(target.Real)
	if !rootUsable(parent) {
		return "", "", true
	}
	return filepath.Base(target.Real), parent, false
}

// ----------------------------------------------------------------------------
// The undo journal (ADR 0051)
// ----------------------------------------------------------------------------
//
// `/undo` restores what the agent's writes replaced, and the only bytes that can do that are
// the ones that were there BEFORE the write. So the funnel reads them on the way in, holds
// them while the mutation runs, and hands them to the journal only once the mutation has
// actually landed. The two halves are deliberately separate calls: everything that could go
// wrong — a fence refusal, a symlinked parent, a full disk — happens between them, and each
// of those must leave the journal untouched, because a record claiming a change that never
// happened would make a later undo write stale bytes over a file nobody edited.
//
// Nothing here is a precondition of the write. A call outside an engine, or under an engine
// that keeps no journal, produces a nil preImage whose commit does nothing, and the mutation
// behaves byte-for-byte as it did before this existed.

// preImage is one pending journal record: what a mutation is about to replace, plus the
// fencing context (root and approved-escape permit) a revert has to go back through to reach
// the same file the write reached. It exists only between the capture and the commit.
type preImage struct {
	journal   *undo.Journal
	root      string
	path      string
	permitted string
	data      []byte
	existed   bool
}

// capturePreImage reads the current bytes of the file a mutation of input under root is about
// to change, through the same fence THAT mutation writes through (readWriteTarget, which
// follows an approved escape to its permitted target and nowhere else).
//
// It answers nil — journal nothing — in three cases: no journal is recording, the argument
// names no inspectable target, or the current bytes could not be read for any reason OTHER
// than the file being absent. The last one is the load-bearing refusal: a pre-image that is a
// guess would make a later undo destroy content rather than restore it, so an unreadable
// target is left out of the journal entirely and the write proceeds unchanged.
func capturePreImage(ctx context.Context, input, root string) *preImage {
	journal := undo.FromContext(ctx)
	if journal == nil {
		return nil
	}
	path, permitted := journalTarget(ctx, input, root)
	if path == "" {
		return nil
	}

	data, err := readWriteTarget(ctx, input, root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return &preImage{
		journal:   journal,
		root:      root,
		path:      path,
		permitted: permitted,
		data:      data,
		existed:   err == nil,
	}
}

// commit records the completed mutation against the pre-image this value captured. Call it
// ONLY after the mutation succeeded — that ordering is the whole reason capture and commit are
// two calls. post is the content the mutation left and exists says whether it left any (false
// for a removal); perm is the mode a restore recreates an absent file with.
//
// A nil receiver is the "nothing is recording" case and does nothing, so a caller journals by
// writing one unconditional line rather than by branching around the journal.
func (p *preImage) commit(post []byte, exists bool, perm os.FileMode) {
	if p == nil {
		return
	}
	p.journal.Record(undo.Mutation{
		Root:       p.root,
		Path:       p.path,
		Permitted:  p.permitted,
		Perm:       perm,
		Pre:        p.data,
		PreExisted: p.existed,
		Post:       post,
		PostExists: exists,
	})
}

// journalTarget answers the pair a journal record identifies this mutation by: the absolute
// path that IS the record's identity, and the approved-escape permit a revert must carry to
// reach it (empty for every ordinary write).
//
// The ordinary answer is the path the argument NAMES, root-joined and cleaned — not its
// symlink-resolved twin — because that is the spelling internal/security's fenced primitives
// take: they relativise it against the workspace root lexically, so a revert handed a resolved
// path would be refused as an escape on any host whose root is itself reached through a
// symlink (macOS /tmp). The approved escape is the one exception and takes the RESOLVED path,
// because that is what the permit names and what the approval pane disclosed (ADR 0049) — and
// it is recognised by exactly the test escapeTargetPin uses, so a record can never claim a
// permit the write itself did not run under.
func journalTarget(ctx context.Context, input, root string) (path, permitted string) {
	target, ok := resolveTargetUnbounded(input, root)
	if !ok {
		return "", ""
	}
	permitted = writeEscapeTarget(ctx)
	if permitted == "" || target.Real != filepath.Clean(permitted) {
		return target.Named, ""
	}
	if _, err := resolveInRoot(input, root); err == nil {
		return target.Named, ""
	}
	return target.Real, permitted
}
