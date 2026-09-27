package workflow

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// dirToken stands in for a run's workflow folder in a golden, so the goldens do not depend on the
// test's temporary directory.
const dirToken = "<dir>"

// withDirToken replaces the workflow folder in text with dirToken.
func withDirToken(text string, result Result) string {
	return strings.ReplaceAll(text, result.Dir, dirToken)
}

// assertGolden fails the test when got differs from want, showing both in full.
func assertGolden(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("Format =\n%s\n\nwant\n%s", got, want)
	}
}

func TestFormatThreeVerifiedItems(t *testing.T) {
	t.Parallel()
	result := Result{
		ID: "20260927-140309-audit", Dir: "/scratch/workflows/20260927-140309-audit", Phase: PhaseDone,
		Report: "/scratch/workflows/20260927-140309-audit/report.md",
		Stages: []StageResult{{
			Name: "find", Kind: StageFanout, Phase: PhaseDone,
			Items: []ItemResult{
				{Label: "a.go", Phase: PhaseDone, Verdict: VerdictConfirmed, Receipt: &Receipt{
					Status: StatusOK, Summary: "two nil derefs\nand more detail",
					Fields: map[string]any{"issues": float64(2), "files": []any{"a.go", "b.go"}},
				}},
				{Label: "b.go", Phase: PhaseDone, Verdict: VerdictRefuted, Receipt: &Receipt{
					Status: StatusPartial, Summary: "ran out of steps", Fields: map[string]any{"issues": 1, "area": "the parser"},
				}},
				{Label: "c.go", Phase: PhaseDone, Receipt: &Receipt{Status: StatusBlocked, Summary: "file unreadable"}},
			},
			Tally: Tally{OK: 1, Partial: 1, Blocked: 1, Confirmed: 1, Refuted: 1},
		}, {
			Name: "check", Kind: StageVerify, Phase: PhaseDone,
		}, {
			Name: "report", Kind: StageMerge, Phase: PhaseDone,
		}},
	}

	got := Format(result)

	assertGolden(t, got, strings.Join([]string{
		"#1 a.go — ok — two nil derefs files=a.go,b.go issues=2 verdict=confirmed",
		`#2 b.go — partial — ran out of steps area="the parser" issues=1 verdict=refuted`,
		"#3 c.go — blocked — file unreadable",
		"items 3 · ok 1 · partial 1 · blocked 1 · confirmed 1 · refuted 1 · unclear 0",
		"report: /scratch/workflows/20260927-140309-audit/report.md",
	}, "\n"))
}

func TestFormatStoppedWorkflow(t *testing.T) {
	t.Parallel()
	result := Result{
		ID: "20260927-140309-audit", Dir: "/scratch/workflows/20260927-140309-audit", Phase: PhaseStopped,
		Stages: []StageResult{{
			Name: "find", Kind: StageFanout, Phase: PhaseStopped,
			Items: []ItemResult{
				{Label: "a", Phase: PhaseDone, Receipt: okReceipt("a")},
				{Label: "b", Phase: PhaseStopped},
				{Label: "c", Phase: PhaseStopped},
				{Label: "d", Phase: PhasePending},
			},
			Tally: Tally{OK: 1, Unfinished: 3},
		}},
	}

	got := Format(result)

	assertGolden(t, got, strings.Join([]string{
		"stopped by the user: 1 of 4 done",
		"#1 a — ok — checked a",
		"#2 b — stopped — no receipt",
		"#3 c — stopped — no receipt",
		"#4 d — pending — no receipt",
		"items 4 · ok 1 · partial 0 · blocked 0 · unfinished 3",
	}, "\n"))
}

func TestFormatPast40ListsOnlyTheNonOKItems(t *testing.T) {
	t.Parallel()
	labels := make([]string, 41)
	for index := range labels {
		labels[index] = fmt.Sprintf("i%02d", index+1)
	}
	spawner := spawnFunc(func(_ context.Context, spec ItemSpec) (Outcome, error) {
		switch spec.Item.Label {
		case "i07":
			return Outcome{Ending: EndCompleted, Receipt: &Receipt{Status: StatusPartial, Summary: "half done"}}, nil
		case "i30":
			return Outcome{Ending: EndCompleted, Receipt: &Receipt{Status: StatusBlocked, Summary: "no access"}}, nil
		}
		return Outcome{Ending: EndCompleted, Receipt: okReceipt(spec.Item.Label)}, nil
	})
	runner := newTestRunner(t, spawner)

	result := runPlan(t, runner, context.Background(), fanPlan(labels...))
	got := withDirToken(Format(result), result)

	assertGolden(t, got, strings.Join([]string{
		"#7 i07 — partial — half done",
		"#30 i30 — blocked — no access",
		"items 41 · ok 39 · partial 1 · blocked 1",
		"items: <dir>/items.md",
	}, "\n"))
	listing, err := os.ReadFile(result.Listing)
	if err != nil {
		t.Fatalf("items.md: %v", err)
	}
	for _, want := range []string{"#1 i01 — ok — checked i01", "#41 i41 — ok — checked i41", "#7 i07 — partial — half done"} {
		if !strings.Contains(string(listing), want+"\n   output: ") {
			t.Errorf("items.md lacks %q with its output path:\n%s", want, listing)
		}
	}
}

func TestFormatShortRunStillWritesItemsButDoesNotPointToIt(t *testing.T) {
	t.Parallel()
	runner := newTestRunner(t, okSpawner())

	result := runPlan(t, runner, context.Background(), fanPlan("a", "b"))
	got := Format(result)

	if _, err := os.Stat(result.Listing); err != nil {
		t.Errorf("items.md: %v", err)
	}
	assertGolden(t, got, strings.Join([]string{
		"#1 a — ok — checked a",
		"#2 b — ok — checked b",
		"items 2 · ok 2 · partial 0 · blocked 0",
	}, "\n"))
}

func TestFormatFailedMerge(t *testing.T) {
	t.Parallel()
	spawner := spawnFunc(func(_ context.Context, spec ItemSpec) (Outcome, error) {
		if spec.Stage.Kind == StageMerge {
			return Outcome{Ending: EndCompleted, Receipt: &Receipt{Status: StatusOK, Summary: "merged"}}, nil
		}
		return Outcome{Ending: EndCompleted, Receipt: issuesReceipt(spec.Item.Label, 3)}, nil
	})
	runner := newTestRunner(t, spawner)
	merge := Stage{Name: "report", Kind: StageMerge, Task: "merge"}

	result := runPlan(t, runner, context.Background(), countedPlan([]string{"a", "b"}, merge))
	got := withDirToken(Format(result), result)

	assertGolden(t, got, strings.Join([]string{
		"#1 a — ok — audited a issues=3",
		"#2 b — ok — audited b issues=3",
		"items 2 · ok 2 · partial 0 · blocked 0",
		"merge report: no report — the merge child finished ok but wrote no report at <dir>/report.md",
	}, "\n"))
}

func TestFormatAskThatTookItsDefault(t *testing.T) {
	t.Parallel()
	runner := newTestRunner(t, okSpawner())

	result := runPlan(t, runner, context.Background(), askPlan())
	got := Format(result)

	assertGolden(t, got, strings.Join([]string{
		"#1 a — ok — checked a",
		"items 1 · ok 1 · partial 0 · blocked 0",
		"ask scope: ok — took the default no answer=no (default taken: no one to ask)",
		"fanout deep: skipped: scope.answer == yes is false",
	}, "\n"))
}
