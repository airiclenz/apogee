package present

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// recordingRunner is the fake behind Opener.Run: it captures the argv it was handed instead of
// running it (the point of the seam — the argv is the thing under test, and executing it would
// test the machine the suite runs on) and returns the error the test wants surfaced.
type recordingRunner struct {
	calls [][]string
	err   error
}

// run is the Runner the test installs on an Opener.
func (r *recordingRunner) run(name string, args ...string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	return r.err
}

// only returns the single argv the runner was called with, failing the test when the opener ran
// nothing or ran more than once.
func (r *recordingRunner) only(t *testing.T) []string {
	t.Helper()

	if len(r.calls) != 1 {
		t.Fatalf("runner called %d times, want exactly 1: %v", len(r.calls), r.calls)
	}
	return r.calls[0]
}

// The document path used throughout: absolute (as the tool resolves it) and containing a space,
// because argument boundaries are the one thing an opener can silently get wrong.
const testDocPath = "/workspace/my reports/review.html"

// Each desktop OS has exactly one opener command line, and the argv is asserted literally —
// these strings are the contract with three operating systems, not an implementation detail.
func TestOpenerBuildsThePlatformCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		goos string
		vars map[string]string
		want []string
	}{
		{
			name: "darwin opens with the LaunchServices opener",
			goos: "darwin",
			want: []string{"open", testDocPath},
		},
		{
			name: "windows goes through cmd's start, whose first argument is the window title",
			goos: "windows",
			want: []string{"cmd", "/c", "start", "", testDocPath},
		},
		{
			name: "linux with X11 opens with xdg-open",
			goos: "linux",
			vars: map[string]string{"DISPLAY": ":0"},
			want: []string{"xdg-open", testDocPath},
		},
		{
			name: "linux with Wayland opens with xdg-open too",
			goos: "linux",
			vars: map[string]string{"WAYLAND_DISPLAY": "wayland-0"},
			want: []string{"xdg-open", testDocPath},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runner := &recordingRunner{}
			opener := Opener{GOOS: tt.goos, Env: envFrom(tt.vars), Run: runner.run}

			if err := opener.Open(testDocPath); err != nil {
				t.Fatalf("Open() = %v, want no error", err)
			}
			if got := runner.only(t); !equalArgv(got, tt.want) {
				t.Errorf("Open() ran %q, want %q", got, tt.want)
			}
		})
	}
}

// A machine with no desktop to open into is a normal outcome, not a failure: Open reports the
// sentinel so the ladder degrades to the baseline rung, and it runs nothing at all.
func TestOpenerReportsNoOpenerWithoutADesktop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		goos string
		vars map[string]string
	}{
		{
			name: "headless linux (the devbox): no display server, no opener",
			goos: "linux",
			vars: map[string]string{"TERM": "xterm-256color"},
		},
		{
			name: "linux with blank display variables is still headless",
			goos: "linux",
			vars: map[string]string{"DISPLAY": "", "WAYLAND_DISPLAY": "   "},
		},
		{
			name: "an OS with no known opener",
			goos: "freebsd",
			vars: map[string]string{"DISPLAY": ":0"},
		},
		{
			name: "an unset goos (the zero-value Opener) opens nothing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runner := &recordingRunner{}
			opener := Opener{GOOS: tt.goos, Env: envFrom(tt.vars), Run: runner.run}

			err := opener.Open(testDocPath)
			if !errors.Is(err, ErrNoOpener) {
				t.Fatalf("Open() = %v, want ErrNoOpener", err)
			}
			if len(runner.calls) != 0 {
				t.Errorf("Open() ran %v, want nothing run", runner.calls)
			}
		})
	}
}

// The configured command replaces the OS opener everywhere, and the path is substituted AFTER
// the template is split — so the user's quoting fixes the argument boundaries and a path with a
// space in it can never become two arguments.
func TestOpenerCommandOverride(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		goos     string
		vars     map[string]string
		override string
		want     []string
	}{
		{
			name:     "the documented template: one placeholder, one argument",
			goos:     "darwin",
			override: "zed {path}",
			want:     []string{"zed", testDocPath},
		},
		{
			name:     "the override replaces the platform opener rather than joining it",
			goos:     "linux",
			vars:     map[string]string{"DISPLAY": ":0"},
			override: "zed {path}",
			want:     []string{"zed", testDocPath},
		},
		{
			name:     "it works on an OS that has no opener of its own",
			goos:     "freebsd",
			override: "xdg-open {path}",
			want:     []string{"xdg-open", testDocPath},
		},
		{
			name:     "and on a headless session, where the user has said how to show a document",
			goos:     "linux",
			override: "zed {path}",
			want:     []string{"zed", testDocPath},
		},
		{
			name:     "a quoted program with spaces stays one argument",
			goos:     "darwin",
			override: `"/Applications/My Editor.app/Contents/MacOS/edit" {path}`,
			want:     []string{"/Applications/My Editor.app/Contents/MacOS/edit", testDocPath},
		},
		{
			name:     "flags around the placeholder are preserved in order",
			goos:     "darwin",
			override: "open -a Preview {path}",
			want:     []string{"open", "-a", "Preview", testDocPath},
		},
		{
			name:     "the placeholder can sit inside an argument",
			goos:     "darwin",
			override: "zed --goto {path}:1",
			want:     []string{"zed", "--goto", testDocPath + ":1"},
		},
		{
			name:     "a template that never names the path gets it appended",
			goos:     "darwin",
			override: "zed",
			want:     []string{"zed", testDocPath},
		},
		{
			name:     "every occurrence is substituted",
			goos:     "darwin",
			override: "diffviewer {path} {path}",
			want:     []string{"diffviewer", testDocPath, testDocPath},
		},
		{
			name:     "a padded template is not mistaken for an unset one",
			goos:     "linux",
			override: "  zed {path}  ",
			want:     []string{"zed", testDocPath},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runner := &recordingRunner{}
			opener := Opener{
				GOOS:            tt.goos,
				Env:             envFrom(tt.vars),
				CommandOverride: tt.override,
				Run:             runner.run,
			}

			if err := opener.Open(testDocPath); err != nil {
				t.Fatalf("Open() = %v, want no error", err)
			}
			if got := runner.only(t); !equalArgv(got, tt.want) {
				t.Errorf("Open() ran %q, want %q", got, tt.want)
			}
		})
	}
}

// A template that cannot be parsed is a configuration mistake, and it must read as one: an
// error naming the template, and nothing launched with mangled arguments.
func TestOpenerRejectsAnUnusableOverride(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		override string
	}{
		{name: "an unbalanced quote is not a command line", override: `zed "{path}`},
		{name: "a template that names no program", override: `'' {path}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runner := &recordingRunner{}
			opener := Opener{GOOS: "darwin", CommandOverride: tt.override, Run: runner.run}

			err := opener.Open(testDocPath)
			if err == nil {
				t.Fatal("Open() = nil, want an error naming the template")
			}
			if errors.Is(err, ErrNoOpener) {
				t.Errorf("Open() = %v, want a configuration error rather than ErrNoOpener", err)
			}
			if !strings.Contains(err.Error(), "present.command") {
				t.Errorf("Open() = %v, want the message to name the config key", err)
			}
			if len(runner.calls) != 0 {
				t.Errorf("Open() ran %v, want nothing run", runner.calls)
			}
		})
	}
}

// An opener that was tried and failed is NOT the same as one that never existed: the runner's
// error surfaces (wrapped, so errors.Is still finds it) for the fail-visible handling upstream,
// and it must not be mistaken for ErrNoOpener.
func TestOpenerSurfacesARunnerFailure(t *testing.T) {
	t.Parallel()

	launchFailed := errors.New("exec: \"xdg-open\": executable file not found in $PATH")
	runner := &recordingRunner{err: launchFailed}
	opener := Opener{
		GOOS: "linux",
		Env:  envFrom(map[string]string{"DISPLAY": ":0"}),
		Run:  runner.run,
	}

	err := opener.Open(testDocPath)
	if !errors.Is(err, launchFailed) {
		t.Fatalf("Open() = %v, want the runner's error wrapped", err)
	}
	if errors.Is(err, ErrNoOpener) {
		t.Error("Open() reported ErrNoOpener for an opener that ran and failed")
	}
	if got := runner.only(t); !equalArgv(got, []string{"xdg-open", testDocPath}) {
		t.Errorf("Open() ran %q, want the platform opener", got)
	}
}

// The opener is given a resolved document path by the tool; a blank one means a caller lost it
// somewhere, and running `open ""` would be a mystery rather than a message.
func TestOpenerRejectsABlankPath(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{}
	opener := Opener{GOOS: "darwin", Run: runner.run}

	if err := opener.Open("   "); err == nil {
		t.Fatal("Open(\"   \") = nil, want an error")
	}
	if len(runner.calls) != 0 {
		t.Errorf("Open(\"   \") ran %v, want nothing run", runner.calls)
	}
}

// A nil Env is an empty environment, the same reading the detectors take: a Linux session with
// no display variables is headless, and the GUI OSes are unaffected.
func TestOpenerToleratesANilEnv(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{}
	if err := (Opener{GOOS: "linux", Run: runner.run}).Open(testDocPath); !errors.Is(err, ErrNoOpener) {
		t.Errorf("Open() on linux with a nil env = %v, want ErrNoOpener", err)
	}
	if err := (Opener{GOOS: "darwin", Run: runner.run}).Open(testDocPath); err != nil {
		t.Errorf("Open() on darwin with a nil env = %v, want no error", err)
	}
}

// The production runner is the one piece that really does start a process, so it is checked
// against real ones: a program that does not exist must report the failure (the fail-visible
// half), and a program that exits cleanly must come back nil rather than hang. The stand-in for
// "a launcher that exits promptly" is this very test binary, told to run a test that does not
// exist — portable to every OS the project builds for, unlike `true` or `cmd /c exit`.
func TestLaunchDetachedReportsWhatHappened(t *testing.T) {
	t.Parallel()

	if err := launchDetached(os.Args[0], "-test.run=TestLaunchDetachedNoSuchTest"); err != nil {
		t.Errorf("launchDetached(a program that exits 0) = %v, want no error", err)
	}
	if err := launchDetached("apogee-no-such-opener-binary"); err == nil {
		t.Error("launchDetached(a program that does not exist) = nil, want the launch failure")
	}
}

// equalArgv compares two command lines element by element.
func equalArgv(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
