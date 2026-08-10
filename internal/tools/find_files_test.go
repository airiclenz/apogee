package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedFindTree lays out the fixture the find_files tests read: files at the top level, in a
// shallow directory, in a deep one, and inside an excluded directory — plus two .go files
// whose PATHS differ but whose base names both end in .go, which is what pins basename-only
// matching.
func seedFindTree(t *testing.T, root string) {
	t.Helper()
	for _, dir := range []string{"src", "test", "a/b/c/d", ".git/objects"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("setup mkdir %s: %v", dir, err)
		}
	}
	files := []string{
		"README.md",
		"top.go",
		"src/main.go",
		"src/notes.md",
		"test/main_test.go",
		"a/b/c/d/deep.go",
		".git/objects/blob.go",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("setup write %s: %v", name, err)
		}
	}
}

// findFiles runs the tool over root with args and returns the result content, failing on a Go
// error or an unexpected tool error.
func findFiles(t *testing.T, root string, args map[string]any) string {
	t.Helper()
	result, err := NewFindFiles(root).Execute(context.Background(), callWith(t, "c1", args))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	return result.Content
}

func TestFindFiles_Execute_FindsFlatAndNestedMatches(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedFindTree(t, root)

	content := findFiles(t, root, map[string]any{"pattern": "*.md"})

	for _, want := range []string{"README.md", "src/notes.md"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in result: %q", want, content)
		}
	}
	if strings.Contains(content, ".go") {
		t.Errorf("a non-matching name leaked through the glob: %q", content)
	}
}

func TestFindFiles_Execute_FindsDeepSubdirectoryMatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedFindTree(t, root)

	// Recursion is unconditional — there is no flag to forget, which is the tool's point.
	content := findFiles(t, root, map[string]any{"pattern": "deep.go"})

	if !strings.Contains(content, "a/b/c/d/deep.go") {
		t.Errorf("deep match missing: %q", content)
	}
}

func TestFindFiles_Execute_MatchesBaseNameNotPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedFindTree(t, root)

	content := findFiles(t, root, map[string]any{"pattern": "*.go"})

	// Both live under different directories: the glob is measured against the base name, so
	// neither path prefix takes part in the decision.
	for _, want := range []string{"src/main.go", "test/main_test.go", "top.go"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in result: %q", want, content)
		}
	}

	// The other half of the contract: a PATH pattern is not a supported syntax and matches
	// nothing, rather than quietly behaving like a basename glob.
	if got := findFiles(t, root, map[string]any{"pattern": "src/*.go"}); !strings.Contains(got, "No files found") {
		t.Errorf("a path pattern must not match; got %q", got)
	}
}

func TestFindFiles_Execute_MultiGlobIsCommaSeparated(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedFindTree(t, root)

	content := findFiles(t, root, map[string]any{"pattern": "*.md, deep.go"})

	for _, want := range []string{"README.md", "src/notes.md", "a/b/c/d/deep.go"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in result: %q", want, content)
		}
	}
	if strings.Contains(content, "src/main.go") {
		t.Errorf("a name matching neither glob leaked through: %q", content)
	}
}

func TestFindFiles_Execute_SkipsExcludedDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedFindTree(t, root)

	content := findFiles(t, root, map[string]any{"pattern": "blob.go"})

	if !strings.Contains(content, "No files found") {
		t.Errorf(".git match leaked through exclusion: %q", content)
	}
}

func TestFindFiles_Execute_SearchesOnlyTheNamedSubtree(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedFindTree(t, root)

	content := findFiles(t, root, map[string]any{"pattern": "*.go", "path": "src"})

	if !strings.Contains(content, "src/main.go") {
		t.Errorf("subtree match missing: %q", content)
	}
	if strings.Contains(content, "test/main_test.go") || strings.Contains(content, "top.go") {
		t.Errorf("the search left the named subtree: %q", content)
	}
}

func TestFindFiles_Execute_PaginatesWithTruncationNote(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for i := range 5 {
		name := fmt.Sprintf("page%d.txt", i)
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("setup write %s: %v", name, err)
		}
	}

	first := findFiles(t, root, map[string]any{"pattern": "*.txt", "max_results": 2})

	if !strings.Contains(first, "[5 files found, showing 1-2]") {
		t.Errorf("header does not state the total and the page: %q", first)
	}
	if !strings.Contains(first, "[...3 more, continue with offset 2]") {
		t.Errorf("a truncated page must carry a truncation note: %q", first)
	}
	if strings.Contains(first, "page2.txt") {
		t.Errorf("page 1 shows more than max_results entries: %q", first)
	}

	second := findFiles(t, root, map[string]any{"pattern": "*.txt", "max_results": 2, "offset": 2})

	if !strings.Contains(second, "showing 3-4") {
		t.Errorf("offset did not advance the page: %q", second)
	}
	if strings.Contains(second, "page0.txt") {
		t.Errorf("offset did not skip the first page: %q", second)
	}

	last := findFiles(t, root, map[string]any{"pattern": "*.txt", "offset": 4})

	if strings.Contains(last, "continue with offset") {
		t.Errorf("a final page must carry no truncation note: %q", last)
	}
}

func TestFindFiles_Execute_MalformedGlobMatchesNothingLikeGrep(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedFindTree(t, root)

	// "[" is an unterminated character class: filepath.Match reports ErrBadPattern, which
	// grep's include treats as "matches nothing". find_files parses the same way, so both
	// tools answer a malformed glob the same — no error for the model to decode.
	content := findFiles(t, root, map[string]any{"pattern": "["})
	if !strings.Contains(content, "No files found") {
		t.Errorf("a malformed glob must match nothing: %q", content)
	}

	grepped, err := NewGrep(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "x", "include": "["}))
	if err != nil {
		t.Fatalf("grep Execute returned error: %v", err)
	}
	if !strings.Contains(grepped.Content, "No matches found") {
		t.Errorf("grep's include is the oracle for malformed globs and it matched: %q", grepped.Content)
	}
}

func TestFindFiles_Execute_RequiresPattern(t *testing.T) {
	t.Parallel()

	result, err := NewFindFiles(t.TempDir()).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "   "}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.IsError || !strings.Contains(result.Content, "pattern is required") {
		t.Errorf("a blank pattern must be an IsError result: %+v", result)
	}
}

func TestFindFiles_Execute_RefusesPathEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	seedFindTree(t, root)

	result, err := NewFindFiles(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"pattern": "*.go", "path": "../"}))

	if err != nil {
		t.Fatalf("a path escape is not caller cancellation, so it must not be a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("a path outside the workspace must be refused: %q", result.Content)
	}
	if !strings.Contains(result.Content, "outside the workspace") {
		t.Errorf("content %q does not carry the path-escape message", result.Content)
	}
}

// A symlink pointing OUT of the workspace is a name the walk can see but the fence must not
// vouch for: find_files reports paths, and a reported path is a claim that the file is in the
// workspace. The link is skipped exactly as grep skips a file it cannot read through the fence.
func TestFindFiles_Execute_SkipsSymlinkOutOfWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "id_rsa"), []byte(outsideMarker), 0o600); err != nil {
		t.Fatalf("setup write: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "id_rsa"), filepath.Join(root, "leak.txt")); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "kept.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	content := findFiles(t, root, map[string]any{"pattern": "*.txt"})

	if strings.Contains(content, "leak.txt") {
		t.Errorf("a symlink out of the workspace was named: %q", content)
	}
	if !strings.Contains(content, "kept.txt") {
		t.Errorf("the ordinary file next to it went missing: %q", content)
	}
}

func TestFindFiles_IsRegisteredAndReadOnly(t *testing.T) {
	t.Parallel()

	registry := NewDefaultRegistry(t.TempDir())

	tool, ok := registry.Lookup("find_files")
	if !ok {
		t.Fatal("find_files is missing from the default registry")
	}
	if ro, ok := tool.(interface{ ReadOnly() bool }); !ok || !ro.ReadOnly() {
		t.Error("find_files must declare itself read-only — it only names files")
	}
	// The description is the model's only clue about which of the two discovery tools to
	// reach for, so it must name both halves of the split.
	desc := tool.Description()
	if !strings.Contains(desc, "NAME") || !strings.Contains(desc, "grep") {
		t.Errorf("description must contrast name-matching with grep's content search: %q", desc)
	}
}
