package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestMarkerAccessors_NonMarkerTool proves the marker accessors report a non-marker tool
// as NOT a workspace-scoped writer: IsWorkspaceScopedWriter is false and
// WorkspaceWriteTarget yields ("", false). read_file is read-only and structurally does
// not satisfy the unexported workspaceScopedWriter marker, so dispatch must not treat it
// as an Apogee-own path-safety-bounded write.
func TestMarkerAccessors_NonMarkerTool(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ro := NewReadFile(root) // read-only; carries no marker

	if IsWorkspaceScopedWriter(ro) {
		t.Error("IsWorkspaceScopedWriter(read_file) = true, want false (read-only tool is not a workspace-scoped writer)")
	}

	call := domain.ToolCall{ID: "c1", Tool: "read_file", Arguments: []byte(`{"path":"file.txt"}`)}
	abs, ok := WorkspaceWriteTarget(ro, call)
	if ok || abs != "" {
		t.Errorf("WorkspaceWriteTarget(read_file) = (%q, %v), want (\"\", false) for a non-marker tool", abs, ok)
	}
}

// TestMarkerAccessors_MarkerTool is the positive contrast: write_file DOES carry the
// marker, so IsWorkspaceScopedWriter is true and WorkspaceWriteTarget resolves the call's
// target path. This guards against a regression where the accessors stop recognising a
// genuine marker carrier (which would wrongly route an in-workspace write through gating).
func TestMarkerAccessors_MarkerTool(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	w := NewWriteFile(root) // carries the workspaceScopedWriter marker

	if !IsWorkspaceScopedWriter(w) {
		t.Fatal("IsWorkspaceScopedWriter(write_file) = false, want true (write_file is an Apogee-own workspace-scoped writer)")
	}

	call := domain.ToolCall{ID: "c1", Tool: "write_file", Arguments: []byte(`{"path":"file.txt","content":"x"}`)}
	abs, ok := WorkspaceWriteTarget(w, call)
	if !ok || abs == "" {
		t.Errorf("WorkspaceWriteTarget(write_file) = (%q, %v), want a resolved in-workspace target", abs, ok)
	}
}

// TestWriteTargetsAgreeOnPath is what makes the shared decode safe. All four
// workspace-scoped writers resolve their target through one body (pathArgWriteTarget),
// which decodes a minimal {"path":…} struct rather than each tool's own args type — sound
// only while every write tool spells the argument "path". If one ever renames it, the
// shared decode would silently yield ok=false and dispatch would mis-classify an
// out-of-workspace write as in-bounds; this fails first instead. The negative rows pin the
// other half of the contract: nothing inspectable must yield ("", false), never a bare root.
func TestWriteTargetsAgreeOnPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	target := filepath.Join(root, "sub", "f.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// The oracle is the stdlib's own symlink resolution rather than the resolver under
	// test, so a temp dir reached through a symlinked parent (macOS /tmp) compares equal
	// for the right reason.
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	writers := []struct {
		name string
		tool domain.Tool
	}{
		{"write_file", NewWriteFile(root)},
		{"single_find_and_replace", NewSingleFindReplace(root)},
		{"multi_find_and_replace", NewMultiFindReplace(root)},
		{"edit_existing_file", NewEditExistingFile(root)},
	}

	cases := []struct {
		name    string
		args    string
		wantAbs string
		wantOK  bool
	}{
		{name: "path resolves against the root", args: `{"path":"sub/f.txt"}`, wantAbs: want, wantOK: true},
		{name: "undecodable arguments", args: `{"path":`, wantAbs: "", wantOK: false},
		{name: "empty path", args: `{"path":""}`, wantAbs: "", wantOK: false},
	}

	for _, w := range writers {
		for _, tc := range cases {
			t.Run(w.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				call := domain.ToolCall{ID: "c1", Tool: w.name, Arguments: []byte(tc.args)}
				abs, ok := WorkspaceWriteTarget(w.tool, call)
				if abs != tc.wantAbs || ok != tc.wantOK {
					t.Errorf("WorkspaceWriteTarget(%s, %s) = (%q, %v), want (%q, %v)",
						w.name, tc.args, abs, ok, tc.wantAbs, tc.wantOK)
				}
			})
		}
	}
}
