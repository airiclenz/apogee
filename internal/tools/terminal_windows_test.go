//go:build windows

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Windows execution tests the POSIX-shell cases in terminal_test.go skip. They run a
// real cmd.exe: the quoting path is the one that cannot be proven by a table test, because
// what breaks is the boundary between os/exec's argv joining and cmd.exe's parser.

func TestTerminal_WindowsQuotedCommandReachesTheShellIntact(t *testing.T) {
	t.Parallel()

	// A model writes quotes constantly (`git commit -m "..."`). Joined as an argv, the
	// quotes arrive at cmd.exe escaped as \" and are echoed back literally; delivered as
	// the process command line they survive.
	term := NewTerminal(t.TempDir())
	res, err := term.Execute(context.Background(), terminalCall("c1", `echo "hello world"`))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("clean command produced an error result: %q", res.Content)
	}
	if !strings.Contains(res.Content, `"hello world"`) {
		t.Errorf("output = %q, want the quotes intact", res.Content)
	}
	if strings.Contains(res.Content, `\"`) {
		t.Errorf("output = %q, want no backslash-escaped quotes (os/exec's argv joining leaked)", res.Content)
	}
}

func TestTerminal_WindowsRedirectToAQuotedSpacedPath(t *testing.T) {
	t.Parallel()

	// The shape the confinement escape battery uses: a write redirected to a quoted path.
	// %USERPROFILE% routinely contains a space, so this is the ordinary case, not an edge.
	root := t.TempDir()
	dir := filepath.Join(root, "pro be")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	target := filepath.Join(dir, "out.txt")

	term := NewTerminal(root)
	res, err := term.Execute(context.Background(), terminalCall("c1", "echo x> "+shellHost.Quote(target)))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("redirect produced an error result: %q", res.Content)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("redirect to the quoted path %q did not land: %v", target, err)
	}
}

func TestTerminal_WindowsNonZeroExitIsErrorResult(t *testing.T) {
	t.Parallel()

	term := NewTerminal(t.TempDir())
	res, err := term.Execute(context.Background(), terminalCall("c1", "exit 3"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil (a non-zero exit is a result, not a Go error)", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "exit code 3") {
		t.Errorf("result = %q, want it to report exit code 3", res.Content)
	}
}
