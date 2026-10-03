package present

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/security"
)

// ErrNoClipboardProgram is the sentinel WriteSystem returns when this machine has no clipboard
// program to hand the text to: none of the candidates for its OS is on PATH, or the OS has no
// program route at all (Windows writes its clipboard through the Win32 API, which is the caller's
// business, not a program this type resolves). Callers test for it with errors.Is.
var ErrNoClipboardProgram = errors.New("present: no clipboard program available")

// ErrNoClipboardImage is the sentinel ReadImage returns when the clipboard holds no image it can
// read: no image-reading program is on PATH for this OS, or every one that is answered with nothing
// (the clipboard holds text, or is empty). It is not a failure — the caller pastes text instead.
var ErrNoClipboardImage = errors.New("present: no image on the clipboard")

// ErrClipboardImageTooLarge is what ReadImage returns when a program wrote more than
// maxClipboardImageRead bytes: an image far past any cap a message could carry, read no further.
var ErrClipboardImageTooLarge = errors.New("present: clipboard image too large")

// clipboardImageTimeout bounds one image-reading child: a helper that wedges waiting on a
// clipboard owner must not leave a ctrl+v waiting forever.
const clipboardImageTimeout = 2 * time.Second

// maxClipboardImageRead bounds what ReadImage reads from one program: comfortably past the domain's
// 5 MiB image cap even in the base64 PowerShell writes, so an image just over that cap is still
// read whole and refused by its size, never by a truncated read.
const maxClipboardImageRead = 16 * 1024 * 1024

// tmuxClipboardTimeout bounds the `tmux load-buffer` child: a wedged tmux server must not leave a
// caller waiting forever on a copy nothing waits for.
const tmuxClipboardTimeout = 2 * time.Second

// StdinRunner runs program with args, feeding stdin to its standard input, and returns its exit
// error. ctx bounds the run. program is the absolute path Clipboard resolved, never a bare name.
type StdinRunner func(ctx context.Context, stdin, program string, args ...string) error

// OutputRunner runs program with args and returns what it wrote to its standard output. ctx bounds
// the run. program is the absolute path Clipboard resolved, never a bare name.
type OutputRunner func(ctx context.Context, program string, args ...string) ([]byte, error)

// clipboardCandidate is one clipboard program and the arguments it takes — for a write, to read
// the text to copy from its standard input (atotto/clipboard v0.1.4's copy argv, carried
// verbatim); for an image read, to write the clipboard's PNG to its standard output. base64 marks
// a reader whose output is the image base64-encoded rather than raw.
type clipboardCandidate struct {
	program string
	args    []string
	base64  bool
}

var (
	wlCopyCandidate             = clipboardCandidate{program: "wl-copy"}
	xclipCandidate              = clipboardCandidate{program: "xclip", args: []string{"-in", "-selection", "clipboard"}}
	xselCandidate               = clipboardCandidate{program: "xsel", args: []string{"--input", "--clipboard"}}
	termuxClipboardSetCandidate = clipboardCandidate{program: "termux-clipboard-set"}
	clipExeCandidate            = clipboardCandidate{program: "clip.exe"}
	pbcopyCandidate             = clipboardCandidate{program: "pbcopy"}
)

// powershellImageScript writes the Windows clipboard's image as base64 PNG, or nothing when it
// holds none. Base64 because a PowerShell pipeline re-encodes raw bytes written to stdout.
const powershellImageScript = "Add-Type -AssemblyName System.Drawing; " +
	"$img = Get-Clipboard -Format Image; " +
	"if ($img) { $ms = New-Object System.IO.MemoryStream; " +
	"$img.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png); " +
	"[Convert]::ToBase64String($ms.ToArray()) }"

var (
	wlPasteImageCandidate    = clipboardCandidate{program: "wl-paste", args: []string{"--no-newline", "--type", "image/png"}}
	xclipImageCandidate      = clipboardCandidate{program: "xclip", args: []string{"-selection", "clipboard", "-t", "image/png", "-o"}}
	pngpasteCandidate        = clipboardCandidate{program: "pngpaste", args: []string{"-"}}
	powershellImageCandidate = clipboardCandidate{
		program: "powershell.exe",
		args:    []string{"-NoProfile", "-NonInteractive", "-Command", powershellImageScript},
		base64:  true,
	}
)

// unixClipboardGOOS is the set of operating systems atotto/clipboard v0.1.4 serves through its
// program ladder (its clipboard_unix.go build tags, plus the GOOS values Go builds under them:
// android under linux, illumos under solaris).
var unixClipboardGOOS = map[string]bool{
	"linux": true, "android": true, "freebsd": true, "netbsd": true, "openbsd": true,
	"solaris": true, "illumos": true, "dragonfly": true,
}

// Clipboard writes copied text through the host's own clipboard programs — the system clipboard
// helper (pbcopy, wl-copy, xclip, xsel, termux-clipboard-set, WSL's clip.exe) and tmux's paste
// buffer — and resolves every one of them through security.ResolveProgram against WorkspaceRoot
// before it runs. These writes run with no approval and no confinement box, on every copy the
// human makes, so "which bytes are `xclip`" must not be a question a workspace directory on the
// inherited PATH answers: a program that resolves inside the workspace is refused and nothing
// runs.
//
// The child's standard output and error are the null device (runWithStdin), so a helper that
// prints a warning never writes into the frame the TUI is drawing.
//
// Every input is injected. The zero value reads an empty environment, uses exec.LookPath and the
// real runner, and fences nothing — an empty root has no policy to apply
// (security.RefuseExecFromWritablePath).
type Clipboard struct {
	// GOOS picks the candidate list — runtime.GOOS in production, a table row's string in tests.
	GOOS string
	// Env is the environment lookup: WAYLAND_DISPLAY decides whether wl-copy leads the unix list
	// and TMUX whether WriteTmux runs at all. Nil reads as an empty environment.
	Env func(string) string
	// WorkspaceRoot is the workspace the model writes in — the fence no clipboard program may
	// resolve inside. It is the session workspace the file tools are scoped to.
	WorkspaceRoot string
	// LookPath resolves a program name to the absolute file PATH says it is: exec.LookPath in
	// production, a fake in tests. Nil means exec.LookPath.
	LookPath func(name string) (string, error)
	// Run starts the resolved program. Nil means runWithStdin, the production runner.
	Run StdinRunner
	// Output starts a resolved image-reading program and collects its standard output. Nil means
	// runForOutput, the production runner.
	Output OutputRunner
}

// WriteSystem writes text to the system clipboard through the first candidate program this OS
// has on PATH, in atotto/clipboard v0.1.4's order: wl-copy when WAYLAND_DISPLAY is set, then
// xclip, xsel, termux-clipboard-set and clip.exe on the unix family; pbcopy on darwin.
//
// It returns ErrNoClipboardProgram when no candidate is installed, the exec fence's refusal
// (security.ErrExecFromWritablePath) when the first candidate PATH answers with resolves inside
// the workspace or to a relative path — the walk stops there rather than quietly trying the next
// program, because a poisoned PATH is worth reporting — and the helper's own exit error otherwise.
// The run carries no deadline, as atotto's did: a clipboard helper either hands off and exits or
// fails at once.
func (c Clipboard) WriteSystem(text string) error {
	for _, candidate := range c.systemCandidates() {
		resolved, err := security.ResolveProgram(c.LookPath, candidate.program, c.WorkspaceRoot, nil)
		if errors.Is(err, security.ErrExecFromWritablePath) {
			return fmt.Errorf("present: refusing clipboard program %s: %w", candidate.program, err)
		}
		if err != nil {
			continue
		}
		return c.runner()(context.Background(), text, resolved, candidate.args...)
	}
	return ErrNoClipboardProgram
}

// WriteTmux runs `tmux load-buffer -w -` with text on stdin when Env("TMUX") is non-empty, under
// tmuxClipboardTimeout: tmux's own clipboard write, which it forwards to the outer terminal on its
// default `set-clipboard external` where an application's OSC 52 is dropped. Outside tmux it
// resolves and runs nothing and returns nil. tmux is resolved through security.ResolveProgram
// first; a refusal or a missing tmux is returned and nothing runs, as is a failed run.
func (c Clipboard) WriteTmux(text string) error {
	if c.env("TMUX") == "" {
		return nil
	}

	resolved, err := security.ResolveProgram(c.LookPath, "tmux", c.WorkspaceRoot, nil)
	if err != nil {
		return fmt.Errorf("present: tmux load-buffer: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), tmuxClipboardTimeout)
	defer cancel()
	if err := c.runner()(ctx, text, resolved, "load-buffer", "-w", "-"); err != nil {
		return fmt.Errorf("present: tmux load-buffer: %w", err)
	}
	return nil
}

// ReadImage reads an image off the system clipboard through the image-reading programs this OS
// has on PATH, in order: wl-paste when WAYLAND_DISPLAY is set, then xclip, on the unix family, and
// under WSL (WSL_DISTRO_NAME or WSL_INTEROP set) Windows PowerShell's Get-Clipboard; pngpaste on
// darwin. Each is resolved through security.ResolveProgram exactly as WriteSystem resolves its
// programs, and each run is bounded by clipboardImageTimeout.
//
// The first program that writes something answers: its bytes come back as read (base64-decoded
// for PowerShell), and telling whether they ARE an image is the caller's sniff. A program that
// fails or writes nothing — the clipboard holds text, or no image of the type asked for — passes
// to the next. ErrNoClipboardImage when none answers; the exec fence's refusal when a candidate
// resolves inside the workspace, which stops the walk as it does WriteSystem's; and
// ErrClipboardImageTooLarge when a program wrote past maxClipboardImageRead.
func (c Clipboard) ReadImage() ([]byte, error) {
	for _, candidate := range c.imageCandidates() {
		resolved, err := security.ResolveProgram(c.LookPath, candidate.program, c.WorkspaceRoot, nil)
		if errors.Is(err, security.ErrExecFromWritablePath) {
			return nil, fmt.Errorf("present: refusing clipboard program %s: %w", candidate.program, err)
		}
		if err != nil {
			continue
		}
		data, err := c.readCandidate(resolved, candidate)
		if errors.Is(err, ErrClipboardImageTooLarge) {
			return nil, err
		}
		if err == nil && len(data) > 0 {
			return data, nil
		}
	}
	return nil, ErrNoClipboardImage
}

// readCandidate runs one resolved image reader under clipboardImageTimeout and returns its output,
// decoded when the candidate writes base64.
func (c Clipboard) readCandidate(resolved string, candidate clipboardCandidate) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), clipboardImageTimeout)
	defer cancel()
	out, err := c.outputRunner()(ctx, resolved, candidate.args...)
	if err != nil {
		return nil, err
	}
	if !candidate.base64 {
		return out, nil
	}
	return base64.StdEncoding.DecodeString(string(bytes.TrimSpace(out)))
}

// imageCandidates is the ordered image-reader list for c.GOOS; empty where the OS has no program
// route (native Windows, whose clipboard image is not read through a program).
func (c Clipboard) imageCandidates() []clipboardCandidate {
	if c.GOOS == "darwin" {
		return []clipboardCandidate{pngpasteCandidate}
	}
	if !unixClipboardGOOS[c.GOOS] {
		return nil
	}

	candidates := make([]clipboardCandidate, 0, 3)
	if c.env("WAYLAND_DISPLAY") != "" {
		candidates = append(candidates, wlPasteImageCandidate)
	}
	candidates = append(candidates, xclipImageCandidate)
	if c.env("WSL_DISTRO_NAME") != "" || c.env("WSL_INTEROP") != "" {
		candidates = append(candidates, powershellImageCandidate)
	}
	return candidates
}

// systemCandidates is the ordered candidate list for c.GOOS; empty where the OS has no program
// route. Only the copy program is required on PATH — atotto also demanded each rung's paste
// partner (wl-paste, termux-clipboard-get, powershell.exe), which a write never runs.
func (c Clipboard) systemCandidates() []clipboardCandidate {
	if c.GOOS == "darwin" {
		return []clipboardCandidate{pbcopyCandidate}
	}
	if !unixClipboardGOOS[c.GOOS] {
		return nil
	}

	candidates := make([]clipboardCandidate, 0, 5)
	if c.env("WAYLAND_DISPLAY") != "" {
		candidates = append(candidates, wlCopyCandidate)
	}
	return append(candidates, xclipCandidate, xselCandidate, termuxClipboardSetCandidate, clipExeCandidate)
}

// env reads key through c.Env, a nil Env answering every key with "".
func (c Clipboard) env(key string) string {
	if c.Env == nil {
		return ""
	}
	return c.Env(key)
}

// runner is c.Run, or runWithStdin when unset.
func (c Clipboard) runner() StdinRunner {
	if c.Run == nil {
		return runWithStdin
	}
	return c.Run
}

// outputRunner is c.Output, or runForOutput when unset.
func (c Clipboard) outputRunner() OutputRunner {
	if c.Output == nil {
		return runForOutput
	}
	return c.Output
}

// runForOutput is the production OutputRunner: it starts program with args, no stdin and no
// stderr of ours (the null device, as runWithStdin's), collects at most maxClipboardImageRead
// bytes of its standard output and waits for it, or for ctx to kill it. A program that writes
// more is answered with ErrClipboardImageTooLarge.
func runForOutput(ctx context.Context, program string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	out := &cappedBuffer{limit: maxClipboardImageRead}
	cmd.Stdout = out
	err := cmd.Run()
	if out.over {
		return nil, ErrClipboardImageTooLarge
	}
	if err != nil {
		return nil, err
	}
	return out.buf.Bytes(), nil
}

// cappedBuffer is an io.Writer that keeps at most limit bytes and fails the write that would pass
// it, so a child writing without end is stopped by its broken pipe rather than read into memory.
type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
	over  bool
}

// Write keeps p, or refuses it whole once it would take the buffer past limit.
func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > b.limit {
		b.over = true
		return 0, ErrClipboardImageTooLarge
	}
	return b.buf.Write(p)
}

// runWithStdin is the production StdinRunner: it starts program with args, stdin as its standard
// input and no stdout or stderr of ours — a nil stream in [exec.Cmd] is the null device, so the
// child can never write into the frame — and waits for it, or for ctx to kill it. program is
// already absolute, so exec performs no PATH search of its own.
func runWithStdin(ctx context.Context, stdin, program string, args ...string) error {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Stdin = strings.NewReader(stdin)
	return cmd.Run()
}
