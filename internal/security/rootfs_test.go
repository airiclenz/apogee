package security

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// RootFS keeps every read inside its folder: a file and a relative symlink that stays inside
// read; a symlink resolving outside — relative or absolute — and an absolute symlink resolving
// inside are refused without their bytes.
func TestRootFSFencesItsFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	t.Parallel()

	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	writeRootFSFile(t, outside, "HOST SECRET")
	writeRootFSFile(t, filepath.Join(dir, "sub", "real.md"), "in the folder")
	rel, err := filepath.Rel(dir, outside)
	if err != nil {
		t.Fatal(err)
	}
	symlinkRootFS(t, filepath.Join("sub", "real.md"), filepath.Join(dir, "relative-inside.md"))
	symlinkRootFS(t, rel, filepath.Join(dir, "relative-escape.md"))
	symlinkRootFS(t, outside, filepath.Join(dir, "absolute-escape.md"))
	symlinkRootFS(t, filepath.Join(dir, "sub", "real.md"), filepath.Join(dir, "absolute-inside.md"))

	files := RootFS(dir)
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "a plain file reads", path: "sub/real.md"},
		{name: "a relative in-root symlink is followed", path: "relative-inside.md"},
		{name: "a relative escaping symlink is refused", path: "relative-escape.md", wantErr: true},
		{name: "an absolute escaping symlink is refused", path: "absolute-escape.md", wantErr: true},
		{name: "an absolute in-root symlink is refused", path: "absolute-inside.md", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, err := fs.ReadFile(files, tt.path)
			if tt.wantErr {
				if err == nil || len(body) != 0 {
					t.Errorf("ReadFile(%s) = %q, %v; want a refusal without the bytes", tt.path, body, err)
				}
				return
			}
			if err != nil || string(body) != "in the folder" {
				t.Errorf("ReadFile(%s) = %q, %v; want the folder's file", tt.path, body, err)
			}
		})
	}
}

// A folder that does not open still yields an FS — one whose every Open fails with the open
// error — so a reader holding it never mistakes it for "no folder" and falls back to another tree.
func TestRootFSServesTheOpenErrorForAMissingFolder(t *testing.T) {
	t.Parallel()

	files := RootFS(filepath.Join(t.TempDir(), "missing"))
	if files == nil {
		t.Fatal("RootFS(missing folder) = nil; want an FS that refuses every read")
	}
	for _, name := range []string{"p.md", "."} {
		if _, err := files.Open(name); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Open(%s) on a missing folder = %v; want its not-exist error", name, err)
		}
	}
}

// writeRootFSFile writes content to path, creating its parent folders.
func writeRootFSFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// symlinkRootFS creates link pointing at target.
func symlinkRootFS(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}
