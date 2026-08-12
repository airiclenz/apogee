package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func TestWriteFile_Execute_CreatesFileAndParents(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

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

	root := t.TempDir()
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

	root := t.TempDir()

	result, err := NewWriteFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "out.txt", "content": "hello"}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if want := "wrote 5 bytes to out.txt"; result.Content != want {
		t.Errorf("Content = %q, want %q", result.Content, want)
	}
	wrote, ok := result.Summary.(domain.WroteBytes)
	if !ok {
		t.Fatalf("Summary = %#v, want a domain.WroteBytes", result.Summary)
	}
	if want := (domain.WroteBytes{Bytes: 5}); wrote != want {
		t.Errorf("Summary = %+v, want %+v", wrote, want)
	}
}

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

	root := t.TempDir()
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

	root := t.TempDir()
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

	root := t.TempDir()
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
