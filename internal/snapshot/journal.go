package snapshot

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/airiclenz/apogee/internal/undo"
)

// storeRootName is the directory under the apogee home that holds one store per session, and
// journalFileName the index that sits beside a session's objects. Both are spelled once here
// because [Dir] is the single seam every caller composes the path through — the Driver that
// opens a journal, the sweep that removes an aged one and the session delete that removes the
// live one all name the same directory or none of them do.
const (
	storeRootName   = "snapshots"
	journalFileName = "journal.json"
)

// The three reasons OpenJournal falls back to the in-memory funnel journal of ADR 0051, plus the
// one an incomplete call yields. Each is a phrase a Driver puts in a sentence it shows the human
// — `/undo` says what it can still reach and why the wider coverage is not in force (ADR 0074
// decision 2) — so they are lower-case fragments rather than sentences of their own.
const (
	reasonDisabled  = "undo-snapshots is off"
	reasonNoGit     = "git not found"
	reasonMismatch  = "workspace mismatch"
	reasonNoHome    = "no apogee home"
	reasonNoWorkDir = "no workspace"
)

// Dir names the store directory a session's snapshots live in: `<home>/snapshots/<sessionID>`,
// outside the workspace by construction (ADR 0074 decision 1). It answers "" when either half is
// missing, which is the honest encoding of a call that has no store to name rather than a path
// under the current directory.
func Dir(home, sessionID string) string {
	if home == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(home, storeRootName, sessionID)
}

// OpenJournal is the one call a Driver makes to give a session snapshot-backed undo: it opens
// the session's own object store under home, loads whatever index the last process left beside
// it, and hands back a journal ready to capture (ADR 0074).
//
// It never fails a start. Where snapshots cannot be had — `undo-snapshots: false`, no git on
// PATH, a store that images a different workspace, or a call with no home or workspace to name
// one — it returns ADR 0051's in-memory funnel journal together with a REASON, which is a
// supported configuration rather than a degraded one and is exactly what `/undo` names in the
// same breath as what it can still reach. The reason is empty when snapshots ARE in force.
//
// An error is the one case the caller must decide about: the store could not be prepared, or the
// index on disk is unreadable at all (a truncated file, an index from a newer apogee). The
// journal is nil there, and a Driver's answer is the same fallback with the error's own text as
// the reason — a start is never lost over an undo store.
func OpenJournal(ctx context.Context, home, sessionID, workspace string, enabled bool) (*undo.Journal, string, error) {
	switch {
	case !enabled:
		return undo.New(), reasonDisabled, nil
	case home == "" || sessionID == "":
		return undo.New(), reasonNoHome, nil
	case workspace == "":
		return undo.New(), reasonNoWorkDir, nil
	case !Available():
		return undo.New(), reasonNoGit, nil
	}

	dir := Dir(home, sessionID)
	store, err := Open(ctx, dir, workspace)
	if err != nil {
		return nil, "", err
	}

	journal, err := undo.Load(filepath.Join(dir, journalFileName), journalSource{store}, workspace)
	switch {
	case errors.Is(err, undo.ErrWorkspaceMismatch):
		// The store belongs to this session but its images were taken of another tree, so every
		// recorded path names a file this run does not have. There is no revert to be had from it
		// and no error to report either — the human moved the session, which is allowed.
		return undo.New(), reasonMismatch, nil
	case err != nil:
		return nil, "", err
	}
	return journal, "", nil
}

// journalSource adapts a [Store] to [undo.Snapshotter]. The journal speaks in plain strings so it
// keeps the imports ADR 0051 gave it, and this store speaks in the [Tree] type that validates an
// id before it reaches a git argument; the adapter is the one place the two spellings meet, and
// the conversion is safe in both directions because every call the journal makes carries an id
// this store minted and each of the Store's own calls validates what it is given.
type journalSource struct{ store *Store }

func (s journalSource) Capture(ctx context.Context) (string, error) {
	tree, err := s.store.Capture(ctx)
	return tree.String(), err
}

func (s journalSource) Diff(ctx context.Context, a, b string) ([]string, error) {
	return s.store.Diff(ctx, Tree(a), Tree(b))
}

func (s journalSource) ListBlobs(ctx context.Context, tree string) (map[string]string, error) {
	return s.store.ListBlobs(ctx, Tree(tree))
}

func (s journalSource) Content(tree, path string) ([]byte, bool, error) {
	return s.store.Content(Tree(tree), path)
}
