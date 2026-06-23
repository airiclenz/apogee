package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/tui"
)

// recordingLauncher is a fake launcher: it captures what the binary handed the UI and
// returns immediately (a clean quit), so construction is provable without a terminal.
type recordingLauncher struct {
	called bool
	engine tui.Engine
	bridge *tui.Bridge
	opts   tui.Options
}

func (r *recordingLauncher) launch(_ context.Context, eng tui.Engine, br *tui.Bridge, opts tui.Options) error {
	r.called = true
	r.engine = eng
	r.bridge = br
	r.opts = opts
	return nil
}

func TestRunRootConstructsAndLaunches(t *testing.T) {
	t.Parallel()
	rec := &recordingLauncher{}
	opts := options{
		endpoint:  "http://127.0.0.1:1111",
		model:     "fake",
		mode:      "ask-before",
		workspace: t.TempDir(),
	}

	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if !rec.called {
		t.Fatal("launcher was not invoked")
	}
	if rec.engine == nil {
		t.Fatal("launcher received a nil engine")
	}
	if rec.bridge == nil {
		t.Fatal("launcher received a nil bridge (the sink/approver were not late-bound)")
	}
	if rec.opts.Model != "fake" {
		t.Errorf("opts.Model = %q; want %q", rec.opts.Model, "fake")
	}
	if rec.opts.Mode != apogee.ModeAskBefore {
		t.Errorf("opts.Mode = %q; want %q", rec.opts.Mode, apogee.ModeAskBefore)
	}
	if rec.opts.Workspace != opts.workspace {
		t.Errorf("opts.Workspace = %q; want %q", rec.opts.Workspace, opts.workspace)
	}
}

func TestRunRootAutoRefused(t *testing.T) {
	t.Parallel()
	rec := &recordingLauncher{}
	opts := options{
		endpoint: "http://127.0.0.1:1111",
		model:    "fake",
		mode:     "auto",
	}

	err := runRoot(context.Background(), opts, rec.launch)
	if !errors.Is(err, errAutoPhase3) {
		t.Fatalf("runRoot --mode auto: err = %v; want errAutoPhase3", err)
	}
	if rec.called {
		t.Error("launcher should not run when construction is refused")
	}
}

func TestRunRootInvalidMode(t *testing.T) {
	t.Parallel()
	rec := &recordingLauncher{}
	err := runRoot(context.Background(), options{mode: "bogus"}, rec.launch)
	if err == nil {
		t.Fatal("runRoot --mode bogus: want error, got nil")
	}
	if rec.called {
		t.Error("launcher should not run for an invalid mode")
	}
}

func TestRootCommandHelp(t *testing.T) {
	t.Parallel()
	cmd := newRootCommand((&recordingLauncher{}).launch)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("--help returned an error: %v", err)
	}

	help := out.String()
	for _, flag := range []string{"--endpoint", "--model", "--mode", "--workspace", "--bypass", "--resume", "--config"} {
		if !strings.Contains(help, flag) {
			t.Errorf("--help output missing %q\n%s", flag, help)
		}
	}
}

func TestRootCommandExecuteCleanQuit(t *testing.T) {
	t.Parallel()
	rec := &recordingLauncher{}
	cmd := newRootCommand(rec.launch)
	cmd.SetArgs([]string{
		"--endpoint", "http://127.0.0.1:1111",
		"--model", "fake",
		"--workspace", t.TempDir(),
		"--config", t.TempDir(), // hermetic: no real ~/.apogee/config.yaml in the loop
	})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !rec.called {
		t.Fatal("launcher was not invoked through the command tree")
	}
}
