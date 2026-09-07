package undo

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"path/filepath"

	"github.com/airiclenz/apogee/internal/security"
)

// sha256HexLen is the width, in hexadecimal characters, of an object id from a sha256 store.
// A recorded id's own width is what says which algorithm to fingerprint current bytes with,
// so a store initialised either way compares correctly without the journal being told which.
const sha256HexLen = 2 * sha256.Size

// Snapshotter is the whole-tree image source a snapshot-backed journal captures through: it
// takes an image of the workspace, says which paths differ between two images, reads an
// image out as path → blob id, and produces the bytes one image holds for one path.
//
// It is an interface, and its tree ids are plain strings, so this package keeps the imports
// ADR 0051 gave it — internal/security and the standard library — and the object store that
// implements it (internal/snapshot) depends on the journal rather than the other way round.
//
// Capture, Diff and ListBlobs take a context because they run at exchange boundaries, where
// there is one to bound them. Content does not: its callers are [Journal.Preview] and
// [Journal.Revert], whose ctx-free signatures are the human protocol of ADR 0051 decision 7,
// so an implementation bounds that call internally instead.
type Snapshotter interface {
	Capture(ctx context.Context) (string, error)
	Diff(ctx context.Context, a, b string) ([]string, error)
	ListBlobs(ctx context.Context, tree string) (map[string]string, error)
	Content(tree, path string) ([]byte, bool, error)
}

// WithSnapshotter gives the journal the image source its groups take their pre and post
// trees through, which is what extends `/undo` past the write funnel to everything that
// changed the workspace — subprocesses, MCP servers, git checkouts (ADR 0074).
//
// It needs [WithWorkspace] beside it: a tree spells its paths relative to the work-tree, and
// without that root the journal cannot say which file on disk a diff path names. A
// snapshotter given without a workspace is therefore ignored, leaving the funnel-only
// journal — the same supported fallback as a machine with no git (ADR 0074 decision 2) —
// rather than a half-wired one.
func WithSnapshotter(s Snapshotter) Option {
	return func(j *Journal) { j.snap = s }
}

// WithWorkspace names the root the snapshotter takes its images of: the workspace the
// session runs against, and the fence a revert of a diff-only path writes under. See
// [WithSnapshotter], which it pairs with.
func WithWorkspace(root string) Option {
	return func(j *Journal) { j.workspace = root }
}

// snapshots reports whether this journal is wired to take images at all. Callers hold the lock.
func (j *Journal) snapshots() bool { return j.snap != nil && j.workspace != "" }

// MarkPre captures the image of the workspace an exchange starts from, once per open group.
// The engine calls it immediately before the exchange's first write-capable tool call, which
// is why an exchange that never reaches one costs nothing (ADR 0074 decision 3).
//
// It is a no-op, reporting no error, on a journal with no snapshotter — the funnel-only
// configuration — and opening the group is not the same as materialising it: a group that
// then neither records nor diffs is dropped again by [Journal.Close], and the redo stack is
// left alone until something actually writes.
//
// A capture that FAILS is reported and leaves the group funnel-only, exactly as if no
// snapshotter were wired: `/undo` still reaches every path the funnel journals, which is the
// coverage ADR 0051 shipped. The caller's business with the error is to tell the human that
// the wider coverage is not in force, never to abandon the exchange.
func (j *Journal) MarkPre(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if !j.snapshots() {
		return nil
	}
	current := j.openGroup()
	if current.pre != "" {
		return nil
	}

	tree, err := j.snap.Capture(ctx)
	if err != nil {
		return fmt.Errorf("undo: capture the pre-image of %s: %w", j.workspace, err)
	}
	current.pre = tree
	return nil
}

// Close ends the current exchange's group: it captures the post image where a pre image was
// taken, records the diff between the two as the scope a revert may reach, and reads both
// trees out as path → blob id so a later ctx-free preview can fingerprint the files on disk
// without a context of its own.
//
// A group that recorded nothing AND diffs to nothing is dropped, so an exchange that changed
// no file never becomes a step the human has to walk past (ADR 0074 decision 3). A group
// whose two trees are identical but whose funnel DID record — the approved out-of-workspace
// write of ADR 0074 decision 9 — is kept: its paths live outside the work-tree, so the empty
// diff says nothing about them.
//
// A non-empty diff is the other place a group materialises, and it clears the redo stack for
// the same reason the first [Journal.Record] does: the exchange wrote, so re-applying an
// older tree would land on top of work the human has just asked for (ADR 0074 decision 6).
//
// A funnel-only group — no snapshotter, or a [Journal.MarkPre] that failed — is kept exactly
// as ADR 0051 kept it, with its funnel entries and nothing else. A failure of the closing
// capture or of either read is returned and leaves the group in that same state.
//
// This is where a group becomes durable: a journal given an index path ([WithIndexPath]) writes
// journal.json here, and a save that fails is returned without disturbing the closed group.
//
// EVERY path out of it saves, including the ones that end the exchange early. The exchange that
// just ran may already have discarded the redo stack in memory — [Journal.Record] clears it at its
// first record, and records nothing on disk — so a close that skipped the save would leave
// journal.json offering the next process a `/redo` this journal has thrown away, re-applying an
// old tree over the very write that discarded it (ADR 0074 decision 6). A failure of the close and
// a failure of the save are reported together, and neither disturbs the closed group.
func (j *Journal) Close(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	return errors.Join(j.closeGroup(ctx), j.persist())
}

// closeGroup is the closing work itself, with the save left to [Journal.Close] so that every way
// out of here — the four early ones included — is followed by one. Callers hold the lock.
func (j *Journal) closeGroup(ctx context.Context) error {
	if len(j.groups) == 0 {
		return nil
	}
	current := j.groups[len(j.groups)-1]
	if current.pre == "" || !j.snapshots() {
		j.dropIfEmpty(current)
		return nil
	}

	post, err := j.snap.Capture(ctx)
	if err != nil {
		j.dropIfEmpty(current)
		return fmt.Errorf("undo: capture the post-image of %s: %w", j.workspace, err)
	}
	diff, err := j.snap.Diff(ctx, current.pre, post)
	if err != nil {
		j.dropIfEmpty(current)
		return fmt.Errorf("undo: diff the exchange's images: %w", err)
	}
	if len(diff) == 0 && len(current.entries) == 0 {
		j.dropIfEmpty(current)
		return nil
	}

	preBlobs, err := j.snap.ListBlobs(ctx, current.pre)
	if err != nil {
		return fmt.Errorf("undo: read the pre-image: %w", err)
	}
	postBlobs, err := j.snap.ListBlobs(ctx, post)
	if err != nil {
		return fmt.Errorf("undo: read the post-image: %w", err)
	}

	current.post, current.preBlobs, current.postBlobs, current.touched = post, preBlobs, postBlobs, diff
	if len(diff) > 0 {
		j.redo = nil
		j.generation++
	}
	current.generation = j.generation
	return nil
}

// dropIfEmpty removes a group that never became a step — opened by [Journal.MarkPre] for an
// exchange that turned out to write nothing the journal can see. A group with funnel entries
// is a step whether or not it has trees, and stays.
//
// Dropping restores the boundary the group was opened at, so the next record opens a group
// of its own rather than joining the exchange BEFORE the dropped one. Callers hold the lock.
func (j *Journal) dropIfEmpty(g *group) {
	if len(g.entries) > 0 || len(j.groups) == 0 || j.groups[len(j.groups)-1] != g {
		return
	}
	j.groups = j.groups[:len(j.groups)-1]
	j.pending = true
}

// resolver is what a ctx-free step reads images through: the group whose trees hold them and
// the snapshotter that can fetch bytes out of those trees. A funnel-only group's resolver
// carries no snapshotter, and every read through it answers "not there" rather than failing.
type resolver struct {
	snap  Snapshotter
	group *group
}

// source builds the resolver for one group. Callers hold the lock.
func (j *Journal) source(g *group) resolver {
	return resolver{snap: j.snap, group: g}
}

// tracks reports whether this group's trees can be asked about a path at all: both images
// were taken, and the path is one a tree of the workspace spells.
func (r resolver) tracks(rel string) bool {
	return rel != "" && r.snap != nil && r.group.snapshotted()
}

// content reads the bytes one tree holds for one path, answering "not there" for a group
// with no trees rather than reaching a snapshotter it has not got.
func (r resolver) content(tree, rel string) ([]byte, bool, error) {
	if !r.tracks(rel) || tree == "" {
		return nil, false, nil
	}
	return r.snap.Content(tree, rel)
}

// reach lists every path one group's step acts on, in the order the writes happened: the
// funnel's own entries first, then the paths only the tree diff saw, in git's tree order.
//
// The two capture paths never contest a path: where both saw one file, the funnel entry
// wins, because its pre-image was read at the mutation site with no pipeline between it and
// the bytes (ADR 0074 decision 10). So the diff only ever ADDS — the subprocess, MCP and
// git-checkout writes the funnel never sees. Callers hold the lock.
func (j *Journal) reach(g *group) []reversible {
	paths := make([]reversible, 0, len(g.entries)+len(g.touched))
	for _, recorded := range g.entries {
		paths = append(paths, recorded)
	}
	if !j.snapshots() {
		return paths
	}
	for _, rel := range g.touched {
		path := filepath.Join(j.workspace, filepath.FromSlash(rel))
		if _, ok := g.index[path]; ok {
			continue
		}
		paths = append(paths, tracked{rel: rel, root: j.workspace, path: path})
	}
	return paths
}

// tracked is one path the snapshot diff added: a file the exchange changed without passing
// through the write funnel, so the only record of either of its states is the pair of trees.
// Both its images and both its fingerprints are read from there, which is why it is a
// separate kind of [reversible] rather than an entry with empty fields.
type tracked struct {
	rel  string
	root string
	path string
}

// target returns the path this record is about.
func (t tracked) target() string { return t.path }

// plan decides what this path's step would do, from the file as it is now. The conflict
// check is the same rule the funnel entries use, asked in the trees' own currency: the git
// blob id of the bytes on disk equals the recorded id exactly when nobody has touched the
// file since the capture, so a mismatch is the human's own edit and it outranks the step.
func (t tracked) plan(src resolver, d direction) Change {
	data, exists, err := readCurrent(t.root, t.path)
	if err != nil {
		return t.skip(fmt.Sprintf("cannot be read back: %v", err))
	}
	wantBlob, wantExists := src.expected(t.rel, d)
	switch {
	case exists != wantExists:
		return t.skip(existenceMismatch(exists, d))
	case exists && blobID(data, len(wantBlob)) != wantBlob:
		return t.skip(changedReason(d))
	}

	if _, ok := src.wanted(t.rel, d); !ok {
		return Change{Path: t.path, Action: ActionDelete}
	}
	return Change{Path: t.path, Action: ActionRestore}
}

// apply plans this path's step and carries it out through the same fenced primitives every
// other restore goes through, so a diff-scoped revert can reach no further than a funnel one.
func (t tracked) apply(src resolver, d direction) Change {
	planned := t.plan(src, d)

	var err error
	switch planned.Action {
	case ActionRestore:
		var data []byte
		var exists bool
		data, exists, err = src.content(src.targetTree(d), t.rel)
		switch {
		case err == nil && !exists:
			err = fmt.Errorf("the image no longer holds %s", t.rel)
		case err == nil:
			err = security.SafeWriteFile(t.root, t.path, data, defaultRestorePerm, "")
		}
	case ActionDelete:
		err = security.SafeRemove(t.root, t.path, "")
	default:
		return planned
	}
	if err != nil {
		return t.skip(fmt.Sprintf("%s failed: %v", planned.Action, err))
	}
	return planned
}

// skip builds this path's skip change with the given reason.
func (t tracked) skip(reason string) Change {
	return Change{Path: t.path, Action: ActionSkip, Reason: reason}
}

// expected is the blob id the file on disk must still hold for the step to be safe — the
// post image for an undo, the pre image for a redo — with the second return saying whether
// that image held the path at all.
func (r resolver) expected(rel string, d direction) (string, bool) {
	if d == redoward {
		id, ok := r.group.preBlobs[rel]
		return id, ok
	}
	id, ok := r.group.postBlobs[rel]
	return id, ok
}

// wanted is the blob id of the state the step puts back — the pre image for an undo, the
// post image for a redo. A path the target image does not hold is one the step deletes.
func (r resolver) wanted(rel string, d direction) (string, bool) {
	if d == redoward {
		id, ok := r.group.postBlobs[rel]
		return id, ok
	}
	id, ok := r.group.preBlobs[rel]
	return id, ok
}

// targetTree is the image a restore reads its bytes out of, the same side [resolver.wanted]
// answers from.
func (r resolver) targetTree(d direction) string {
	if d == redoward {
		return r.group.post
	}
	return r.group.pre
}

// blobID computes the git object id of a blob holding data, in the hash format the recorded
// id it will be compared against is written in — sha1 unless that id is a sha256 one. The
// header is git's own (`blob <length>\0`), which is what makes the result comparable to an
// id that came out of a tree listing without ever hashing the file through git.
func blobID(data []byte, idWidth int) string {
	var sum hash.Hash
	if idWidth == sha256HexLen {
		sum = sha256.New()
	} else {
		sum = sha1.New()
	}
	sum.Write(fmt.Appendf(nil, "blob %d\x00", len(data)))
	sum.Write(data)
	return hex.EncodeToString(sum.Sum(nil))
}
