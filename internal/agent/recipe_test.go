package agent

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/workflow"
)

// The recipe launch (ADR 0087 D6): each test drives a parent Agent whose skill resolver also
// serves recipes (fakeRecipes), over workflowResponder, which answers the parent and every item
// child from a script keyed by a word of its last user message.

// reviewBody is the review skill's body, which an attach — never a launch — puts in the message.
const reviewBody = "REVIEW SKILL BODY"

// fakeRecipes is a skill resolver that serves recipes too: every recipe is also a skill whose body
// an attach injects.
type fakeRecipes struct {
	recipes map[string]workflow.Recipe
}

func (f fakeRecipes) ResolveSkills(ids []string) []domain.ResolvedSkill {
	var out []domain.ResolvedSkill
	for _, id := range ids {
		if _, ok := f.recipes[id]; ok {
			out = append(out, domain.ResolvedSkill{ID: id, DisplayName: id, Body: reviewBody})
		}
	}
	return out
}

func (f fakeRecipes) Recipe(id string) (workflow.Recipe, bool) {
	recipe, ok := f.recipes[id]
	return recipe, ok
}

func (f fakeRecipes) RecipeIDs() []string {
	ids := make([]string, 0, len(f.recipes))
	for id := range f.recipes {
		ids = append(ids, id)
	}
	return ids
}

// reviewRecipe is a one-fanout recipe over alpha and beta whose brief names its required `scope`
// input, so a child's task shows the value it was bound to.
func reviewRecipe() workflow.Recipe {
	return workflow.Recipe{
		ID: "review",
		Plan: workflow.Plan{Name: "review", Stages: []workflow.Stage{{
			Name:    "items",
			Kind:    workflow.StageFanout,
			Task:    "check {item} in {scope}",
			Over:    &workflow.ItemSource{List: []string{"alpha", "beta"}},
			Returns: workflow.ReceiptSpec{"count": "int"},
		}}},
		Inputs: []workflow.InputDecl{{Name: "scope", Required: true, Description: "the folder to review"}},
		Dir:    "/skills/review",
	}
}

// scriptRecipe is a one-script recipe whose command runs a file of the shipped skill's folder.
func scriptRecipe(command string) workflow.Recipe {
	return workflow.Recipe{
		ID: "tally",
		Plan: workflow.Plan{Name: "tally", Stages: []workflow.Stage{{
			Name:    "count",
			Kind:    workflow.StageScript,
			Run:     command,
			Returns: workflow.ReceiptSpec{"count": "int"},
		}}},
		Dir:   "shipped:tally",
		Files: fstest.MapFS{"bin/count.sh": &fstest.MapFile{Data: []byte("echo COUNT=3\n")}},
	}
}

// recipeConfig is workflowConfig with recipes served through its skill resolver.
func recipeConfig(t *testing.T, sink domain.EventSink, recipes ...workflow.Recipe) domain.Config {
	t.Helper()
	cfg := workflowConfig(t, sink)
	served := fakeRecipes{recipes: map[string]workflow.Recipe{}}
	for _, recipe := range recipes {
		served.recipes[recipe.ID] = recipe
	}
	cfg.Skills = served
	return cfg
}

// reviewUpstream answers the review recipe's two children and then the parent, whose request is
// told apart by the "/review" its message opens with.
func reviewUpstream(scope string) *requestLog {
	return &requestLog{inner: (&workflowResponder{}).
		route("/review", nil, contentScript("reviewed")).
		route("check alpha in "+scope, nil, finishScript("f1", "alpha is fine")).
		route("check beta in "+scope, nil, finishScript("f2", "beta is fine"))}
}

// requestLog records the last user message of every request it forwards.
type requestLog struct {
	inner provider.Responder
	mu    sync.Mutex
	lasts []string
}

func (r *requestLog) Stream(ctx context.Context, req provider.Request) iter.Seq[provider.Delta] {
	r.mu.Lock()
	r.lasts = append(r.lasts, lastUserText(req))
	r.mu.Unlock()
	return r.inner.Stream(ctx, req)
}

// first is the first recorded message containing key.
func (r *requestLog) first(t *testing.T, key string) string {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, last := range r.lasts {
		if strings.Contains(last, key) {
			return last
		}
	}
	t.Fatalf("no request carried %q; saw %q", key, r.lasts)
	return ""
}

// runInput submits in to a and runs it to its boundary.
func runInput(t *testing.T, a *Agent, in domain.UserInput) domain.StepResult {
	t.Helper()
	if err := a.Submit(in); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

// wantResultLines fails unless message carries the review recipe's result lines after the user's
// line and no attached body.
func wantResultLines(t *testing.T, message, line string) {
	t.Helper()
	if !strings.HasPrefix(message, line+"\n\n") {
		t.Errorf("the message does not open with the user's line %q:\n%s", line, message)
	}
	for _, want := range []string{
		"recipe /review ran as a workflow:",
		"#1 alpha — ok — alpha is fine count=1",
		"#2 beta — ok — beta is fine count=1",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("the message lacks %q:\n%s", want, message)
		}
	}
	if strings.Contains(message, reviewBody) {
		t.Errorf("a launch attached the skill body:\n%s", message)
	}
}

// scriptedAsker answers every question with answer and keeps the questions.
type scriptedAsker struct {
	answer    string
	mu        sync.Mutex
	questions []domain.AskRequest
}

func (s *scriptedAsker) Ask(_ context.Context, req domain.AskRequest) (domain.AskAnswer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.questions = append(s.questions, req)
	return domain.AskAnswer{Text: s.answer}, nil
}

// shellTool stands in for the terminal tool: a subprocess tool whose run is the test's.
type shellTool struct {
	mu   sync.Mutex
	runs []string
	run  func(command string) domain.ToolResult
}

func (s *shellTool) Name() string            { return shellToolName }
func (s *shellTool) Description() string     { return "a shell" }
func (s *shellTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s *shellTool) ReadOnly() bool          { return false }
func (s *shellTool) Subprocess() bool        { return true }

func (s *shellTool) Execute(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return domain.ToolResult{}, err
	}
	s.mu.Lock()
	s.runs = append(s.runs, args.Command)
	s.mu.Unlock()
	result := s.run(args.Command)
	result.CallID = call.ID
	return result, nil
}

func TestRecipe_ALeadingReferenceLaunchesAndTheFirstRequestCarriesTheResultLines(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, &recordingSink{}, reviewRecipe())
	up := reviewUpstream("src")
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runInput(t, a, domain.UserInput{Text: "/review src", SkillIDs: []string{"review"}})

	wantResultLines(t, up.first(t, "/review"), "/review src")
	if folders := workflowFolders(t, cfg.ScratchDir); len(folders) != 1 {
		t.Errorf("workflow folders = %v, want the recipe's one", folders)
	}
}

func TestRecipe_StartRecipeSubmitsTheLaunch(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, &recordingSink{}, reviewRecipe())
	up := reviewUpstream("lib")
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	id, err := a.StartRecipe(context.Background(), RecipeLaunch{SkillID: "review", Text: "scope=lib"})
	if err != nil || id != "" {
		t.Fatalf("StartRecipe = %q, %v; want a foreground launch", id, err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	wantResultLines(t, up.first(t, "/review"), "/review scope=lib")
}

func TestRecipe_StartRecipeRefusals(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		launch RecipeLaunch
		want   string
	}{
		{"unknown id lists the recipes", RecipeLaunch{SkillID: "nope"}, `"nope" is not a recipe; the recipes are: review`},
		{"missing input with no asker", RecipeLaunch{SkillID: "review"}, "missing input: scope"},
		{"unknown key", RecipeLaunch{SkillID: "review", Text: "depth=2"}, "depth"},
		{"background", RecipeLaunch{SkillID: "review", Text: "src", Background: true}, errBackgroundRecipe.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, err := newAgent(recipeConfig(t, &recordingSink{}, reviewRecipe()), reviewUpstream("src"))
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}

			_, err = a.StartRecipe(context.Background(), tc.launch)

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("StartRecipe error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestRecipe_AMissingInputIsAskedOfTheUser(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, &recordingSink{}, reviewRecipe())
	asker := &scriptedAsker{answer: "pkg"}
	cfg.Asker = asker
	up := reviewUpstream("pkg")
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runInput(t, a, domain.UserInput{Text: "/review", SkillIDs: []string{"review"}})

	if len(asker.questions) != 1 || asker.questions[0].Question != "/review needs scope: the folder to review" {
		t.Fatalf("questions = %+v, want the one scope question", asker.questions)
	}
	wantResultLines(t, up.first(t, "/review"), "/review")
}

func TestRecipe_AMissingInputWithNoAskerRunsNothing(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	cfg := recipeConfig(t, sink, reviewRecipe())
	up := reviewUpstream("src")
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runInput(t, a, domain.UserInput{Text: "/review", SkillIDs: []string{"review"}})

	if got := up.first(t, "/review"); !strings.Contains(got, "recipe /review could not run: missing input: scope") {
		t.Errorf("the message does not say why the recipe did not run:\n%s", got)
	}
	if folders := workflowFolders(t, cfg.ScratchDir); len(folders) != 0 {
		t.Errorf("workflow folders = %v, want none", folders)
	}
}

func TestRecipe_AMidTextReferenceAttachesTheBody(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, &recordingSink{}, reviewRecipe())
	up := &requestLog{inner: (&workflowResponder{}).route("please", nil, contentScript("ok"))}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runInput(t, a, domain.UserInput{Text: "please follow /review here", SkillIDs: []string{"review"}})

	if got := up.first(t, "please"); !strings.Contains(got, reviewBody) || strings.Contains(got, "ran as a workflow") {
		t.Errorf("a mid-text reference did not attach the body alone:\n%s", got)
	}
	if folders := workflowFolders(t, cfg.ScratchDir); len(folders) != 0 {
		t.Errorf("workflow folders = %v, want none", folders)
	}
}

func TestRecipe_ADelegateLaunchesNothing(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, &recordingSink{}, reviewRecipe())
	a, err := newAgent(cfg, reviewUpstream("src"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.depth = 1

	message := a.composeUserMessage(context.Background(), 0,
		domain.UserInput{Text: "/review src", SkillIDs: []string{"review"}}, false)

	if !strings.Contains(message.Content, reviewBody) || strings.Contains(message.Content, "ran as a workflow") {
		t.Errorf("a delegate's recipe reference did not attach the body alone:\n%s", message.Content)
	}
	if folders := workflowFolders(t, cfg.ScratchDir); len(folders) != 0 {
		t.Errorf("workflow folders = %v, want none", folders)
	}
}

func TestRecipe_InterjectRefusesALaunch(t *testing.T) {
	t.Parallel()

	a, err := newAgent(recipeConfig(t, &recordingSink{}, reviewRecipe()), reviewUpstream("src"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.turns.openExchange()
	before := a.conv.Len()

	err = a.Interject(context.Background(), domain.UserInput{Text: "/review src", SkillIDs: []string{"review"}})

	if !errors.Is(err, errRecipeInterjection) {
		t.Errorf("Interject = %v, want errRecipeInterjection", err)
	}
	if a.conv.Len() != before {
		t.Errorf("a refused interjection changed the conversation")
	}
}

func TestRecipe_AScriptStageIsRefusedInPlanMode(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, &recordingSink{}, scriptRecipe("sh {{SKILL_DIR}}/bin/count.sh > /tmp/out"))
	cfg.Mode = domain.ModePlan
	shell := &shellTool{run: func(string) domain.ToolResult { return domain.ToolResult{Content: "COUNT=3\n"} }}
	_ = cfg.Tools.Register(shell)
	up := &requestLog{inner: (&workflowResponder{}).route("/tally", nil, contentScript("ok"))}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runInput(t, a, domain.UserInput{Text: "/tally", SkillIDs: []string{"tally"}})

	got := up.first(t, "/tally")
	if !strings.Contains(got, "script count: blocked") || !strings.Contains(got, "plan mode") {
		t.Errorf("the script stage was not refused by Plan mode:\n%s", got)
	}
	if len(shell.runs) != 0 {
		t.Errorf("the shell ran %q in Plan mode", shell.runs)
	}
}

func TestRecipe_AShippedScriptIsStagedAndRun(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, &recordingSink{}, scriptRecipe("sh {{SKILL_DIR}}/bin/count.sh {workflow_dir}"))
	cfg.Approver = &fakeApprover{decision: domain.ApprovalAllow}
	var staged string
	shell := &shellTool{run: func(command string) domain.ToolResult {
		fields := strings.Fields(command)
		data, err := os.ReadFile(fields[1])
		if err != nil || fields[1] != filepath.Join(fields[2], stagedSkillDir, "bin", "count.sh") {
			return domain.ToolResult{Content: "not staged: " + command, IsError: true}
		}
		staged = string(data)
		return domain.ToolResult{Content: domain.CwdLinePrefix + "/ws\nCOUNT=3\nsummary=counted three\n"}
	}}
	_ = cfg.Tools.Register(shell)
	up := &requestLog{inner: (&workflowResponder{}).route("/tally", nil, contentScript("ok"))}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runInput(t, a, domain.UserInput{Text: "/tally", SkillIDs: []string{"tally"}})

	if staged != "echo COUNT=3\n" {
		t.Errorf("staged script = %q, want the shipped file's content (runs %q)", staged, shell.runs)
	}
	if got := up.first(t, "/tally"); !strings.Contains(got, "script count: ok — counted three count=3") {
		t.Errorf("the script's receipt is not in the result lines:\n%s", got)
	}
}

func TestRecipe_ShellToolNameIsTheTerminals(t *testing.T) {
	t.Parallel()

	if got := tools.NewTerminal(t.TempDir(), nil).Name(); got != shellToolName {
		t.Errorf("terminal tool name = %q, want %q", got, shellToolName)
	}
}

func TestBindPlanInputs(t *testing.T) {
	t.Parallel()

	recipe := workflow.Recipe{ID: "r", Dir: "shipped:r", Plan: workflow.Plan{Stages: []workflow.Stage{
		{Name: "s", Kind: workflow.StageScript, Run: "sh x {scope} {focus}"},
		{Name: "f", Kind: workflow.StageFanout, Task: "look at {item} for {focus}", Prompt: "shipped:r/prompts/f.md",
			Over: &workflow.ItemSource{Files: "{scope}/*.go"}},
	}}}

	plan := bindPlanInputs(recipe, map[string]string{"scope": "internal", "focus": "error handling", "item": "x"})

	if plan.Name != "r" {
		t.Errorf("plan name = %q, want the recipe id", plan.Name)
	}
	if got, want := plan.Stages[0].Run, "sh x internal 'error handling'"; got != want {
		t.Errorf("run = %q, want %q", got, want)
	}
	if got, want := plan.Stages[1].Task, "look at {item} for error handling"; got != want {
		t.Errorf("task = %q, want %q", got, want)
	}
	if got, want := plan.Stages[1].Over.Files, "internal/*.go"; got != want {
		t.Errorf("over files = %q, want %q", got, want)
	}
	if got, want := plan.Stages[1].Prompt, "prompts/f.md"; got != want {
		t.Errorf("prompt = %q, want %q", got, want)
	}
	if recipe.Plan.Stages[1].Over.Files != "{scope}/*.go" {
		t.Errorf("binding rewrote the recipe's own plan")
	}
}

func TestShellQuote(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{"internal/agent", "internal/agent"},
		{"two words", "'two words'"},
		{"it's", `'it'\''s'`},
		{"", "''"},
		{"$(rm -rf /)", "'$(rm -rf /)'"},
	} {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
