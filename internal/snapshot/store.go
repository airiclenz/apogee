package snapshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/gitexec"
)

// snapshotTimeout bounds every git invocation the store makes. It is deliberately generous
// where the tracked-file mutation floor's is not (internal/agent/treesnapshot.go uses two
// seconds): the floor is bookkeeping on a tool call's success path and may be skipped, while a
// capture that gives up early loses the only way back from an Exchange. A whole-tree `add -A`
// over a large workspace is the slow case, and the ceiling exists to stop a wedged git rather
// than to keep the call snappy.
const snapshotTimeout = 60 * time.Second

// storeDirPerm keeps the store readable by its owner alone: it holds whole-tree images of the
// workspace, so its contents are exactly as confidential as the workspace itself.
const storeDirPerm = 0o700

// indexFileName is the private index the captures stage into, kept inside the store directory
// so no index of ours is ever written under the workspace.
const indexFileName = "index"

// headFileName is the file whose presence says the store directory already holds a repository,
// which is what makes Open reopen rather than re-initialise.
const headFileName = "HEAD"

// blobType is the git object type Content and ListBlobs keep. A tree also carries `tree`
// entries and, for a nested repository, a `commit` gitlink — neither has bytes to restore.
const blobType = "blob"

// treeIDPattern accepts a full hexadecimal object id in either of git's hash formats: 40
// characters for sha1, 64 for sha256. Abbreviated ids are refused on purpose — a tree id is
// stored, passed between processes and read back out of journal.json, and an abbreviation that
// was unique when it was written can become ambiguous once more objects land.
var treeIDPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// Tree is a validated git tree id: the whole-tree image one capture produced. It is a string
// so it can be persisted and read back verbatim, and validated at every entry point so a
// malformed id from a hand-edited journal becomes an error rather than an argument git
// interprets as something else (a ref name, an option).
type Tree string

// ParseTree validates a tree id read from outside the process — journal.json, a command line —
// and returns it as a Tree.
func ParseTree(id string) (Tree, error) {
	tree := Tree(id)
	if err := tree.validate(); err != nil {
		return "", err
	}
	return tree, nil
}

// String returns the tree id as git spells it.
func (t Tree) String() string { return string(t) }

// validate reports whether the id is a full hexadecimal object id in one of git's hash formats.
func (t Tree) validate() error {
	if !treeIDPattern.MatchString(string(t)) {
		return fmt.Errorf("apogee: snapshot: %q is not a git tree id", string(t))
	}
	return nil
}

// Store is one session's object database: a bare repository directory, the workspace it takes
// images of, and the private index the captures stage into. It holds no mutable state of its
// own, so a Store is safe to share between goroutines — git's own index lock serialises
// concurrent captures.
type Store struct {
	dir       string // the bare GIT_DIR the session owns, outside the workspace
	workspace string // the work-tree a capture stages, never written to
	index     string // the private index file, inside dir
}

// Available reports whether a git this store could use is on PATH. It is the cheap predicate a
// Driver asks before wiring snapshots at all; Open still resolves and fences git itself, so a
// true answer here is an invitation rather than a guarantee.
func Available() bool {
	_, err := gitexec.Resolve(context.Background(), "", gitexec.LookPath)
	return err == nil
}

// Open prepares dir as the object store for images of workspace, creating it (0700) and
// initialising a bare repository inside it on first use and reopening it on every later call.
// Both paths are made absolute, because they are handed to a child process as GIT_DIR and
// GIT_WORK_TREE, where a relative path would resolve against whatever directory git happens to
// run in.
//
// Nothing is written under workspace, and a dir that would sit inside it is refused rather than
// silently accepted (ADR 0074 decision 1). The repository is created with --object-format=sha1
// explicitly: the operator's own ~/.gitconfig reaches this child (HOME stays on gitexec's
// environment allowlist), so an init.defaultObjectFormat of sha256 there would otherwise decide
// the id width of every tree the session persists.
//
// It returns an error when git is missing, fenced or refused — the caller's signal to fall back
// to the in-memory funnel journal (ADR 0074 decision 2), which is a supported configuration.
func Open(ctx context.Context, dir, workspace string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("apogee: snapshot: no store directory")
	}
	if workspace == "" {
		return nil, errors.New("apogee: snapshot: no workspace directory")
	}

	storeDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("apogee: snapshot: store directory %q: %w", dir, err)
	}
	workTree, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("apogee: snapshot: workspace %q: %w", workspace, err)
	}
	if info, err := os.Stat(workTree); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("apogee: snapshot: workspace %q is not a directory", workTree)
	}
	if isInside(workTree, storeDir) {
		return nil, fmt.Errorf("apogee: snapshot: store directory %q is inside the workspace", storeDir)
	}
	if err := os.MkdirAll(storeDir, storeDirPerm); err != nil {
		return nil, fmt.Errorf("apogee: snapshot: create store directory: %w", err)
	}

	store := &Store{
		dir:       storeDir,
		workspace: workTree,
		index:     filepath.Join(storeDir, indexFileName),
	}
	if _, err := os.Stat(filepath.Join(storeDir, headFileName)); err == nil {
		return store, nil
	}
	// The init call carries GIT_DIR alone: git refuses a GIT_WORK_TREE without a repository to
	// attach it to, and the repository is what this call is creating.
	initEnv := []string{"GIT_DIR=" + storeDir}
	if _, err := gitexec.Run(ctx, workTree, initEnv, snapshotTimeout, "init", "--bare", "--object-format=sha1", "-q"); err != nil {
		return nil, fmt.Errorf("apogee: snapshot: initialise store: %w", err)
	}
	return store, nil
}

// Remove deletes a store directory and everything in it — the sweep a Driver runs when a
// session's images are no longer reachable (ADR 0074 decision 13). An empty dir is refused
// rather than treated as the current directory.
func Remove(dir string) error {
	if dir == "" {
		return errors.New("apogee: snapshot: no store directory")
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("apogee: snapshot: remove store: %w", err)
	}
	return nil
}

// Capture stages the whole work-tree into the private index and returns the id of the tree
// that results — one complete image of the workspace as it stands.
//
// The staging runs with an emptied core.excludesFile so only the workspace's own .gitignore
// files decide what is left out (ADR 0074 decision 11), and with --ignore-errors so a path git
// cannot add does not cost the rest of the tree its image. That combination makes a non-zero
// exit from `add` the NORMAL outcome for a workspace holding, say, a commit-less nested
// repository: git stages everything it could and exits 1. So the add's status is not fatal on
// its own — write-tree decides whether there is an image — and the add's error is folded into
// the failure message only when write-tree also fails, where it is the likely explanation.
func (s *Store) Capture(ctx context.Context) (Tree, error) {
	_, addErr := gitexec.Run(ctx, s.workspace, s.env(), snapshotTimeout,
		"-c", "core.excludesFile=", "add", "-A", "--ignore-errors")

	out, err := gitexec.Run(ctx, s.workspace, s.env(), snapshotTimeout, "write-tree")
	if err != nil {
		if addErr != nil {
			return "", fmt.Errorf("apogee: snapshot: capture: %w (staging: %v)", err, addErr)
		}
		return "", fmt.Errorf("apogee: snapshot: capture: %w", err)
	}
	return ParseTree(strings.TrimSpace(out))
}

// Diff returns the workspace-relative paths that differ between two captures — added, removed
// and modified alike — in git's own tree order. It is the scope a revert is confined to (ADR
// 0074 decision 4): a path outside it is not read, not written and not considered.
func (s *Store) Diff(ctx context.Context, a, b Tree) ([]string, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	if err := b.validate(); err != nil {
		return nil, err
	}

	out, err := s.stream(ctx, "diff-tree", "-r", "-z", "--name-only", a.String(), b.String())
	if err != nil {
		return nil, fmt.Errorf("apogee: snapshot: diff %s %s: %w", a, b, err)
	}
	return splitRecords(out), nil
}

// ListBlobs returns every file in one capture as path → blob id. The ids are what a conflict
// check compares against: the blob id of a file's current bytes equals its recorded id exactly
// when nobody has touched the file since the capture.
//
// Only blob entries are returned. A nested repository appears as a `commit` gitlink with no
// bytes behind it, and a symlink is a blob whose content is the link target — which is the
// right thing to record, since that is what a restore has to put back.
func (s *Store) ListBlobs(ctx context.Context, tree Tree) (map[string]string, error) {
	if err := tree.validate(); err != nil {
		return nil, err
	}

	out, err := s.stream(ctx, "ls-tree", "-r", "-z", tree.String())
	if err != nil {
		return nil, fmt.Errorf("apogee: snapshot: list %s: %w", tree, err)
	}
	blobs := make(map[string]string)
	for _, record := range splitRecords(out) {
		if path, id, ok := parseTreeEntry(record); ok {
			blobs[path] = id
		}
	}
	return blobs, nil
}

// Content returns the bytes one capture holds for a workspace-relative path. exists is false —
// with a nil error — when the capture has no blob at that path, which is the answer a revert
// reads as "this file did not exist yet, so put it back by deleting it". An error means the
// store could not be asked, and is never mistakable for absence.
//
// It takes no context because its callers are internal/undo's ctx-free Preview and Revert (ADR
// 0051 decision 7's protocol predates this store); the bounded snapshotTimeout above applies
// internally instead, so a wedged git still cannot hang an undo.
func (s *Store) Content(tree Tree, path string) (data []byte, exists bool, err error) {
	if err := tree.validate(); err != nil {
		return nil, false, err
	}
	if path == "" {
		return nil, false, errors.New("apogee: snapshot: no path")
	}
	target := filepath.ToSlash(path)

	ctx, cancel := context.WithTimeout(context.Background(), snapshotTimeout)
	defer cancel()

	// Existence is asked with a LISTING rather than by reading the blob and calling a failure
	// "absent": ls-tree exits zero whether or not the path is in the tree, so a real failure —
	// no git, a corrupt store, a timeout — stays an error instead of quietly becoming a
	// deletion. The :(literal) magic stops a path that contains glob characters from being
	// read as a pattern.
	listing, err := s.stream(ctx, "ls-tree", "-r", "-z", tree.String(), "--", ":(literal)"+target)
	if err != nil {
		return nil, false, fmt.Errorf("apogee: snapshot: look up %s in %s: %w", target, tree, err)
	}
	found := false
	for _, record := range splitRecords(listing) {
		if entryPath, _, ok := parseTreeEntry(record); ok && entryPath == target {
			found = true
			break
		}
	}
	if !found {
		return nil, false, nil
	}

	blob, err := s.stream(ctx, "cat-file", blobType, tree.String()+":"+target)
	if err != nil {
		return nil, false, fmt.Errorf("apogee: snapshot: read %s from %s: %w", target, tree, err)
	}
	return blob, true, nil
}

// env is the redirection every call carries: apogee's own object database, the workspace as its
// work-tree, and a private index inside the store. gitexec appends it to the hardened,
// allowlisted environment, so it adds a destination without weakening anything.
func (s *Store) env() []string {
	return []string{
		"GIT_DIR=" + s.dir,
		"GIT_WORK_TREE=" + s.workspace,
		"GIT_INDEX_FILE=" + s.index,
	}
}

// stream runs one read-side git command and returns its standard output UNCAPPED. The capped
// path (gitexec.Run) would silently truncate at 256 KiB, which for a blob being restored or a
// wide Exchange's path list is a corrupt answer rather than a short one.
func (s *Store) stream(ctx context.Context, args ...string) ([]byte, error) {
	var out bytes.Buffer
	if err := gitexec.RunTo(ctx, s.workspace, s.env(), snapshotTimeout, &out, args...); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// splitRecords splits git's -z output into records. NUL is the one byte a path cannot contain,
// which is why every listing here asks for it: git's default output QUOTES a path holding a
// non-ASCII byte, a quote or a backslash, and an unquoting step is a corruption waiting to
// happen. A trailing terminator yields no empty final record.
func splitRecords(out []byte) []string {
	records := strings.Split(string(out), "\x00")
	kept := make([]string, 0, len(records))
	for _, record := range records {
		if record != "" {
			kept = append(kept, record)
		}
	}
	return kept
}

// parseTreeEntry reads one `ls-tree -z` record — "<mode> <type> <object>\t<path>" — and returns
// its path and object id. ok is false for anything that is not a blob, so tree and gitlink
// entries are dropped at the one place records are read.
func parseTreeEntry(record string) (path, id string, ok bool) {
	meta, path, found := strings.Cut(record, "\t")
	if !found {
		return "", "", false
	}
	fields := strings.Fields(meta)
	if len(fields) != 3 || fields[1] != blobType {
		return "", "", false
	}
	return path, fields[2], true
}

// isInside reports whether candidate lies within root — the check that keeps a store directory
// from ever being created under the workspace it takes images of.
func isInside(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
