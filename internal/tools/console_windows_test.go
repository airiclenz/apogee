package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/console"
	"github.com/airiclenz/apogee/internal/domain"
)

// The apogee-2uh.6 hands-on check, driven on a real Windows host through the registered Console
// tools with the exact arguments the model sends. Ask-Before is a call with no confinement handle
// on its context; Auto is one whose handle carries the real Confiner and a box — the context a
// Confine verdict hands a call. The Approval demotion is read at the tool seam: the
// domain.ConfineDemoteError wrapping domain.ErrConfinementUnavailable is what the dispatch turns
// into an Approval prompt. What stays owner-run is only what the TUI shows: the prompt's wording
// after /mode auto and the Console pane's rendering.

// TestConsoleJourney_WindowsAskBeforeOpenSendReadClose drives the Ask-Before half: console_open of
// cmd, a non-raw console_send whose line runs (Enter is pressed after it), console_read returning
// the program's output with the terminal escapes stripped, and console_close tearing down the
// Console's whole process tree — a detached grandchild started from inside it included.
func TestConsoleJourney_WindowsAskBeforeOpenSendReadClose(t *testing.T) {
	t.Parallel()
	// The root is made before the registry, so the registry's CloseAll cleanup runs first and the
	// cmd.exe working in it is gone by the time the temp dir is removed.
	root := t.TempDir()
	ctx, registry := consoleTestCtx(t)

	opened, err := NewConsoleOpen(root, nil).Execute(ctx, consoleOpenCall("open", "cmd", 500))
	if err != nil || opened.IsError {
		t.Fatalf("console_open of cmd = %q (err=%v), want it opened", opened.Content, err)
	}
	target := openedConsole(t, registry)
	if target.Confined {
		t.Fatalf("console %d opened with no confinement handle is confined", target.ID)
	}

	// A non-raw send: the line only runs if console_send pressed Enter after it.
	sent, err := NewConsoleSend().Execute(ctx, consoleSendCall("send", target.ID, "echo ran>apogee-sent.txt", false, 200))
	if err != nil || sent.IsError {
		t.Fatalf("console_send = %q (err=%v), want the line typed", sent.Content, err)
	}
	waitForFile(t, filepath.Join(root, "apogee-sent.txt"))

	// The caret keeps the typed line (echoed by the terminal as APOGEE-READ-^MARK) from matching:
	// only the program's own output reads APOGEE-READ-MARK. The ping delays it past the send's
	// window, so it is console_read that collects it.
	const mark = "APOGEE-READ-MARK"
	line := "ping -n 3 127.0.0.1 >nul & echo APOGEE-READ-^MARK"
	if sent, err := NewConsoleSend().Execute(ctx, consoleSendCall("send2", target.ID, line, false, 100)); err != nil || sent.IsError {
		t.Fatalf("console_send = %q (err=%v), want the line typed", sent.Content, err)
	}
	read := readConsoleUntil(t, ctx, target.ID, mark)
	if strings.ContainsRune(read, 0x1b) {
		t.Errorf("console_read output %q carries a terminal escape; want it stripped", read)
	}

	// A detached grandchild the Console's cmd.exe cannot reach by parentage: only the job object
	// the Console runs in can reap it.
	pidFile := filepath.Join(root, "grandchild.pid")
	script := filepath.Join(root, "spawn.ps1")
	body := "$p = Start-Process -FilePath cmd.exe -ArgumentList '/c','ping -n 240 127.0.0.1' -PassThru -WindowStyle Hidden\r\n" +
		"Set-Content -LiteralPath '" + pidFile + "' -Value $p.Id\r\n"
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	spawn := `powershell -NoProfile -ExecutionPolicy Bypass -File "` + script + `"`
	if sent, err := NewConsoleSend().Execute(ctx, consoleSendCall("send3", target.ID, spawn, false, 100)); err != nil || sent.IsError {
		t.Fatalf("console_send = %q (err=%v), want the line typed", sent.Content, err)
	}
	grandchild := waitForPIDFile(t, pidFile)
	t.Cleanup(func() { killPID(grandchild) })
	if syscallKill0(grandchild) != nil {
		t.Fatalf("grandchild PID %d was gone before console_close; the test proves nothing", grandchild)
	}

	closed, err := NewConsoleClose().Execute(ctx, consoleCloseCall("close", target.ID))
	if err != nil || closed.IsError {
		t.Fatalf("console_close = %q (err=%v), want the Console closed", closed.Content, err)
	}
	if ids := registry.OpenIDs(); len(ids) != 0 {
		t.Errorf("OpenIDs() after console_close = %v, want none", ids)
	}
	if pidAlive(grandchild, 3*time.Second) {
		t.Errorf("detached grandchild PID %d survived console_close; the job object did not reap the tree", grandchild)
	}
}

// TestConsoleJourney_WindowsAutoConfinesTheOpenAndDemotesAnUnfencedSend drives the Auto half: a
// Console opened in Ask-Before stays unconfined, a console_open under the real Confiner's handle
// opens confined with no Go error — nothing for the dispatch to demote, so no Approval — and a
// console_send under that handle to the unconfined Console is demoted to Approval with the advice
// to close it and reopen it fenced, while the same send to the confined Console goes through.
func TestConsoleJourney_WindowsAutoConfinesTheOpenAndDemotesAnUnfencedSend(t *testing.T) {
	// Not parallel: the real confiner journals its labels under the real apogee home.
	askRoot := t.TempDir()
	autoRoot := t.TempDir()
	ctx, registry := consoleTestCtx(t)

	asked, err := NewConsoleOpen(askRoot, nil).Execute(ctx, consoleOpenCall("open1", "cmd /q", 200))
	if err != nil || asked.IsError {
		t.Fatalf("Ask-Before console_open = %q (err=%v), want an unconfined Console", asked.Content, err)
	}
	unconfined := openedConsole(t, registry)
	if unconfined.Confined {
		t.Fatalf("console %d opened in Ask-Before is confined", unconfined.ID)
	}

	// /mode auto: every later call carries the Confine verdict's handle.
	auto := domain.WithConfinement(ctx, domain.Confinement{
		Confiner: newWindowsTestConfiner(t),
		Box:      domain.ConfinementBox{WorkspaceRoot: autoRoot},
	})

	fenced, err := NewConsoleOpen(autoRoot, nil).Execute(auto, consoleOpenCall("open2", "cmd /q", 200))
	if err != nil {
		t.Fatalf("Auto console_open err = %v, want nil (a confined open needs no Approval)", err)
	}
	if fenced.IsError {
		t.Fatalf("Auto console_open produced an error result: %q", fenced.Content)
	}
	confined := newestConsole(t, registry, unconfined.ID)
	if !confined.Confined {
		t.Fatalf("console %d opened in Auto is not confined", confined.ID)
	}

	_, demoted := NewConsoleSend().Execute(auto, consoleSendCall("send1", unconfined.ID, "echo hi", false, 10))
	if !errors.Is(demoted, domain.ErrConfinementUnavailable) {
		t.Fatalf("Auto send to the unconfined Console = %v, want an error wrapping ErrConfinementUnavailable", demoted)
	}
	var demote *domain.ConfineDemoteError
	if !errors.As(demoted, &demote) {
		t.Errorf("Auto send to the unconfined Console = %v (%T), want a *domain.ConfineDemoteError", demoted, demoted)
	}
	if msg := demoted.Error(); !strings.Contains(msg, "close it") || !strings.Contains(msg, "reopen it") {
		t.Errorf("Auto send to the unconfined Console = %q, want the close-and-reopen advice", msg)
	}

	sent, err := NewConsoleSend().Execute(auto, consoleSendCall("send2", confined.ID, "echo in>apogee-auto.txt", false, 200))
	if err != nil || sent.IsError {
		t.Fatalf("Auto send to the confined Console = %q (err=%v), want it typed", sent.Content, err)
	}
	waitForFile(t, filepath.Join(autoRoot, "apogee-auto.txt"))
}

// readConsoleUntil calls console_read until its accumulated output contains want, failing the test
// at windowsConsoleWait, and returns everything it read.
func readConsoleUntil(t *testing.T, ctx context.Context, id int, want string) string {
	t.Helper()
	tool := NewConsoleRead()
	var read strings.Builder
	deadline := time.Now().Add(windowsConsoleWait)
	for !strings.Contains(read.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("console_read never returned %q within %v; read %q", want, windowsConsoleWait, read.String())
		}
		res, err := tool.Execute(ctx, consoleReadCall("read", id, 500))
		if err != nil || res.IsError {
			t.Fatalf("console_read = %q (err=%v)", res.Content, err)
		}
		read.WriteString(res.Content)
	}
	return read.String()
}

// newestConsole returns the one open Console other than except, after a second console_open.
func newestConsole(t *testing.T, registry *console.Registry, except int) *console.Console {
	t.Helper()
	for _, id := range registry.OpenIDs() {
		if id == except {
			continue
		}
		if found, ok := registry.Get(id); ok {
			return found
		}
	}
	t.Fatalf("no Console besides %d is open (OpenIDs() = %v)", except, registry.OpenIDs())
	return nil
}
