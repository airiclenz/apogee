package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFixtureFile writes a fixture file, creating its parent directory.
func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// realPath renders path as the resolved real path resolve answers with, so a box whose temp
// dir is reached through a symlink (macOS /tmp) does not fail the comparison. A path that
// does not exist is resolved through its parent, as the guard itself resolves one.
func realPath(t *testing.T, path string) string {
	t.Helper()

	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		t.Fatalf("eval symlinks %s: %v", filepath.Dir(path), err)
	}
	return filepath.Join(parent, filepath.Base(path))
}

// tempRoot is t.TempDir() with symlinks resolved by the same rule realPath uses. A root reached
// through a symlink (macOS /tmp) does not equal what a tool resolves it to, so every path under it
// carries a resolution note and every path comparison inside the safety fence is made against a
// name the fence never produces — both break on that box alone.
//
// The package rule: a suite whose paths can reach a tool's BARE success sentence or the safety
// fence takes its workspace from tempRoot — that is the whole write family (write_file, file_edit,
// find_replace, the file operations, read_file, the approved-escape permits) plus this file's own
// fixtures. Every other suite here holds its workspace incidentally — registry, terminal, git,
// python, grep, find_files, list_dir, diff, network, exec, present_document, workspace-scoped and
// sub-agent among them — and needs no resolution: its assertions never depend on the root's
// spelling, so raw t.TempDir() stays correct there.
func tempRoot(t *testing.T) string {
	t.Helper()

	return realPath(t, t.TempDir())
}
