package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/daemon"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/snapshot"
)

// ---------------------------------------------------------------------------
// `apogee undo <session-id> [confirm]` (ADR 0074, bead apogee-kk0.7)
// ---------------------------------------------------------------------------

// undoStore seeds one saved session's snapshot store the way a Firing would have left it: an image
// of the workspace before the exchange, the change the agent made, and the closing image that makes
// the pair a step. The change callback is handed the temporary workspace it imaged, so a test names
// its own files.
//
// It goes through snapshot.OpenJournal rather than reaching into the store, because that is the
// call the verb itself makes: a fixture built any other way could pass while the seam the verb
// depends on was broken.
func undoStore(t *testing.T, home, id string, change func(workspace string)) {
	t.Helper()

	workspace := t.TempDir()
	journal, reason, err := snapshot.OpenJournal(context.Background(), home, id, workspace, true)
	if err != nil {
		t.Fatalf("OpenJournal: %v", err)
	}
	if reason != "" {
		t.Fatalf("OpenJournal fell back to the in-memory journal: %s", reason)
	}
	if err := journal.MarkPre(context.Background()); err != nil {
		t.Fatalf("MarkPre: %v", err)
	}

	change(workspace)

	if err := journal.Close(context.Background()); err != nil {
		t.Fatalf("Close the exchange: %v", err)
	}
}

// runUndoCmd executes one `apogee undo` invocation against home and returns its stdout and error.
func runUndoCmd(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()

	cmd := newUndoCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append(args, "--config", home))

	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

// TestUndoVerbPreviewsThenReverts is the verb's headline: the same two steps `/undo` offers, from a
// fresh process that was never in the session. The preview discloses the path and touches nothing;
// the confirm puts the file back exactly as the exchange found it.
func TestUndoVerbPreviewsThenReverts(t *testing.T) {
	requireSnapshotStore(t)

	home := t.TempDir()
	var file string
	undoStore(t, home, "s-undo-1", func(ws string) {
		file = filepath.Join(ws, "notes.txt")
		if err := os.WriteFile(file, []byte("what the agent wrote"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	})

	preview, err := runUndoCmd(t, home, "s-undo-1")
	if err != nil {
		t.Fatalf("the preview failed: %v", err)
	}
	if !strings.Contains(preview, "apogee undo s-undo-1 — exchange 1:") {
		t.Errorf("the preview is not headed by the command that was typed: %q", preview)
	}
	if !strings.Contains(preview, file) {
		t.Errorf("the preview does not disclose the recorded path %q: %q", file, preview)
	}
	if !strings.Contains(preview, "apogee undo s-undo-1 confirm applies this") {
		t.Errorf("the preview does not close with the line that executes it: %q", preview)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("the preview touched the workspace: %v", err)
	}

	report, err := runUndoCmd(t, home, "s-undo-1", "confirm")
	if err != nil {
		t.Fatalf("the confirm failed: %v", err)
	}
	if !strings.Contains(report, "undone — exchange 1:") {
		t.Errorf("the confirm did not report what it did: %q", report)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("the file the exchange created is still there after the revert (err %v)", err)
	}

	// The step is spent: a second confirm has nothing left, and says so rather than walking into
	// an exchange the human never previewed.
	if _, err := runUndoCmd(t, home, "s-undo-1", "confirm"); err == nil ||
		!strings.Contains(err.Error(), "nothing to undo for session s-undo-1") {
		t.Errorf("a second confirm answered %v, want the empty-journal refusal", err)
	}
}

// TestUndoVerbRestoresAFileTheExchangeChanged is the other half of the coverage: a file that existed
// before the exchange comes back with its ORIGINAL bytes, not merely removed.
func TestUndoVerbRestoresAFileTheExchangeChanged(t *testing.T) {
	requireSnapshotStore(t)

	home := t.TempDir()
	var file string
	undoStore(t, home, "s-undo-2", func(ws string) {
		file = filepath.Join(ws, "kept.txt")
		if err := os.WriteFile(file, []byte("the human's own text"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	})
	// The seed above created the file INSIDE the exchange, so re-seed the pair around an edit: a
	// second exchange whose pre-image already holds the file is what a restore needs.
	journal, reason, err := snapshot.OpenJournal(context.Background(), home, "s-undo-2",
		filepath.Dir(file), true)
	if err != nil || reason != "" {
		t.Fatalf("reopen the store: %v (%s)", err, reason)
	}
	if err := journal.MarkPre(context.Background()); err != nil {
		t.Fatalf("MarkPre: %v", err)
	}
	if err := os.WriteFile(file, []byte("what the agent replaced it with"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := journal.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := runUndoCmd(t, home, "s-undo-2", "confirm"); err != nil {
		t.Fatalf("the confirm failed: %v", err)
	}

	back, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read the reverted file: %v", err)
	}
	if string(back) != "the human's own text" {
		t.Errorf("the reverted file holds %q, want the bytes the exchange found", back)
	}
}

// TestUndoVerbRefusesAnIdThatIsNotAPathComponent pins the gate that stands between a command line
// and a path joined onto the apogee home: the id is a single clean component or the verb refuses it,
// so `apogee undo ../../.ssh` can never name a directory outside the snapshot root.
func TestUndoVerbRefusesAnIdThatIsNotAPathComponent(t *testing.T) {
	home := t.TempDir()

	for _, id := range []string{"..", "../elsewhere", "a/b", `a\b`, ".hidden", "with\nnewline"} {
		t.Run(id, func(t *testing.T) {
			out, err := runUndoCmd(t, home, id)
			if err == nil {
				t.Fatalf("the verb accepted the id %q and printed %q", id, out)
			}
			if !strings.Contains(err.Error(), "apogee undo:") {
				t.Errorf("the refusal of %q reads %q, want this command's own refusal", id, err)
			}
		})
	}

	// And the store root stays untouched: a refused id must not have reached far enough to create
	// anything under the apogee home.
	if _, err := os.Stat(filepath.Join(home, "snapshots")); !os.IsNotExist(err) {
		t.Errorf("a refused id still created a snapshot root (err %v)", err)
	}
}

// TestUndoVerbOnAnUnknownSessionSaysNothingToUndo covers the ordinary mistake — a mistyped or swept
// id — and pins the read-only pledge that goes with it: the index is read before the store is
// opened, so an id that names nothing leaves no freshly initialised object database behind.
func TestUndoVerbOnAnUnknownSessionSaysNothingToUndo(t *testing.T) {
	home := t.TempDir()

	_, err := runUndoCmd(t, home, "s-never-existed")

	if err == nil || !strings.Contains(err.Error(), "nothing to undo for session s-never-existed") {
		t.Fatalf("the verb answered %v, want the nothing-to-undo refusal", err)
	}
	if _, err := os.Stat(filepath.Join(home, "snapshots", "s-never-existed")); !os.IsNotExist(err) {
		t.Errorf("an unknown id still opened a store (err %v)", err)
	}
}

// TestUndoVerbHonoursTheApogeeConfigVariable: the home is resolved on the documented precedence
// (--config > APOGEE_CONFIG > ~/.apogee), and this verb has to spell the env read itself because it
// loads no config file and so never reaches ApplyConfig. Without it, a human whose home is set by
// the variable would be told a session they can see has nothing to undo.
func TestUndoVerbHonoursTheApogeeConfigVariable(t *testing.T) {
	requireSnapshotStore(t)

	home := t.TempDir()
	undoStore(t, home, "s-undo-8", func(ws string) {
		if err := os.WriteFile(filepath.Join(ws, "note.txt"), []byte("written"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	})
	t.Setenv(config.EnvConfig, home)

	cmd := newUndoCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"s-undo-8"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("the verb failed with the home in the environment: %v", err)
	}
	if !strings.Contains(out.String(), "exchange 1:") {
		t.Errorf("the preview did not find the store the variable names: %q", out.String())
	}
}

// TestUndoVerbWithoutGitNamesTheReason: the store is a git object database and git is a convenience
// dependency (ADR 0042 decision 2), so a host without it gets the REASON rather than a bare failure
// — the same sentence `/undo` names in the session.
func TestUndoVerbWithoutGitNamesTheReason(t *testing.T) {
	requireSnapshotStore(t)

	home := t.TempDir()
	undoStore(t, home, "s-undo-3", func(ws string) {
		if err := os.WriteFile(filepath.Join(ws, "note.txt"), []byte("written"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	})

	// The store is seeded; now take git away from the process that would read it.
	t.Setenv("PATH", "")

	_, err := runUndoCmd(t, home, "s-undo-3")

	if err == nil || !strings.Contains(err.Error(), "undo snapshots unavailable: git not found") {
		t.Fatalf("the verb answered %v, want the unavailable-with-reason refusal", err)
	}
}

// TestUndoVerbRefusesAWorkspaceFlag: the store images ONE tree and journal.json says which, so a
// caller-supplied workspace could only ever restore one project's files over another's. The flag is
// not registered at all, which is what makes the refusal unconditional.
func TestUndoVerbRefusesAWorkspaceFlag(t *testing.T) {
	home := t.TempDir()

	_, err := runUndoCmd(t, home, "s-undo-4", "--workspace", t.TempDir())

	if err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("the verb answered %v, want it to refuse the --workspace flag", err)
	}
}

// TestUndoVerbRefusesASecondArgumentThatIsNotConfirm: a human who typed `apogee undo <id> yes` meant
// to revert. Previewing silently would leave them believing the files were put back.
func TestUndoVerbRefusesASecondArgumentThatIsNotConfirm(t *testing.T) {
	home := t.TempDir()

	_, err := runUndoCmd(t, home, "s-undo-5", "yes")

	if err == nil || !strings.Contains(err.Error(), `"confirm"`) {
		t.Fatalf("the verb answered %v, want a refusal naming the word it takes", err)
	}
}

// TestUndoVerbIsRegistered fails when the binary ships without the verb the unattended Drivers point
// their readers at — a report naming a command that does not exist is worse than no report.
func TestUndoVerbIsRegistered(t *testing.T) {
	t.Parallel()

	for _, sub := range subcommands() {
		if sub.Name() == "undo" {
			return
		}
	}
	t.Fatal("`undo` is not registered in subcommands()")
}

// TestUndoVerbReadsTheWorkspaceFromTheIndex pins the two facts the verb composes for itself because
// internal/snapshot keeps them unexported: the index file's NAME, beside the objects at snapshot.Dir,
// and the workspace field inside it. A rename on either side would otherwise turn every session into
// "nothing to undo" silently.
func TestUndoVerbReadsTheWorkspaceFromTheIndex(t *testing.T) {
	requireSnapshotStore(t)

	home := t.TempDir()
	var workspace string
	undoStore(t, home, "s-undo-7", func(ws string) {
		workspace = ws
		if err := os.WriteFile(filepath.Join(ws, "note.txt"), []byte("written"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	})

	read, err := undoIndexWorkspace(snapshot.Dir(home, "s-undo-7"))

	if err != nil {
		t.Fatalf("undoIndexWorkspace: %v", err)
	}
	if read != workspace {
		t.Errorf("the index names the workspace %q, want %q", read, workspace)
	}
}

// ---------------------------------------------------------------------------
// The `undo with:` line the unattended Drivers close their report with
// ---------------------------------------------------------------------------

// TestHeadlessOffersTheUndoVerbForItsWrites pins the offer and its three conditions. The report of
// what a run changed is only half an account if the human reading it afterwards has no way to act on
// it — and pointing them at a command that would answer "nothing to undo" is worse than silence.
func TestHeadlessOffersTheUndoVerbForItsWrites(t *testing.T) {
	t.Run("a saved run whose journal persisted names the command", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{
			SessionID: "s-42", FinalText: "the answer", Turns: 1, Wrote: []string{"/ws/new.go"},
		}}

		_, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}

		if !strings.Contains(errOut, "  undo with: apogee undo s-42") {
			t.Errorf("the report does not offer the revert verb: %q", errOut)
		}
		if changed, undo := strings.Index(errOut, "changed — "), strings.Index(errOut, "undo with:"); changed > undo {
			t.Errorf("the offer printed above the block it belongs under: %q", errOut)
		}
	})

	// UndoNote is why the journal was the in-memory funnel one, and a funnel journal died with the
	// process: there is nothing on disk for the verb to open.
	t.Run("a run whose journal was the in-memory one offers nothing", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{
			SessionID: "s-43", FinalText: "the answer", Turns: 1,
			Wrote: []string{"/ws/new.go"}, UndoNote: "git not found",
		}}

		_, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}

		if strings.Contains(errOut, "undo with:") {
			t.Errorf("a run with no persistent journal still offered a revert: %q", errOut)
		}
	})

	t.Run("a run that saved no record offers nothing", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{
			FinalText: "the answer", Turns: 1, Wrote: []string{"/ws/new.go"},
		}}

		_, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}

		if strings.Contains(errOut, "undo with:") {
			t.Errorf("a run with no record id still offered a revert: %q", errOut)
		}
	})

	t.Run("a run that changed nothing offers nothing", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{SessionID: "s-44", FinalText: "the answer", Turns: 1}}

		_, errOut, err := headlessRun(t, stub, "a prompt")
		if err != nil {
			t.Fatalf("headless: %v", err)
		}

		if strings.Contains(errOut, "undo with:") {
			t.Errorf("a run that wrote nothing still offered a revert: %q", errOut)
		}
	})
}

// TestDaemonFireLogsTheUndoVerb: the daemon's log is the only place a supervisor sees a Firing's
// session id and its writes together, so it carries the same offer in the same wording — both
// Drivers compose it once (undoVerbLine, headless.go).
func TestDaemonFireLogsTheUndoVerb(t *testing.T) {
	harness := newDaemonFireHarness(t, config.Options{
		Servers: []config.ServerEntry{{Name: "box", Endpoint: "http://box.invalid"}},
	})
	harness.runner.res = run.Result{SessionID: "s-45", Turns: 1, Wrote: []string{"/ws/new.go"}}

	harness.fire(t, entryFor(t, "nightly", daemon.Action{Server: "box"}))

	if logged := harness.logged.String(); !strings.Contains(logged, "undo with: apogee undo s-45") {
		t.Errorf("the daemon log does not offer the revert verb:\n%s", logged)
	}
}

// TestHeadlessReportsAnUndoTheVerbCanActuallyPerform is the end-to-end claim the two halves of this
// item make together: the command the report NAMES, typed as printed, reverts the Firing's writes.
// It lifts the line out of stderr rather than composing the command itself, because the whole point
// is that what a human reads is what a human can run.
func TestHeadlessReportsAnUndoTheVerbCanActuallyPerform(t *testing.T) {
	requireSnapshotStore(t)

	home := testConfigHome(t, "")
	var file string
	undoStore(t, home, "s-undo-6", func(ws string) {
		file = filepath.Join(ws, "written.txt")
		if err := os.WriteFile(file, []byte("what the firing wrote"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	})

	stub := &stubRunner{res: run.Result{
		SessionID: "s-undo-6", FinalText: "done", Turns: 1, Wrote: []string{file},
	}}
	_, errOut, err := headlessRunOn(t, stub, fenceableHost, home, "a prompt")
	if err != nil {
		t.Fatalf("headless: %v", err)
	}

	id := undoIDFromReport(t, errOut)
	if _, err := runUndoCmd(t, home, id, "confirm"); err != nil {
		t.Fatalf("the command the report named failed: %v", err)
	}

	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("the file the firing wrote survived the revert the report offered (err %v)", err)
	}
}

// undoIDFromReport lifts the session id out of the `undo with: apogee undo <id>` line an unattended
// report ends with, failing the test when the line is absent or shaped differently.
func undoIDFromReport(t *testing.T, report string) string {
	t.Helper()

	const lead = "undo with: apogee undo "
	at := strings.Index(report, lead)
	if at < 0 {
		t.Fatalf("the report carries no undo offer: %q", report)
	}
	rest := report[at+len(lead):]
	if end := strings.IndexByte(rest, '\n'); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}
