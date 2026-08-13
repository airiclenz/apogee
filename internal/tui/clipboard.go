package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
)

// ----------------------------------------------------------------------------
// System-clipboard fallback for copy (mouse.go's copyFlash)
// ----------------------------------------------------------------------------
//
// OSC 52 (tea.SetClipboard) is the primary, SSH-safe copy channel and stays first, but a terminal
// is free to ignore the escape — several do, some only until the human turns the feature on — and
// then a copy that flashed "copied N chars" put nothing anywhere (the ISSUES defect). The
// fallback writes the same text through the host's own clipboard program, so the two channels
// cover each other: OSC 52 reaches the far end of an ssh session where no local program can, the
// system write reaches a local terminal that drops the escape.

// writeSystemClipboard writes text to the host's system clipboard. It is a package-level variable
// rather than a direct call so a test can substitute a recorder — the same injectable seam
// [Options.ExternalEditSpec] uses for the external editor, one level down: the platform program
// (pbcopy, xclip/xsel/wl-copy, clip.exe) is the one thing a unit test cannot have.
var writeSystemClipboard = clipboard.WriteAll

// systemClipboardCmd returns a Cmd that writes text to the system clipboard, best-effort. Any
// error is swallowed deliberately: the write runs BESIDE tea.SetClipboard, never instead of it, so
// a machine with no clipboard program (a bare Linux box, a container) must degrade to exactly
// today's OSC-52-only behaviour rather than report a failure for a copy that may well have landed.
// The Cmd body runs off the Update goroutine, which is what keeps the shell-out off the render
// path; it yields no message because nothing in the model depends on the outcome.
func systemClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		_ = writeSystemClipboard(text)
		return nil
	}
}
