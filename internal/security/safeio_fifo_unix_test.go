//go:build !windows

package security

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// safeOpenDeadline is how long a SafeOpen may take before a test calls it wedged. A blocking
// open of a FIFO with no writer never returns, so the pre-fix failure is this deadline, not an
// error.
const safeOpenDeadline = 2 * time.Second

// safeOpenOutcome is one SafeOpen call's result, carried out of the goroutine the deadline runs it in.
type safeOpenOutcome struct {
	file *os.File
	err  error
}

// safeOpenWithin runs SafeOpen under safeOpenDeadline and fails the test when it does not return in
// time — the wedge the FIFO case pins.
func safeOpenWithin(t *testing.T, root, input string) (*os.File, error) {
	t.Helper()

	done := make(chan safeOpenOutcome, 1)
	go func() {
		f, err := SafeOpen(root, input)
		done <- safeOpenOutcome{file: f, err: err}
	}()
	select {
	case out := <-done:
		return out.file, out.err
	case <-time.After(safeOpenDeadline):
		t.Fatalf("SafeOpen(%q) did not return within %v: the open blocked", input, safeOpenDeadline)
		return nil, nil
	}
}

// TestSafeOpen_RefusesAFIFOWithoutBlocking is the audit's wedge: a named pipe planted in the
// workspace, opened read-only with no writer, blocks a plain open forever. SafeOpen opens
// non-blocking, fstats the descriptor and refuses the pipe with ErrNotRegular, promptly. Fails
// against the pre-fix code on the deadline.
func TestSafeOpen_RefusesAFIFOWithoutBlocking(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Skipf("mkfifo unsupported here: %v", err)
	}

	f, err := safeOpenWithin(t, root, "pipe")

	if f != nil {
		_ = f.Close()
		t.Fatal("SafeOpen returned a handle onto a FIFO, want a refusal")
	}
	if !errors.Is(err, ErrNotRegular) {
		t.Fatalf("SafeOpen(fifo) err = %v, want ErrNotRegular", err)
	}
	if want := "not a regular file: pipe"; err.Error() != want {
		t.Errorf("error text = %q, want %q", err.Error(), want)
	}
}

// TestSafeOpen_RegularFileStillReadsBlocking proves the non-blocking open costs a regular file
// nothing: the flag stays on the descriptor, and read(2) ignores it there, so a 1 MiB body comes
// back whole through io.ReadAll with no EAGAIN surfacing.
func TestSafeOpen_RegularFileStillReadsBlocking(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	body := bytes.Repeat([]byte{'x'}, 1<<20)
	if err := os.WriteFile(filepath.Join(root, "big.bin"), body, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	f, err := SafeOpen(root, "big.bin")
	if err != nil {
		t.Fatalf("SafeOpen: %v", err)
	}
	defer func() { _ = f.Close() }()
	got, err := io.ReadAll(f)

	if err != nil {
		t.Fatalf("read through the handle: %v (EAGAIN would mean the non-blocking flag reached a read)", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("read %d bytes, want %d identical bytes", len(got), len(body))
	}
}

// TestSafeOpen_DirectoryStillOpens pins that a directory is not refused by the non-regular gate —
// statInRoot, list_dir, path_suggest and directoryFiles all open directories through SafeOpen —
// and that getdents ignores the non-blocking flag: the listing comes back through the handle.
func TestSafeOpen_DirectoryStillOpens(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "dir", "entry"), []byte("e"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	f, err := SafeOpen(root, "dir")
	if err != nil {
		t.Fatalf("SafeOpen(dir): %v", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		t.Fatalf("fstat: %v", err)
	}
	entries, err := f.ReadDir(-1)

	if !info.IsDir() {
		t.Errorf("fstat mode = %v, want a directory", info.Mode())
	}
	if err != nil {
		t.Fatalf("ReadDir through the handle: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "entry" {
		t.Errorf("entries = %v, want exactly [entry]", entries)
	}
}

// TestSafeOpen_RefusesASocket pins the socket outcome: a UNIX socket in the workspace never
// reaches SafeOpen's fstat gate, because open(2) refuses it first (ENXIO on Linux) — so the
// error is an ordinary I/O failure, not ErrNotRegular and not an escape, and the read tools render
// their absent wording for it. The call must also return promptly: nothing here may block.
func TestSafeOpen_RefusesASocket(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	listener, err := net.Listen("unix", filepath.Join(root, "sock"))
	if err != nil {
		t.Skipf("unix sockets unsupported here: %v", err)
	}
	defer func() { _ = listener.Close() }()

	f, err := safeOpenWithin(t, root, "sock")

	if f != nil {
		_ = f.Close()
		t.Fatal("SafeOpen returned a handle onto a socket, want a refusal")
	}
	if err == nil {
		t.Fatal("SafeOpen(socket) err = nil, want the open(2) refusal")
	}
	if errors.Is(err, ErrNotRegular) || errors.Is(err, ErrPathEscape) {
		t.Errorf("SafeOpen(socket) err = %v, want the bare open(2) refusal (neither ErrNotRegular nor ErrPathEscape)", err)
	}
	if runtime.GOOS == "linux" && !errors.Is(err, syscall.ENXIO) {
		t.Errorf("SafeOpen(socket) err = %v, want ENXIO from open(2)", err)
	}
}
