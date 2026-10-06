package workflow

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// countedPlan is a fanout over items whose receipts carry an `issues` count, followed by extra.
func countedPlan(items []string, extra ...Stage) Plan {
	stages := []Stage{{
		Name: "find", Kind: StageFanout, Task: "audit {item} into {out}",
		Over: &ItemSource{List: items}, Returns: ReceiptSpec{"issues": "int"},
	}}
	return Plan{Name: "audit", Stages: append(stages, extra...)}
}

// issuesReceipt is an ok receipt reporting count issues for label.
func issuesReceipt(label string, count int) *Receipt {
	return &Receipt{Status: StatusOK, Summary: "audited " + label, Fields: map[string]any{"issues": count}}
}

// stageNamed returns the result's stage called name.
func stageNamed(t *testing.T, result Result, name string) StageResult {
	t.Helper()
	for _, stage := range result.Stages {
		if stage.Name == name {
			return stage
		}
	}
	t.Fatalf("result has no stage %q", name)
	return StageResult{}
}

func TestVerifyChecksOnlyTheItemsItsConditionSelects(t *testing.T) {
	t.Parallel()
	issues := map[string]int{"a": 0, "b": 2, "c": 5}
	spawner := &recordingSpawner{script: func(_ context.Context, spec ItemSpec) (Outcome, error) {
		if spec.Stage.Kind == StageVerify {
			receipt := &Receipt{Status: StatusOK, Summary: "checked", Fields: map[string]any{VerdictField: "confirmed"}}
			return Outcome{Ending: EndCompleted, Receipt: receipt}, nil
		}
		return Outcome{Ending: EndCompleted, Receipt: issuesReceipt(spec.Item.Label, issues[spec.Item.Label])}, nil
	}}
	runner := newTestRunner(t, spawner)
	verify := Stage{Name: "check", Kind: StageVerify, When: "issues > 0", Task: "focus on {item}"}

	result := runPlan(t, runner, context.Background(), countedPlan([]string{"a", "b", "c"}, verify))

	var verified []string
	for _, spec := range spawner.specs {
		if spec.Stage.Kind == StageVerify {
			verified = append(verified, spec.Item.Label)
		}
	}
	if len(verified) != 2 || strings.Join(slices.Sorted(slices.Values(verified)), ",") != "b,c" {
		t.Fatalf("verified %v, want only b and c (issues > 0)", verified)
	}
	spec := spawner.specsFor("c")[1]
	if !reflect.DeepEqual(spec.Stage.Returns, verifyReturns()) {
		t.Errorf("verify child's returns = %v, want the engine's verdict field", spec.Stage.Returns)
	}
	lead, own, found := strings.Cut(spec.Brief, "\n\n")
	if !found || !strings.Contains(lead, "refute") || !strings.Contains(lead, "issues: 5") || own != "focus on c" {
		t.Errorf("verify brief = %q, want the engine's refute brief with the claim, then the stage's own", spec.Brief)
	}
	find := stageNamed(t, result, "find")
	if find.Items[0].Verdict != "" || find.Items[2].Verdict != VerdictConfirmed {
		t.Errorf("fanout verdicts = %q, %q; want none for a, confirmed for c", find.Items[0].Verdict, find.Items[2].Verdict)
	}
}

func TestVerifyTalliesVerdicts(t *testing.T) {
	t.Parallel()
	verdicts := map[string]*Receipt{
		"a": {Status: StatusOK, Summary: "holds", Fields: map[string]any{VerdictField: "confirmed"}},
		"b": {Status: StatusOK, Summary: "wrong", Fields: map[string]any{VerdictField: "refuted"}},
		"c": {Status: StatusOK, Summary: "wrong too", Fields: map[string]any{VerdictField: "refuted"}},
		"d": {Status: StatusBlocked, Summary: "could not build"},
	}
	spawner := spawnFunc(func(_ context.Context, spec ItemSpec) (Outcome, error) {
		if spec.Stage.Kind == StageVerify {
			return Outcome{Ending: EndCompleted, Receipt: verdicts[spec.Item.Label]}, nil
		}
		return Outcome{Ending: EndCompleted, Receipt: issuesReceipt(spec.Item.Label, 1)}, nil
	})
	runner := newTestRunner(t, spawner)
	verify := Stage{Name: "check", Kind: StageVerify}

	result := runPlan(t, runner, context.Background(), countedPlan([]string{"a", "b", "c", "d"}, verify))

	check := stageNamed(t, result, "check")
	if check.Tally.Confirmed != 1 || check.Tally.Refuted != 2 || check.Tally.Unclear != 1 {
		t.Errorf("verify tally = %+v, want 1 confirmed, 2 refuted, 1 unclear", check.Tally)
	}
	find := stageNamed(t, result, "find")
	if find.Tally.Confirmed != 1 || find.Tally.Refuted != 2 || find.Tally.Unclear != 1 || find.Tally.OK != 4 {
		t.Errorf("fanout tally = %+v, want the verdicts folded in beside 4 ok", find.Tally)
	}
	if find.Items[3].Verdict != VerdictUnclear || find.Items[3].Receipt.Status != StatusOK {
		t.Errorf("item d = verdict %q, receipt %+v; want unclear over its own ok receipt", find.Items[3].Verdict, find.Items[3].Receipt)
	}
}

// A verify child's verdict counts only when it ended ok: a blocked or partial receipt is unclear
// whatever verdict it wrote, and an unfinished item has none.
func TestVerdictOfCountsOnlyAnOKReceipt(t *testing.T) {
	t.Parallel()
	confirmed := map[string]any{VerdictField: string(VerdictConfirmed)}
	for _, tc := range []struct {
		name string
		item ItemResult
		want Verdict
	}{
		{"ok and confirmed", ItemResult{Phase: PhaseDone, Receipt: &Receipt{Status: StatusOK, Fields: confirmed}}, VerdictConfirmed},
		{"ok without a verdict", ItemResult{Phase: PhaseDone, Receipt: &Receipt{Status: StatusOK}}, VerdictUnclear},
		{"blocked but confirmed", ItemResult{Phase: PhaseDone, Receipt: &Receipt{Status: StatusBlocked, Fields: confirmed}}, VerdictUnclear},
		{"partial but confirmed", ItemResult{Phase: PhaseDone, Receipt: &Receipt{Status: StatusPartial, Fields: confirmed}}, VerdictUnclear},
		{"partial but refuted", ItemResult{Phase: PhaseDone, Receipt: &Receipt{Status: StatusPartial, Fields: map[string]any{VerdictField: string(VerdictRefuted)}}}, VerdictUnclear},
		{"unfinished", ItemResult{Receipt: &Receipt{Status: StatusOK, Fields: confirmed}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := verdictOf(tc.item); got != tc.want {
				t.Errorf("verdictOf = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMergeSeesEveryOutputPath(t *testing.T) {
	t.Parallel()
	var manifest string
	spawner := spawnFunc(func(_ context.Context, spec ItemSpec) (Outcome, error) {
		if spec.Stage.Kind != StageMerge {
			return Outcome{Ending: EndCompleted, Receipt: issuesReceipt(spec.Item.Label, 1)}, nil
		}
		data, err := os.ReadFile(spec.Item.Units[0])
		if err != nil {
			return Outcome{}, err
		}
		manifest = string(data)
		if err := os.WriteFile(spec.Output, []byte("# Report\n"), 0o600); err != nil {
			return Outcome{}, err
		}
		return Outcome{Ending: EndCompleted, Receipt: &Receipt{Status: StatusOK, Summary: "merged"}}, nil
	})
	runner := newTestRunner(t, spawner)
	merge := Stage{Name: "report", Kind: StageMerge, Task: "group by severity"}

	result := runPlan(t, runner, context.Background(), countedPlan([]string{"a", "b", "c"}, merge))

	for _, item := range stageNamed(t, result, "find").Items {
		if !strings.Contains(manifest, item.Output) {
			t.Errorf("manifest misses item %q's output %s:\n%s", item.Label, item.Output, manifest)
		}
	}
	if want := filepath.Join(result.Dir, reportName); result.Report != want || result.ReportMissing != "" {
		t.Errorf("report = %q (missing %q), want %q", result.Report, result.ReportMissing, want)
	}
	if phase := stageNamed(t, result, "report").Phase; phase != PhaseDone || result.Phase != PhaseDone {
		t.Errorf("merge phase = %s, workflow phase = %s; want both done", phase, result.Phase)
	}
}

func TestMergeFailureLeavesItemResultsIntact(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		outcome     Outcome
		wantMissing string
	}{
		{"faulted", Outcome{Ending: EndFaulted, Report: "provider fault"}, "blocked"},
		{"no report written", Outcome{Ending: EndCompleted, Receipt: &Receipt{Status: StatusOK, Summary: "merged"}}, "wrote no report"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spawner := spawnFunc(func(_ context.Context, spec ItemSpec) (Outcome, error) {
				if spec.Stage.Kind == StageMerge {
					return tc.outcome, nil
				}
				return Outcome{Ending: EndCompleted, Receipt: issuesReceipt(spec.Item.Label, 3)}, nil
			})
			runner := newTestRunner(t, spawner)
			merge := Stage{Name: "report", Kind: StageMerge, Task: "merge"}

			result := runPlan(t, runner, context.Background(), countedPlan([]string{"a", "b"}, merge))

			find := stageNamed(t, result, "find")
			if find.Tally.OK != 2 || find.Items[1].Receipt.Fields["issues"] != 3 {
				t.Errorf("fanout = %+v, want both items ok with their receipts", find)
			}
			if phase := stageNamed(t, result, "report").Phase; phase != PhaseFailed {
				t.Errorf("merge phase = %s, want failed", phase)
			}
			if result.Report != "" || !strings.Contains(result.ReportMissing, tc.wantMissing) {
				t.Errorf("report = %q, missing = %q; want no report and a reason mentioning %q",
					result.Report, result.ReportMissing, tc.wantMissing)
			}
			if result.Phase != PhaseDone {
				t.Errorf("workflow phase = %s, want done", result.Phase)
			}
		})
	}
}

func TestPickFileDedupesEntries(t *testing.T) {
	t.Parallel()
	scripts := scriptFunc(func(_ context.Context, spec ScriptSpec) (ScriptOutput, error) {
		if err := os.WriteFile(filepath.Join(spec.Dir, "parts.txt"), []byte("p1\np2\n  p1  \np3\np2\n"), 0o600); err != nil {
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

	want := [][]string{{"p1"}, {"p2"}, {"p3"}}
	if got := specUnits(spawner.specsOfStage("each")); !reflect.DeepEqual(got, want) {
		t.Errorf("each items = %v, want each line once %v", got, want)
	}
	wantNote := "picked 3 entries into 3 items; dropped 2 duplicate entries: each entry runs once"
	if note := result.Stages[1].Note; note != wantNote {
		t.Errorf("pick note = %q, want %q", note, wantNote)
	}
}

// mergeReceipt is an ok merge receipt reporting sections report sections.
func mergeReceipt(sections int) *Receipt {
	return &Receipt{Status: StatusOK, Summary: "merged", Fields: map[string]any{"sections": sections}}
}

// writeReport writes a report at a merge child's output path.
func writeReport(spec ItemSpec) error {
	return os.WriteFile(spec.Output, []byte("# Report\n"), 0o600)
}

func TestMergeRerunFailsWhenItsChildWritesNoReport(t *testing.T) {
	t.Parallel()
	merges := 0
	spawner := spawnFunc(func(_ context.Context, spec ItemSpec) (Outcome, error) {
		if spec.Stage.Kind != StageMerge {
			return Outcome{Ending: EndCompleted, Receipt: issuesReceipt(spec.Item.Label, 1)}, nil
		}
		merges++
		if merges == 1 {
			if err := writeReport(spec); err != nil {
				return Outcome{}, err
			}
			return Outcome{Ending: EndCompleted, Receipt: &Receipt{Status: StatusBlocked, Summary: "half done"}}, nil
		}
		return Outcome{Ending: EndCompleted, Receipt: &Receipt{Status: StatusOK, Summary: "merged"}}, nil
	})
	runner := newTestRunner(t, spawner)
	plan := countedPlan([]string{"a", "b"}, Stage{Name: "report", Kind: StageMerge, Task: "merge"})

	runPlan(t, runner, context.Background(), plan)
	second := runPlan(t, runner, context.Background(), plan)

	if merges != 2 {
		t.Fatalf("merge spawned %d times over two runs, want 2: a blocked merge is never resumed", merges)
	}
	if phase := stageNamed(t, second, "report").Phase; phase != PhaseFailed {
		t.Errorf("second merge phase = %s, want failed: its child wrote no report", phase)
	}
	if second.Report != "" || !strings.Contains(second.ReportMissing, "wrote no report") {
		t.Errorf("report = %q, missing = %q; want the first run's report.md cleared, not announced",
			second.Report, second.ReportMissing)
	}
}

func TestRepeatMergeClearsAStaleReport(t *testing.T) {
	t.Parallel()
	spawner := spawnFunc(func(_ context.Context, spec ItemSpec) (Outcome, error) {
		if spec.Stage.Kind != StageMerge {
			return Outcome{Ending: EndCompleted, Receipt: issuesReceipt(spec.Item.Label, 1)}, nil
		}
		if spec.RepeatRound == 0 {
			if err := writeReport(spec); err != nil {
				return Outcome{}, err
			}
			return Outcome{Ending: EndCompleted, Receipt: mergeReceipt(0)}, nil
		}
		return Outcome{Ending: EndCompleted, Receipt: mergeReceipt(1)}, nil
	})
	runner := newTestRunner(t, spawner)
	plan := countedPlan([]string{"a", "b"},
		Stage{Name: "report", Kind: StageMerge, Task: "merge", Returns: ReceiptSpec{"sections": "int"}},
		Stage{Name: "redo", Kind: StageRepeat, Repeat: "report", When: "sections < 1", Max: 1},
	)

	result := runPlan(t, runner, context.Background(), plan)

	if merge := stageNamed(t, result, "report"); merge.Round != 1 || merge.Phase != PhaseFailed {
		t.Errorf("merge = round %d phase %s, want the repeat's round 1 failed", merge.Round, merge.Phase)
	}
	if result.Report != "" || !strings.Contains(result.ReportMissing, "wrote no report") {
		t.Errorf("report = %q, missing = %q; want no report announced after round 1 wrote none",
			result.Report, result.ReportMissing)
	}
	if _, err := os.Stat(filepath.Join(result.Dir, reportName)); !os.IsNotExist(err) {
		t.Errorf("report.md stat = %v, want round 0's report removed", err)
	}
	if formatted := Format(result); strings.Contains("\n"+formatted, "\n"+reportPrefix) {
		t.Errorf("Format announces a report:\n%s", formatted)
	}
}

func TestResumedMergeKeepsItsReport(t *testing.T) {
	t.Parallel()
	merges := 0
	spawner := spawnFunc(func(_ context.Context, spec ItemSpec) (Outcome, error) {
		if spec.Stage.Kind != StageMerge {
			return Outcome{Ending: EndCompleted, Receipt: issuesReceipt(spec.Item.Label, 1)}, nil
		}
		merges++
		if err := writeReport(spec); err != nil {
			return Outcome{}, err
		}
		return Outcome{Ending: EndCompleted, Receipt: &Receipt{Status: StatusOK, Summary: "merged"}}, nil
	})
	runner := newTestRunner(t, spawner)
	plan := countedPlan([]string{"a", "b"}, Stage{Name: "report", Kind: StageMerge, Task: "merge"})

	runPlan(t, runner, context.Background(), plan)
	second := runPlan(t, runner, context.Background(), plan)

	merge := stageNamed(t, second, "report")
	if merges != 1 || !merge.Items[0].Resumed {
		t.Fatalf("merge spawned %d times (resumed %v), want once and resumed on the second run", merges, merge.Items[0].Resumed)
	}
	if want := filepath.Join(second.Dir, reportName); second.Report != want || second.ReportMissing != "" || merge.Phase != PhaseDone {
		t.Errorf("report = %q (missing %q, phase %s), want %q kept and done", second.Report, second.ReportMissing, merge.Phase, want)
	}
}

func TestMergeContinuationKeepsTheReportItsFirstChildWrote(t *testing.T) {
	t.Parallel()
	spawner := spawnFunc(func(_ context.Context, spec ItemSpec) (Outcome, error) {
		if spec.Stage.Kind != StageMerge {
			return Outcome{Ending: EndCompleted, Receipt: issuesReceipt(spec.Item.Label, 1)}, nil
		}
		if len(spec.Prior) == 0 {
			if err := writeReport(spec); err != nil {
				return Outcome{}, err
			}
			return Outcome{Ending: EndCapped, Receipt: &Receipt{Status: StatusPartial, Summary: "half"}, Report: "ran out"}, nil
		}
		return Outcome{Ending: EndCompleted, Receipt: &Receipt{Status: StatusOK, Summary: "merged"}}, nil
	})
	runner := newTestRunner(t, spawner)
	runner.Continuations = 1
	plan := countedPlan([]string{"a"}, Stage{Name: "report", Kind: StageMerge, Task: "merge"})

	result := runPlan(t, runner, context.Background(), plan)

	merge := stageNamed(t, result, "report")
	if merge.Items[0].Continuations != 1 || merge.Phase != PhaseDone {
		t.Errorf("merge = %d continuations, phase %s; want 1 and done", merge.Items[0].Continuations, merge.Phase)
	}
	if want := filepath.Join(result.Dir, reportName); result.Report != want || result.ReportMissing != "" {
		t.Errorf("report = %q (missing %q), want %q: a continuation keeps the report", result.Report, result.ReportMissing, want)
	}
}
