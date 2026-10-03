package present

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/security"
)

// clipboardBinDir is the directory the fake PATH lookup answers with: absolute on every OS and
// outside any workspace root a test builds, so a row resolves with nothing for the fence to refuse.
var clipboardBinDir = filepath.Join(os.TempDir(), "apogee-clipboard-bin")

// clipboardRun is one call a recording StdinRunner saw: the argv, what was fed to stdin, and
// whether the run carried a deadline.
type clipboardRun struct {
	argv        []string
	stdin       string
	hasDeadline bool
}

// recordRuns returns a StdinRunner that appends every call to runs and returns err.
func recordRuns(runs *[]clipboardRun, err error) StdinRunner {
	return func(ctx context.Context, stdin, program string, args ...string) error {
		_, hasDeadline := ctx.Deadline()
		*runs = append(*runs, clipboardRun{
			argv:        append([]string{program}, args...),
			stdin:       stdin,
			hasDeadline: hasDeadline,
		})
		return err
	}
}

// lookInstalled is a PATH lookup on which exactly the named programs exist, in clipboardBinDir;
// every other name is exec's own not-found error.
func lookInstalled(installed ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, program := range installed {
			if program == name {
				return filepath.Join(clipboardBinDir, name), nil
			}
		}
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
}

// inBin is argv with its program rewritten to the absolute path lookInstalled resolves it to.
func inBin(argv ...string) string {
	return strings.Join(append([]string{filepath.Join(clipboardBinDir, argv[0])}, argv[1:]...), " ")
}

// plantProgram writes an executable named name inside root/.venv/bin and returns its path — the
// shape a model-written virtualenv on PATH takes.
func plantProgram(t *testing.T, root, name string) string {
	t.Helper()
	bin := filepath.Join(root, ".venv", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", bin, err)
	}
	planted := filepath.Join(bin, name)
	if err := os.WriteFile(planted, []byte("#!/bin/sh\nexec /bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", planted, err)
	}
	return planted
}

// TestClipboardWriteSystemRunsTheFirstInstalledCandidate pins atotto/clipboard v0.1.4's ladder and
// argv, resolved: the first candidate the OS has on PATH runs — its absolute path as argv[0] — with
// the copied text on stdin, and no other candidate runs.
func TestClipboardWriteSystemRunsTheFirstInstalledCandidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		goos      string
		vars      map[string]string
		installed []string
		wantArgv  string
	}{
		{
			name:      "wayland session with wl-copy",
			goos:      "linux",
			vars:      map[string]string{"WAYLAND_DISPLAY": "wayland-0"},
			installed: []string{"wl-copy", "xclip"},
			wantArgv:  inBin("wl-copy"),
		},
		{
			name:      "wayland session without wl-copy falls to xclip",
			goos:      "linux",
			vars:      map[string]string{"WAYLAND_DISPLAY": "wayland-0"},
			installed: []string{"xclip"},
			wantArgv:  inBin("xclip", "-in", "-selection", "clipboard"),
		},
		{
			name:      "no wayland skips wl-copy",
			goos:      "linux",
			installed: []string{"wl-copy", "xclip", "xsel"},
			wantArgv:  inBin("xclip", "-in", "-selection", "clipboard"),
		},
		{
			name:      "xsel when xclip is missing",
			goos:      "freebsd",
			installed: []string{"xsel"},
			wantArgv:  inBin("xsel", "--input", "--clipboard"),
		},
		{
			name:      "termux",
			goos:      "android",
			installed: []string{"termux-clipboard-set"},
			wantArgv:  inBin("termux-clipboard-set"),
		},
		{
			name:      "WSL's clip.exe last",
			goos:      "linux",
			installed: []string{"clip.exe"},
			wantArgv:  inBin("clip.exe"),
		},
		{
			name:      "darwin pbcopy",
			goos:      "darwin",
			installed: []string{"pbcopy", "xclip"},
			wantArgv:  inBin("pbcopy"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var runs []clipboardRun
			board := Clipboard{
				GOOS:          tt.goos,
				Env:           envFrom(tt.vars),
				WorkspaceRoot: t.TempDir(),
				LookPath:      lookInstalled(tt.installed...),
				Run:           recordRuns(&runs, nil),
			}

			err := board.WriteSystem("héllo\nwörld")

			if err != nil {
				t.Fatalf("WriteSystem() = %v, want nil", err)
			}
			if len(runs) != 1 {
				t.Fatalf("runner called %d times (%v), want once", len(runs), runs)
			}
			if got := strings.Join(runs[0].argv, " "); got != tt.wantArgv {
				t.Errorf("argv = %q, want %q", got, tt.wantArgv)
			}
			if runs[0].stdin != "héllo\nwörld" {
				t.Errorf("stdin = %q, want the copied text", runs[0].stdin)
			}
		})
	}
}

// TestClipboardWriteSystemReportsNoProgram pins the absent outcome: an OS with nothing installed,
// or one with no program route at all (Windows writes through the Win32 API), answers
// ErrNoClipboardProgram and runs nothing.
func TestClipboardWriteSystemReportsNoProgram(t *testing.T) {
	t.Parallel()

	for _, goos := range []string{"linux", "darwin", "windows", "plan9"} {
		t.Run(goos, func(t *testing.T) {
			t.Parallel()
			var runs []clipboardRun
			board := Clipboard{GOOS: goos, LookPath: lookInstalled(), Run: recordRuns(&runs, nil)}

			err := board.WriteSystem("hello")

			if !errors.Is(err, ErrNoClipboardProgram) {
				t.Fatalf("WriteSystem() = %v, want ErrNoClipboardProgram", err)
			}
			if len(runs) != 0 {
				t.Fatalf("runner called %d times (%v), want never", len(runs), runs)
			}
		})
	}
}

// TestClipboardRefusesAProgramInsideTheWorkspace pins the fence on both routes: a clipboard helper
// or a tmux that PATH resolves inside the workspace — or to a relative path, which the child would
// re-resolve against a working directory that is usually the workspace — is refused with the exec
// fence's error and nothing runs. The system walk stops at the refusal rather than quietly running
// the next candidate.
func TestClipboardRefusesAProgramInsideTheWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	plantProgram(t, root, "xclip")
	plantProgram(t, root, "tmux")
	planted := func(name string) (string, error) {
		if name == "xsel" {
			return filepath.Join(clipboardBinDir, name), nil
		}
		return filepath.Join(root, ".venv", "bin", name), nil
	}
	relative := func(name string) (string, error) { return filepath.Join("bin", name), exec.ErrDot }

	for _, look := range []struct {
		name string
		look func(string) (string, error)
	}{{"planted in the workspace", planted}, {"relative PATH entry", relative}} {
		t.Run(look.name, func(t *testing.T) {
			t.Parallel()
			var runs []clipboardRun
			board := Clipboard{
				GOOS:          "linux",
				Env:           envFrom(map[string]string{"TMUX": "/tmp/tmux-1000/default,1,0"}),
				WorkspaceRoot: root,
				LookPath:      look.look,
				Run:           recordRuns(&runs, nil),
			}

			systemErr := board.WriteSystem("hello")
			tmuxErr := board.WriteTmux("hello")

			if !errors.Is(systemErr, security.ErrExecFromWritablePath) {
				t.Errorf("WriteSystem() = %v, want the exec fence's refusal", systemErr)
			}
			if !errors.Is(tmuxErr, security.ErrExecFromWritablePath) {
				t.Errorf("WriteTmux() = %v, want the exec fence's refusal", tmuxErr)
			}
			if len(runs) != 0 {
				t.Errorf("runner called %d times (%v), want never", len(runs), runs)
			}
		})
	}
}

// TestTmuxClipboardStartsNothingOutsideTmux pins the gate: with TMUX empty or unset, a copy neither
// looks tmux up nor starts it, and reports nothing.
func TestTmuxClipboardStartsNothingOutsideTmux(t *testing.T) {
	t.Parallel()
	var runs []clipboardRun
	looked := false
	board := Clipboard{
		GOOS:     "linux",
		Env:      envFrom(nil),
		LookPath: func(string) (string, error) { looked = true; return "", exec.ErrNotFound },
		Run:      recordRuns(&runs, nil),
	}

	err := board.WriteTmux("hello")

	if err != nil {
		t.Fatalf("WriteTmux() outside tmux = %v, want nil", err)
	}
	if looked || len(runs) != 0 {
		t.Fatalf("outside tmux: looked up = %v, runs = %v; want neither", looked, runs)
	}
}

// TestTmuxClipboardLoadsTheBufferInsideTmux pins the route itself: inside tmux the copy runs
// exactly the resolved `tmux load-buffer -w -`, once, under a deadline, with the copied text
// byte-for-byte on stdin — multi-byte and multi-line text included.
func TestTmuxClipboardLoadsTheBufferInsideTmux(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"hello", "héllo wörld ✓ 世界", "line one\nline two\n"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			var runs []clipboardRun
			board := Clipboard{
				Env:           envFrom(map[string]string{"TMUX": "/tmp/tmux-1000/default,1,0"}),
				WorkspaceRoot: t.TempDir(),
				LookPath:      lookInstalled("tmux"),
				Run:           recordRuns(&runs, nil),
			}

			err := board.WriteTmux(text)

			if err != nil {
				t.Fatalf("WriteTmux() inside tmux = %v, want nil", err)
			}
			if len(runs) != 1 {
				t.Fatalf("inside tmux the runner was called %d times, want once", len(runs))
			}
			if got, want := strings.Join(runs[0].argv, " "), inBin("tmux", "load-buffer", "-w", "-"); got != want {
				t.Errorf("argv = %q, want %q", got, want)
			}
			if runs[0].stdin != text {
				t.Errorf("stdin = %q, want the copied text %q", runs[0].stdin, text)
			}
			if !runs[0].hasDeadline {
				t.Error("the tmux run carries no deadline — a wedged tmux server would hold the Cmd forever")
			}
		})
	}
}

// TestTmuxClipboardReportsARunnerFailure pins that a failed run, and a tmux missing from PATH, reach
// the caller — swallowing them is the TUI Cmd's job, not this type's.
func TestTmuxClipboardReportsARunnerFailure(t *testing.T) {
	t.Parallel()
	refused := errors.New("exit status 1")
	inTmux := envFrom(map[string]string{"TMUX": "/tmp/tmux-1000/default,1,0"})
	var runs []clipboardRun

	failed := Clipboard{Env: inTmux, LookPath: lookInstalled("tmux"), Run: recordRuns(&runs, refused)}.WriteTmux("hello")
	missing := Clipboard{Env: inTmux, LookPath: lookInstalled(), Run: recordRuns(&runs, nil)}.WriteTmux("hello")

	if !errors.Is(failed, refused) {
		t.Errorf("WriteTmux() with a failing run = %v, want it to wrap %v", failed, refused)
	}
	if !errors.Is(missing, exec.ErrNotFound) {
		t.Errorf("WriteTmux() with no tmux = %v, want it to wrap exec.ErrNotFound", missing)
	}
	if len(runs) != 1 {
		t.Errorf("runner called %d times, want once (the failing run only)", len(runs))
	}
}

// outputRun is one call a recording OutputRunner saw: the argv and whether the run carried a
// deadline.
type outputRun struct {
	argv        []string
	hasDeadline bool
}

// scriptOutputs returns an OutputRunner that records every call into runs and answers each program
// (by base name) with its scripted output, or with an exit error when none is scripted.
func scriptOutputs(runs *[]outputRun, outputs map[string]string) OutputRunner {
	return func(ctx context.Context, program string, args ...string) ([]byte, error) {
		_, hasDeadline := ctx.Deadline()
		*runs = append(*runs, outputRun{argv: append([]string{program}, args...), hasDeadline: hasDeadline})
		out, ok := outputs[filepath.Base(program)]
		if !ok {
			return nil, errors.New("exit status 1")
		}
		return []byte(out), nil
	}
}

// TestClipboardReadImageWalksTheReaders pins each OS's image-reader ladder and argv, resolved: the
// readers run in order under a deadline, one that fails or writes nothing passes to the next, the
// first that writes something answers (PowerShell's base64 decoded), and an OS or PATH with no
// reader answers ErrNoClipboardImage.
func TestClipboardReadImageWalksTheReaders(t *testing.T) {
	t.Parallel()

	const png = "\x89PNG\r\n\x1a\nimage"
	tests := []struct {
		name      string
		goos      string
		env       map[string]string
		installed []string
		outputs   map[string]string
		wantData  string
		wantErr   error
		wantRuns  []string
	}{
		{
			name:      "wayland reads through wl-paste",
			goos:      "linux",
			env:       map[string]string{"WAYLAND_DISPLAY": "wayland-0"},
			installed: []string{"wl-paste", "xclip"},
			outputs:   map[string]string{"wl-paste": png},
			wantData:  png,
			wantRuns:  []string{inBin("wl-paste", "--no-newline", "--type", "image/png")},
		},
		{
			name:      "a reader with no image passes to the next",
			goos:      "linux",
			env:       map[string]string{"WAYLAND_DISPLAY": "wayland-0"},
			installed: []string{"wl-paste", "xclip"},
			outputs:   map[string]string{"xclip": png},
			wantData:  png,
			wantRuns: []string{
				inBin("wl-paste", "--no-newline", "--type", "image/png"),
				inBin("xclip", "-selection", "clipboard", "-t", "image/png", "-o"),
			},
		},
		{
			name:      "x11 skips wl-paste",
			goos:      "linux",
			installed: []string{"wl-paste", "xclip"},
			outputs:   map[string]string{"xclip": png},
			wantData:  png,
			wantRuns:  []string{inBin("xclip", "-selection", "clipboard", "-t", "image/png", "-o")},
		},
		{
			name:      "wsl falls back to powershell and decodes its base64",
			goos:      "linux",
			env:       map[string]string{"WSL_DISTRO_NAME": "Ubuntu"},
			installed: []string{"powershell.exe"},
			outputs:   map[string]string{"powershell.exe": base64.StdEncoding.EncodeToString([]byte(png)) + "\r\n"},
			wantData:  png,
			wantRuns: []string{
				inBin("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", powershellImageScript),
			},
		},
		{
			name:      "powershell only under wsl",
			goos:      "linux",
			installed: []string{"powershell.exe"},
			wantErr:   ErrNoClipboardImage,
		},
		{
			name:      "darwin reads through pngpaste",
			goos:      "darwin",
			installed: []string{"pngpaste"},
			outputs:   map[string]string{"pngpaste": png},
			wantData:  png,
			wantRuns:  []string{inBin("pngpaste", "-")},
		},
		{
			name:      "an empty clipboard answers no image",
			goos:      "linux",
			installed: []string{"xclip"},
			outputs:   map[string]string{"xclip": ""},
			wantErr:   ErrNoClipboardImage,
			wantRuns:  []string{inBin("xclip", "-selection", "clipboard", "-t", "image/png", "-o")},
		},
		{
			name:      "native windows has no reader",
			goos:      "windows",
			installed: []string{"powershell.exe"},
			wantErr:   ErrNoClipboardImage,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var runs []outputRun
			board := Clipboard{
				GOOS:          tc.goos,
				Env:           envFrom(tc.env),
				WorkspaceRoot: t.TempDir(),
				LookPath:      lookInstalled(tc.installed...),
				Output:        scriptOutputs(&runs, tc.outputs),
			}

			data, err := board.ReadImage()

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ReadImage() error = %v, want %v", err, tc.wantErr)
			}
			if string(data) != tc.wantData {
				t.Errorf("ReadImage() = %q, want %q", data, tc.wantData)
			}
			got := make([]string, 0, len(runs))
			for _, run := range runs {
				got = append(got, strings.Join(run.argv, " "))
				if !run.hasDeadline {
					t.Errorf("%s ran without a deadline", run.argv[0])
				}
			}
			if strings.Join(got, "\n") != strings.Join(tc.wantRuns, "\n") {
				t.Errorf("runs =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.wantRuns, "\n"))
			}
		})
	}
}

// TestClipboardReadImageRefusesAProgramInsideTheWorkspace pins the exec fence on the read side: a
// reader that resolves inside the workspace is refused, nothing runs, and the walk stops there.
func TestClipboardReadImageRefusesAProgramInsideTheWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	plantProgram(t, root, "xclip")
	var runs []outputRun
	board := Clipboard{
		GOOS:          "linux",
		WorkspaceRoot: root,
		LookPath:      func(name string) (string, error) { return filepath.Join(root, ".venv", "bin", name), nil },
		Output:        scriptOutputs(&runs, map[string]string{"xclip": "image"}),
	}

	_, err := board.ReadImage()

	if !errors.Is(err, security.ErrExecFromWritablePath) {
		t.Errorf("ReadImage() = %v, want the exec fence's refusal", err)
	}
	if len(runs) != 0 {
		t.Errorf("runner called %d times (%v), want never", len(runs), runs)
	}
}

// TestClipboardReadImageStopsAtAnOversizedImage pins the read bound: a reader that wrote past it
// ends the walk with ErrClipboardImageTooLarge rather than passing to the next reader.
func TestClipboardReadImageStopsAtAnOversizedImage(t *testing.T) {
	t.Parallel()

	calls := 0
	board := Clipboard{
		GOOS:     "linux",
		Env:      envFrom(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}),
		LookPath: lookInstalled("wl-paste", "xclip"),
		Output: func(context.Context, string, ...string) ([]byte, error) {
			calls++
			return nil, ErrClipboardImageTooLarge
		},
	}

	_, err := board.ReadImage()

	if !errors.Is(err, ErrClipboardImageTooLarge) || calls != 1 {
		t.Errorf("ReadImage() = %v after %d runs, want ErrClipboardImageTooLarge after 1", err, calls)
	}
}

// TestCappedBufferRefusesPastItsLimit pins the production runner's bound: writes up to the limit
// are kept, the one that would pass it is refused whole and marks the buffer over.
func TestCappedBufferRefusesPastItsLimit(t *testing.T) {
	t.Parallel()

	b := &cappedBuffer{limit: 4}
	if n, err := b.Write([]byte("abc")); n != 3 || err != nil {
		t.Fatalf("Write(abc) = %d, %v", n, err)
	}
	if _, err := b.Write([]byte("de")); !errors.Is(err, ErrClipboardImageTooLarge) || !b.over {
		t.Errorf("Write past the limit = %v, over = %v; want ErrClipboardImageTooLarge and over", err, b.over)
	}
	if got := b.buf.String(); got != "abc" {
		t.Errorf("kept %q, want %q", got, "abc")
	}
}
