//go:build !windows

package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRecordAndCaptureUsageErrorsExitTwo(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"record without a storyboard", []string{"record"}},
		{"record with two storyboards", []string{"record", "a.yaml", "b.yaml"}},
		{"record with an unknown flag", []string{"record", "a.yaml", "--pace", "2"}},
		{"capture without --upstream", []string{"capture", "a.yaml"}},
		{"capture without a storyboard", []string{"capture", "--upstream", "http://127.0.0.1:1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := newRootCommand()
			root.SetArgs(tc.args)
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			err := root.ExecuteContext(context.Background())
			if err == nil {
				t.Fatal("want a usage error, got none")
			}
			if got := exitCodeFor(err); got != exitBadUsage {
				t.Errorf("exit code: want %d, got %d (%v)", exitBadUsage, got, err)
			}
		})
	}
}

func TestRecordMissingCassetteFailsBeforeReset(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	marker := filepath.Join(work, "reset-ran")
	writeFakeRig(t, work, "#!/bin/sh\ntouch "+shellQuote(marker)+"\n", freePort(t))
	boardDir := t.TempDir()
	board := filepath.Join(boardDir, "clip.yaml")
	writeRigFile(t, board, tinyStoryboard("missing.cassette"))

	root := newRootCommand()
	root.SetArgs([]string{"record", board, "--work", work})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("want an error for a missing cassette, got none")
	}
	if got := exitCodeFor(err); got != exitRunFailed {
		t.Errorf("exit code: want %d, got %d", exitRunFailed, got)
	}
	if want := filepath.Join(boardDir, "missing.cassette"); !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name the cassette %s", err, want)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Errorf("reset.sh ran before the cassette was validated (marker stat: %v)", statErr)
	}
}

func TestRecordTakeStartsAtFirstPaint(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	port := freePort(t)
	writeFakeRig(t, work, "#!/bin/sh\necho stage reset\n", port)
	// The fake apogee prints a shell banner, then claims the alternate screen, clears it and
	// paints — what apogee's first frame does after the shell has spoken.
	bin := filepath.Join(work, "bin")
	writeRigFile(t, filepath.Join(bin, "apogee"), "#!/bin/sh\n"+
		"printf 'demo$ banner\\n'\nsleep 0.3\n"+
		"printf '\\033[?1049h\\033[2J\\033[Hpainted'\nsleep 30\n")
	if err := os.Chmod(filepath.Join(bin, "apogee"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRigFile(t, filepath.Join(work, "env.sh"), "export PATH="+shellQuote(bin)+":$PATH\necho env noise\n")

	r, err := openRig(work)
	if err != nil {
		t.Fatalf("openRig: %v", err)
	}
	board := &Storyboard{
		Clip:  "tiny",
		Frame: Frame{Cols: 40, Rows: 5, FPS: 30},
		Beats: []Beat{{ID: 1, Title: "open", Do: []Action{
			{Wait: &WaitAction{Screen: "painted", Timeout: 5 * time.Second}},
			{Pause: &PauseAction{For: 100 * time.Millisecond}},
		}}},
	}
	take, err := runTake(context.Background(), board, r, http.NotFoundHandler(), io.Discard)
	if err != nil {
		t.Fatalf("runTake: %v", err)
	}
	if len(take.Snapshots) == 0 {
		t.Fatal("the take holds no snapshot")
	}
	first := take.Snapshots[0]
	if first.At != 0 {
		t.Errorf("first snapshot at %s, want 0 — the take's clock starts at the first paint", first.At)
	}
	if got := rowText(first, 0); got != "painted" {
		t.Errorf("first snapshot row 0 = %q, want the first paint %q", got, "painted")
	}
	for i, snap := range take.Snapshots {
		for y := range snap.Cells {
			if row := rowText(snap, y); strings.Contains(row, "banner") || strings.Contains(row, "env noise") {
				t.Errorf("snapshot %d row %d shows pre-paint output %q", i, y, row)
			}
		}
	}
}

// TestRecordTakeEndsApogeeByQuitting pins how a take ends: apogee is quit, not killed, so a session
// it saves on the way out is on disk before the check reads it — and what it paints on the way out
// never reaches the take. The fake apogee saves its session only when it is interrupted, the way
// apogee's ⌃c⌃c flushes; a take that killed it would find no session.
func TestRecordTakeEndsApogeeByQuitting(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	writeFakeRig(t, work, "#!/bin/sh\n:\n", freePort(t))
	r, err := openRig(work)
	if err != nil {
		t.Fatalf("openRig: %v", err)
	}
	session := filepath.Join(r.sessionsDir(), "take.json")
	bin := filepath.Join(work, "bin")
	writeRigFile(t, filepath.Join(bin, "apogee"), "#!/bin/sh\n"+
		"trap 'trap \"\" INT; sleep 0.2; mkdir -p "+shellQuote(r.sessionsDir())+"; "+
		"echo {} > "+shellQuote(session)+"; printf \"\\033[2J\\033[Hgoodbye\"; exit 0' INT\n"+
		"printf '\\033[?1049h\\033[2J\\033[Hpainted'\nwhile :; do sleep 0.05; done\n")
	if err := os.Chmod(filepath.Join(bin, "apogee"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRigFile(t, filepath.Join(work, "env.sh"), "export PATH="+shellQuote(bin)+":$PATH\n")
	board := &Storyboard{
		Clip:  "tiny",
		Frame: Frame{Cols: 40, Rows: 5, FPS: 30},
		Beats: []Beat{{ID: 1, Title: "open", Do: []Action{
			{Wait: &WaitAction{Screen: "painted", Timeout: 5 * time.Second}},
		}}},
	}

	take, err := runTake(context.Background(), board, r, http.NotFoundHandler(), io.Discard)
	if err != nil {
		t.Fatalf("runTake: %v", err)
	}
	if _, err := os.Stat(session); err != nil {
		t.Errorf("the session apogee saves on quitting is not on disk after the take: %v", err)
	}
	for i, snap := range take.Snapshots {
		if row := rowText(snap, 0); strings.Contains(row, "goodbye") {
			t.Errorf("snapshot %d at %s shows what apogee painted on its way out: %q", i, snap.At, row)
		}
	}
}

// writeFakeRig lays out a work dir openRig accepts: env.sh, the given reset.sh and a rig.env
// naming port.
func writeFakeRig(t *testing.T, work, reset string, port int) {
	t.Helper()
	writeRigFile(t, filepath.Join(work, "env.sh"), ":\n")
	writeRigFile(t, filepath.Join(work, "reset.sh"), reset)
	if err := os.Chmod(filepath.Join(work, "reset.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRigFile(t, filepath.Join(work, "rig.env"), "KEY_ENV=\n"+rigPortKey+"="+strconv.Itoa(port)+"\n")
}

// writeRigFile writes content to path, creating its directory.
func writeRigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
}

// freePort is a loopback port nothing listened on a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

// tinyStoryboard is a one-beat storyboard naming cassette.
func tinyStoryboard(cassette string) string {
	return "clip: tiny\nship: tiny.gif\ncassette: " + cassette + "\nfonts: fonts\n" +
		"frame: {cols: 40, rows: 5, padding: 0, font_size: 14, line_height: 1.2, scale: 1, width: 400, fps: 24, max_colors: 64}\n" +
		"beats:\n  - id: 1\n    title: open\n    do:\n      - pause: {for: 100ms}\n    duration: 1s\n"
}
