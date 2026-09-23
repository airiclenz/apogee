package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
)

// ----------------------------------------------------------------------------
// The copy routes beside OSC 52 (mouse.go's copyFlash)
// ----------------------------------------------------------------------------
//
// OSC 52 (tea.SetClipboard) is the primary, SSH-safe copy channel and stays first, but a terminal
// is free to ignore the escape — several do, some only until the human turns the feature on — and
// then a copy that flashed "copied N chars" put nothing anywhere (the ISSUES defect). The
// fallback writes the same text through the host's own clipboard program, so those two channels
// cover each other: OSC 52 reaches the far end of an ssh session where no local program can, the
// system write reaches a local terminal that drops the escape.
//
// tmux is the third route. On its default `set-clipboard external` tmux drops the OSC 52 an
// application writes (input.c honours it only under `on`), so inside tmux neither channel above
// reaches the outer terminal's clipboard over ssh (apogee-tmux-copy-dropped). `tmux load-buffer -w`
// is tmux's OWN clipboard write, which it forwards to the outer terminal under `external` too — so
// inside tmux the copy also hands the text to it. `set-clipboard off` blocks that route as well,
// and apogee neither detects nor rewrites the user's tmux options.

// writeSystemClipboard writes text to the host's system clipboard. It is a package-level variable
// rather than a direct call so a test can substitute a recorder — the same injectable seam
// [ConfigHost.ExternalEditSpec] uses for the external editor, one level down: the platform program
// (pbcopy, xclip/xsel/wl-copy, clip.exe) is the one thing a unit test cannot have.
var writeSystemClipboard = clipboard.WriteAll

// writeTmuxClipboard hands text to tmux's paste buffer and, through `-w`, to the outer terminal's
// clipboard — only when apogee runs inside tmux (TMUX non-empty, read at call time); outside tmux
// it starts no process and returns nil. A package-level variable for the same reason as
// writeSystemClipboard: a test substitutes a recorder, so a suite run inside tmux never overwrites
// the developer's own tmux buffer.
var writeTmuxClipboard = func(text string) error {
	return loadTmuxBuffer(os.Getenv, runWithStdin, text)
}

// tmuxClipboardTimeout bounds the `tmux load-buffer` child: a wedged tmux server must not leave a
// Cmd goroutine waiting forever on a copy nothing in the model waits for.
const tmuxClipboardTimeout = 2 * time.Second

// commandRunner runs the program name with args, feeding stdin to its standard input, and returns
// its exit error. ctx bounds the run. It is the one piece of loadTmuxBuffer a unit test replaces.
type commandRunner func(ctx context.Context, stdin, name string, args ...string) error

// loadTmuxBuffer runs `tmux load-buffer -w -` with text on stdin when getenv("TMUX") is non-empty,
// under tmuxClipboardTimeout. Outside tmux it calls nothing and returns nil. The environment and
// the runner are parameters so the gate and the argv are testable without touching the process env
// or spawning tmux.
func loadTmuxBuffer(getenv func(string) string, run commandRunner, text string) error {
	if getenv("TMUX") == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), tmuxClipboardTimeout)
	defer cancel()
	if err := run(ctx, text, "tmux", "load-buffer", "-w", "-"); err != nil {
		return fmt.Errorf("tmux load-buffer: %w", err)
	}
	return nil
}

// runWithStdin is the real commandRunner: it starts name with args, stdin as its standard input and
// no stdout or stderr of ours — a nil stream in [exec.Cmd] is the null device, so the child can
// never write into the frame — and waits for it, or for ctx to kill it.
func runWithStdin(ctx context.Context, stdin, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	return cmd.Run()
}

// systemClipboardCmd returns a Cmd that writes text to the system clipboard, best-effort. Any
// error is swallowed deliberately: the write runs BESIDE tea.SetClipboard, never instead of it, so
// a machine with no clipboard program (a bare Linux box, a container) must degrade to exactly
// today's OSC-52-only behaviour rather than report a failure for a copy that may well have landed.
// The Cmd body runs off the Update goroutine, which is what keeps the shell-out off the render
// path; it yields no message because nothing in the model depends on the outcome. The seam is read
// HERE, when the copy builds its batch, not when the Cmd body runs: the body runs on a goroutine
// the copy never waits for, so a late read would reach whatever the seam holds by then — in a test,
// a real clipboard program once the recorder is restored, or the next test's recorder.
func systemClipboardCmd(text string) tea.Cmd {
	write := writeSystemClipboard
	return func() tea.Msg {
		_ = write(text)
		return nil
	}
}

// tmuxClipboardCmd returns a Cmd that hands text to tmux (writeTmuxClipboard), best-effort on the
// same terms as systemClipboardCmd: the error is swallowed — a tmux that refuses the buffer must
// not turn a copy OSC 52 or the system write may well have landed into a reported failure — the
// shell-out runs off the Update goroutine, the Cmd yields no message, and the seam is read when the
// Cmd is built, not when it runs.
func tmuxClipboardCmd(text string) tea.Cmd {
	write := writeTmuxClipboard
	return func() tea.Msg {
		_ = write(text)
		return nil
	}
}
