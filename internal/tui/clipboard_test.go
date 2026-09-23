package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// tmuxRun is one call a fake commandRunner saw: the argv and what was fed to stdin.
type tmuxRun struct {
	argv        []string
	stdin       string
	hasDeadline bool
}

// fakeRunner returns a commandRunner that records every call into runs and returns err.
func fakeRunner(runs *[]tmuxRun, err error) commandRunner {
	return func(ctx context.Context, stdin, name string, args ...string) error {
		_, hasDeadline := ctx.Deadline()
		*runs = append(*runs, tmuxRun{
			argv:        append([]string{name}, args...),
			stdin:       stdin,
			hasDeadline: hasDeadline,
		})
		return err
	}
}

// envWith returns a getenv that answers TMUX with tmux and every other key with "".
func envWith(tmux string) func(string) string {
	return func(key string) string {
		if key == "TMUX" {
			return tmux
		}
		return ""
	}
}

// TestTmuxClipboardStartsNothingOutsideTmux pins the gate: with TMUX empty or unset, a copy starts
// no tmux process at all and reports nothing.
func TestTmuxClipboardStartsNothingOutsideTmux(t *testing.T) {
	t.Parallel()
	var runs []tmuxRun

	err := loadTmuxBuffer(envWith(""), fakeRunner(&runs, nil), "hello")

	if err != nil {
		t.Fatalf("loadTmuxBuffer outside tmux = %v, want nil", err)
	}
	if len(runs) != 0 {
		t.Fatalf("outside tmux the runner was called %d times (%v), want never", len(runs), runs)
	}
}

// TestTmuxClipboardLoadsTheBufferInsideTmux pins the route itself: inside tmux the copy runs
// exactly `tmux load-buffer -w -`, once, under a deadline, with the copied text byte-for-byte on
// stdin — multi-byte and multi-line text included.
func TestTmuxClipboardLoadsTheBufferInsideTmux(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"hello", "héllo wörld ✓ 世界", "line one\nline two\n"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			var runs []tmuxRun

			err := loadTmuxBuffer(envWith("/tmp/tmux-1000/default,1,0"), fakeRunner(&runs, nil), text)

			if err != nil {
				t.Fatalf("loadTmuxBuffer inside tmux = %v, want nil", err)
			}
			if len(runs) != 1 {
				t.Fatalf("inside tmux the runner was called %d times, want once", len(runs))
			}
			if got := strings.Join(runs[0].argv, " "); got != "tmux load-buffer -w -" {
				t.Errorf("argv = %q, want %q", got, "tmux load-buffer -w -")
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

// TestTmuxClipboardReportsARunnerFailure pins that the helper passes a failed run up to its caller
// — swallowing it is tmuxClipboardCmd's job, not the helper's.
func TestTmuxClipboardReportsARunnerFailure(t *testing.T) {
	t.Parallel()
	refused := errors.New("exit status 1")
	var runs []tmuxRun

	err := loadTmuxBuffer(envWith("/tmp/tmux-1000/default,1,0"), fakeRunner(&runs, refused), "hello")

	if !errors.Is(err, refused) {
		t.Fatalf("loadTmuxBuffer = %v, want it to wrap %v", err, refused)
	}
}
