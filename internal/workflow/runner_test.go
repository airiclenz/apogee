package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// spawnFunc adapts a function to the Spawner interface.
type spawnFunc func(ctx context.Context, spec ItemSpec) (Outcome, error)

// Spawn calls f.
func (f spawnFunc) Spawn(ctx context.Context, spec ItemSpec) (Outcome, error) { return f(ctx, spec) }

// recordingSpawner scripts a child per spec and records every spec it was handed.
type recordingSpawner struct {
	mu     sync.Mutex
	specs  []ItemSpec
	script func(ctx context.Context, spec ItemSpec) (Outcome, error)
}

// Spawn records spec and runs the script.
func (r *recordingSpawner) Spawn(ctx context.Context, spec ItemSpec) (Outcome, error) {
	r.mu.Lock()
	r.specs = append(r.specs, spec)
	r.mu.Unlock()
	return r.script(ctx, spec)
}

// specsFor returns the specs recorded for the item labelled label, in spawn order.
func (r *recordingSpawner) specsFor(label string) []ItemSpec {
	r.mu.Lock()
	defer r.mu.Unlock()
	var specs []ItemSpec
	for _, spec := range r.specs {
		if spec.Item.Label == label {
			specs = append(specs, spec)
		}
	}
	return specs
}

// okReceipt is an ok receipt summarising label.
func okReceipt(label string) *Receipt {
	return &Receipt{Status: StatusOK, Summary: "checked " + label}
}

// fanPlan is a one-fanout plan over a literal list.
func fanPlan(items ...string) Plan {
	return Plan{Name: "audit", Stages: []Stage{{
		Name: "find", Kind: StageFanout, Task: "audit {item} into {out}",
		Over: &ItemSource{List: items},
	}}}
}

// newTestRunner returns a runner over a fresh store and an empty workspace.
func newTestRunner(t *testing.T, spawner Spawner) *Runner {
	t.Helper()
	return &Runner{
		Spawner: spawner, Store: newTestStore(t), Workspace: fstest.MapFS{},
		Width: 2, Now: func() time.Time { return storeClock },
	}
}

// runPlan runs plan and fails the test on error.
func runPlan(t *testing.T, runner *Runner, ctx context.Context, plan Plan) Result {
	t.Helper()
	result, err := runner.Run(ctx, plan)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return result
}

// onlyStage returns the result's single stage.
func onlyStage(t *testing.T, result Result) StageResult {
	t.Helper()
	if len(result.Stages) != 1 {
		t.Fatalf("stages = %d, want 1", len(result.Stages))
	}
	return result.Stages[0]
}

func TestRunnerWavesNeverExceedWidth(t *testing.T) {
	t.Parallel()
	var inFlight, peak atomic.Int32
	spawner := spawnFunc(func(_ context.Context, spec ItemSpec) (Outcome, error) {
		now := inFlight.Add(1)
		for {
			seen := peak.Load()
			if now <= seen || peak.CompareAndSwap(seen, now) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		inFlight.Add(-1)
		return Outcome{Ending: EndCompleted, Receipt: okReceipt(spec.Item.Label)}, nil
	})
	runner := newTestRunner(t, spawner)
	runner.Width = 3

	result := runPlan(t, runner, context.Background(), fanPlan("a", "b", "c", "d", "e", "f", "g"))

	if got := peak.Load(); got > 3 {
		t.Errorf("peak concurrent children = %d, want at most Width 3", got)
	}
	stage := onlyStage(t, result)
	if stage.Tally.OK != 7 || result.Phase != PhaseDone {
		t.Errorf("tally = %+v, phase = %s; want 7 ok and done", stage.Tally, result.Phase)
	}
	for index, item := range stage.Items {
		if want := string(rune('a' + index)); item.Label != want {
			t.Errorf("item %d label = %q, want %q (item order kept)", index, item.Label, want)
		}
	}
}

func TestRunnerSkipsFinishedItemsOnResume(t *testing.T) {
	t.Parallel()
	statusOf := map[string]Status{"a": StatusOK, "b": StatusPartial, "c": StatusBlocked}
	spawner := &recordingSpawner{script: func(_ context.Context, spec ItemSpec) (Outcome, error) {
		return Outcome{Ending: EndCompleted, Receipt: &Receipt{Status: statusOf[spec.Item.Label], Summary: "done"}}, nil
	}}
	runner := newTestRunner(t, spawner)
	plan := fanPlan("a", "b", "c")

	first := runPlan(t, runner, context.Background(), plan)
	second := runPlan(t, runner, context.Background(), plan)

	if second.ID != first.ID {
		t.Errorf("re-issue ran in %q, want the first run's folder %q", second.ID, first.ID)
	}
	for label, want := range map[string]int{"a": 1, "b": 1, "c": 2} {
		if got := len(spawner.specsFor(label)); got != want {
			t.Errorf("item %q spawned %d times over two runs, want %d", label, got, want)
		}
	}
	stage := onlyStage(t, second)
	if stage.Tally.Resumed != 2 || !stage.Items[0].Resumed || !stage.Items[1].Resumed || stage.Items[2].Resumed {
		t.Errorf("resumed = %d (%v %v %v), want a and b resumed, c re-run", stage.Tally.Resumed,
			stage.Items[0].Resumed, stage.Items[1].Resumed, stage.Items[2].Resumed)
	}
	if stage.Tally.OK != 1 || stage.Tally.Partial != 1 || stage.Tally.Blocked != 1 {
		t.Errorf("tally = %+v, want 1 ok, 1 partial, 1 blocked", stage.Tally)
	}
}

func TestRunnerRecordsTheRecipeInStatus(t *testing.T) {
	t.Parallel()
	spawner := &recordingSpawner{script: func(_ context.Context, spec ItemSpec) (Outcome, error) {
		return Outcome{Ending: EndCompleted, Receipt: okReceipt(spec.Item.Label)}, nil
	}}
	runner := newTestRunner(t, spawner)
	runner.Recipe = "audit"

	result := runPlan(t, runner, context.Background(), fanPlan("a"))

	status, err := runner.Store.ReadStatus(result.ID)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if status.Recipe != "audit" {
		t.Errorf("status.json recipe = %q, want the runner's %q", status.Recipe, "audit")
	}
}

func TestRunnerRetriesThenBlocked(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		outcome     Outcome
		err         error
		wantSummary string
	}{
		{"faulted", Outcome{Ending: EndFaulted, Report: "provider fault: 500\nstack"}, nil, "faulted: provider fault: 500"},
		{"spawn error", Outcome{}, errors.New("spawn refused"), "faulted: spawn refused"},
		{"no receipt", Outcome{Ending: EndCompleted}, nil, "without calling finish"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spawner := &recordingSpawner{script: func(context.Context, ItemSpec) (Outcome, error) {
				return tc.outcome, tc.err
			}}
			runner := newTestRunner(t, spawner)
			runner.Retries, runner.Continuations = 2, 3

			stage := onlyStage(t, runPlan(t, runner, context.Background(), fanPlan("a")))

			specs := spawner.specsFor("a")
			if len(specs) != 3 {
				t.Fatalf("spawns = %d, want 1 + 2 retries", len(specs))
			}
			for index, spec := range specs {
				if spec.Attempt != index+1 || len(spec.Prior) != 0 {
					t.Errorf("spawn %d: attempt %d with %d prior rounds, want attempt %d fresh",
						index, spec.Attempt, len(spec.Prior), index+1)
				}
			}
			item := stage.Items[0]
			if item.Receipt == nil || item.Receipt.Status != StatusBlocked || !strings.Contains(item.Receipt.Summary, tc.wantSummary) {
				t.Fatalf("receipt = %+v, want blocked mentioning %q", item.Receipt, tc.wantSummary)
			}
			if problems := (ReceiptSpec{}).Check(*item.Receipt); len(problems) > 0 {
				t.Errorf("the runner's blocked receipt is malformed: %v", problems)
			}
			stored, found, err := runner.Store.ReadReceipt(onlyWorkflowID(t, runner), item.Key)
			if err != nil || !found || stored.Status != StatusBlocked {
				t.Errorf("stored receipt = %+v found=%v err=%v, want the blocked receipt", stored, found, err)
			}
		})
	}
}

// onlyWorkflowID returns the id of the one workflow in runner's store.
func onlyWorkflowID(t *testing.T, runner *Runner) string {
	t.Helper()
	status, found, err := runner.Store.Find(mustHash(t, fanPlan("a"), []Item{{Label: "a", Units: []string{"a"}}}))
	if err != nil || !found {
		t.Fatalf("Find: found=%v err=%v", found, err)
	}
	return status.ID
}

// mustHash is PlanHash with no inputs, failing the test on error.
func mustHash(t *testing.T, plan Plan, items []Item) string {
	t.Helper()
	hash, err := PlanHash(plan, nil, items)
	if err != nil {
		t.Fatalf("PlanHash: %v", err)
	}
	return hash
}

func TestRunnerContinuesACappedChild(t *testing.T) {
	t.Parallel()
	spawner := &recordingSpawner{script: func(_ context.Context, spec ItemSpec) (Outcome, error) {
		round := len(spec.Prior) + 1
		if round <= 2 {
			return Outcome{
				Ending:  EndCapped,
				Receipt: &Receipt{Status: StatusPartial, Summary: "half"},
				Report:  "round " + string(rune('0'+round)),
			}, nil
		}
		return Outcome{Ending: EndCompleted, Receipt: okReceipt(spec.Item.Label)}, nil
	}}
	runner := newTestRunner(t, spawner)
	runner.Retries, runner.Continuations = 1, 2

	stage := onlyStage(t, runPlan(t, runner, context.Background(), fanPlan("a")))

	specs := spawner.specsFor("a")
	if len(specs) != 3 {
		t.Fatalf("spawns = %d, want 1 + 2 continuations", len(specs))
	}
	for index, spec := range specs {
		if spec.Attempt != 1 || len(spec.Prior) != index {
			t.Errorf("spawn %d: attempt %d with %d prior rounds, want attempt 1 with %d", index, spec.Attempt, len(spec.Prior), index)
		}
		for round, prior := range spec.Prior {
			if want := "round " + string(rune('1'+round)); prior.Report != want || prior.Receipt == nil || prior.Output != spec.Output {
				t.Errorf("spawn %d prior[%d] = %+v, want report %q, the partial receipt and the output path", index, round, prior, want)
			}
		}
	}
	item := stage.Items[0]
	if item.Continuations != 2 || item.Attempts != 1 || item.Receipt.Status != StatusOK {
		t.Errorf("item = %+v, want 2 continuations in 1 attempt ending ok", item)
	}
}

func TestRunnerCappedPastContinuationsFallsToRetries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		receipt    *Receipt
		wantStatus Status
	}{
		{"without a receipt", nil, StatusBlocked},
		{"with a partial receipt", &Receipt{Status: StatusPartial, Summary: "got halfway"}, StatusPartial},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spawner := &recordingSpawner{script: func(context.Context, ItemSpec) (Outcome, error) {
				return Outcome{Ending: EndCapped, Receipt: tc.receipt, Report: "ran out"}, nil
			}}
			runner := newTestRunner(t, spawner)
			runner.Retries, runner.Continuations = 1, 1

			stage := onlyStage(t, runPlan(t, runner, context.Background(), fanPlan("a")))

			var got []string
			for _, spec := range spawner.specsFor("a") {
				got = append(got, string(rune('0'+spec.Attempt))+"/"+string(rune('0'+len(spec.Prior))))
			}
			if want := "1/0 1/1 2/0 2/1"; strings.Join(got, " ") != want {
				t.Errorf("spawns (attempt/prior rounds) = %v, want %s", got, want)
			}
			if receipt := stage.Items[0].Receipt; receipt == nil || receipt.Status != tc.wantStatus {
				t.Errorf("receipt = %+v, want status %s", receipt, tc.wantStatus)
			}
		})
	}
}

func TestRunnerCancelMidWaveKeepsFinishedReceipts(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bStarted := make(chan struct{})
	spawner := spawnFunc(func(ctx context.Context, spec ItemSpec) (Outcome, error) {
		switch spec.Item.Label {
		case "a":
			return Outcome{Ending: EndCompleted, Receipt: okReceipt("a")}, nil
		case "b":
			close(bStarted)
		case "c":
			// c takes a's freed slot, so a has finished; with b running too, the cancel lands
			// mid-wave.
			<-bStarted
			cancel()
		}
		<-ctx.Done()
		return Outcome{Ending: EndStopped}, nil
	})
	runner := newTestRunner(t, spawner)
	runner.Retries = 3

	result := runPlan(t, runner, ctx, fanPlan("a", "b", "c", "d"))

	if !result.Stopped() {
		t.Fatalf("phase = %s, want stopped", result.Phase)
	}
	stage := onlyStage(t, result)
	if stage.Tally.OK != 1 || stage.Tally.Unfinished != 3 {
		t.Errorf("tally = %+v, want 1 ok and 3 unfinished", stage.Tally)
	}
	wantPhases := []Phase{PhaseDone, PhaseStopped, PhaseStopped, PhasePending}
	for index, item := range stage.Items {
		if item.Phase != wantPhases[index] {
			t.Errorf("item %q phase = %s, want %s", item.Label, item.Phase, wantPhases[index])
		}
		_, found, err := runner.Store.ReadReceipt(result.ID, item.Key)
		if err != nil || found != (index == 0) {
			t.Errorf("item %q stored receipt found=%v err=%v, want found only for the finished item", item.Label, found, err)
		}
	}
	status, err := runner.Store.ReadStatus(result.ID)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if status.Phase != PhaseStopped || status.Stages[0].Phase != PhaseStopped || status.Stages[0].Items[0].Receipt == nil {
		t.Errorf("status.json = %+v, want the workflow and stage stopped with a's receipt kept", status)
	}
}

// phaseLog is an Observer that records every phase it is told of.
type phaseLog struct {
	stages []Phase
	items  []Phase
}

func (l *phaseLog) StagePhase(event StageEvent) { l.stages = append(l.stages, event.Phase) }
func (l *phaseLog) ItemPhase(event ItemEvent)   { l.items = append(l.items, event.Phase) }

func TestRunnerHandsTheChildItsBriefOutputAndTranscriptHome(t *testing.T) {
	t.Parallel()
	transcript := []domain.Message{{Role: domain.RoleUser, Content: "audit a"}}
	spawner := &recordingSpawner{script: func(_ context.Context, spec ItemSpec) (Outcome, error) {
		return Outcome{Ending: EndCompleted, Receipt: okReceipt(spec.Item.Label), Transcript: transcript}, nil
	}}
	runner := newTestRunner(t, spawner)
	log := &phaseLog{}
	runner.Observer = log

	result := runPlan(t, runner, context.Background(), fanPlan("a"))

	spec := spawner.specsFor("a")[0]
	wantOutput := filepath.Join(result.Dir, "items", spec.Key, "output.md")
	if spec.Output != wantOutput || spec.Workflow != result.ID {
		t.Errorf("spec output = %q in %q, want %q in %q", spec.Output, spec.Workflow, wantOutput, result.ID)
	}
	if want := "audit a into " + wantOutput; spec.Brief != want {
		t.Errorf("brief = %q, want %q", spec.Brief, want)
	}
	if _, err := filepath.Rel(result.Dir, wantOutput); err != nil {
		t.Errorf("output path is not in the workflow folder: %v", err)
	}
	if got := strings.Join(phaseStrings(log.stages), " "); got != "running done" {
		t.Errorf("stage phases = %s, want running done", got)
	}
	if got := strings.Join(phaseStrings(log.items), " "); got != "pending running done" {
		t.Errorf("item phases = %s, want pending running done", got)
	}
	status, err := runner.Store.ReadStatus(result.ID)
	if err != nil || status.Phase != PhaseDone || status.Stages[0].Items[0].Phase != PhaseDone {
		t.Errorf("status.json = %+v err=%v, want the workflow and item done", status, err)
	}
}

// phaseStrings spells phases for a one-line comparison.
func phaseStrings(phases []Phase) []string {
	spelled := make([]string, len(phases))
	for i, phase := range phases {
		spelled[i] = string(phase)
	}
	return spelled
}

func TestRunnerRefusesWhatItCannotRunYet(t *testing.T) {
	t.Parallel()
	spawner := spawnFunc(func(context.Context, ItemSpec) (Outcome, error) {
		t.Error("a refused plan spawned a child")
		return Outcome{}, nil
	})
	withScript := fanPlan("a")
	withScript.Stages = append(withScript.Stages, Stage{Name: "check", Kind: StageScript, Run: "make test"})
	cases := map[string]Plan{
		"invalid plan":                {Name: "empty"},
		"script with no ScriptRunner": withScript,
		"empty item source":           {Name: "x", Stages: []Stage{{Name: "f", Kind: StageFanout, Task: "t", Over: &ItemSource{Files: "none/*.go"}}}},
	}
	for name, plan := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runner := newTestRunner(t, spawner)
			if _, err := runner.Run(context.Background(), plan); err == nil {
				t.Errorf("Run err = nil, want a refusal")
			}
		})
	}
}
