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
type entry struct {
	root       string
	readRoot   string
	permitted  string
	path       string
	perm       os.FileMode
	pre        []byte
	preExisted bool
	postHash   [sha256.Size]byte
	postExists bool
}

// group is one exchange's worth of records: insertion-ordered, one entry per path.
type group struct {
	entries []*entry
	index   map[string]*entry
}

// Journal is the ordered stack of per-exchange record groups behind `/undo`.
//
// It is safe for concurrent use — delegated sub-agents record into their parent's
// journal from their own goroutines (ADR 0039) — and every method takes the same lock,
// so a preview or a revert never observes a half-written group.
//
// The stack is per process and in memory only: nothing is written to disk, and a new
// process starts with an empty journal. The zero value is not usable; call [New].
type Journal struct {
	mu         sync.Mutex
	groups     []*group
	pending    bool
	generation uint64
}

// New returns an empty journal.
func New() *Journal {
	return &Journal{}
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

	if j.pending || len(j.groups) == 0 {
		j.groups = append(j.groups, &group{index: make(map[string]*entry)})
		j.pending = false
	}
	current := j.groups[len(j.groups)-1]
	path := filepath.Clean(m.Path)
	j.generation++

	if merged, ok := current.index[path]; ok {
		merged.postHash, merged.postExists = hashOf(m.Post, m.PostExists)
		return
	}

	recorded := &entry{
		root:       m.Root,
		readRoot:   readBackRoot(m.Root, path),
		permitted:  m.Permitted,
		path:       path,
		perm:       restorePerm(m.Perm),
		pre:        append([]byte(nil), m.Pre...),
		preExisted: m.PreExisted,
	}
	recorded.postHash, recorded.postExists = hashOf(m.Post, m.PostExists)

	current.index[path] = recorded
	current.entries = append(current.entries, recorded)
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

	changes := make([]Change, 0, len(top.entries))
	for _, recorded := range top.entries {
		changes = append(changes, recorded.classify())
	}
	return Step{Ordinal: len(j.groups), Generation: j.generation, Changes: changes}, true
}

// Revert executes the top group and pops it, returning what it did. It reverts in the
// REVERSE of the order the writes happened, so a path created after another is removed
// before it, and it applies the same conflict rule [Journal.Preview] previews: a file
// that no longer holds what the agent left is skipped with its reason rather than
// overwritten. A path whose restore or removal FAILS is reported the same way, so a
// partial revert is a full report rather than a lost one.
//
// The group is popped whether or not every path was reverted — skipped paths are not
// retried by a later undo, which would otherwise revive an edit the human made on
// purpose. Reverting also closes the current group: the next record starts a new one.
//
// It returns [ErrNothingToUndo], and does nothing, when no group remains.
func (j *Journal) Revert() (Report, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if len(j.groups) == 0 {
		return Report{}, ErrNothingToUndo
	}
	top := j.groups[len(j.groups)-1]

	outcomes := make([]Change, len(top.entries))
	for i := len(top.entries) - 1; i >= 0; i-- {
		outcomes[i] = top.entries[i].revert()
	}

	report := Report{Ordinal: len(j.groups)}
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

	j.groups = j.groups[:len(j.groups)-1]
	j.pending = true
	j.generation++
	return report, nil
}

// classify decides what this entry's revert would do, from the file as it is now.
func (e *entry) classify() Change {
	hash, exists, err := e.currentState()
	switch {
	case err != nil:
		return e.skip(fmt.Sprintf("cannot be read back: %v", err))
	case exists != e.postExists:
		return e.skip(e.existenceMismatch(exists))
	case exists && hash != e.postHash:
		return e.skip("changed since the agent wrote it")
	case e.preExisted:
		return Change{Path: e.path, Action: ActionRestore}
	default:
		return Change{Path: e.path, Action: ActionDelete}
	}
}

// revert classifies this entry and carries the result out, through the same fenced
// primitives the original write went through. A primitive that refuses — an escaping
// symlink swapped in since, a directory gone read-only — turns the change into a skip
// carrying the refusal, because that is what the caller has to tell the human.
func (e *entry) revert() Change {
	planned := e.classify()

	var err error
	switch planned.Action {
	case ActionRestore:
		err = security.SafeWriteFile(e.root, e.path, e.pre, e.perm, e.permitted)
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

// currentState hashes the file as it is now, fenced by the same root the mutation was.
//
// A fence root that will not open means the directory chain above the target is gone or
// unreachable, so the recorded file is not there to match either: it is reported absent
// and the post-state comparison decides. That errs toward SKIPPING (an absent file
// mismatches a post-state that exists) or toward a restore that the fenced primitive
// will refuse on its own — never toward writing over something unexamined.
func (e *entry) currentState() ([sha256.Size]byte, bool, error) {
	data, err := security.SafeReadFile(e.readRoot, e.path)
	switch {
	case err == nil:
		return sha256.Sum256(data), true, nil
	case errors.Is(err, os.ErrNotExist), errors.Is(err, security.ErrRootInaccessible):
		return [sha256.Size]byte{}, false, nil
	default:
		return [sha256.Size]byte{}, false, err
	}
}

// existenceMismatch words the conflict where the file's mere presence disagrees with
// what the agent left, which is a different story to tell than a content change.
func (e *entry) existenceMismatch(exists bool) string {
	if exists {
		return "recreated since the agent removed it"
	}
	return "deleted since the agent wrote it"
}

// skip builds this entry's skip change with the given reason.
func (e *entry) skip(reason string) Change {
	return Change{Path: e.path, Action: ActionSkip, Reason: reason}
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
