//go:build !windows

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/testtime"
)

// editToolDeadline is how long a read-modify-write tool call may take before the test calls it
// wedged: a blocking open of a FIFO with no writer never returns, which is the failure the FIFO
// cases here pin.
var editToolDeadline = testtime.Budget(2 * time.Second)

// editToolOverCapBytes is one byte past security.SafeReadFileCapped's 64 MiB bound — the size of
// the sparse fixture the over-cap cases plant.
const editToolOverCapBytes int64 = 64<<20 + 1

// executeWithin runs tool on args under editToolDeadline and fails the test when the call does not
// return in time, or returns a Go error, or succeeds. It answers the refusal the model would read.
func executeWithin(t *testing.T, tool domain.Tool, args map[string]any) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), editToolDeadline)
	defer cancel()
	type outcome struct {
		result domain.ToolResult
		err    error
	}
	done := make(chan outcome, 1)

	go func() {
		result, err := tool.Execute(ctx, callWith(t, "c1", args))
		done <- outcome{result: result, err: err}
	}()
	var got outcome
	select {
	case got = <-done:
	case <-ctx.Done():
		t.Fatalf("%T did not return within %v: the open blocked", tool, editToolDeadline)
	}

	if got.err != nil {
		t.Fatalf("Execute returned a Go error: %v", got.err)
	}
	if !got.result.IsError {
		t.Fatalf("IsError = false, want true (content: %q)", got.result.Content)
	}
	return got.result.Content
}

// plantWorkspaceFIFO makes a named pipe called name under root, skipping where mkfifo is
// unsupported.
func plantWorkspaceFIFO(t *testing.T, root, name string) {
	t.Helper()

	if err := syscall.Mkfifo(filepath.Join(root, name), 0o600); err != nil {
		t.Skipf("mkfifo unsupported here: %v", err)
	}
}

// plantOverCapFile makes a sparse file called name under root, one byte over the capped read's
// bound, so the fixture costs no disk.
func plantOverCapFile(t *testing.T, root, name string) {
	t.Helper()

	f, err := os.Create(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := f.Truncate(editToolOverCapBytes); err != nil {
		_ = f.Close()
		t.Fatalf("setup: truncate: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("setup: close: %v", err)
	}
}

// overCapRefusal is the wording a capped read's refusal reaches the model in.
func overCapRefusal() string {
	return fmt.Sprintf("file too large: %d bytes (max %d)", editToolOverCapBytes, int64(64<<20))
}

// TestEditExistingFile_RefusesAFIFOWithoutBlocking is the audit's wedge at the edit tool: a named
// pipe planted in the workspace blocked the edit's read forever. The call now returns promptly
// with the refusal spelled as security spells it, never as "file not found".
func TestEditExistingFile_RefusesAFIFOWithoutBlocking(t *testing.T) {
	t.Parallel()

	root := tempRoot(t)
	plantWorkspaceFIFO(t, root, "pipe")

	got := executeWithin(t, NewEditExistingFile(root), map[string]any{"path": "pipe", "content": "x"})

	if want := "not a regular file: pipe"; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

// TestEditExistingFile_RefusesAnOverCapFile pins the edit tool's bound: a file over 64 MiB is
// refused in the read tools' own wording rather than read whole into memory.
func TestEditExistingFile_RefusesAnOverCapFile(t *testing.T) {
	root := tempRoot(t)
	plantOverCapFile(t, root, "huge.txt")

	got := executeWithin(t, NewEditExistingFile(root), map[string]any{"path": "huge.txt", "content": "x"})

	if want := overCapRefusal(); got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

// TestSingleFindReplace_RefusesAFIFOAndAnOverCapFile pins both refusals at single_find_and_replace.
func TestSingleFindReplace_RefusesAFIFOAndAnOverCapFile(t *testing.T) {
	root := tempRoot(t)
	plantWorkspaceFIFO(t, root, "pipe")
	plantOverCapFile(t, root, "huge.txt")
	tool := NewSingleFindReplace(root)

	pipe := executeWithin(t, tool, map[string]any{"path": "pipe", "oldText": "a", "newText": "b"})
	huge := executeWithin(t, tool, map[string]any{"path": "huge.txt", "oldText": "a", "newText": "b"})

	if want := "not a regular file: pipe"; pipe != want {
		t.Errorf("FIFO content = %q, want %q", pipe, want)
	}
	if want := overCapRefusal(); huge != want {
		t.Errorf("over-cap content = %q, want %q", huge, want)
	}
}

// TestMultiFindReplace_RefusesAFIFOAndAnOverCapFile pins both refusals at multi_find_and_replace.
func TestMultiFindReplace_RefusesAFIFOAndAnOverCapFile(t *testing.T) {
	root := tempRoot(t)
	plantWorkspaceFIFO(t, root, "pipe")
	plantOverCapFile(t, root, "huge.txt")
	tool := NewMultiFindReplace(root)
	replacements := []map[string]any{{"oldText": "a", "newText": "b"}}

	pipe := executeWithin(t, tool, map[string]any{"path": "pipe", "replacements": replacements})
	huge := executeWithin(t, tool, map[string]any{"path": "huge.txt", "replacements": replacements})

	if want := "not a regular file: pipe"; pipe != want {
		t.Errorf("FIFO content = %q, want %q", pipe, want)
	}
	if want := overCapRefusal(); huge != want {
		t.Errorf("over-cap content = %q, want %q", huge, want)
	}
}

// TestDiagnostics_RefusesAFIFOAndAnOverCapFile pins both refusals at diagnostics: the Go source is
// read through the capped variant, so a planted pipe cannot wedge it and a huge file cannot be read
// whole into the parser.
func TestDiagnostics_RefusesAFIFOAndAnOverCapFile(t *testing.T) {
	root := tempRoot(t)
	plantWorkspaceFIFO(t, root, "pipe.go")
	plantOverCapFile(t, root, "huge.go")
	tool := NewDiagnostics(root)

	pipe := executeWithin(t, tool, map[string]any{"path": "pipe.go", "vet": false})
	huge := executeWithin(t, tool, map[string]any{"path": "huge.go", "vet": false})

	if want := "not a regular file: pipe.go"; pipe != want {
		t.Errorf("FIFO content = %q, want %q", pipe, want)
	}
	if want := overCapRefusal(); huge != want {
		t.Errorf("over-cap content = %q, want %q", huge, want)
	}
}
