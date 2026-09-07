package undo

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/airiclenz/apogee/internal/security"
)

// defaultRestorePerm is the mode a restore creates a file with when the recorded
// mutation named none. It is the mode the write tools themselves use, so a file the
// agent deleted comes back as the write funnel would have created it.
const defaultRestorePerm os.FileMode = 0o644

// ErrNothingToUndo is returned by [Journal.Revert] when no un-undone group remains.
// It is the same condition [Journal.Preview] reports with a false second return, given
// as an error because Revert has no such channel; callers distinguish it with errors.Is
// rather than reading the message.
var ErrNothingToUndo = errors.New("undo: nothing to undo")

// ErrStaleGeneration refuses a revert whose quoted generation no longer matches the
// journal's (ADR 0051, ratified call 7): the journal moved between the preview a human
// read and the confirmation they gave, so the step they authorised is no longer the step
// that would run. It is the guard's typed refusal, defined here — beside the [Step] and
// [Report] both sides of the confirmation already speak — so a Driver can recognise it
// with errors.Is without importing the engine.
var ErrStaleGeneration = errors.New("undo: the journal moved since the preview")

// direction is which way a step runs over a group. The two directions are mirror images —
// an undo writes the pre-image and expects the post-state, a redo does the reverse — so
// every classification, ordering and wording rule is written once and reads the direction.
type direction int

const (
	// undoward puts the exchange's pre-images back: what `/undo` does.
	undoward direction = iota
	// redoward re-applies the exchange's post-images: what `/redo` does.
	redoward
)

// reversible is one path a step acts on, whichever of the two capture paths recorded it:
// a funnel [entry] with its pre-image in hand, or a [tracked] path the snapshot diff added.
type reversible interface {
	// target is the path's absolute address — the identity a preview discloses.
	target() string
	// plan says what the step would do to it, without changing anything on disk.
	plan(src resolver, d direction) Change
	// apply plans the step and carries it out, reporting what actually happened.
	apply(src resolver, d direction) Change
}

// Action is what a revert will do to one recorded path.
type Action int

const (
	// ActionRestore writes the recorded pre-image back over the file.
	ActionRestore Action = iota
	// ActionDelete removes the file, which the recorded exchange had created.
	ActionDelete
	// ActionSkip leaves the file untouched, because it no longer holds what the agent
	// wrote — the human's own edit outranks the undo — or because the revert failed.
	ActionSkip
)

// String renders the action as the verb a report uses for it.
func (a Action) String() string {
	switch a {
	case ActionRestore:
		return "restore"
	case ActionDelete:
		return "delete"
	default:
		return "skip"
	}
}

// Change is one path in a [Step]: what the revert will do to it, and — for a skip —
// the one-line reason it will not be touched.
type Change struct {
	Path   string
	Action Action
	Reason string
}

// Step is the preview of the top un-undone group: what reverting it would do, decided
// from the filesystem as it is now and without changing anything.
//
// Ordinal is the group's 1-based position counted from the oldest group still in the
// journal, so the top group's ordinal is the number of undo steps available.
// Generation is the journal's state stamp at preview time — the value a caller hands
// back to prove the journal has not moved since the human read the preview.
// Changes is in the order the writes happened.
type Step struct {
	Ordinal    int
	Generation uint64
	Changes    []Change
}

// Skipped is one path a revert left alone, with the reason it did.
type Skipped struct {
	Path   string
	Reason string
}

// Report is the outcome of a [Journal.Revert]: the paths whose pre-image was written
// back, the paths that were removed because the exchange had created them, and the
// paths left alone with the reason for each. All three lists are in the order the
// writes happened, so a report reads in the same order as the preview that announced it.
type Report struct {
	Ordinal  int
	Restored []string
	Deleted  []string
	Skipped  []Skipped
}

// Mutation is one completed filesystem mutation, as the write funnel saw it.
//
// Root is the workspace root the mutation was fenced against and Permitted is the one
// approved out-of-workspace target it carried (ADR 0049), or empty — together they let
// a revert reach exactly as far as the original write reached and no further. Path is
// the identity of the record — the thing a preview discloses and the key a group merges
// on. For an ordinary write it is the path the argument NAMED, root-joined and cleaned
// with nothing followed; only an approved escape records the permit's RESOLVED target,
// the one the approval pane disclosed (ADR 0049). Why the ordinary case must not resolve
// is stated once, at journalTarget in internal/tools — the recorder that fills this field.
//
// Pre/PreExisted are the state before the mutation; Post/PostExists the state after.
// Only Pre is kept whole — Post is reduced to a hash, since it is only ever compared.
// Perm is the mode a restore creates the file with when it no longer exists; zero means
// the package default.
//
// A Mutation is recorded only after the mutation SUCCEEDED: a failed write leaves the
// file as it was, so a record of it would claim a change that never happened.
type Mutation struct {
	Root       string
	Path       string
	Permitted  string
	Perm       os.FileMode
	Pre        []byte
	PreExisted bool
	Post       []byte
	PostExists bool
}

// entry is one path's record inside a group: the first pre-image seen for that path and
// the latest post-state, plus the fencing context a revert needs to write it back.
//
// rel is the same path as the tree that images this group spells it — workspace-relative
// and slash-separated — or empty for a path outside the workspace, which no tree holds.
// post carries the whole post-image, but only where nothing else can: a group with no
// snapshot has no tree to read the bytes back out of, and neither has an approved escape
// (ADR 0074 decision 9), so those keep their bytes here and every other entry keeps the
// hash alone, exactly as before. postKept says which of the two this entry is.
type entry struct {
	root       string
	readRoot   string
	permitted  string
	path       string
	rel        string
	perm       os.FileMode
	pre        []byte
	preExisted bool
	post       []byte
	postKept   bool
	postHash   [sha256.Size]byte
	postExists bool
}

// group is one exchange's worth of records: insertion-ordered, one entry per path.
//
// pre and post are the ids of the whole-tree images taken around the exchange, empty on a
// funnel-only group (no Snapshotter, or a capture that failed). preBlobs and postBlobs are
// those trees read out as path → blob id, which is what a ctx-free conflict check compares
// the bytes on disk against; touched is the diff between the two trees, workspace-relative
// and in git's own order, and it is the whole scope a revert of this group may reach beyond
// its funnel entries (ADR 0074 decision 4).
//
// generation is the journal's state stamp at the moment the group closed. Nothing in a step
// reads it; it is carried into journal.json ([GroupRecord]) so a reloaded stack still says
// which generation each of its exchanges was taken at, and it does not change when the group
// moves between the undo and redo stacks.
type group struct {
	entries    []*entry
	index      map[string]*entry
	generation uint64
	pre        string
	post       string
	preBlobs   map[string]string
	postBlobs  map[string]string
	touched    []string
}

// snapshotted reports whether both of this group's images were taken, which is what makes
// its trees readable — a group with a pre but no post is one whose closing capture failed.
func (g *group) snapshotted() bool { return g.pre != "" && g.post != "" }

// Journal is the ordered stack of per-exchange record groups behind `/undo`.
//
// It is safe for concurrent use — delegated sub-agents record into their parent's
// journal from their own goroutines (ADR 0039) — and every method takes the same lock,
// so a preview or a revert never observes a half-written group.
//
// Without a [Snapshotter] the stack holds this process's own funnel records, in memory and
// nowhere else, exactly as ADR 0051 built it. With one ([WithSnapshotter] and [WithWorkspace]
// together) each group also carries the pair of whole-tree images taken around its exchange,
// which is what lets a revert reach the writes the funnel never saw and what gives `/redo`
// something to re-apply (ADR 0074).
// Reverted groups move to a redo stack, which the next exchange that writes clears. Given an
// index path beside those images ([WithIndexPath]) the stack outlives the process too: the
// journal writes journal.json after every close, revert and redo, and [Load] reads it back.
//
// The zero value is not usable; call [New].
type Journal struct {
	mu         sync.Mutex
	groups     []*group
	redo       []*group
	pending    bool
	generation uint64
	snap       Snapshotter
	workspace  string
	indexPath  string
}

// Option configures a [Journal] at construction. The options are independent of one another
// except that snapshots need both of them: see [WithSnapshotter].
type Option func(*Journal)

// New returns an empty journal configured by opts. New() with no options is the in-memory,
// funnel-only journal of ADR 0051 — the supported configuration wherever git is missing or
// `undo-snapshots` is off (ADR 0074 decision 2).
func New(opts ...Option) *Journal {
	j := &Journal{}
	for _, opt := range opts {
		opt(j)
	}
	return j
}

// BeginGroup marks the boundary of a new exchange. It does NOT create a group: the
// group materializes on the first [Journal.Record] after this call, so an exchange that
// writes nothing never becomes an undo step the human has to step past. Calling it
// repeatedly with no write in between is therefore the same as calling it once.
func (j *Journal) BeginGroup() {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.pending = true
}

// Record adds one completed mutation to the current group, opening a group first when
// a [Journal.BeginGroup] is outstanding or the journal is empty — a record is never lost
// for want of a boundary call.
//
// Within a group there is exactly ONE entry per path: the FIRST pre-image wins (it is
// the state the exchange started from, which is what an undo must restore) and the LAST
// post-state wins (it is what the file must still hold for the undo to be safe). The
// entry keeps the position of its first record, so groups stay in the order the writes
// happened.
//
// A Mutation with an empty Path is ignored; the funnel never produces one.
func (j *Journal) Record(m Mutation) {
	if m.Path == "" {
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	current := j.openGroup()
	path := filepath.Clean(m.Path)
	j.generation++

	// The FIRST record is where the group stops being a boundary and becomes a step, and
	// that is the moment a redo of anything older would re-apply an old tree over work the
	// human has just asked for (ADR 0074 decision 6). A group opened by MarkPre is not yet
	// materialised, so the test is the entry count, not the group's existence.
	if len(current.entries) == 0 {
		j.redo = nil
	}

	if merged, ok := current.index[path]; ok {
		merged.post, merged.postKept = keptPost(current, merged.rel, m.Post, m.PostExists)
		merged.postHash, merged.postExists = hashOf(m.Post, m.PostExists)
		return
	}

	recorded := &entry{
		root:       m.Root,
		readRoot:   readBackRoot(m.Root, path),
		permitted:  m.Permitted,
		path:       path,
		rel:        treePath(m.Root, path),
		perm:       restorePerm(m.Perm),
		pre:        append([]byte(nil), m.Pre...),
		preExisted: m.PreExisted,
	}
	recorded.post, recorded.postKept = keptPost(current, recorded.rel, m.Post, m.PostExists)
	recorded.postHash, recorded.postExists = hashOf(m.Post, m.PostExists)

	current.index[path] = recorded
	current.entries = append(current.entries, recorded)
}

// openGroup returns the group records land in, opening one when a [Journal.BeginGroup]
// boundary is outstanding or the journal is empty — a record is never lost for want of a
// boundary call, and a pre-image capture has a group to hang itself on. It does not
// materialise the group as a step: an opened group that neither records nor diffs is
// dropped again by [Journal.Close]. Callers hold the lock.
func (j *Journal) openGroup() *group {
	if j.pending || len(j.groups) == 0 {
		j.groups = append(j.groups, &group{index: make(map[string]*entry)})
		j.pending = false
	}
	return j.groups[len(j.groups)-1]
}

// Generation returns the journal's state stamp. It changes on every record and on every
// revert, and it is the whole of the staleness protocol: a caller that showed a human a
// preview passes the generation it read back with the confirmation, and a mismatch means
// the journal moved under the human and the preview they answered is no longer true.
func (j *Journal) Generation() uint64 {
	j.mu.Lock()
	defer j.mu.Unlock()

	return j.generation
}

// Preview describes what reverting the top group would do, without changing anything on
// disk. It reports false when the journal holds nothing to undo.
//
// Each recorded path is classified against the file as it is NOW: restore when the
// exchange replaced or deleted existing content, delete when the exchange created the
// file, and skip when the file no longer matches what the agent left — a hash mismatch,
// a file that has since been deleted, or one that has since reappeared. Paths are the
// journal's recorded absolute addresses — a root-joined named path for an ordinary write,
// the permit-pinned resolved target for an approved escape — which is what makes the
// preview the disclosure surface the human authorises the revert from.
func (j *Journal) Preview() (Step, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if len(j.groups) == 0 {
		return Step{}, false
	}
	top := j.groups[len(j.groups)-1]

	return j.previewOf(top, len(j.groups), undoward), true
}

// previewOf classifies every path one group's step would act on, in the order the writes
// happened, and stamps the journal's current generation on the result. Callers hold the lock.
func (j *Journal) previewOf(g *group, ordinal int, d direction) Step {
	src := j.source(g)
	paths := j.reach(g)

	changes := make([]Change, 0, len(paths))
	for _, target := range paths {
		changes = append(changes, target.plan(src, d))
	}
	return Step{Ordinal: ordinal, Generation: j.generation, Changes: changes}
}

// runStep carries one group's step out over every path it reaches and reports what it did.
// An undo runs in the REVERSE of the order the writes happened, so a path created after
// another is removed before it; a redo runs forward, the order the writes themselves took.
// The report is in write order either way, so it reads like the preview that announced it.
// Callers hold the lock.
func (j *Journal) runStep(g *group, ordinal int, d direction) Report {
	src := j.source(g)
	paths := j.reach(g)

	outcomes := make([]Change, len(paths))
	if d == undoward {
		for i := len(paths) - 1; i >= 0; i-- {
			outcomes[i] = paths[i].apply(src, d)
		}
	} else {
		for i := range paths {
			outcomes[i] = paths[i].apply(src, d)
		}
	}

	report := Report{Ordinal: ordinal}
	for _, outcome := range outcomes {
		switch outcome.Action {
		case ActionRestore:
			report.Restored = append(report.Restored, outcome.Path)
		case ActionDelete:
			report.Deleted = append(report.Deleted, outcome.Path)
		default:
			report.Skipped = append(report.Skipped, Skipped{Path: outcome.Path, Reason: outcome.Reason})
		}
	}
	return report
}

// Revert executes the top group and pops it, returning what it did. It reverts in the
// REVERSE of the order the writes happened, so a path created after another is removed
// before it, and it applies the same conflict rule [Journal.Preview] previews: a file
// that no longer holds what the agent left is skipped with its reason rather than
// overwritten. A path whose restore or removal FAILS is reported the same way, so a
// partial revert is a full report rather than a lost one.
//
// A journal given an index path writes it before returning, and a save that fails is reported
// alongside the report it could not record: the revert itself stands (see [Journal.persist]).
//
// The group is popped whether or not every path was reverted — skipped paths are not
// retried by a later undo, which would otherwise revive an edit the human made on
// purpose. Reverting also closes the current group: the next record starts a new one.
// The popped group moves to the redo stack, where [Journal.Redo] can put it back until
// the next exchange that writes clears it.
//
// It returns [ErrNothingToUndo], and does nothing, when no group remains.
func (j *Journal) Revert() (Report, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if len(j.groups) == 0 {
		return Report{}, ErrNothingToUndo
	}
	top := j.groups[len(j.groups)-1]

	report := j.runStep(top, len(j.groups), undoward)

	j.groups = j.groups[:len(j.groups)-1]
	j.redo = append(j.redo, top)
	j.pending = true
	j.generation++
	return report, j.persist()
}

// Wrote lists every path this journal has a record for, across ALL groups and in the order
// each path was FIRST written, with no path repeated. It is the whole run's account of what
// was changed on disk — deletes and move sources included, since the funnel journals those
// too — where [Journal.Preview] describes only the top un-undone group.
//
// It is a REPORT, not a handle: paths only, no generation and no [Change], so nothing can
// revert from it. It reads no file and hashes nothing — the classification Preview pays for
// is a human's price at `/undo`, not one an unattended run may be made to pay — so the paths
// are the journal's recorded addresses whether or not they still exist. An empty slice means
// nothing was recorded.
func (j *Journal) Wrote() []string {
	j.mu.Lock()
	defer j.mu.Unlock()

	paths := make([]string, 0, len(j.groups))
	seen := make(map[string]struct{})
	for _, recorded := range j.groups {
		for _, target := range j.reach(recorded) {
			path := target.target()
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			paths = append(paths, path)
		}
	}
	return paths
}

// plan decides what this entry's step would do, from the file as it is now. Undoing
// restores the pre-image and expects the file to still hold the post-state; redoing swaps
// the two, and additionally needs a post-image to write — which an entry whose group lost
// its closing capture has neither in hand nor in a tree.
func (e *entry) plan(src resolver, d direction) Change {
	hash, exists, err := e.currentState()
	wantHash, wantExists := e.expected(d)
	switch {
	case err != nil:
		return e.skip(fmt.Sprintf("cannot be read back: %v", err))
	case exists != wantExists:
		return e.skip(existenceMismatch(exists, d))
	case exists && hash != wantHash:
		return e.skip(changedReason(d))
	}

	if !e.wantsFile(d) {
		return Change{Path: e.path, Action: ActionDelete}
	}
	if d == redoward && !e.hasPostImage(src) {
		return e.skip(noPostImage)
	}
	return Change{Path: e.path, Action: ActionRestore}
}

// apply plans this entry's step and carries the result out, through the same fenced
// primitives the original write went through. A primitive that refuses — an escaping
// symlink swapped in since, a directory gone read-only — turns the change into a skip
// carrying the refusal, because that is what the caller has to tell the human.
func (e *entry) apply(src resolver, d direction) Change {
	planned := e.plan(src, d)

	var err error
	switch planned.Action {
	case ActionRestore:
		var data []byte
		data, err = e.image(src, d)
		if err == nil {
			err = security.SafeWriteFile(e.root, e.path, data, e.perm, e.permitted)
		}
	case ActionDelete:
		err = security.SafeRemove(e.root, e.path, e.permitted)
	default:
		return planned
	}
	if err != nil {
		return e.skip(fmt.Sprintf("%s failed: %v", planned.Action, err))
	}
	return planned
}

// target returns the path this entry is the record of.
func (e *entry) target() string { return e.path }

// expected is the state the file must still be in for the step to be safe: the post-state
// for an undo (what the agent left), the pre-image for a redo (what the undo put back).
func (e *entry) expected(d direction) ([sha256.Size]byte, bool) {
	if d == redoward {
		return hashOf(e.pre, e.preExisted)
	}
	return e.postHash, e.postExists
}

// wantsFile reports whether the step's target state has the file existing at all — the
// pre-image for an undo, the post-state for a redo. False means the step is a deletion.
func (e *entry) wantsFile(d direction) bool {
	if d == redoward {
		return e.postExists
	}
	return e.preExisted
}

// hasPostImage reports whether the bytes the agent left can still be produced: kept whole
// on the entry, or readable out of the group's post tree at this entry's tree path.
func (e *entry) hasPostImage(src resolver) bool {
	if e.postKept {
		return true
	}
	if !src.tracks(e.rel) {
		return false
	}
	_, ok := src.group.postBlobs[e.rel]
	return ok
}

// image produces the bytes the step writes: the pre-image for an undo, always in hand, and
// for a redo the post-image from wherever this entry keeps it.
func (e *entry) image(src resolver, d direction) ([]byte, error) {
	if d == undoward {
		return e.pre, nil
	}
	if e.postKept {
		return e.post, nil
	}
	data, exists, err := src.content(src.group.post, e.rel)
	switch {
	case err != nil:
		return nil, err
	case !exists:
		return nil, errors.New(noPostImage)
	}
	return data, nil
}

// currentState hashes the file as it is now, fenced by the same root the mutation was.
//
// A fence root that will not open means the directory chain above the target is gone or
// unreachable, so the recorded file is not there to match either: it is reported absent
// and the post-state comparison decides. That errs toward SKIPPING (an absent file
// mismatches a post-state that exists) or toward a restore that the fenced primitive
// will refuse on its own — never toward writing over something unexamined.
func (e *entry) currentState() ([sha256.Size]byte, bool, error) {
	data, exists, err := readCurrent(e.readRoot, e.path)
	switch {
	case err != nil:
		return [sha256.Size]byte{}, false, err
	case !exists:
		return [sha256.Size]byte{}, false, nil
	}
	return sha256.Sum256(data), true, nil
}

// skip builds this entry's skip change with the given reason.
func (e *entry) skip(reason string) Change {
	return Change{Path: e.path, Action: ActionSkip, Reason: reason}
}

// noPostImage is the reason a redo gives for a path whose post-image is not recoverable:
// the entry did not keep the bytes (its group was snapshot-backed at record time) and the
// group's post tree has none either, because the closing capture failed or git's own add
// pipeline left the path out (ADR 0074 decision 12).
const noPostImage = "no post-image recorded"

// existenceMismatch words the conflict where the file's mere presence disagrees with the
// state the step expects, which is a different story to tell than a content change. An undo
// speaks of what the agent did; a redo of what the undo just did.
func existenceMismatch(exists bool, d direction) string {
	switch {
	case d == redoward && exists:
		return "recreated since the undo removed it"
	case d == redoward:
		return "deleted since the undo restored it"
	case exists:
		return "recreated since the agent removed it"
	default:
		return "deleted since the agent wrote it"
	}
}

// changedReason words the conflict where the bytes on disk are no longer the ones the step
// expects to find — the human's own edit, which outranks both directions of the step.
func changedReason(d direction) string {
	if d == redoward {
		return "changed since the undo restored it"
	}
	return "changed since the agent wrote it"
}

// readCurrent reads the file as it is now through the fence the mutation ran under, mapping
// "not there" — and a fence root that will not open — to absence rather than to an error, so
// the caller's state comparison decides instead of the read.
func readCurrent(root, path string) ([]byte, bool, error) {
	data, err := security.SafeReadFile(root, path)
	switch {
	case err == nil:
		return data, true, nil
	case errors.Is(err, os.ErrNotExist), errors.Is(err, security.ErrRootInaccessible):
		return nil, false, nil
	default:
		return nil, false, err
	}
}

// keptPost answers whether one record keeps its post-image whole, and the copy it keeps.
// Only a path no tree will hold does: a group with no pre-image capture has no trees at
// all, and an approved out-of-workspace target is outside the work-tree of the ones it has
// (ADR 0074 decision 9). Everything else reads its post bytes back out of the tree.
func keptPost(g *group, rel string, post []byte, exists bool) ([]byte, bool) {
	if !exists || (g.pre != "" && rel != "") {
		return nil, false
	}
	return append([]byte(nil), post...), true
}

// treePath spells an absolute recorded path the way a tree of the workspace does —
// workspace-relative and slash-separated — or empty for a path outside the workspace,
// which no tree of it holds.
func treePath(root, path string) string {
	if !isInside(root, path) {
		return ""
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

// hashOf reduces a mutation's post-image to what a conflict check needs.
func hashOf(data []byte, exists bool) ([sha256.Size]byte, bool) {
	if !exists {
		return [sha256.Size]byte{}, false
	}
	return sha256.Sum256(data), true
}

// restorePerm answers the mode a restore creates an absent file with.
func restorePerm(perm os.FileMode) os.FileMode {
	if perm == 0 {
		return defaultRestorePerm
	}
	return perm
}

// readBackRoot answers the root a later read-back of path is fenced against: the
// workspace root for a path inside it, and the path's own parent directory for an
// approved out-of-workspace target (ADR 0049). The second case exists because permits
// are write-side only — internal/security's read primitives take none by design — so an
// approved escape is re-fenced as tightly as the write itself was, at the target's own
// parent, which exists at the moment the mutation being recorded succeeded.
func readBackRoot(root, path string) string {
	if isInside(root, path) {
		return root
	}
	return filepath.Dir(path)
}

// isInside reports whether path lies within root, lexically.
func isInside(root, path string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
