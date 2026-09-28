package workflow_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/workflow"
)

// ----------------------------------------------------------------------------
// PlanToRecipe (torecipe.go)
// ----------------------------------------------------------------------------
//
// An external test package: the round trip reads the saved SKILL.md back through the skills loader,
// and an in-package test importing internal/skills would be an import cycle.

// fanOutPlan is a plan of the shape a fan_out call writes: a fanout over source, then a verify and a
// merge.
func fanOutPlan(source workflow.ItemSource) workflow.Plan {
	return workflow.Plan{Name: "check every handler for a missing error return", Stages: []workflow.Stage{
		{
			Name: "fanout", Kind: workflow.StageFanout,
			Task:    "Check {item} for a missing error return.\nWrite what you find to {out}.",
			Over:    &source,
			Returns: workflow.ReceiptSpec{"findings": "int", "severity": "low|high"},
			Context: []string{"AGENTS.md"},
			Tools:   []string{"read_file", "grep"},
		},
		{Name: "verify", Kind: workflow.StageVerify, When: "findings > 0", Task: "Refute the claim."},
		{Name: "merge", Kind: workflow.StageMerge, Task: "Merge every finding into one report."},
	}}
}

// saveAndLoad writes plan's recipe as the skill name in a temp home's library and reads it back
// through skills.Load, failing the test when either step does.
func saveAndLoad(t *testing.T, plan workflow.Plan, name string) skills.Skill {
	t.Helper()
	content, err := workflow.PlanToRecipe(plan)
	if err != nil {
		t.Fatalf("PlanToRecipe: %v", err)
	}
	home := t.TempDir()
	if _, err := skills.WriteNew(name, filepath.Join(home, "skills"), content); err != nil {
		t.Fatalf("WriteNew: %v", err)
	}
	catalog, err := skills.Load(skills.Sources{Home: home})
	if err != nil {
		t.Fatalf("Load: %v\n%s", err, content)
	}
	skill, ok := catalog.Get(name)
	if !ok || skill.Recipe == nil {
		t.Fatalf("the saved skill %q did not load as a recipe:\n%s", name, content)
	}
	if skill.Summary == "" || skill.Body == "" {
		t.Errorf("the saved skill has summary %q and body %q; want both set", skill.Summary, skill.Body)
	}
	return skill
}

// bindScope is the parsed recipe with `{scope}` put back as the scope input's default — the plan a
// bare invocation runs.
func bindScope(t *testing.T, skill skills.Skill) []workflow.Stage {
	t.Helper()
	stages := skill.Recipe.Stages
	for _, decl := range skill.Inputs {
		if decl.Name != workflow.ScopeInput {
			continue
		}
		for index := range stages {
			if over := stages[index].Over; over != nil {
				bound := *over
				bound.Files = strings.ReplaceAll(bound.Files, "{scope}", decl.Default)
				bound.Lines = strings.ReplaceAll(bound.Lines, "{scope}", decl.Default)
				bound.Split = strings.ReplaceAll(bound.Split, "{scope}", decl.Default)
				stages[index].Over = &bound
			}
		}
	}
	return stages
}

// A fan_out's plan saves as a recipe that parses back to the same stages; a path source becomes the
// scope input defaulting to that path, and a literal list stays literal with no input.
func TestPlanToRecipeRoundTrips(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		source    workflow.ItemSource
		wantScope string
	}{
		{"files", workflow.ItemSource{Files: "internal/**/*.go", Batch: 2}, "internal/**/*.go"},
		{"lines", workflow.ItemSource{Lines: "notes/todo.txt"}, "notes/todo.txt"},
		{"split", workflow.ItemSource{Split: "internal/tui"}, "internal/tui"},
		{"list", workflow.ItemSource{List: []string{"a.go", "b c.go"}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := fanOutPlan(tc.source)
			skill := saveAndLoad(t, plan, "saved-"+tc.name)

			var scope *workflow.InputDecl
			for index := range skill.Inputs {
				if skill.Inputs[index].Name == workflow.ScopeInput {
					scope = &skill.Inputs[index]
				}
			}
			switch {
			case tc.wantScope == "" && len(skill.Inputs) != 0:
				t.Errorf("a literal list declared inputs %+v; want none", skill.Inputs)
			case tc.wantScope != "" && (scope == nil || scope.Default != tc.wantScope || scope.Required):
				t.Errorf("inputs = %+v; want an optional scope defaulting to %q", skill.Inputs, tc.wantScope)
			}
			if got := bindScope(t, skill); !reflect.DeepEqual(got, plan.Stages) {
				t.Errorf("the recipe runs\n%+v\nwant the plan's own stages\n%+v", got, plan.Stages)
			}
			if plan.Stages[0].Over.Files == "{scope}" || plan.Stages[0].Over.Split == "{scope}" {
				t.Error("PlanToRecipe rewrote the caller's plan")
			}
		})
	}
}

// A plan whose text already spells {scope} keeps its path literal: an input would rewrite that text.
func TestPlanToRecipeKeepsAPathWhenScopeIsSpelled(t *testing.T) {
	t.Parallel()
	plan := fanOutPlan(workflow.ItemSource{Files: "*.go"})
	plan.Stages[0].Task = "Check {item} within {scope}; write {out}."

	skill := saveAndLoad(t, plan, "spelled")
	if len(skill.Inputs) != 0 || !reflect.DeepEqual(skill.Recipe.Stages, plan.Stages) {
		t.Errorf("inputs %+v, stages %+v; want no input and the plan's stages verbatim", skill.Inputs, skill.Recipe.Stages)
	}
}

// A plan Validate refuses, or one reading a brief from a prompt file the skill folder would not
// hold, is refused rather than saved as a recipe that cannot run.
func TestPlanToRecipeRefusesAnUnrunnablePlan(t *testing.T) {
	t.Parallel()
	invalid := workflow.Plan{Stages: []workflow.Stage{{Name: "fanout", Kind: workflow.StageFanout}}}
	prompted := fanOutPlan(workflow.ItemSource{Files: "*.go"})
	prompted.Stages[0].Task, prompted.Stages[0].Prompt = "", "prompts/find.md"
	for name, plan := range map[string]workflow.Plan{"invalid": invalid, "prompt file": prompted} {
		if content, err := workflow.PlanToRecipe(plan); err == nil {
			t.Errorf("%s: PlanToRecipe saved\n%s\nwant a refusal", name, content)
		}
	}
}

// An existing name is refused and its folder left as it was; a name that is a path or carries a
// space is refused before anything is written.
func TestWriteNewRefusesAnExistingOrMalformedName(t *testing.T) {
	t.Parallel()
	content, err := workflow.PlanToRecipe(fanOutPlan(workflow.ItemSource{Files: "*.go"}))
	if err != nil {
		t.Fatal(err)
	}
	library := filepath.Join(t.TempDir(), "skills")
	if _, err := skills.WriteNew("mine", library, content); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(library, "mine", "SKILL.md")
	if err := os.WriteFile(kept, []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := skills.WriteNew("mine", library, content); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a second write of mine = %v; want an already-exists refusal", err)
	}
	if data, _ := os.ReadFile(kept); string(data) != "edited" {
		t.Errorf("the existing skill now holds %q; it was overwritten", data)
	}
	for _, name := range []string{"../x", "a/b", "two words", ".hidden", ""} {
		if dir, err := skills.WriteNew(name, library, content); err == nil {
			t.Errorf("WriteNew(%q) wrote %s; want a refusal", name, dir)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(library), "x")); !os.IsNotExist(err) {
		t.Errorf("../x reached outside the library: %v", err)
	}
}
