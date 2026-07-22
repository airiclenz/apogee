package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"

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

// TestRunRootAutoConstructs proves --mode auto now CONSTRUCTS and reaches the launcher,
// because runRoot injects the host's real Confiner (platform.NewConfiner(), always
// non-nil): under ADR 0012 Auto is no longer refused for a present-but-incapable Confiner
// — it is entered and the subprocess surface gates ("confine if you can, gate if you
// can't"). This is the reversal of the old Phase-2 refuse-Auto behaviour. confineToWorkspace
// defaults true here so no unconfined-warning prints.
func TestRunRootAutoConstructs(t *testing.T) {
	t.Parallel()
	rec := &recordingLauncher{}
	opts := options{
		endpoint:           "http://127.0.0.1:1111",
		model:              "fake",
		mode:               "auto",
		confineToWorkspace: true,
	}

	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot --mode auto: err = %v; want nil (Auto constructs and reaches the launcher)", err)
	}
	if !rec.called {
		t.Error("launcher should run once --mode auto constructs successfully")
	}
	if rec.opts.Mode != apogee.ModeAuto {
		t.Errorf("launcher Mode = %q; want %q", rec.opts.Mode, apogee.ModeAuto)
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

// fakeSubcommand is a stand-in for a real subcommand (probe, later headless): it records
// that it ran, so a test can tell dispatch from a root invocation.
func fakeSubcommand(ran *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "fake",
		Short: "stand-in subcommand used by the registration-seam tests",
		RunE: func(*cobra.Command, []string) error {
			*ran = true
			return nil
		},
	}
}

// TestRootCommandDispatchesSubcommand proves the registration seam works: a command handed
// to newRootCommand is reachable by name and runs INSTEAD of the TUI launch, not after it.
func TestRootCommandDispatchesSubcommand(t *testing.T) {
	t.Parallel()
	rec := &recordingLauncher{}
	var ran bool
	cmd := newRootCommand(rec.launch, fakeSubcommand(&ran))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"fake"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("Execute fake: %v\n%s", err, out.String())
	}
	if !ran {
		t.Error("the registered subcommand did not run")
	}
	if rec.called {
		t.Error("the TUI launcher ran for a subcommand invocation")
	}
}

// TestRootCommandBareInvocationSurvivesSubcommands is the guarantee the seam is judged on:
// registering children changes nothing about bare `apogee`. With a subcommand present the
// no-args run still launches the TUI, --help still carries every root flag, and an
// unrecognised word still fails as an unknown command (Args: cobra.NoArgs retained).
func TestRootCommandBareInvocationSurvivesSubcommands(t *testing.T) {
	t.Parallel()

	t.Run("no args still launches the TUI", func(t *testing.T) {
		t.Parallel()
		rec := &recordingLauncher{}
		var ran bool
		cmd := newRootCommand(rec.launch, fakeSubcommand(&ran))
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
			t.Error("bare invocation stopped launching the TUI once a subcommand was registered")
		}
		if ran {
			t.Error("the subcommand ran for a bare invocation")
		}
	})

	t.Run("help keeps the flags and gains the command", func(t *testing.T) {
		t.Parallel()
		var ran bool
		cmd := newRootCommand((&recordingLauncher{}).launch, fakeSubcommand(&ran))
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"--help"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("--help returned an error: %v", err)
		}
		help := out.String()
		for _, want := range []string{
			"--endpoint", "--model", "--mode", "--workspace", "--bypass", "--resume", "--config", "fake",
		} {
			if !strings.Contains(help, want) {
				t.Errorf("--help output missing %q\n%s", want, help)
			}
		}
	})

	t.Run("an unknown word is still an unknown command", func(t *testing.T) {
		t.Parallel()
		rec := &recordingLauncher{}
		var ran bool
		cmd := newRootCommand(rec.launch, fakeSubcommand(&ran))
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"bogus"})

		err := cmd.ExecuteContext(context.Background())
		if err == nil {
			t.Fatal("Execute bogus: want an error, got nil")
		}
		if !strings.Contains(err.Error(), `unknown command "bogus"`) {
			t.Errorf("Execute bogus: err = %v; want an unknown-command error", err)
		}
		if rec.called || ran {
			t.Error("an unknown word must run neither the TUI nor a subcommand")
		}
	})
}
