package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
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
	opts := config.Options{
		Endpoint:  "http://127.0.0.1:1111",
		Model:     "fake",
		Mode:      "ask-before",
		Workspace: t.TempDir(),
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
	if rec.opts.Workspace != opts.Workspace {
		t.Errorf("opts.Workspace = %q; want %q", rec.opts.Workspace, opts.Workspace)
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
	opts := config.Options{
		Endpoint:           "http://127.0.0.1:1111",
		Model:              "fake",
		Mode:               "auto",
		ConfineToWorkspace: true,
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
	err := runRoot(context.Background(), config.Options{Mode: "bogus"}, rec.launch)
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

// TestRootCommandVersionWired proves the CLI --version is wired: cmd.Version carries the
// single source of truth (the embedded VERSION file, via apogee.Version), so Cobra exposes
// --version. The value is never empty (a blank file degenerates to "dev"), so an accidental
// blank is caught.
func TestRootCommandVersionWired(t *testing.T) {
	t.Parallel()
	cmd := newRootCommand((&recordingLauncher{}).launch)
	if cmd.Version == "" {
		t.Error("newRootCommand().Version is empty; the CLI --version is not wired to apogee.Version")
	}
}

func TestRootCommandExecuteCleanQuit(t *testing.T) {
	t.Parallel()
	rec := &recordingLauncher{}
	cmd := newRootCommand(rec.launch)
	cmd.SetArgs([]string{
		"--workspace", t.TempDir(),
		// hermetic: a home of this test's own, so no real ~/.apogee/config.yaml is in the loop
		"--config", upstreamHome(t, "http://127.0.0.1:1111", "fake"),
	})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !rec.called {
		t.Fatal("launcher was not invoked through the command tree")
	}
}

// TestRootStartsWithNoModelAndNoServer is decision 8's headline, and it is the exact inverse of
// the behaviour it replaces: with no model configured by any layer and nothing listening at the
// endpoint, startup used to fail hard ("no model configured and discovery from … failed"), which
// meant the tool was unusable precisely when the server needed attention. It now reaches the
// launcher with no model bound at all — the first beat binds one, or the footer says the server is
// offline and the submit block explains itself — and both upstream seams are wired for it to do so.
func TestRootStartsWithNoModelAndNoServer(t *testing.T) {
	t.Parallel()
	rec := &recordingLauncher{}
	cmd := newRootCommand(rec.launch)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"--workspace", t.TempDir(),
		// nothing listens at that endpoint — and nothing asks; the entry names no model either
		"--config", upstreamHome(t, "http://127.0.0.1:1"),
	})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("Execute with no model and no server: %v\n%s", err, out.String())
	}
	if !rec.called {
		t.Fatal("the launcher was not invoked; startup still refuses to open without a reachable server")
	}
	if rec.opts.Model != "" {
		t.Errorf("tui.Options.Model = %q; want \"\" — nothing may be invented before the first beat", rec.opts.Model)
	}
	if rec.opts.Heartbeat == nil {
		t.Error("tui.Options.Heartbeat is nil; the upstream monitor was not wired")
	}
	if rec.opts.Rebind == nil {
		t.Error("tui.Options.Rebind is nil; the rebind closure was not wired")
	}
}

// The refusal the TUI answers instead of printing (ADR 0036 decision 3): a config that lists a
// server but records no choice cannot say where to start, and every other Driver stops there. The
// root command carries on — pre-bound, with no engine and the reason for the renderer to act on —
// because it is the one Driver that can ask. The proof is end to end, through the command tree,
// because the conversion from refusal to reason lives in RunE and nowhere else.
func TestRootStartsPreboundWhenNothingIsChosen(t *testing.T) {
	// The host's own environment must not choose a server the test is asserting nobody chose.
	t.Setenv(config.EnvServer, "")
	t.Setenv(config.EnvEndpoint, "")

	home := t.TempDir()
	const listWithoutAChoice = "servers:\n  - name: laptop\n    endpoint: http://127.0.0.1:1111\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(listWithoutAChoice), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	rec := &recordingLauncher{}
	cmd := newRootCommand(rec.launch)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--workspace", t.TempDir(), "--config", home})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("Execute with no startup server chosen: %v\n%s", err, out.String())
	}
	if !rec.called {
		t.Fatal("the launcher was not invoked; the TUI still refuses a config it could ask about")
	}
	want := tui.PreboundStart{Reason: tui.PreboundFirstBoot}
	if rec.opts.Prebound != want {
		t.Errorf("tui.Options.Prebound = %+v; want %+v", rec.opts.Prebound, want)
	}
	// The list it will ask WITH survived the refusal, and so did the seam that ends it.
	if choices := rec.opts.Servers(); len(choices) != 1 || choices[0].Name != "laptop" {
		t.Errorf("tui.Options.Servers() = %+v; want the configured list", choices)
	}
	if rec.opts.BindServer == nil {
		t.Error("tui.Options.BindServer is nil; the pre-bound session cannot bind the server it picks")
	}
}

// TestRootMakesNoStartupProbe is the other half of decision 8: not merely that startup survives an
// unreachable server, but that it never asks. A reachable server is the harder case — a surviving
// probe would succeed and hide itself — so this one counts requests against a live endpoint and
// requires the count to still be zero at the moment the launcher is handed the wiring. Everything
// upstream now happens inside the running TUI, on the heartbeat's cadence.
func TestRootMakesNoStartupProbe(t *testing.T) {
	t.Parallel()
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"loaded-model","context_length":32768}]}`))
	}))
	defer srv.Close()

	launched := false
	atLaunch := int64(-1)
	launch := func(context.Context, tui.Engine, *tui.Bridge, tui.Options) error {
		launched = true
		atLaunch = requests.Load()
		return nil
	}

	cmd := newRootCommand(launch)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"--workspace", t.TempDir(),
		"--config", upstreamHome(t, srv.URL),
	})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}
	if !launched {
		t.Fatal("the launcher was not invoked")
	}
	if atLaunch != 0 {
		t.Errorf("the server saw %d request(s) before the UI launched; want 0 — startup must not probe", atLaunch)
	}
}

// fakeSubcommand is a stand-in for a real subcommand (probe, headless): it records
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
			"--workspace", t.TempDir(),
			// hermetic: a home of this test's own, so no real ~/.apogee/config.yaml is in the loop
			"--config", upstreamHome(t, "http://127.0.0.1:1111", "fake"),
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

// TestRootCommandTUIDiagnosticFlagsAreHiddenAndDefaultOff pins the whole surface contract of the
// two rendering-diagnostic seams: they are real flags on the shipped binary — so a rendering bug
// is capturable from a stock build rather than from a patched renderer — but they stay out of
// --help, because the root's advertised flag set is deliberately minimal, and they default to
// empty, which is the off state the renderer checks for.
func TestRootCommandTUIDiagnosticFlagsAreHiddenAndDefaultOff(t *testing.T) {
	t.Parallel()
	cmd := newRootCommand((&recordingLauncher{}).launch)

	for _, name := range []string{"tui-trace", "tui-diag"} {
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("--%s is not registered", name)
		}
		if !flag.Hidden {
			t.Errorf("--%s is not hidden; it would appear in --help", name)
		}
		if flag.DefValue != "" {
			t.Errorf("--%s defaults to %q, want empty (the off state)", name, flag.DefValue)
		}
	}

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("--help returned an error: %v", err)
	}
	for _, name := range []string{"--tui-trace", "--tui-diag"} {
		if strings.Contains(out.String(), name) {
			t.Errorf("--help lists %s; the diagnostic flags are meant to be hidden\n%s", name, out.String())
		}
	}
}

// TestRunRootWiresTheTUIDiagnosticFlags proves the two paths reach the renderer, which is the
// only thing the binary owes them — what a named path MEANS is internal/tui's (diagnostics.go).
func TestRunRootWiresTheTUIDiagnosticFlags(t *testing.T) {
	t.Parallel()
	rec := &recordingLauncher{}
	opts := config.Options{
		Endpoint:  "http://127.0.0.1:1111",
		Model:     "fake",
		Mode:      "ask-before",
		Workspace: t.TempDir(),
		TUITrace:  filepath.Join(t.TempDir(), "trace.txt"),
		TUIDiag:   filepath.Join(t.TempDir(), "diag.txt"),
	}

	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if rec.opts.TracePath != opts.TUITrace {
		t.Errorf("opts.TracePath = %q; want %q", rec.opts.TracePath, opts.TUITrace)
	}
	if rec.opts.DiagPath != opts.TUIDiag {
		t.Errorf("opts.DiagPath = %q; want %q", rec.opts.DiagPath, opts.TUIDiag)
	}
}

// TestRunRootLeavesTheTUIDiagnosticFlagsOffByDefault is the other half: an ordinary run must hand
// the renderer two empty paths, so no trace file is ever opened and no wrapper is ever installed
// on a session nobody asked to debug.
func TestRunRootLeavesTheTUIDiagnosticFlagsOffByDefault(t *testing.T) {
	t.Parallel()
	rec := &recordingLauncher{}
	opts := config.Options{
		Endpoint:  "http://127.0.0.1:1111",
		Model:     "fake",
		Mode:      "ask-before",
		Workspace: t.TempDir(),
	}

	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if rec.opts.TracePath != "" || rec.opts.DiagPath != "" {
		t.Errorf("opts.TracePath = %q and opts.DiagPath = %q; want both empty",
			rec.opts.TracePath, rec.opts.DiagPath)
	}
}

// The Driver half of internal/config's startup-override guard: resolution asks this command's flag
// set whether each override flag was set, so a name the table advertises and the root command never
// registers is a question cobra answers with "no such flag" forever — the override would silently
// stop reading the command line. Walking the exported list keeps the two sides from drifting
// without either restating the other's names.
func TestRootCommandRegistersTheStartupOverrideFlags(t *testing.T) {
	t.Parallel()
	flags := newRootCommand((&recordingLauncher{}).launch).Flags()
	for _, name := range config.StartupOverrideFlags() {
		if flags.Lookup(name) == nil {
			t.Errorf("startup-override resolution reads --%s, which the root command does not register", name)
		}
	}
}
