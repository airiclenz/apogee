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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// realUserHome is the process's home as it was BEFORE TestMain overrode it — the value the
// guard test asserts nothing resolves to any more. Empty when the OS could not name one.
var realUserHome string

// suiteTempHome is the throwaway home TestMain created for THIS process, kept so a test that
// exits without returning through m.Run can still remove it (keysource_test.go's re-exec
// fixture). Empty before TestMain runs.
var suiteTempHome string

// e2eBinary is the apogee binary the black-box driver runs (internal/tuitest.PTYDriver): the
// SHIPPED shape of this tree, built once per `go test` invocation by TestMain into the suite's own
// temp home. Empty when the build failed, in which case e2eBuildErr says why and every PTY test
// skips with it — a test that cannot build the binary is not a test that found a bug in it.
var (
	e2eBinary   string
	e2eBuildErr error
)

// TestMain gives the whole suite a throwaway home directory. `t.TempDir` belongs to a running
// test, so the dir is an `os.MkdirTemp` removed after `m.Run` — and the exit code is taken
// before the removal, because `os.Exit` runs no deferred functions. A test that calls os.Exit
// itself never reaches that removal, so it must remove suiteTempHome on its own way out.
func TestMain(m *testing.M) {
	// The same interception main() opens with, and for the same reason: on Linux a confined
	// subprocess is run by re-invoking THIS executable in the landlock helper mode
	// (confinement-execution-contract §2.3 / §2.6), and under the in-process driver this
	// executable is the test binary. Without it the helper child falls through to m.Run and a
	// driven Auto run gets the whole suite's output back as its command's, instead of the
	// command's. It MUST stay the first thing TestMain does, ahead of the binary build below.
	maybeDispatchConfinedExec()

	realUserHome, _ = os.UserHomeDir()

	home, err := os.MkdirTemp("", "apogee-cmd-test-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmd/apogee suite: create temp home: %v\n", err)
		os.Exit(1)
	}
	suiteTempHome = home
	// HOME is what os.UserHomeDir reads on POSIX, USERPROFILE what it reads on Windows.
	_ = os.Setenv("HOME", home)
	_ = os.Setenv("USERPROFILE", home)

	// Built for every ordinary run of the suite — never for the key-command fixture's re-exec of
	// this binary (keysource_test.go), which happens several times per run and only ever prints a
	// key. Building there multiplies one build by every re-exec, measured at +10 s on the package.
	if !keyFixtureInvocation() {
		e2eBinary, e2eBuildErr = buildE2EBinary(home)
	}

	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}

// keyFixtureInvocation reports whether this process is keysource_test.go re-invoking the test
// binary as an `api-key-cmd:` program rather than an ordinary run of the suite. The marker is the
// last argument, exactly as the fixture itself reads it.
func keyFixtureInvocation() bool {
	return len(os.Args) > 0 && strings.HasPrefix(os.Args[len(os.Args)-1], keyFixtureMarker)
}

// buildE2EBinary compiles the package under test into dir and returns the binary's path. It is
// built unconditionally rather than only when a PTY test is selected: it is one cached `go build`
// of a package the suite has already compiled, and a conditional build is a second thing to get
// wrong. `-race` is deliberately off — what the black-box driver drives is the binary as it ships.
//
// The build runs with the DEVELOPER's home rather than the suite's throwaway one. HOME is where the
// Go build cache and the module cache live, and building under a fresh temp home would re-resolve
// the module graph and recompile the world on every `go test` — minutes, per run, for nothing.
// exec keeps the last definition of a duplicated variable, so appending is how the real home wins.
func buildE2EBinary(dir string) (string, error) {
	bin := filepath.Join(dir, "apogee")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = os.Environ()
	if realUserHome != "" {
		cmd.Env = append(cmd.Env, "HOME="+realUserHome, "USERPROFILE="+realUserHome)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build the binary under test: %w\n%s", err, out)
	}
	return bin, nil
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
