package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// readCallFor builds a read_file call for the absolute path — the one argument these tests vary.
func readCallFor(t *testing.T, path string) domain.ToolCall {
	t.Helper()

	args, err := json.Marshal(map[string]any{"path": path})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return domain.ToolCall{ID: "c1", Tool: "read_file", Arguments: args}
}

// mountFixture lays out the two dirs every case here needs — a workspace and a directory outside
// it holding one file — and returns the workspace, the outside dir, and that file's absolute path.
func mountFixture(t *testing.T) (workspace, outside, file string) {
	t.Helper()

	workspace, outside = t.TempDir(), t.TempDir()
	file = filepath.Join(outside, "skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(file, []byte("bundled bytes"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return workspace, outside, file
}

// readThrough executes read_file out of registry against path and returns its content, failing
// the test when the tool is missing or returns a Go error (a refusal comes back as content).
func readThrough(t *testing.T, registry *domain.ToolRegistry, path string) (content string, isErr bool) {
	t.Helper()

	tool, ok := registry.Lookup("read_file")
	if !ok {
		t.Fatal("read_file is not in the registry")
	}
	result, err := tool.Execute(context.Background(), readCallFor(t, path))
	if err != nil {
		t.Fatalf("read_file returned a Go error: %v", err)
	}
	return result.Content, result.IsError
}

// TestResolveToolsMountsExtraReadRoots is the engine half of the wiring: a Config carrying
// ExtraReadRoots yields a default tool set whose read_file reads under that root, and the same
// Config without it yields one that refuses the very same path. The engine never defaults the
// field — mounting is the host's act, and nothing here knows the root happens to hold skills.
func TestResolveToolsMountsExtraReadRoots(t *testing.T) {
	t.Parallel()

	workspace, outside, file := mountFixture(t)

	mounted := resolveTools(domain.Config{
		WorkspaceDir:   workspace,
		ExtraReadRoots: func() []string { return []string{outside} },
	})
	content, isErr := readThrough(t, mounted, file)
	if isErr {
		t.Fatalf("read under the mounted root failed: %q", content)
	}
	if !strings.Contains(content, "bundled bytes") {
		t.Errorf("content %q does not carry the file under the mounted root", content)
	}

	unmounted := resolveTools(domain.Config{WorkspaceDir: workspace})
	content, isErr = readThrough(t, unmounted, file)
	if !isErr {
		t.Fatalf("read_file read outside the workspace with no root mounted: %q", content)
	}
	if strings.Contains(content, "bundled bytes") {
		t.Errorf("refusal leaked the file's bytes: %q", content)
	}
}

// TestSubsetInheritsExtraReadRoots pins the claim the wiring rests on: a sub-agent needs no
// mounting of its own. A child's registry is a Subset of the parent's tool INSTANCES, so the
// parent's read_file — and with it the mount it was built with — is the very object the child
// dispatches, at every depth.
func TestSubsetInheritsExtraReadRoots(t *testing.T) {
	t.Parallel()

	workspace, outside, file := mountFixture(t)

	parent := resolveTools(domain.Config{
		WorkspaceDir:   workspace,
		ExtraReadRoots: func() []string { return []string{outside} },
	})
	content, isErr := readThrough(t, parent.Subset("read_file"), file)
	if isErr {
		t.Fatalf("a Subset's read_file lost the mount: %q", content)
	}
	if !strings.Contains(content, "bundled bytes") {
		t.Errorf("content %q does not carry the file under the mounted root", content)
	}
}

// TestExtraReadRootsAreLiveThroughTheEngine pins that the engine carries the func rather than the
// dirs: the mount is evaluated per call, so a host that changes what it mounts mid-session — the
// `use-project-skills` flip behind the skill source dirs — is honoured by the next read, with no
// reconstruction of the Agent or its registry.
func TestExtraReadRootsAreLiveThroughTheEngine(t *testing.T) {
	t.Parallel()

	workspace, outside, file := mountFixture(t)

	var mounted bool
	registry := resolveTools(domain.Config{
		WorkspaceDir: workspace,
		ExtraReadRoots: func() []string {
			if !mounted {
				return nil
			}
			return []string{outside}
		},
	})

	if content, isErr := readThrough(t, registry, file); !isErr {
		t.Fatalf("read succeeded before the host mounted anything: %q", content)
	}

	mounted = true
	content, isErr := readThrough(t, registry, file)
	if isErr {
		t.Fatalf("read failed after the host mounted the root: %q", content)
	}
	if !strings.Contains(content, "bundled bytes") {
		t.Errorf("content %q does not carry the file under the newly mounted root", content)
	}
}
