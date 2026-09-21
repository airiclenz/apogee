//go:build !windows

package tools

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// readFileDeadline is how long a read_file call may take before the test calls it wedged: a
// blocking open of a FIFO with no writer never returns, which is the failure this file pins.
const readFileDeadline = 2 * time.Second

// TestReadFile_RefusesAFIFO is the audit's wedge at the tool boundary: a named pipe planted in
// the workspace no longer blocks read_file — the call returns promptly with the refusal spelled
// as SafeOpen spells it, never as "file not found", which would invite a retry.
func TestReadFile_RefusesAFIFO(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Skipf("mkfifo unsupported here: %v", err)
	}
	call := callWith(t, "c1", map[string]any{"path": "pipe"})
	ctx, cancel := context.WithTimeout(context.Background(), readFileDeadline)
	defer cancel()
	done := make(chan struct{})
	var result struct {
		content string
		isError bool
		err     error
	}

	go func() {
		defer close(done)
		r, err := NewReadFile(root, ReadMounts{}).Execute(ctx, call)
		result.content, result.isError, result.err = r.Content, r.IsError, err
	}()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("read_file on a FIFO did not return within %v: the open blocked", readFileDeadline)
	}

	if result.err != nil {
		t.Fatalf("Execute returned a Go error: %v", result.err)
	}
	if !result.isError {
		t.Fatalf("IsError = false, want true (content: %q)", result.content)
	}
	if want := "not a regular file: pipe"; result.content != want {
		t.Errorf("content = %q, want %q", result.content, want)
	}
}
