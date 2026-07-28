package agent

// Item 1 of the workspace-context-files plan: the loader, the construction-time name gate, and
// the session-scoped cache. Nothing here reaches the wire yet (that is item 2) — these tests pin
// WHAT is discovered and WHEN it is re-read: at a session boundary and nowhere else.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// contextConfig returns a baseConfig rooted at dir and looking for names — the construction
// surface every boundary test below drives.
func contextConfig(sink domain.EventSink, dir string, names ...string) domain.Config {
	cfg := baseConfig(sink)
	cfg.WorkspaceDir = dir
	cfg.ContextFiles = names
	return cfg
}

// cachedContent returns the cached content for name, or "" when the cache holds no entry for it
// — enough to prove which bytes a session is holding without pinning the whole cache shape.
func cachedContent(files []contextFile, name string) string {
	for _, f := range files {
		if f.name == name {
			return f.content
		}
	}
	return ""
}

// TestLoadContextFiles pins the discovery policy: found files carry verbatim bytes in list
// order, missing and empty ones leave no trace, and a present-but-unreadable one is kept as an
// error entry so the host can report the skip out loud.
func TestLoadContextFiles(t *testing.T) {
	t.Parallel()

	type wantEntry struct {
		name    string
		content string
		wantErr bool
	}
	tests := []struct {
		title string
		files map[string]string // files written into the workspace before loading
		dirs  []string          // names created as DIRECTORIES: present, but unreadable
		names []string
		want  []wantEntry
	}{
		{
			title: "a found file carries its bytes verbatim",
			files: map[string]string{"AGENTS.md": "# Guide\nBraces {{workspace}} are data.\n"},
			names: []string{"AGENTS.md"},
			want:  []wantEntry{{name: "AGENTS.md", content: "# Guide\nBraces {{workspace}} are data.\n"}},
		},
		{
			title: "a missing file is skipped with no trace",
			names: []string{"AGENTS.md"},
		},
		{
			title: "a present-but-unreadable file is kept as an error entry",
			dirs:  []string{"AGENTS.md"},
			names: []string{"AGENTS.md"},
			want:  []wantEntry{{name: "AGENTS.md", wantErr: true}},
		},
		{
			title: "an empty file is skipped with no trace",
			files: map[string]string{"AGENTS.md": ""},
			names: []string{"AGENTS.md"},
		},
		{
			title: "a whitespace-only file is skipped with no trace",
			files: map[string]string{"AGENTS.md": "  \n\t\n"},
			names: []string{"AGENTS.md"},
		},
		{
			title: "every found file is included, in LIST order, skipping the misses",
			files: map[string]string{
				"AGENTS.md":          "agents",
				"CONVENTIONS.md":     "conventions",
				"docs/NESTED.md":     "nested",
				"NOT-IN-THE-LIST.md": "ignored",
			},
			names: []string{"CONVENTIONS.md", "MISSING.md", "AGENTS.md", filepath.Join("docs", "NESTED.md")},
			want: []wantEntry{
				{name: "CONVENTIONS.md", content: "conventions"},
				{name: "AGENTS.md", content: "agents"},
				{name: filepath.Join("docs", "NESTED.md"), content: "nested"},
			},
		},
		{
			title: "no names means nothing is read",
			files: map[string]string{"AGENTS.md": "agents"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.title, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for name, content := range tc.files {
				writeWorkspaceFile(t, dir, name, content)
			}
			for _, name := range tc.dirs {
				if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
					t.Fatalf("MkdirAll %q: %v", name, err)
				}
			}

			got := loadContextFiles(dir, tc.names)

			if len(got) != len(tc.want) {
				t.Fatalf("loadContextFiles returned %d entries, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, want := range tc.want {
				switch {
				case got[i].name != want.name:
					t.Errorf("entry %d name = %q, want %q", i, got[i].name, want.name)
				case (got[i].err != nil) != want.wantErr:
					t.Errorf("entry %d err = %v, wantErr = %v", i, got[i].err, want.wantErr)
				case got[i].content != want.content:
					t.Errorf("entry %d content = %q, want %q", i, got[i].content, want.content)
				case got[i].size != len(got[i].content):
					t.Errorf("entry %d size = %d, want %d (the content's byte length)", i, got[i].size, len(got[i].content))
				}
			}
		})
	}
}

// TestLoadContextFilesInertWithoutWorkspace: an embedder with no workspace has no root to
// resolve a relative name against, so the loader stays inert rather than reaching for the
// process working directory.
func TestLoadContextFilesInertWithoutWorkspace(t *testing.T) {
	t.Parallel()

	if got := loadContextFiles("", []string{"AGENTS.md"}); got != nil {
		t.Errorf("loadContextFiles with no workspace returned %+v, want nil", got)
	}
}

// TestNewAgentRejectsBadContextFileName: the engine's defense-in-depth gate — an empty,
// absolute, or workspace-escaping name fails construction with an error naming the offender,
// exactly as an unknown prompt placeholder does.
func TestNewAgentRejectsBadContextFileName(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tests := []struct {
		title string
		name  string
	}{
		{"an empty name", ""},
		{"an absolute path", filepath.Join(workspace, "AGENTS.md")},
		{"a parent escape", filepath.Join("..", "secrets.md")},
		{"an escape hidden mid-path", filepath.Join("docs", "..", "..", "secrets.md")},
	}

	for _, tc := range tests {
		t.Run(tc.title, func(t *testing.T) {
			t.Parallel()
			cfg := contextConfig(&recordingSink{}, workspace, "AGENTS.md", tc.name)

			_, err := newAgent(cfg, &recordingResponder{reply: "unused"})

			if err == nil {
				t.Fatalf("newAgent accepted the context-file name %q; a bad name must fail construction", tc.name)
			}
			if !strings.Contains(err.Error(), "ContextFiles") {
				t.Errorf("error %q does not name the offending config surface", err)
			}
			if tc.name != "" && !strings.Contains(err.Error(), tc.name) {
				t.Errorf("error %q does not name the offending entry %q", err, tc.name)
			}
		})
	}
}

// TestContextFilesLoadedAtConstruction: construction is the first session boundary, so a
// workspace context file is in the cache before the first request is ever built.
func TestContextFilesLoadedAtConstruction(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", "first")

	a, err := newAgent(contextConfig(&recordingSink{}, dir, "AGENTS.md"), echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if got := cachedContent(a.contextFiles, "AGENTS.md"); got != "first" {
		t.Errorf("cached content = %q, want %q", got, "first")
	}
}

// TestContextFilesRereadOnClearContext: a mid-session edit never swaps the content under a
// running session (KV-cache stability), and /clear — a session boundary — picks it up.
func TestContextFilesRereadOnClearContext(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", "first")
	a, err := newAgent(contextConfig(&recordingSink{}, dir, "AGENTS.md"), echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	writeWorkspaceFile(t, dir, "AGENTS.md", "second")

	if got := cachedContent(a.contextFiles, "AGENTS.md"); got != "first" {
		t.Fatalf("cached content = %q after a mid-session edit, want the session's original %q", got, "first")
	}
	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext: %v", err)
	}
	if got := cachedContent(a.contextFiles, "AGENTS.md"); got != "second" {
		t.Errorf("cached content = %q after ClearContext, want the re-read %q", got, "second")
	}
}

// TestContextFilesUntouchedByRefusedClearContext: a refused boundary is not a boundary — a
// mid-Exchange ClearContext changes neither the conversation nor the cache.
func TestContextFilesUntouchedByRefusedClearContext(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", "first")
	a, err := newAgent(contextConfig(&recordingSink{}, dir, "AGENTS.md"), echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	writeWorkspaceFile(t, dir, "AGENTS.md", "second")
	a.turns.inExchange = true // a mid-Exchange boundary — re-reading here would swap content mid-task

	if err := a.ClearContext(); !errors.Is(err, domain.ErrInputPending) {
		t.Fatalf("ClearContext mid-Exchange = %v, want ErrInputPending", err)
	}

	if got := cachedContent(a.contextFiles, "AGENTS.md"); got != "first" {
		t.Errorf("cached content = %q after a REFUSED ClearContext, want the untouched %q", got, "first")
	}
}

// TestContextFilesRereadOnRestoreSession: a live restore starts a new session, so it re-reads
// the CURRENT files (the resolved-live posture — content is never serialized); a refused
// restore leaves the cache exactly as it was.
func TestContextFilesRereadOnRestoreSession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", "first")
	a, err := newAgent(contextConfig(&recordingSink{}, dir, "AGENTS.md"), echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "one"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	writeWorkspaceFile(t, dir, "AGENTS.md", "second")

	// A refused restore (a snapshot newer than this build) is not a boundary.
	future := domain.Session{Version: domain.SessionVersion + 1, State: snap.State}
	if err := a.RestoreSession(future); !errors.Is(err, domain.ErrSessionVersion) {
		t.Fatalf("RestoreSession of a future snapshot = %v, want ErrSessionVersion", err)
	}
	if got := cachedContent(a.contextFiles, "AGENTS.md"); got != "first" {
		t.Errorf("cached content = %q after a REFUSED restore, want the untouched %q", got, "first")
	}

	if err := a.RestoreSession(snap); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}
	if got := cachedContent(a.contextFiles, "AGENTS.md"); got != "second" {
		t.Errorf("cached content = %q after RestoreSession, want the re-read %q", got, "second")
	}
}

// TestSubAgentInheritsParentContextFiles: a sub-agent belongs to the parent's session, so it is
// handed the parent's bytes — even when the file is gone from disk by the time it spawns.
func TestSubAgentInheritsParentContextFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", "first")
	a, err := newAgent(contextConfig(&recordingSink{}, dir, "AGENTS.md"), echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	child, err := a.newChildAgent()
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}

	if got := cachedContent(child.contextFiles, "AGENTS.md"); got != "first" {
		t.Errorf("sub-agent cached content = %q, want the parent session's %q (copied, never re-read)", got, "first")
	}
}
