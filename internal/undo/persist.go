package undo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// indexVersion is the shape of the file this package writes. It is checked on the way back
// in, so a journal.json written by a later apogee is refused with a legible error rather
// than half-understood by an older one — the caller's signal to start a fresh journal.
const indexVersion = 1

// indexFilePerm keeps the index readable by its owner alone. It names the paths one
// session's exchanges touched, so it is exactly as confidential as the workspace itself and
// carries the same mode as the object store it sits inside (internal/snapshot).
const indexFilePerm os.FileMode = 0o600

// ErrWorkspaceMismatch refuses an index that images a different workspace to the one this run
// has (ADR 0074 decision 5): the recorded trees spell their paths relative to the root they
// were taken of, so reverting them against another tree would write one workspace's files over
// another's. The caller's answer is a fresh in-memory journal and a reason to say, never a
// revert; it is a sentinel so a Driver can recognise it with errors.Is.
var ErrWorkspaceMismatch = errors.New("undo: the index images a different workspace")

// Index is journal.json: the durable half of a snapshot-backed journal, written beside the
// objects in the session's own store and never inside the workspace (ADR 0074 decision 5).
//
// It holds ids, not bytes. Every state a step restores lives in the object store as a tree;
// this file only says which trees belong to which exchange, in which order, and which
// workspace they were taken of. That is what makes it small, human-readable and safe to keep:
// it is an INDEX of the store, and on its own it can restore nothing.
//
// Groups is the undo stack, oldest first; Redo is the redo stack, with the group `/redo` would
// re-apply last. Generation is the journal's state stamp, carried across the process boundary
// so a resumed session's confirmations continue the same protocol rather than restarting it.
type Index struct {
	Version    int           `json:"version"`
	Workspace  string        `json:"workspace"`
	Generation uint64        `json:"generation"`
	Groups     []GroupRecord `json:"groups"`
	Redo       []GroupRecord `json:"redo"`
}

// GroupRecord is one exchange in the [Index]: the pair of whole-tree ids taken around it,
// its 1-based position in the stack it sits on, and the journal generation it closed at.
//
// Ordinal is derived from position on the way out and checked against position on the way
// back in, which is what makes a hand-edited or truncated file an error rather than a stack
// whose steps are numbered differently to the ones the human is shown. Generation is the
// stamp the group closed at and does not change when the group moves between the stacks — it
// says when the exchange happened, not when it was last undone.
type GroupRecord struct {
	Ordinal    uint64 `json:"ordinal"`
	Generation uint64 `json:"generation"`
	Pre        string `json:"pre"`
	Post       string `json:"post"`
}

// WithIndexPath names the journal.json this journal keeps itself in, which is what makes its
// stack outlive the process (ADR 0074 decision 5). The journal writes it after every
// [Journal.Close], [Journal.Revert] and [Journal.Redo]; without the option it writes nothing
// and is exactly the in-memory journal of ADR 0051, which is a supported configuration.
//
// The path belongs beside the objects, in the session's own store directory — never inside
// the workspace, which a capture would then image.
func WithIndexPath(path string) Option {
	return func(j *Journal) { j.indexPath = path }
}

// Save writes the journal's index to path, replacing whatever was there, and leaves the
// in-memory journal untouched whether it succeeds or fails.
//
// The write is atomic: the bytes go to a temporary file in the same directory and are renamed
// over the target, so a crash or a full disk leaves either the previous index or the new one
// and never a half-written file that would read as corrupt on the next start.
//
// A group whose two tree ids are EQUAL is not written, and neither is one that has no trees at
// all. Such a group is a step only this process can take: an exchange whose only writes were
// approved out-of-workspace paths (ADR 0074 decision 9) images an unchanged workspace, and its
// pre-images live on the funnel entries in memory rather than in the store, so persisting it
// would leave the next process an empty step to walk past with nothing behind it. The same
// goes for a funnel-only group and for one whose closing capture failed.
func (j *Journal) Save(path string) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	return j.saveTo(path)
}

// saveTo encodes the index and writes it atomically. Callers hold the lock.
func (j *Journal) saveTo(path string) error {
	if path == "" {
		return errors.New("undo: save the journal index: no path")
	}

	index := Index{
		Version:    indexVersion,
		Workspace:  j.workspace,
		Generation: j.generation,
		Groups:     records(j.groups),
		Redo:       records(j.redo),
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("undo: encode the journal index: %w", err)
	}
	if err := writeAtomic(path, append(data, '\n')); err != nil {
		return fmt.Errorf("undo: save the journal index %s: %w", path, err)
	}
	return nil
}

// persist writes the index where one was configured. It is the whole of "the journal keeps
// itself": [Journal.Close], [Journal.Revert] and [Journal.Redo] each end with it, and a
// journal built without [WithIndexPath] does nothing here.
//
// A failure is RETURNED and never rolls anything back. The step the human asked for has
// already happened on disk; what failed is the record of it, and the caller's business with
// the error is to say so — the in-memory journal is still true, and the next successful save
// writes the same state again. Callers hold the lock.
func (j *Journal) persist() error {
	if j.indexPath == "" {
		return nil
	}
	return j.saveTo(j.indexPath)
}

// records renders one stack as index records, numbering only the groups that are written.
// A skipped group takes no ordinal with it, so the numbers a reloaded journal shows are the
// numbers of the steps it can actually take. Callers hold the lock.
func records(groups []*group) []GroupRecord {
	written := make([]GroupRecord, 0, len(groups))
	for _, g := range groups {
		if !g.persistable() {
			continue
		}
		written = append(written, GroupRecord{
			Ordinal:    uint64(len(written) + 1),
			Generation: g.generation,
			Pre:        g.pre,
			Post:       g.post,
		})
	}
	return written
}

// persistable reports whether a group is a step a LATER process can still take: both of its
// tree images were captured, and they differ. See [Journal.Save] for why the equal and missing
// cases stay in memory.
func (g *group) persistable() bool {
	return g.pre != "" && g.post != "" && g.pre != g.post
}

// Load reads the index at path and returns the journal it describes, wired to snap and
// workspace and already carrying [WithIndexPath] — so the journal a resumed session gets keeps
// writing the same file it came from.
//
// A MISSING file is an empty journal and no error: the session simply has not written yet.
// Every other refusal returns an error naming the path, and the caller's answer to all of them
// is the same — a fresh in-memory journal plus a reason to tell the human (ADR 0074 decision
// 2), never a silent one. [ErrWorkspaceMismatch] is the one worth recognising: it means the
// store belongs to this session but was taken of another tree.
//
// Loading MATERIALISES each recorded exchange: its diff and both trees' blob ids are read back
// out through snap, because that is what a ctx-free preview compares the files on disk against
// and this package keeps no copy of its own. Load has no context to bound those reads with —
// it runs at start-up and behind `apogee undo`, neither of which has one — so the snapshotter
// bounds them itself, as it already does for [Snapshotter.Content].
//
// The loaded journal opens a NEW group for the next write: a resumed session's first write
// belongs to its own exchange, never to the last exchange of the process before it.
func Load(path string, snap Snapshotter, workspace string) (*Journal, error) {
	loaded := New(WithSnapshotter(snap), WithWorkspace(workspace), WithIndexPath(path))

	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return loaded, nil
	case err != nil:
		return nil, fmt.Errorf("undo: read the journal index %s: %w", path, err)
	}

	var index Index
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("undo: read the journal index %s: %w", path, err)
	}
	if index.Version != indexVersion {
		return nil, fmt.Errorf("undo: read the journal index %s: version %d, not the %d this apogee writes",
			path, index.Version, indexVersion)
	}
	if filepath.Clean(index.Workspace) != filepath.Clean(workspace) {
		return nil, fmt.Errorf("%w: the index images %s, this run has %s",
			ErrWorkspaceMismatch, index.Workspace, workspace)
	}
	if snap == nil && len(index.Groups)+len(index.Redo) > 0 {
		return nil, fmt.Errorf("undo: read the journal index %s: no snapshotter to read the recorded images with", path)
	}

	groups, err := materialise(index.Groups, snap, path)
	if err != nil {
		return nil, err
	}
	redo, err := materialise(index.Redo, snap, path)
	if err != nil {
		return nil, err
	}

	loaded.groups, loaded.redo = groups, redo
	loaded.generation = index.Generation
	loaded.pending = true
	return loaded, nil
}

// materialise turns one stack's records back into groups, reading each recorded pair of trees
// out through the snapshotter. A record whose ids are equal or missing is skipped for the same
// reason [Journal.Save] never writes one — it names no step a later process can take — and the
// ordinals are checked against position, so a truncated or hand-edited file is an error rather
// than a stack numbered differently to the one the human confirms against.
func materialise(records []GroupRecord, snap Snapshotter, path string) ([]*group, error) {
	ctx := context.Background()

	groups := make([]*group, 0, len(records))
	for _, record := range records {
		if record.Pre == "" || record.Post == "" || record.Pre == record.Post {
			continue
		}
		if record.Ordinal != uint64(len(groups)+1) {
			return nil, fmt.Errorf("undo: read the journal index %s: a group is numbered %d where %d was expected",
				path, record.Ordinal, len(groups)+1)
		}

		touched, err := snap.Diff(ctx, record.Pre, record.Post)
		if err != nil {
			return nil, fmt.Errorf("undo: read the journal index %s: diff the images of exchange %d: %w",
				path, record.Ordinal, err)
		}
		preBlobs, err := snap.ListBlobs(ctx, record.Pre)
		if err != nil {
			return nil, fmt.Errorf("undo: read the journal index %s: read the pre-image of exchange %d: %w",
				path, record.Ordinal, err)
		}
		postBlobs, err := snap.ListBlobs(ctx, record.Post)
		if err != nil {
			return nil, fmt.Errorf("undo: read the journal index %s: read the post-image of exchange %d: %w",
				path, record.Ordinal, err)
		}

		groups = append(groups, &group{
			index:      make(map[string]*entry),
			generation: record.Generation,
			pre:        record.Pre,
			post:       record.Post,
			preBlobs:   preBlobs,
			postBlobs:  postBlobs,
			touched:    touched,
		})
	}
	return groups, nil
}

// writeAtomic puts data at path through a temporary file in the same directory, renamed over
// the target once it is whole and flushed. The temporary is created 0600 by os.CreateTemp and
// the mode is restated before the rename, because the renamed file's mode is the one the index
// ends up with whatever the previous file's was.
//
// A failure at any point leaves the temporary behind for the deferred removal and the target
// exactly as it was; only the rename, which is atomic on every filesystem apogee runs on, can
// change what a reader sees.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".journal-*.tmp")
	if err != nil {
		return fmt.Errorf("open a temporary file: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("flush %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	if err := os.Chmod(name, indexFilePerm); err != nil {
		return fmt.Errorf("set the mode of %s: %w", name, err)
	}
	return os.Rename(name, path)
}
