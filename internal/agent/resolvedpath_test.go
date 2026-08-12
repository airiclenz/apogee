package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// The engine already resolves a write's true target — it is what the blast-radius ladder classifies
// the call by — and used to consume it as a bool and nothing else, leaving every surface that shows
// the call to quote the model's own argument. These two carriers are that same value surfaced:
// domain.ToolCallEvent.ResolvedPath for the card a human reads when no gate fires at all, and
// domain.ApprovalRequest.ResolvedPath for the pane when one does. Both are populated ONLY when the
// resolution differs from the path the argument names, so an ordinary call carries nothing extra
// and a Driver rendering the field unconditionally stays silent about ordinary writes.
func TestResolvedPathRidesTheCallAndTheApproval(t *testing.T) {
	t.Parallel()

	t.Run("a symlinked directory is disclosed on both carriers", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(ws, "docs")); err != nil {
			t.Skipf("symlinks unavailable on this host: %v", err)
		}
		// The path the model would write is the resolved one, not the one it named — the very
		// divergence the approval pane could not previously show.
		want := filepath.Join(realPath(t, outside), "notes.md")

		sink := &recordingSink{}
		conf := &fakeConfiner{caps: capsBoth()}
		cfg := autoConfigWS(sink, conf, true, ws, tools.NewWriteFile(ws))
		cfg.Approver = &fakeApprover{decision: domain.ApprovalDeny}

		driveToolCall(t, cfg, sink, "c1", "write_file", `{"path":"docs/notes.md","content":"hi"}`)

		if got := resolvedPathOnCall(t, sink.events); got != want {
			t.Errorf("ToolCallEvent.ResolvedPath = %q, want %q", got, want)
		}
		if got := resolvedPathOnApproval(t, sink.events); got != want {
			t.Errorf("ApprovalRequest.ResolvedPath = %q, want %q", got, want)
		}
	})

	t.Run("a path that names its own target discloses nothing", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()

		sink := &recordingSink{}
		conf := &fakeConfiner{caps: capsBoth()}
		cfg := autoConfigWS(sink, conf, true, ws, tools.NewWriteFile(ws))
		cfg.Approver = &fakeApprover{decision: domain.ApprovalAllow}

		driveToolCall(t, cfg, sink, "c1", "write_file", `{"path":"notes.md","content":"hi"}`)

		if got := resolvedPathOnCall(t, sink.events); got != "" {
			t.Errorf("ToolCallEvent.ResolvedPath = %q, want empty — the argument names its own target", got)
		}
	})

	t.Run("a tool with no write target discloses nothing", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()

		sink := &recordingSink{}
		conf := &fakeConfiner{caps: capsBoth()}
		cfg := autoConfigWS(sink, conf, true, ws, tools.NewReadFile(ws, nil))
		cfg.Approver = &fakeApprover{decision: domain.ApprovalAllow}

		driveToolCall(t, cfg, sink, "c1", "read_file", `{"path":"notes.md"}`)

		if got := resolvedPathOnCall(t, sink.events); got != "" {
			t.Errorf("ToolCallEvent.ResolvedPath = %q, want empty — read_file is not a workspace-scoped writer", got)
		}
	})
}

// resolvedPathOnCall returns the disclosure the first ToolCallEvent carried.
func resolvedPathOnCall(t *testing.T, events []domain.Event) string {
	t.Helper()
	for _, e := range events {
		if call, ok := e.(domain.ToolCallEvent); ok {
			return call.ResolvedPath
		}
	}
	t.Fatal("no ToolCallEvent was emitted")
	return ""
}

// resolvedPathOnApproval returns the disclosure the first ApprovalEvent's request carried — the
// request the Approver itself was handed, since dispatch emits the very value it sent.
func resolvedPathOnApproval(t *testing.T, events []domain.Event) string {
	t.Helper()
	for _, e := range events {
		if approval, ok := e.(domain.ApprovalEvent); ok {
			return approval.Request.ResolvedPath
		}
	}
	t.Fatal("no ApprovalEvent was emitted; the write did not gate")
	return ""
}

// realPath resolves p through symlinks with the stdlib rather than with the resolver under test, so
// a temp dir reached through a symlinked parent (macOS /tmp) compares equal for the right reason.
func realPath(t *testing.T, p string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", p, err)
	}
	return real
}
