package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/daemon"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/schedule"
)

// daemonFireHarness is one daemon's wiring with every seam onto the host replaced: the runner, so
// nothing is sent; the slot probe, so no Firing dials a server to ask how wide it may fan out; and
// the confinement backend, so the composition is the same on a kernel that can fence and one that
// cannot. Every test in this file goes through it, which is why none of them calls t.Parallel —
// they replace package-level vars, exactly as the headless and schedule composition tests do.
type daemonFireHarness struct {
	wiring *daemonWiring
	runner *stubRunner
	// probed records the endpoint the slot probe was asked about, empty when a pin skipped it.
	probed string
}

// newDaemonFireHarness builds the harness for one host configuration. The apogee home is temporary
// so no real ~/.apogee can reach the resolution — a caller that needs to prepare that home before
// the wiring is built (planting a stale scratch dir, say) sets ConfigDir itself and keeps it.
func newDaemonFireHarness(t *testing.T, opts config.Options) *daemonFireHarness {
	t.Helper()

	if opts.ConfigDir == "" {
		opts.ConfigDir = t.TempDir()
	}
	harness := &daemonFireHarness{runner: &stubRunner{}}

	prevRunner, prevSlots, prevConfiner := runOnce, discoverSlots, newConfiner
	runOnce = harness.runner.once
	discoverSlots = func(_ context.Context, endpoint, _, _ string) int {
		harness.probed = endpoint
		return 0
	}
	newConfiner = func() apogee.Confiner { return fenceableHost }
	t.Cleanup(func() { runOnce, discoverSlots, newConfiner = prevRunner, prevSlots, prevConfiner })

	wiring, err := newDaemonWiring(opts)
	if err != nil {
		t.Fatalf("newDaemonWiring: %v", err)
	}
	harness.wiring = wiring
	return harness
}

// fire adopts the entry and fires it, returning the run.Spec the stubbed runner was handed.
func (h *daemonFireHarness) fire(t *testing.T, entry daemon.Entry) run.Spec {
	t.Helper()

	h.wiring.adopt([]daemon.Entry{entry})
	firing := schedule.Firing{
		ScheduleID:   "sched-1",
		ScheduleName: entry.Name,
		Prompt:       entry.Run.Prompt,
		Mode:         entry.Run.Mode,
	}
	if _, err := h.wiring.fire(context.Background(), firing); err != nil {
		t.Fatalf("fire: %v", err)
	}
	if !h.runner.called {
		t.Fatal("the firing composed no run at all")
	}
	return h.runner.spec
}

// entryFor is one validated schedule entry as internal/daemon's Load would hand it over: an
// existing workspace, a prompt, and the plan mode the schema defaults to.
func entryFor(t *testing.T, name string, run daemon.Action) daemon.Entry {
	t.Helper()

	if run.Workspace == "" {
		run.Workspace = t.TempDir()
	}
	if run.Prompt == "" {
		run.Prompt = "audit the tree"
	}
	if run.Mode == "" {
		run.Mode = domain.ModePlan
	}
	return daemon.Entry{Name: name, On: daemon.Trigger{Cycle: schedule.MinCycle}, Run: run}
}

// A schedule that NAMES a server is bound to that entry — its endpoint, its key source, its model —
// and not to the one the host happens to start sessions on (ADR 0055 decision 1). The two entries
// differ in every one of those three fields, so a composition that reached for the startup default
// fails on all three at once.
func TestDaemonFireBindsTheServerTheEntryNames(t *testing.T) {
	harness := newDaemonFireHarness(t, config.Options{
		// The startup selection, flattened onto the options exactly as ApplyConfig leaves it.
		HostAlias: "startup",
		Endpoint:  "http://startup.invalid",
		APIKey:    "startup-key",
		Model:     "startup-model",
		Servers: []config.ServerEntry{
			{Name: "startup", Endpoint: "http://startup.invalid", APIKey: "startup-key", Model: "startup-model"},
			{Name: "nightly", Endpoint: "http://nightly.invalid", APIKey: "nightly-key", Model: "nightly-model"},
		},
	})

	spec := harness.fire(t, entryFor(t, "audit", daemon.Action{Server: "nightly"}))

	if got := spec.Config.Endpoint; got != "http://nightly.invalid" {
		t.Errorf("the firing dials %q, want the named entry's http://nightly.invalid", got)
	}
	if got := spec.Config.APIKey; got != "nightly-key" {
		t.Errorf("the firing sends %q, want the named entry's key nightly-key", got)
	}
	if got := spec.Config.Model; got != "nightly-model" {
		t.Errorf("the firing runs %q, want the named entry's model nightly-model", got)
	}
}

// A schedule that names NO server binds to the same startup default a fresh session or a headless
// run on this host gets (ADR 0055 decision 1) — the flattened selection, reassembled by
// startupEntry, so one configuration means one default whichever Driver reads it.
func TestDaemonFireFallsBackToTheStartupServer(t *testing.T) {
	harness := newDaemonFireHarness(t, config.Options{
		HostAlias: "startup",
		Endpoint:  "http://startup.invalid",
		APIKey:    "startup-key",
		Model:     "startup-model",
		Servers: []config.ServerEntry{
			{Name: "startup", Endpoint: "http://startup.invalid", APIKey: "startup-key", Model: "startup-model"},
			{Name: "other", Endpoint: "http://other.invalid", APIKey: "other-key", Model: "other-model"},
		},
	})

	spec := harness.fire(t, entryFor(t, "audit", daemon.Action{}))

	if got := spec.Config.Endpoint; got != "http://startup.invalid" {
		t.Errorf("the firing dials %q, want the startup default http://startup.invalid", got)
	}
	if got := spec.Config.Model; got != "startup-model" {
		t.Errorf("the firing runs %q, want the startup default's model startup-model", got)
	}
}

// Every daemon Firing runs in a scratch dir of its OWN, named after the record it will be saved
// under (residuals sweep item 6, 2026-08-24). A daemon has no session host to mint one for it, so
// before this its model was offered no writable scratch inside the box at all and put its working
// files wherever else it could reach; two entries firing on the same minute must land in separate
// dirs for the same reason two sessions do.
func TestDaemonFireGivesEachFiringItsOwnScratchDir(t *testing.T) {
	harness := newDaemonFireHarness(t, config.Options{
		HostAlias: "startup",
		Endpoint:  "http://startup.invalid",
		Model:     "startup-model",
		Servers:   []config.ServerEntry{{Name: "startup", Endpoint: "http://startup.invalid", Model: "startup-model"}},
	})
	roots, err := resolveRoots(harness.wiring.opts.ConfigDir, "")
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}

	first := harness.fire(t, entryFor(t, "audit", daemon.Action{}))
	assertFiringScratchDir(t, first.RecordID, first.Config.ScratchDir, roots.scratch)

	second := harness.fire(t, entryFor(t, "audit", daemon.Action{}))
	assertFiringScratchDir(t, second.RecordID, second.Config.ScratchDir, roots.scratch)
	if second.Config.ScratchDir == first.Config.ScratchDir {
		t.Errorf("two firings of the same schedule shared the scratch dir %q; each one owns its own",
			first.Config.ScratchDir)
	}
}

// A `model:` on the entry is a per-request selection, and it overlays the bound entry's own choice
// (ADR 0055 decision 2). Validation has already refused the key where that is not what it would
// mean, so the composition applies it without a second opinion.
func TestDaemonFireOverlaysTheEntrysModel(t *testing.T) {
	harness := newDaemonFireHarness(t, config.Options{
		HostAlias: "startup",
		Endpoint:  "http://startup.invalid",
		Model:     "startup-model",
		Servers:   []config.ServerEntry{{Name: "startup", Endpoint: "http://startup.invalid", Model: "startup-model"}},
	})

	spec := harness.fire(t, entryFor(t, "audit", daemon.Action{Model: "qwen/qwen3-72b"}))

	if got := spec.Config.Model; got != "qwen/qwen3-72b" {
		t.Errorf("the firing runs %q, want the entry's overlay qwen/qwen3-72b", got)
	}
}

// A launcher-fronted server is dialled exactly as it stands: the daemon never actuates the launcher
// (ADR 0055 decision 3), so a Firing sends to whatever is serving and nothing on this path loads,
// unloads or restores a profile. Nothing is left to stub, because no actuation call site exists —
// which is the assertion the sibling test below makes structurally.
func TestDaemonFireSendsToALauncherFrontedServerAsItStands(t *testing.T) {
	harness := newDaemonFireHarness(t, config.Options{
		HostAlias: "local",
		Endpoint:  "http://local.invalid",
		Servers: []config.ServerEntry{{
			Name:          "local",
			Endpoint:      "http://local.invalid",
			LlamaLauncher: "~/.llama-launcher/config.yaml",
			LaunchProfile: "gpt-oss-20b",
		}},
	})

	spec := harness.fire(t, entryFor(t, "audit", daemon.Action{Server: "local"}))

	if got := spec.Config.Endpoint; got != "http://local.invalid" {
		t.Errorf("the firing dials %q, want the launcher-fronted entry's endpoint as it stands", got)
	}
	// Whatever that server is serving — the entry names no `model:` and validation refuses one here,
	// so the Firing states none and the server answers with its loaded slot.
	if got := spec.Config.Model; got != "" {
		t.Errorf("the firing names model %q, want none — a model name here would be a request to load one", got)
	}
}

// ...and the same decision read off the source rather than off a run: the Firing composition names
// no launcher identifier at all. A behavioural test cannot prove an actuation that never happens,
// so the invariant is pinned where it can be — an actuation would have to reach a Launcher-shaped
// symbol, and there is none in this file.
func TestDaemonFireCompositionNamesNoLauncher(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "daemonfire.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse daemonfire.go: %v", err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		ident, isIdent := node.(*ast.Ident)
		if isIdent && strings.Contains(strings.ToLower(ident.Name), "launcher") {
			t.Errorf("daemonfire.go names %q — the daemon never actuates the launcher (ADR 0055 "+
				"decision 3), so a Firing must reach no launcher symbol at all", ident.Name)
		}
		return true
	})
}

// The mode a Firing runs in is the Schedule's, taken from the Firing the library raised rather than
// re-read off the entry or inherited from anything else (ADR 0033, decision 3). Both rungs a
// schedule may use reach the runner as themselves; the eligibility of the Auto rung was ruled on at
// validation, where the refusal could name the entry.
func TestDaemonFireRunsTheModeTheScheduleFired(t *testing.T) {
	for _, tt := range []struct {
		name string
		mode domain.Mode
	}{
		{name: "plan", mode: domain.ModePlan},
		{name: "auto", mode: domain.ModeAuto},
	} {
		t.Run(tt.name, func(t *testing.T) {
			harness := newDaemonFireHarness(t, config.Options{
				HostAlias: "startup",
				Endpoint:  "http://startup.invalid",
				Servers:   []config.ServerEntry{{Name: "startup", Endpoint: "http://startup.invalid"}},
			})

			spec := harness.fire(t, entryFor(t, "audit", daemon.Action{Mode: tt.mode}))

			if got := spec.Config.Mode; got != tt.mode {
				t.Errorf("the firing runs in mode %q, want the Schedule's %q", got, tt.mode)
			}
			if spec.Config.Confiner == nil {
				t.Error("the firing composed no Confiner — an Auto Firing must be fenced by the same " +
					"box an Auto session would be")
			}
		})
	}
}

// What the daemon's log can say about a Firing comes back on the Outcome and nowhere else: the
// library is runner-agnostic (ADR 0033), so the answer, the record id and the counts have to be
// mapped out of the run.Result here or they are lost.
func TestDaemonFireReportsWhatTheRunDid(t *testing.T) {
	harness := newDaemonFireHarness(t, config.Options{
		HostAlias: "startup",
		Endpoint:  "http://startup.invalid",
		Servers:   []config.ServerEntry{{Name: "startup", Endpoint: "http://startup.invalid"}},
	})
	harness.runner.res = run.Result{
		SessionID: "rec-42",
		Title:     "nightly audit — 03:00",
		FinalText: "three findings",
		Turns:     7,
		Denied:    2,
	}

	entry := entryFor(t, "audit", daemon.Action{})
	harness.wiring.adopt([]daemon.Entry{entry})
	out, err := harness.wiring.fire(context.Background(), schedule.Firing{
		ScheduleID:   "sched-1",
		ScheduleName: entry.Name,
		Prompt:       entry.Run.Prompt,
		Mode:         entry.Run.Mode,
	})
	if err != nil {
		t.Fatalf("fire: %v", err)
	}

	want := schedule.Outcome{
		RecordID:  "rec-42",
		Title:     "nightly audit — 03:00",
		FinalText: "three findings",
		Turns:     7,
		Denied:    2,
	}
	if out != want {
		t.Errorf("the firing reports %+v, want the run's own %+v", out, want)
	}
}

// The Schedule identity is stamped onto the record so a schedule's runs read chronologically under
// its name in /sessions (ADR 0034), and the entry's own workspace — not the daemon's working
// directory — is where the run happens.
func TestDaemonFireRunsInTheEntrysWorkspace(t *testing.T) {
	workspace := t.TempDir()
	harness := newDaemonFireHarness(t, config.Options{
		HostAlias: "startup",
		Endpoint:  "http://startup.invalid",
		Servers:   []config.ServerEntry{{Name: "startup", Endpoint: "http://startup.invalid"}},
	})

	spec := harness.fire(t, entryFor(t, "audit", daemon.Action{Workspace: workspace}))

	if got := spec.Config.WorkspaceDir; got != workspace {
		t.Errorf("the firing runs in %q, want the entry's workspace %q", got, workspace)
	}
	if spec.ScheduleName != "audit" || spec.ScheduleID != "sched-1" {
		t.Errorf("the run is stamped %q/%q, want the Schedule identity audit/sched-1",
			spec.ScheduleID, spec.ScheduleName)
	}
	if spec.Store == nil {
		t.Error("the firing saves nothing — a Firing's deliverable is its record in the shared store")
	}
}

// A tick whose entry has since left the adopted set is reported rather than fired: the daemon's
// Firing composition resolves the file's half of `run:` by name, and a name it cannot resolve has
// no workspace, no server and no instruction to run.
func TestDaemonFireRefusesAnUnadoptedSchedule(t *testing.T) {
	harness := newDaemonFireHarness(t, config.Options{
		HostAlias: "startup",
		Endpoint:  "http://startup.invalid",
		Servers:   []config.ServerEntry{{Name: "startup", Endpoint: "http://startup.invalid"}},
	})

	_, err := harness.wiring.fire(context.Background(), schedule.Firing{
		ScheduleName: "gone",
		Prompt:       "audit the tree",
		Mode:         domain.ModePlan,
	})

	if err == nil {
		t.Fatal("fire returned nil for a schedule no entry is adopted under; want the refusal")
	}
	if harness.runner.called {
		t.Error("the firing reached the runner with no entry behind it")
	}
}

// The daemon sweeps the stale scratch dirs its own Firings left behind, once at startup — the
// daemon twin of TestHeadlessRunGetsItsOwnScratchDirAndSweepsStaleOnes (headless_test.go). A host
// driven only by a daemon never passes the TUI's boot sweep (wire.go), so a Firing's dir would
// otherwise accumulate one per run forever. The claim is about the START, so the stale dir is
// planted before the wiring is built and asserted gone the moment it is — no Firing involved.
func TestDaemonStartupSweepsStaleScratchDirs(t *testing.T) {
	configDir := t.TempDir()
	roots, err := resolveRoots(configDir, "")
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	stale := filepath.Join(roots.scratch, "2026-01-01T00-00-00-stale")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	aged := time.Now().Add(-scratchMaxAge - time.Hour)
	if err := os.Chtimes(stale, aged, aged); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	newDaemonFireHarness(t, config.Options{ConfigDir: configDir})

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("a stale scratch dir survived the daemon's startup (stat err = %v); a host driven "+
			"only by a daemon never passes the TUI's boot sweep", err)
	}
}
