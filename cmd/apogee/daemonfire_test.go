package main

import (
	"bytes"
	"context"
	"errors"
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
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/notice"
	"github.com/airiclenz/apogee/internal/provider"
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
	// probed records the endpoint the Firing's one beat was taken against.
	probed string
	// beat is what that beat observes. It ANSWERS by default, because the daemon refuses a Firing
	// whose server answered nothing at all (daemonfire.go): a fixture that observed nothing would
	// refuse every test in this file rather than compose the run each one is about. A test about the
	// refusal dictates its own before firing.
	beat heartbeat.Beat
	// logged is the daemon log the wiring narrates through — the one stream this Driver speaks on
	// (daemon.go), captured so a test can read what a Firing said as it composed and as it landed.
	logged *bytes.Buffer
}

// newDaemonFireHarness builds the harness for one host configuration. The apogee home is temporary
// so no real ~/.apogee can reach the resolution — a caller that needs to prepare that home before
// the wiring is built (planting a stale scratch dir, say) sets ConfigDir itself and keeps it.
func newDaemonFireHarness(t *testing.T, opts config.Options) *daemonFireHarness {
	t.Helper()

	if opts.ConfigDir == "" {
		opts.ConfigDir = t.TempDir()
	}
	harness := &daemonFireHarness{
		runner: &stubRunner{},
		beat:   heartbeat.Beat{Reachable: true, Answered: true},
		logged: &bytes.Buffer{},
	}

	prevRunner, prevBeat, prevConfiner := runOnce, discoverBeat, newConfiner
	runOnce = harness.runner.once
	discoverBeat = func(_ context.Context, endpoint, _, _ string) heartbeat.Beat {
		harness.probed = endpoint
		return harness.beat
	}
	newConfiner = func() apogee.Confiner { return fenceableHost }
	t.Cleanup(func() { runOnce, discoverBeat, newConfiner = prevRunner, prevBeat, prevConfiner })

	wiring, _, err := newDaemonWiring(opts, &daemonLog{out: harness.logged, now: time.Now})
	if err != nil {
		t.Fatalf("newDaemonWiring: %v", err)
	}
	harness.wiring = wiring
	return harness
}

// fire adopts the entry and fires it, returning the run.Spec the stubbed runner was handed.
func (h *daemonFireHarness) fire(t *testing.T, entry daemon.Entry) run.Spec {
	t.Helper()

	if _, err := h.raise(entry); err != nil {
		t.Fatalf("fire: %v", err)
	}
	if !h.runner.called {
		t.Fatal("the firing composed no run at all")
	}
	return h.runner.spec
}

// raise is fire without the expectation that a run came of it: it adopts the entry, fires it, and
// hands back exactly what the wiring answered. A Firing the daemon REFUSES answers with an error and
// composes nothing, which is a verdict about the wiring rather than a failure of the test.
func (h *daemonFireHarness) raise(entry daemon.Entry) (schedule.Outcome, error) {
	h.wiring.adopt([]daemon.Entry{entry})
	return h.wiring.fire(context.Background(), schedule.Firing{
		ScheduleID:   "sched-1",
		ScheduleName: entry.Name,
		Prompt:       entry.Run.Prompt,
		Mode:         entry.Run.Mode,
	})
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
		Usage:     run.Usage{TotalTokens: 30000},
		SubAgents: []run.SubAgentUsage{
			{Task: "read the tree", TotalTokens: 8000},
			{Task: "read the tests", TotalTokens: 3984},
		},
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
		// The whole Firing's spend, the run's own plus both delegations' — the sum /sessions
		// shows — and the delegations as a COUNT, which is all the report line says of them.
		TotalTokens: 41984,
		SubAgents:   2,
	}
	if out != want {
		t.Errorf("the firing reports %+v, want the run's own %+v", out, want)
	}
}

// A faulted final Turn crosses onto the Outcome as DATA beside the answer. The Firing RETURNED —
// the Exchange reached its boundary — so fire raises no error and the library will report it
// completed; these two fields are then the only thing that tells the daemon's log that the text it
// is carrying is the run's LAST WORDS rather than its answer to the prompt.
func TestDaemonFireReportsAnAbandonedFinalTurn(t *testing.T) {
	harness := newDaemonFireHarness(t, config.Options{
		HostAlias: "startup",
		Endpoint:  "http://startup.invalid",
		Servers:   []config.ServerEntry{{Name: "startup", Endpoint: "http://startup.invalid"}},
	})
	harness.runner.res = run.Result{
		SessionID: "rec-42",
		FinalText: "half a thought",
		Turns:     3,
		Faulted:   true,
		Fault:     "upstream returned an empty reply",
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
		t.Fatalf("fire: %v — a faulted run reached its boundary, so the Firing itself did not fail", err)
	}
	if !out.Faulted || out.Fault != "upstream returned an empty reply" {
		t.Errorf("the firing reports Faulted=%v, Fault=%q; want the run's own true, %q",
			out.Faulted, out.Fault, "upstream returned an empty reply")
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

// A Firing whose bound server answered NOTHING is refused before a prompt is sent — and only that
// one: a server that answered anything at all keeps today's proceed-and-degrade, because a 401, a
// 500 and a 429 are answers this Driver has no standing to judge while nobody is watching. The
// sentence is the TUI's own (internal/tui/heartbeat.go's upstreamBlockNote), so the two Drivers word
// one refusal one way.
func TestDaemonFireRefusesOnlyAServerThatAnsweredNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		beat heartbeat.Beat
		// want is the refusal sentence, and "" when this beat must let the Firing run.
		want string
	}{
		{
			name: "a refused dial says why",
			beat: heartbeat.Beat{Failure: "dial tcp 127.0.0.1:9: connect: connection refused"},
			want: "cannot send — server offline (http://nightly.invalid): " +
				"dial tcp 127.0.0.1:9: connect: connection refused",
		},
		{
			// The zero Beat: nothing observed and nothing to say about it. The sentence still names
			// the endpoint, which is the one fact a human reading the log acts on.
			name: "nothing observed names the endpoint alone",
			beat: heartbeat.Beat{},
			want: "cannot send — server offline (http://nightly.invalid)",
		},
		{
			// A throttled model list ANSWERED: the box is there and merely would not answer this
			// question now (internal/heartbeat). Refusing over it would turn a rate limit into a
			// silent gap in the schedule's record.
			name: "a throttled model list runs",
			beat: heartbeat.Beat{Answered: true, Throttled: true, Failure: "the model list answered HTTP 429"},
		},
		{
			// A completions-only endpoint serves no model list at all and answers the completion
			// anyway — the beat is unreachable, not absent.
			name: "a server with no model list runs",
			beat: heartbeat.Beat{Answered: true, Failure: "the model list answered HTTP 404"},
		},
		{
			name: "a healthy server runs",
			beat: heartbeat.Beat{Answered: true, Reachable: true, ActiveModel: "nightly-model"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			harness := newDaemonFireHarness(t, config.Options{
				HostAlias: "startup",
				Endpoint:  "http://startup.invalid",
				Servers: []config.ServerEntry{
					{Name: "startup", Endpoint: "http://startup.invalid"},
					{Name: "nightly", Endpoint: "http://nightly.invalid", Model: "nightly-model"},
				},
			})
			harness.beat = tc.beat

			out, err := harness.raise(entryFor(t, "audit", daemon.Action{Server: "nightly"}))

			if tc.want == "" {
				if err != nil {
					t.Fatalf("fire: %v; a server that ANSWERED must run the firing as before", err)
				}
				if !harness.runner.called {
					t.Error("the firing composed no run at all")
				}
				return
			}
			if err == nil {
				t.Fatal("fire returned nil for a server that answered nothing; want the refusal")
			}
			if got := err.Error(); got != tc.want {
				t.Errorf("the refusal reads %q; want the TUI's own sentence %q", got, tc.want)
			}
			// Nothing was sent, so nothing was spent and no record was written — the whole point of
			// gating before the run rather than reporting after it.
			if harness.runner.called {
				t.Error("the refused firing still reached the runner")
			}
			// No Outcome at all, and so never Faulted: internal/schedule reserves Faulted for a run
			// that RETURNED with its Exchange at a boundary, and a run with no Turn has none. The
			// error alone is what the library renders, through its EventFailed line.
			if out != (schedule.Outcome{}) {
				t.Errorf("the refused firing recorded %+v; want no Outcome at all", out)
			}
		})
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

// ----------------------------------------------------------------------------
// What a Firing narrates
// ----------------------------------------------------------------------------

// The composition's own notices reach the daemon log. A Firing that binds a model its server never
// advertised is the case that matters: the run proceeds, so nothing else says so, and before this
// the line was composed and thrown away — leaving a supervisor's journal with no record that the
// nightly job has been prompting an id the box does not list. The wanted sentence comes from
// hintNotice itself rather than a hand-typed copy, for the reason the headless twin reads it off
// notice.ContextFileNotices: the point of one composer is that the Drivers cannot drift.
func TestDaemonFireLogsTheCompositionsNotices(t *testing.T) {
	harness := newDaemonFireHarness(t, config.Options{
		Servers: []config.ServerEntry{{Name: "box", Endpoint: "http://box.invalid", APIKey: "k", Model: "my-alias"}},
	})
	harness.beat = heartbeat.Beat{
		Reachable: true, Answered: true,
		ActiveModel: "my-alias", ContextWindow: 131072,
		Resolution: provider.HintTrusted,
	}

	harness.fire(t, entryFor(t, "nightly", daemon.Action{Server: "box"}))

	// Unpinned, so the bound window is zero — rebindSpecFor keeps its hard-coded observed window
	// and a Firing states the pin or nothing (wire_firing.go).
	want := hintNotice("my-alias", provider.HintTrusted, 131072, 0)
	if want == "" {
		t.Fatal("the fixture composes no hint at all; it no longer covers the unadvertised-model case")
	}
	if !strings.Contains(harness.logged.String(), want) {
		t.Errorf("the daemon log is missing the composition's notice %q; it holds:\n%s", want, harness.logged.String())
	}
}

// A Firing logs what its run found WRONG with the workspace's context files and nothing else. The
// plain record of what loaded stays off the daemon log by ratified call — a journal is read for
// trouble, and one line per tick naming every file that loaded as expected buries the ticks worth
// looking at.
func TestDaemonFireLogsContextFileAnomaliesAlone(t *testing.T) {
	// One of each kind the composer distinguishes: a file that loaded, a file present but
	// unreadable, and standing content past its Budget share.
	report := domain.ContextFilesReport{
		Files: []domain.ContextFileNote{
			{Name: "AGENTS.md", Bytes: 3174},
			{Name: "BROKEN.md", Err: "permission denied"},
		},
		StandingTokens: 9000,
		SystemShare:    4000,
	}

	t.Run("the anomalies are logged and the loaded line is not", func(t *testing.T) {
		harness := newDaemonFireHarness(t, config.Options{
			Servers: []config.ServerEntry{{Name: "box", Endpoint: "http://box.invalid"}},
		})
		harness.runner.res = run.Result{SessionID: "s-1", Turns: 1, ContextFiles: report}

		harness.fire(t, entryFor(t, "nightly", daemon.Action{Server: "box"}))

		composed := notice.ContextFileNotices(report)
		if len(composed) != 3 {
			t.Fatalf("the composer yielded %d notices, want 3 — the fixture no longer covers all three kinds", len(composed))
		}
		logged := harness.logged.String()
		for _, n := range composed {
			switch {
			case n.Anomaly && !strings.Contains(logged, n.Text):
				t.Errorf("the daemon log is missing the anomaly %q; it holds:\n%s", n.Text, logged)
			case !n.Anomaly && strings.Contains(logged, n.Text):
				t.Errorf("the plain loaded-files line %q reached the daemon log; it is stderr's and the "+
					"transcript's, never this journal's", n.Text)
			}
		}
	})

	t.Run("a clean run says nothing at all", func(t *testing.T) {
		harness := newDaemonFireHarness(t, config.Options{
			Servers: []config.ServerEntry{{Name: "box", Endpoint: "http://box.invalid"}},
		})
		harness.runner.res = run.Result{SessionID: "s-2", Turns: 1}

		harness.fire(t, entryFor(t, "nightly", daemon.Action{Server: "box"}))

		if logged := harness.logged.String(); logged != "" {
			t.Errorf("a firing with no notices and no context files still wrote to the daemon log:\n%s", logged)
		}
	})
}

// An `auto:` schedule's deliverable IS the state of the workspace afterwards, and the daemon's log
// is the only place a supervisor sees what moved: the same block runHeadless prints on stderr, in
// the same wording, because both Drivers compose it once (writtenFilesLines, headless.go). No
// revert is offered here either — the journal behind the list died with the run.
func TestDaemonFireLogsTheFilesTheFiringWrote(t *testing.T) {
	t.Run("the header and one indented path per entry reach the log", func(t *testing.T) {
		harness := newDaemonFireHarness(t, config.Options{
			Servers: []config.ServerEntry{{Name: "box", Endpoint: "http://box.invalid"}},
		})
		harness.runner.res = run.Result{
			SessionID: "s-1", Turns: 1, Wrote: []string{"/ws/new.go", "/ws/old.go"},
		}

		harness.fire(t, entryFor(t, "nightly", daemon.Action{Server: "box"}))

		logged := harness.logged.String()
		for _, want := range writtenFilesLines(harness.runner.res.Wrote) {
			if !strings.Contains(logged, want+"\n") {
				t.Errorf("the daemon log is missing the line %q; it holds:\n%s", want, logged)
			}
		}
		if !strings.Contains(logged, "changed — 2 file(s) this run:") {
			t.Errorf("the composed header is not the one the log carries:\n%s", logged)
		}
	})

	t.Run("a firing that recorded no write logs nothing", func(t *testing.T) {
		harness := newDaemonFireHarness(t, config.Options{
			Servers: []config.ServerEntry{{Name: "box", Endpoint: "http://box.invalid"}},
		})
		harness.runner.res = run.Result{SessionID: "s-2", Turns: 1}

		harness.fire(t, entryFor(t, "nightly", daemon.Action{Server: "box"}))

		if logged := harness.logged.String(); strings.Contains(logged, "changed — ") {
			t.Errorf("a firing that wrote nothing still announced a change:\n%s", logged)
		}
	})

	// A Firing that stopped halfway is exactly the one whose partial writes a supervisor has to
	// know about, so the block is logged on the failure path too — beside the Outcome, not instead
	// of it.
	t.Run("a failed firing still reports what it changed", func(t *testing.T) {
		harness := newDaemonFireHarness(t, config.Options{
			Servers: []config.ServerEntry{{Name: "box", Endpoint: "http://box.invalid"}},
		})
		harness.runner.res = run.Result{SessionID: "s-3", Turns: 2, Wrote: []string{"/ws/half.go"}}
		harness.runner.err = errors.New("the model stopped mid-edit")

		if _, err := harness.raise(entryFor(t, "nightly", daemon.Action{Server: "box"})); err == nil {
			t.Fatal("the failed firing reported no error")
		}

		if logged := harness.logged.String(); !strings.Contains(logged, "  /ws/half.go\n") {
			t.Errorf("the failed firing's write went unreported:\n%s", logged)
		}
	})
}

// `confine-to-workspace: false` is the one blanket loosen in the system (ADR 0012), so an Auto
// Firing running under it says so on the daemon's log — in the launch path's own words, because a
// user who met that wording at a launch must not meet a softer one here. It is said ONCE per daemon
// process: the sentence is about the host's posture, which no tick changes, and a nightly Auto
// schedule that repeated it every tick would bury the ticks a supervisor reads this journal for.
func TestDaemonFireWarnsOnceOnUnconfinedAuto(t *testing.T) {
	t.Run("two auto firings warn exactly once", func(t *testing.T) {
		harness := newDaemonFireHarness(t, config.Options{
			ConfineToWorkspace: false,
			Servers:            []config.ServerEntry{{Name: "box", Endpoint: "http://box.invalid"}},
		})
		entry := entryFor(t, "nightly", daemon.Action{Server: "box", Mode: domain.ModeAuto})

		harness.fire(t, entry)
		harness.fire(t, entry)

		if got := strings.Count(harness.logged.String(), unconfinedAutoWarning); got != 1 {
			t.Errorf("the unconfined-auto warning was logged %d times over two firings, want exactly 1; "+
				"the log holds:\n%s", got, harness.logged.String())
		}
	})

	t.Run("a plan firing never warns", func(t *testing.T) {
		harness := newDaemonFireHarness(t, config.Options{
			ConfineToWorkspace: false,
			Servers:            []config.ServerEntry{{Name: "box", Endpoint: "http://box.invalid"}},
		})

		harness.fire(t, entryFor(t, "nightly", daemon.Action{Server: "box", Mode: domain.ModePlan}))

		if strings.Contains(harness.logged.String(), unconfinedAutoWarning) {
			t.Errorf("a plan firing said Auto's unconfined warning; the log holds:\n%s", harness.logged.String())
		}
	})
}

// The label-walk pre-warm is latched per WORKSPACE, not per process: two schedules bound to two
// trees each have a first confined command that would otherwise stall on the walk, while a second
// Firing of the same tree has nothing left to warm.
//
// What is asserted is the LATCH, never "the pre-warm ran": platform.PrewarmLabelWalk is an empty
// function off Windows (internal/platform/prewarm_other.go), so on this suite's hosts the call
// emits nothing to read back. The gate's own truth table is TestShouldPrewarmLabelWalk's
// (wire_boot_test.go); the verdict is re-read here only to prove the fixture still reaches the
// latch at all.
func TestDaemonFirePrewarmsEachWorkspaceOnce(t *testing.T) {
	harness := newDaemonFireHarness(t, config.Options{
		ConfineToWorkspace: true,
		Servers:            []config.ServerEntry{{Name: "box", Endpoint: "http://box.invalid"}},
	})
	if !shouldPrewarmLabelWalk(domain.ModeAuto, true, harness.wiring.confiner.Capabilities().FSWrite) {
		t.Fatal("the fixture's confined auto firing does not open the pre-warm gate at all; " +
			"the fake confiner no longer reports FSWrite")
	}

	first := entryFor(t, "nightly", daemon.Action{Server: "box", Mode: domain.ModeAuto})
	harness.fire(t, first)
	harness.fire(t, first)

	if got := len(harness.wiring.prewarmed); got != 1 {
		t.Errorf("two firings of one workspace latched %d pre-warms, want 1", got)
	}

	second := entryFor(t, "weekly", daemon.Action{Server: "box", Mode: domain.ModeAuto})
	if second.Run.Workspace == first.Run.Workspace {
		t.Fatal("the two entries share a workspace; the second-tree half of this test proves nothing")
	}
	harness.fire(t, second)

	if got := len(harness.wiring.prewarmed); got != 2 {
		t.Errorf("a firing of a second workspace left %d pre-warms latched, want 2", got)
	}
}
