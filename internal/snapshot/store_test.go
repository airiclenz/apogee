package snapshot

// Tests for the session-owned object store: what a capture covers, what a diff
// scopes, what a blob read returns, and the three things the store must never do —
// write inside the workspace, disturb a workspace repository, or let the operator's
// own git configuration decide what undo can reach.
//
// Every test that captures needs a real git, so each is gated by requireGit: the
// store's whole contract is what git's plumbing does, and a fake would assert the
// fake. The ones that only parse or validate run everywhere.

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/gitexec"
	"github.com/airiclenz/apogee/internal/subprocess"
	"github.com/airiclenz/apogee/internal/testtime"
)

// requireGit skips the test when no git binary is on PATH — the store degrades to
// unavailable in that case and the engine falls back to the funnel journal.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available on PATH")
	}
}

// writeFile creates one file under root, making its parent directories as needed.
func writeFile(t *testing.T, root, relative, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mustGit runs one git command in dir, failing the test on error.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// newStore opens a store for a fresh workspace directory, returning both. The store
// directory is a sibling of the workspace, never a child of it.
func newStore(t *testing.T) (store *Store, workspace string) {
	t.Helper()
	workspace = t.TempDir()
	store, err := Open(context.Background(), gitexec.OS(), t.TempDir(), workspace)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store, workspace
}

// mustCapture takes one snapshot, failing the test on error.
func mustCapture(t *testing.T, store *Store) Tree {
	t.Helper()
	tree, err := store.Capture(context.Background())
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	return tree
}

// blobID is git's own object id for a file's bytes: sha1 over "blob <len>\0" and the
// content. It is what a conflict check recomputes from the bytes on disk.
func blobID(data []byte) string {
	sum := sha1.Sum(append([]byte(fmt.Sprintf("blob %d\x00", len(data))), data...))
	return fmt.Sprintf("%x", sum)
}

// listWorkspace returns every path under root, workspace-relative and sorted.
func listWorkspace(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.Walk(root, func(path string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative != "." {
			paths = append(paths, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	return paths
}

func TestCaptureDiffScopesExactlyTheUnignoredChanges(t *testing.T) {
	requireGit(t)
	store, workspace := newStore(t)
	writeFile(t, workspace, ".gitignore", "ignored.txt\n")
	writeFile(t, workspace, "a.txt", "original\n")
	writeFile(t, workspace, "sub/b.txt", "nested\n")
	writeFile(t, workspace, "ignored.txt", "before\n")

	pre := mustCapture(t, store)
	writeFile(t, workspace, "a.txt", "edited\n")
	writeFile(t, workspace, "added.txt", "new\n")
	writeFile(t, workspace, "ignored.txt", "after\n")
	if err := os.Remove(filepath.Join(workspace, "sub", "b.txt")); err != nil {
		t.Fatal(err)
	}
	post := mustCapture(t, store)

	changed, err := store.Diff(context.Background(), pre, post)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	sort.Strings(changed)
	want := []string{"a.txt", "added.txt", "sub/b.txt"}
	if strings.Join(changed, ",") != strings.Join(want, ",") {
		t.Fatalf("Diff = %v, want %v", changed, want)
	}
}

func TestContentReturnsPreImageBytesAndReportsAnAbsentPath(t *testing.T) {
	requireGit(t)
	store, workspace := newStore(t)
	writeFile(t, workspace, "a.txt", "original\n")
	writeFile(t, workspace, "sub/b.txt", "nested\n")

	pre := mustCapture(t, store)
	writeFile(t, workspace, "a.txt", "edited\n")
	writeFile(t, workspace, "added.txt", "new\n")

	data, exists, err := store.Content(pre, "a.txt")
	if err != nil || !exists || string(data) != "original\n" {
		t.Fatalf("Content(a.txt) = %q, %v, %v; want %q, true, nil", data, exists, err, "original\n")
	}
	if data, exists, err = store.Content(pre, "sub/b.txt"); err != nil || !exists || string(data) != "nested\n" {
		t.Fatalf("Content(sub/b.txt) = %q, %v, %v; want %q, true, nil", data, exists, err, "nested\n")
	}
	if data, exists, err = store.Content(pre, "added.txt"); err != nil || exists || data != nil {
		t.Fatalf("Content(added.txt) = %q, %v, %v; want nil, false, nil", data, exists, err)
	}
}

func TestContentRoundTripsABlobLargerThanTheSubprocessOutputCap(t *testing.T) {
	requireGit(t)
	store, workspace := newStore(t)
	// The capped subprocess path truncates at 256 KiB; this is comfortably past it.
	large := strings.Repeat("0123456789abcdef", 40*1024)
	writeFile(t, workspace, "large.bin", large)

	tree := mustCapture(t, store)

	data, exists, err := store.Content(tree, "large.bin")
	if err != nil || !exists {
		t.Fatalf("Content(large.bin) = %v, %v; want true, nil", exists, err)
	}
	if len(data) != len(large) || string(data) != large {
		t.Fatalf("Content(large.bin) returned %d bytes, want %d", len(data), len(large))
	}
}

func TestNonASCIIPathsStayUnescapedThroughDiffListAndContent(t *testing.T) {
	requireGit(t)
	store, workspace := newStore(t)
	const accented = "café.txt"
	writeFile(t, workspace, accented, "one\n")

	pre := mustCapture(t, store)
	writeFile(t, workspace, accented, "two\n")
	post := mustCapture(t, store)

	changed, err := store.Diff(context.Background(), pre, post)
	if err != nil || len(changed) != 1 || changed[0] != accented {
		t.Fatalf("Diff = %v, %v; want [%s]", changed, err, accented)
	}
	blobs, err := store.ListBlobs(context.Background(), post)
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	if _, ok := blobs[accented]; !ok {
		t.Fatalf("ListBlobs = %v, want a %s entry", blobs, accented)
	}
	data, exists, err := store.Content(pre, accented)
	if err != nil || !exists || string(data) != "one\n" {
		t.Fatalf("Content(%s) = %q, %v, %v; want %q, true, nil", accented, data, exists, err, "one\n")
	}
}

func TestCaptureIgnoresTheOperatorsGlobalExcludesFile(t *testing.T) {
	requireGit(t)
	home := t.TempDir()
	writeFile(t, home, "globalignore", "personal.txt\n")
	writeFile(t, home, ".gitconfig", "[core]\n\texcludesFile = "+filepath.Join(home, "globalignore")+"\n")
	t.Setenv("HOME", home)

	store, workspace := newStore(t)
	writeFile(t, workspace, "personal.txt", "covered\n")

	blobs, err := store.ListBlobs(context.Background(), mustCapture(t, store))
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	if _, ok := blobs["personal.txt"]; !ok {
		t.Fatalf("ListBlobs = %v, want personal.txt captured despite the global excludes file", blobs)
	}
}

func TestListBlobsReturnsSHA1IDsUnderASHA256DefaultObjectFormat(t *testing.T) {
	requireGit(t)
	requireSHA256DefaultingGit(t)

	store, workspace := newStore(t)
	const content = "hashed\n"
	writeFile(t, workspace, "a.txt", content)

	tree := mustCapture(t, store)
	if len(tree) != 40 {
		t.Fatalf("Capture returned a %d-character tree id, want sha1's 40", len(tree))
	}
	blobs, err := store.ListBlobs(context.Background(), tree)
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	if got, want := blobs["a.txt"], blobID([]byte(content)); got != want {
		t.Fatalf("ListBlobs[a.txt] = %q, want the sha1 blob id %q", got, want)
	}
}

// requireSHA256DefaultingGit makes the host's git default to sha256 object ids for the rest of the
// test, so the store's own --object-format=sha1 is the only thing that can keep its ids sha1 —
// and skips the test where that default cannot be installed, because a test that cannot tell the
// flag from the host's default asserts nothing.
//
// Two spellings of the default go in, because git changed which one it reads: the config key an
// operator's own ~/.gitconfig carries (ignored by gits that predate it — 2.43 parses it and keeps
// sha1) and GIT_DEFAULT_HASH, which every hash-agnostic git honours. The environment variable has
// to travel WITH THE BINARY rather than in this process's environment: gitexec hands the child an
// allowlisted environment that GIT_DEFAULT_HASH is deliberately not on, so a wrapper on PATH is
// the only way an operator's git can reach the store already defaulting to sha256 — which is
// exactly the host this test is about.
//
// The probe at the end is what keeps the whole thing honest: unless a plain `git init` really
// produces a sha256 repository here, the test is skipped rather than passed.
func requireSHA256DefaultingGit(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the sha256-defaulting git is delivered as a POSIX shell wrapper")
	}
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available on PATH")
	}

	home := t.TempDir()
	writeFile(t, home, ".gitconfig", "[init]\n\tdefaultObjectFormat = sha256\n")
	t.Setenv("HOME", home)

	binDir := t.TempDir()
	wrapper := "#!/bin/sh\nGIT_DEFAULT_HASH=sha256\nexport GIT_DEFAULT_HASH\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(wrapper), 0o755); err != nil {
		t.Fatalf("write the sha256-defaulting git wrapper: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	probe := t.TempDir()
	mustGit(t, probe, "init", "--bare", "-q")
	if got := strings.TrimSpace(mustGit(t, probe, "rev-parse", "--show-object-format")); got != "sha256" {
		t.Skipf("this git still defaults to %s with init.defaultObjectFormat and GIT_DEFAULT_HASH both set: "+
			"the store's --object-format=sha1 cannot be told from the default here", got)
	}
}

func TestCaptureSkipsACommitlessNestedRepositoryAndKeepsTheRest(t *testing.T) {
	requireGit(t)
	store, workspace := newStore(t)
	writeFile(t, workspace, "a.txt", "kept\n")
	writeFile(t, workspace, "nested/n.txt", "inside a repository of its own\n")
	mustGit(t, filepath.Join(workspace, "nested"), "init", "-q")

	blobs, err := store.ListBlobs(context.Background(), mustCapture(t, store))
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	if _, ok := blobs["a.txt"]; !ok {
		t.Fatalf("ListBlobs = %v, want a.txt captured", blobs)
	}
	if _, ok := blobs["nested/n.txt"]; ok {
		t.Fatalf("ListBlobs = %v, want the nested repository left out", blobs)
	}
}

func TestCaptureWritesNothingUnderTheWorkspace(t *testing.T) {
	requireGit(t)
	store, workspace := newStore(t)
	writeFile(t, workspace, "a.txt", "one\n")
	writeFile(t, workspace, "sub/b.txt", "two\n")
	before := listWorkspace(t, workspace)

	mustCapture(t, store)

	after := listWorkspace(t, workspace)
	if strings.Join(after, ",") != strings.Join(before, ",") {
		t.Fatalf("workspace holds %v after a capture, want %v", after, before)
	}
	for _, path := range after {
		if path == ".git" || strings.HasSuffix(path, "/.git") {
			t.Fatalf("capture left a repository at %s inside the workspace", path)
		}
	}
}

func TestCaptureLeavesAWorkspaceRepositoryUntouched(t *testing.T) {
	requireGit(t)
	store, workspace := newStore(t)
	mustGit(t, workspace, "init", "-q")
	writeFile(t, workspace, "tracked.txt", "committed\n")
	mustGit(t, workspace, "add", "tracked.txt")
	mustGit(t, workspace, "-c", "user.email=test@test", "-c", "user.name=test", "commit", "-q", "-m", "seed")
	writeFile(t, workspace, "tracked.txt", "edited by hand\n")
	writeFile(t, workspace, "untracked.txt", "also by hand\n")
	before := mustGit(t, workspace, "status", "--porcelain")

	mustCapture(t, store)

	if after := mustGit(t, workspace, "status", "--porcelain"); after != before {
		t.Fatalf("git status is %q after a capture, want %q", after, before)
	}
}

func TestOpenReopensAnExistingStore(t *testing.T) {
	requireGit(t)
	storeDir := t.TempDir()
	workspace := t.TempDir()
	writeFile(t, workspace, "a.txt", "first\n")

	first, err := Open(context.Background(), gitexec.OS(), storeDir, workspace)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	tree := mustCapture(t, first)

	second, err := Open(context.Background(), gitexec.OS(), storeDir, workspace)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	data, exists, err := second.Content(tree, "a.txt")
	if err != nil || !exists || string(data) != "first\n" {
		t.Fatalf("reopened Content = %q, %v, %v; want %q, true, nil", data, exists, err, "first\n")
	}
}

func TestOpenRefusesAStoreDirectoryInsideTheWorkspace(t *testing.T) {
	workspace := t.TempDir()

	_, err := Open(context.Background(), gitexec.OS(), filepath.Join(workspace, ".apogee-snapshots"), workspace)

	if err == nil || !strings.Contains(err.Error(), "inside the workspace") {
		t.Fatalf("Open = %v, want a refusal naming the workspace", err)
	}
	if entries, readErr := os.ReadDir(workspace); readErr != nil || len(entries) != 0 {
		t.Fatalf("workspace holds %v after the refusal, want it untouched", entries)
	}
}

func TestOpenRejectsMissingArguments(t *testing.T) {
	cases := map[string]struct{ dir, workspace string }{
		"no store directory": {dir: "", workspace: t.TempDir()},
		"no workspace":       {dir: t.TempDir(), workspace: ""},
		"absent workspace":   {dir: t.TempDir(), workspace: filepath.Join(t.TempDir(), "gone")},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Open(context.Background(), gitexec.OS(), testCase.dir, testCase.workspace); err == nil {
				t.Fatal("Open succeeded, want an error")
			}
		})
	}
}

func TestRemoveDeletesTheStoreAndRefusesAnEmptyPath(t *testing.T) {
	storeDir := t.TempDir()
	writeFile(t, storeDir, "objects/keep", "bytes\n")

	if err := Remove(storeDir); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(storeDir); !os.IsNotExist(err) {
		t.Fatalf("store directory still present after Remove (%v)", err)
	}
	if err := Remove(""); err == nil {
		t.Fatal("Remove(\"\") succeeded, want an error")
	}
}

func TestParseTreeAcceptsOnlyFullObjectIDs(t *testing.T) {
	cases := map[string]struct {
		id    string
		valid bool
	}{
		"sha1":         {id: strings.Repeat("a", 40), valid: true},
		"sha256":       {id: strings.Repeat("0", 64), valid: true},
		"abbreviated":  {id: strings.Repeat("a", 7)},
		"uppercase":    {id: strings.Repeat("A", 40)},
		"empty":        {id: ""},
		"ref name":     {id: "HEAD"},
		"option-shape": {id: "--upload-pack=touch"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			tree, err := ParseTree(testCase.id)
			if testCase.valid {
				if err != nil || tree.String() != testCase.id {
					t.Fatalf("ParseTree(%q) = %q, %v; want the id and no error", testCase.id, tree, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ParseTree(%q) succeeded, want an error", testCase.id)
			}
		})
	}
}

func TestReadsRefuseAMalformedTreeID(t *testing.T) {
	store := &Store{dir: t.TempDir(), workspace: t.TempDir()}
	valid := Tree(strings.Repeat("a", 40))

	if _, err := store.Diff(context.Background(), "HEAD", valid); err == nil {
		t.Fatal("Diff accepted a ref name, want an error")
	}
	if _, err := store.ListBlobs(context.Background(), "HEAD"); err == nil {
		t.Fatal("ListBlobs accepted a ref name, want an error")
	}
	if _, _, err := store.Content("HEAD", "a.txt"); err == nil {
		t.Fatal("Content accepted a ref name, want an error")
	}
	if _, _, err := store.Content(valid, ""); err == nil {
		t.Fatal("Content accepted an empty path, want an error")
	}
}

func TestAvailableReportsGitOnPath(t *testing.T) {
	requireGit(t)

	if !Available(gitexec.OS()) {
		t.Fatal("Available(gitexec.OS()) = false with git on PATH")
	}
}

// The store runs git through the Host it is handed, never the machine's own: a Host whose lookup
// finds no git makes the store unavailable and its Open fail, with no git spawned — on a machine
// that has git on PATH as much as on one that has not.
func TestAStoreOnAHostWithNoGitIsUnavailableAndNeverSpawns(t *testing.T) {
	t.Parallel()
	spawned := false
	absent := gitexec.Host{
		Look: func(string) (string, error) { return "", exec.ErrNotFound },
		Spawn: func(context.Context, subprocess.SubprocessSpec) (subprocess.SubprocessResult, error) {
			spawned = true
			return subprocess.SubprocessResult{}, nil
		},
		SpawnTo: func(context.Context, subprocess.SubprocessSpec, io.Writer) (subprocess.SubprocessResult, error) {
			spawned = true
			return subprocess.SubprocessResult{}, nil
		},
	}

	available := Available(absent)
	_, openErr := Open(context.Background(), absent, t.TempDir(), t.TempDir())

	if available {
		t.Error("Available = true on a Host whose lookup finds no git")
	}
	if openErr == nil || !strings.Contains(openErr.Error(), gitexec.UnavailableMessage) {
		t.Errorf("Open err = %v, want the absent-git refusal %q", openErr, gitexec.UnavailableMessage)
	}
	if spawned {
		t.Error("a git child was spawned through a Host with no git")
	}
}

// A path git cannot read at the second capture must be ABSENT from that image, never frozen at
// the content an earlier capture staged: a stale index entry would let a revert "restore" bytes
// the file no longer held when the exchange began (ADR 0074 decision 12).
func TestCaptureLeavesOutAnUnreadablePathInsteadOfItsStaleContent(t *testing.T) {
	requireGit(t)
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits cannot make a file unreadable on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-000 file, so git add cannot be made to fail")
	}
	store, workspace := newStore(t)
	writeFile(t, workspace, "kept.txt", "kept\n")
	writeFile(t, workspace, "locked.txt", "old content\n")
	mustCapture(t, store)

	writeFile(t, workspace, "locked.txt", "new content, not the old one\n")
	locked := filepath.Join(workspace, "locked.txt")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })

	blobs, err := store.ListBlobs(context.Background(), mustCapture(t, store))
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	if id, ok := blobs["locked.txt"]; ok {
		t.Fatalf("ListBlobs holds locked.txt as %s (old content is %s), want the unreadable path absent",
			id, blobID([]byte("old content\n")))
	}
	if _, ok := blobs["kept.txt"]; !ok {
		t.Fatalf("ListBlobs = %v, want kept.txt captured", blobs)
	}
}

// A path a capture could not read is absent from that image whether or not the file existed, so
// a diff that named it would read the absence as a creation or a deletion. Diff leaves it out on
// either side — the pre-image that could not read it and the post-image that could not — and
// keeps every other change.
func TestDiffLeavesOutAPathEitherCaptureCouldNotRead(t *testing.T) {
	requireGit(t)
	requireUnreadableFiles(t)
	store, workspace := newStore(t)
	ctx := context.Background()
	locked := filepath.Join(workspace, "locked.txt")
	writeFile(t, workspace, "locked.txt", "there all along\n")
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })

	readable := mustCapture(t, store)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	writeFile(t, workspace, "one.txt", "changed while locked\n")
	unreadable := mustCapture(t, store)
	if err := os.Chmod(locked, 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, workspace, "two.txt", "changed once readable again\n")
	readableAgain := mustCapture(t, store)

	for _, pair := range []struct {
		name string
		a, b Tree
		want string
	}{
		{"unreadable in the post-image", readable, unreadable, "one.txt"},
		{"unreadable in the pre-image", unreadable, readableAgain, "two.txt"},
	} {
		t.Run(pair.name, func(t *testing.T) {
			diff, err := store.Diff(ctx, pair.a, pair.b)
			if err != nil {
				t.Fatalf("Diff: %v", err)
			}
			if strings.Join(diff, ",") != pair.want {
				t.Errorf("Diff = %v, want only %s — locked.txt could not be read, so it is residue", diff, pair.want)
			}
		})
	}
}

// An unread record covers the path it names and, for the directory git lists a nested repository
// as, every path beneath it — never a sibling that merely shares the prefix.
func TestWithoutUnreadDropsCoveredPathsOnly(t *testing.T) {
	t.Parallel()
	paths := []string{"a.txt", "locked.txt", "nested", "nested/n.txt", "nestedsibling.txt", "sub/locked.txt"}
	got := withoutUnread(paths, []string{"locked.txt", "nested/"})
	want := []string{"a.txt", "nestedsibling.txt", "sub/locked.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("withoutUnread = %v, want %v", got, want)
	}
	if got := withoutUnread(paths, nil); strings.Join(got, ",") != strings.Join(paths, ",") {
		t.Errorf("withoutUnread with no record = %v, want every path", got)
	}
}

// A directory that stops opening after a capture staged it is only warned about — the add exits
// zero — yet the index still holds what that earlier capture read inside it. The image must leave
// those paths out rather than freeze the old bytes as their content, keep the rest, and take them
// back in once the directory opens again.
func TestCaptureLeavesOutTheStaleEntriesOfADirectoryItCouldNotOpen(t *testing.T) {
	requireGit(t)
	requireUnreadableFiles(t)
	store, workspace := newStore(t)
	ctx := context.Background()
	writeFile(t, workspace, "kept.txt", "kept\n")
	writeFile(t, workspace, "dir/inner.txt", "old content\n")
	mustCapture(t, store)

	writeFile(t, workspace, "dir/inner.txt", "new content, not the old one\n")
	dir := filepath.Join(workspace, "dir")
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	closed := mustCapture(t, store)
	blobs, err := store.ListBlobs(ctx, closed)
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	if id, ok := blobs["dir/inner.txt"]; ok {
		t.Fatalf("ListBlobs holds dir/inner.txt as %s (old content is %s), want the unopened directory's file absent",
			id, blobID([]byte("old content\n")))
	}
	if _, ok := blobs["kept.txt"]; !ok {
		t.Fatalf("ListBlobs = %v, want kept.txt captured", blobs)
	}

	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	blobs, err = store.ListBlobs(ctx, mustCapture(t, store))
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	if want := blobID([]byte("new content, not the old one\n")); blobs["dir/inner.txt"] != want {
		t.Errorf("dir/inner.txt once the directory opens = %q, want %s", blobs["dir/inner.txt"], want)
	}
}

// The warning is git's only word on a directory it could not open, and a message rather than a
// listing: a path is taken from it only while the account is whole, and anything short of that
// walks the workspace instead — which, here, finds nothing to add.
func TestUnopenedDirsTrustsOnlyAWholeAccount(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	for _, dir := range []string{"dir", "sp ace", "a/b"} {
		if err := os.MkdirAll(filepath.Join(workspace, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	warn := func(dir string) string { return openWarningPrefix + dir + openWarningEnd + "Permission denied\n" }

	tests := []struct {
		name     string
		warnings string
		want     string
	}{
		{"no warning names a directory", "warning: in the working copy of 'x', LF will be replaced by CRLF\n", ""},
		{"whole", warn("dir/") + "dir/inner.txt: Permission denied\n" + warn("sp ace/") + warn("a/b/"), "dir/,sp ace/,a/b/"},
		{"a control character git wrote over", warn("d?r/"), ""},
		{"a directory that is not there", warn("gone/"), ""},
		{"no trailing slash", warn("dir"), ""},
		{"outside the workspace", warn("../dir/"), ""},
		{"a line cut before its closing quote", openWarningPrefix + "dir/\n", ""},
		{"a stream cut at its cap", warn("dir/") + strings.Repeat("x", subprocess.MaxSubprocessOutputBytes), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := unopenedDirs(workspace, tt.warnings)
			if err != nil {
				t.Fatalf("unopenedDirs: %v", err)
			}
			if strings.Join(got, ",") != tt.want {
				t.Errorf("unopenedDirs = %q, want %q", got, tt.want)
			}
		})
	}
}

// gitRun is one git child a scripted Host was asked to spawn: its subcommand, the index it was
// pointed at, and the bound it was given.
type gitRun struct {
	sub     string
	index   string
	timeout time.Duration
}

// gitLog records every git child a scripted Host spawned, in order.
type gitLog struct {
	mu   sync.Mutex
	runs []gitRun
}

func (l *gitLog) add(run gitRun) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.runs = append(l.runs, run)
}

// count returns how many runs of sub the log holds.
func (l *gitLog) count(sub string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, run := range l.runs {
		if run.sub == sub {
			n++
		}
	}
	return n
}

// all returns a copy of the runs recorded so far.
func (l *gitLog) all() []gitRun {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]gitRun(nil), l.runs...)
}

// subcommandOf returns the git subcommand an argv runs — the first word after the program that
// is neither an option nor a `-c` value.
func subcommandOf(argv []string) string {
	for i := 1; i < len(argv); i++ {
		switch {
		case argv[i] == "-c":
			i++
		case strings.HasPrefix(argv[i], "-"):
		default:
			return argv[i]
		}
	}
	return ""
}

// indexOf returns the GIT_INDEX_FILE an environment points git at, or "".
func indexOf(env []string) string {
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "GIT_INDEX_FILE="); ok {
			return value
		}
	}
	return ""
}

// play decides one scripted run: handled is false to spawn it for real (spec may be rewritten),
// true to answer it with result instead.
type play func(run gitRun, spec *subprocess.SubprocessSpec) (result subprocess.SubprocessResult, handled bool)

// scriptedGit is the real git with some of its runs answered by p, and every run logged.
func scriptedGit(log *gitLog, p play) gitexec.Host {
	decide := func(spec *subprocess.SubprocessSpec) (subprocess.SubprocessResult, bool) {
		run := gitRun{sub: subcommandOf(spec.Argv), index: indexOf(spec.Env), timeout: spec.Timeout}
		log.add(run)
		if p == nil {
			return subprocess.SubprocessResult{}, false
		}
		return p(run, spec)
	}
	return gitexec.Host{
		Spawn: func(ctx context.Context, spec subprocess.SubprocessSpec) (subprocess.SubprocessResult, error) {
			if result, handled := decide(&spec); handled {
				return result, nil
			}
			return subprocess.RunSubprocess(ctx, spec)
		},
		SpawnTo: func(ctx context.Context, spec subprocess.SubprocessSpec, stdout io.Writer) (subprocess.SubprocessResult, error) {
			if result, handled := decide(&spec); handled {
				return result, nil
			}
			return subprocess.RunSubprocessTo(ctx, spec, stdout)
		},
	}
}

// taking plays each named subcommand as running for its duration: a run whose bound is shorter
// times out — what subprocess reports for a git killed at its bound — and any other runs for real.
func taking(durations map[string]time.Duration) play {
	return func(run gitRun, _ *subprocess.SubprocessSpec) (subprocess.SubprocessResult, bool) {
		if took, ok := durations[run.sub]; ok && took > run.timeout {
			return subprocess.SubprocessResult{TimedOut: true, ExitCode: -1}, true
		}
		return subprocess.SubprocessResult{}, false
	}
}

// newScriptedStore opens a store for a fresh workspace through scriptedGit, holding one file.
func newScriptedStore(t *testing.T, p play) (*Store, string, *gitLog) {
	t.Helper()
	log := &gitLog{}
	workspace := t.TempDir()
	writeFile(t, workspace, "main.go", "package main\n")
	store, err := Open(context.Background(), scriptedGit(log, p), t.TempDir(), workspace)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store, workspace, log
}

// A slow machine makes a capture slower, never failed: every git run that walks or hashes the
// work-tree takes the wedge backstop, so one that outlasts the read bound — the whole-tree add of
// a large workspace on a slow disk — still completes, whichever of the capture's passes it is.
func TestWorkTreeRunsOutlastTheReadBound(t *testing.T) {
	requireGit(t)
	t.Parallel()
	slow := testtime.Fixed(5 * time.Minute) // past readTimeout, short of workTreeBackstop
	cases := []struct {
		name string
		sub  string
		run  func(ctx context.Context, store *Store) error
	}{
		{"add", "add", func(ctx context.Context, store *Store) error {
			_, err := store.Capture(ctx)
			return err
		}},
		{"write-tree", "write-tree", func(ctx context.Context, store *Store) error {
			_, err := store.Capture(ctx)
			return err
		}},
		{"ls-files --deleted", "ls-files", func(ctx context.Context, store *Store) error {
			out, err := store.git.Run(ctx, store.workspace, store.env(), readTimeout, "write-tree")
			if err != nil {
				return err
			}
			_, _, err = store.settleWarnings(ctx, out, "warning: something to settle\n")
			return err
		}},
		{"rm --cached", "rm", func(ctx context.Context, store *Store) error {
			return store.dropFromIndex(ctx, []string{"main.go"})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store, _, log := newScriptedStore(t, taking(map[string]time.Duration{tc.sub: slow}))
			if err := tc.run(context.Background(), store); err != nil {
				t.Fatalf("a %s past the read bound failed: %v", tc.sub, err)
			}
			if log.count(tc.sub) == 0 {
				t.Fatalf("no %s ran", tc.sub)
			}
		})
	}

	// The fresh-index walk runs only when the rebuild's own add fails, so it is driven directly.
	t.Run("ls-files --others", func(t *testing.T) {
		t.Parallel()
		walk := taking(map[string]time.Duration{"ls-files": slow})
		store, _, _ := newScriptedStore(t, func(run gitRun, spec *subprocess.SubprocessSpec) (subprocess.SubprocessResult, bool) {
			if run.sub == "add" {
				return subprocess.SubprocessResult{ExitCode: 1, CombinedOutput: "error: could not add\n"}, true
			}
			return walk(run, spec)
		})
		_, unread, _, err := store.captureFresh(context.Background())
		if err != nil {
			t.Fatalf("a fresh-index walk past the read bound failed: %v", err)
		}
		if !slices.Contains(unread, "main.go") {
			t.Fatalf("unread = %v, want the path the failed add left out", unread)
		}
	})
}

// The bound is picked by the work: a run over the work-tree takes the backstop, a read of what
// the store already holds keeps the short bound.
func TestStoreBoundsEachRunByItsWork(t *testing.T) {
	requireGit(t)
	t.Parallel()
	store, _, log := newScriptedStore(t, nil)
	ctx := context.Background()
	tree := mustCapture(t, store)
	if _, err := store.Diff(ctx, tree, tree); err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if _, _, err := store.Content(tree, "main.go"); err != nil {
		t.Fatalf("Content: %v", err)
	}

	want := map[string]time.Duration{
		"add": workTreeBackstop, "write-tree": workTreeBackstop,
		"diff-tree": readTimeout, "ls-tree": readTimeout, "cat-file": readTimeout,
	}
	seen := map[string]bool{}
	for _, run := range log.all() {
		bound, ok := want[run.sub]
		if !ok {
			continue
		}
		seen[run.sub] = true
		if run.timeout != bound {
			t.Errorf("git %s ran under %s, want %s", run.sub, run.timeout, bound)
		}
	}
	for sub := range want {
		if !seen[sub] {
			t.Errorf("no git %s ran", sub)
		}
	}
}

// An add that hit its backstop is not a path git could not read: the capture fails with the
// incomplete answer and never stages the whole tree a second time into a fresh index, which would
// only meet the same wedge.
func TestCaptureDoesNotRebuildAfterTheAddRanOutItsBound(t *testing.T) {
	requireGit(t)
	t.Parallel()
	store, _, log := newScriptedStore(t, taking(map[string]time.Duration{"add": 2 * workTreeBackstop}))

	_, err := store.Capture(context.Background())

	if !errors.Is(err, gitexec.ErrIncomplete) {
		t.Fatalf("Capture err = %v, want one matching gitexec.ErrIncomplete", err)
	}
	if n := log.count("add"); n != 1 {
		t.Errorf("%d adds ran, want 1: a deadline must not chain into a fresh-index rebuild", n)
	}
	if n := log.count("write-tree"); n != 0 {
		t.Errorf("%d write-trees ran after an add that gave no answer, want 0", n)
	}
}

// A git that never exits is still ended — by the backstop, which a test scales down — and the
// capture fails once, without a rebuild.
func TestCaptureEndsAWedgedAddAtTheBackstop(t *testing.T) {
	requireGit(t)
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep binary to stand in for a wedged git")
	}
	t.Parallel()
	store, _, log := newScriptedStore(t, func(run gitRun, spec *subprocess.SubprocessSpec) (subprocess.SubprocessResult, bool) {
		if run.sub == "add" {
			spec.Argv = []string{sleep, "3600"}
		}
		return subprocess.SubprocessResult{}, false
	})
	store.workBound = testtime.Fixed(300 * time.Millisecond)

	start := time.Now()
	_, err = store.Capture(context.Background())
	elapsed := time.Since(start)

	if !errors.Is(err, gitexec.ErrIncomplete) {
		t.Fatalf("Capture err = %v, want the backstop's incomplete answer", err)
	}
	if limit := testtime.Budget(30 * time.Second); elapsed > limit {
		t.Errorf("Capture took %s, want it ended by the backstop well within %s", elapsed, limit)
	}
	if n := log.count("add"); n != 1 {
		t.Errorf("%d adds ran, want 1", n)
	}
}

// A capture killed mid-add leaves git's index.lock in the store; the next capture clears it and
// stages into the persistent index as usual, rather than failing its add and rebuilding the
// whole tree into a fresh index on every capture after.
func TestCaptureClearsAStaleIndexLock(t *testing.T) {
	requireGit(t)
	t.Parallel()
	store, workspace, log := newScriptedStore(t, nil)
	mustCapture(t, store)
	lock := store.index + ".lock"
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	writeFile(t, workspace, "new.txt", "new\n")
	before := log.count("add")

	blobs, err := store.ListBlobs(context.Background(), mustCapture(t, store))

	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	if _, ok := blobs["new.txt"]; !ok {
		t.Errorf("ListBlobs = %v, want new.txt captured", blobs)
	}
	if n := log.count("add") - before; n != 1 {
		t.Errorf("%d adds ran, want 1: the stale lock sent the capture to a fresh-index rebuild", n)
	}
	for _, run := range log.all() {
		if run.sub == "add" && run.index != store.index {
			t.Errorf("an add ran into %s, want only the persistent index %s", run.index, store.index)
		}
	}
	if _, err := os.Stat(lock); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("index.lock still present after the capture (stat err %v)", err)
	}
}
