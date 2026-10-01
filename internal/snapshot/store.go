package snapshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/gitexec"
	"github.com/airiclenz/apogee/internal/subprocess"
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

// unreadDirName is the directory inside the store that records, per tree, the paths its capture
// could not read (see [Store.Capture]); unreadFilePerm is each record's mode, owner-only like
// everything else the store holds, since a record names workspace paths.
const (
	unreadDirName  = "unread"
	unreadFilePerm = 0o600
)

// cLocale runs a capture's add in git's untranslated messages. The warning a capture reads for a
// directory git could not open is recognisable only in the C locale, and the allowlisted
// environment passes the operator's LANG and LC_ALL through, which would translate it.
const cLocale = "LC_ALL=C"

// openWarningPrefix and openWarningEnd bracket the path in the warning git prints for each
// directory its walk could not open — "warning: could not open directory 'dir/': Permission
// denied" — which is the only word git gives about such a directory: the add stages the rest and
// exits zero.
const (
	openWarningPrefix = "warning: could not open directory '"
	openWarningEnd    = "': "
)

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
// concurrent captures, and the per-tree record of paths a capture could not read is only ever
// appended to.
type Store struct {
	dir       string // the bare GIT_DIR the session owns, outside the workspace
	workspace string // the work-tree a capture stages, never written to
	index     string // the private index file, inside dir
}

// Available reports whether a git this store could use is on PATH. It is the cheap predicate a
// Driver asks before wiring snapshots at all; Open still resolves and fences git itself, so a
// true answer here is an invitation rather than a guarantee.
func Available() bool {
	_, err := gitexec.OS().Resolve(context.Background(), "")
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
//
// A failed add is not harmless to the persistent index, though: a path git could not read this
// time keeps whatever entry an EARLIER capture staged for it, and write-tree would freeze that
// stale content into the image as if the file still held it. So when the add reports an error
// the image is staged again into a fresh, throwaway index, where a path that fails is simply
// absent — the residue ADR 0074 decision 12 hands to the funnel journal — and never its old
// bytes. The persistent index is left alone rather than deleted, so a capture running beside
// this one never sees it vanish between its own add and write-tree.
//
// An add that exits zero can still have left paths out: a directory git could not open, and an
// entry already in the index that git could not even stat, are only warned about. Such a capture
// is settled from those warnings instead (see [Store.settleWarnings]), so no exit status is the
// sole judge of whether an image is whole.
//
// Absent is not the same as "did not exist", and the image alone cannot tell them apart: a file
// this capture could not read, made readable by the exchange, is in the next image and would
// diff as created — a revert would delete a file that was there all along. So the paths the
// capture could not stage are recorded beside the objects, keyed by the tree, and [Store.Diff]
// leaves them out of every diff that tree takes part in. A capture that cannot write that record
// fails rather than return an image whose absences read as deletions.
func (s *Store) Capture(ctx context.Context) (Tree, error) {
	out, warnings, addErr, err := s.stageAndWrite(ctx, s.index)
	var unread []string
	switch {
	case err == nil && addErr != nil:
		out, unread, addErr, err = s.captureFresh(ctx)
	case err == nil && warnings != "":
		out, unread, err = s.settleWarnings(ctx, out, warnings)
	}
	if err != nil {
		if addErr != nil {
			return "", fmt.Errorf("apogee: snapshot: capture: %w (staging: %v)", err, addErr)
		}
		return "", fmt.Errorf("apogee: snapshot: capture: %w", err)
	}
	tree, err := ParseTree(strings.TrimSpace(out))
	if err != nil {
		return "", err
	}
	if err := s.recordUnread(tree, unread); err != nil {
		return "", fmt.Errorf("apogee: snapshot: capture: record the paths it could not read: %w", err)
	}
	return tree, nil
}

// captureFresh stages the work-tree into a new, empty index in a scratch directory inside the
// store and writes the tree from it, so no entry from an earlier capture can reach the image.
// When that add fails too, unread lists what it left out: every path git still reports as
// untracked against the fresh index, which — since the add was asked to stage them all under
// the same excludes — is exactly what it could not read, plus every directory git could not
// open, which it only warns about and so lists nowhere. That list comes from a walk rather than
// from the add's warnings: a failed add may have died partway through its own walk (a directory
// it can list but not search is fatal), so its warnings are not a complete account. When the
// fresh add succeeds, its walk finished and its warnings are. The scratch directory goes with
// the call.
func (s *Store) captureFresh(ctx context.Context) (out string, unread []string, addErr, err error) {
	scratch, err := os.MkdirTemp(s.dir, "rebuild-")
	if err != nil {
		return "", nil, nil, fmt.Errorf("fresh index: %w", err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	index := filepath.Join(scratch, indexFileName)
	out, warnings, addErr, err := s.stageAndWrite(ctx, index)
	if err != nil {
		return out, nil, addErr, err
	}
	if addErr == nil {
		dirs, err := unopenedDirs(s.workspace, warnings)
		if err != nil {
			return "", nil, nil, fmt.Errorf("list the directories the fresh index left out: %w", err)
		}
		return out, dirs, nil, nil
	}
	listing, err := s.streamWith(ctx, s.envWithIndex(index),
		"-c", "core.excludesFile=", "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return "", nil, addErr, fmt.Errorf("list the paths the fresh index left out: %w", err)
	}
	dirs, err := unopenableDirs(s.workspace)
	if err != nil {
		return "", nil, addErr, fmt.Errorf("list the directories the fresh index left out: %w", err)
	}
	return out, append(splitRecords(listing), dirs...), addErr, nil
}

// settleWarnings finishes a capture whose add into the persistent index exited zero but printed
// something, and returns the tree to keep and the paths to record as unread. Two warnings mean
// the image is not whole. A directory git could not open leaves every file inside it that the
// index did not already hold out of the image, unnamed. And an entry the index DID hold that git
// could not stat — inside that directory, or under one that lost only its search bit — is kept
// at whatever an earlier capture staged, which write-tree would freeze as the file's content:
// the stale image the fresh index exists to prevent. So every entry git cannot stat is dropped
// from the persistent index and the tree written again, leaving the path absent rather than
// stale, and both kinds are recorded. Any other warning — a line-ending conversion, an embedded
// repository — costs one stat pass over the index and records nothing.
func (s *Store) settleWarnings(ctx context.Context, out, warnings string) (string, []string, error) {
	listing, err := s.stream(ctx, "ls-files", "-z", "--deleted")
	if err != nil {
		return "", nil, fmt.Errorf("list the index entries git could not stat: %w", err)
	}
	stale := splitRecords(listing)
	if len(stale) > 0 {
		if err := s.dropFromIndex(ctx, stale); err != nil {
			return "", nil, fmt.Errorf("drop the index entries git could not stat: %w", err)
		}
		if out, err = gitexec.Run(ctx, s.workspace, s.env(), snapshotTimeout, "write-tree"); err != nil {
			return "", nil, err
		}
	}
	dirs, err := unopenedDirs(s.workspace, warnings)
	if err != nil {
		return "", nil, fmt.Errorf("list the directories the capture could not open: %w", err)
	}
	return out, append(stale, dirs...), nil
}

// dropFromIndex removes paths from the persistent index and touches no work-tree file. The paths
// reach git through a NUL-separated pathspec file inside the store rather than argv, which a wide
// directory could overflow, and --literal-pathspecs stops a path holding glob characters from
// matching its neighbours. --force skips rm's up-to-date check, which compares against a HEAD
// this store never has.
func (s *Store) dropFromIndex(ctx context.Context, paths []string) error {
	file, err := os.CreateTemp(s.dir, "drop-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	_, writeErr := file.WriteString(strings.Join(paths, "\x00") + "\x00")
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	_, err = gitexec.Run(ctx, s.workspace, s.env(), snapshotTimeout, "--literal-pathspecs",
		"rm", "--cached", "--force", "--quiet", "--ignore-unmatch",
		"--pathspec-from-file="+file.Name(), "--pathspec-file-nul")
	return err
}

// unopenedDirs returns the directories an add's warnings say it could not open, workspace-
// relative with the trailing slash that makes an unread record cover everything inside them.
// The warning is a message for a human rather than a listing, though: git turns a control
// character in a path into '?', lets a newline through to split the line, and caps both the line
// and the stream. So a path is taken from it only while the account is whole — the stream was
// not cut at its cap, and every warning line closes its quote around a local path that names a
// directory still there and holds no '?' — and the workspace is walked instead the moment it is
// not. A newline can only end a line early at a slash inside the real path, so what survives
// those checks is the directory or one of its parents: a record at least as wide as the one it
// stands for.
func unopenedDirs(workspace, warnings string) ([]string, error) {
	if !strings.Contains(warnings, openWarningPrefix) {
		return nil, nil
	}
	if len(warnings) > subprocess.MaxSubprocessOutputBytes {
		return unopenableDirs(workspace)
	}
	var dirs []string
	for _, line := range strings.Split(warnings, "\n") {
		rest, found := strings.CutPrefix(line, openWarningPrefix)
		if !found {
			continue
		}
		end := strings.LastIndex(rest, openWarningEnd)
		if end < 0 || !isWarnedDir(workspace, rest[:end]) {
			return unopenableDirs(workspace)
		}
		dirs = append(dirs, rest[:end])
	}
	return dirs, nil
}

// isWarnedDir reports whether dir, as read out of a warning, is a path the warning can be
// trusted to have spelled: slash-terminated like every directory git warns about, local to the
// workspace, free of the '?' git writes over a control character, and naming a directory.
func isWarnedDir(workspace, dir string) bool {
	name := strings.TrimSuffix(dir, "/")
	if name == dir || !filepath.IsLocal(filepath.FromSlash(name)) || strings.Contains(dir, "?") {
		return false
	}
	info, err := os.Lstat(filepath.Join(workspace, filepath.FromSlash(name)))
	return err == nil && info.IsDir()
}

// unopenableDirs walks workspace and returns every directory beneath it that cannot be opened,
// workspace-relative with a trailing slash so an unread record covers everything inside it. git
// only warns about such a directory and stages none of it, and `ls-files --others` cannot list
// what it cannot open either, so without this a file inside it that the exchange made readable
// would diff as created. A .git entry is skipped, as git skips it, and a directory that vanished
// mid-walk is simply gone. A workspace root that cannot be walked is an error, so the capture
// fails rather than record nothing.
func unopenableDirs(workspace string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, walkErr error) error {
		switch {
		case walkErr != nil && path == workspace:
			return walkErr
		case walkErr != nil && errors.Is(walkErr, fs.ErrNotExist):
			return nil
		case walkErr != nil:
			rel, err := filepath.Rel(workspace, path)
			if err != nil {
				return err
			}
			dirs = append(dirs, filepath.ToSlash(rel)+"/")
			return filepath.SkipDir
		case entry.IsDir() && entry.Name() == ".git" && path != workspace:
			return filepath.SkipDir
		}
		return nil
	})
	return dirs, err
}

// recordUnread appends paths to the record of what the capture of tree could not read. The
// record is a union: the same tree id can come out of more than one capture, and a path any of
// them could not read stays out of that tree's diffs — the direction that can cost a revert a
// path, never delete one. It is one NUL-terminated write to a file opened for appending, so two
// captures recording at once interleave whole records rather than tear one.
func (s *Store) recordUnread(tree Tree, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	dir := filepath.Join(s.dir, unreadDirName)
	if err := os.MkdirAll(dir, storeDirPerm); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, tree.String()), os.O_WRONLY|os.O_CREATE|os.O_APPEND, unreadFilePerm)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(strings.Join(paths, "\x00") + "\x00")
	return errors.Join(writeErr, file.Close())
}

// unreadOf reads back what [Store.recordUnread] recorded for tree; a tree whose capture read
// everything has no record and answers none.
func (s *Store) unreadOf(tree Tree) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, unreadDirName, tree.String()))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("apogee: snapshot: read the paths the capture of %s could not read: %w", tree, err)
	}
	return splitRecords(data), nil
}

// withoutUnread drops from paths every path an unread record covers. git lists a nested
// repository it would not descend into as its directory with a trailing slash, so a record
// covers the path it names and everything beneath it.
func withoutUnread(paths, unread []string) []string {
	if len(unread) == 0 {
		return paths
	}
	kept := make([]string, 0, len(paths))
	for _, path := range paths {
		if !coveredBy(path, unread) {
			kept = append(kept, path)
		}
	}
	return kept
}

// coveredBy reports whether path is one of records or lies beneath one of them.
func coveredBy(path string, records []string) bool {
	for _, record := range records {
		record = strings.TrimSuffix(record, "/")
		if path == record || strings.HasPrefix(path, record+"/") {
			return true
		}
	}
	return false
}

// stageAndWrite runs one `add -A --ignore-errors` into index and then `write-tree` from it,
// returning write-tree's output and what the add printed on standard error. The add's error is
// returned beside that output rather than instead of it: see [Store.Capture] for why it is not
// fatal on its own. The add runs in the C locale so its warnings read the same on every host.
func (s *Store) stageAndWrite(ctx context.Context, index string) (out, warnings string, addErr, err error) {
	env := s.envWithIndex(index)
	_, warnings, addErr = gitexec.RunDiagnosed(ctx, s.workspace, append(env, cLocale), snapshotTimeout,
		"-c", "core.excludesFile=", "add", "-A", "--ignore-errors")

	out, err = gitexec.Run(ctx, s.workspace, env, snapshotTimeout, "write-tree")
	return out, warnings, addErr, err
}

// Diff returns the workspace-relative paths that differ between two captures — added, removed
// and modified alike — in git's own tree order. It is the scope a revert is confined to (ADR
// 0074 decision 4): a path outside it is not read, not written and not considered.
//
// A path either capture could not read is left out (see [Store.Capture]): its absence from one
// image says nothing about whether the file existed, so a diff naming it would have a revert
// delete a file that was there all along, or restore one over bytes the image never saw. It is
// residue, and the funnel journal is what reverts it (ADR 0074 decision 12).
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
	unreadA, err := s.unreadOf(a)
	if err != nil {
		return nil, err
	}
	unreadB, err := s.unreadOf(b)
	if err != nil {
		return nil, err
	}
	return withoutUnread(splitRecords(out), append(unreadA, unreadB...)), nil
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
	return s.envWithIndex(s.index)
}

// envWithIndex is env with index as the staging file in place of the persistent private index.
func (s *Store) envWithIndex(index string) []string {
	return []string{
		"GIT_DIR=" + s.dir,
		"GIT_WORK_TREE=" + s.workspace,
		"GIT_INDEX_FILE=" + index,
	}
}

// stream runs one read-side git command and returns its standard output UNCAPPED. The capped
// path (gitexec.Run) would silently truncate at 256 KiB, which for a blob being restored or a
// wide Exchange's path list is a corrupt answer rather than a short one.
func (s *Store) stream(ctx context.Context, args ...string) ([]byte, error) {
	return s.streamWith(ctx, s.env(), args...)
}

// streamWith is stream with env in place of the persistent private index's redirection.
func (s *Store) streamWith(ctx context.Context, env []string, args ...string) ([]byte, error) {
	var out bytes.Buffer
	if err := gitexec.RunTo(ctx, s.workspace, env, snapshotTimeout, &out, args...); err != nil {
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
