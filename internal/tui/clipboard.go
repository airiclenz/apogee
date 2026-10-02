package tui

import (
	"os"
	"runtime"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"github.com/airiclenz/apogee/internal/present"
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

// writeSystemClipboard writes text to the host's system clipboard, resolving the helper program
// against workspace (hostClipboard). It is a package-level variable rather than a direct call so a
// test can substitute a recorder — the same injectable seam [ConfigHost.ExternalEditSpec] uses for
// the external editor, one level down: the platform program (pbcopy, xclip/xsel/wl-copy, clip.exe)
// is the one thing a unit test cannot have. Only tests assign it, never before the model they
// drive is built from it.
var writeSystemClipboard = writeHostClipboard

// writeTmuxClipboard hands text to tmux's paste buffer and, through `-w`, to the outer terminal's
// clipboard — only when apogee runs inside tmux (TMUX non-empty, read at call time); outside tmux
// it starts no process and returns nil. tmux is resolved against workspace (hostClipboard). A
// package-level variable for the same reason as writeSystemClipboard: a test substitutes a
// recorder, so a suite run inside tmux never overwrites the developer's own tmux buffer.
var writeTmuxClipboard = func(workspace, text string) error {
	return hostClipboard(workspace).WriteTmux(text)
}

// hostClipboard is the production present.Clipboard for a session in workspace: this process's
// OS and environment, exec.LookPath, and the session workspace as the exec fence, so a workspace
// directory on the inherited PATH can never supply the clipboard or tmux program.
func hostClipboard(workspace string) present.Clipboard {
	return present.Clipboard{GOOS: runtime.GOOS, Env: os.Getenv, WorkspaceRoot: workspace}
}

// writeHostClipboard is writeSystemClipboard's production value. Windows keeps atotto's Win32
// clipboard write — no program runs there, so there is nothing to resolve; every other OS goes
// through hostClipboard's resolved helper program.
func writeHostClipboard(workspace, text string) error {
	if runtime.GOOS == "windows" {
		return clipboard.WriteAll(text)
	}
	return hostClipboard(workspace).WriteSystem(text)
}

// systemClipboardCmd returns a Cmd that writes text to the system clipboard, best-effort, with the
// helper program resolved against workspace. Any error is swallowed deliberately: the write runs
// BESIDE tea.SetClipboard, never instead of it, so a machine with no clipboard program (a bare
// Linux box, a container) must degrade to exactly today's OSC-52-only behaviour rather than report
// a failure for a copy that may well have landed. The Cmd body — the PATH lookup and the
// shell-out both — runs off the Update goroutine, which is what keeps them off the render path; it
// yields no message because nothing in the model depends on the outcome. The seam is read HERE,
// when the copy builds its batch, not when the Cmd body runs: the body runs on a goroutine the copy
// never waits for, so a late read would reach whatever the seam holds by then — in a test, a real
// clipboard program once the recorder is restored, or the next test's recorder.
func systemClipboardCmd(workspace, text string) tea.Cmd {
	write := writeSystemClipboard
	return func() tea.Msg {
		_ = write(workspace, text)
		return nil
	}
}

// tmuxClipboardCmd returns a Cmd that hands text to tmux (writeTmuxClipboard), best-effort on the
// same terms as systemClipboardCmd: the error is swallowed — a tmux that refuses the buffer, or a
// tmux the exec fence refuses, must not turn a copy OSC 52 or the system write may well have
// landed into a reported failure — the lookup and the shell-out run off the Update goroutine, the
// Cmd yields no message, and the seam is read when the Cmd is built, not when it runs.
func tmuxClipboardCmd(workspace, text string) tea.Cmd {
	write := writeTmuxClipboard
	return func() tea.Msg {
		_ = write(workspace, text)
		return nil
	}
}
