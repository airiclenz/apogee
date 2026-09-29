package agent

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
// When the call carries a confinement handle, it hands the backend a command and records the box,
// as the real terminal does before it spawns: a backend that cannot confine is the error it
// returns, and the command does not run.
type shellTool struct {
	mu    sync.Mutex
	runs  []string
	boxes []domain.ConfinementBox
	run   func(command string) domain.ToolResult
}

func (s *shellTool) Name() string            { return shellToolName }
func (s *shellTool) Description() string     { return "a shell" }
func (s *shellTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s *shellTool) ReadOnly() bool          { return false }
func (s *shellTool) Subprocess() bool        { return true }

func (s *shellTool) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return domain.ToolResult{}, err
	}
	if conf, ok := domain.ConfinementFromContext(ctx); ok {
		if err := conf.Confiner.Confine(ctx, conf.Box, exec.Command("sh", "-c", args.Command)); err != nil {
			return domain.ToolResult{}, err
		}
		s.mu.Lock()
		s.boxes = append(s.boxes, conf.Box)
		s.mu.Unlock()
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
		{"background missing input", RecipeLaunch{SkillID: "review", Background: true}, "missing input: scope"},
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

// runScriptRecipe runs the tally recipe under cfg's mode and confiner with a shell that prints
// COUNT=3 and an Approver that allows, and returns the config, the shell and the result lines the
// parent's first request carried.
func runScriptRecipe(t *testing.T, mode domain.Mode, confiner domain.Confiner) (domain.Config, *shellTool, string) {
	t.Helper()
	cfg := recipeConfig(t, &recordingSink{}, scriptRecipe("sh {{SKILL_DIR}}/bin/count.sh {workflow_dir}"))
	cfg.Mode = mode
	cfg.Confiner = confiner
	cfg.ConfineToWorkspace = true
	cfg.Approver = &gateApprover{decision: domain.ApprovalAllow}
	shell := &shellTool{run: func(string) domain.ToolResult { return domain.ToolResult{Content: "COUNT=3\n"} }}
	_ = cfg.Tools.Register(shell)
	up := &requestLog{inner: (&workflowResponder{}).route("/tally", nil, contentScript("ok"))}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runInput(t, a, domain.UserInput{Text: "/tally", SkillIDs: []string{"tally"}})
	return cfg, shell, up.first(t, "/tally")
}

// TestRecipe_AScriptStageInPlanModeRunsOnlyConfined pins both halves of the Plan rule (ADR 0012
// amendment 2026-09-27): with a backend the stage runs inside a box whose one writable root is its
// workflow folder; with none — or a box that cannot be established at run time — it is refused
// and the shell never runs, whatever the Approver would have said.
func TestRecipe_AScriptStageInPlanModeRunsOnlyConfined(t *testing.T) {
	t.Parallel()

	t.Run("a backend runs it boxed to the workflow folder", func(t *testing.T) {
		t.Parallel()
		cfg, shell, got := runScriptRecipe(t, domain.ModePlan, &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}})
		if !strings.Contains(got, "script count: ok") {
			t.Fatalf("the script stage did not run in Plan mode:\n%s", got)
		}
		if len(shell.boxes) != 1 {
			t.Fatalf("the shell ran %d times confined, want once (runs %q)", len(shell.boxes), shell.runs)
		}
		box := shell.boxes[0]
		folder := box.WorkspaceRoot
		if filepath.Dir(folder) != filepath.Join(cfg.ScratchDir, "workflows") || box.ScratchDir != folder || len(box.WritablePaths) != 0 {
			t.Errorf("box = %+v, want the workflow folder under %s as its one writable root", box, cfg.ScratchDir)
		}
		if !strings.Contains(shell.runs[0], folder) {
			t.Errorf("the command %q does not name the box's folder %s", shell.runs[0], folder)
		}
	})

	for name, confiner := range map[string]domain.Confiner{
		"no backend refuses it":                    nil,
		"a box that cannot be established refuses": &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}, unavailable: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, shell, got := runScriptRecipe(t, domain.ModePlan, confiner)
			if !strings.Contains(got, "script count: blocked") || !strings.Contains(got, "plan mode runs a recipe script only inside a sandbox") {
				t.Errorf("the script stage was not refused for want of a sandbox:\n%s", got)
			}
			if len(shell.runs) != 0 {
				t.Errorf("the shell ran %q in Plan mode", shell.runs)
			}
		})
	}
}

// TestRecipe_AnAutoScriptStageKeepsItsBoxAndDemote pins that the Plan rule reads nothing outside
// Plan: in Auto a script stage is confined to the workspace and scratch dir as a model's terminal
// call is, and a box that cannot be established demotes to the forced gate, whose allow re-runs it
// unconfined.
func TestRecipe_AnAutoScriptStageKeepsItsBoxAndDemote(t *testing.T) {
	t.Parallel()

	t.Run("confined to the workspace and scratch dir", func(t *testing.T) {
		t.Parallel()
		cfg, shell, got := runScriptRecipe(t, domain.ModeAuto, &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}})
		if !strings.Contains(got, "script count: ok") || len(shell.boxes) != 1 {
			t.Fatalf("the Auto script stage did not run confined once (boxes %+v):\n%s", shell.boxes, got)
		}
		box := shell.boxes[0]
		if box.WorkspaceRoot != cfg.WorkspaceDir || box.ScratchDir != cfg.ScratchDir || !slices.Contains(box.WritablePaths, cfg.ScratchDir) {
			t.Errorf("box = %+v, want the workspace %s with the scratch dir %s writable", box, cfg.WorkspaceDir, cfg.ScratchDir)
		}
	})

	t.Run("a failed box demotes to the forced gate", func(t *testing.T) {
		t.Parallel()
		cfg, shell, got := runScriptRecipe(t, domain.ModeAuto, &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}, unavailable: true})
		if !strings.Contains(got, "script count: ok") || len(shell.runs) != 1 || len(shell.boxes) != 0 {
			t.Errorf("the demoted stage did not re-run unconfined on allow (runs %q, boxes %+v):\n%s", shell.runs, shell.boxes, got)
		}
		if approver := cfg.Approver.(*gateApprover); len(approver.requests) != 1 || approver.requests[0].Reason != confineDemoteGateReason {
			t.Errorf("approvals = %+v, want the one forced demote gate", approver.requests)
		}
	})
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

// parentRequests is how many recorded requests were the launching parent's — told apart by the
// "/review" its opening message starts with.
func (r *requestLog) parentRequests() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, last := range r.lasts {
		if strings.HasPrefix(last, "/review") {
			count++
		}
	}
	return count
}

// cancelledReviewLaunch launches the review recipe on a over up with ctx, whose every item child
// cancels the run, and settles the stopped Exchange. It returns whether the settle dropped it.
func cancelledReviewLaunch(t *testing.T, a *Agent, up *requestLog) bool {
	t.Helper()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	inner := up.inner.(*workflowResponder)
	inner.route("check alpha in src", cancelWith(cancel, nil), cancelledScript()).
		route("check beta in src", cancelWith(cancel, nil), cancelledScript())
	if err := a.Submit(domain.UserInput{Text: "/review src", SkillIDs: []string{"review"}}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusCancelled {
		t.Fatalf("launch result = %+v, want a cancel", res)
	}
	return a.SettleExchange()
}

// TestCancelledRecipeLaunchKeepsItsOpening pins the cancel trace of a recipe launch: the opening
// holding the user's line and the stopped result lines stays in the conversation with the
// cancelled note, the settle reports it kept, and no model request follows the stopped run.
func TestCancelledRecipeLaunchKeepsItsOpening(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, &recordingSink{}, reviewRecipe())
	up := &requestLog{inner: &workflowResponder{}}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if dropped := cancelledReviewLaunch(t, a, up); dropped {
		t.Error("SettleExchange dropped the cancelled launch, want its opening kept")
	}

	messages := a.conv.Messages()
	if len(messages) != 1 {
		t.Fatalf("conversation holds %d messages, want the opening alone: %+v", len(messages), messages)
	}
	last := messages[0]
	if last.Role != domain.RoleUser {
		t.Fatalf("last message role = %q, want the user opening", last.Role)
	}
	for _, want := range []string{"/review src", "recipe /review ran as a workflow:", "stopped by the user:", cancelledNoteLine} {
		if !strings.Contains(last.Content, want) {
			t.Errorf("the opening lacks %q:\n%s", want, last.Content)
		}
	}
	if n := up.parentRequests(); n != 0 {
		t.Errorf("the provider saw %d parent requests, want none after a stopped launch", n)
	}
}

// TestCancelledPlainExchangeStillAborts pins that the kept opening is a recipe launch's alone: a
// plain Exchange cancelled with no tool result, on the same Agent after a cancelled launch, is
// still scrapped — a recipe flag leaked across the Exchange would keep it.
func TestCancelledPlainExchangeStillAborts(t *testing.T) {
	t.Parallel()

	cfg := recipeConfig(t, &recordingSink{}, reviewRecipe())
	up := &requestLog{inner: &workflowResponder{}}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	cancelledReviewLaunch(t, a, up)
	before := a.conv.Len()

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	up.inner.(*workflowResponder).route("just a question", cancelWith(cancel, nil), cancelledScript())
	if res := runSubmitted(t, ctx, a, "just a question"); res.Status != domain.StatusCancelled {
		t.Fatalf("plain result = %+v, want a cancel", res)
	}

	if dropped := a.SettleExchange(); !dropped {
		t.Error("SettleExchange kept a plain Exchange with no tool result, want it scrapped")
	}
	if got := a.conv.Len(); got != before {
		t.Errorf("conversation holds %d messages, want the %d before the plain Exchange", got, before)
	}
}

// auditRecipe is a one-fanout recipe over alpha and beta with two positional inputs, so a typed
// line like `/audit internal/mcp security` binds both.
func auditRecipe() workflow.Recipe {
	return workflow.Recipe{
		ID: "audit",
		Plan: workflow.Plan{Name: "audit", Stages: []workflow.Stage{{
			Name:    "items",
			Kind:    workflow.StageFanout,
			Task:    "check {item} in {scope} for {lens}",
			Over:    &workflow.ItemSource{List: []string{"alpha", "beta"}},
			Returns: workflow.ReceiptSpec{"count": "int"},
		}}},
		Inputs: []workflow.InputDecl{{Name: "scope", Required: true}, {Name: "lens", Required: true}},
		Dir:    "/skills/audit",
	}
}

// TestStoppedRecipeAnswerCarriesResumeLine pins the resume line a cancelled recipe launch's result
// lines end on: the user's typed line for a typed launch, the recipe's id for a StartRecipe one.
func TestStoppedRecipeAnswerCarriesResumeLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		launch func(a *Agent) error
		want   string
	}{
		{
			name: "typed line",
			launch: func(a *Agent) error {
				return a.Submit(domain.UserInput{Text: "  /audit internal/mcp security \n", SkillIDs: []string{"audit"}})
			},
			want: "to resume: re-run `/audit internal/mcp security` — finished items are kept",
		},
		{
			name: "StartRecipe",
			launch: func(a *Agent) error {
				_, err := a.StartRecipe(context.Background(), RecipeLaunch{SkillID: "audit", Text: "internal/mcp security"})
				return err
			},
			want: "to resume: run `/audit` again with the same inputs — finished items are kept",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			cfg := recipeConfig(t, &recordingSink{}, auditRecipe())
			up := (&workflowResponder{}).
				route("check alpha in internal/mcp for security", nil, finishScript("f1", "alpha is fine")).
				route("check beta in internal/mcp for security", cancelWith(cancel, nil), cancelledScript())
			a, err := newAgent(cfg, up)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			if err := tc.launch(a); err != nil {
				t.Fatalf("launch: %v", err)
			}
			if res, err := a.Run(ctx); err != nil || res.Status != domain.StatusCancelled {
				t.Fatalf("Run = %+v, %v; want a cancel", res, err)
			}
			a.SettleExchange()

			messages := a.conv.Messages()
			if len(messages) == 0 {
				t.Fatal("the conversation kept no opening")
			}
			opening := messages[0].Content
			if !strings.Contains(opening, "stopped by the user:") || !strings.Contains(opening, "\n"+tc.want+"\n") {
				t.Errorf("the opening = %q, want the stopped result lines with %q", opening, tc.want)
			}
		})
	}
}

// startedPhase is the one WorkflowStarted among events.
func startedPhase(t *testing.T, events []domain.Event) domain.WorkflowPhaseEvent {
	t.Helper()
	var started []domain.WorkflowPhaseEvent
	for _, phase := range workflowPhaseEvents(events) {
		if phase.Phase == domain.WorkflowStarted {
			started = append(started, phase)
		}
	}
	if len(started) != 1 {
		t.Fatalf("started phases = %d, want one", len(started))
	}
	return started[0]
}

// TestRecipeStartedEventCarriesResume pins the resume command a recipe launch's started phase
// carries for the user: the typed line for a typed launch, the recipe's id for a StartRecipe one.
func TestRecipeStartedEventCarriesResume(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		scope  string
		launch func(a *Agent) error
		want   string
	}{
		{
			name:  "typed line",
			scope: "src",
			launch: func(a *Agent) error {
				return a.Submit(domain.UserInput{Text: "  /review src \n", SkillIDs: []string{"review"}})
			},
			want: "re-run `/review src` to resume",
		},
		{
			name:  "StartRecipe",
			scope: "lib",
			launch: func(a *Agent) error {
				_, err := a.StartRecipe(context.Background(), RecipeLaunch{SkillID: "review", Text: "scope=lib"})
				return err
			},
			want: "run `/review` again with the same inputs to resume",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &recordingSink{}
			a, err := newAgent(recipeConfig(t, sink, reviewRecipe()), reviewUpstream(tc.scope))
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			if err := tc.launch(a); err != nil {
				t.Fatalf("launch: %v", err)
			}
			if _, err := a.Run(context.Background()); err != nil {
				t.Fatalf("Run: %v", err)
			}

			if got := startedPhase(t, sink.events).Resume; got != tc.want {
				t.Errorf("started Resume = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestFanOutStartedEventHasNoResume pins that a model's fan_out — plain or naming a recipe — starts
// its workflow with no resume command: the user never typed it, so there is nothing to re-run.
func TestFanOutStartedEventHasNoResume(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args string
	}{
		{name: "plain", args: seatArgsJSON("", false, "alpha")},
		{name: "recipe form", args: `{"recipe":"review","inputs":{"scope":"src"},"task":"","over":[]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &recordingSink{}
			cfg := recipeConfig(t, sink, reviewRecipe())
			up := recipeFanOutUpstream(tc.args).route("check alpha carefully", nil, finishScript("f3", "alpha is fine"))

			runWorkflowParent(t, context.Background(), cfg, up, "please run it")

			if got := startedPhase(t, sink.events).Resume; got != "" {
				t.Errorf("started Resume = %q, want none for a fan_out", got)
			}
		})
	}
}
