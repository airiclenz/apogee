package agent

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/undo"
	"github.com/airiclenz/apogee/internal/workflow"
)

// Background workflows (ADR 0089): each test launches a recipe with StartRecipe{Background: true}
// on a parent whose skill resolver serves recipes (recipeConfig), over workflowResponder, which
// answers every item child from a script keyed by its task — "sweep <item>" — so the scripts hold
// however the children interleave. The parent itself never takes a Turn.

// sweepRecipe is a one-fanout recipe id over items, each child's task "sweep <item>".
func sweepRecipe(id string, items ...string) workflow.Recipe {
	return workflow.Recipe{
		ID: id,
		Plan: workflow.Plan{Name: id, Stages: []workflow.Stage{{
			Name:    "items",
			Kind:    workflow.StageFanout,
			Task:    "sweep {item}",
			Over:    &workflow.ItemSource{List: items},
			Returns: workflow.ReceiptSpec{"count": "int"},
		}}},
		Dir: "/skills/" + id,
	}
}

// newBackgroundParent builds a parent on cfg over up and stops its background workflows when the
// test ends.
func newBackgroundParent(t *testing.T, cfg domain.Config, up provider.Responder) *Agent {
	t.Helper()
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	t.Cleanup(a.stopAllBackground)
	return a
}

// launchBackground starts the recipe id in the background on a and returns the workflow's id.
func launchBackground(t *testing.T, a *Agent, id string) string {
	t.Helper()
	workflowID, err := a.StartRecipe(context.Background(), RecipeLaunch{SkillID: id, Background: true})
	if err != nil {
		t.Fatalf("StartRecipe(%s, background): %v", id, err)
	}
	if workflowID == "" {
		t.Fatalf("StartRecipe(%s, background) returned no workflow id", id)
	}
	return workflowID
}

// signalThenWait is a gate that closes started, then waits for release or the child's end.
func signalThenWait(started chan<- struct{}, release <-chan struct{}) func(context.Context) {
	return func(ctx context.Context) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
}

// awaitClosed fails the test unless ch closes within five seconds.
func awaitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// workflowInfo is the session's listing of the workflow id.
func workflowInfo(t *testing.T, a *Agent, id string) WorkflowInfo {
	t.Helper()
	infos, err := a.Workflows()
	if err != nil {
		t.Fatalf("Workflows: %v", err)
	}
	for _, info := range infos {
		if info.Status.ID == id {
			return info
		}
	}
	t.Fatalf("Workflows lists no %s: %+v", id, infos)
	return WorkflowInfo{}
}

// itemPhases maps each item of the workflow's first stage to its phase.
func itemPhases(info WorkflowInfo) map[string]workflow.Phase {
	phases := map[string]workflow.Phase{}
	if len(info.Status.Stages) == 0 {
		return phases
	}
	for _, item := range info.Status.Stages[0].Items {
		phases[item.Label] = item.Phase
	}
	return phases
}

func TestBackground_RunsAtTheServerWidthMinusOne(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		cap      int
		items    []string
		wantPeak int
	}{
		{"a cap-3 server keeps one slot free", 3, []string{"a1", "a2", "a3"}, 2},
		{"a width-1 server shares its slot", 1, []string{"b1", "b2"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := recipeConfig(t, newLockedSink(), sweepRecipe("sweep", tc.items...))
			cfg.ParallelAgents = tc.cap
			probe := newConcurrencyProbe(len(tc.items), 300*time.Millisecond)
			up := &workflowResponder{}
			for _, item := range tc.items {
				up.route("sweep "+item, probe.enter, finishScript("f-"+item, item+" is fine"))
			}
			a := newBackgroundParent(t, cfg, up)

			launchBackground(t, a, "sweep")
			a.background.waitAll()

			if peak := probe.peakInFlight(); peak != tc.wantPeak {
				t.Errorf("peak children in flight = %d, want %d on a cap-%d server", peak, tc.wantPeak, tc.cap)
			}
		})
	}
}

// TestBackground_ASessionSeatedFanOutQueuesOnTheSessionServer holds a background fan_out on the
// latched Sub-agent server, then launches a second with `run_on: "session"`: the line is kept per
// server, so the second starts at once beside the first and its child runs on the session server.
func TestBackground_ASessionSeatedFanOutQueuesOnTheSessionServer(t *testing.T) {
	t.Parallel()

	cfg := withSeatChoiceFanOut(workflowConfig(t, newLockedSink()), true)
	cfg.ParallelAgents = 2
	started, release := make(chan struct{}), make(chan struct{})
	up := (&workflowResponder{}).
		route("launch far", nil, toolCallScript("fo1", tools.FanOutToolName, seatArgsJSON("", true, "alpha"))).
		route("launch far", nil, contentScript("started it")).
		route("launch near", nil, toolCallScript("fo2", tools.FanOutToolName, seatArgsJSON(tools.RunOnSession, true, "beta"))).
		route("launch near", nil, contentScript("started it too")).
		route("check beta", nil, finishScript("s-beta", "beta is fine"))
	grunt := (&workflowResponder{}).
		route("check alpha", signalThenWait(started, release), finishScript("g-alpha", "alpha is fine")).
		route("check beta", nil, finishScript("g-beta", "beta is fine"))
	a := newBackgroundParent(t, cfg, up)
	a.dial = dialerTo(grunt).dial
	a.SetDelegationTarget(gruntTarget(gruntEndpoint, 2))

	runSubmitted(t, context.Background(), a, "launch far")
	awaitClosed(t, started, "the far workflow's child")
	runSubmitted(t, context.Background(), a, "launch near")

	for id, queued := range a.background.liveStates() {
		if queued {
			t.Errorf("workflow %s waits in line, want the session-seated one started beside the far one", id)
		}
	}
	close(release)
	a.background.waitAll()

	if onSession, onGrunt := up.askedCount("check beta"), grunt.askedCount("check beta"); onSession != 1 || onGrunt != 0 {
		t.Errorf("beta ran %d times on the session server and %d on the target, want once on the session server", onSession, onGrunt)
	}
}

// TestBackground_AFinishNoteSaysAnItemFellBackFromTheSubAgentsServer launches a background fan_out
// whose `run_on` asks for the Sub-agent server (ADR 0069 decision 9). With nothing latched its items
// run on the session server, and the finish note — the one line the model reads of a background
// run — ends on SeatFallbackNote, once. With a target latched the ask is honoured and the note
// carries none.
func TestBackground_AFinishNoteSaysAnItemFellBackFromTheSubAgentsServer(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		latched  bool
		wantNote bool
	}{
		{"nothing latched falls back to the session server and says so", false, true},
		{"a latched target runs the items where asked and adds no note", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := withSeatChoiceFanOut(workflowConfig(t, newLockedSink()), true)
			items := []string{"alpha", "beta"}
			up := (&workflowResponder{}).
				route("launch far", nil, toolCallScript("fo1", tools.FanOutToolName,
					seatArgsJSON(tools.RunOnSubAgentsServer, true, items...))).
				route("launch far", nil, contentScript("started it"))
			grunt := &workflowResponder{}
			for _, item := range items {
				up.route("check "+item, nil, finishScript("s-"+item, item+" is fine"))
				grunt.route("check "+item, nil, finishScript("g-"+item, item+" is fine"))
			}
			a := newBackgroundParent(t, cfg, up)
			if tc.latched {
				a.dial = dialerTo(grunt).dial
				a.SetDelegationTarget(gruntTarget(gruntEndpoint, 2))
			}

			runSubmitted(t, context.Background(), a, "launch far")
			a.background.waitAll()

			notes := a.background.takeNotes()
			if len(notes) != 1 {
				t.Fatalf("held notes = %q, want the one finish note", notes)
			}
			note := notes[0]
			if got := strings.Count(note, SeatFallbackNote); got != boolCount(tc.wantNote) {
				t.Errorf("finish note carries the seat-fallback note %d times, want %d:\n%s", got, boolCount(tc.wantNote), note)
			}
			if tc.wantNote && !strings.HasSuffix(note, finishSeparator+SeatFallbackNote) {
				t.Errorf("finish note does not end on the seat-fallback note:\n%s", note)
			}
			if strings.Contains(note, "\n") {
				t.Errorf("finish note spans more than one line:\n%s", note)
			}
		})
	}
}

// TestBackground_TheIdleOnlyMutatorsDoNotRaceARunningWorkflow runs each idle-only mutator over and
// over while a background workflow spawns its children one after another. Nothing orders the two
// goroutines, so under -race a child built off the live Agent's fields is a reported race; built
// off the launch-time host (backgroundHost) it is not, and every item still finishes on the Upstream
// and tools the workflow started with.
func TestBackground_TheIdleOnlyMutatorsDoNotRaceARunningWorkflow(t *testing.T) {
	t.Parallel()

	items := []string{"m1", "m2", "m3", "m4"}
	for _, tc := range []struct {
		name   string
		mutate func(a *Agent) error
	}{
		{"Rebind", func(a *Agent) error { return a.Rebind(RebindSpec{Model: "moved-model"}) }},
		{"SwitchUpstream", func(a *Agent) error { return a.SwitchUpstream(UpstreamSpec{Endpoint: "http://127.0.0.1:1"}) }},
		{"SwapTools", func(a *Agent) error { return a.SwapTools(domain.NewToolRegistry()) }},
		{"SetProfile", func(a *Agent) error { return a.SetProfile(domain.ModelProfile{}) }},
		{"SetJournal", func(a *Agent) error { a.SetJournal(undo.New(), ""); return nil }},
		{"reloadContextFiles", func(a *Agent) error { a.reloadContextFiles(); return nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := recipeConfig(t, newLockedSink(), sweepRecipe("sweep", items...))
			cfg.ParallelAgents = 2 // a background width of 1: the children spawn one after another
			up := &workflowResponder{}
			for _, item := range items {
				up.route("sweep "+item, nil, finishScript("f-"+item, item+" is fine"))
			}
			a := newBackgroundParent(t, cfg, up)
			id := launchBackground(t, a, "sweep")

			for range 20 {
				if err := tc.mutate(a); err != nil {
					t.Fatalf("%s while the workflow runs: %v", tc.name, err)
				}
			}
			a.background.waitAll()

			phases := itemPhases(workflowInfo(t, a, id))
			for _, item := range items {
				if phases[item] != workflow.PhaseDone {
					t.Errorf("item phases = %v, want every item done on the launch-time host", phases)
					break
				}
			}
		})
	}
}

func TestBackground_ASecondWorkflowOnTheSameServerWaitsInLine(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, newLockedSink(), sweepRecipe("first", "one"), sweepRecipe("second", "two"))
	started, release := make(chan struct{}), make(chan struct{})
	up := (&workflowResponder{}).
		route("sweep one", signalThenWait(started, release), finishScript("f1", "one is fine")).
		route("sweep two", nil, finishScript("f2", "two is fine"))
	a := newBackgroundParent(t, cfg, up)

	first := launchBackground(t, a, "first")
	awaitClosed(t, started, "the first workflow's child")
	second := launchBackground(t, a, "second")

	if info := workflowInfo(t, a, first); !info.Background || info.Queued {
		t.Errorf("first = %+v, want it running in the background", info)
	}
	if info := workflowInfo(t, a, second); !info.Background || !info.Queued || info.Status.Phase != workflow.PhasePending {
		t.Errorf("second = %+v, want it queued and pending", info)
	}
	if n := up.askedCount("sweep two"); n != 0 {
		t.Fatalf("the queued workflow's child ran %d times while the first held the server", n)
	}

	close(release)
	a.background.waitAll()

	if n := up.askedCount("sweep two"); n != 1 {
		t.Errorf("the queued workflow's child ran %d times, want 1 once the first ended", n)
	}
	if info := workflowInfo(t, a, second); info.Background || info.Status.Phase != workflow.PhaseDone {
		t.Errorf("second = %+v, want it done and no longer live", info)
	}
}

func TestBackground_FolderStampsFollowTheAgentClock(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, newLockedSink(), sweepRecipe("first", "one"), sweepRecipe("second", "two"))
	started, release := make(chan struct{}), make(chan struct{})
	up := (&workflowResponder{}).
		route("sweep one", signalThenWait(started, release), finishScript("f1", "one is fine")).
		route("sweep two", nil, finishScript("f2", "two is fine"))
	a := newBackgroundParent(t, cfg, up)
	pinned := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	a.now = func() time.Time { return pinned }

	first := launchBackground(t, a, "first")
	awaitClosed(t, started, "the first workflow's child")
	second := launchBackground(t, a, "second")
	if info := workflowInfo(t, a, second); !info.Queued {
		t.Fatalf("second = %+v, want it queued behind the first", info)
	}
	if err := a.StopWorkflow(second); err != nil {
		t.Fatalf("StopWorkflow(%s): %v", second, err)
	}
	close(release)
	a.background.waitAll()

	for _, id := range []string{first, second} {
		status := workflowInfo(t, a, id).Status
		if !status.Created.Equal(pinned) || !status.Updated.Equal(pinned) {
			t.Errorf("%s stamped created %v, updated %v; want both %v from the Agent's clock",
				id, status.Created, status.Updated, pinned)
		}
	}
	if phase := workflowInfo(t, a, second).Status.Phase; phase != workflow.PhaseStopped {
		t.Errorf("the stopped queued workflow is %s, want stopped", phase)
	}
}

func TestBackground_StopKeepsTheFinishedItems(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, newLockedSink(), sweepRecipe("pair", "alpha", "beta"))
	started := make(chan struct{})
	up := (&workflowResponder{}).
		route("sweep alpha", nil, finishScript("f1", "alpha is fine")).
		route("sweep beta", signalThenWait(started, nil), cancelledScript())
	a := newBackgroundParent(t, cfg, up)
	id := launchBackground(t, a, "pair")
	awaitClosed(t, started, "beta's child")

	if err := a.StopWorkflow(id); err != nil {
		t.Fatalf("StopWorkflow: %v", err)
	}
	a.background.waitAll()

	info := workflowInfo(t, a, id)
	if info.Background || info.Status.Phase != workflow.PhaseStopped {
		t.Errorf("after the stop = %+v, want a stopped workflow no longer live", info)
	}
	if phases := itemPhases(info); phases["alpha"] != workflow.PhaseDone || phases["beta"] == workflow.PhaseDone {
		t.Errorf("item phases = %v, want alpha kept done and beta unfinished", phases)
	}
	if err := a.StopWorkflow(id); err == nil || !strings.Contains(err.Error(), id) {
		t.Errorf("a second stop = %v, want the no-such-workflow error naming %s", err, id)
	}
}

// blockedScript is a child's finish call declaring its item blocked.
func blockedScript(id, summary string) []provider.Delta {
	return toolCallScript(id, tools.FinishToolName, `{"status":"blocked","summary":"`+summary+`","count":0}`)
}

// itemReceipts maps each item of the workflow's first stage to its receipt's status ("" for none).
func itemReceipts(info WorkflowInfo) map[string]workflow.Status {
	statuses := map[string]workflow.Status{}
	if len(info.Status.Stages) == 0 {
		return statuses
	}
	for _, item := range info.Status.Stages[0].Items {
		statuses[item.Label] = ""
		if item.Receipt != nil {
			statuses[item.Label] = item.Receipt.Status
		}
	}
	return statuses
}

func TestBackground_RerunFailedRunsOnlyTheBlockedAndFaultedItems(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, newLockedSink(), sweepRecipe("trio", "alpha", "beta", "gamma"))
	// gamma has no route on the first run: every attempt faults, and its retries end it blocked.
	up := (&workflowResponder{}).
		route("sweep alpha", nil, finishScript("f1", "alpha is fine")).
		route("sweep beta", nil, blockedScript("b1", "beta is stuck"))
	a := newBackgroundParent(t, cfg, up)
	id := launchBackground(t, a, "trio")
	a.background.waitAll()

	first := workflowInfo(t, a, id)
	if first.Status.Recipe != "trio" {
		t.Errorf("status.json recipe = %q, want the launching recipe %q", first.Status.Recipe, "trio")
	}
	want := map[string]workflow.Status{"alpha": workflow.StatusOK, "beta": workflow.StatusBlocked, "gamma": workflow.StatusBlocked}
	if got := itemReceipts(first); !maps.Equal(got, want) {
		t.Fatalf("first run receipts = %v, want %v", got, want)
	}

	up.route("sweep beta", nil, finishScript("f2", "beta is fine")).
		route("sweep gamma", nil, finishScript("f3", "gamma is fine"))
	if err := a.RerunFailed(id); err != nil {
		t.Fatalf("RerunFailed: %v", err)
	}
	a.background.waitAll()

	for key, runs := range map[string]int{"sweep alpha": 1, "sweep beta": 2, "sweep gamma": 1} {
		if n := up.askedCount(key); n != runs {
			t.Errorf("%q ran %d times over both runs, want %d — the re-run touches only the failed items", key, n, runs)
		}
	}
	second := workflowInfo(t, a, id)
	want = map[string]workflow.Status{"alpha": workflow.StatusOK, "beta": workflow.StatusOK, "gamma": workflow.StatusOK}
	if got := itemReceipts(second); !maps.Equal(got, want) || second.Status.Phase != workflow.PhaseDone {
		t.Errorf("after the re-run = %v (%s), want every item ok and the workflow done", got, second.Status.Phase)
	}
	if folders := workflowFolders(t, cfg.ScratchDir); len(folders) != 1 {
		t.Errorf("workflow folders = %v, want the one folder run again", folders)
	}
	if err := a.RerunFailed(id); err == nil || !strings.Contains(err.Error(), "no blocked or faulted items") {
		t.Errorf("a re-run with nothing failed = %v, want the nothing-to-re-run refusal", err)
	}
}

func TestBackground_RerunFailedRefusesALiveOrUnfinishedWorkflow(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, newLockedSink(), sweepRecipe("pair", "alpha", "beta"))
	started := make(chan struct{})
	up := (&workflowResponder{}).
		route("sweep alpha", nil, blockedScript("b1", "alpha is stuck")).
		route("sweep beta", signalThenWait(started, nil), cancelledScript())
	a := newBackgroundParent(t, cfg, up)
	id := launchBackground(t, a, "pair")
	awaitClosed(t, started, "beta's child")

	if err := a.RerunFailed(id); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("a re-run of a running workflow = %v, want the already-running refusal", err)
	}
	if err := a.StopWorkflow(id); err != nil {
		t.Fatalf("StopWorkflow: %v", err)
	}
	a.background.waitAll()
	if err := a.RerunFailed(id); err == nil || !strings.Contains(err.Error(), "has not finished") {
		t.Errorf("a re-run of a stopped workflow = %v, want the not-finished refusal", err)
	}
	if a.background.isLive(id) {
		t.Error("a refused re-run left the workflow live")
	}
	if err := a.RerunFailed("20260101-000000-nothing"); err == nil {
		t.Error("a re-run of an unknown workflow succeeded")
	}
}

func TestBackground_StoppingAQueuedRerunLeavesTheFolderDone(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, newLockedSink(),
		sweepRecipe("pair", "alpha"), sweepRecipe("slow", "hold"), sweepRecipe("fresh", "new"))
	started, release := make(chan struct{}), make(chan struct{})
	up := (&workflowResponder{}).
		route("sweep alpha", nil, blockedScript("b1", "alpha is stuck")).
		route("sweep hold", signalThenWait(started, release), finishScript("f1", "hold is fine")).
		route("sweep new", nil, finishScript("f2", "new is fine"))
	a := newBackgroundParent(t, cfg, up)
	id := launchBackground(t, a, "pair")
	a.background.waitAll()

	launchBackground(t, a, "slow")
	awaitClosed(t, started, "the slow workflow's child")
	if err := a.RerunFailed(id); err != nil {
		t.Fatalf("RerunFailed behind a running workflow: %v", err)
	}
	fresh := launchBackground(t, a, "fresh")
	if info := workflowInfo(t, a, id); !info.Queued {
		t.Fatalf("the re-run = %+v, want it queued behind the slow workflow", info)
	}

	for _, stop := range []string{id, fresh} {
		if err := a.StopWorkflow(stop); err != nil {
			t.Fatalf("StopWorkflow(%s): %v", stop, err)
		}
	}
	if info := workflowInfo(t, a, id); info.Background || info.Status.Phase != workflow.PhaseDone {
		t.Errorf("the stopped re-run's folder = %+v, want it still done and no longer live", info)
	}
	if info := workflowInfo(t, a, fresh); info.Background || info.Status.Phase != workflow.PhaseStopped {
		t.Errorf("the stopped new workflow = %+v, want it stopped and no longer live", info)
	}
	close(release)
	a.background.waitAll()

	up.route("sweep alpha", nil, finishScript("f3", "alpha is fine"))
	if err := a.RerunFailed(id); err != nil {
		t.Fatalf("RerunFailed after the queued re-run was stopped: %v", err)
	}
	a.background.waitAll()
	if info := workflowInfo(t, a, id); info.Status.Phase != workflow.PhaseDone || itemReceipts(info)["alpha"] != workflow.StatusOK {
		t.Errorf("after the second re-run = %+v, want alpha ok and the workflow done", info)
	}
	if n := up.askedCount("sweep new"); n != 0 {
		t.Errorf("the stopped new workflow's child ran %d times, want 0", n)
	}
}

func TestBackground_CloseStopsEveryWorkflow(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, newLockedSink(), sweepRecipe("first", "one"), sweepRecipe("second", "two"))
	started := make(chan struct{})
	up := (&workflowResponder{}).
		route("sweep one", signalThenWait(started, nil), cancelledScript()).
		route("sweep two", nil, finishScript("f2", "two is fine"))
	a := newBackgroundParent(t, cfg, up)
	first := launchBackground(t, a, "first")
	awaitClosed(t, started, "the first workflow's child")
	second := launchBackground(t, a, "second")

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	for _, id := range []string{first, second} {
		if info := workflowInfo(t, a, id); info.Background || info.Status.Phase != workflow.PhaseStopped {
			t.Errorf("%s after Close = %+v, want it stopped and no longer live", id, info)
		}
	}
	if n := up.askedCount("sweep two"); n != 0 {
		t.Errorf("the queued workflow's child ran %d times; Close drops it from the line", n)
	}
}

func TestBackground_AFinishedDelegationLeavesTheWorkflowRunning(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, newLockedSink(), sweepRecipe("pair", "alpha"))
	started := make(chan struct{})
	up := (&workflowResponder{}).route("sweep alpha", signalThenWait(started, nil), cancelledScript())
	a := newBackgroundParent(t, cfg, up)
	id := launchBackground(t, a, "pair")
	awaitClosed(t, started, "alpha's child")
	child, err := a.newChildAgent("c1", "a delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}

	if err := child.Close(); err != nil {
		t.Fatalf("child Close: %v", err)
	}

	if !a.background.isLive(id) {
		t.Error("a delegate's Close stopped the parent's background workflow")
	}
}

func TestBackground_ASnapshotRoundTripResumesTheWorkflow(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, newLockedSink(), sweepRecipe("pair", "alpha", "beta"))
	started := make(chan struct{})
	up := (&workflowResponder{}).
		route("sweep alpha", nil, finishScript("f1", "alpha is fine")).
		route("sweep beta", signalThenWait(started, nil), cancelledScript()).
		route("sweep beta", nil, finishScript("f2", "beta is fine"))
	a := newBackgroundParent(t, cfg, up)
	id := launchBackground(t, a, "pair")
	awaitClosed(t, started, "beta's child")

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	var st agentState
	if err := json.Unmarshal(snap.State, &st); err != nil {
		t.Fatalf("decode the snapshot: %v", err)
	}
	if want := []workflowEntryJSON{{ID: id, Recipe: "pair"}}; len(st.Workflows) != 1 || st.Workflows[0] != want[0] {
		t.Fatalf("snapshot workflows = %+v, want %+v", st.Workflows, want)
	}

	b, err := resumeAgent(cfg, snap, up)
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	t.Cleanup(b.stopAllBackground)
	if b.background.isLive(id) {
		t.Fatal("the restore started the workflow; only ResumeWorkflows may")
	}
	if err := b.ResumeWorkflows(); err != nil {
		t.Fatalf("ResumeWorkflows: %v", err)
	}
	b.background.waitAll()

	info := workflowInfo(t, b, id)
	if info.Status.Phase != workflow.PhaseDone {
		t.Errorf("resumed workflow = %+v, want it done in its own folder", info)
	}
	if n := up.askedCount("sweep alpha"); n != 1 {
		t.Errorf("alpha's child ran %d times, want 1 — the resume skips a finished item", n)
	}
	if folders := workflowFolders(t, cfg.ScratchDir); len(folders) != 1 {
		t.Errorf("workflow folders = %v, want the one folder resumed", folders)
	}
}

func TestBackground_RestoreSessionStopsTheOutgoingSet(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, newLockedSink(), sweepRecipe("pair", "alpha"))
	started := make(chan struct{})
	up := (&workflowResponder{}).route("sweep alpha", signalThenWait(started, nil), cancelledScript())
	a := newBackgroundParent(t, cfg, up)
	id := launchBackground(t, a, "pair")
	awaitClosed(t, started, "alpha's child")
	incoming := []workflowEntryJSON{{ID: "20260101-000000-other"}}

	err := a.RestoreSession(cutFixtureSession(t, agentState{Conversation: domain.NewConversation(nil), Workflows: incoming}))
	if err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}

	if info := workflowInfo(t, a, id); info.Background || info.Status.Phase != workflow.PhaseStopped {
		t.Errorf("outgoing workflow = %+v, want it stopped before the restore returned", info)
	}
	if got := a.background.entries(a.ScratchDir()); len(got) != 1 || got[0] != incoming[0] {
		t.Errorf("entries after the restore = %+v, want the incoming set loaded", got)
	}
}

// countingApprover counts its calls and allows every one.
type countingApprover struct {
	calls atomic.Int32
}

func (c *countingApprover) Approve(context.Context, domain.ApprovalRequest) (domain.ApprovalDecision, error) {
	c.calls.Add(1)
	return domain.ApprovalAllow, nil
}

func TestBackground_AnApprovalWaitsInTheManagersQueue(t *testing.T) {
	t.Parallel()

	var ran atomic.Int32
	write := fakeTool{name: "write_thing", execute: func(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
		ran.Add(1)
		return domain.ToolResult{CallID: call.ID, Content: "written"}, nil
	}}
	cfg := workflowConfig(t, newLockedSink(), write)
	cfg.Skills = fakeRecipes{recipes: map[string]workflow.Recipe{"pair": sweepRecipe("pair", "alpha")}}
	approver := &countingApprover{}
	cfg.Approver = approver
	up := (&workflowResponder{}).
		route("sweep alpha", nil, toolCallScript("w1", "write_thing", `{}`)).
		route("sweep alpha", nil, finishScript("f1", "alpha is fine"))
	a := newBackgroundParent(t, cfg, up)
	id := launchBackground(t, a, "pair")

	var prompt *backgroundPrompt
	for deadline := time.Now().Add(5 * time.Second); prompt == nil; time.Sleep(5 * time.Millisecond) {
		if waiting := a.background.waiting(); len(waiting) > 0 {
			prompt = waiting[0]
		}
		if time.Now().After(deadline) {
			t.Fatal("no approval reached the manager's queue")
		}
	}
	if prompt.workflow != id || prompt.approval == nil || prompt.approval.Tool != "write_thing" {
		t.Fatalf("queued prompt = %+v, want write_thing's approval for %s", prompt, id)
	}
	if n := approver.calls.Load(); n != 0 {
		t.Fatalf("the host Approver was asked %d times; a background gate waits in the queue", n)
	}

	if !a.background.answer(prompt, backgroundAnswer{decision: domain.ApprovalAllow}) {
		t.Fatal("the queued approval could not be answered")
	}
	a.background.waitAll()

	if n := ran.Load(); n != 1 {
		t.Errorf("write_thing ran %d times, want 1 once allowed", n)
	}
	if n := approver.calls.Load(); n != 0 {
		t.Errorf("the host Approver was asked %d times, want 0", n)
	}
	if phases := itemPhases(workflowInfo(t, a, id)); phases["alpha"] != workflow.PhaseDone {
		t.Errorf("item phases = %v, want alpha done", phases)
	}
}

func TestBackground_AMissingInputIsRefusedNotAsked(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, newLockedSink(), reviewRecipe())
	asker := &scriptedAsker{answer: "lib"}
	cfg.Asker = asker
	a := newBackgroundParent(t, cfg, &workflowResponder{})

	id, err := a.StartRecipe(context.Background(), RecipeLaunch{SkillID: "review", Background: true})

	if err == nil || err.Error() != "missing input: scope" || id != "" {
		t.Errorf("StartRecipe = %q, %v; want the refusal `missing input: scope`", id, err)
	}
	if len(asker.questions) != 0 {
		t.Errorf("a background launch asked %+v", asker.questions)
	}
}

// askRecipe asks whether to sweep, then sweeps alpha.
func askRecipe() workflow.Recipe {
	recipe := sweepRecipe("asking", "alpha")
	recipe.Plan.Stages = append([]workflow.Stage{{
		Name: "go", Kind: workflow.StageAsk, Question: "Sweep now?", Options: []string{"yes", "no"}, Default: "no",
	}}, recipe.Plan.Stages...)
	return recipe
}

// promptProbeSink records every event and, on each background WorkflowWaiting, what the engine's
// queue lists at that moment — so a test can pin that the event never runs ahead of the queue. With
// autoAnswer set it answers each prompt so listed at once, so a question asked where none should be
// ends the workflow instead of hanging it.
type promptProbeSink struct {
	*lockedSink
	agent      atomic.Pointer[Agent]
	autoAnswer atomic.Pointer[string]
	mu         sync.Mutex
	listed     [][]domain.WorkflowPrompt
	waited     chan struct{}
	once       sync.Once
}

func newPromptProbeSink() *promptProbeSink {
	return &promptProbeSink{lockedSink: newLockedSink(), waited: make(chan struct{})}
}

func (s *promptProbeSink) Emit(e domain.Event) {
	s.lockedSink.Emit(e)
	phase, ok := e.(domain.WorkflowPhaseEvent)
	if !ok || phase.Phase != domain.WorkflowWaiting || !phase.Background {
		return
	}
	if a := s.agent.Load(); a != nil {
		listed := a.WorkflowPrompts()
		s.mu.Lock()
		s.listed = append(s.listed, listed)
		s.mu.Unlock()
		if answer := s.autoAnswer.Load(); answer != nil {
			for _, prompt := range listed {
				a.AnswerWorkflowPrompt(prompt.ID, domain.WorkflowPromptAnswer{Text: *answer})
			}
		}
	}
	s.once.Do(func() { close(s.waited) })
}

// waitings returns every background WorkflowWaiting event the sink saw, in order.
func (s *promptProbeSink) waitings() []domain.WorkflowPhaseEvent {
	s.lockedSink.mu.Lock()
	defer s.lockedSink.mu.Unlock()
	var waitings []domain.WorkflowPhaseEvent
	for _, e := range s.lockedSink.events {
		if phase, ok := e.(domain.WorkflowPhaseEvent); ok && phase.Phase == domain.WorkflowWaiting && phase.Background {
			waitings = append(waitings, phase)
		}
	}
	return waitings
}

func TestBackground_AQuestionIsListedBeforeItIsReportedAndItsAnswerResumesTheWorkflow(t *testing.T) {
	t.Parallel()

	sink := newPromptProbeSink()
	cfg := recipeConfig(t, sink, askRecipe())
	host := &scriptedAsker{answer: "host"}
	cfg.Asker = host
	up := (&workflowResponder{}).route("sweep alpha", nil, finishScript("f1", "alpha is fine"))
	a := newBackgroundParent(t, cfg, up)
	sink.agent.Store(a)
	id := launchBackground(t, a, "asking")
	awaitClosed(t, sink.waited, "the question's waiting event")

	sink.mu.Lock()
	listed := sink.listed[0]
	sink.mu.Unlock()
	if len(listed) != 1 || listed[0].Workflow != id || listed[0].Name != "asking" || listed[0].Question == nil ||
		listed[0].Question.Question != "Sweep now?" || !slices.Equal(listed[0].Question.Choices, []string{"yes", "no"}) {
		t.Fatalf("queue at the waiting event = %+v, want the question already listed", listed)
	}
	if waiting := sink.waitings(); len(waiting) != 1 || waiting[0].Workflow != id || waiting[0].Stage != "go" || waiting[0].Detail != "Sweep now?" {
		t.Errorf("waiting events = %+v, want one naming the stage and its question", waiting)
	}

	if !a.AnswerWorkflowPrompt(listed[0].ID, domain.WorkflowPromptAnswer{Text: "yes"}) {
		t.Fatal("AnswerWorkflowPrompt refused the waiting question")
	}
	if a.AnswerWorkflowPrompt(listed[0].ID, domain.WorkflowPromptAnswer{Text: "no"}) {
		t.Error("AnswerWorkflowPrompt answered the same question twice")
	}
	a.background.waitAll()

	info := workflowInfo(t, a, id)
	if receipt := info.Status.Stages[0].Items[0].Receipt; receipt == nil || receipt.Fields[workflow.AskAnswerField] != "yes" {
		t.Errorf("ask stage receipt = %+v, want the answer yes", receipt)
	}
	if n := up.askedCount("sweep alpha"); n != 1 {
		t.Errorf("alpha's child ran %d times, want 1 after the answer", n)
	}
	if len(host.questions) != 0 || len(a.WorkflowPrompts()) != 0 {
		t.Errorf("host Asker asked %+v, queue %+v; want neither used", host.questions, a.WorkflowPrompts())
	}
}

func TestBackground_AnApprovalIsListedAndAnsweredThroughTheDriverCalls(t *testing.T) {
	t.Parallel()

	var ran atomic.Int32
	write := fakeTool{name: "write_thing", execute: func(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
		ran.Add(1)
		return domain.ToolResult{CallID: call.ID, Content: "written"}, nil
	}}
	sink := newPromptProbeSink()
	cfg := workflowConfig(t, sink, write)
	cfg.Skills = fakeRecipes{recipes: map[string]workflow.Recipe{"pair": sweepRecipe("pair", "alpha")}}
	cfg.Approver = &countingApprover{}
	up := (&workflowResponder{}).
		route("sweep alpha", nil, toolCallScript("w1", "write_thing", `{}`)).
		route("sweep alpha", nil, finishScript("f1", "alpha is fine"))
	a := newBackgroundParent(t, cfg, up)
	sink.agent.Store(a)
	id := launchBackground(t, a, "pair")
	awaitClosed(t, sink.waited, "the approval's waiting event")

	prompts := a.WorkflowPrompts()
	if len(prompts) != 1 || prompts[0].Workflow != id || prompts[0].Approval == nil || prompts[0].Approval.Tool != "write_thing" {
		t.Fatalf("WorkflowPrompts = %+v, want write_thing's approval", prompts)
	}
	if waiting := sink.waitings(); len(waiting) != 1 || waiting[0].Detail != "approve write_thing" {
		t.Errorf("waiting events = %+v, want one naming the tool", waiting)
	}
	if !a.AnswerWorkflowPrompt(prompts[0].ID, domain.WorkflowPromptAnswer{Decision: domain.ApprovalAllow}) {
		t.Fatal("AnswerWorkflowPrompt refused the waiting approval")
	}
	a.background.waitAll()

	if n := ran.Load(); n != 1 {
		t.Errorf("write_thing ran %d times, want 1 once allowed", n)
	}
}

func TestBackground_AResumeReplaysAnAnsweredQuestion(t *testing.T) {
	t.Parallel()

	sink := newPromptProbeSink()
	cfg := recipeConfig(t, sink, askRecipe())
	cfg.Asker = &scriptedAsker{answer: "host"}
	started := make(chan struct{})
	up := (&workflowResponder{}).
		route("sweep alpha", signalThenWait(started, nil), cancelledScript()).
		route("sweep alpha", nil, finishScript("f1", "alpha is fine"))
	a := newBackgroundParent(t, cfg, up)
	sink.agent.Store(a)
	id := launchBackground(t, a, "asking")
	awaitClosed(t, sink.waited, "the question's waiting event")
	if !a.AnswerWorkflowPrompt(a.WorkflowPrompts()[0].ID, domain.WorkflowPromptAnswer{Text: "yes"}) {
		t.Fatal("AnswerWorkflowPrompt refused the waiting question")
	}
	awaitClosed(t, started, "alpha's child")
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	b, err := resumeAgent(cfg, snap, up)
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	t.Cleanup(b.stopAllBackground)
	sink.agent.Store(b)
	again := "yes"
	sink.autoAnswer.Store(&again)
	if err := b.ResumeWorkflows(); err != nil {
		t.Fatalf("ResumeWorkflows: %v", err)
	}
	b.background.waitAll()

	if waiting := sink.waitings(); len(waiting) != 1 {
		t.Errorf("waiting events = %+v, want the one before the resume: an answered question is not asked again", waiting)
	}
	info := workflowInfo(t, b, id)
	if info.Status.Phase != workflow.PhaseDone {
		t.Errorf("resumed workflow = %+v, want it done", info)
	}
	if receipt := info.Status.Stages[0].Items[0].Receipt; receipt == nil || receipt.Fields[workflow.AskAnswerField] != "yes" {
		t.Errorf("ask stage receipt = %+v, want the replayed answer yes", receipt)
	}
}

// A session boundary and the background set (ADR 0089 D5, plan 2026-09-27 - 00 item 34): a clear
// or restore stops the outgoing set and drops its held notes, unless KeepWorkflows marked it to
// keep them; a held note rides the snapshot. Each test ends "pair" first, so its note is held, and
// then launches "slow", whose one child waits until it is stopped.

// newBoundaryParent builds a parent serving "pair" (one item that finishes) and "slow" (one item
// whose first child waits for its stop, and whose next one finishes) over a scratch directory
// named like a session's, and returns it, its request log and the channel slow's child closes.
func newBoundaryParent(t *testing.T) (*Agent, *requestLog, chan struct{}) {
	t.Helper()
	started := make(chan struct{})
	up := (&workflowResponder{}).
		route("sweep alpha", nil, finishScript("f1", "alpha is fine")).
		route("sweep gamma", signalThenWait(started, nil), cancelledScript()).
		route("sweep gamma", nil, finishScript("f2", "gamma is fine")).
		route(wakeUserText, nil, contentScript("done"))
	cfg := recipeConfig(t, newLockedSink(), sweepRecipe("pair", "alpha"), sweepRecipe("slow", "gamma"))
	cfg.ScratchDir = sessionScratch(t, filepath.Dir(cfg.ScratchDir), "20260928T100000Z-0a0a0a0a")
	log := &requestLog{inner: up}
	return newBackgroundParent(t, cfg, log), log, started
}

// sessionScratch creates and returns the session scratch directory id under root.
func sessionScratch(t *testing.T, root, id string) string {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return dir
}

// holdPairNoteThenRunSlow ends "pair" so its note is held, then starts "slow" and waits for its
// child; it returns slow's workflow id.
func holdPairNoteThenRunSlow(t *testing.T, a *Agent, started chan struct{}) string {
	t.Helper()
	launchBackground(t, a, "pair")
	a.background.waitAll()
	slow := launchBackground(t, a, "slow")
	awaitClosed(t, started, "slow's child")
	return slow
}

func TestBackground_AClearStopsTheSetAndDropsItsNotes(t *testing.T) {
	t.Parallel()

	a, _, started := newBoundaryParent(t)
	slow := holdPairNoteThenRunSlow(t, a, started)

	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext: %v", err)
	}

	if info := workflowInfo(t, a, slow); info.Background || info.Status.Phase != workflow.PhaseStopped {
		t.Errorf("slow after the clear = %+v, want it stopped before the clear returned", info)
	}
	if notes := a.background.takeNotes(); len(notes) != 0 {
		t.Errorf("held notes after the clear = %q, want none: a stopped set leaves nothing behind", notes)
	}
	if woke, err := a.Wake(context.Background()); err != nil || woke {
		t.Errorf("Wake after the clear = %v, %v; want nothing to wake on", woke, err)
	}
}

func TestBackground_AClearWithNothingRunningKeepsTheHeldNote(t *testing.T) {
	t.Parallel()

	a, log, _ := newBoundaryParent(t)
	launchBackground(t, a, "pair")
	a.background.waitAll()

	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext: %v", err)
	}

	runInput(t, a, domain.UserInput{Text: wakeUserText})
	sent := log.first(t, wakeUserText)
	if !strings.HasPrefix(sent, wakeUserText) || !strings.Contains(sent, workflowNoteHeader+"\n"+pairNoteLine) {
		t.Errorf("the new conversation's first message = %q, want the human's text and then the note "+
			"held before a clear that stopped nothing", sent)
	}
}

func TestBackground_AKeptClearLeavesTheSetRunningAndItsNoteOpensTheNewConversation(t *testing.T) {
	t.Parallel()

	a, log, started := newBoundaryParent(t)
	slow := holdPairNoteThenRunSlow(t, a, started)

	a.KeepWorkflows()
	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext: %v", err)
	}

	if !a.background.isLive(slow) {
		t.Fatal("a kept clear stopped the running workflow")
	}
	runInput(t, a, domain.UserInput{Text: wakeUserText})
	sent := log.first(t, wakeUserText)
	if !strings.HasPrefix(sent, wakeUserText) || !strings.Contains(sent, workflowNoteHeader+"\n"+pairNoteLine) {
		t.Errorf("the new conversation's first message = %q, want the human's text and then the held note", sent)
	}
	if err := a.ClearContext(); err != nil {
		t.Fatalf("second ClearContext: %v", err)
	}
	if a.background.isLive(slow) {
		t.Error("the keep mark outlived its boundary: a second clear left the workflow running")
	}
}

func TestBackground_AKeptRestoreKeepsTheSetAndItsNote(t *testing.T) {
	t.Parallel()

	a, _, started := newBoundaryParent(t)
	slow := holdPairNoteThenRunSlow(t, a, started)

	a.KeepWorkflows()
	err := a.RestoreSession(cutFixtureSession(t, agentState{Conversation: domain.NewConversation(nil)}))
	if err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}

	if !a.background.isLive(slow) {
		t.Error("a kept restore stopped the running workflow")
	}
	if notes := a.background.takeNotes(); len(notes) != 1 || !strings.HasPrefix(notes[0], pairNoteLine) {
		t.Errorf("held notes after a kept restore = %q, want pair's note carried over", notes)
	}
}

func TestBackground_AHeldNoteSurvivesASnapshotAndRestore(t *testing.T) {
	t.Parallel()

	a, log, _ := newBoundaryParent(t)
	launchBackground(t, a, "pair")
	a.background.waitAll()

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	var st agentState
	if err := json.Unmarshal(snap.State, &st); err != nil {
		t.Fatalf("decode the snapshot: %v", err)
	}
	if len(st.WorkflowNotes) != 1 || !strings.HasPrefix(st.WorkflowNotes[0], pairNoteLine) {
		t.Fatalf("snapshot workflow_notes = %q, want pair's held note", st.WorkflowNotes)
	}

	b, err := resumeAgent(a.cfg, snap, log)
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	t.Cleanup(b.stopAllBackground)
	if err := b.ResumeWorkflows(); err != nil {
		t.Fatalf("ResumeWorkflows: %v", err)
	}
	runInput(t, b, domain.UserInput{Text: wakeUserText})
	assertNoteMessage(t, log.first(t, wakeUserText))
}

func TestBackground_AWorkflowKeptAcrossAScratchMoveIsListedAndResumesFromItsHome(t *testing.T) {
	t.Parallel()

	a, _, started := newBoundaryParent(t)
	home := filepath.Base(a.ScratchDir())
	slow := launchBackground(t, a, "slow")
	awaitClosed(t, started, "slow's child")
	a.KeepWorkflows()
	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext: %v", err)
	}
	a.SetScratchDir(sessionScratch(t, filepath.Dir(a.ScratchDir()), "20260928T110000Z-0b0b0b0b"))

	if info := workflowInfo(t, a, slow); !info.Background {
		t.Errorf("the kept workflow after the scratch move = %+v, want it listed as running", info)
	}
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	var st agentState
	if err := json.Unmarshal(snap.State, &st); err != nil {
		t.Fatalf("decode the snapshot: %v", err)
	}
	if want := (workflowEntryJSON{ID: slow, Recipe: "slow", Home: home}); len(st.Workflows) != 1 || st.Workflows[0] != want {
		t.Fatalf("snapshot workflows = %+v, want %+v", st.Workflows, want)
	}

	cfg := a.cfg
	cfg.ScratchDir = a.ScratchDir()
	b, err := resumeAgent(cfg, snap, a.upstream)
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	t.Cleanup(b.stopAllBackground)
	if err := b.ResumeWorkflows(); err != nil {
		t.Fatalf("ResumeWorkflows: %v", err)
	}
	b.background.waitAll()
	if info := workflowInfo(t, b, slow); info.Status.Phase != workflow.PhaseDone {
		t.Errorf("the resumed workflow = %+v, want it done in its own folder", info)
	}
}

// noteCount is how many messages of a's conversation carry the "pair" recipe's finish note.
func noteCount(a *Agent) int {
	count := 0
	for i := range a.conv.Len() {
		if strings.Contains(a.conv.At(i).Content, pairNoteLine) {
			count++
		}
	}
	return count
}

func TestBackground_AnAbortedOpeningHoldsItsNoteAgainForTheNextExchange(t *testing.T) {
	t.Parallel()

	up := (&workflowResponder{}).
		route("sweep alpha", nil, finishScript("f1", "alpha is fine")).
		route(wakeUserText, nil, toolCallScript("c1", "read_thing", `{}`)).
		route("try again", nil, contentScript("done")).
		route("third", nil, contentScript("ok"))
	a, log := newPairParent(t, newLockedSink(), up, nil)
	launchBackground(t, a, "pair")
	a.background.waitAll()
	if err := a.Submit(domain.UserInput{Text: wakeUserText}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res, err := a.Step(context.Background()); err != nil || res.Status != domain.StatusTurnComplete {
		t.Fatalf("first Step = %+v, %v; want a Turn that leaves the Exchange open", res, err)
	}

	a.AbortExchange()

	runInput(t, a, domain.UserInput{Text: "try again"})
	assertNoteMessage(t, log.first(t, "try again"))
	if got := noteCount(a); got != 1 {
		t.Errorf("messages carrying the note = %d, want 1: the aborted opening's copy is gone", got)
	}
	runInput(t, a, domain.UserInput{Text: "third"})
	if sent := log.first(t, "third"); strings.Contains(sent, workflowNoteHeader) {
		t.Errorf("the Exchange after a completed one = %q, want the delivered note not delivered again", sent)
	}
	if notes := a.background.takeNotes(); len(notes) != 0 {
		t.Errorf("held notes after a completed Exchange = %q, want none", notes)
	}
}

func TestBackground_AnAbortHoldsAgainTheNoteADrainInterjected(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	up := (&workflowResponder{}).
		route("sweep alpha", signalThenWait(make(chan struct{}), release), finishScript("f1", "alpha is fine")).
		route(wakeUserText, nil, toolCallScript("c1", "read_thing", `{}`)).
		route("try again", nil, contentScript("done"))
	a, log := newPairParent(t, newLockedSink(), up, nil)
	launchBackground(t, a, "pair")
	if err := a.Submit(domain.UserInput{Text: wakeUserText}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res, err := a.Step(context.Background()); err != nil || res.Status != domain.StatusTurnComplete {
		t.Fatalf("first Step = %+v, %v; want a Turn that leaves the Exchange open", res, err)
	}
	close(release)
	a.background.waitAll()
	note, ok := a.TakeWorkflowNotes()
	if !ok {
		t.Fatal("TakeWorkflowNotes found no note after the workflow ended mid-Exchange")
	}
	if err := a.Interject(context.Background(), note); err != nil {
		t.Fatalf("Interject(note): %v", err)
	}

	a.AbortExchange()

	runInput(t, a, domain.UserInput{Text: "try again"})
	assertNoteMessage(t, log.first(t, "try again"))
	if got := noteCount(a); got != 1 {
		t.Errorf("messages carrying the note = %d, want 1: the scrapped interjection's copy is gone", got)
	}
}

func TestBackground_ACancelDuringTheWakeReplyHoldsTheNoteAgain(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	up := (&workflowResponder{}).
		route("sweep alpha", nil, finishScript("f1", "alpha is fine")).
		route(workflowNoteHeader, signalThenWait(started, nil), contentScript("never read")).
		route(workflowNoteHeader, nil, contentScript("noted"))
	a, log := newPairParent(t, newLockedSink(), up, nil)
	launchBackground(t, a, "pair")
	a.background.waitAll()
	if woke, err := a.Wake(context.Background()); err != nil || !woke {
		t.Fatalf("Wake = %v, %v; want it to open an Exchange on the held note", woke, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stepped := make(chan struct{})
	go func() {
		defer close(stepped)
		_, _ = a.Step(ctx)
	}()
	awaitClosed(t, started, "the wake reply's request")
	cancel()
	awaitClosed(t, stepped, "the cancelled Step")

	if dropped := a.SettleExchange(); !dropped {
		t.Fatal("SettleExchange kept the wake's Exchange; want the lone opening scrapped")
	}

	woke, err := a.Wake(context.Background())
	if err != nil || !woke {
		t.Fatalf("Wake after the cancel = %v, %v; want the note held again to wake on", woke, err)
	}
	runToEnd(t, a)
	assertNoteMessage(t, log.first(t, workflowNoteHeader))
	if got := noteCount(a); got != 1 {
		t.Errorf("messages carrying the note = %d, want 1", got)
	}
}

func TestBackground_AStopAllDuringAnOpenExchangeLeavesNoNoteForAnAbortToHoldAgain(t *testing.T) {
	t.Parallel()

	up := (&workflowResponder{}).
		route("sweep alpha", nil, finishScript("f1", "alpha is fine")).
		route(wakeUserText, nil, toolCallScript("c1", "read_thing", `{}`)).
		route("try again", nil, contentScript("done"))
	a, log := newPairParent(t, newLockedSink(), up, nil)
	launchBackground(t, a, "pair")
	a.background.waitAll()
	if err := a.Submit(domain.UserInput{Text: wakeUserText}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res, err := a.Step(context.Background()); err != nil || res.Status != domain.StatusTurnComplete {
		t.Fatalf("first Step = %+v, %v; want a Turn that leaves the Exchange open", res, err)
	}

	a.stopAllBackground()
	a.AbortExchange()

	if notes := a.background.heldNotes(); len(notes) != 0 {
		t.Errorf("held notes after a stop-all then an abort = %q, want none: the stop dropped them", notes)
	}
	runInput(t, a, domain.UserInput{Text: "try again"})
	if sent := log.first(t, "try again"); strings.Contains(sent, workflowNoteHeader) {
		t.Errorf("the Exchange after the abort = %q, want no note from the stopped session", sent)
	}
}

// TestBackground_AnItemChildStoppedThroughTheRootEndsStopped pins the registry a background item
// child is published in: the ROOT Agent's, not the launch-time snapshot's that runs the workflow
// (backgroundHost) — so the root's StopChild reaches it, and it ends stopped while its sibling
// keeps its receipt.
func TestBackground_AnItemChildStoppedThroughTheRootEndsStopped(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, newLockedSink(), sweepRecipe("pair", "alpha", "beta"))
	started := make(chan struct{})
	up := (&workflowResponder{}).
		route("sweep alpha", nil, finishScript("f1", "alpha is fine")).
		route("sweep beta", signalThenWait(started, nil), cancelledScript())
	a := newBackgroundParent(t, cfg, up)
	id := launchBackground(t, a, "pair")
	awaitClosed(t, started, "beta's child")

	var betaRunID string
	for _, child := range a.children.all() {
		if child.workflowItem != nil && child.workflowItem.label == "beta" {
			betaRunID = child.runID
		}
	}
	if betaRunID == "" {
		t.Fatal("the root Agent's registry lists no child for beta")
	}
	if err := a.StopChild(betaRunID); err != nil {
		t.Fatalf("the root's StopChild of beta's child = %v, want nil", err)
	}
	a.background.waitAll()

	phases := itemPhases(workflowInfo(t, a, id))
	if phases["alpha"] != workflow.PhaseDone || phases["beta"] != workflow.PhaseStopped {
		t.Errorf("item phases = %v, want alpha done and beta stopped", phases)
	}
}

func TestBackground_ANamedSubAgentChildIsRetainedOnTheRootAndContinued(t *testing.T) {
	t.Parallel()

	const firstReport = "The survey found three modules."
	sink := &recordingSink{}
	upstream := scriptedResponder(t, contentTurn(firstReport), contentTurn("Counted: forty tests."))
	a := newBackgroundParent(t, workflowConfig(t, sink), upstream)
	built, err := a.buildLaunch(workflowLaunch{
		plan: subAgentWorkflowPlan(`{"task":"survey the repo","name":"surveyor"}`),
		mode: launchModeBackground,
	})
	if err != nil {
		t.Fatalf("buildLaunch: %v", err)
	}

	result, err := built.runner.Run(context.Background(), built.plan)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if built.host == a {
		t.Fatal("the background launch ran off the root, not its launch-time host")
	}
	if receipt := result.Stages[0].Items[0].Receipt; receipt == nil || receipt.Status != workflow.StatusOK {
		t.Fatalf("receipt = %+v, want ok", receipt)
	}
	if names := a.retained.names(); !slices.Equal(names, []string{"surveyor"}) {
		t.Errorf("root retained = %v, want the named background child", names)
	}
	if names := built.host.retained.names(); len(names) != 0 {
		t.Errorf("host retained = %v, want none — the snapshot keeps nothing", names)
	}

	continued, _ := a.runSubAgent(context.Background(), domain.ToolCall{
		ID: "c2", Tool: tools.SubAgentToolName,
		Arguments: json.RawMessage(`{"continue":"surveyor","task":"now count the tests"}`),
	}, a.runIDs.mint())

	if continued.IsError {
		t.Fatalf("continue result = %q, want the continued child's report", continued.Content)
	}
	requests := upstream.requests()
	if len(requests) != 2 {
		t.Fatalf("upstream requests = %d, want the background child's and the continuation's", len(requests))
	}
	seeded := false
	for _, message := range requests[1].Messages {
		seeded = seeded || strings.Contains(message.Content, firstReport)
	}
	if !seeded {
		t.Errorf("continuation request = %+v, want it seeded with the background child's report", requests[1].Messages)
	}
}

// TestBackground_ARetainedSubAgentChildSurvivesAnAbortedExchange pins the background write-through
// (apogee-background-child-exchange-rollback): a named child a background sub_agent item retains
// on the root while the root's Exchange is open is still retained once that Exchange aborts — its
// run rode none of the Exchange's Turns — and a later sub_agent call still continues it by name.
func TestBackground_ARetainedSubAgentChildSurvivesAnAbortedExchange(t *testing.T) {
	t.Parallel()

	upstream := scriptedResponder(t, contentTurn("The survey found three modules."), contentTurn("Counted: forty tests."))
	a := newBackgroundParent(t, workflowConfig(t, &recordingSink{}), upstream)
	built, err := a.buildLaunch(workflowLaunch{
		plan: subAgentWorkflowPlan(`{"task":"survey the repo","name":"surveyor"}`),
		mode: launchModeBackground,
	})
	if err != nil {
		t.Fatalf("buildLaunch: %v", err)
	}
	a.retained.markExchange()
	if _, err := built.runner.Run(context.Background(), built.plan); err != nil {
		t.Fatalf("Run: %v", err)
	}

	a.exchangeAborted()

	if names := a.retained.names(); !slices.Equal(names, []string{"surveyor"}) {
		t.Fatalf("root retained after the abort = %v, want the background child kept", names)
	}
	continued, _ := a.runSubAgent(context.Background(), domain.ToolCall{
		ID: "c2", Tool: tools.SubAgentToolName,
		Arguments: json.RawMessage(`{"continue":"surveyor","task":"now count the tests"}`),
	}, a.runIDs.mint())
	if continued.IsError {
		t.Errorf("continue result = %q, want the continued child's report", continued.Content)
	}
}

// TestBackground_ABlockingWorkflowChildIsRolledBackWithAnAbortedExchange pins the other side of
// the write-through: a blocking workflow's sub_agent item rides the open Exchange's Turn, so the
// child it retained is undone with that Exchange's abort.
func TestBackground_ABlockingWorkflowChildIsRolledBackWithAnAbortedExchange(t *testing.T) {
	t.Parallel()

	upstream := scriptedResponder(t, contentTurn("The survey found three modules."))
	a := newBackgroundParent(t, workflowConfig(t, &recordingSink{}), upstream)
	built, err := a.buildLaunch(workflowLaunch{
		plan: subAgentWorkflowPlan(`{"task":"survey the repo","name":"surveyor"}`),
		mode: launchModeBlocking,
	})
	if err != nil {
		t.Fatalf("buildLaunch: %v", err)
	}
	a.retained.markExchange()
	if _, err := built.runner.Run(context.Background(), built.plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if names := a.retained.names(); !slices.Equal(names, []string{"surveyor"}) {
		t.Fatalf("root retained before the abort = %v, want the blocking child", names)
	}

	a.exchangeAborted()

	if names := a.retained.names(); len(names) != 0 {
		t.Errorf("root retained after the abort = %v, want the blocking child rolled back", names)
	}
}
