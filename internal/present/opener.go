package present

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/google/shlex"
)

// ErrNoOpener is the sentinel Open returns when this machine has nothing to hand a document
// to: an OS with no known opener command, or a Linux session with no display server behind it
// (see HasDesktop). It is a NORMAL outcome rather than a failure — the caller degrades to the
// baseline rung, which is the rung that is never wrong, and says so in the transcript
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
// for HTML, the OS-associated app for everything else, or the one application the user named in
// present.command.
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
// The OS table is the one every desktop documents: `open <path>` on macOS, `cmd /c start ""
// <path>` on Windows (start's first quoted argument is the window TITLE, and omitting it makes
// start read the path as one), `xdg-open <path>` on Linux. Windows ships unexercised until the
// merge plan's Phase 5 provides a real Windows harness — stated in ADR 0019 rather than hidden.
func (o Opener) argv(path string) ([]string, error) {
	if template := strings.TrimSpace(o.CommandOverride); template != "" {
		return overrideArgv(template, path)
	}
	if !HasDesktop(o.GOOS, o.Env) {
		return nil, ErrNoOpener
	}

	switch o.GOOS {
	case "darwin":
		return []string{"open", path}, nil
	case "windows":
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
