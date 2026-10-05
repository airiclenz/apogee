//go:build !windows

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fifoReadDeadline is how long a read of a FIFO source may take before a test calls it wedged. A
// blocking open of a FIFO with no writer never returns, so the pre-fix failure is this deadline,
// not an error.
const fifoReadDeadline = 5 * time.Second

// fifoWorkspace is an os.Root-pinned workspace — the FS the Runner reads through — holding a named
// pipe called "pipe" that no one ever writes to.
func fifoWorkspace(t *testing.T) *os.Root {
	t.Helper()

	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o600); err != nil {
		t.Skipf("mkfifo unsupported here: %v", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("open the workspace root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

// errWithin runs read under fifoReadDeadline and fails the test when it does not return in time.
func errWithin(t *testing.T, what string, read func() error) error {
	t.Helper()

	done := make(chan error, 1)
	go func() { done <- read() }()
	select {
	case err := <-done:
		return err
	case <-time.After(fifoReadDeadline):
		t.Fatalf("%s did not return within %v: the read blocked on the FIFO", what, fifoReadDeadline)
		return nil
	}
}

func TestExpandRefusesALinesFIFOWithoutBlocking(t *testing.T) {
	t.Parallel()
	root := fifoWorkspace(t)

	err := errWithin(t, "Expand", func() error {
		_, err := Expand(ItemSource{Lines: "pipe"}, root.FS(), 100)
		return err
	})

	if err == nil || !strings.Contains(err.Error(), "lines: pipe:") || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("Expand of a FIFO err = %v, want a refusal naming lines: pipe as not a regular file", err)
	}
}

func TestItemKeyRefusesAContextFIFOWithoutBlocking(t *testing.T) {
	t.Parallel()
	root := fifoWorkspace(t)
	item := Item{Label: "a.go", Units: []string{"a.go"}}

	err := errWithin(t, "ItemKey", func() error {
		_, err := ItemKey("audit {item}", item, []string{"pipe"}, root.FS())
		return err
	})

	if err == nil || !strings.Contains(err.Error(), `context file "pipe"`) || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("ItemKey with a FIFO context file err = %v, want a refusal naming the file as not a regular file", err)
	}
}
