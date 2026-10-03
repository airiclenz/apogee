package tui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"github.com/airiclenz/apogee/internal/domain"
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

// The image paste: ctrl+v in the prompt asks the clipboard for an IMAGE first (present.Clipboard.ReadImage, through
// the same exec fence as the copy routes) and pastes its text only when there is none: the image
// probe is a Cmd, so the program it runs never blocks the Update loop, and its answer
// (clipboardImageMsg) either attaches the image as a pending one or hands the key back to the
// textarea's own clipboard paste (textarea.Paste), exactly what ctrl+v did before.

// readClipboardImage reads an image off the host clipboard, resolving the reader program against
// workspace (hostClipboard). A package-level variable for writeSystemClipboard's reason: a test
// substitutes a fake, since a real clipboard is the one thing a unit test cannot have. It is read
// when the Cmd is built (clipboardImageCmd), never when it runs.
var readClipboardImage = func(workspace string) ([]byte, error) {
	return hostClipboard(workspace).ReadImage()
}

// clipboardImageMsg is what the ctrl+v probe found: the bytes a clipboard reader wrote, or the
// error it ended on (present.ErrNoClipboardImage when the clipboard holds no image).
type clipboardImageMsg struct {
	data []byte
	err  error
}

// clipboardImageCmd returns the Cmd that probes the clipboard for an image on a ctrl+v. The seam
// is read here, when the key builds the Cmd, for systemClipboardCmd's reason.
func clipboardImageCmd(workspace string) tea.Cmd {
	read := readClipboardImage
	return func() tea.Msg {
		data, err := read(workspace)
		return clipboardImageMsg{data: data, err: err}
	}
}

// foldClipboardImage settles a ctrl+v. Bytes that sniff as an image are attached as the next
// clipboard-<n>.png pending image — unless the bound server does not accept images, which is
// said in the status line instead, since the engine would refuse the message they rode. Anything
// else — no reader, no image, bytes that are not an image — falls back to the textarea's own text
// paste. A box that stopped being editable while the probe ran takes neither. A reader the exec
// fence refused, or an image past the read bound, is said in the status line and pastes nothing.
func (m Model) foldClipboardImage(msg clipboardImageMsg) (tea.Model, tea.Cmd) {
	if !m.inputEditable() {
		return m, nil
	}
	if msg.err != nil && !errors.Is(msg.err, present.ErrNoClipboardImage) {
		return m, m.showFlash(fmt.Sprintf("clipboard image not read: %v", msg.err))
	}
	mediaType := imageMediaType(msg.data)
	if msg.err != nil || mediaType == "" {
		return m, textarea.Paste
	}
	if !m.serverAcceptsImages() {
		return m, m.showFlash(noVisionNote)
	}
	img := domain.Image{Name: m.nextClipboardName(), MediaType: mediaType, Data: msg.data}
	if err := m.attachImage(img); err != nil {
		return m, m.showFlash(err.Error())
	}
	return m, nil
}

// noVisionNote is the flash a clipboard image meets on a server that does not accept images.
const noVisionNote = "this server does not accept images: set vision: true on its servers: entry"

// visionReporter is the engine's optional answer to whether the bound server accepts image input
// (agent.Agent.Vision, through the composition root's holder). It is asked by assertion rather
// than added to [Engine] because only the image attach needs it.
type visionReporter interface {
	Vision() bool
}

// serverAcceptsImages reports whether an image attached now could be sent: the engine says its
// bound server has `vision: true`. An engine that cannot say — none bound, a test double — is
// answered no, so nothing is attached that the send would refuse.
func (m Model) serverAcceptsImages() bool {
	v, ok := m.eng.(visionReporter)
	return ok && v.Vision()
}

// imageSniffBytes is how many leading bytes imageMediaType needs: WebP's RIFF....WEBP header.
const imageSniffBytes = 12

// imageSignatures are the leading bytes of the image formats an attached image may carry, with
// the media type each is sent as — the engine's own @ref sniff (internal/agent, imageSignatures),
// restated because this package cannot import the engine (ADR 0010). WebP is a RIFF container,
// so its signature is checked at two offsets (imageMediaType). The prefixes are byte slices,
// not string literals: the PNG and JPEG magic is invalid UTF-8, which a literal would carry into
// the source (cmd/demorig's TestRasterResolvesEveryTUIRune reads every TUI literal as text).
var imageSignatures = []struct {
	prefix    []byte
	mediaType string
}{
	{[]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, "image/png"},
	{[]byte{0xff, 0xd8, 0xff}, "image/jpeg"},
	{[]byte("GIF87a"), "image/gif"},
	{[]byte("GIF89a"), "image/gif"},
}

// imageMediaType reports the media type of data when its leading bytes are a PNG, JPEG, GIF or
// WebP signature, and "" for anything else.
func imageMediaType(data []byte) string {
	for _, sig := range imageSignatures {
		if bytes.HasPrefix(data, sig.prefix) {
			return sig.mediaType
		}
	}
	if len(data) >= imageSniffBytes && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	return ""
}
