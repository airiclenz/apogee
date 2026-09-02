package tools

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func TestWriteFile_Execute_CreatesFileAndParents(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)

	result, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "nested/dir/out.txt", "content": "hello"}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	got, readErr := os.ReadFile(filepath.Join(root, "nested", "dir", "out.txt"))
	if readErr != nil {
		t.Fatalf("file was not created: %v", readErr)
	}
	if string(got) != "hello" {
		t.Errorf("file content = %q, want %q", string(got), "hello")
	}
}

func TestWriteFile_Execute_OverwritesExisting(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	path := filepath.Join(root, "f.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt", "content": "new"}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("content = %q, want %q", string(got), "new")
	}
}

func TestWriteFile_Execute_ReportsBytesWritten(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)

	result, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "out.txt", "content": "hello"}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if want := "wrote 5 bytes to out.txt"; result.Content != want {
		t.Errorf("Content = %q, want %q", result.Content, want)
	}
	// The sentence still counts BYTES; the structured half beside it is the change itself.
	if _, ok := result.Summary.(domain.EditRegions); !ok {
		t.Fatalf("Summary = %#v, want a domain.EditRegions", result.Summary)
	}
}

// A write onto nothing records the whole content as ONE region of pure insertion. The before side
// of a create is ZERO lines, not one empty one, and this pins the difference: an empty before text
// handed to the region cutter splits to [""], which would record a phantom removed blank line and
// read the BLANK LINE in this content as unchanged context instead of an inserted one.
func TestWriteFile_Execute_RecordsACreateAsOneInsertedRegion(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)

	result, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "new.txt", "content": "alpha\n\nbeta\n"}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	region := onlyRegion(t, result)
	if region.BeforeStart != 1 || region.AfterStart != 1 {
		t.Errorf("start lines = before %d / after %d, want 1 / 1", region.BeforeStart, region.AfterStart)
	}
	if len(region.Removed) != 0 {
		t.Errorf("Removed = %q, want empty — a create removes nothing", region.Removed)
	}
	if want := []string{"alpha", "", "beta"}; !slices.Equal(region.Inserted, want) {
		t.Errorf("Inserted = %q, want %q", region.Inserted, want)
	}
	if len(region.Leading) != 0 || len(region.Trailing) != 0 {
		t.Errorf("context = leading %q / trailing %q, want neither — there is no before file to give it",
			region.Leading, region.Trailing)
	}
}

// An overwrite reports the lines that DIFFER, not the file it wrote: one region at the real line
// numbers of the change, with the usual three lines of context each side.
func TestWriteFile_Execute_RecordsTheChangedRegionOfAnOverwrite(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	before := numberedLines(40)
	after := slices.Clone(before)
	after[19] = "line 20 changed"
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte(wholeFile(before)), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt", "content": wholeFile(after)}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	region := onlyRegion(t, result)
	if region.BeforeStart != 17 || region.AfterStart != 17 {
		t.Errorf("start lines = before %d / after %d, want 17 / 17 (line 20 less three of context)",
			region.BeforeStart, region.AfterStart)
	}
	if want := []string{"line 20"}; !slices.Equal(region.Removed, want) {
		t.Errorf("Removed = %q, want %q", region.Removed, want)
	}
	if want := []string{"line 20 changed"}; !slices.Equal(region.Inserted, want) {
		t.Errorf("Inserted = %q, want %q", region.Inserted, want)
	}
	if want := []string{"line 17", "line 18", "line 19"}; !slices.Equal(region.Leading, want) {
		t.Errorf("Leading = %q, want %q", region.Leading, want)
	}
	if want := []string{"line 21", "line 22", "line 23"}; !slices.Equal(region.Trailing, want) {
		t.Errorf("Trailing = %q, want %q", region.Trailing, want)
	}
}

// A write that changes nothing attaches NO summary: an EditRegions holding no region would claim
// the write changed nothing WHILE asking the card to paint a diff, so the card keeps its prose floor.
func TestWriteFile_Execute_RecordsNoSummaryForIdenticalContent(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("same\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt", "content": "same\n"}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}
	if result.Summary != nil {
		t.Errorf("Summary = %#v, want none for a write that changed nothing", result.Summary)
	}
	if want := "wrote 5 bytes to f.txt"; result.Content != want {
		t.Errorf("Content = %q, want %q", result.Content, want)
	}
}

// The pre-read is for the CARD, never a precondition of the write: an original the tool cannot read
// degrades to an empty before side, exactly as an absent one does. A tool that read nothing at all
// until today must not start refusing writes on a read error.
func TestWriteFile_Execute_WritesThroughAnUnreadableOriginal(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("running as root: a mode-0 file is still readable, so an unreadable original cannot be staged")
	}

	root := tempRoot(t)
	path := filepath.Join(root, "f.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o000); err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt", "content": "alpha\nbeta\n"}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("the write was refused over an unreadable original: %q", result.Content)
	}

	// The replacement inherits the original's mode, so the check has to open the door it just
	// walked through: what is under test is the WRITE, not the perms it preserved.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod after the write: %v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("file was not written: %v", readErr)
	}
	if string(got) != "alpha\nbeta\n" {
		t.Errorf("content = %q, want the written content", string(got))
	}

	region := onlyRegion(t, result)
	if region.BeforeStart != 1 || region.AfterStart != 1 || len(region.Removed) != 0 {
		t.Errorf("region = %+v, want an empty before side starting at line 1", region)
	}
	if want := []string{"alpha", "beta"}; !slices.Equal(region.Inserted, want) {
		t.Errorf("Inserted = %q, want %q — the whole content reads as inserted", region.Inserted, want)
	}
}

// onlyRegion returns the single Edit region a write result carries, failing the test when the
// result carries no regions summary or carries more than one region.
func onlyRegion(t *testing.T, result domain.ToolResult) domain.EditRegion {
	t.Helper()

	regions, ok := result.Summary.(domain.EditRegions)
	if !ok {
		t.Fatalf("Summary = %#v, want a domain.EditRegions", result.Summary)
	}
	if len(regions.Regions) != 1 {
		t.Fatalf("regions = %d, want exactly 1: %+v", len(regions.Regions), regions.Regions)
	}
	return regions.Regions[0]
}

// wholeFile joins lines into the bytes of a newline-TERMINATED file, the shape an ordinary text
// file has and the one whose trailing empty split element must not read as a written line.
func wholeFile(lines []string) string { return strings.Join(lines, "\n") + "\n" }

// The sentence a write reports names where it landed when that is not where the argument said. It
// is the result-string third of one disclosure — the approval pane and the tool card say the same
// thing about the same call, off the same resolution — and it matters here because this string is
// what the MODEL reads back and what an expanded block prints: a `docs/` that is a symlink is
// otherwise invisible on every one of the three.
//
// The redirect is resolved BEFORE the write for a reason the second half pins: afterwards the final
// name is a plain file whatever it was, so a note read after the fact would go quiet on exactly the
// call worth disclosing.
//
// The link is a final NAME rather than a parent directory because a write whose PARENT chain
// crosses a symlink is now refused outright (security.ErrSymlinkedParent) — so the surviving
// divergence, and the one this disclosure exists for, is the name the write is about to replace.
func TestWriteFile_Execute_NamesTheResolvedTarget(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(real, "target.md"), []byte("old"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// A RELATIVE in-root symlink AT THE TARGET NAME: the write succeeds (the fence follows a link
	// that stays inside it, and the rename replaces the name), so the SUCCESS sentence is the one
	// that has to carry the disclosure. An absolute link is refused by the fence whatever it points
	// at, and a refusal is not this test's subject.
	if err := os.Symlink(filepath.Join("real", "target.md"), filepath.Join(root, "notes.md")); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	result, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "notes.md", "content": "hello"}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", result.Content)
	}

	want := "wrote 5 bytes to notes.md → resolves to " + realPath(t, filepath.Join(real, "target.md"))
	if result.Content != want {
		t.Errorf("Content = %q, want %q", result.Content, want)
	}

	// A path that names its own target reports the sentence it always did.
	plain, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c2", map[string]any{"path": "plain.md", "content": "hello"}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if plain.Content != "wrote 5 bytes to plain.md" {
		t.Errorf("Content = %q, want the bare sentence for a path that resolves to itself", plain.Content)
	}
}

// The disclosure above is the second-best outcome; for a write the first is not to happen at all.
// An in-root `docs → .git` keeps the whole call inside the workspace, so confine-to-workspace never
// fires and the pane says "docs/config" for a call that would rewrite the repository's own config.
// write_file must refuse it, and the refusal the model reads must say why rather than read as an
// ordinary I/O failure.
func TestWriteFile_Execute_RefusesSymlinkedParent(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(".git", filepath.Join(root, "docs")); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	result, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "docs/config", "content": "[core]\n\tfsmonitor = pwned\n"}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, want the write refused (content: %q)", result.Content)
	}
	if !strings.Contains(result.Content, "symlinked directory") || !strings.Contains(result.Content, "docs") {
		t.Errorf("refusal %q does not name the rule and the symlinked component", result.Content)
	}

	got, readErr := os.ReadFile(filepath.Join(gitDir, "config"))
	if readErr != nil {
		t.Fatalf("read redirect target: %v", readErr)
	}
	if string(got) != "[core]\n" {
		t.Errorf(".git/config content = %q, want it untouched", got)
	}
}

func TestWriteFile_Execute_ToolErrors(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	tool := NewWriteFile(root)

	cases := []struct {
		name        string
		args        map[string]any
		wantContain string
	}{
		{"path escape", map[string]any{"path": "../evil.txt", "content": "x"}, "outside the workspace"},
		{"missing path", map[string]any{"content": "x"}, "path is required"},
		{"oversized content", map[string]any{"path": "big.txt", "content": strings.Repeat("a", maxFileContentBytes+1)}, "content too large"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(), callWith(t, "c1", tc.args))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("IsError = false, want true (content: %q)", result.Content)
			}
			if !strings.Contains(result.Content, tc.wantContain) {
				t.Errorf("content %q does not contain %q", result.Content, tc.wantContain)
			}
		})
	}
}

// A write that breaks the file it writes still LANDS: the result stays a success, keeps its
// summary, and the file holds exactly the bytes asked for. What changes is that the model is told,
// on the same result, that what it just wrote does not parse — structural feedback where it acted,
// not a refusal it has to argue with.
func TestWriteFile_Execute_AppendsTheSyntaxTrailer(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	const broken = "package main\n\nfunc main() {}\n}\n"

	result, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "main.go", "content": broken}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("result is an error: %q — a broken write still lands", result.Content)
	}
	if _, ok := result.Summary.(domain.EditRegions); !ok {
		t.Errorf("Summary = %#v, want the Edit regions a successful write always carries", result.Summary)
	}
	if !strings.HasPrefix(result.Content, "wrote 31 bytes to main.go\n") {
		t.Errorf("Content = %q, want the trailer behind the tool's own sentence", result.Content)
	}
	if !strings.Contains(result.Content, "syntax check: 1 problem") || !strings.Contains(result.Content, "line 4:") {
		t.Errorf("Content = %q, want the located syntax problem", result.Content)
	}

	written, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(written) != broken {
		t.Errorf("file = %q, want exactly the bytes the call asked for", written)
	}
}

// The trailer rides BEHIND the resolved-target disclosure, so the sentence that says where the
// write really landed is still the first line of the result and still says what it always said.
func TestWriteFile_Execute_SyntaxTrailerFollowsTheResolvedTarget(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(real, "target.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(filepath.Join("real", "target.go"), filepath.Join(root, "code.go")); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	result, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "code.go", "content": "package main\n\nfunc main() {}\n}\n"}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	head, tail, found := strings.Cut(result.Content, "\n")
	if want := "wrote 31 bytes to code.go → resolves to " + realPath(t, filepath.Join(real, "target.go")); head != want {
		t.Errorf("first line = %q, want the unchanged disclosure %q", head, want)
	}
	if !found || !strings.HasPrefix(tail, "syntax check: ") {
		t.Errorf("Content = %q, want the trailer on the lines after the disclosure", result.Content)
	}
}
