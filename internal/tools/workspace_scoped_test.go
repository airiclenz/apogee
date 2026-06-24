package tools

import (
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
