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

// TestResolvedPathAgreesWithTheResultForEveryWriteKey carries the disclosure above past the one
// tool that names its target `path` and writes it directly. Three more writers reach a resolved
// target by other spellings — copy_file and move_file write their `destination`, delete_file
// unlinks its `path` — and each states that target a SECOND time in its own success sentence
// (file_ops.go, resolvedTargetNote). Two independent statements of one fact drift apart silently,
// so this pins them together: the pane a human approves, the card a Driver renders, and the
// sentence the model reads all name the same EvalSymlinks-resolved file. The redirect stays
// inside the workspace on purpose — the divergence is what is under test, not the fence — and
// Ask-Before is what gates every write, so the approval carrier is populated for all three.
func TestResolvedPathAgreesWithTheResultForEveryWriteKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		newTool  func(ws string) domain.Tool
		tool     string
		args     string
		sentence string
	}{
		{
			name:     "copy_file discloses its resolved destination",
			newTool:  func(ws string) domain.Tool { return tools.NewCopyFile(ws, nil) },
			tool:     "copy_file",
			args:     `{"source":"payload.txt","destination":"docs/notes.md","overwrite":true}`,
			sentence: "copied payload.txt to docs/notes.md",
		},
		{
			name:     "move_file discloses its resolved destination",
			newTool:  func(ws string) domain.Tool { return tools.NewMoveFile(ws) },
			tool:     "move_file",
			args:     `{"source":"payload.txt","destination":"docs/notes.md","overwrite":true}`,
			sentence: "moved payload.txt to docs/notes.md",
		},
		{
			name:     "delete_file discloses its resolved path",
			newTool:  func(ws string) domain.Tool { return tools.NewDeleteFile(ws) },
			tool:     "delete_file",
			args:     `{"path":"docs/notes.md"}`,
			sentence: "deleted docs/notes.md",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ws := t.TempDir()
			want := realPath(t, redirectedWriteFixture(t, ws))

			sink := &recordingSink{}
			cfg := configWithTools(sink, tc.newTool(ws))
			cfg.Mode = domain.ModeAskBefore
			cfg.WorkspaceDir = ws
			cfg.Approver = &fakeApprover{decision: domain.ApprovalAllow}

			driveToolCall(t, cfg, sink, "c1", tc.tool, tc.args)

			if got := resolvedPathOnCall(t, sink.events); got != want {
				t.Errorf("ToolCallEvent.ResolvedPath = %q, want %q", got, want)
			}
			if got := resolvedPathOnApproval(t, sink.events); got != want {
				t.Errorf("ApprovalRequest.ResolvedPath = %q, want %q", got, want)
			}
			res, ok := lastToolResult(sink.events)
			if !ok {
				t.Fatal("no ToolResultEvent was emitted; the approved call did not execute")
			}
			if res.IsError {
				t.Fatalf("tool errored: %q", res.Content)
			}
			if wantSentence := tc.sentence + " → resolves to " + want; res.Content != wantSentence {
				t.Errorf("result = %q, want %q — the sentence must name the same target the carriers do", res.Content, wantSentence)
			}
		})
	}
}

// redirectedWriteFixture builds a workspace whose `docs/notes.md` is a symlink to `store/notes.md`
// — a LEAF link, the shape the writers reach success through (a mutated symlinked PARENT is
// refused) — alongside the `payload.txt` the copy and the move take as their source. It returns
// the redirect target, the path every carrier must name once the resolution runs.
func redirectedWriteFixture(t *testing.T, ws string) string {
	t.Helper()
	for _, dir := range []string{"store", "docs"} {
		if err := os.MkdirAll(filepath.Join(ws, dir), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	target := filepath.Join(ws, "store", "notes.md")
	if err := os.WriteFile(target, []byte("redirect target\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "payload.txt"), []byte("payload\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(filepath.Join("..", "store", "notes.md"), filepath.Join(ws, "docs", "notes.md")); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}
	return target
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
