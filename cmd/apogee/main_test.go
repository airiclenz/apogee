package main

// The suite's home. Tests that wire a runtime with `ConfigDir: ""` resolve the apogee home
// through `os.UserHomeDir()`, which — left alone — is the developer's real `~`: a `go test`
// run littered it with empty `~/.apogee/scratch/<id>/` dirs, one per wiring. TestMain points
// the home at a temp dir for the whole binary so no test can reach the real one, and
// TestNoTestWritesTheRealApogeeHome guards the override against a future test file that
// resets HOME to the real home.

import (
	"fmt"
	"os"
	"testing"
)

// realUserHome is the process's home as it was BEFORE TestMain overrode it — the value the
// guard test asserts nothing resolves to any more. Empty when the OS could not name one.
var realUserHome string

// suiteTempHome is the throwaway home TestMain created for THIS process, kept so a test that
// exits without returning through m.Run can still remove it (keysource_test.go's re-exec
// fixture). Empty before TestMain runs.
var suiteTempHome string

// TestMain gives the whole suite a throwaway home directory. `t.TempDir` belongs to a running
// test, so the dir is an `os.MkdirTemp` removed after `m.Run` — and the exit code is taken
// before the removal, because `os.Exit` runs no deferred functions. A test that calls os.Exit
// itself never reaches that removal, so it must remove suiteTempHome on its own way out.
func TestMain(m *testing.M) {
	realUserHome, _ = os.UserHomeDir()

	home, err := os.MkdirTemp("", "apogee-cmd-test-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmd/apogee suite: create temp home: %v\n", err)
		os.Exit(1)
	}
	suiteTempHome = home
	// HOME is what os.UserHomeDir reads on POSIX, USERPROFILE what it reads on Windows.
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)

	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// TestNoTestWritesTheRealApogeeHome asserts the suite still resolves its apogee home somewhere
// other than the developer's own: it fails the moment a test file points HOME back at the real
// home and leaves it there.
func TestNoTestWritesTheRealApogeeHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve home directory: %v", err)
	}

	if realUserHome != "" && home == realUserHome {
		t.Fatalf("suite home is the real user home %q — a test reset HOME; every ConfigDir:\"\" wiring now writes to ~/.apogee", home)
	}
}
