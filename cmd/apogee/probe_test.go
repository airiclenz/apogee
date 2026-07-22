package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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

// The command reports the host WITHOUT running an agent and against a live endpoint: it names
// the backend that answered on this machine, the roots it resolved, and the discovery outcome.
// The endpoint is an httptest server, so the /v1/models + /props probes are the real ones.
func TestProbeCommandReportsTheHost(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"probe-model","context_length":4096}]}`)
		case "/props":
			w.WriteHeader(http.StatusNotFound) // a bare OpenAI-compatible server
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	configHome := t.TempDir()
	report := runProbe(t, newProbeCommand(), configHome, t.TempDir(), "--endpoint", srv.URL)

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
	if entries, err := os.ReadDir(configHome); err != nil || len(entries) != 0 {
		t.Errorf("probe wrote into the apogee home (entries=%v, err=%v); the host half writes nothing", entries, err)
	}
}

// `apogee probe host` is the named child form of the bare parent's report — the scriptable
// spelling ADR 0021 §1 promises — so the two must print the same thing.
func TestProbeHostChildMatchesTheParent(t *testing.T) {
	t.Parallel()
	configHome := t.TempDir()

	workspace := t.TempDir()
	parent := runProbe(t, newProbeCommand(), configHome, workspace)
	child := runProbe(t, newProbeCommand(), configHome, workspace, "host")

	if parent != child {
		t.Errorf("`probe` and `probe host` printed different reports:\n--- probe ---\n%s\n--- probe host ---\n%s", parent, child)
	}
	if !strings.Contains(parent, "no endpoint is configured") {
		t.Errorf("with no endpoint set anywhere, the report should say so:\n%s", parent)
	}
}

// The reported settings are the ones a SESSION would run with on this host: the probe resolves
// config.yaml exactly as applyConfig does, including the effective confine-to-workspace after a
// Host acknowledgement — which is the fact the whole report exists to make diagnosable.
func TestProbeCommandReadsTheConfigFile(t *testing.T) {
	t.Parallel()
	configHome := t.TempDir()
	config := "endpoint: http://127.0.0.1:1\nconfine-to-workspace: false\n"
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

	found := false
	for _, c := range root.Commands() {
		if c.Name() == "probe" {
			found = true
		}
	}
	if !found {
		t.Fatal("the shipped subcommand set does not register `probe`")
	}
}
