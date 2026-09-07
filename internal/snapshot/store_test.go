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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
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
	store, err := Open(context.Background(), t.TempDir(), workspace)
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
	home := t.TempDir()
	writeFile(t, home, ".gitconfig", "[init]\n\tdefaultObjectFormat = sha256\n")
	t.Setenv("HOME", home)

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

	first, err := Open(context.Background(), storeDir, workspace)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	tree := mustCapture(t, first)

	second, err := Open(context.Background(), storeDir, workspace)
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

	_, err := Open(context.Background(), filepath.Join(workspace, ".apogee-snapshots"), workspace)

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
			if _, err := Open(context.Background(), testCase.dir, testCase.workspace); err == nil {
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

	if !Available() {
		t.Fatal("Available() = false with git on PATH")
	}
}
