package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------------
// Saving a fan_out's plan as a Recipe
// ----------------------------------------------------------------------------
//
// A `fan_out` the model asked for is a one-off: its plan lives in the workflow folder and is gone
// with the session. PlanToRecipe is the way to keep one — it renders the plan as a SKILL.md whose
// `recipe:` header runs the same stages, so the human can invoke it by name and edit it like any
// other recipe (ADR 0087 D6). The rendering is data only: where the file goes, and the name rule it
// is written under, belong to the skills library the caller writes it into.

// ScopeInput is the input a saved recipe declares for its path source: the `files:`, `lines:` or
// `split:` the fan_out ran over becomes `{scope}`, defaulting to the path it had, so the recipe
// re-runs the same work bare and another path's with `scope=<path>`.
const ScopeInput = "scope"

// scopePlaceholder is ScopeInput as a stage field spells it.
const scopePlaceholder = "{" + ScopeInput + "}"

// recipeNameMaxRunes bounds how much of the workflow's name the saved skill's description quotes:
// a fan_out's name is the first line of its brief, and a description is a menu hint.
const recipeNameMaxRunes = 80

// recipeHeader is the saved SKILL.md's frontmatter, in the order a reader meets it. The skill's id
// is its folder's name, so the header carries none and a rename of the folder renames the skill.
type recipeHeader struct {
	Description string      `yaml:"description"`
	Inputs      []InputDecl `yaml:"inputs,omitempty"`
	Recipe      []Stage     `yaml:"recipe"`
}

// PlanToRecipe renders plan as the content of a recipe skill's SKILL.md: a description, the
// recipe's stages with every brief written inline (`task:`), and a short body saying what the skill
// is. Where the plan's first fanout takes its items from a path — a `files:` glob, a `lines:` file
// or a `split:` folder — that path becomes the `scope` input, defaulting to it; a literal list stays
// literal, and so does a path when a stage's text already spells `{scope}`, which an input would
// rewrite.
//
// It refuses a plan Validate refuses, and one whose stage reads its brief from a prompt file: the
// saved skill's folder holds no such file, so the recipe could not run.
func PlanToRecipe(plan Plan) ([]byte, error) {
	if problems := Validate(plan); len(problems) > 0 {
		return nil, fmt.Errorf("workflow: the plan cannot be saved as a recipe: %s", joinProblemText(problems))
	}
	header := recipeHeader{
		Description: recipeDescription(plan.Name),
		Recipe:      cloneStages(plan.Stages),
	}
	for _, stage := range header.Recipe {
		if stage.Prompt != "" {
			return nil, fmt.Errorf("workflow: stage %q reads its brief from %s; only a plan whose briefs are "+
				"written inline saves as a recipe", stage.Name, stage.Prompt)
		}
	}
	if decl, ok := scopeStage(header.Recipe); ok {
		header.Inputs = []InputDecl{decl}
	}

	var out bytes.Buffer
	out.WriteString("---\n")
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(header); err != nil {
		return nil, fmt.Errorf("workflow: render the recipe: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("workflow: render the recipe: %w", err)
	}
	out.WriteString("---\n\n")
	out.WriteString(recipeBody(plan.Name, len(header.Inputs) > 0))
	return out.Bytes(), nil
}

// scopeStage turns the path source of stages' first fanout into `{scope}` in place and returns the
// input that declares it; ok is false when there is no such source, or when a stage already spells
// `{scope}` (binding an input would rewrite that text too).
func scopeStage(stages []Stage) (InputDecl, bool) {
	if spellsScope(stages) {
		return InputDecl{}, false
	}
	for index := range stages {
		stage := &stages[index]
		if stage.Kind != StageFanout || stage.Over == nil {
			continue
		}
		var (
			field *string
			what  string
		)
		switch over := stage.Over; {
		case over.Files != "":
			field, what = &over.Files, "the files glob the items are matched by"
		case over.Lines != "":
			field, what = &over.Lines, "the file whose lines are the items"
		case over.Split != "":
			field, what = &over.Split, "the folder cut into the items"
		default:
			return InputDecl{}, false
		}
		original := *field
		*field = scopePlaceholder
		return InputDecl{
			Name:        ScopeInput,
			Default:     original,
			Description: what + ", workspace-relative",
		}, true
	}
	return InputDecl{}, false
}

// spellsScope reports whether any stage field already carries `{scope}`, read off the stages' own
// JSON so no field can be missed.
func spellsScope(stages []Stage) bool {
	encoded, err := json.Marshal(stages)
	return err != nil || bytes.Contains(encoded, []byte(scopePlaceholder))
}

// cloneStages copies stages deeply enough that scopeStage's rewrite never reaches the caller's plan.
func cloneStages(stages []Stage) []Stage {
	out := make([]Stage, len(stages))
	for index, stage := range stages {
		if stage.Over != nil {
			over := *stage.Over
			stage.Over = &over
		}
		out[index] = stage
	}
	return out
}

// recipeDescription is the saved skill's description: what it runs, quoting the workflow's name
// clipped to one short line.
func recipeDescription(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if runes := []rune(name); len(runes) > recipeNameMaxRunes {
		name = strings.TrimSpace(string(runes[:recipeNameMaxRunes])) + "…"
	}
	if name == "" {
		return "A recipe saved from a fan_out workflow."
	}
	return fmt.Sprintf("A recipe saved from the fan_out workflow %q.", name)
}

// recipeBody is the saved skill's Markdown body: what the skill is, and how its scope input is
// written when it has one.
func recipeBody(name string, scoped bool) string {
	title := strings.TrimSpace(strings.Join(strings.Fields(name), " "))
	if title == "" {
		title = "Saved workflow"
	}
	var body strings.Builder
	fmt.Fprintf(&body, "# %s\n\n", title)
	body.WriteString("This skill is a recipe saved from a `fan_out` workflow: invoking it runs the same stages " +
		"again as a workflow, each item in a fresh child.\n")
	if scoped {
		fmt.Fprintf(&body, "\nWith no text it runs over the path the workflow ran over; write `%s=<path>` "+
			"(or the path alone) to run it over another.\n", ScopeInput)
	}
	body.WriteString("\nEdit the `recipe:` header above to change what it does.\n")
	return body.String()
}

// joinProblemText renders a validator's problems as one line.
func joinProblemText(problems []Problem) string {
	texts := make([]string, len(problems))
	for index, problem := range problems {
		texts[index] = problem.String()
	}
	return strings.Join(texts, "; ")
}

// ReadFolderPlan reads the plan a workflow folder records — its plan.json — by the folder's path,
// the address a listing (Info.Dir) names it by, for a reader that holds no Store.
func ReadFolderPlan(dir string) (Plan, error) {
	var plan Plan
	return plan, readJSON(filepath.Join(dir, planFileName), &plan)
}
