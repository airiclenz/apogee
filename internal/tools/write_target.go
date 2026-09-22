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
// The fence itself is security's: every mutation a method lands goes through a verb of the
// security.Fence the scope holds, and every read through the same os.Root-pinned primitives
// path_safety.go's free functions reach. What the value ADDS is the undo capture (ADR
// 0051): write and journaled are the package's two write funnels, and the capture they take — a
// pre-image read through the value before the mutation, committed through it after — is a method of
// the value, so a writer that holds one cannot land bytes past the journal without also spelling a
// capture call of its own, which TestUndoCaptureHasExactlyTwoCallers refuses outside this file.

// errPathRequired is the refusal every single-path writer spells for an empty argument, answered by
// writeScope.target so a tool reads it off the one error return rather than checking first. It is a
// sentinel because the text IS the contract: the write tools' tests and the TUI fixtures quote it.
var errPathRequired = errors.New("path is required")

// writeScope is what ONE execution of a write tool may write, and through which fence: the
// security.Fence — the workspace root plus the one resolved out-of-workspace path an approved
// escape permits (ADR 0049; a zero Permit for every ordinary call) — and the undo journal recording
// this execution (ADR 0051; nil outside an engine, or under one that keeps none). It is built per
// call from the execution context because the permit and the journal are stamped there by dispatch
// (writeScopeOf), and holds nothing a tool could not read for itself — its job is to answer the
// question once and hand every later operation the same answer. The context itself does not ride
// along: everything the value needs of it is read here, once.
type writeScope struct {
	fence   security.Fence
	journal *undo.Journal
}

// writeScopeOf reads the scope of one execution off its context: the Fence is the workspace root
// the tool was built with paired with the escape permit dispatch stamped for THIS call, if any
// (a bare context yields the zero permit, so the fence is the workspace root alone), and the undo
// journal is the one the engine put in force, if any.
func writeScopeOf(ctx context.Context, root string) writeScope {
	permit, _ := domain.WriteEscapePermitFrom(ctx)
	return writeScope{
		fence:   security.Fence{Root: root, Permit: permit},
		journal: undo.FromContext(ctx),
	}
}

// target resolves the one argument the marker names into the value every later operation of this
// call goes through — the same root-only resolution the marker's own workspaceWriteTarget performs
// (resolveTargetUnbounded), so what dispatch classified, what the approval pane disclosed and what
// the write reads and lands on are one reading of the call. An empty argument is the one error:
// there is nothing to resolve, and errPathRequired is the refusal every writer spells for it.
func (s writeScope) target(input string) (writeTarget, error) {
	target, ok := resolveTargetUnbounded(input, s.fence.Root)
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
// The workspace root is the unconditional answer for every call the Fence does not govern
// (security.Fence.Governs — the ONE question the mutation root routes on, asked here of the same
// argument): with no permit, with a permit naming another path, and for every in-workspace path
// under any permit, so a call carrying one behaves identically to one that does not for every path
// inside the fence. Governs decides by RESOLUTION, which is why a workspace-spelled path that leaves
// the fence through a symlink to the permitted target IS governed. For the governed call the pair is
// repointed to the target's own parent directory, so the one name reachable through the returned
// root is the approved one (ADR 0049).
//
// absent is true when that parent is not an openable directory. The target cannot exist then, and
// the caller reports ordinary absence: pinning a root that cannot be opened would surface a fence
// refusal instead, which for a not-yet-created destination is both wrong and unexplainable.
func (t writeTarget) pin() (pinInput, pinRoot string, absent bool) {
	if !t.scope.fence.Governs(t.input) {
		return t.input, t.scope.fence.Root, false
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
	root := t.scope.fence.Root
	return notFoundOrRefusal(err, prefix, root, workspaceRelative(t.input, root), t.input)
}

// redirected reports whether the argument's OWN path went somewhere other than where it reads —
// the one comparison both halves of the disclosure (ResolvedWriteTarget, note) decide on. It
// compares Real against expected, the argument re-spelled under the root's real path, rather
// than against Named: a workspace root that is itself reached through a symlink (macOS /tmp) makes
// Real and Named differ on EVERY call, and that difference is the root's, not the argument's, so
// disclosing it would put the note on the common path the contract keeps quiet. Only a link the
// argument's path passes through of its own — a component or a final name — makes this true.
func (t writeTarget) redirected() bool {
	return t.Real != t.expected
}

// note is the RESULT-STRING half of the disclosure (ResolvedWriteTarget): the tail a write appends
// to the sentence it reports, naming where the call really landed when that is not the path the
// argument named — " → resolves to <Real>" — and "" for an ordinary call, so a result the model
// reads grows nothing on the common path. A virtual-mount reference names no host path at all, so
// there is nothing to disclose. Whether there is something to say is redirected's answer; the
// path it names is the fully resolved Real, the root's own link resolved along with the rest.
//
// Compute it BEFORE the write: the write replaces a symlinked final name with a regular file, so
// afterwards nothing is left to say that the call went somewhere else, and the sentence would part
// company with the approval pane on exactly the call worth disclosing.
func (t writeTarget) note() string {
	if _, _, isMount := virtualMountRef(t.input); isMount {
		return ""
	}
	if !t.redirected() {
		return ""
	}
	return " → resolves to " + t.Real
}

// ----------------------------------------------------------------------------
// The two write funnels (ADR 0051)
// ----------------------------------------------------------------------------
//
// Every byte a write verb lands goes through one of the two methods below, and the undo journal
// takes its pre-image inside them: the bytes a mutation is about to replace are read BEFORE it and
// recorded only AFTER it succeeded, so a refused write journals nothing. write is the funnel of the
// content verbs — write_file and the three edit tools, which hold the bytes they land — and
// journaled is its sibling for the verbs that move or remove whole files, none of which hold in
// memory the bytes they land. Because every writer reaches the filesystem through one of the two,
// that is two capture sites rather than seven.

// write lands data on this target with perm through the shared TOCTOU-safe guard: the workspace
// fence — or the approved escape's permitted target — is enforced AT WRITE TIME (os.Root-pinned), so
// a symlinked path component swapped to point outside the root — including a concurrent swap by a
// confined subprocess — is refused rather than followed (security review H1), and parent
// directories are created within the same fence.
//
// A target spelled as a virtual-mount reference is refused before anything else happens
// (refuseVirtual): those trees are read-only by construction, and resolving `shipped:…` as an
// ordinary relative name would create a colon-named file inside the workspace and report the write
// as landed.
//
// The write is the scope's Fence's (ADR 0049): a zero Permit for every ordinary call, so the
// workspace root alone bounds the write exactly as it always did, and otherwise the ONE resolved
// path the operator was shown and approved — which the Fence re-resolves the argument against
// (Governs) before anything lands.
func (t writeTarget) write(data []byte, perm os.FileMode) error {
	if err := t.refuseVirtual(); err != nil {
		return err
	}
	pre := t.capturePreImage()
	if err := t.scope.fence.WriteFile(t.input, data, perm); err != nil {
		return err
	}
	pre.commit(data, true)
	return nil
}

// postImage says how a mutated path's after-state reaches the journal once the body of a
// journaled mutation has landed it. There is deliberately no "bytes the caller already holds"
// case: that is write's shape, and a verb that holds its own post-bytes belongs there.
type postImage int

const (
	// postAbsent is the path the mutation removed — a delete's target, a move's source. Its
	// record's post-state is "nothing", which is what makes an undo put the file back.
	postAbsent postImage = iota
	// postReadBack is the path the mutation landed bytes on that this process never held — a
	// copy's or a move's destination — so the journal reads them back off the file it left.
	postReadBack
)

// journaled runs body as a mutation of this ONE target that lands or removes bytes this process
// never holds — a delete, or one end of a copy or move — through the sibling funnel
// (journaledTargets): the pre-image is captured before body runs, body is handed the scope's
// Fence (ADR 0049), and the record is committed under post only when body reports the target
// landed. body's error is returned unchanged; which verb of the Fence it calls stays body's choice.
func (t writeTarget) journaled(post postImage, body func(fence security.Fence) (landed bool, err error)) error {
	return journaledTargets(t.scope.fence, []journaledPath{t.mutation(post)}, func(fence security.Fence) ([]bool, error) {
		landed, err := body(fence)
		return []bool{landed}, err
	})
}

// journaledPath is one path a multi-path mutation touches, as the VALUE the funnel captures its
// pre-image through and reads its post-image back through, under its post-image policy.
type journaledPath struct {
	target writeTarget
	post   postImage
}

// journaledTargets is the multi-path form of journaled, and the funnel proper: copy_file, move_file
// and delete_file land bytes this process never holds, and one of them changes two paths. It takes
// the paths as the VALUES the verb resolved (writeTarget.mutation) and the Fence of the scope that
// resolved them, captures a pre-image for EVERY path before body runs, hands body that Fence (ADR
// 0049), then commits exactly the paths body reports as landed — each under its own post-image
// policy — and returns body's error unchanged. journaled is its one-target spelling; the directory
// copy and the move call it directly with the slice they assemble.
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
// The Fence verb stays the BODY's choice — Rename, CopyFileFrom and Remove differ in what they
// take and in how their failures triage — so this owns only the capture and the commit. Outside an
// engine, or under one that keeps no journal, every capture is nil and body runs byte-for-byte as
// it would have alone.
func journaledTargets(
	fence security.Fence,
	paths []journaledPath,
	body func(fence security.Fence) (landed []bool, err error),
) error {
	for _, path := range paths {
		// Every path here is one this mutation WRITES, so a virtual-mount reference is refused
		// before anything is captured: the mounts are read-only by construction, and a copy,
		// move or delete that resolved `shipped:…` as an ordinary relative name would touch a
		// colon-named file inside the workspace instead (path_virtual.go).
		if err := path.target.refuseVirtual(); err != nil {
			return err
		}
	}

	captured := make([]*preImage, len(paths))
	for i, path := range paths {
		captured[i] = path.target.capturePreImage()
	}

	landed, err := body(fence)

	for i, path := range paths {
		if i >= len(landed) || !landed[i] {
			continue
		}
		switch path.post {
		case postAbsent:
			captured[i].commit(nil, false)
		case postReadBack:
			captured[i].commitReadBack()
		}
	}
	return err
}

// mutation is this target as ONE path of a multi-path mutation (journaledTargets): the value the
// funnel captures its pre-image through and reads its post-image back through, under post. It is
// how the two-path verbs spell the value's MULTI-PATH form — a copy's directory form hands the
// funnel N destinations and a move its two ends, each resolved by the execution's own scope exactly
// as the verb resolves its destination (writeScope.target) — so those verbs assemble the slice from
// their values and keep the funnel's own body shape (one landed flag per path), where journaled
// above is the one-target case.
func (t writeTarget) mutation(post postImage) journaledPath {
	return journaledPath{target: t, post: post}
}

// ----------------------------------------------------------------------------
// The undo capture (ADR 0051)
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

// preImage is one pending journal record: what a mutation is about to replace, plus the value the
// mutation reached it through — whose read is the fence a read-back (commitReadBack) goes through —
// and the Fence the record identifies the mutation by (journalTarget): the root and permit a revert
// has to go back through to reach the same file the write reached. It exists only between the
// capture and the commit.
type preImage struct {
	target  writeTarget
	path    string
	fence   security.Fence
	data    []byte
	existed bool
	perm    os.FileMode
}

// capturePreImage reads the current bytes of the file a mutation of this target is about to
// change, through the same fence THAT mutation writes through (read, which follows an approved
// escape to its permitted target and nowhere else).
//
// It answers nil — journal nothing — in two cases: no journal is recording, or the current bytes
// could not be read for any reason OTHER than the file being absent. The second is the
// load-bearing refusal: a pre-image
// that is a guess would make a later undo destroy content rather than restore it, so an
// unreadable target is left out of the journal entirely and the write proceeds unchanged.
func (t writeTarget) capturePreImage() *preImage {
	if t.scope.journal == nil {
		return nil
	}
	path, fence := t.journalTarget()
	data, err := t.read()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil
	}
	captured := &preImage{
		target:  t,
		path:    path,
		fence:   fence,
		data:    data,
		existed: err == nil,
	}
	if captured.existed {
		captured.perm = t.perm()
	}
	return captured
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
	p.target.scope.journal.Record(undo.Mutation{
		Fence:      p.fence,
		Path:       p.path,
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
func (p *preImage) commitReadBack() {
	if p == nil {
		return
	}
	data, err := p.target.read()
	if err != nil {
		return
	}
	p.commit(data, true)
}

// journalTarget answers the pair a journal record identifies this mutation by: the absolute
// path that IS the record's identity, and the Fence a revert must go back through to reach it —
// the scope's own Fence for the approved escape, and the bare workspace fence for every ordinary
// write, so a record never claims a permit the write did not run under.
//
// The ordinary answer is the path the argument NAMES, root-joined and cleaned — not its
// symlink-resolved twin — because that is the spelling internal/security's fenced primitives
// take: they relativise it against the workspace root lexically, so a revert handed a resolved
// path would be refused as an escape on any host whose root is itself reached through a
// symlink (macOS /tmp). The approved escape is the one exception and takes the RESOLVED path,
// because that is what the permit names and what the approval pane disclosed (ADR 0049) — and
// it is recognised by exactly the question pin asks (security.Fence.Governs), so the record and
// the write route on one answer.
func (t writeTarget) journalTarget() (path string, fence security.Fence) {
	if !t.scope.fence.Governs(t.input) {
		return t.Named, security.WorkspaceFence(t.scope.fence.Root)
	}
	return t.Real, t.scope.fence
}
