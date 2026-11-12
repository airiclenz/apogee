package main

// The start-up session log end to end (layout.md, "The start-up box"): a launch into a workspace
// carrying an AGENTS.md paints the routine context-files note folded inside one collapsed
// `✦ Session log (1 note)` block rather than on a line of its own, and the block cursor's ⏎ opens
// it onto the note verbatim. The unit tests in internal/tui pin what joins the log and how it
// paints; this pins the shipped frame a human meets at launch.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

const (
	// sessionLogModel is the id the stub advertises. It matches no built-in model profile, carries no
	// hint and no mandatory reasoning, so the first beat adds no routine note of its own and the one
	// note the log holds is the context-files line.
	sessionLogModel = "session-log-model"
	// sessionLogWindow is the window the stub advertises for it, so the first beat binds a KNOWN
	// window and says nothing about an unknown one.
	sessionLogWindow = 32768
	// sessionLogHeader is the collapsed log's counted header as the frame shows it.
	sessionLogHeader = "Session log (1 note)"
	// sessionLogContextNote is the routine note the log folds away: the AGENTS.md the launch loaded.
	sessionLogContextNote = "context: AGENTS.md"
)

// TestE2ESessionLogCollapsedThenExpanded launches with an AGENTS.md in the workspace and waits for
// the collapsed session log with no `context:` row on screen, then opens it from the keyboard and
// waits for the note's row.
func TestE2ESessionLogCollapsedThenExpanded(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, stubllm.Script{
		Model: sessionLogModel,
		Discovery: stubllm.Discovery{Models: []stubllm.DiscoveredModel{
			{ID: sessionLogModel, ContextLength: sessionLogWindow},
		}},
	})
	ws := e2eWorkspace(t)
	if err := os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte("house rules\n"), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	drv := tuitest.NewDriver(t, e2eSize)
	launchTUIIn(t, drv, stub, ws, "")

	drv.WaitText(sessionLogHeader)
	if frame := drv.Frame().String(); strings.Contains(frame, sessionLogContextNote) {
		t.Fatalf("the collapsed session log shows its %q note; collapsed, it is its header alone:\n%s",
			sessionLogContextNote, frame)
	}

	expandLastBlock(drv)
	drv.WaitText(sessionLogContextNote)
	if frame := drv.Frame().String(); !strings.Contains(frame, sessionLogHeader) {
		t.Errorf("the open session log lost its %q header:\n%s", sessionLogHeader, frame)
	}
}
