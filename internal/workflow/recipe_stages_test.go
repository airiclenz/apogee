package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// scriptFunc adapts a function to the ScriptRunner interface.
type scriptFunc func(ctx context.Context, spec ScriptSpec) (ScriptOutput, error)

// RunScript calls f.
func (f scriptFunc) RunScript(ctx context.Context, spec ScriptSpec) (ScriptOutput, error) {
	return f(ctx, spec)
}

// askFunc adapts a function to the Asker interface.
type askFunc func(ctx context.Context, question Question) (string, error)

// Ask calls f.
func (f askFunc) Ask(ctx context.Context, question Question) (string, error) { return f(ctx, question) }

// okSpawner completes every child with an ok receipt and records the specs it was handed.
func okSpawner() *recordingSpawner {
	return &recordingSpawner{script: func(_ context.Context, spec ItemSpec) (Outcome, error) {
		return Outcome{Ending: EndCompleted, Receipt: okReceipt(spec.Item.Label)}, nil
	}}
}

// specsOfStage returns the recorded specs of the named stage, in spawn order.
func (r *recordingSpawner) specsOfStage(stage string) []ItemSpec {
	r.mu.Lock()
	defer r.mu.Unlock()
	var specs []ItemSpec
	for _, spec := range r.specs {
		if spec.Stage.Name == stage {
			specs = append(specs, spec)
		}
	}
	return specs
}

// specUnits returns each spec's item units, in spawn order.
func specUnits(specs []ItemSpec) [][]string {
	units := make([][]string, len(specs))
	for i, spec := range specs {
		units[i] = spec.Item.Units
	}
	return units
}

func TestPickFromAReceiptListFeedsTheNextFanout(t *testing.T) {
	t.Parallel()
	paths := map[string][]any{"a": {"x.go", "y.go"}, "b": {"y.go", "z.go"}, "c": {"w.go"}}
	spawner := &recordingSpawner{script: func(_ context.Context, spec ItemSpec) (Outcome, error) {
		receipt := okReceipt(spec.Item.Label)
		if spec.Stage.Name == "find" {
			receipt.Fields = map[string]any{"paths": paths[spec.Item.Label]}
		}
		return Outcome{Ending: EndCompleted, Receipt: receipt}, nil
	}}
	runner := newTestRunner(t, spawner)
	runner.Width = 1
	plan := Plan{Name: "audit", Stages: []Stage{
		{Name: "find", Kind: StageFanout, Task: "find in {item}", Over: &ItemSource{List: []string{"a", "b", "c"}}, Returns: ReceiptSpec{"paths": "list"}},
		{Name: "picked", Kind: StagePick, From: "find", Field: "paths", Cap: 3, Batch: 2},
		{Name: "deep", Kind: StageFanout, Task: "look at {item}", Over: &ItemSource{Stage: "picked"}},
	}}

	result := runPlan(t, runner, context.Background(), plan)

	want := [][]string{{"x.go", "y.go"}, {"z.go"}}
	if got := specUnits(spawner.specsOfStage("deep")); !reflect.DeepEqual(got, want) {
		t.Errorf("deep items = %v, want %v (the union, each once, capped at 3, two to a child)", got, want)
	}
	pick := stageNamed(t, result, "picked")
	if pick.Phase != PhaseDone || !strings.Contains(pick.Note, "3 of 4 entries (cap 3) into 2 items") {
		t.Errorf("pick = %s %q, want done noting the cap", pick.Phase, pick.Note)
	}
	if deep := stageNamed(t, result, "deep"); deep.Tally.OK != 2 {
		t.Errorf("deep tally = %+v, want 2 ok", deep.Tally)
	}
}

func TestPickFromAFileInTheWorkflowFolder(t *testing.T) {
	t.Parallel()
	var seen ScriptSpec
	scripts := scriptFunc(func(_ context.Context, spec ScriptSpec) (ScriptOutput, error) {
		seen = spec
		if err := os.WriteFile(filepath.Join(spec.Dir, "parts.txt"), []byte("p1\n\n  p2  \np3\n"), 0o600); err != nil {
			return ScriptOutput{}, err
		}
		return ScriptOutput{Stdout: "parts=3\n"}, nil
	})
	spawner := okSpawner()
	runner := newTestRunner(t, spawner)
	runner.Width = 1
	runner.Scripts = scripts
	plan := Plan{Name: "split", Stages: []Stage{
		{Name: "split", Kind: StageScript, Run: "sh split.sh", Returns: ReceiptSpec{"parts": "int"}},
		{Name: "parts", Kind: StagePick, File: "parts.txt"},
		{Name: "each", Kind: StageFanout, Task: "audit {item}", Over: &ItemSource{Stage: "parts"}},
	}}

	result := runPlan(t, runner, context.Background(), plan)

	if seen.Command != "sh split.sh" || seen.Dir != result.Dir || seen.Stage != "split" || seen.Workflow != result.ID {
		t.Errorf("script spec = %+v, want the command, the stage and the workflow folder %s", seen, result.Dir)
	}
	want := [][]string{{"p1"}, {"p2"}, {"p3"}}
	if got := specUnits(spawner.specsOfStage("each")); !reflect.DeepEqual(got, want) {
		t.Errorf("each items = %v, want the file's non-blank lines %v", got, want)
	}
}

func TestPickOfAMissingFileFailsTheStageAndTheWorkflowGoesOn(t *testing.T) {
	t.Parallel()
	spawner := okSpawner()
	runner := newTestRunner(t, spawner)
	plan := Plan{Name: "split", Stages: []Stage{
		{Name: "parts", Kind: StagePick, File: "parts.txt"},
		{Name: "each", Kind: StageFanout, Task: "audit {item}", Over: &ItemSource{Stage: "parts"}},
	}}

	result := runPlan(t, runner, context.Background(), plan)

	if pick := stageNamed(t, result, "parts"); pick.Phase != PhaseFailed || !strings.Contains(pick.Note, "cannot read parts.txt") {
		t.Errorf("pick = %s %q, want failed saying the file cannot be read", pick.Phase, pick.Note)
	}
	if each := stageNamed(t, result, "each"); each.Phase != PhaseDone || len(each.Items) != 0 || len(spawner.specs) != 0 {
		t.Errorf("each = %s with %d items, %d spawns; want done over no items", each.Phase, len(each.Items), len(spawner.specs))
	}
	if result.Phase != PhaseDone {
		t.Errorf("workflow phase = %s, want done", result.Phase)
	}
}

func TestScriptReadsKeyValueLinesAsReceiptFields(t *testing.T) {
	t.Parallel()
	spec := ReceiptSpec{"parts": "int", "groups": "list", "mode": "fast|slow", "note": "text"}
	cases := []struct {
		name   string
		output ScriptOutput
		want   Receipt
	}{
		{
			name: "every field kind",
			output: ScriptOutput{Stdout: "splitting…\nPARTS=4\ngroups=internal\ngroups = cmd\nmode=slow\nnote=two big files\n" +
				"other=ignored\nsummary=split into 4 parts\n"},
			want: Receipt{Status: StatusOK, Summary: "split into 4 parts", Fields: map[string]any{
				"parts": 4, "groups": []string{"internal", "cmd"}, "mode": "slow", "note": "two big files",
			}},
		},
		{
			name:   "a non-zero exit is blocked",
			output: ScriptOutput{Stdout: "parts=1\n", ExitCode: 3},
			want:   Receipt{Status: StatusBlocked, Summary: "the script exited 3", Fields: map[string]any{"parts": 1}},
		},
		{
			name:   "a missing declared field is blocked",
			output: ScriptOutput{Stdout: "parts=2\nmode=fast\nnote=x\n"},
			want:   Receipt{Status: StatusBlocked, Fields: map[string]any{"parts": 2, "mode": "fast", "note": "x"}},
		},
		{
			name:   "a mistyped value is blocked",
			output: ScriptOutput{Stdout: "parts=many\ngroups=a\nmode=fast\nnote=x\n"},
			want:   Receipt{Status: StatusBlocked, Fields: map[string]any{"parts": "many", "groups": []string{"a"}, "mode": "fast", "note": "x"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := scriptReceipt(tc.output, spec)

			if got.Status != tc.want.Status || !reflect.DeepEqual(got.Fields, tc.want.Fields) {
				t.Errorf("receipt = %+v, want status %s fields %v", got, tc.want.Status, tc.want.Fields)
			}
			if tc.want.Summary != "" && got.Summary != tc.want.Summary {
				t.Errorf("summary = %q, want %q", got.Summary, tc.want.Summary)
			}
			if len(strings.Fields(got.Summary)) == 0 || len(strings.Fields(got.Summary)) > SummaryMaxWords {
				t.Errorf("summary %q is not one short line", got.Summary)
			}
		})
	}
}

func TestScriptThatCannotRunFailsTheStage(t *testing.T) {
	t.Parallel()
	runner := newTestRunner(t, okSpawner())
	runner.Scripts = scriptFunc(func(context.Context, ScriptSpec) (ScriptOutput, error) {
		return ScriptOutput{}, errors.New("approval refused")
	})
	plan := Plan{Name: "s", Stages: []Stage{{Name: "prep", Kind: StageScript, Run: "make gen"}}}

	result := runPlan(t, runner, context.Background(), plan)

	stage := stageNamed(t, result, "prep")
	if stage.Phase != PhaseFailed || len(stage.Items) != 1 {
		t.Fatalf("script stage = %+v, want failed with its one receipt", stage)
	}
	if receipt := stage.Items[0].Receipt; receipt.Status != StatusBlocked || !strings.Contains(receipt.Summary, "approval refused") {
		t.Errorf("receipt = %+v, want blocked saying why", receipt)
	}
	status, err := runner.Store.ReadStatus(result.ID)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if lines := status.Stages[0].Items; len(lines) != 1 || lines[0].Receipt == nil || lines[0].Receipt.Status != StatusBlocked {
		t.Errorf("status.json stage items = %+v, want the blocked receipt inline", lines)
	}
}

// askPlan asks whether to go deep, then runs a fanout only on yes and another only on no.
func askPlan() Plan {
	return Plan{Name: "ask", Stages: []Stage{
		{Name: "scope", Kind: StageAsk, Question: "Go deep?", Options: []string{"yes", "no"}, Default: "no"},
		{Name: "deep", Kind: StageFanout, When: "scope.answer == yes", Task: "deep {item}", Over: &ItemSource{List: []string{"a"}}},
		{Name: "shallow", Kind: StageFanout, When: "scope.answer == no", Task: "shallow {item}", Over: &ItemSource{List: []string{"a"}}},
	}}
}

func TestAskStoresTheAnswerALaterConditionReads(t *testing.T) {
	t.Parallel()
	var (
		mu    sync.Mutex
		asked []Question
	)
	runner := newTestRunner(t, okSpawner())
	runner.Asker = askFunc(func(_ context.Context, question Question) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, question)
		return " yes ", nil
	})

	result := runPlan(t, runner, context.Background(), askPlan())

	if len(asked) != 1 || asked[0].Text != "Go deep?" || asked[0].Default != "no" || !reflect.DeepEqual(asked[0].Options, []string{"yes", "no"}) {
		t.Errorf("asked = %+v, want the question with its options and default once", asked)
	}
	scope := stageNamed(t, result, "scope")
	if scope.Phase != PhaseDone || scope.Note != "" || scope.Items[0].Receipt.Fields[AskAnswerField] != "yes" {
		t.Errorf("ask stage = %+v, want done with answer yes and no note", scope)
	}
	if deep, shallow := stageNamed(t, result, "deep"), stageNamed(t, result, "shallow"); deep.Phase != PhaseDone || shallow.Phase != PhaseSkipped {
		t.Errorf("deep = %s, shallow = %s; want deep run and shallow skipped", deep.Phase, shallow.Phase)
	}
}

func TestAskTakesItsDefault(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		asker    Asker
		wantNote string
	}{
		{"no one to ask", nil, "(default taken: no one to ask)"},
		{"an answer outside the options", askFunc(func(context.Context, Question) (string, error) { return "maybe", nil }), "is not one of the options"},
		{"an empty answer", askFunc(func(context.Context, Question) (string, error) { return "  ", nil }), "no answer"},
		{"a failed question", askFunc(func(context.Context, Question) (string, error) { return "", errors.New("no terminal") }), "could not be asked: no terminal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner := newTestRunner(t, okSpawner())
			runner.Asker = tc.asker

			result := runPlan(t, runner, context.Background(), askPlan())

			scope := stageNamed(t, result, "scope")
			if !strings.Contains(scope.Note, tc.wantNote) || scope.Items[0].Receipt.Fields[AskAnswerField] != "no" {
				t.Errorf("ask stage note %q, receipt %+v; want the default no and a note containing %q", scope.Note, scope.Items[0].Receipt, tc.wantNote)
			}
			if shallow := stageNamed(t, result, "shallow"); shallow.Phase != PhaseDone {
				t.Errorf("shallow = %s, want it run on the default", shallow.Phase)
			}
		})
	}
}

func TestRepeatIsBoundedByMax(t *testing.T) {
	t.Parallel()
	spawner := okSpawner()
	runner := newTestRunner(t, spawner)
	plan := fanPlan("a")
	plan.Stages = append(plan.Stages, Stage{Name: "again", Kind: StageRepeat, Repeat: "find", When: "ok > 0", Max: 3})

	result := runPlan(t, runner, context.Background(), plan)

	if spawns := len(spawner.specsFor("a")); spawns != 4 {
		t.Errorf("item a spawned %d times, want 4 (its own run and 3 rounds)", spawns)
	}
	repeat := stageNamed(t, result, "again")
	if repeat.Phase != PhaseDone || !strings.Contains(repeat.Note, "3 of at most 3 rounds; its condition still held") {
		t.Errorf("repeat = %s %q, want done at the bound", repeat.Phase, repeat.Note)
	}
	if find := stageNamed(t, result, "find"); find.Round != 3 {
		t.Errorf("find round = %d, want its last re-run, 3", find.Round)
	}
}

func TestRepeatStopsWhenItsConditionFails(t *testing.T) {
	t.Parallel()
	var (
		mu    sync.Mutex
		calls int
	)
	spawner := &recordingSpawner{script: func(_ context.Context, spec ItemSpec) (Outcome, error) {
		mu.Lock()
		calls++
		isBlocked := calls <= 2
		mu.Unlock()
		if isBlocked {
			return Outcome{Ending: EndCompleted, Receipt: &Receipt{Status: StatusBlocked, Summary: "flaky"}}, nil
		}
		return Outcome{Ending: EndCompleted, Receipt: okReceipt(spec.Item.Label)}, nil
	}}
	runner := newTestRunner(t, spawner)
	plan := fanPlan("a")
	plan.Stages = append(plan.Stages, Stage{Name: "again", Kind: StageRepeat, Repeat: "find", When: "find.blocked > 0", Max: 5})

	result := runPlan(t, runner, context.Background(), plan)

	if repeat := stageNamed(t, result, "again"); !strings.Contains(repeat.Note, "2 of at most 5 rounds") || strings.Contains(repeat.Note, "still held") {
		t.Errorf("repeat note = %q, want 2 rounds and the condition cleared", repeat.Note)
	}
	if find := stageNamed(t, result, "find"); find.Tally.OK != 1 || find.Round != 2 {
		t.Errorf("find = tally %+v round %d, want the ok round 2", find.Tally, find.Round)
	}
}

func TestRepeatedFanoutRespawnsItsItemsAndResumesEachRound(t *testing.T) {
	t.Parallel()
	spawner := okSpawner()
	runner := newTestRunner(t, spawner)
	plan := fanPlan("a", "b")
	plan.Stages = append(plan.Stages, Stage{Name: "again", Kind: StageRepeat, Repeat: "find", When: "ok > 0", Max: 2})

	first := runPlan(t, runner, context.Background(), plan)

	keys := map[string]bool{}
	for _, spec := range spawner.specsFor("a") {
		keys[spec.Key] = true
	}
	if len(keys) != 3 {
		t.Errorf("item a ran under %d keys, want 3: one per round, so a re-run never finds the round before", len(keys))
	}
	spawned := len(spawner.specs)

	second := runPlan(t, runner, context.Background(), plan)

	if second.ID != first.ID || len(spawner.specs) != spawned {
		t.Errorf("resume spawned %d more children in %s (first %s), want none: every round's receipts are stored",
			len(spawner.specs)-spawned, second.ID, first.ID)
	}
	if find := stageNamed(t, second, "find"); find.Tally.Resumed != 2 || find.Round != 2 {
		t.Errorf("resumed find = tally %+v round %d, want both items resumed in round 2", find.Tally, find.Round)
	}
}

func TestWhenSkipsAStageOnAFanoutsTally(t *testing.T) {
	t.Parallel()
	spawner := &recordingSpawner{script: func(_ context.Context, spec ItemSpec) (Outcome, error) {
		if spec.Stage.Kind == StageMerge {
			if err := os.WriteFile(spec.Output, []byte("# Report\n"), 0o600); err != nil {
				return Outcome{}, err
			}
		}
		return Outcome{Ending: EndCompleted, Receipt: issuesReceipt(spec.Item.Label, 1)}, nil
	}}
	runner := newTestRunner(t, spawner)
	runner.Scripts = scriptFunc(func(context.Context, ScriptSpec) (ScriptOutput, error) {
		t.Error("a skipped script ran")
		return ScriptOutput{}, nil
	})
	plan := countedPlan([]string{"a", "b"},
		Stage{Name: "fix", Kind: StageFanout, When: "find.blocked > 0", Task: "fix {item}", Over: &ItemSource{List: []string{"a"}}},
		Stage{Name: "check", Kind: StageVerify, From: "fix", Task: "recheck"},
		Stage{Name: "lint", Kind: StageScript, When: "find.ok < 2", Run: "make lint"},
		Stage{Name: "report", Kind: StageMerge, From: "find", When: "find.ok >= 2 and not find.partial > 0", Task: "merge"},
	)

	result := runPlan(t, runner, context.Background(), plan)

	for name, wantNote := range map[string]string{
		"fix":   "skipped: find.blocked > 0 is false",
		"check": "skipped: stage fix was skipped",
		"lint":  "skipped: find.ok < 2 is false",
	} {
		if stage := stageNamed(t, result, name); stage.Phase != PhaseSkipped || stage.Note != wantNote {
			t.Errorf("stage %s = %s %q, want skipped %q", name, stage.Phase, stage.Note, wantNote)
		}
	}
	if len(spawner.specsOfStage("fix")) != 0 || len(spawner.specsOfStage("check")) != 0 {
		t.Error("a skipped stage spawned a child")
	}
	if report := stageNamed(t, result, "report"); report.Phase != PhaseDone || result.Report == "" {
		t.Errorf("merge = %s, report %q; want it run on the tally", report.Phase, result.Report)
	}
	status, err := runner.Store.ReadStatus(result.ID)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if line := status.Stages[1]; line.Phase != PhaseSkipped || line.Note == "" {
		t.Errorf("status.json fix line = %+v, want skipped with its note", line)
	}
}
