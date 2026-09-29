package agent

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/workflow"
)

// The workflow child spawner (ADR 0087): each test builds a top-level Agent over a scripted
// upstream and spawns one item child through it directly, the way a running workflow does. The
// child is the only thing that speaks to the upstream, so scripts[N] is the child's N-th request.

// fanOutCall is the call a workflow's children answer under in these tests.
var fanOutCall = domain.ToolCall{ID: "fo1", Tool: "fan_out"}

// finishTurn is a child turn that calls finish with args.
func finishTurn(id, args string) stubllm.Turn {
	return toolCallTurn(id, tools.FinishToolName, args)
}

// itemSpec is a fanout item spec over label whose stage declares returns.
func itemSpec(label string, returns workflow.ReceiptSpec) workflow.ItemSpec {
	return workflow.ItemSpec{
		Workflow: "wf",
		Stage:    workflow.Stage{Name: "find", Kind: workflow.StageFanout, Task: "audit {item}", Returns: returns},
		Item:     workflow.Item{Label: label, Units: []string{label}},
		Key:      "k1",
		Brief:    "audit " + label,
		Output:   "/tmp/out.md",
		Attempt:  1,
	}
}

// newWorkflowParent builds a top-level Agent in mode over turns, with one read-only tool.
func newWorkflowParent(t *testing.T, sink domain.EventSink, mode domain.Mode, edit func(*domain.Config), turns ...stubllm.Turn) (*Agent, *scriptedUpstream) {
	t.Helper()
	cfg := baseConfig(sink)
	cfg.Mode = mode
	reg := domain.NewToolRegistry()
	_ = reg.Register(fakeTool{name: "read_thing", readOnly: true, result: "package main"})
	cfg.Tools = reg
	if edit != nil {
		edit(&cfg)
	}
	upstream := scriptedResponder(t, turns...)
	a, err := newAgent(cfg, upstream)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a, upstream
}

// spawnItem runs spec's child through a's workflow spawner and fails the test on a spawn error.
func spawnItem(t *testing.T, a *Agent, spec workflow.ItemSpec) workflow.Outcome {
	t.Helper()
	outcome, err := a.newWorkflowSpawner(0, fanOutCall, nil).Spawn(context.Background(), spec)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	return outcome
}

func TestWorkflowSpawn_ValidFinishEndsTheChildWithItsReceipt(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a, upstream := newWorkflowParent(t, sink, domain.ModeAskBefore, nil,
		finishTurn("f1", `{"status":"ok","summary":"two findings","count":2}`),
	)

	outcome := spawnItem(t, a, itemSpec("a.go", workflow.ReceiptSpec{"count": "int"}))

	if outcome.Ending != workflow.EndCompleted {
		t.Fatalf("ending = %q, want completed", outcome.Ending)
	}
	if outcome.Receipt == nil || outcome.Receipt.Status != workflow.StatusOK || outcome.Receipt.Summary != "two findings" {
		t.Fatalf("receipt = %+v, want ok / two findings", outcome.Receipt)
	}
	if got := upstream.calls(); got != 1 {
		t.Errorf("upstream requests = %d, want 1 — an accepted finish ends the child", got)
	}
	if !slices.Contains(upstream.requests()[0].Tools, tools.FinishToolName) {
		t.Errorf("child menu = %v, want finish on it", upstream.requests()[0].Tools)
	}
}

func TestWorkflowSpawn_BracketsTheChildWithPhaseEventsUnderTheCall(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a, _ := newWorkflowParent(t, sink, domain.ModeAskBefore, nil,
		finishTurn("f1", `{"status":"ok","summary":"done"}`),
	)

	spawnItem(t, a, itemSpec("a.go", nil))

	var phases []domain.SubAgentPhaseEvent
	for _, event := range sink.events {
		if phase, ok := event.(domain.SubAgentPhaseEvent); ok {
			phases = append(phases, phase)
		}
	}
	if len(phases) != 2 || phases[0].Phase != domain.SubAgentStarted || phases[1].Phase != domain.SubAgentFinished {
		t.Fatalf("phases = %+v, want started then finished", phases)
	}
	for _, phase := range phases {
		if phase.CallID != fanOutCall.ID || phase.RunID == "" || phase.Depth != 1 {
			t.Errorf("phase stamp = call %q run %q depth %d, want the fan_out call, a run id, depth 1",
				phase.CallID, phase.RunID, phase.Depth)
		}
	}
	if phases[0].RunID != phases[1].RunID {
		t.Errorf("run ids differ: %q vs %q", phases[0].RunID, phases[1].RunID)
	}
	if got := phases[1].Result.Content; got != "ok — done" {
		t.Errorf("finished result = %q, want the receipt line", got)
	}
}

func TestWorkflowSpawn_OutOfEnumVerdictIsRefusedThenCorrected(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a, upstream := newWorkflowParent(t, sink, domain.ModeAskBefore, nil,
		finishTurn("f1", `{"status":"ok","summary":"checked","verdict":"maybe"}`),
		finishTurn("f2", `{"status":"ok","summary":"checked","verdict":"confirmed"}`),
	)

	outcome := spawnItem(t, a, itemSpec("a.go", workflow.ReceiptSpec{"verdict": "confirmed|refuted|unclear"}))

	if outcome.Ending != workflow.EndCompleted || outcome.Receipt == nil {
		t.Fatalf("outcome = %+v, want completed with a receipt", outcome)
	}
	if got := outcome.Receipt.Fields["verdict"]; got != "confirmed" {
		t.Errorf("verdict = %v, want the corrected confirmed", got)
	}
	requests := upstream.requests()
	if len(requests) != 2 {
		t.Fatalf("upstream requests = %d, want 2 — the refused finish keeps the child running", len(requests))
	}
	refusal := requests[1].Messages[len(requests[1].Messages)-1]
	if refusal.Role != "tool" || !strings.Contains(refusal.Content, `field "verdict"`) {
		t.Errorf("second request's tail = %+v, want the refusal naming the verdict field", refusal)
	}
}

func TestWorkflowSpawn_CappedChildClosesOnFinishAlone(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a, upstream := newWorkflowParent(t, sink, domain.ModeAskBefore,
		func(cfg *domain.Config) { cfg.Delegation.MaxSteps = 1 },
		narratedToolCallTurn("t0", "read_thing", `{"n":0}`, "reading"),
		contentTurn(childFoldSummary), // the engine fold, made before the closing Turn
		finishTurn("f1", `{"status":"partial","summary":"one file read"}`),
	)

	outcome := spawnItem(t, a, itemSpec("a.go", nil))

	if outcome.Ending != workflow.EndCapped {
		t.Fatalf("ending = %q, want capped", outcome.Ending)
	}
	if outcome.Receipt == nil || outcome.Receipt.Status != workflow.StatusPartial {
		t.Fatalf("receipt = %+v, want the closing Turn's partial receipt", outcome.Receipt)
	}
	if !strings.Contains(outcome.Report, childFoldSummary) {
		t.Errorf("report = %q, want the engine fold", outcome.Report)
	}
	closing := upstream.last()
	if !slices.Equal(closing.Tools, []string{tools.FinishToolName}) {
		t.Errorf("closing Turn menu = %v, want finish alone", closing.Tools)
	}
	var directive bool
	for _, message := range closing.Messages {
		if strings.Contains(message.Content, "no further tool calls are possible but one call to finish") {
			directive = true
		}
	}
	if !directive {
		t.Error("the closing request carries no finish directive")
	}
}

func TestWorkflowSpawn_FinishBlockReplacesTheDelegateReportBlock(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a, upstream := newWorkflowParent(t, sink, domain.ModeAskBefore,
		func(cfg *domain.Config) { cfg.SystemPrompt = "You are a careful engineer." },
		finishTurn("f1", `{"status":"ok","summary":"done"}`),
	)

	spawnItem(t, a, itemSpec("a.go", nil))

	system := upstream.requests()[0].Messages[0]
	if system.Role != "system" || !strings.Contains(system.Content, workflowFinishBlock) {
		t.Fatalf("system message = %q, want the finish block", system.Content)
	}
	if strings.Contains(system.Content, DelegateReportBlock) {
		t.Error("the delegate report block rides beside the finish block; it must be replaced")
	}
}

func TestWorkflowSpawn_FinishRunsInPlanAndAskBefore(t *testing.T) {
	t.Parallel()

	for _, mode := range []domain.Mode{domain.ModePlan, domain.ModeAskBefore} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()

			sink := &recordingSink{}
			// No Approver: a call that gated would be refused, so a receipt proves finish ran.
			a, upstream := newWorkflowParent(t, sink, mode, nil,
				finishTurn("f1", `{"status":"ok","summary":"done"}`),
			)

			outcome := spawnItem(t, a, itemSpec("a.go", nil))

			if outcome.Receipt == nil {
				t.Fatalf("no receipt in %s: finish did not run (outcome %+v)", mode, outcome)
			}
			if !slices.Contains(upstream.requests()[0].Tools, tools.FinishToolName) {
				t.Errorf("%s menu = %v, want finish offered", mode, upstream.requests()[0].Tools)
			}
		})
	}
}

func TestWorkflowSpawn_BooksNoLedgerRowRetentionOrName(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	namer := &stubNamer{reply: "generated name"}
	a, _ := newWorkflowParent(t, sink, domain.ModeAskBefore,
		func(cfg *domain.Config) { cfg.Namer = namer },
		finishTurn("f1", `{"status":"ok","summary":"done"}`),
	)

	spawnItem(t, a, itemSpec("a.go", nil))

	if rows := a.delegations.rows(); len(rows) != 0 {
		t.Errorf("ledger rows = %+v, want none", rows)
	}
	if names := a.retained.names(); len(names) != 0 {
		t.Errorf("retained = %v, want none", names)
	}
	if calls := namer.calls(); len(calls) != 0 {
		t.Errorf("namer calls = %d, want none — the item's label is its name", len(calls))
	}
}

func TestWorkflowSpawn_TranscriptFileIsWritten(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a, _ := newWorkflowParent(t, sink, domain.ModeAskBefore, nil,
		finishTurn("f1", `{"status":"ok","summary":"done"}`),
	)
	store, err := workflow.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	runner := &workflow.Runner{
		Spawner:   a.newWorkflowSpawner(0, fanOutCall, nil),
		Store:     store,
		Workspace: fstest.MapFS{},
	}
	plan := workflow.Plan{Name: "audit", Stages: []workflow.Stage{{
		Name: "find", Kind: workflow.StageFanout, Task: "audit {item} into {out}",
		Over: &workflow.ItemSource{List: []string{"a.go"}},
	}}}

	result, err := runner.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	key := result.Stages[0].Items[0].Key
	data, err := os.ReadFile(filepath.Join(result.Dir, "items", key, "transcript.jsonl"))
	if err != nil {
		t.Fatalf("transcript not written: %v", err)
	}
	if !strings.Contains(string(data), "audit a.go") || !strings.Contains(string(data), tools.FinishToolName) {
		t.Errorf("transcript = %s, want the task and the finish call", data)
	}
}

// observedRun runs plan through a Runner over a's workflow spawner, with the given second chances,
// observed as a fan_out's Workflow is, and returns the phases sink recorded.
func observedRun(t *testing.T, a *Agent, sink *recordingSink, plan workflow.Plan, retries, continuations int) []domain.WorkflowPhaseEvent {
	t.Helper()
	store, err := workflow.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	runner := &workflow.Runner{
		Spawner:       a.newWorkflowSpawner(0, fanOutCall, nil),
		Store:         store,
		Workspace:     fstest.MapFS{},
		Retries:       retries,
		Continuations: continuations,
	}
	observer := a.observeWorkflow(runner, 0, plan)
	observer.call = fanOutCall.ID
	result, err := runner.Run(context.Background(), plan)
	observer.end(result, err)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return workflowPhaseEvents(sink.events)
}

// phasesAt keeps the phases whose Phase is one of wanted, in order.
func phasesAt(phases []domain.WorkflowPhaseEvent, wanted ...domain.WorkflowPhase) []domain.WorkflowPhaseEvent {
	var kept []domain.WorkflowPhaseEvent
	for _, phase := range phases {
		if slices.Contains(wanted, phase.Phase) {
			kept = append(kept, phase)
		}
	}
	return kept
}

// fanoutPlan is a one-stage plan auditing items.
func fanoutPlan(items ...string) workflow.Plan {
	return workflow.Plan{Name: "audit", Stages: []workflow.Stage{{
		Name: "find", Kind: workflow.StageFanout, Task: "audit {item}",
		Over: &workflow.ItemSource{List: items},
	}}}
}

func TestWorkflowSpawn_ARepeatedStageReportsEachRound(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a, _ := newWorkflowParent(t, sink, domain.ModeAskBefore, nil,
		finishTurn("f1", `{"status":"ok","summary":"first round"}`),
		finishTurn("f2", `{"status":"ok","summary":"second round"}`),
	)
	plan := fanoutPlan("a.go")
	plan.Stages = append(plan.Stages, workflow.Stage{Name: "again", Kind: workflow.StageRepeat, Repeat: "find", When: "ok > 0", Max: 1})

	phases := observedRun(t, a, sink, plan, 0, 0)

	type stagePhase struct {
		phase  domain.WorkflowPhase
		stage  string
		round  int
		rounds int
	}
	var stages []stagePhase
	for _, phase := range phasesAt(phases, domain.WorkflowStageStarted, domain.WorkflowStageFinished) {
		stages = append(stages, stagePhase{phase.Phase, phase.Stage, phase.Round, phase.Rounds})
	}
	want := []stagePhase{
		{domain.WorkflowStageStarted, "find", 1, 2},
		{domain.WorkflowStageFinished, "find", 1, 0},
		{domain.WorkflowStageStarted, "again", 1, 0},
		{domain.WorkflowStageStarted, "find", 2, 2},
		{domain.WorkflowStageFinished, "find", 2, 0},
		{domain.WorkflowStageFinished, "again", 1, 0},
	}
	if !slices.Equal(stages, want) {
		t.Fatalf("stage phases = %+v, want %+v", stages, want)
	}
	items := phasesAt(phases, domain.WorkflowItemStarted, domain.WorkflowItemFinished)
	if len(items) != 4 {
		t.Fatalf("item phases = %v, want a started and a finished per round", phaseNames(items))
	}
	for round := 1; round <= 2; round++ {
		started, finished := items[2*(round-1)], items[2*(round-1)+1]
		if started.Round != round || started.Attempt != 1 || finished.Round != round || finished.Run != started.Run {
			t.Errorf("round %d: started %+v, finished %+v; want round %d, attempt 1, the same run", round, started, finished, round)
		}
	}
	if items[0].Run == items[2].Run {
		t.Errorf("both rounds ran as %q, want a run of each round's own", items[0].Run)
	}
}

// TestWorkflowSpawn_EachStageFinishesWithItsOutcome pins the outcome a stage's finished phase
// carries, which a Driver paints its row from: a fanout that ran its item ends done, a merge whose
// child wrote no report ends failed, and a stage its `when:` skipped — which never started — ends
// skipped.
func TestWorkflowSpawn_EachStageFinishesWithItsOutcome(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a, _ := newWorkflowParent(t, sink, domain.ModeAskBefore, nil,
		finishTurn("f1", `{"status":"ok","summary":"audited"}`),
		finishTurn("f2", `{"status":"ok","summary":"merged, but wrote nothing"}`),
	)
	plan := fanoutPlan("a.go")
	plan.Stages = append(plan.Stages,
		workflow.Stage{Name: "report", Kind: workflow.StageMerge, Task: "merge the findings into {out}"},
		workflow.Stage{
			Name: "recheck", Kind: workflow.StageFanout, Task: "recheck {item}", When: "find.blocked > 0",
			Over: &workflow.ItemSource{List: []string{"a.go"}},
		},
	)

	phases := observedRun(t, a, sink, plan, 0, 0)

	type stageEnd struct {
		stage   string
		outcome domain.WorkflowStageOutcome
	}
	var ends []stageEnd
	for _, phase := range phasesAt(phases, domain.WorkflowStageFinished) {
		ends = append(ends, stageEnd{phase.Stage, phase.Outcome})
	}
	want := []stageEnd{
		{"find", domain.WorkflowStageDone},
		{"report", domain.WorkflowStageFailed},
		{"recheck", domain.WorkflowStageSkipped},
	}
	if !slices.Equal(ends, want) {
		t.Fatalf("stage finishes = %+v, want %+v", ends, want)
	}
	for _, phase := range phasesAt(phases, domain.WorkflowStageStarted) {
		if phase.Stage == "recheck" {
			t.Errorf("skipped stage reported %+v, want no start", phase)
		}
		if phase.Outcome != "" {
			t.Errorf("stage_started %+v carries an outcome, want it on stage_finished only", phase)
		}
	}
}

func TestWorkflowSpawn_ARetriedItemReportsEachAttempt(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a, _ := newWorkflowParent(t, sink, domain.ModeAskBefore, nil,
		contentTurn("I looked around"), // ends without a receipt: the item is retried
		finishTurn("f1", `{"status":"ok","summary":"done"}`),
	)

	phases := observedRun(t, a, sink, fanoutPlan("a.go"), 1, 0)

	assertAttempts(t, phases, "ok")
}

func TestWorkflowSpawn_AContinuedItemReportsEachRun(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a, _ := newWorkflowParent(t, sink, domain.ModeAskBefore,
		func(cfg *domain.Config) { cfg.Delegation.MaxSteps = 1 },
		narratedToolCallTurn("t0", "read_thing", `{"n":0}`, "reading"),
		contentTurn(childFoldSummary),
		finishTurn("f1", `{"status":"partial","summary":"one file read"}`),
		finishTurn("f2", `{"status":"ok","summary":"done"}`),
	)

	phases := observedRun(t, a, sink, fanoutPlan("a.go"), 0, 1)

	assertAttempts(t, phases, "ok")
}

// assertAttempts checks an item that ran twice: two item started phases on attempts 1 and 2, each
// with a run of its own, and one item finished on status naming the second run.
func assertAttempts(t *testing.T, phases []domain.WorkflowPhaseEvent, status string) {
	t.Helper()
	started := phasesAt(phases, domain.WorkflowItemStarted)
	finished := phasesAt(phases, domain.WorkflowItemFinished)
	if len(started) != 2 || len(finished) != 1 {
		t.Fatalf("item phases = %v, want two started and one finished", phaseNames(phasesAt(phases, domain.WorkflowItemStarted, domain.WorkflowItemFinished)))
	}
	if started[0].Attempt != 1 || started[1].Attempt != 2 || started[0].Run == "" || started[0].Run == started[1].Run {
		t.Errorf("item started = %+v, %+v; want attempts 1 and 2, each on a run of its own", started[0], started[1])
	}
	if finished[0].Run != started[1].Run || finished[0].Receipt.Status != status {
		t.Errorf("item finished = %+v, want the second run %q on status %s", finished[0], started[1].Run, status)
	}
}

func TestWorkflowSpawn_ItemsSharingAKeyEachNameTheirOwnRun(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a, _ := newWorkflowParent(t, sink, domain.ModeAskBefore, nil,
		finishTurn("f1", `{"status":"ok","summary":"first"}`),
		finishTurn("f2", `{"status":"ok","summary":"second"}`),
	)

	phases := observedRun(t, a, sink, fanoutPlan("a.go", "a.go"), 0, 0)

	runs := map[int]string{}
	for _, phase := range phasesAt(phases, domain.WorkflowItemStarted) {
		runs[phase.Index] = phase.Run
	}
	if len(runs) != 2 || runs[0] == runs[1] {
		t.Fatalf("item runs = %v, want one run for each of the two items", runs)
	}
	finished := phasesAt(phases, domain.WorkflowItemFinished)
	if len(finished) != 2 {
		t.Fatalf("item finished phases = %d, want 2", len(finished))
	}
	for _, phase := range finished {
		if phase.Run != runs[phase.Index] {
			t.Errorf("item #%d finished naming run %q, want its own run %q", phase.Index, phase.Run, runs[phase.Index])
		}
	}
}

func TestWorkflowSpawn_ASpawnRefusedBeforeItsRunReportsNoStart(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a, _ := newWorkflowParent(t, sink, domain.ModeAskBefore, nil)
	a.depth = a.maxDepth()

	phases := observedRun(t, a, sink, fanoutPlan("a.go"), 0, 0)

	if started := phasesAt(phases, domain.WorkflowItemStarted); len(started) != 0 {
		t.Errorf("item started phases = %+v, want none for a child that was never built", started)
	}
	finished := phasesAt(phases, domain.WorkflowItemFinished)
	if len(finished) != 1 || finished[0].Run != "" || finished[0].Receipt.Status != "blocked" {
		t.Errorf("item finished phases = %+v, want one blocked item naming no run", finished)
	}
}

// TestWorkflowSpawn_AnItemThatFellBackFromTheSubAgentsServerSaysSo drives a fan_out whose
// `run_on` asks for the Sub-agent server (ADR 0069 decision 9). With nothing latched every item
// child runs on the session server, and each item's finished phase carries SeatFallbackNote as its
// last body line while the call's answer — what the model reads — carries it once, last. With a
// target latched the ask is honoured and neither carries it.
func TestWorkflowSpawn_AnItemThatFellBackFromTheSubAgentsServerSaysSo(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		latched  bool
		wantNote bool
	}{
		{"nothing latched falls back to the session server and says so", false, true},
		{"a latched target runs the items where asked and adds no note", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink := &recordingSink{}
			cfg := withSeatChoiceFanOut(workflowConfig(t, sink), false)
			items := []string{"alpha", "beta"}
			up := (&workflowResponder{}).
				route("please fan out", nil, toolCallScript("fo1", tools.FanOutToolName,
					seatArgsJSON(tools.RunOnSubAgentsServer, false, items...))).
				route("please fan out", nil, contentScript("all done"))
			grunt := &workflowResponder{}
			for _, item := range items {
				up.route("check "+item, nil, finishScript("s-"+item, item+" is fine"))
				grunt.route("check "+item, nil, finishScript("g-"+item, item+" is fine"))
			}
			a, err := newAgent(cfg, up)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			if tc.latched {
				a.dial = dialerTo(grunt).dial
				a.SetDelegationTarget(gruntTarget(gruntEndpoint, 2))
			}

			runSubmitted(t, context.Background(), a, "please fan out")

			var finished []domain.SubAgentPhaseEvent
			for _, event := range sink.events {
				if phase, ok := event.(domain.SubAgentPhaseEvent); ok && phase.Phase == domain.SubAgentFinished {
					finished = append(finished, phase)
				}
			}
			if len(finished) != len(items) {
				t.Fatalf("finished phases = %d, want one per item (%d)", len(finished), len(items))
			}
			for _, phase := range finished {
				content := phase.Result.Content
				if got := strings.HasSuffix(content, "\n"+SeatFallbackNote); got != tc.wantNote {
					t.Errorf("item result %q ends on the seat-fallback note: %v, want %v", content, got, tc.wantNote)
				}
				if !strings.HasPrefix(content, "ok — ") {
					t.Errorf("item result %q, want the receipt line first", content)
				}
			}
			answer := callResult(t, sink.events, "fo1")
			if answer.IsError {
				t.Fatalf("fan_out result is an error: %q", answer.Content)
			}
			if got := strings.Count(answer.Content, SeatFallbackNote); got != boolCount(tc.wantNote) {
				t.Errorf("answer carries the seat-fallback note %d times, want %d:\n%s", got, boolCount(tc.wantNote), answer.Content)
			}
			if tc.wantNote && !strings.HasSuffix(answer.Content, "\n"+SeatFallbackNote) {
				t.Errorf("answer does not end on the seat-fallback note:\n%s", answer.Content)
			}
		})
	}
}

// boolCount is 1 for true and 0 for false.
func boolCount(b bool) int {
	if b {
		return 1
	}
	return 0
}
