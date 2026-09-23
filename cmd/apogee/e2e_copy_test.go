package main

// The copy path end to end (plan "2026-09-23 - 00", item 2): a drag-select reaches the terminal as
// an OSC 52 sequence. The copy tests one layer down (internal/tui/mouse_test.go) assert a non-nil
// Cmd and the "copied N chars" flash, and neither proves the escape is ever WRITTEN — a copyFlash
// that dropped tea.SetClipboard would still flash, still return a Cmd, and put nothing on any
// clipboard. Only the bytes on the program's output show the copy left the process, so this test
// records them and asserts the exact sequence.

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The copy fixture's words: the prompt that is typed and dragged over, the word taken from it, the
// reply the upstream answers every completion with and the word taken from that, and the title the
// session record carries — distinct from the reply, so the reply's word is found on its transcript
// row rather than in wherever the frame shows the title.
const (
	copyPrompt     = "Please echo the marigold."
	copyPromptWord = "marigold"
	copyReply      = "The heliotrope is echoed back."
	copyReplyWord  = "heliotrope"
	copyTitle      = "Copy probe"
	// copyTitleRequest is the opening of apogee's session-title request — the one completion that
	// must not take the reply (see testdata/stubllm/smoke.yaml).
	copyTitleRequest = "The user's first request"
)

// osc52Recorder is the tee a copy test hands the launch: every byte the program writes, kept in
// order. The program writes from its own goroutine while the test reads, so both sides lock.
type osc52Recorder struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends p to the recording. It never fails, so the tee never cuts the driver's own screen
// off mid-frame.
func (r *osc52Recorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.buf.Write(p) //nolint:wrapcheck
}

// holds reports whether the recording contains seq.
func (r *osc52Recorder) holds(seq string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return bytes.Contains(r.buf.Bytes(), []byte(seq))
}

// osc52For is the sequence a copy of text writes: OSC 52 on the system clipboard (`c`), the text
// base64-encoded, BEL-terminated — spelled here rather than through the ansi package, so a change
// to what the program writes is caught instead of followed.
func osc52For(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
}

// dragAcross selects the cells [x0, x1) on row y the way a terminal in SGR mouse mode reports a
// drag: a left press at x0, a motion report with the button held (button 0 + the motion bit 32) at
// x1, and the release at x1. The release is what copies (mouse.go handleMouseRelease).
func dragAcross(drv *tuitest.Driver, x0, x1, y int) {
	drv.Press(tuitest.Click(x0, y))
	drv.Press(tuitest.Key(fmt.Sprintf("\x1b[<32;%d;%dM", x1+1, y+1)))
	drv.Press(tuitest.Release(x1, y))
}

// neutraliseCopyRoutes keeps the driven copy off the developer's real clipboards. The copy runs
// every route it has (copyFlash): tmux's is off with TMUX empty, and the system route's X11 and
// Wayland programs find no display to write to with DISPLAY and WAYLAND_DISPLAY empty. macOS and
// Windows need no display — pbcopy and the user32 clipboard write anyway — so the test skips there.
func neutraliseCopyRoutes(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skipf("the system-clipboard route on %s writes the real clipboard; no display to withhold",
			runtime.GOOS)
	}
	t.Setenv("TMUX", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
}

// copyWord drags across the first place word appears in the frame, waits for the flash, and
// asserts the recording holds the OSC 52 sequence for exactly that word.
func copyWord(t *testing.T, drv *tuitest.Driver, rec *osc52Recorder, word, where string) {
	t.Helper()

	x, y, ok := drv.Frame().Find(word)
	if !ok {
		t.Fatalf("the frame does not show %q in %s:\n%s", word, where, drv.Frame())
	}
	dragAcross(drv, x, x+len(word), y)
	drv.WaitText(fmt.Sprintf("copied %d chars", len(word)))

	want := osc52For(word)
	drv.WaitFor(func() bool { return rec.holds(want) },
		tuitest.Awaiting(fmt.Sprintf("the OSC 52 sequence for %q from %s", word, where)))
}

// TestE2ECopyWritesOSC52 drags over typed prompt text and, after a `--continue` relaunch, over a
// restored transcript row, and asserts each drag writes `ESC ] 52 ; c ; <base64> BEL` for exactly
// the text it took — the bytes a terminal acts on, not the Cmd that should produce them.
//
// What it cannot reach is tmux: a multiplexer between apogee and the terminal is invisible to an
// in-process test, so tmux dropping the application's OSC 52 under `set-clipboard external` (the
// reason copyFlash also runs `tmux load-buffer -w`) is pinned one layer down, at loadTmuxBuffer's
// gate and argv. Serial: it sets the environment.
func TestE2ECopyWritesOSC52(t *testing.T) {
	neutraliseCopyRoutes(t)
	stub := stubllm.New(t, stubllm.Script{Model: "copy-model", Turns: []stubllm.Turn{
		{When: &stubllm.Match{LastMessage: copyTitleRequest}, Repeat: true, Text: copyTitle},
		{Repeat: true, Text: copyReply},
	}})
	rec := &osc52Recorder{}
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIRecorded(t, drv, stub, rec)

	// The prompt copies the exact typed runes under the drag.
	drv.Type(copyPrompt)
	drv.WaitFor(func() bool {
		rows, ok := drv.Frame().PromptBox()
		return ok && strings.Contains(strings.Join(rows, "\n"), copyPrompt)
	}, tuitest.Awaiting("the typed prompt in the prompt box"))
	copyWord(t, drv, rec, copyPromptWord, "the prompt")

	// A transcript row restored by --continue copies the rendered text under the drag.
	drv.Press(tuitest.Enter)
	drv.WaitText(copyReply)
	drv.WaitQuiet(settled)
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
	next := sess.RelaunchWith("--continue")
	next.WaitText(copyReply)
	next.WaitQuiet(settled)
	copyWord(t, next, rec, copyReplyWord, "the resumed transcript")
}
