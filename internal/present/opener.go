package present

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/shlex"
)

// ErrNoOpener is the sentinel Open returns when this machine has nothing to hand a document
// to: an OS with no known opener command, a Linux session with no display server behind it
// (see HasDesktop), a document whose extension is not one an OS handler should be handed
// (see OpenerRenderable), or — on Windows alone — a path whose name cmd.exe would read as its
// own syntax (see cmdSafe). It is a NORMAL outcome rather than a failure — the caller degrades to
// the baseline rung, which is the rung that is never wrong, and says so in the transcript
// (ADR 0019 §4). Callers test for it with errors.Is, because "there was nothing to open into"
// has to read differently from "an opener was tried and it failed".
var ErrNoOpener = errors.New("present: no opener available on this platform")

// pathPlaceholder is the token a present.command template uses to say where the document path
// goes ("zed {path}"). It is substituted AFTER the template is split into arguments, so a path
// containing spaces stays one argument no matter how the user quoted their template.
const pathPlaceholder = "{path}"

// launchGrace bounds how long the default runner waits for an opener command to report an exit
// status before it declares the launch successful and stops watching. Every opener in the table
// is a launcher — open, xdg-open and start hand the document to another process and exit in
// milliseconds — so a command still running after the grace has plainly launched, while one
// that fails (no such program, no handler for the type, exit 3 from xdg-open) has already said
// so. The wait is what makes rung 1 fail VISIBLE; the bound is what stops a user-configured
// foreground command (present.command: "vim {path}") from stalling the Turn that presented.
const launchGrace = 2 * time.Second

// Runner launches one opener command and reports whether it started and, within the runner's
// own patience, whether it failed. It is the seam the tests fake: the OS opener's whole subject
// is a program on the machine running it, so the argv this package builds can only be pinned by
// capturing it instead of executing it. Production leaves Opener.Run nil and gets launchDetached.
type Runner func(name string, args ...string) error

// Opener is the presentation ladder's rung 1 and rung 3 (ADR 0019): the host's act of handing a
// finished document to the desktop application that knows how to show it — the default browser
// for HTML, the OS-associated app for every other extension rung 1 will hand over
// (OpenerRenderable), or the one application the user named in present.command.
//
// It decides only WHAT to run; whether an opener should run at all is the ladder's call, because
// that answer needs the locality fact this type deliberately does not consult (rung 1 is right
// only on a Local session — an opener fired on a remote box opens into a display nobody is
// looking at). Open still re-checks the desktop half itself, so a mis-wired caller degrades to
// ErrNoOpener rather than shelling out into a headless box.
//
// The opener never touches the terminal Apogee is drawing on: the child's standard streams go to
// the null device (see launchDetached), because an opener that printed a warning would scribble
// straight across the Bubble Tea screen and corrupt the frame.
//
// All three inputs are injected. The zero value is safe and opens nothing: an empty GOOS matches
// no branch, so Open reports ErrNoOpener.
type Opener struct {
	// GOOS is the operating system whose opener command to build — runtime.GOOS in production,
	// a table row's string in tests.
	GOOS string
	// Env is the environment lookup HasDesktop reads (os.Getenv in production). A nil Env reads
	// as an empty environment, which makes a Linux session headless.
	Env func(string) string
	// CommandOverride is the present.command template — a command line naming the application
	// the user wants their documents in, with {path} where the document goes. When set it
	// REPLACES the OS opener on every OS (ADR 0019 rung 3), including the ones that have no
	// built-in opener: it is the user's own statement of how a document is shown on their
	// machine, so it also stands in for the desktop check this type would otherwise make.
	CommandOverride string
	// Run launches the command Open built. Nil means launchDetached, the production runner.
	Run Runner
}

// Open shows path in a desktop application and reports what happened. A nil error means the
// opener launched; ErrNoOpener means this machine has no opener to launch (degrade to the
// baseline rung); any other error means an opener was tried and failed, which the caller
// surfaces rather than swallows — a document the user was told about but never saw is the one
// outcome the ladder must never produce silently.
//
// path must be absolute and already resolved inside the workspace root by the caller: the model
// never supplies a command here, only a document the tool has already checked is a regular file
// under the root (ADR 0019 §5 — this is why the opener may run outside tool confinement).
func (o Opener) Open(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("present: no document path to open")
	}

	argv, err := o.argv(path)
	if err != nil {
		return err
	}

	run := o.Run
	if run == nil {
		run = launchDetached
	}
	if err := run(argv[0], argv[1:]...); err != nil {
		return fmt.Errorf("present: opener %q failed: %w", argv[0], err)
	}
	return nil
}

// argv builds the exact command line for this machine, the configured override first because it
// is the user speaking about their own desktop and outranks anything this package can infer.
//
// Rung 1's OS table is bounded twice: by the MACHINE (HasDesktop) and by the DOCUMENT
// (OpenerRenderable). The second bound is the one that keeps the launch honest — on every
// desktop it is the EXTENSION that picks the program, so handing the handler a model-chosen
// extension is handing it a model-chosen program. A refused extension yields no argv at all and
// reads as ErrNoOpener, so the ladder degrades to the baseline rung exactly as it does for a
// headless session (ADR 0019 §4, amended 2026-07-26).
//
// The bound stops at rung 3 on purpose: a present.command template names ONE application, so the
// extension selects nothing there, and narrowing the user's own configured opener to a curated
// list would refuse the source files and odd formats they configured it for. ADR 0019 §5's
// reasoning holds on that rung — present.command is the user's own configuration, with the same
// standing as their shell.
//
// The OS table is the one every desktop documents: `open <path>` on macOS, `cmd /c start ""
// <path>` on Windows (start's first quoted argument is the window TITLE, and omitting it makes
// start read the path as one), `xdg-open <path>` on Linux. Windows ships unexercised until the
// merge plan's Phase 5 provides a real Windows harness — stated in ADR 0019 rather than hidden.
//
// The Windows line is the one rung that travels through a SHELL — cmd.exe re-parses the joined
// command line — so it alone carries a third bound, on the NAME: a path holding a character cmd
// reads as syntax (cmdSafe) builds no argv and degrades exactly like a refused extension
// (ADR 0019, second amendment 2026-07-26). macOS and Linux need no name bound, because `open`
// and `xdg-open` receive the path as one execve argument with no shell in between.
func (o Opener) argv(path string) ([]string, error) {
	if template := strings.TrimSpace(o.CommandOverride); template != "" {
		return overrideArgv(template, path)
	}
	if !OpenerRenderable(path) {
		return nil, ErrNoOpener
	}
	if !HasDesktop(o.GOOS, o.Env) {
		return nil, ErrNoOpener
	}

	switch o.GOOS {
	case "darwin":
		return []string{"open", path}, nil
	case "windows":
		if !cmdSafe(path) {
			return nil, ErrNoOpener
		}
		return []string{"cmd", "/c", "start", "", path}, nil
	case "linux":
		return []string{"xdg-open", path}, nil
	default:
		// Unreachable while HasDesktop and this switch agree on which systems have a desktop;
		// kept so that teaching HasDesktop a new OS degrades to the baseline rung instead of
		// running an argv nobody wrote.
		return nil, ErrNoOpener
	}
}

// openerRenderableExts is rung 1's allow-list: the extensions whose desktop handler RENDERS the
// document rather than executing it. It is what bounds the launch, and the bound is the whole
// point — the model chooses the document and the extension chooses the program, so an unbounded
// `open <path>` is `run <path>` the moment the model writes report.command, report.bat or
// notes.hta (audit 2026-07-26 H-2, under ADR 0012's invariant that an unattended call has a
// bounded blast radius).
//
// It is an ALLOW-list because the deny side is unbounded and OS-specific — Windows executes
// everything on PATHEXT plus .hta/.scr/.msi/.reg/.lnk, macOS has .command/.terminal/.app/.scpt,
// Linux has .desktop — and a list of what must never run is a list somebody is always one entry
// behind on. An extension earns a place here only when its default handler DISPLAYS the file,
// which is what excludes scripts, installers, shortcuts and every office format whose container
// can carry a macro: the macro-enabled OOXML formats (.docm/.xlsm/.pptm) and equally the
// pre-2007 binary formats (.doc/.xls/.ppt), which have no macro-free variant — their handler
// offers to run whatever the document carries on a single Enable Content click. The line the
// set draws is .docx vs .docm, and the legacy trio sits on the .docm side of it (ADR 0019,
// third amendment 2026-07-26). It is also why a file with NO extension is refused: an
// executable text file with a shebang is exactly what a content-sniffing xdg-open would hand
// to a shell.
//
// .csv stays IN, ruled explicitly by the same line (same amendment): a CSV is plain text with
// no container for code, so there is nothing in the file its handler can be asked to run. The
// residual surface — spreadsheet formula/DDE injection — exists only when the handler happens
// to be a spreadsheet, and even there nothing reaches the OS without the user clicking through
// that application's own security prompts; meanwhile .csv is a format a coding agent's
// deliverables genuinely arrive in, which .doc and .ppt are not.
//
// It is deliberately WIDER than rung 2's browser set (browserRenderableExts, internal/tui) —
// an OS handler shows the .docx, .png and .md a browser would download or render as source, and
// opening a deliverable in the application that knows it is rung 1's whole value. Rung 2's four
// extensions are a subset of this set, pinned by a test in internal/tui.
var openerRenderableExts = map[string]bool{
	// Text and markup: the shapes a Skill's own deliverables come in.
	".txt":      true,
	".text":     true,
	".md":       true,
	".markdown": true,
	".rst":      true,
	".adoc":     true,
	".log":      true,
	".csv":      true,
	".tsv":      true,
	".json":     true,
	".xml":      true,
	".yaml":     true,
	".yml":      true,
	".toml":     true,
	".html":     true,
	".htm":      true,
	".xhtml":    true,
	".svg":      true,

	// Documents: the formats an office or reader application owns. The pre-2007 binary
	// formats (.doc/.xls/.ppt) are deliberately absent — see the macro rule above.
	".pdf":  true,
	".rtf":  true,
	".epub": true,
	".docx": true,
	".odt":  true,
	".xlsx": true,
	".ods":  true,
	".pptx": true,
	".odp":  true,

	// Images: a diagram or screenshot is a deliverable too.
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
	".bmp":  true,
	".tif":  true,
	".tiff": true,
	".avif": true,
	".heic": true,
}

// OpenerRenderable reports whether path is a document rung 1 may hand to this machine's OS
// handler (see openerRenderableExts for the rule the set follows). Anything else — an
// executable extension, an unknown one, or none at all — is refused, and Open reports
// ErrNoOpener so the ladder degrades to the baseline transcript rung rather than launching a
// program the model named by choosing a file name.
//
// It is exported so the bound is a stated, testable part of this package's contract rather than
// a hidden filter: a host wiring its own presentation ladder asks the same question, and rung
// 2's narrower browser set is a subset of the answer.
//
// The extension is lowercased first, so a Windows-authored REPORT.HTML is the same document as
// report.html — and REPORT.BAT is refused exactly like report.bat.
func OpenerRenderable(path string) bool {
	return openerRenderableExts[strings.ToLower(filepath.Ext(path))]
}

// cmdMetacharacters are the characters cmd.exe reads as its own syntax somewhere in the one
// line rung 1's Windows opener hands it: the operators (`&`, `|`, `^`, `<`, `>`), the two
// expansions (`%`, which fires even inside double quotes, and `!`, which fires when a
// machine-wide registry key turns delayed expansion on), the quote itself (Go escapes an
// embedded `"` as `\"`, which cmd's own parser does not honour — the two disagree about where
// the quoted region ends, and everything after that is live syntax), and the token delimiters
// (`;`, `,`, `=` — an unquoted path holding one splits into TWO start arguments, and start
// resolves its first argument like a command name, PATHEXT and all).
//
// A space is deliberately absent: Go double-quotes an argument carrying one (syscall.EscapeArg),
// and this set is exactly the set that stays live inside — or breaks out of — those quotes.
// Parentheses are absent too: they are literal to cmd mid-argument once this set is refused,
// and `report(1).html` is a name real deliverables have.
const cmdMetacharacters = "&|^<>%\"!;,="

// cmdSafe reports whether path can ride `cmd /c start "" <path>` without cmd.exe reading any of
// it as grammar rather than file name. Go joins an argv into one command line and cmd.exe
// RE-PARSES it, so on Windows — and only there — the model-chosen name is a third bound beside
// the machine and the extension: a refusal here is what keeps `report&calc&.html` in a
// space-free workspace path from reading back as three commands. Control characters are refused
// with the metacharacters (`\r` and `\n` end a cmd command the way `&` does); a refused path
// degrades to the baseline rung via ErrNoOpener, never an error.
func cmdSafe(path string) bool {
	if strings.ContainsAny(path, cmdMetacharacters) {
		return false
	}
	for _, r := range path {
		if r < ' ' {
			return false
		}
	}
	return true
}

// overrideArgv turns a present.command template into an argv for path. The template is split
// with the POSIX command-line splitter (shlex, as the terminal tool uses) and {path} is
// substituted into the resulting arguments afterwards — that order is the whole point: the
// user's quoting decides the argument boundaries, and a document path containing spaces can
// then never split one argument into two, whatever the user wrote.
//
// A template that never mentions {path} gets the path appended, the convention git's core.editor
// established: "zed" and "zed {path}" both mean "show it in Zed", and an opener that launched
// but opened nothing would be the worst outcome available — a success the user cannot see.
//
// An unparseable template (an unbalanced quote) is an error rather than a guess, so a typo in
// the config surfaces as a message instead of an application launched with mangled arguments.
func overrideArgv(template, path string) ([]string, error) {
	argv, err := shlex.Split(template)
	if err != nil {
		return nil, fmt.Errorf("present: could not parse present.command %q: %w", template, err)
	}
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return nil, fmt.Errorf("present: present.command %q names no program", template)
	}

	substituted := false
	for i, arg := range argv {
		if strings.Contains(arg, pathPlaceholder) {
			argv[i] = strings.ReplaceAll(arg, pathPlaceholder, path)
			substituted = true
		}
	}
	if !substituted {
		argv = append(argv, path)
	}
	return argv, nil
}

// launchDetached is the production Runner: it starts the opener with its standard streams
// detached from the terminal Apogee is drawing on (nil Stdin/Stdout/Stderr connect the child to
// the null device), waits up to launchGrace for an exit status, and returns nil once the command
// has outlived that grace.
//
// Both halves are deliberate. Waiting at all is what makes a failed launch visible — a launcher
// that cannot find a handler exits immediately and non-zero, and reporting nil there would tell
// the user a document was opened that never appeared. Giving up on waiting is what keeps a
// command the user configured as a foreground application from holding the presenting Turn open
// for as long as they keep reading. Either way the child is reaped by the watching goroutine, so
// nothing is left behind.
func launchDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)

	if err := cmd.Start(); err != nil {
		return err
	}

	// Buffered so the watcher can hand over its result and exit even after the grace expired
	// and nobody is listening any more.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	select {
	case err := <-exited:
		return err
	case <-time.After(launchGrace):
		return nil // still running: it launched, and the goroutine above still reaps it
	}
}
