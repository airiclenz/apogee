package agent

import (
	"context"
	"encoding/json"
	"maps"
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
