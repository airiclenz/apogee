package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// callWith builds a ToolCall whose Arguments is the JSON encoding of args.
func callWith(t *testing.T, id string, args map[string]any) domain.ToolCall {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return domain.ToolCall{ID: id, Arguments: raw}
}

func TestReadFile_Execute(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("line1\nline2\nline3"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tool := NewReadFile(root)

	cases := []struct {
		name        string
		args        map[string]any
		wantErr     bool
		wantContain string
	}{
		{
			name:        "reads full file with a header",
			args:        map[string]any{"path": "hello.txt"},
			wantContain: "line1\nline2\nline3",
		},
		{
			name:        "header reports total line count",
			args:        map[string]any{"path": "hello.txt"},
			wantContain: "3 lines total",
		},
		{
			name:        "line range narrows output",
			args:        map[string]any{"path": "hello.txt", "start_line": 2, "end_line": 2},
			wantContain: "line2",
		},
		{
			name:        "missing file is a tool error",
			args:        map[string]any{"path": "absent.txt"},
			wantErr:     true,
			wantContain: "file not found",
		},
		{
			name:        "path escape is a tool error",
			args:        map[string]any{"path": "../escape.txt"},
			wantErr:     true,
			wantContain: "outside the workspace",
		},
		{
			name:        "missing path argument is a tool error",
			args:        map[string]any{},
			wantErr:     true,
			wantContain: "path is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := tool.Execute(context.Background(), callWith(t, "c1", tc.args))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if result.IsError != tc.wantErr {
				t.Fatalf("IsError = %v, want %v (content: %q)", result.IsError, tc.wantErr, result.Content)
			}
			if !strings.Contains(result.Content, tc.wantContain) {
				t.Errorf("content %q does not contain %q", result.Content, tc.wantContain)
			}
		})
	}
}

func TestReadFile_Execute_RejectsRangeOnLineTwo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("a\nb\nc\nd"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := NewReadFile(root).Execute(context.Background(),
		callWith(t, "c1", map[string]any{"path": "f.txt", "start_line": 2, "end_line": 3}))

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if strings.Contains(result.Content, "\na\n") || strings.Contains(result.Content, "\nd") {
		t.Errorf("range leaked lines outside 2-3: %q", result.Content)
	}
	if !strings.Contains(result.Content, "b\nc") {
		t.Errorf("range did not include lines 2-3: %q", result.Content)
	}
}

func TestReadFile_Execute_HonoursCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewReadFile(t.TempDir()).Execute(ctx, callWith(t, "c1", map[string]any{"path": "x"}))

	if err == nil {
		t.Fatalf("Execute on a cancelled ctx returned nil error, want ctx error")
	}
}
