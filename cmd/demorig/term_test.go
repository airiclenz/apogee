//go:build !windows

package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// termTimeout bounds every terminal test: a program that hangs on an unanswered query fails here
// rather than hanging the suite.
const termTimeout = 5 * time.Second

// recordShell runs script under sh in a 40×5 terminal and returns the take once the shell exits.
func recordShell(t *testing.T, script string) *Take {
	t.Helper()
	term, err := StartTerminal("sh", []string{"-c", script},
		TermOptions{Cols: 40, Rows: 5, FPS: 30, Env: os.Environ()})
	if err != nil {
		t.Fatalf("StartTerminal: %v", err)
	}
	t.Cleanup(func() { term.Close() })
	select {
	case <-term.Done():
	case <-time.After(termTimeout):
		screen := rowText(term.Current(), 0)
		term.Close()
		t.Fatalf("the program did not exit within %s; row 0 reads %q", termTimeout, screen)
	}
	return term.Close()
}

// rowText is row y of a snapshot as plain text, trailing blanks trimmed.
func rowText(s Snapshot, y int) string {
	if y >= len(s.Cells) {
		return ""
	}
	var b strings.Builder
	for _, c := range s.Cells[y] {
		if c.Width == 0 {
			continue
		}
		if c.Rune == "" {
			b.WriteString(" ")
			continue
		}
		b.WriteString(c.Rune)
	}
	return strings.TrimRight(b.String(), " ")
}

func TestTerminal_RecordsStyledCells(t *testing.T) {
	t.Parallel()
	take := recordShell(t, `printf '\033[1;31mhi\033[0m'; sleep 0.2`)

	if take.Cols != 40 || take.Rows != 5 || take.FPS != 30 {
		t.Fatalf("take header = %dx%d@%d, want 40x5@30", take.Cols, take.Rows, take.FPS)
	}
	if len(take.Snapshots) == 0 {
		t.Fatal("the take holds no snapshot")
	}
	last := take.Snapshots[len(take.Snapshots)-1]
	for x, want := range []string{"h", "i"} {
		c := last.Cells[0][x]
		if c.Rune != want || c.Width != 1 {
			t.Fatalf("cell (%d,0) = %q width %d, want %q width 1", x, c.Rune, c.Width, want)
		}
		if c.FG != "@1" || !c.Bold {
			t.Fatalf("cell (%d,0) fg=%q bold=%v, want the palette red, bold", x, c.FG, c.Bold)
		}
		r, g, b, _ := c.FG.Color().RGBA()
		if r <= g || r <= b {
			t.Fatalf("cell (%d,0) resolves to rgb(%d,%d,%d), not red", x, r>>8, g>>8, b>>8)
		}
	}
	if c := last.Cells[0][2]; c.FG != "" || c.Bold {
		t.Fatalf("cell (2,0) after the reset = fg %q bold %v, want the default", c.FG, c.Bold)
	}
	if last.Cursor != (Cursor{X: 2, Y: 0, Visible: true}) {
		t.Fatalf("cursor = %+v, want {2 0 true}", last.Cursor)
	}
}

func TestTerminal_AnswersDA1(t *testing.T) {
	t.Parallel()
	// The shell asks the terminal who it is and will not go on until three bytes of the answer
	// arrive: an unanswered query hangs it, and recordShell fails on the timeout.
	take := recordShell(t, `stty -icanon -echo; printf '\033[c'; head -c 3 >/dev/null; printf answered`)

	last := take.Snapshots[len(take.Snapshots)-1]
	if got := rowText(last, 0); got != "answered" {
		t.Fatalf("row 0 = %q, want %q", got, "answered")
	}
}

func TestTerminal_UnchangedScreenAddsNoSnapshot(t *testing.T) {
	t.Parallel()
	// The second write repaints exactly what is already there.
	take := recordShell(t, `printf hi; sleep 0.3; printf '\rhi'; sleep 0.3`)

	for i := 1; i < len(take.Snapshots); i++ {
		if sameScreen(take.Snapshots[i-1], take.Snapshots[i]) {
			t.Fatalf("snapshots %d and %d show the same screen", i-1, i)
		}
		if gap := take.Snapshots[i].At - take.Snapshots[i-1].At; gap <= 0 {
			t.Fatalf("snapshot %d is %s after the one before it; times must increase", i, gap)
		}
	}
	if len(take.Snapshots) != 2 {
		t.Fatalf("the take holds %d snapshots, want 2 (the blank screen, then \"hi\")", len(take.Snapshots))
	}
	if got := rowText(take.Snapshots[1], 0); got != "hi" {
		t.Fatalf("row 0 = %q, want %q", got, "hi")
	}
}

func TestTerminal_EventsAndInput(t *testing.T) {
	t.Parallel()
	term, err := StartTerminal("sh", []string{"-c", `stty -icanon -echo; printf ready; head -c 2 >/dev/null; printf ' got'`},
		TermOptions{Cols: 20, Rows: 3, FPS: 30, Env: os.Environ()})
	if err != nil {
		t.Fatalf("StartTerminal: %v", err)
	}
	t.Cleanup(func() { term.Close() })
	// Typed before stty has run, the input would be echoed onto the screen.
	for deadline := time.Now().Add(termTimeout); rowText(term.Current(), 0) != "ready"; {
		if time.Now().After(deadline) {
			t.Fatal("the program never said it was ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	term.Event("type", "ok")
	if err := term.Send([]byte("ok")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case <-term.Done():
	case <-time.After(termTimeout):
		t.Fatal("the program never read its input")
	}
	if code := term.ExitCode(); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	take := term.Close()
	if len(take.Events) != 1 || take.Events[0].Kind != "type" || take.Events[0].Detail != "ok" {
		t.Fatalf("events = %+v, want the one type event", take.Events)
	}
	if got := rowText(take.Snapshots[len(take.Snapshots)-1], 0); got != "ready got" {
		t.Fatalf("row 0 = %q, want %q", got, "ready got")
	}
}

func TestTerminal_CloseKillsARunningProgram(t *testing.T) {
	t.Parallel()
	term, err := StartTerminal("sh", []string{"-c", "sleep 30"},
		TermOptions{Cols: 10, Rows: 2, FPS: 10, Env: os.Environ()})
	if err != nil {
		t.Fatalf("StartTerminal: %v", err)
	}
	done := make(chan *Take, 1)
	go func() { done <- term.Close() }()
	select {
	case take := <-done:
		if take == nil || len(take.Snapshots) == 0 {
			t.Fatal("Close returned no take")
		}
	case <-time.After(termTimeout):
		t.Fatal("Close did not return")
	}
}

func TestStartTerminal_RejectsBadOptions(t *testing.T) {
	t.Parallel()
	for _, opts := range []TermOptions{
		{Cols: 0, Rows: 5, FPS: 30},
		{Cols: 40, Rows: maxTermSize + 1, FPS: 30},
		{Cols: 40, Rows: 5, FPS: 0},
	} {
		if _, err := StartTerminal("true", nil, opts); err == nil {
			t.Errorf("StartTerminal(%+v) succeeded, want an error", opts)
		}
	}
}
