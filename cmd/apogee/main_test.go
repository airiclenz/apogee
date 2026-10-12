package main

// The suite's home. Tests that wire a runtime with `ConfigDir: ""` resolve the apogee home
// through `os.UserHomeDir()`, which — left alone — is the developer's real `~`: a `go test`
// run littered it with empty `~/.apogee/scratch/<id>/` dirs, one per wiring. TestMain points
// the home at a temp dir for the whole binary so no test can reach the real one, and
// TestNoTestWritesTheRealApogeeHome guards the override against a future test file that
// resets HOME to the real home. The same TestMain clears the developer's ambient `APOGEE_*`
// configuration once for the binary — TestAmbientApogeeConfigIsClearedForTheSuite guards
// that — so the launch helpers assert it is absent instead of each `t.Setenv`-ing it away. It
// also puts the session's config watcher on its fast cadence once, for the same reason: the seam
// is a package var, and a per-launch swap of it is a write that parallel tests would race on.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/config"
)

// ambientApogeeEnv is the `APOGEE_*` configuration the developer's shell may carry into the
// suite: every one of them would silently reshape a driven run — APOGEE_CONFIG would move the
// home out from under --config's own resolution order — so TestMain unsets them once for the
// binary. APOGEE_API_KEY is deliberately absent: no launch helper reads it, and the key tests
// set it themselves.
var ambientApogeeEnv = []string{
	config.EnvConfig, config.EnvServer, config.EnvEndpoint, config.EnvModel, config.EnvMode,
	config.EnvBypass, config.EnvWorkspace,
}

// suiteNoUpdateCheck is the APOGEE_NO_UPDATE_CHECK value TestMain sets for the whole binary and
// ptyEnv hands every PTY launch: any non-empty value turns the boot update check off.
const suiteNoUpdateCheck = "1"

// realUserHome is the process's home as it was BEFORE TestMain overrode it — the value the
// guard test asserts nothing resolves to any more. Empty when the OS could not name one.
var realUserHome string

// suiteTempHome is the throwaway home TestMain created for THIS process, kept so a test that
// exits without returning through m.Run can still remove it (keysource_test.go's re-exec
// fixture). Empty before TestMain runs.
var suiteTempHome string

// e2eBinary is the apogee binary the black-box driver runs (internal/tuitest.PTYDriver): the
// SHIPPED shape of this tree. TestMain takes it prebuilt from APOGEE_TEST_E2E_BINARY when the
// variable is set (scripts/test-shards.sh builds it once for every chunk of the run), and otherwise
// builds it once per `go test` invocation into the suite's own temp home. Empty when that build
// failed, in which case e2eBuildErr says why and every PTY test skips with it — a test that cannot
// build the binary is not a test that found a bug in it.
var (
	e2eBinary   string
	e2eBuildErr error
)

// e2eBinaryEnv names a prebuilt binary under test for TestMain to use instead of building its
// own. It is the test runner's handshake with this package, not apogee configuration: it stays out
// of ambientApogeeEnv (whose guard asserts each name empty), and no driven launch inherits it,
// since ptyEnv states the child's environment.
const e2eBinaryEnv = "APOGEE_TEST_E2E_BINARY"

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
	// Once, here, rather than per launch: a `t.Setenv` in a launch helper is what kept every
	// e2e test out of `t.Parallel` (the testing package panics on the pair). A test that wants
	// one of these set still `t.Setenv`s it and stays serial.
	for _, name := range ambientApogeeEnv {
		_ = os.Unsetenv(name)
	}
	// And the one variable the suite SETS rather than clears: the boot update check off for every
	// driven launch, so no test sends a request to the real release server (ADR 0097). It stays out
	// of ambientApogeeEnv, whose guard asserts each name empty; a test of the enabled path clears it
	// with `t.Setenv(config.EnvNoUpdateCheck, "")` and stays serial.
	_ = os.Setenv(config.EnvNoUpdateCheck, suiteNoUpdateCheck)
	// The config watcher's cadence, likewise once for the binary rather than per launch. The
	// production one — a poll a second plus a quarter-second settle (internal/filewatch) — is the
	// right number for a human saving a document and a second and a half of the suite's budget for
	// every save a driver makes; the one test that saves (T-16's watcher step) is why the seam
	// exists, and a run that never touches config.yaml pays nothing for the fast one. Set here and
	// never restored: production never reassigns the seam, and a per-test swap with a t.Cleanup
	// restore was the write that kept every driven launch out of t.Parallel.
	configWatchTiming = watchTiming{Interval: 50 * time.Millisecond, Settle: 50 * time.Millisecond}

	// Built for every ordinary run of the suite — never for the key-command fixture's re-exec of
	// this binary (keysource_test.go), which happens several times per run and only ever prints a
	// key. Building there multiplies one build by every re-exec, measured at +10 s on the package.
	// Nor for a run that only LISTS the tests (`go test -list`, which scripts/test-shards.sh plans
	// its shards with): nothing runs, so nothing needs the binary, and the build was most of that
	// listing's cost.
	//
	// A run whose runner already built the binary names it in APOGEE_TEST_E2E_BINARY, and every
	// process of that run uses the one file. A variable that names no binary is the runner's bug,
	// so it fails the run here, loudly, rather than setting e2eBuildErr: that would skip every PTY
	// test, and a run of skips reads green.
	if !keyFixtureInvocation() && !listingOnly(os.Args[1:]) {
		prebuilt, err := prebuiltE2EBinary(os.Getenv(e2eBinaryEnv))
		if err != nil {
			fmt.Fprintf(os.Stderr, "cmd/apogee suite: %v\n", err)
			_ = os.RemoveAll(home)
			os.Exit(1)
		}
		if prebuilt != "" {
			e2eBinary = prebuilt
		} else {
			e2eBinary, e2eBuildErr = buildE2EBinary(home)
		}
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

// listingOnly reports whether args — the test binary's own, after its name — ask it only to name its
// tests: `go test -list` hands the binary -test.list, and the testing package then prints the names
// and runs nothing. Either dash spelling, with the pattern after `=` or as the next argument.
func listingOnly(args []string) bool {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			continue // a flag's value, or an argument after the flags, never the flag itself
		}
		if name, _, _ := strings.Cut(strings.TrimLeft(arg, "-"), "="); name == "test.list" {
			return true
		}
	}
	return false
}

// prebuiltE2EBinary resolves value, the APOGEE_TEST_E2E_BINARY setting, to the binary it names:
// "" when the variable is unset or empty (TestMain then builds its own), the path made absolute when
// it names an existing regular file — absolute because a test may change the working directory
// under a relative one — and an error naming the variable and the path when it names nothing
// usable.
func prebuiltE2EBinary(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("%s=%q: resolve the path: %w", e2eBinaryEnv, value, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s=%q names no binary: %w", e2eBinaryEnv, value, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s=%q is not a regular file", e2eBinaryEnv, value)
	}
	return path, nil
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

// TestListingOnlyReadsTheListFlag pins what keeps a `go test -list` from building the binary under
// test, and what keeps every other run building it: only -test.list, in any of the spellings the
// flag package accepts, is a listing.
func TestListingOnlyReadsTheListFlag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		args []string
		want bool
	}{
		{args: []string{"-test.list=.", "-test.paniconexit0"}, want: true},
		{args: []string{"-test.paniconexit0", "-test.list", "^TestE2E"}, want: true},
		{args: []string{"--test.list=."}, want: true},
		{args: []string{"-test.paniconexit0", "-test.run=^TestE2ESmokeInProcess$"}, want: false},
		{args: []string{"-test.run", "test.list"}, want: false},
		{args: nil, want: false},
	}
	for _, tc := range cases {
		if got := listingOnly(tc.args); got != tc.want {
			t.Errorf("listingOnly(%q) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

// TestE2EBinaryFromEnvSelectsThePrebuiltBinary pins TestMain's choice of the binary under test: an
// unset APOGEE_TEST_E2E_BINARY builds as before, one naming a file uses that file, and one naming
// nothing is an error that names the variable and the path — never a silent fall-back to a build,
// and never a binary path that a launch would then fail to exec.
func TestE2EBinaryFromEnvSelectsThePrebuiltBinary(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	existing := filepath.Join(dir, "apogee")
	if err := os.WriteFile(existing, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write the stand-in binary: %v", err)
	}
	missing := filepath.Join(dir, "absent", "apogee")

	t.Run("unset", func(t *testing.T) {
		t.Parallel()

		got, err := prebuiltE2EBinary("")
		if err != nil || got != "" {
			t.Fatalf("prebuiltE2EBinary(\"\") = %q, %v; want \"\", nil (build as before)", got, err)
		}
	})
	t.Run("set to an existing file", func(t *testing.T) {
		t.Parallel()

		got, err := prebuiltE2EBinary(existing)
		if err != nil || got != existing {
			t.Fatalf("prebuiltE2EBinary(%q) = %q, %v; want the file itself", existing, got, err)
		}
	})
	t.Run("set to a missing file", func(t *testing.T) {
		t.Parallel()

		got, err := prebuiltE2EBinary(missing)
		if err == nil {
			t.Fatalf("prebuiltE2EBinary(%q) = %q, nil; want an error", missing, got)
		}
		for _, want := range []string{e2eBinaryEnv, missing} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not name %q", err, want)
			}
		}
	})
	t.Run("set to a directory", func(t *testing.T) {
		t.Parallel()

		if _, err := prebuiltE2EBinary(dir); err == nil || !strings.Contains(err.Error(), e2eBinaryEnv) {
			t.Fatalf("prebuiltE2EBinary(%q) error = %v; want one naming %s", dir, err, e2eBinaryEnv)
		}
	})
}

// TestAmbientApogeeConfigIsClearedForTheSuite asserts the suite starts every test with no
// `APOGEE_*` configuration in its environment: it fails the moment TestMain stops unsetting
// one, or a test file exports one and leaves it there.
func TestAmbientApogeeConfigIsClearedForTheSuite(t *testing.T) {
	for _, name := range ambientApogeeEnv {
		if value := os.Getenv(name); value != "" {
			t.Errorf("%s=%q is in the suite's environment; TestMain unsets it so every launch helper can assume it is absent", name, value)
		}
	}
}
