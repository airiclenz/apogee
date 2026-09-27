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
