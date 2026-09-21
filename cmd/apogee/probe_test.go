package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/stubllm"
)

// runProbe executes one probe invocation against a hermetic apogee home and workspace and
// returns everything it printed. Both roots are passed in — the report states them, so two
// invocations under comparison must be given the same ones. args are appended after them, so a
// test can point the command at a fake endpoint or reach the `host` child.
func runProbe(t *testing.T, cmd *cobra.Command, configHome, workspace string, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"--config", configHome, "--workspace", workspace}, args...))

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("probe: %v", err)
	}
	return out.String()
}

// probeUpstream scripts a stubllm upstream that advertises one model with a 4096-token window and
// scripts no /props — a bare OpenAI-compatible server — and no Turns, so a host probe that strays
// into the battery is refused and logged rather than answered.
func probeUpstream(t *testing.T, modelID string) *stubllm.Server {
	t.Helper()
	return stubllm.New(t, stubllm.Script{Discovery: stubllm.Discovery{
		Models: []stubllm.DiscoveredModel{{ID: modelID, ContextLength: 4096}},
	}})
}

// The command reports the host WITHOUT running an agent and against a live endpoint: it names
// the backend that answered on this machine, the roots it resolved, and the discovery outcome.
// The endpoint is a stubllm server, so the /v1/models + /props probes are the real ones.
func TestProbeCommandReportsTheHost(t *testing.T) {
	t.Parallel()
	srv := probeUpstream(t, "probe-model")

	configHome := upstreamHome(t, srv.URL)
	report := runProbe(t, newProbeCommand(), configHome, t.TempDir())

	for _, want := range []string{
		"apogee probe — host report",
		"confinement (ADR 0012)",
		"backend:",
		"1 advertised · active: probe-model",
		"context window 4096",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("probe report does not state %q:\n%s", want, report)
		}
	}

	// The host half is read-only: unlike the root's RunE it seeds no starter config, so a
	// diagnosis run leaves the apogee home exactly as it found it.
	assertHomeHoldsOnlyConfig(t, configHome, "the host report")
}

// `apogee probe host` is the named child form of the bare parent's report — the scriptable
// spelling ADR 0021 §1 promises — so the two must print the same thing.
func TestProbeHostChildMatchesTheParent(t *testing.T) {
	t.Parallel()
	// A configured but dead endpoint: the report reaches the same "could not be dialled" state
	// by both spellings, without either one depending on a live server.
	configHome := upstreamHome(t, "http://127.0.0.1:1")

	workspace := t.TempDir()
	parent := runProbe(t, newProbeCommand(), configHome, workspace)
	child := runProbe(t, newProbeCommand(), configHome, workspace, "host")

	if parent != child {
		t.Errorf("`probe` and `probe host` printed different reports:\n--- probe ---\n%s\n--- probe host ---\n%s", parent, child)
	}
	if !strings.Contains(parent, "http://127.0.0.1:1") {
		t.Errorf("the report does not name the configured endpoint:\n%s", parent)
	}
}

// The stream split as a SHELL sees it: `apogee probe > host.txt` must leave the report in the
// file, not in the terminal. Both the parent and its `host` child come out of probeHostCommand,
// so one invocation pins both.
//
// This is the guard on the mistake `apogee headless` made first: Cobra's cmd.Println writes to
// OutOrStderr, so printing the product with it sends the whole report to stderr everywhere
// except in a test that has called SetOut. Asserting on Cobra's buffers cannot catch that —
// hence the process's own stdout and stderr here, with no out writer wired.
func TestProbeHostReportLandsOnTheProcessStdout(t *testing.T) {
	srv := probeUpstream(t, "probe-model")

	configHome, workspace := upstreamHome(t, srv.URL), t.TempDir()
	var runErr error
	stdout, stderr := captureProcessStreams(t, func() {
		cmd := newProbeCommand()
		// Deliberately no SetOut: the fallback under test is the one every real run takes.
		cmd.SetArgs([]string{"--config", configHome, "--workspace", workspace})
		runErr = cmd.ExecuteContext(context.Background())
	})
	if runErr != nil {
		t.Fatalf("probe: %v (stderr: %q)", runErr, stderr)
	}

	if !strings.Contains(stdout, "apogee probe — host report") {
		t.Errorf("process stdout = %q; want the host report", stdout)
	}
	if strings.Contains(stderr, "apogee probe — host report") {
		t.Errorf("the report reached process stderr; a redirect of stdout would lose it: %q", stderr)
	}
}

// The host report names the endpoint's ACTIVE model, and the endpoint is exactly the thing the
// operator ran `apogee probe` to distrust: printed raw, the id it advertises would carry an OSC 8
// hyperlink (ADR 0019 rung 0) and a bidi override straight onto the terminal of the diagnostic
// judging it. The sink strips (internal/sanitize); the `active:` line still names the model.
func TestProbeCommandReportStripsTerminalEscapes(t *testing.T) {
	t.Parallel()
	// The stub JSON-encodes the id it advertises: a literal ESC or BEL pasted into a JSON string
	// would be a syntax error, and discovery would fail before the id could reach the report.
	srv := probeUpstream(t, "\x1b]8;;mailto:evil\aqwen-\u202e3")

	report := runProbe(t, newProbeCommand(), upstreamHome(t, srv.URL), t.TempDir())

	if !strings.Contains(report, "active: ") || !strings.Contains(report, "qwen-") {
		t.Errorf("the report no longer names the advertised model:\n%q", report)
	}
	assertNoTerminalControls(t, "probe host report", report)
}

// An interrupted Windows run leaves mandatory labels on the disk and a journal describing how
// to undo them, and ADR 0020 §2 makes the host report the surface that says so off-session. The
// report must therefore READ that state and leave it exactly where it found it: constructing
// the backend through the recovery path would revert the labels and delete the journal before
// the residue line could be composed, so the one line written for an interrupted run could
// never fire — and `probe`'s read-only pledge (ADR 0021 §1, the README, the command's own Long
// text) would be false besides.
//
// The journal home is deliberately independent of --config (a crashed run's record must be
// findable without one), so redirecting the user profile is the only way to plant one; the
// layout below mirrors winlabel.Home/winlabel.JournalPath.
func TestProbeReportsConfinementResidueWithoutHealingIt(t *testing.T) {
	// Not parallel: it redirects the process environment os.UserHomeDir reads.
	home := t.TempDir()
	t.Setenv("HOME", home)        // POSIX
	t.Setenv("USERPROFILE", home) // Windows

	labelled := filepath.Join(home, "crashed-workspace")
	journal := filepath.Join(home, ".apogee", "confinement", "labels-0.json")
	if err := os.MkdirAll(filepath.Dir(journal), 0o700); err != nil {
		t.Fatalf("create the journal directory: %v", err)
	}
	// PID 0 owns no process on any OS, so recovery would certainly treat this as an
	// interrupted run's journal and consume it — which is what makes the assertions below a
	// real distinction rather than an accident of whichever PID happened to be free.
	raw, err := json.Marshal(map[string]any{
		"pid":     0,
		"entries": []map[string]any{{"path": labelled, "root": true}},
	})
	if err != nil {
		t.Fatalf("encode the planted journal: %v", err)
	}
	if err := os.WriteFile(journal, raw, 0o600); err != nil {
		t.Fatalf("plant a journal: %v", err)
	}

	// Both spellings of the host report, because both build their probe.Inputs in the same
	// place: the second invocation seeing the same residue is itself proof the first did not
	// consume it.
	for _, args := range [][]string{nil, {"host"}} {
		report := runProbe(t, newProbeCommand(), upstreamHome(t, "http://127.0.0.1:1"), t.TempDir(), args...)
		if !strings.Contains(report, "labels:") || !strings.Contains(report, labelled) {
			t.Errorf("`apogee probe %s` does not report the outstanding label journal:\n%s", strings.Join(args, " "), report)
		}
	}

	got, err := os.ReadFile(journal)
	if err != nil {
		t.Fatalf("the host report consumed the label journal it exists to report: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("the journal changed under the host report:\n got %s\nwant %s", got, raw)
	}
}

// The reported settings are the ones a SESSION would run with on this host: the probe resolves
// config.yaml exactly as ApplyConfig does, including the effective confine-to-workspace after a
// Host acknowledgement — which is the fact the whole report exists to make diagnosable.
func TestProbeCommandReadsTheConfigFile(t *testing.T) {
	t.Parallel()
	configHome := t.TempDir()
	config := "confine-to-workspace: false\n" +
		"servers:\n  - name: probe-target\n    endpoint: http://127.0.0.1:1\nserver: probe-target\n"
	if err := os.WriteFile(filepath.Join(configHome, "config.yaml"), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	report := runProbe(t, newProbeCommand(), configHome, t.TempDir())

	if !strings.Contains(report, "http://127.0.0.1:1") {
		t.Errorf("probe did not report the endpoint from config.yaml:\n%s", report)
	}
	if !strings.Contains(report, "NO — auto runs every command with your full privileges") {
		t.Errorf("probe did not report the configured (unconfined) blast radius:\n%s", report)
	}
}

// The shipped registration seam carries probe: `apogee probe` is reachable through the real
// root, which is what makes the report available off-session at all.
func TestSubcommandsRegistersProbe(t *testing.T) {
	t.Parallel()
	root := newRootCommand((&recordingLauncher{}).launch, subcommands()...)

	var probe *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "probe" {
			probe = c
		}
	}
	if probe == nil {
		t.Fatal("the shipped subcommand set does not register `probe`")
	}
	children := map[string]bool{}
	for _, c := range probe.Commands() {
		children[c.Name()] = true
	}
	for _, want := range []string{"host", "model", "terminal", "config", "context"} {
		if !children[want] {
			t.Errorf("`probe` does not register the %q child; has %v", want, children)
		}
	}
}
