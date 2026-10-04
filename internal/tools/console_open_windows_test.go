package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/console"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
)

// The Windows Console journeys: console_open under a confinement box runs cmd.exe behind a
// pseudoconsole under the real Confiner's restricted token, and console_send to a Console opened
// unconfined is demoted with the same reopen-fenced advice POSIX gives. They drive the real host
// and the exact console_open / console_send arguments the model sends. They never use
// fakeConfiner: it sets no token, and a Windows confined open without one fails closed.

// windowsConsoleWait bounds how long a test waits for a confined cmd.exe to act.
const windowsConsoleWait = 15 * time.Second

// newWindowsTestConfiner returns the real Windows Confiner, closed when the test ends — it
// journals its labels under the real apogee home, and a journal left behind is replayed by the
// next session's Recover. It skips on a host whose Confiner cannot fence writes.
func newWindowsTestConfiner(t *testing.T) domain.Confiner {
	t.Helper()
	confiner := platform.NewConfiner()
	if closer, ok := confiner.(io.Closer); ok {
		t.Cleanup(func() { _ = closer.Close() })
	}
	if !confiner.Capabilities().FSWrite {
		t.Skipf("this host's Confiner (%T) cannot fence writes", confiner)
	}
	return confiner
}

// windowsConfinedCtx returns a Console-tool context whose confinement handle carries the real
// Confiner and box — the context a Confine verdict (Auto, confine-to-workspace on) hands a call.
func windowsConfinedCtx(t *testing.T, box domain.ConfinementBox) (context.Context, *console.Registry) {
	t.Helper()
	ctx, registry := consoleTestCtx(t)
	return domain.WithConfinement(ctx, domain.Confinement{
		Confiner: newWindowsTestConfiner(t),
		Box:      box,
	}), registry
}

// openedConsole returns the one Console the registry holds after a successful console_open.
func openedConsole(t *testing.T, registry *console.Registry) *console.Console {
	t.Helper()
	ids := registry.OpenIDs()
	if len(ids) != 1 {
		t.Fatalf("OpenIDs() = %v, want exactly one opened Console", ids)
	}
	opened, ok := registry.Get(ids[0])
	if !ok {
		t.Fatalf("Get(%d) found nothing", ids[0])
	}
	return opened
}

// waitForFile polls until path exists, failing the test at windowsConsoleWait.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(windowsConsoleWait)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not appear within %v", path, windowsConsoleWait)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestConsoleOpen_ConfinesThroughTheHandleOnContextOnWindows proves the Confine handoff happens on
// the command the pseudoconsole actually starts: console_open under a box opens an interactive
// cmd.exe recorded as confined, a line typed into it with console_send under the same box writes
// inside the workspace, and the same line's write outside the box is refused by the token.
func TestConsoleOpen_ConfinesThroughTheHandleOnContextOnWindows(t *testing.T) {
	// Not parallel: the real confiner journals its labels under the real apogee home.
	root := t.TempDir()
	outside := t.TempDir()
	ctx, registry := windowsConfinedCtx(t, domain.ConfinementBox{WorkspaceRoot: root})

	res, err := NewConsoleOpen(root, nil).Execute(ctx, consoleOpenCall("c1", "cmd /q", 500))
	if err != nil {
		t.Fatalf("console_open err = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("console_open produced an error result: %q", res.Content)
	}
	opened := openedConsole(t, registry)
	if !opened.Confined {
		t.Fatalf("console %d opened under a box is not confined", opened.ID)
	}

	// The write outside runs first, so by the time the inside file exists it has been tried.
	outsideFile := filepath.Join(outside, "apogee-outside.txt")
	line := fmt.Sprintf(`echo out>"%s" & echo in>apogee-inside.txt`, outsideFile)
	sent, err := NewConsoleSend().Execute(ctx, consoleSendCall("c2", opened.ID, line, false, 500))
	if err != nil || sent.IsError {
		t.Fatalf("console_send to the confined Console = %q (err=%v), want it typed", sent.Content, err)
	}

	waitForFile(t, filepath.Join(root, "apogee-inside.txt"))
	if _, err := os.Stat(outsideFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the confined Console wrote outside its box (%s): stat err = %v", outsideFile, err)
	}
}

// TestConsoleOpen_ConfinedConsoleCarriesTheSeededScratchEnvOnWindows pins that a confined Windows
// Console starts with what the subprocess funnel gives every other confined run: TMPDIR (and the
// rest of subprocess.ScratchEnv) beneath the box's ScratchDir. It is read inside the running
// cmd.exe, so the seed measured is the seed the pseudoconsole child starts with; the check writes
// a marker file rather than echoing the path, which the pseudoconsole may wrap.
func TestConsoleOpen_ConfinedConsoleCarriesTheSeededScratchEnvOnWindows(t *testing.T) {
	// Not parallel: the real confiner journals its labels under the real apogee home.
	root := t.TempDir()
	scratch := filepath.Join(t.TempDir(), "scratch")
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, registry := windowsConfinedCtx(t, domain.ConfinementBox{WorkspaceRoot: root, ScratchDir: scratch})
	wantTmp := filepath.Join(scratch, "tmp")
	command := fmt.Sprintf(`if /i "%%TMPDIR%%"=="%s" (echo seeded>apogee-scratch.txt) else (echo missing>apogee-scratch.txt)`, wantTmp)

	res, err := NewConsoleOpen(root, nil).Execute(ctx, consoleOpenCall("c1", command, 500))
	if err != nil {
		t.Fatalf("console_open err = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("console_open produced an error result: %q", res.Content)
	}
	if opened := openedConsole(t, registry); !opened.Confined {
		t.Fatalf("console %d opened under a box is not confined", opened.ID)
	}

	marker := filepath.Join(root, "apogee-scratch.txt")
	waitForFile(t, marker)
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "seeded" {
		t.Errorf("the confined Console's TMPDIR is not %q (marker = %q)", wantTmp, got)
	}
	if info, err := os.Stat(wantTmp); err != nil || !info.IsDir() {
		t.Errorf("scratch/tmp was not created by the hook: %v", err)
	}
}

// TestConsoleSend_UnconfinedConsoleIsDemotedUnderABoxOnWindows pins the Windows half of the send
// fence (ADR 0059, Amendment 2026-10-04): a Windows Console open can confine, so a cmd.exe Console
// opened unconfined (in Ask-Before, before a switch to Auto) is demoted on a send under a box with
// the POSIX advice — a domain.ConfineDemoteError telling the model to close it and reopen it
// fenced — through the real host's rules, not an override. The box's fakeConfiner is never
// called: a send confines nothing, it only reads whether the target was opened confined.
func TestConsoleSend_UnconfinedConsoleIsDemotedUnderABoxOnWindows(t *testing.T) {
	t.Parallel()
	// The root is made before the registry, so the registry's CloseAll cleanup runs first and the
	// cmd.exe working in it is gone by the time the temp dir is removed.
	root := t.TempDir()
	ctx, registry := consoleTestCtx(t)
	res, err := NewConsoleOpen(root, nil).Execute(ctx, consoleOpenCall("open", "cmd /q", 200))
	if err != nil || res.IsError {
		t.Fatalf("console_open = %q (err=%v), want an unconfined Console", res.Content, err)
	}
	opened := openedConsole(t, registry)
	if opened.Confined {
		t.Fatalf("console %d was opened confined; the test needs an unconfined one", opened.ID)
	}
	id := opened.ID

	_, boxedErr := NewConsoleSend().Execute(withFSConfinement(t, ctx), consoleSendCall("c1", id, "echo hi", false, 10))

	if !errors.Is(boxedErr, domain.ErrConfinementUnavailable) {
		t.Fatalf("send under a box = %v, want an error wrapping ErrConfinementUnavailable", boxedErr)
	}
	var demote *domain.ConfineDemoteError
	if !errors.As(boxedErr, &demote) {
		t.Errorf("send under a box = %v (%T), want a *domain.ConfineDemoteError", boxedErr, boxedErr)
	}
	if msg := boxedErr.Error(); !strings.Contains(msg, "close it") || !strings.Contains(msg, "reopen it") {
		t.Errorf("send under a box = %q, want it to tell the model to close the console and reopen it", msg)
	}
}
