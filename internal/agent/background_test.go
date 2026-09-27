package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
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
	if got := a.background.entries(); len(got) != 1 || got[0] != incoming[0] {
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
