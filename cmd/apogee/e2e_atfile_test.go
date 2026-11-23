package main

// The "@" file menu through the whole program: the workspace walk behind it runs off the Update
// goroutine (internal/tui/filecache.go), so the first "@" typed shows nothing until the walk lands
// and then paints the workspace's files. Only the running program's Cmd runner closes that loop —
// a unit test's step() drops the Cmd — so this is where "the rows arrive" is held.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The files the menu's workspace holds, and the heading the open file menu paints.
const (
	atFileFirst  = "atfile-alpha.go"
	atFileSecond = "atfile-notes.md"
	atFileTitle  = "files"
)

// TestE2EAtFileMenuListsWorkspaceFiles types "@" at an idle prompt and waits for the menu to list the
// workspace's files: the walk the keystroke dispatched off the loop lands and opens the menu over it.
func TestE2EAtFileMenuListsWorkspaceFiles(t *testing.T) {
	t.Parallel()

	ws := t.TempDir()
	for _, name := range []string{atFileFirst, filepath.Join("docs", atFileSecond)} {
		path := filepath.Join(ws, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create the fixture dir: %v", err)
		}
		if err := os.WriteFile(path, []byte("fixture\n"), 0o600); err != nil {
			t.Fatalf("write the fixture file: %v", err)
		}
	}

	stub := stubllm.New(t, loadScript(t, "popups"))
	drv := tuitest.NewDriver(t, e2eSize)
	launchTUIIn(t, drv, stub, ws, "")
	waitIdle(drv)

	drv.Type("@")
	drv.WaitFor(func() bool {
		frame := drv.Frame().String()
		return strings.Contains(frame, "@"+atFileFirst) && strings.Contains(frame, "@docs/"+atFileSecond)
	}, tuitest.Awaiting("the @ menu to list the workspace's files once the walk lands"))
	if frame := drv.Frame().String(); !strings.Contains(frame, atFileTitle) {
		t.Errorf("the open @ menu is missing its %q title:\n%s", atFileTitle, frame)
	}
}
