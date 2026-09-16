package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// ----------------------------------------------------------------------------
// The write side's scope value
// ----------------------------------------------------------------------------
//
// readScope (path_read.go) is the READ side's one answer to "what may this tool read, and through
// which fence": every read tool holds one and asks it once per call. writeScope is the WRITE side's
// twin — what this execution is entitled to write, and through which fence — and writeTarget is the
// answer it gives for one call's argument. A write tool asks its scope ONCE, for the argument the
// workspaceScopedWriter marker names, and then reads, stats, discloses and writes through the value
// it got back, so the six things a write verb does to its path can never be six resolutions of it.
//
// The fence itself is unchanged: every method reaches the filesystem through the same os.Root-pinned
// primitives the free functions in path_safety.go reach, and the two of those the undo capture still
// takes by argument and root (readWriteTarget, currentPerm) are one-line shims over these methods.

// errPathRequired is the refusal every single-path writer spells for an empty argument, answered by
// writeScope.target so a tool reads it off the one error return rather than checking first. It is a
// sentinel because the text IS the contract: the write tools' tests and the TUI fixtures quote it.
var errPathRequired = errors.New("path is required")

// writeScope is what ONE execution of a write tool may write, and through which fence: the
// workspace root, and the one resolved out-of-workspace path an approved escape permits (ADR
// 0049; "" for every ordinary call). It is built per call from the execution context because the
// permit is stamped there by dispatch (writeScopeOf), and holds nothing a tool could not read for
// itself — its job is to answer the question once and hand every later operation the same answer.
//
// ctx rides along ONLY for the undo funnels: capture still lives in path_safety.go and reads the
// journal and the permit off the context (safeWriteFile, journaledMutation — ADR 0051), so write
// and journaled hand it on unchanged. When capture becomes structural the journal joins the scope
// beside the permit and the context leaves it.
type writeScope struct {
	ctx    context.Context
	root   string
	permit string
}

// writeScopeOf reads the scope of one execution off its context: the workspace root the tool was
// built with and the approved-escape permit dispatch stamped for THIS call, if any.
func writeScopeOf(ctx context.Context, root string) writeScope {
	return writeScope{ctx: ctx, root: root, permit: writeEscapeTarget(ctx)}
}

// target resolves the one argument the marker names into the value every later operation of this
// call goes through — the same root-only resolution the marker's own workspaceWriteTarget performs
// (resolveTargetUnbounded), so what dispatch classified, what the approval pane disclosed and what
// the write reads and lands on are one reading of the call. An empty argument is the one error:
// there is nothing to resolve, and errPathRequired is the refusal every writer spells for it.
func (s writeScope) target(input string) (writeTarget, error) {
	target, ok := resolveTargetUnbounded(input, s.root)
	if !ok {
		return writeTarget{}, errPathRequired
	}
	target.input = input
	target.scope = s
	return target, nil
}

// pin answers the (input, root) pair a fenced read or stat of THIS CALL'S OWN write target must
// use, and whether that target is knowably absent.
//
// The workspace branch is checked FIRST and is unconditional: a permit never moves an in-workspace
// read, so a call carrying one behaves identically to one that does not for every path inside the
// fence. "Inside" is decided by RESOLUTION, which is why a workspace-spelled path that leaves the
// fence through a symlink is not inside it. Outside, the pair is repointed only when the target's
// disclosed Real IS the permitted path — the same equality security's mutation root routes on
// (internal/security/writepermit.go) — and then only to that target's own parent directory, so the
// one name reachable through the returned root is the approved one (ADR 0049).
//
// absent is true when that parent is not an openable directory. The target cannot exist then, and
// the caller reports ordinary absence: pinning a root that cannot be opened would surface a fence
// refusal instead, which for a not-yet-created destination is both wrong and unexplainable.
func (t writeTarget) pin() (pinInput, pinRoot string, absent bool) {
	if t.scope.permit == "" {
		return t.input, t.scope.root, false
	}
	if _, err := resolveInRoot(t.input, t.scope.root); err == nil {
		return t.input, t.scope.root, false
	}
	if t.Real != filepath.Clean(t.scope.permit) {
		return t.input, t.scope.root, false
	}
	parent := filepath.Dir(t.Real)
	if !rootUsable(parent) {
		return "", "", true
	}
	return filepath.Base(t.Real), parent, false
}

// read reads the file this call is about to replace, through the fence THAT CALL is entitled to:
// the workspace root for an ordinary edit, and — for an approved escape — an os.Root pinned at the
// permitted target's own parent directory, which is where the write's own root is pinned
// (security's permitted branch). The read-modify-write verbs need this half: a patch or a
// find-and-replace has to see the bytes it is about to rewrite, so refusing the read would make
// "an approved gate executes" false for exactly the verbs a model edits with.
//
// It widens nothing else. The permit names one fully-resolved path and pin repoints to that path
// alone; the READ tools keep reading through readScope and are handed no permit ever, which is ADR
// 0049's write-side-only rule where it is enforceable. And the bytes cannot part company with the
// write: the write re-resolves the argument against the same permitted target, so an argument that
// has come to mean something else is refused and nothing lands.
func (t writeTarget) read() ([]byte, error) {
	pinInput, pinRoot, absent := t.pin()
	if absent {
		return nil, os.ErrNotExist
	}
	return safeReadFile(pinInput, pinRoot)
}

// stat stats the file this call is about to create, replace or remove, through the same fence
// read reads it through. It serves the write verbs' friendly pre-flight refusals — a directory
// where a file was expected, a missing source — which have to look where the operation itself
// will look or they would describe a different file than the one that gets touched. The safety
// is still the fenced primitive's: it re-decides containment at operation time, so a name swapped
// after this returns can only turn a friendly refusal into a blunt one.
func (t writeTarget) stat() (os.FileInfo, error) {
	if err := t.refuseVirtual(); err != nil {
		return nil, err
	}
	pinInput, pinRoot, absent := t.pin()
	if absent {
		return nil, os.ErrNotExist
	}
	return statInRoot(pinInput, pinRoot)
}

// perm answers the mode bits the target carries right now, read through the same fence its bytes
// were, or 0 when it cannot be stat'd — the undo journal then falls back to its own default mode.
// It is advisory: the mode is consulted only to recreate a file a revert RESTORES, so being wrong
// about it costs a restored file its executable bit and nothing more.
func (t writeTarget) perm() os.FileMode {
	info, err := t.stat()
	if err != nil {
		return 0
	}
	return info.Mode().Perm()
}

// refuseVirtual is the write fence's half of the mount contract on this value: a target spelled
// as a virtual-mount reference is refused with the uniform escape message, because a virtual
// mount is read-only by construction (refuseVirtualWrite). nil is the answer for every ordinary
// path.
func (t writeTarget) refuseVirtual() error {
	return refuseVirtualWrite(t.input)
}

// notFound renders a failed fenced read of this target the way every disk-rooted not-found refusal
// is rendered (notFoundOrRefusal): a fence refusal or an inaccessible root in its own uniform
// words, and an absence the error confirms with the near misses suggestSiblings finds under the
// workspace root, spelled the way the model spelled the path. prefix is the caller's own wording up
// to and including its separator ("file not found: "). A refusal NEVER gains suggestions — a "did
// you mean" on a refusal would read as absence and hide it.
func (t writeTarget) notFound(err error, prefix string) string {
	return notFoundOrRefusal(err, prefix, t.scope.root, workspaceRelative(t.input, t.scope.root), t.input)
}

// note is the RESULT-STRING half of the disclosure (ResolvedWriteTarget): the tail a write appends
// to the sentence it reports, naming where the call really landed when that is not the path the
// argument named — " → resolves to <Real>" — and "" for an ordinary call, so a result the model
// reads grows nothing on the common path. A virtual-mount reference names no host path at all, so
// there is nothing to disclose.
//
// Compute it BEFORE the write: the write replaces a symlinked final name with a regular file, so
// afterwards nothing is left to say that the call went somewhere else, and the sentence would part
// company with the approval pane on exactly the call worth disclosing.
func (t writeTarget) note() string {
	if _, _, isMount := virtualMountRef(t.input); isMount {
		return ""
	}
	if t.Real == t.Named {
		return ""
	}
	return " → resolves to " + t.Real
}

// write lands data on this target with perm through the TOCTOU-safe funnel (safeWriteFile): the
// workspace fence — or the approved escape's permitted target — is enforced AT WRITE TIME through
// an os.Root, parent directories are created within the same fence, and the undo journal takes its
// pre-image before and its record after (ADR 0051). A virtual-mount reference is refused before
// anything happens.
func (t writeTarget) write(data []byte, perm os.FileMode) error {
	return safeWriteFile(t.scope.ctx, t.input, t.scope.root, data, perm)
}

// mutation is this target as ONE path of a multi-path mutation (journaledMutation): the argument's
// spelling and the root the funnel captures its pre-image through and reads its post-image back
// through, under post. It is the value's MULTI-PATH form — a copy's directory form hands the funnel
// N destinations and a move its two ends, so those verbs assemble the slice from their values and
// keep the funnel's own body shape (one landed flag per path), where journaled below is the
// one-target case.
func (t writeTarget) mutation(post postImage) mutationPath {
	return mutationPath{input: t.input, root: t.scope.root, post: post}
}

// journaled runs body as a mutation of this ONE target that lands or removes bytes this process
// never holds — a delete, or one end of a copy or move — through the sibling funnel
// (journaledMutation): the pre-image is captured before body runs, body is handed the
// approved-escape target (ADR 0049), and the record is committed under post only when body reports
// the target landed. body's error is returned unchanged; the fence primitive stays body's choice.
func (t writeTarget) journaled(post postImage, body func(escape string) (landed bool, err error)) error {
	paths := []mutationPath{t.mutation(post)}
	return journaledMutation(t.scope.ctx, paths, func(escape string) ([]bool, error) {
		landed, err := body(escape)
		return []bool{landed}, err
	})
}
