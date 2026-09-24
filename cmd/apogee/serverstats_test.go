package main

// The per-server stats recorder end to end (ADR 0085): a real session and a real headless run over
// a scripted upstream, asserted on the file each leaves under the apogee home — one line per HTTP
// attempt, naming the entry and its redacted endpoint — and on the switch that stops it, at launch
// and live from the settings pane.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

const (
	// serverStatsPrompt is the prompt the journeys send; the script answers anything.
	serverStatsPrompt = "Say something short."
	// serverStatsReply is the script's one answer (testdata/stubllm/server-stats.yaml).
	serverStatsReply = "The stats journey got its answer."
	// serverStatsEntry is the servers: entry name every test home binds (upstreamHome).
	serverStatsEntry = "probe-target"
)

// statsLines reads the stats file of home as decoded JSON lines; a missing file is no lines.
func statsLines(t *testing.T, home string) []map[string]any {
	t.Helper()

	data, err := os.ReadFile(serverStatsPath(home))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read the stats file: %v", err)
	}
	var lines []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("stats line %q does not parse: %v", sc.Text(), err)
		}
		lines = append(lines, line)
	}
	return lines
}

// assertOneAttemptLine checks the file holds exactly the one successful attempt a one-request Turn
// makes, filed under the bound entry's name and its endpoint.
func assertOneAttemptLine(t *testing.T, lines []map[string]any, stub *stubllm.Server) {
	t.Helper()

	if len(lines) != 1 {
		t.Fatalf("the stats file holds %d lines; one Turn of one request is one attempt: %v", len(lines), lines)
	}
	line := lines[0]
	if line["server"] != serverStatsEntry {
		t.Errorf("server = %v; want the bound entry's name %q", line["server"], serverStatsEntry)
	}
	if line["endpoint"] != stub.URL {
		t.Errorf("endpoint = %v; want the entry's endpoint %q", line["endpoint"], stub.URL)
	}
	if line["model"] != stub.Model {
		t.Errorf("model = %v; want the served model %q", line["model"], stub.Model)
	}
	if line["outcome"] != "ok" {
		t.Errorf("outcome = %v; want ok", line["outcome"])
	}
}

// A driven session's Turn lands its one attempt in the home's stats file, through the recorder the
// boot wraps around the session's sink (wire_boot.go).
func TestServerStatsSessionTurnWritesOneLine(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "server-stats"))
	drv := tuitest.NewDriver(t, e2eSize)
	// auto-title off, so the naming call spends no request of its own beside the Turn's.
	sess := launchTUIConfigured(t, drv, stub, "auto-title: false\n")

	submit(drv, serverStatsPrompt)
	drv.WaitText(serverStatsReply)
	drv.WaitFor(func() bool { return len(statsLines(t, sess.Home())) > 0 },
		tuitest.Awaiting("the Turn's attempt to land in the stats file"))
	drv.WaitQuiet(settled)

	assertOneAttemptLine(t, statsLines(t, sess.Home()), stub)
}

// headlessStatsRun runs one headless prompt against stub in a fresh home carrying extraConfig, and
// returns the home.
func headlessStatsRun(t *testing.T, stub *stubllm.Server, extraConfig string) string {
	t.Helper()

	assertNoAmbientApogeeConfig(t)
	home := upstreamHome(t, stub.URL, stub.Model)
	appendHomeConfig(t, home, "auto-title: false\n"+extraConfig)

	cmd := newHeadlessCommandWith(headlessDeps{runner: run.Once})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--config", home, "--workspace", e2eWorkspace(t), serverStatsPrompt})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("headless run: %v\nstderr:\n%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), serverStatsReply) {
		t.Fatalf("headless run answered %q; want the script's reply", out.String())
	}
	return home
}

// A headless run is a Firing, and it records through the wrap firingConfig installs.
func TestServerStatsHeadlessFiringWritesOneLine(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "server-stats"))
	home := headlessStatsRun(t, stub, "")
	stub.AssertConsumed(t)

	assertOneAttemptLine(t, statsLines(t, home), stub)
}

// `server-stats: off` writes nothing: the file is never created.
func TestServerStatsOffWritesNothing(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "server-stats"))
	home := headlessStatsRun(t, stub, "server-stats: off\n")

	if _, err := os.Stat(serverStatsPath(home)); !os.IsNotExist(err) {
		t.Errorf("stat the stats file = %v; server-stats: off must neither write nor create it", err)
	}
}

// The settings-pane row opens and stops the store live: a flip off stops the next attempt from
// being recorded, and a flip back on records again — through a sink wrapped before either flip.
func TestServerStatsSettingsToggleOpensAndStopsTheStore(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	rec := newStatsRecorder(serverStatsPath(home), true)
	live := newLiveSettings(config.Options{ServerStats: true})
	apply := applySettingFor(settingsApplier{live: live, stats: rec})
	sink := rec.wrap(nil)
	attempt := func(id string) {
		sink.Emit(domain.UpstreamAttemptEvent{
			Server: "box", Endpoint: "http://box/v1", Model: "m", RequestID: id,
			TTFT: time.Second, Duration: 2 * time.Second, Outcome: "ok",
		})
	}

	attempt("first")
	if _, err := apply("server-stats", "false"); err != nil {
		t.Fatalf("apply server-stats=false: %v", err)
	}
	if live.options().ServerStats {
		t.Error("the holder still says on after the pane switched the key off")
	}
	attempt("while-off")
	if _, err := apply("server-stats", "true"); err != nil {
		t.Fatalf("apply server-stats=true: %v", err)
	}
	attempt("after")

	lines := statsLines(t, home)
	var ids []string
	for _, line := range lines {
		ids = append(ids, line["request_id"].(string))
	}
	if got := strings.Join(ids, ","); got != "first,after" {
		t.Errorf("recorded request ids = %q; want first,after — nothing while the switch was off", got)
	}
}

// A recorder that starts off never opens the file, so an off switch reads nothing either: a file
// oversized enough to be trimmed on open is left byte-identical.
func TestServerStatsOffRecorderNeitherReadsNorTrims(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := serverStatsPath(home)
	var body strings.Builder
	for i := 0; i < 250; i++ {
		body.WriteString(`{"server":"box","endpoint":"http://box","model":"m","outcome":"ok"}` + "\n")
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o600); err != nil {
		t.Fatalf("seed the stats file: %v", err)
	}

	rec := newStatsRecorder(path, false)
	rec.wrap(nil).Emit(domain.UpstreamAttemptEvent{Server: "box", Outcome: "ok"})

	got, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read the stats file: %v", err)
	}
	if string(got) != body.String() {
		t.Error("an off recorder changed the stats file; off must neither read (trim) nor write it")
	}
}
