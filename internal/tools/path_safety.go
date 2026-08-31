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

// confinementBox returns the box a confined call runs inside, or nil when no Confinement
// handle rides on ctx — the gated/unconfined case, where the workspace root is the whole
// fence. It is the small read the exec sites need to pass the box on to
// security.ResolveProgram, which resolves an executable and fences it in one step: every tool
// that reaches PATH — git, python_exec, run_tests, diagnostics — goes through it, so bytes the
// model was allowed to write can never become argv[0].
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
// A path spelled as a virtual-mount reference is refused here before anything else happens
// (refuseVirtualWrite): those trees are read-only by construction, and resolving `shipped:…`
// as an ordinary relative name would create a colon-named file inside the workspace and report
// the write as landed.
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
	if err := refuseVirtualWrite(input); err != nil {
		return err
	}
	pre := capturePreImage(ctx, input, root)
	if err := security.SafeWriteFile(root, input, data, perm, writeEscapeTarget(ctx)); err != nil {
		return err
	}
	pre.commit(data, true)
	return nil
}

// postImage says how a mutated path's after-state reaches the journal once the body of a
// journaledMutation has landed it. There is deliberately no "bytes the caller already holds"
// case: that is safeWriteFile's shape, and a verb that holds its own post-bytes belongs there.
type postImage int

const (
	// postAbsent is the path the mutation removed — a delete's target, a move's source. Its
	// record's post-state is "nothing", which is what makes an undo put the file back.
	postAbsent postImage = iota
	// postReadBack is the path the mutation landed bytes on that this process never held — a
	// copy's or a move's destination — so the journal reads them back off the file it left.
	postReadBack
)

// mutationPath names one path a multi-path mutation touches, in the spelling and under the root
// that mutation reaches it through.
//
// root is per-path rather than per-call because the ends of a copy do not share one: a copy's
// source may have matched a configured read-only root while its destination stays workspace-fenced
// (checkFileOpsPathsFrom). input is the ARGUMENT's spelling rather than its resolution, because
// that is what a record is identified by (journalTarget) and what a read-back re-resolves through
// the mutation's own fence (commitReadBack).
type mutationPath struct {
	input string
	root  string
	post  postImage
}

// journaledMutation is safeWriteFile's sibling for the verbs that move or remove whole files:
// copy_file, move_file and delete_file, none of which hold in memory the bytes they land, and one
// of which changes two paths over two possible routes. It captures a pre-image for EVERY path
// before body runs, hands body the approved-escape target (ADR 0049), then commits exactly the
// paths body reports as landed — each under its own post-image policy — and returns body's error
// unchanged. Together the two helpers are the whole of this package's undo capture (ADR 0051).
//
// landed carries one entry per path, in paths' order; a nil or short slice means the missing paths
// did not land. It is REPORTED rather than inferred from err because a move can fail half way —
// the copy landed, the removal was refused — and the journal has to keep the half that really
// happened while the call still reports the failure.
//
// Capturing before body is the load-bearing ordering: after a move the source does not exist and
// the destination holds the source's bytes, so neither pre-image is recoverable from the
// filesystem the mutation leaves behind. A landed path whose read-back fails journals nothing, for
// the reason an unreadable pre-image does: a record that describes a file it does not match turns
// every later undo of that path into a conflict it never had.
//
// The fence primitive stays the BODY's choice — security.SafeRename, SafeCopyFileFrom and
// SafeRemove differ in what they take and in how their failures triage — so this owns only the
// permit lookup, the capture and the commit. Outside an engine, or under one that keeps no
// journal, every capture is nil and body runs byte-for-byte as it would have alone.
func journaledMutation(
	ctx context.Context,
	paths []mutationPath,
	body func(escape string) (landed []bool, err error),
) error {
	for _, path := range paths {
		// Every path here is one this mutation WRITES, so a virtual-mount reference is refused
		// before anything is captured: the mounts are read-only by construction, and a copy,
		// move or delete that resolved `shipped:…` as an ordinary relative name would touch a
		// colon-named file inside the workspace instead (path_virtual.go).
		if err := refuseVirtualWrite(path.input); err != nil {
			return err
		}
	}

	captured := make([]*preImage, len(paths))
	for i, path := range paths {
		captured[i] = capturePreImage(ctx, path.input, path.root)
	}

	landed, err := body(writeEscapeTarget(ctx))

	for i, path := range paths {
		if i >= len(landed) || !landed[i] {
			continue
		}
		switch path.post {
		case postAbsent:
			captured[i].commit(nil, false)
		case postReadBack:
			captured[i].commitReadBack(ctx)
		}
	}
	return err
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
	if err := refuseVirtualWrite(path); err != nil {
		return nil, err
	}
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
//
// input is the argument the mutation NAMED, kept beside the resolved path because a read-back
// (commitReadBack) has to go through the same fence the mutation did, and that fence is chosen
// from the argument rather than from its resolution (escapeTargetPin).
type preImage struct {
	journal   *undo.Journal
	root      string
	input     string
	path      string
	permitted string
	data      []byte
	existed   bool
	perm      os.FileMode
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
	captured := &preImage{
		journal:   journal,
		root:      root,
		input:     input,
		path:      path,
		permitted: permitted,
		data:      data,
		existed:   err == nil,
	}
	if captured.existed {
		captured.perm = currentPerm(ctx, input, root)
	}
	return captured
}

// currentPerm answers the mode bits the file at input carries right now, read through the same
// fence its bytes were, or 0 when it cannot be stat'd — the journal then falls back to its own
// default mode. It is advisory: the mode is consulted only to recreate a file a revert RESTORES,
// so being wrong about it costs a restored file its executable bit and nothing more.
func currentPerm(ctx context.Context, input, root string) os.FileMode {
	info, err := statWriteTarget(ctx, input, root)
	if err != nil {
		return 0
	}
	return info.Mode().Perm()
}

// commit records the completed mutation against the pre-image this value captured. Call it
// ONLY after the mutation succeeded — that ordering is the whole reason capture and commit are
// two calls. post is the content the mutation left and exists says whether it left any (false
// for a removal).
//
// The record's restore mode is the PRE-IMAGE's own, never one the caller supplies: the journal
// consults that mode only to recreate a file a revert RESTORES, and a revert restores only a
// path whose pre-image existed — so the mode that file already carried is the only answer that
// can be right, for a deletion and an overwrite alike.
//
// A nil receiver is the "nothing is recording" case and does nothing, so a caller journals by
// writing one unconditional line rather than by branching around the journal.
func (p *preImage) commit(post []byte, exists bool) {
	if p == nil {
		return
	}
	p.journal.Record(undo.Mutation{
		Root:       p.root,
		Path:       p.path,
		Permitted:  p.permitted,
		Perm:       p.perm,
		Pre:        p.data,
		PreExisted: p.existed,
		Post:       post,
		PostExists: exists,
	})
}

// commitReadBack records the completed mutation with its post-image READ BACK from the file the
// mutation left behind — the form the two byte-moving verbs need, since copy_file and move_file
// never hold in memory the bytes they land. The read goes through the same fence the mutation
// wrote through, so it sees exactly the file the mutation wrote.
//
// A read-back that fails journals NOTHING, for the same reason an unreadable pre-image does: a
// post-hash that is a guess describes a file the record does not actually match, and every later
// undo of that path would be refused as a conflict it never had.
func (p *preImage) commitReadBack(ctx context.Context) {
	if p == nil {
		return
	}
	data, err := readWriteTarget(ctx, p.input, p.root)
	if err != nil {
		return
	}
	p.commit(data, true)
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
