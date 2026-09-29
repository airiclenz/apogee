package workflow

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// fanoutStage is a well-formed fanout the cases below build on.
func fanoutStage(name string) Stage {
	return Stage{
		Name:    name,
		Kind:    StageFanout,
		Task:    "Review {item}; write the findings to {out}.",
		Over:    &ItemSource{Files: "internal/**/*.go"},
		Returns: ReceiptSpec{"findings": "int", "verdict": "confirmed|refuted|unclear", "paths": "list"},
	}
}

// withStage returns fanoutStage("find") followed by extra, so each case states only the stage
// under test.
func withStage(extra ...Stage) []Stage {
	return append([]Stage{fanoutStage("find")}, extra...)
}

// problemCase is one plan and the single problem Validate must report for it.
type problemCase struct {
	name        string
	stages      []Stage
	wantStage   string
	wantField   string
	wantMessage string
}

// assertOneProblem fails t unless problems is exactly the one problem c expects.
func assertOneProblem(t *testing.T, c problemCase, problems []Problem) {
	t.Helper()
	if len(problems) != 1 {
		t.Fatalf("got %d problems, want 1: %v", len(problems), problems)
	}
	got := problems[0]
	if got.Stage != c.wantStage || got.Field != c.wantField || !strings.Contains(got.Message, c.wantMessage) {
		t.Errorf("got %q, want stage %q field %q message containing %q", got, c.wantStage, c.wantField, c.wantMessage)
	}
}

func TestValidateAcceptsAWellFormedRecipeUsingEveryKind(t *testing.T) {
	t.Parallel()

	plan := Plan{Name: "audit", Stages: []Stage{
		{Name: "split", Kind: StageScript, Run: "./split.sh", Returns: ReceiptSpec{"parts": "int"}},
		{Name: "scope", Kind: StageAsk, Question: "Audit tests too?", Options: []string{"yes", "no"}, Default: "no"},
		fanoutStage("find"),
		{Name: "check", Kind: StageVerify, When: "verdict == confirmed", Task: "Look hard."},
		{Name: "paths", Kind: StagePick, From: "find", Field: "paths", Cap: 20, Batch: 2},
		{Name: "deep", Kind: StageFanout, Prompt: "prompts/deep.md", Out: "{item}/x.md", Over: &ItemSource{Stage: "paths"}},
		{Name: "again", Kind: StageRepeat, Repeat: "deep", When: "ok < 3", Max: 2},
		{Name: "report", Kind: StageMerge, From: "find", Task: "Merge every finding."},
	}}

	problems := Validate(plan)

	if len(problems) != 0 {
		t.Errorf("Validate = %v, want no problems", problems)
	}
}

func TestValidateNamesEachProblemsStageAndField(t *testing.T) {
	t.Parallel()

	cases := []problemCase{
		{name: "no stages", stages: nil, wantField: "stages", wantMessage: "no stages"},
		{name: "unnamed stage", stages: []Stage{{Kind: StageScript, Run: "true"}}, wantField: "name", wantMessage: "stage 1 has no name"},
		{name: "badly spelled name", stages: []Stage{{Name: "Find All", Kind: StageScript, Run: "true"}}, wantStage: "Find All", wantField: "name", wantMessage: "lower-case"},
		{name: "repeated name", stages: withStage(fanoutStage("find")), wantStage: "find", wantField: "name", wantMessage: "already has this name"},
		{name: "unknown kind", stages: []Stage{{Name: "x", Kind: "loop"}}, wantStage: "x", wantField: "kind", wantMessage: "fanout, merge, pick"},
		{name: "misplaced key", stages: []Stage{func() Stage { s := fanoutStage("find"); s.Max = 3; return s }()}, wantStage: "find", wantField: "max", wantMessage: "only repeat stages do"},
		{name: "fanout without a brief", stages: []Stage{func() Stage { s := fanoutStage("find"); s.Task = ""; return s }()}, wantStage: "find", wantField: "task", wantMessage: "needs a brief"},
		{name: "brief given twice", stages: []Stage{func() Stage { s := fanoutStage("find"); s.Prompt = "p.md"; return s }()}, wantStage: "find", wantField: "task", wantMessage: "given twice"},
		{name: "fanout out without an item", stages: []Stage{func() Stage { s := fanoutStage("find"); s.Out = "results/report.md"; return s }()}, wantStage: "find", wantField: "out", wantMessage: "has no {item}"},
		{name: "fanout without items", stages: []Stage{func() Stage { s := fanoutStage("find"); s.Over = nil; return s }()}, wantStage: "find", wantField: "over", wantMessage: "needs items"},
		{name: "two item sources", stages: []Stage{func() Stage { s := fanoutStage("find"); s.Over.Lines = "parts.txt"; return s }()}, wantStage: "find", wantField: "over", wantMessage: "exactly one item source"},
		{name: "negative item batch", stages: []Stage{func() Stage { s := fanoutStage("find"); s.Over.Batch = -1; return s }()}, wantStage: "find", wantField: "over", wantMessage: "batch is negative"},
		{name: "blank literal item", stages: []Stage{func() Stage { s := fanoutStage("find"); s.Over = &ItemSource{List: []string{"a", " "}}; return s }()}, wantStage: "find", wantField: "over", wantMessage: "blank item"},
		{name: "item stage is not a pick", stages: withStage(Stage{Name: "more", Kind: StageFanout, Task: "t", Over: &ItemSource{Stage: "find"}}), wantStage: "more", wantField: "over", wantMessage: "names a pick stage"},
		{name: "item stage comes later", stages: []Stage{{Name: "more", Kind: StageFanout, Task: "t", Over: &ItemSource{Stage: "later"}}, {Name: "later", Kind: StagePick, File: "f"}}, wantStage: "more", wantField: "over", wantMessage: "does not come before"},
		{name: "batch beside a pick source", stages: withStage(Stage{Name: "p", Kind: StagePick, File: "f"}, Stage{Name: "more", Kind: StageFanout, Task: "t", Over: &ItemSource{Stage: "p", Batch: 2}}), wantStage: "more", wantField: "over", wantMessage: "on the pick stage"},
		{name: "reserved receipt field", stages: []Stage{func() Stage { s := fanoutStage("find"); s.Returns = ReceiptSpec{"status": "text"}; return s }()}, wantStage: "find", wantField: "returns.status", wantMessage: "part of every receipt"},
		{name: "badly spelled receipt field", stages: []Stage{func() Stage { s := fanoutStage("find"); s.Returns = ReceiptSpec{"Count": "int"}; return s }()}, wantStage: "find", wantField: "returns.Count", wantMessage: "lower-case"},
		{name: "unknown receipt type", stages: []Stage{func() Stage { s := fanoutStage("find"); s.Returns = ReceiptSpec{"n": "number"}; return s }()}, wantStage: "find", wantField: "returns.n", wantMessage: "not one of int, text, list"},
		{name: "enum with an empty value", stages: []Stage{func() Stage { s := fanoutStage("find"); s.Returns = ReceiptSpec{"v": "enum a||b"}; return s }()}, wantStage: "find", wantField: "returns.v", wantMessage: "empty value"},
		{name: "enum repeating a value", stages: []Stage{func() Stage { s := fanoutStage("find"); s.Returns = ReceiptSpec{"v": "a|a"}; return s }()}, wantStage: "find", wantField: "returns.v", wantMessage: "twice"},
		{name: "blank context entry", stages: []Stage{func() Stage { s := fanoutStage("find"); s.Context = []string{""}; return s }()}, wantStage: "find", wantField: "context", wantMessage: "blank entry"},
		{name: "blank tools entry", stages: []Stage{func() Stage { s := fanoutStage("find"); s.Tools = []string{"read_file", " "}; return s }()}, wantStage: "find", wantField: "tools", wantMessage: "blank entry"},
		{name: "verify with no fanout before it", stages: []Stage{{Name: "check", Kind: StageVerify}}, wantStage: "check", wantField: "from", wantMessage: "none comes before it"},
		{name: "verify from a non-fanout", stages: withStage(Stage{Name: "s", Kind: StageScript, Run: "true"}, Stage{Name: "check", Kind: StageVerify, From: "s"}), wantStage: "check", wantField: "from", wantMessage: "works over a fanout"},
		{name: "merge without a brief", stages: withStage(Stage{Name: "report", Kind: StageMerge}), wantStage: "report", wantField: "task", wantMessage: "needs a brief"},
		{name: "merge from an unknown stage", stages: withStage(Stage{Name: "report", Kind: StageMerge, Task: "t", From: "nope"}), wantStage: "report", wantField: "from", wantMessage: "earlier stages are find"},
		{name: "pick with neither field nor file", stages: withStage(Stage{Name: "p", Kind: StagePick, From: "find"}), wantStage: "p", wantField: "field", wantMessage: "exactly one of field"},
		{name: "pick with field and file", stages: withStage(Stage{Name: "p", Kind: StagePick, From: "find", Field: "paths", File: "f"}), wantStage: "p", wantField: "field", wantMessage: "exactly one of field"},
		{name: "pick by field without from", stages: withStage(Stage{Name: "p", Kind: StagePick, Field: "paths"}), wantStage: "p", wantField: "from", wantMessage: "needs from"},
		{name: "pick of a field that is not a list", stages: withStage(Stage{Name: "p", Kind: StagePick, From: "find", Field: "findings"}), wantStage: "p", wantField: "field", wantMessage: "no list field"},
		{name: "pick by file with from", stages: withStage(Stage{Name: "p", Kind: StagePick, From: "find", File: "f"}), wantStage: "p", wantField: "from", wantMessage: "read only with field"},
		{name: "pick with a negative cap", stages: withStage(Stage{Name: "p", Kind: StagePick, File: "f", Cap: -1}), wantStage: "p", wantField: "cap", wantMessage: "cap is negative"},
		{name: "pick with a negative batch", stages: withStage(Stage{Name: "p", Kind: StagePick, File: "f", Batch: -2}), wantStage: "p", wantField: "batch", wantMessage: "batch is negative"},
		{name: "script without a command", stages: []Stage{{Name: "s", Kind: StageScript}}, wantStage: "s", wantField: "run", wantMessage: "needs run"},
		{name: "ask without a question", stages: []Stage{{Name: "a", Kind: StageAsk, Default: "no"}}, wantStage: "a", wantField: "question", wantMessage: "needs question"},
		{name: "ask without a default", stages: []Stage{{Name: "a", Kind: StageAsk, Question: "Go?"}}, wantStage: "a", wantField: "default", wantMessage: "needs default"},
		{name: "ask with one option", stages: []Stage{{Name: "a", Kind: StageAsk, Question: "Go?", Options: []string{"yes"}, Default: "yes"}}, wantStage: "a", wantField: "options", wantMessage: "at least two"},
		{name: "ask default outside its options", stages: []Stage{{Name: "a", Kind: StageAsk, Question: "Go?", Options: []string{"yes", "no"}, Default: "maybe"}}, wantStage: "a", wantField: "default", wantMessage: "not among the options"},
		{name: "repeat without a target", stages: withStage(Stage{Name: "r", Kind: StageRepeat, When: "ok < 3", Max: 2}), wantStage: "r", wantField: "repeat", wantMessage: "needs repeat"},
		{name: "repeat of a repeat", stages: withStage(Stage{Name: "r", Kind: StageRepeat, Repeat: "find", When: "ok < 3", Max: 2}, Stage{Name: "rr", Kind: StageRepeat, Repeat: "r", When: "ok < 3", Max: 2}), wantStage: "rr", wantField: "repeat", wantMessage: "itself a repeat"},
		{name: "repeat without a condition", stages: withStage(Stage{Name: "r", Kind: StageRepeat, Repeat: "find", Max: 2}), wantStage: "r", wantField: "when", wantMessage: "needs when"},
		{name: "repeat with no max", stages: withStage(Stage{Name: "r", Kind: StageRepeat, Repeat: "find", When: "ok < 3"}), wantStage: "r", wantField: "max", wantMessage: "between 1 and 10"},
		{name: "repeat past the bound", stages: withStage(Stage{Name: "r", Kind: StageRepeat, Repeat: "find", When: "ok < 3", Max: MaxRepeatRounds + 1}), wantStage: "r", wantField: "max", wantMessage: "between 1 and 10"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			problems := Validate(Plan{Stages: c.stages})

			assertOneProblem(t, c, problems)
		})
	}
}

func TestValidateRefusesABadCondition(t *testing.T) {
	t.Parallel()

	scored := fanoutStage("score")
	scored.Returns = ReceiptSpec{"score": "int"}
	cases := []problemCase{
		{name: "verify on an undeclared field", stages: withStage(Stage{Name: "check", Kind: StageVerify, When: "severity == high"}), wantStage: "check", wantField: "when", wantMessage: `at "severity"`},
		{name: "verify on an undeclared enum value", stages: withStage(Stage{Name: "check", Kind: StageVerify, When: "verdict == maybe"}), wantStage: "check", wantField: "when", wantMessage: "confirmed, refuted, unclear"},
		{name: "verify ordering an enum", stages: withStage(Stage{Name: "check", Kind: StageVerify, When: "verdict > confirmed"}), wantStage: "check", wantField: "when", wantMessage: "only with == or !="},
		{name: "verify comparing a list", stages: withStage(Stage{Name: "check", Kind: StageVerify, When: "paths == x"}), wantStage: "check", wantField: "when", wantMessage: "cannot compare a list"},
		{name: "verify reads the fanout its from names", stages: withStage(scored, Stage{Name: "check", Kind: StageVerify, From: "find", When: "score > 1"}), wantStage: "check", wantField: "when", wantMessage: `at "score"`},
		{name: "verify reads the nearest earlier fanout", stages: withStage(scored, Stage{Name: "check", Kind: StageVerify, When: "findings > 1"}), wantStage: "check", wantField: "when", wantMessage: `at "findings"`},
		{name: "script with a single equals", stages: withStage(Stage{Name: "s", Kind: StageScript, Run: "true", When: "parts = 0"}), wantStage: "s", wantField: "when", wantMessage: "compare with =="},
		{name: "merge with an unclosed paren", stages: withStage(Stage{Name: "m", Kind: StageMerge, Task: "Merge.", When: "(status == ok"}), wantStage: "m", wantField: "when", wantMessage: "never closed"},
		{name: "repeat with a dangling operator", stages: withStage(Stage{Name: "r", Kind: StageRepeat, Repeat: "find", When: "ok <", Max: 2}), wantStage: "r", wantField: "when", wantMessage: "expected a value"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			problems := Validate(Plan{Stages: c.stages})

			assertOneProblem(t, c, problems)
		})
	}
}

func TestValidateTypesAVerifyConditionAgainstItsFanout(t *testing.T) {
	t.Parallel()

	scored := fanoutStage("score")
	scored.Returns = ReceiptSpec{"score": "int"}
	stages := withStage(
		scored,
		Stage{Name: "check", Kind: StageVerify, From: "score", When: "score >= 2 and not status == blocked"},
		Stage{Name: "recheck", Kind: StageVerify, From: "find", When: "(verdict == confirmed or findings > 0) and summary != none"},
	)

	problems := Validate(Plan{Stages: stages})

	if len(problems) != 0 {
		t.Errorf("Validate = %v, want no problems", problems)
	}
}

// conditionStages is a script, an ask and the fanout find, which stage conditions read.
func conditionStages(extra ...Stage) []Stage {
	return append([]Stage{
		{Name: "split", Kind: StageScript, Run: "./split.sh", Returns: ReceiptSpec{"parts": "int", "groups": "list"}},
		{Name: "scope", Kind: StageAsk, Question: "Tests too?", Options: []string{"yes", "no"}, Default: "no"},
		fanoutStage("find"),
		{Name: "paths", Kind: StagePick, From: "find", Field: "paths"},
	}, extra...)
}

func TestValidateTypesAStageConditionAgainstTheStageItNames(t *testing.T) {
	t.Parallel()

	stages := conditionStages(
		Stage{Name: "prep", Kind: StageScript, When: "split.parts > 0 and split.status == ok", Run: "true"},
		Stage{Name: "more", Kind: StageFanout, When: "scope.answer == yes or find.blocked == 0", Task: "t", Over: &ItemSource{List: []string{"a"}}},
		Stage{Name: "report", Kind: StageMerge, From: "find", When: "not find.partial > 2", Task: "merge", Returns: ReceiptSpec{"sections": "int"}},
		Stage{Name: "again", Kind: StageRepeat, Repeat: "find", When: "ok < 3 and split.parts > 1", Max: 2},
		Stage{Name: "redo", Kind: StageRepeat, Repeat: "report", When: "status != ok or sections < 1", Max: 2},
		Stage{Name: "files", Kind: StagePick, When: "report.sections > 0", File: "stages/find/list.txt"},
	)

	problems := Validate(Plan{Stages: stages})

	if len(problems) != 0 {
		t.Errorf("Validate = %v, want no problems", problems)
	}
}

func TestValidateRefusesAStageConditionOnAnotherSubject(t *testing.T) {
	t.Parallel()

	fanout := func(when string) Stage {
		return Stage{Name: "more", Kind: StageFanout, When: when, Task: "t", Over: &ItemSource{List: []string{"a"}}}
	}
	cases := []problemCase{
		{name: "a per-item field of a fanout", stages: conditionStages(fanout("find.findings > 0")), wantStage: "more", wantField: "when", wantMessage: "only a verify stage's when reads its items' fields"},
		{name: "a field that names no stage", stages: conditionStages(fanout("parts > 0")), wantStage: "more", wantField: "when", wantMessage: "write parts as <stage>.parts"},
		{name: "a pick stage", stages: conditionStages(fanout("paths.ok > 0")), wantStage: "more", wantField: "when", wantMessage: "leaves no receipt to read"},
		{name: "a field the script does not return", stages: conditionStages(fanout("split.files > 0")), wantStage: "more", wantField: "when", wantMessage: "no receipt field files"},
		{name: "a list field", stages: conditionStages(fanout("split.groups == a")), wantStage: "more", wantField: "when", wantMessage: "cannot compare a list"},
		{name: "an answer outside the options", stages: conditionStages(fanout("scope.answer == maybe")), wantStage: "more", wantField: "when", wantMessage: "one of yes, no"},
		{name: "a later stage", stages: conditionStages(fanout("later.ok > 0"), Stage{Name: "later", Kind: StageScript, Run: "true"}), wantStage: "more", wantField: "when", wantMessage: "does not come before"},
		{name: "a repeat reading its fanout's items", stages: conditionStages(Stage{Name: "again", Kind: StageRepeat, Repeat: "find", When: "findings > 0", Max: 2}), wantStage: "again", wantField: "when", wantMessage: "only a verify stage's when"},
		{name: "a repeat reading a fanout's status", stages: conditionStages(Stage{Name: "again", Kind: StageRepeat, Repeat: "find", When: "status == ok", Max: 2}), wantStage: "again", wantField: "when", wantMessage: "reads its tally"},
		{name: "a pick file climbing out", stages: withStage(Stage{Name: "p", Kind: StagePick, File: "../secrets.txt"}), wantStage: "p", wantField: "file", wantMessage: "inside the workflow folder"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			problems := Validate(Plan{Stages: c.stages})

			assertOneProblem(t, c, problems)
		})
	}
}

func TestValidateModelPlanKeepsTheFanOutShape(t *testing.T) {
	t.Parallel()

	verify := Stage{Name: "check", Kind: StageVerify, When: "verdict == confirmed"}
	merge := Stage{Name: "report", Kind: StageMerge, Task: "Merge."}
	cases := []problemCase{
		{name: "a second fanout", stages: withStage(fanoutStage("more")), wantStage: "more", wantField: "kind", wantMessage: "needs a recipe"},
		{name: "a pick", stages: withStage(Stage{Name: "p", Kind: StagePick, File: "f"}), wantStage: "p", wantField: "kind", wantMessage: "a pick stage exists only in a recipe"},
		{name: "a script", stages: withStage(Stage{Name: "s", Kind: StageScript, Run: "true"}), wantStage: "s", wantField: "kind", wantMessage: "a script stage exists only in a recipe"},
		{name: "two verifies", stages: withStage(verify, Stage{Name: "again", Kind: StageVerify}), wantStage: "again", wantField: "kind", wantMessage: "at most one verify"},
		{name: "verify after merge", stages: withStage(merge, verify), wantStage: "check", wantField: "kind", wantMessage: "verify comes before merge"},
		{name: "two merges", stages: withStage(merge, Stage{Name: "again", Kind: StageMerge, Task: "t"}), wantStage: "again", wantField: "kind", wantMessage: "at most one merge"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			problems := ValidateModelPlan(Plan{Stages: c.stages})

			assertOneProblem(t, c, problems)
		})
	}
}

func TestValidateModelPlanAcceptsFanoutVerifyMerge(t *testing.T) {
	t.Parallel()

	plan := Plan{Stages: withStage(
		Stage{Name: "check", Kind: StageVerify, When: "verdict == confirmed"},
		Stage{Name: "report", Kind: StageMerge, Task: "Merge."},
	)}

	problems := ValidateModelPlan(plan)

	if len(problems) != 0 {
		t.Errorf("ValidateModelPlan = %v, want no problems", problems)
	}
}

func TestReceiptSpecCheck(t *testing.T) {
	t.Parallel()

	spec := ReceiptSpec{"findings": "int", "verdict": "enum confirmed|refuted|unclear", "note": "text", "paths": "list"}
	complete := map[string]any{"findings": float64(3), "verdict": "confirmed", "note": "short", "paths": []any{"a.go"}}
	cases := []struct {
		name      string
		receipt   Receipt
		wantField string // "" when the receipt is well formed
		wantText  string
	}{
		{name: "well formed ok", receipt: Receipt{Status: StatusOK, Summary: "Three findings in the parser.", Fields: complete}},
		{name: "partial may leave fields out", receipt: Receipt{Status: StatusPartial, Summary: "Ran out of steps."}},
		{name: "json.Number and []string are accepted", receipt: Receipt{Status: StatusBlocked, Summary: "Blocked.", Fields: map[string]any{"findings": json.Number("2"), "paths": []string{"x"}}}},
		{name: "unknown status", receipt: Receipt{Status: "done", Summary: "Done."}, wantField: FieldStatus, wantText: "ok, partial, blocked"},
		{name: "empty summary", receipt: Receipt{Status: StatusPartial}, wantField: FieldSummary, wantText: "summary is empty"},
		{name: "summary past twenty words", receipt: Receipt{Status: StatusPartial, Summary: strings.Repeat("word ", SummaryMaxWords+1)}, wantField: FieldSummary, wantText: "21 words"},
		{name: "undeclared field", receipt: Receipt{Status: StatusPartial, Summary: "s", Fields: map[string]any{"score": 1}}, wantField: "score", wantText: "findings, note, paths, verdict"},
		{name: "fractional int", receipt: Receipt{Status: StatusPartial, Summary: "s", Fields: map[string]any{"findings": 2.5}}, wantField: "findings", wantText: "whole number"},
		{name: "enum value outside the set", receipt: Receipt{Status: StatusPartial, Summary: "s", Fields: map[string]any{"verdict": "maybe"}}, wantField: "verdict", wantText: "confirmed, refuted, unclear"},
		{name: "text past its limit", receipt: Receipt{Status: StatusPartial, Summary: "s", Fields: map[string]any{"note": strings.Repeat("é", TextMaxRunes+1)}}, wantField: "note", wantText: "201 characters"},
		{name: "list holding a number", receipt: Receipt{Status: StatusPartial, Summary: "s", Fields: map[string]any{"paths": []any{"a", 1.0}}}, wantField: "paths", wantText: "list of strings"},
		{name: "ok missing a declared field", receipt: Receipt{Status: StatusOK, Summary: "s", Fields: map[string]any{"findings": 1, "verdict": "refuted", "note": "n"}}, wantField: "paths", wantText: "every declared field"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			problems := spec.Check(c.receipt)

			if c.wantField == "" {
				if len(problems) != 0 {
					t.Errorf("Check = %v, want none", problems)
				}
				return
			}
			if len(problems) != 1 || problems[0].Field != c.wantField || !strings.Contains(problems[0].Message, c.wantText) {
				t.Errorf("Check = %v, want one problem on %q containing %q", problems, c.wantField, c.wantText)
			}
		})
	}
}

func TestReceiptSpecFieldKnowsTheCoreFields(t *testing.T) {
	t.Parallel()

	spec := ReceiptSpec{"verdict": "confirmed|refuted"}
	cases := []struct {
		name     string
		field    string
		want     FieldType
		isKnown  bool
		spelling string
	}{
		{name: "status", field: FieldStatus, want: FieldType{Kind: FieldEnum, Values: []string{"ok", "partial", "blocked"}}, isKnown: true},
		{name: "summary", field: FieldSummary, want: FieldType{Kind: FieldText}, isKnown: true},
		{name: "declared enum", field: "verdict", want: FieldType{Kind: FieldEnum, Values: []string{"confirmed", "refuted"}}, isKnown: true},
		{name: "undeclared", field: "score"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, isKnown := spec.Field(c.field)

			if isKnown != c.isKnown || !reflect.DeepEqual(got, c.want) {
				t.Errorf("Field(%q) = %+v, %v; want %+v, %v", c.field, got, isKnown, c.want, c.isKnown)
			}
		})
	}
}

func TestPlanRoundTripsThroughYAMLAndJSON(t *testing.T) {
	t.Parallel()

	plan := Plan{Name: "audit", Stages: []Stage{
		{Name: "scope", Kind: StageAsk, When: "parts > 0", Question: "Tests too?", Options: []string{"yes", "no"}, Default: "no"},
		{
			Name: "find", Kind: StageFanout, Task: "Audit {item}.", Out: "out/{item}.md",
			Over:    &ItemSource{Split: "internal", Batch: 2},
			Returns: ReceiptSpec{"findings": "int"}, Context: []string{"CONTEXT.md"}, Tools: []string{"read_file"},
		},
		{Name: "paths", Kind: StagePick, From: "find", Field: "paths", Cap: 5, Batch: 1},
		{Name: "report", Kind: StageMerge, From: "find", Prompt: "merge.md"},
		{Name: "prep", Kind: StageScript, Run: "./split.sh"},
		{Name: "again", Kind: StageRepeat, Repeat: "find", When: "ok < 2", Max: 3},
	}}

	t.Run("yaml", func(t *testing.T) {
		t.Parallel()

		encoded, err := yaml.Marshal(plan)
		if err != nil {
			t.Fatalf("yaml.Marshal: %v", err)
		}
		var decoded Plan
		if err := yaml.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("yaml.Unmarshal: %v", err)
		}

		if !reflect.DeepEqual(decoded, plan) {
			t.Errorf("yaml round trip changed the plan:\n%s", encoded)
		}
		for _, key := range []string{"when:", "cap:", "batch:", "max:", "over:", "returns:"} {
			if !strings.Contains(string(encoded), key) {
				t.Errorf("yaml carries no %q key:\n%s", key, encoded)
			}
		}
	})
	t.Run("json", func(t *testing.T) {
		t.Parallel()

		encoded, err := json.Marshal(plan)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		var decoded Plan
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		if !reflect.DeepEqual(decoded, plan) {
			t.Errorf("json round trip changed the plan:\n%s", encoded)
		}
	})
}

func TestProblemStringNamesStageAndField(t *testing.T) {
	t.Parallel()

	got := Problem{Stage: "find", Field: "over", Message: "needs items"}.String()

	if want := `stage "find", field "over": needs items`; got != want {
		t.Errorf("String = %q, want %q", got, want)
	}
}

// TestWorkflowStaysOffTheLoopAndItsDrivers holds ADR 0087 D10's boundary: the package is a
// Driver-free library, so no non-test file may import the loop, config, a Driver, the tool
// registry or the root facade. Files are parsed straight off the directory on
// internal/webhook's pattern, so a build-tagged file cannot hide a violation.
func TestWorkflowStaysOffTheLoopAndItsDrivers(t *testing.T) {
	t.Parallel()

	// The root module path is derived rather than spelled as a bare quoted literal: make check's
	// ADR-0010 gate greps internal/ for exactly that literal as an import of the root facade.
	const internalTree = "github.com/airiclenz/apogee/internal"
	module := strings.TrimSuffix(internalTree, "/internal")
	forbidden := []string{module, internalTree + "/agent", internalTree + "/config", internalTree + "/run", internalTree + "/tools", internalTree + "/tui"}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	fset := token.NewFileSet()
	parsed := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		parsed++
		for _, imported := range file.Imports {
			path := strings.Trim(imported.Path.Value, `"`)
			for _, refused := range forbidden {
				if path == refused || strings.HasPrefix(path, refused+"/") && refused != module {
					t.Errorf("%s imports %q; internal/workflow is a Driver-free library (ADR 0087 D10)", name, path)
				}
			}
		}
	}
	if parsed == 0 {
		t.Fatal("no .go files were parsed; the boundary guard proved nothing")
	}
}
