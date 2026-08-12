package present

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/security"
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
// because argument boundaries are the one thing an opener can silently get wrong. Its extension is
// markdown rather than HTML so that the rows below keep testing what they SAY they test — since
// 2026-08-12 an .html path is refused by the extension bound before any of the platform or name
// logic runs, so a table built on one would pass while exercising nothing.
const testDocPath = "/workspace/my reports/review.md"

// docNamed returns testDocPath's directory with a different file name, so a table row can vary
// the extension — the thing rung 1 now judges — without varying anything else.
func docNamed(name string) string {
	return "/workspace/my reports/" + name
}

// openerBinDir is the directory the tests' PATH lookup answers with: an absolute one on every OS
// (os.TempDir is, where a literal "/usr/bin" would not be on Windows), and outside any workspace
// root a test builds, so the rows below exercise resolution with nothing for the fence to refuse.
var openerBinDir = filepath.Join(os.TempDir(), "apogee-opener-bin")

// lookInBin is the fake behind Opener.LookPath: PATH says every program lives in openerBinDir.
// It is the seam that makes rung 1's resolution testable at all — the suite runs on machines
// with no `open`, no `xdg-open` and no `cmd`, and a test that only passed where the real program
// exists would be testing the machine rather than the opener.
func lookInBin(name string) (string, error) {
	return filepath.Join(openerBinDir, name), nil
}

// resolvedArgv is a wanted command line with its program NAME rewritten to the absolute path
// lookInBin resolves it to. The tables keep naming the program — that name is the contract with
// three operating systems — while the assertion pins what the opener now hands its runner: an
// absolute argv[0], because a bare name would be resolved again, by apogee's inherited PATH, at
// the moment of launch.
func resolvedArgv(argv []string) []string {
	resolved := append([]string(nil), argv...)
	resolved[0] = filepath.Join(openerBinDir, resolved[0])
	return resolved
}

// Each desktop OS has exactly one opener command line, and the argv is asserted literally —
// these strings are the contract with three operating systems, not an implementation detail.
//
// The same table carries rung 1's other half: WHICH documents reach that command line at all.
// The extension is what selects the program on every desktop, so a model that picks the file
// name picks the program unless the launch is bounded — a refused extension must therefore
// produce no argv at all (ErrNoOpener, degrade to the baseline rung), not a command that merely
// goes unrun.
//
// The rows name the program; the assertion expects the ABSOLUTE path PATH resolves it to
// (resolvedArgv), which is what the opener builds since 2026-08-12 — a bare name in argv[0] is a
// second lookup, performed by the exec package against apogee's inherited PATH at launch.
func TestOpenerBuildsThePlatformCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		goos string
		vars map[string]string
		path string   // the document presented; empty means testDocPath
		want []string // nil means the opener must refuse and run nothing
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
		{
			// Rung 1's set is wider than rung 2's browser set on purpose: the OS handler is
			// exactly what shows a word-processor document a browser would only download.
			name: "darwin opens a word-processor document, which no browser renders",
			goos: "darwin",
			path: docNamed("review.docx"),
			want: []string{"open", docNamed("review.docx")},
		},
		{
			name: "windows opens an image",
			goos: "windows",
			path: docNamed("diagram.png"),
			want: []string{"cmd", "/c", "start", "", docNamed("diagram.png")},
		},
		{
			name: "linux opens the markdown a report is usually written in",
			goos: "linux",
			vars: map[string]string{"DISPLAY": ":0"},
			path: docNamed("review.md"),
			want: []string{"xdg-open", docNamed("review.md")},
		},
		{
			name: "a Windows-authored upper-case name is the same document",
			goos: "darwin",
			path: docNamed("REVIEW.MD"),
			want: []string{"open", docNamed("REVIEW.MD")},
		},
		{
			// The active-content half of the bound (ADR 0019, fourth amendment): the handler for
			// these is a browser, which runs the page rather than merely showing it, and a
			// file:// launch can carry no Content-Security-Policy to bound that. Rung 2 keeps
			// them; rung 1 refuses them exactly as it refuses a .bat.
			name: "darwin refuses an HTML page, whose handler runs what the page carries",
			goos: "darwin",
			path: docNamed("report.html"),
		},
		{
			name: "linux refuses an SVG, which is a script container with a picture in it",
			goos: "linux",
			vars: map[string]string{"DISPLAY": ":0"},
			path: docNamed("diagram.svg"),
		},
		{
			name: "the refusal is by extension, so an upper-case page is refused too",
			goos: "darwin",
			path: docNamed("REPORT.HTML"),
		},
		{
			name: "windows refuses the other two active markup extensions",
			goos: "windows",
			path: docNamed("report.xhtml"),
		},
		{
			name: "windows refuses the short HTML extension as well",
			goos: "windows",
			path: docNamed("report.htm"),
		},
		{
			name: "darwin refuses the double-clickable shell script",
			goos: "darwin",
			path: docNamed("review.command"),
		},
		{
			name: "windows refuses a batch file",
			goos: "windows",
			path: docNamed("review.bat"),
		},
		{
			name: "windows refuses an HTML application, which is script rather than markup",
			goos: "windows",
			path: docNamed("notes.hta"),
		},
		{
			name: "an upper-case executable extension is refused just the same",
			goos: "windows",
			path: docNamed("REVIEW.BAT"),
		},
		{
			name: "linux refuses a desktop entry",
			goos: "linux",
			vars: map[string]string{"DISPLAY": ":0"},
			path: docNamed("review.desktop"),
		},
		{
			// No extension means the handler is chosen by sniffing, and a shebang line sniffs
			// as a program.
			name: "a file with no extension is refused",
			goos: "linux",
			vars: map[string]string{"DISPLAY": ":0"},
			path: docNamed("review"),
		},
		{
			name: "an unknown extension is refused rather than guessed at",
			goos: "darwin",
			path: docNamed("review.sqlite3"),
		},
		{
			// The Windows rung is the one that travels through a shell: cmd.exe re-parses the
			// joined line, EscapeArg quotes an argument only when it holds a space or a quote,
			// and `&` splits commands — so this space-free path reads back as three commands and
			// the middle one runs. The NAME is therefore bounded on Windows: no argv at all.
			// Every row below carries a markdown extension for the same reason testDocPath does:
			// the extension bound runs first, so an .html name would be refused before cmdSafe
			// was ever consulted and the row would prove nothing about the NAME.
			name: "windows refuses a name cmd would split into a second command",
			goos: "windows",
			path: "/ws/report&calc&.md",
		},
		{
			name: "windows refuses a pipe in a name",
			goos: "windows",
			path: docNamed("a|b.md"),
		},
		{
			name: "windows refuses cmd's own escape character",
			goos: "windows",
			path: docNamed("x^y.md"),
		},
		{
			// %TEMP% expands even inside the double quotes EscapeArg adds around a space-carrying
			// path, so quoting is no defence against it.
			name: "windows refuses a name that expands an environment variable",
			goos: "windows",
			path: docNamed("%TEMP%.md"),
		},
		{
			// Go escapes an embedded quote as `\"`, which cmd's parser does not honour: the two
			// disagree about where the quoted region ends, and everything after that is live.
			name: "windows refuses a quote in a name",
			goos: "windows",
			path: docNamed(`re"port.md`),
		},
		{
			// `;` is a cmd token delimiter: in an unquoted path it splits the line into TWO
			// start arguments, and start resolves its first argument like a command name.
			name: "windows refuses a cmd token delimiter in a name",
			goos: "windows",
			path: "/ws/a;b.md",
		},
		{
			// `!` fires when delayed expansion is switched on machine-wide (a registry key, not
			// a choice this process makes), so it is refused rather than depended on.
			name: "windows refuses a delayed-expansion trigger in a name",
			goos: "windows",
			path: docNamed("hello!.md"),
		},
		{
			// The bound refuses cmd's grammar, not every unusual character: a space is quoted by
			// EscapeArg and inert inside the quotes, so the common case keeps opening.
			name: "windows still opens a name with a space",
			goos: "windows",
			path: docNamed("annual report.md"),
			want: []string{"cmd", "/c", "start", "", docNamed("annual report.md")},
		},
		{
			name: "windows still opens parentheses, which are literal mid-argument",
			goos: "windows",
			path: docNamed("report(1).md"),
			want: []string{"cmd", "/c", "start", "", docNamed("report(1).md")},
		},
		{
			// The name bound is Windows' alone: `open` receives the path as one execve argument
			// with no shell in between, so the same name is just a file name on macOS.
			name: "darwin opens the name windows refuses, because no shell re-parses it",
			goos: "darwin",
			path: docNamed("report&calc&.md"),
			want: []string{"open", docNamed("report&calc&.md")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := tt.path
			if path == "" {
				path = testDocPath
			}
			runner := &recordingRunner{}
			opener := Opener{GOOS: tt.goos, Env: envFrom(tt.vars), LookPath: lookInBin, Run: runner.run}

			err := opener.Open(path)
			if tt.want == nil {
				if !errors.Is(err, ErrNoOpener) {
					t.Fatalf("Open(%q) = %v, want ErrNoOpener", path, err)
				}
				if len(runner.calls) != 0 {
					t.Errorf("Open(%q) ran %v, want no argv built at all", path, runner.calls)
				}
				return
			}
			if err != nil {
				t.Fatalf("Open(%q) = %v, want no error", path, err)
			}
			if want, got := resolvedArgv(tt.want), runner.only(t); !equalArgv(got, want) {
				t.Errorf("Open(%q) ran %q, want %q", path, got, want)
			}
		})
	}
}

// OpenerRenderable is the bound itself, stated as a predicate so a host walking its own ladder
// can ask the same question. The rule it encodes is "the default handler displays this, it does
// not execute it" — so the whole executable family is out whatever the OS, and the document,
// image and text formats a deliverable actually arrives in are in. The office rows pin the line
// the rule draws — .docx vs .docm — with the pre-2007 binary formats, which have no macro-free
// variant, on the .docm side of it (ADR 0019, third amendment 2026-07-26).
//
// The four active-content extensions moved from the first list to the second on 2026-08-12 (ADR
// 0019, fourth amendment). That is the rule being APPLIED, not relaxed or repaired: a browser is
// the default handler for .html/.htm/.xhtml/.svg and a browser RUNS a page, so script in a
// document that need only have arrived in the clone reaches loopback, RFC1918 and the cloud
// metadata address from the browser's network position. They sit further along the same line as
// .docm — a macro needs one Enable Content click, a <script> needs none — and they keep rung 2,
// where a Content-Security-Policy can bound them and a file:// launch cannot. Restoring any of
// them to the allow-list must fail here.
func TestOpenerRenderableAllowsDocumentsAndRefusesPrograms(t *testing.T) {
	t.Parallel()

	renderable := []string{
		"report.pdf",
		"report.md", "notes.txt", "data.json", "config.yaml", "notes.log",
		"report.docx", "report.odt", "sheet.xlsx", "deck.pptx", "book.epub",
		"data.csv", // ruled IN (ADR 0019, third amendment): plain text, no container for a macro
		"shot.png", "photo.jpg", "photo.jpeg", "anim.gif", "shot.webp", "scan.tiff",
	}
	for _, name := range renderable {
		if !OpenerRenderable(docNamed(name)) {
			t.Errorf("OpenerRenderable(%q) = false, want true — it is a document, not a program", name)
		}
	}

	programs := []string{
		"report.command", "report.terminal", "report.app", "report.scpt", // macOS
		"report.bat", "report.cmd", "report.com", "report.exe", "report.ps1", // Windows
		"report.vbs", "notes.hta", "report.js", "report.scr", "report.msi", "report.reg", "report.lnk",
		"report.desktop", "report.sh", "report.py", // Linux and friends
		"report.docm", "sheet.xlsm", "deck.pptm", // macro-enabled office documents
		"report.doc", "sheet.xls", "deck.ppt", // pre-2007 office: the one binary container carries macros too
		"report.html", "report.htm", "report.xhtml", "diagram.svg", // active content: the handler is a runtime
		"REPORT.HTML", "DIAGRAM.SVG", // and the case fold applies to the refusal too
		"report", "report.", ".bashrc", // no usable extension at all
	}
	for _, name := range programs {
		if OpenerRenderable(docNamed(name)) {
			t.Errorf("OpenerRenderable(%q) = true, want false — an OS handler may run this", name)
		}
	}
}

// A present.command names ONE application, so the extension selects nothing on rung 3 and the
// bound deliberately stops there (ADR 0019 §5, amendment 2026-07-26): narrowing the user's own
// configured opener would refuse the source files and odd formats they configured it for.
func TestOpenerCommandOverrideIsNotExtensionBounded(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{}
	opener := Opener{GOOS: "darwin", CommandOverride: "zed {path}", Run: runner.run}
	path := docNamed("main.go")

	if err := opener.Open(path); err != nil {
		t.Fatalf("Open(%q) = %v, want the user's own opener to run", path, err)
	}
	if got := runner.only(t); !equalArgv(got, []string{"zed", path}) {
		t.Errorf("Open(%q) ran %q, want the configured application", path, got)
	}
}

// The name bound stops at rung 3 exactly as the extension bound does (ADR 0019, both 2026-07-26
// amendments): a present.command names ONE application and is launched without cmd.exe anywhere
// near it, so the user's configured opener still shows a file whose name the Windows OS table
// refuses.
func TestOpenerCommandOverrideIsNotNameBounded(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{}
	opener := Opener{GOOS: "windows", CommandOverride: "zed {path}", Run: runner.run}
	path := docNamed("report&calc&.html")

	if err := opener.Open(path); err != nil {
		t.Fatalf("Open(%q) = %v, want the user's own opener to run", path, err)
	}
	if got := runner.only(t); !equalArgv(got, []string{"zed", path}) {
		t.Errorf("Open(%q) ran %q, want the configured application", path, got)
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
		GOOS:     "linux",
		Env:      envFrom(map[string]string{"DISPLAY": ":0"}),
		LookPath: lookInBin,
		Run:      runner.run,
	}

	err := opener.Open(testDocPath)
	if !errors.Is(err, launchFailed) {
		t.Fatalf("Open() = %v, want the runner's error wrapped", err)
	}
	if errors.Is(err, ErrNoOpener) {
		t.Error("Open() reported ErrNoOpener for an opener that ran and failed")
	}
	if got := runner.only(t); !equalArgv(got, resolvedArgv([]string{"xdg-open", testDocPath})) {
		t.Errorf("Open() ran %q, want the platform opener", got)
	}
}

// Rung 1's own program is the one thing about a presentation the model never chose — until a
// PATH entry inside the workspace makes it one. `.venv/bin` on PATH is the everyday shape of
// that (an activated virtualenv, or node_modules/.bin), and a confined call is ALLOWED to write
// there, so a planted `xdg-open` would be launched by a read-only tool that auto-runs in every
// mode, with no approval and no confinement box. The refusal must therefore be LOUD — a real
// error naming the resolved file, not the ErrNoOpener a headless machine reports — because
// unlike a missing opener it says something is wrong with this session, not with this desktop.
func TestOpenerRefusesAProgramInsideTheWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bin := filepath.Join(root, ".venv", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", bin, err)
	}
	planted := filepath.Join(bin, "xdg-open")
	if err := os.WriteFile(planted, []byte("#!/bin/sh\nexec /bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", planted, err)
	}

	runner := &recordingRunner{}
	opener := Opener{
		GOOS:          "linux",
		Env:           envFrom(map[string]string{"DISPLAY": ":0"}),
		WorkspaceRoot: root,
		LookPath:      func(name string) (string, error) { return filepath.Join(bin, name), nil },
		Run:           runner.run,
	}

	err := opener.Open(testDocPath)
	if !errors.Is(err, security.ErrExecFromWritablePath) {
		t.Fatalf("Open() = %v, want the exec fence's refusal", err)
	}
	if errors.Is(err, ErrNoOpener) {
		t.Error("Open() reported ErrNoOpener for a refused program, degrading silently past the one case worth saying")
	}
	// The operator has to be able to see WHICH file was refused: "not available" would send them
	// after a missing install instead of after their own PATH.
	if resolved := security.EvalRealPath(planted); !strings.Contains(err.Error(), resolved) {
		t.Errorf("Open() = %v, want the message to name the resolved program %q", err, resolved)
	}
	if len(runner.calls) != 0 {
		t.Errorf("Open() launched %v, want nothing run", runner.calls)
	}
}

// A program that cannot be resolved to an absolute path is a fact about the MACHINE, so it takes
// the machine's answer: ErrNoOpener, and the ladder degrades to the baseline transcript rung with
// the document still presented. The relative rows are exec.LookPath's own ErrDot shape — a name
// found only through a relative PATH entry, which the child would re-resolve against its working
// directory (the workspace) — and are refused for the same reason the absolute check exists.
func TestOpenerDegradesWhenTheProgramDoesNotResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		look func(string) (string, error)
	}{
		{
			name: "nothing on PATH",
			look: func(string) (string, error) { return "", exec.ErrNotFound },
		},
		{
			name: "found only through a relative PATH entry (ErrDot)",
			look: func(name string) (string, error) { return filepath.Join("bin", name), exec.ErrDot },
		},
		{
			name: "a relative answer with no error at all",
			look: func(name string) (string, error) { return filepath.Join("bin", name), nil },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runner := &recordingRunner{}
			opener := Opener{
				GOOS:     "linux",
				Env:      envFrom(map[string]string{"DISPLAY": ":0"}),
				LookPath: tt.look,
				Run:      runner.run,
			}

			if err := opener.Open(testDocPath); !errors.Is(err, ErrNoOpener) {
				t.Fatalf("Open() = %v, want ErrNoOpener", err)
			}
			if len(runner.calls) != 0 {
				t.Errorf("Open() ran %v, want nothing run", runner.calls)
			}
		})
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
	if err := (Opener{GOOS: "linux", LookPath: lookInBin, Run: runner.run}).Open(testDocPath); !errors.Is(err, ErrNoOpener) {
		t.Errorf("Open() on linux with a nil env = %v, want ErrNoOpener", err)
	}
	if err := (Opener{GOOS: "darwin", LookPath: lookInBin, Run: runner.run}).Open(testDocPath); err != nil {
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
