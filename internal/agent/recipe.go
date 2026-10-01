package agent

// The RECIPE LAUNCH (ADR 0087 D6, D10): a human-written Recipe — the stage list a skill's header
// declares — started as a Workflow. A user input whose FIRST attached skill carries a recipe and
// whose text opens with that skill's "/<id>" launches it (recipeLaunch): composeUserMessage runs
// the recipe on the Step that opens the Exchange and lands the user's line followed by the
// workflow's result lines, so the model's first request reads both — in every Driver alike,
// because the launch is spelled in the UserInput the Driver already sends. A "/<id>" anywhere
// else, a skill without a recipe, a delegate's task and an interjection attach the body as before
// (an interjection that would launch one is refused instead). StartRecipe is the same launch for a
// Driver that holds a recipe id rather than a typed line: it binds the inputs first, asking the
// user for a missing required one, and submits the launch — or, for a background launch, hands the
// recipe to the background workflow manager (background.go) and submits nothing.
//
// The catalog reaches the loop through workflow.RecipeSource, read off Config.Skills, so the loop
// never imports internal/skills (ADR 0010). runRecipe is the core both launches share and
// fan_out's recipe form calls: it opens no Exchange of its own. It binds the inputs into the
// stages ({<input>} in a brief, a question, an item source, an output path, a context file or —
// shell-quoted — a script's command), so a run with other inputs is another workflow folder, and
// it runs the stages with a ScriptRunner that puts a script stage's command through this Agent's
// own `terminal` Resolution — the Mode and approval rules the model's shell calls obey — and an
// Asker over Config.Asker, the ask_user seam, on the one prompt slot.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/workflow"
)

// shellToolName is the `terminal` tool's name: a script stage's command is run as a call to it,
// so it crosses the same Resolution — Mode, guard, confinement and approval — a model's shell
// call does.
const shellToolName = "terminal"

// recipeCallTool is the tool name a recipe launch's workflow children are bracketed under in
// their phase events, where a fan_out's name its call; recipeSource is the Source of the error a
// launch that could not run reports.
const (
	recipeCallTool = "recipe"
	recipeSource   = "recipe"
)

// The placeholders a script stage's `run:` is rendered with as it runs, beside the recipe's own
// inputs: the skill's folder (a copy staged into the workflow folder for a shipped skill, whose
// `shipped:` address no shell can open), the workflow folder, and the split budget a part may hold.
const (
	workflowDirPlaceholder = "{workflow_dir}"
	partBytesPlaceholder   = "{part_bytes}"
)

// stagedSkillDir is where a shipped skill's scripts are staged inside the workflow folder.
const stagedSkillDir = "skill"

// The brief placeholders a stage's task is rendered with (workflow's renderBrief): an input by
// either name is left unbound, so neither is ever shadowed.
var reservedInputNames = []string{"item", "out"}

// The texts a launch lands after the user's line: the result lines of a run, or why it did not run.
const (
	recipeResultFormat  = "recipe /%s ran as a workflow:\n%s"
	recipeRefusalFormat = "recipe /%s could not run: %v"
	recipeQuestionLead  = "/%s needs %s"
)

// missingInputFormat is the error a required input with no value and no one to ask carries.
const missingInputFormat = "missing input: %s"

var (
	// errRecipeInterjection refuses an interjection that would launch a recipe: a Workflow opens an
	// Exchange of its own and cannot run inside one already open.
	errRecipeInterjection = errors.New("apogee: a recipe cannot start inside a running Exchange; send it once the agent is idle")
	// errNoRecipes is a launch on an Agent whose skill resolver serves no recipes.
	errNoRecipes = errors.New("apogee: no recipes are configured")
)

// exitCodeMarker reads the exit code off the terminal tool's failed result: `[exit code N`.
var exitCodeMarker = regexp.MustCompile(`\[exit code (-?\d+)`)

// skillFileRef matches a `{{SKILL_DIR}}/<path>` reference in a script's command — a file the
// command runs or reads from the skill's folder.
var skillFileRef = regexp.MustCompile(regexp.QuoteMeta(domain.SkillDirToken) + `/([^\s'"]+)`)

// shellSafeValue is a value that needs no quoting in either platform shell.
var shellSafeValue = regexp.MustCompile(`^[A-Za-z0-9_./:=@%+,-]+$`)

// RecipeLaunch is a Driver's request to start a recipe: the recipe skill's id, the user's text
// its inputs bind from (the line after "/<id>"), and whether it runs in the background. It is
// domain.RecipeLaunch, so a Driver's own Engine seam can name it without importing this package.
type RecipeLaunch = domain.RecipeLaunch

// StartRecipe starts the recipe launch names. It resolves the skill, binds its inputs from
// launch.Text — asking the user through Config.Asker for a required input the text left unbound,
// and failing with `missing input: <name>` when there is no one to ask — and submits the launch:
// the Exchange it opens runs the workflow on its first Step and hands the model the user's line
// plus the result lines. It fails on an unknown or recipe-less id (listing the recipes), an input
// error, and anything Submit refuses.
//
// A Background launch submits nothing: the recipe runs as a background workflow (background.go,
// ADR 0089) and the id it returns names it — the id is empty for a foreground launch. Its inputs
// are bound from the text alone, and a required one the text leaves unbound is refused as
// `missing input: <name>` rather than asked, since the conversation may be busy.
func (a *Agent) StartRecipe(ctx context.Context, launch RecipeLaunch) (string, error) {
	recipe, err := a.recipeByID(launch.SkillID)
	if err != nil {
		return "", err
	}
	if launch.Background {
		return a.startBackgroundRecipe(recipe, launch.Text)
	}
	inputs, err := a.bindRecipeInputs(ctx, recipe, launch.Text)
	if err != nil {
		return "", err
	}
	line := "/" + recipe.ID
	if text := strings.TrimSpace(launch.Text); text != "" {
		line += " " + text
	}
	return "", a.Submit(domain.UserInput{Text: line, SkillIDs: []string{recipe.ID}, RecipeInputs: inputs})
}

// recipeSource is the recipe port Config.Skills serves, nil when it serves none.
func (a *Agent) recipeSource() workflow.RecipeSource {
	source, _ := a.cfg.Skills.(workflow.RecipeSource)
	return source
}

// recipeByID resolves id to its recipe, or an error naming the recipes there are.
func (a *Agent) recipeByID(id string) (workflow.Recipe, error) {
	source := a.recipeSource()
	if source == nil {
		return workflow.Recipe{}, errNoRecipes
	}
	if recipe, ok := source.Recipe(id); ok {
		return recipe, nil
	}
	known := "none"
	if ids := source.RecipeIDs(); len(ids) > 0 {
		known = strings.Join(ids, ", ")
	}
	return workflow.Recipe{}, fmt.Errorf("apogee: %q is not a recipe; the recipes are: %s", id, known)
}

// recipeLaunch reports the recipe in launches: its first attached skill carries a recipe and its
// text opens with that skill's "/<id>" as a word of its own. A delegate never launches one — a
// workflow's children do not delegate (ADR 0087 D9) — so its task attaches the body as before.
func (a *Agent) recipeLaunch(in domain.UserInput) (workflow.Recipe, bool) {
	if a.isDelegate() || len(in.SkillIDs) == 0 {
		return workflow.Recipe{}, false
	}
	id := in.SkillIDs[0]
	rest, opens := strings.CutPrefix(strings.TrimLeft(in.Text, " \t\n"), "/"+id)
	if !opens || (rest != "" && !strings.ContainsAny(rest[:1], " \t\n")) {
		return workflow.Recipe{}, false
	}
	source := a.recipeSource()
	if source == nil {
		return workflow.Recipe{}, false
	}
	return source.Recipe(id)
}

// launchRecipe runs the recipe in opens and returns what the opening message carries after the
// user's line: the result lines, or — reported as an ErrorEvent too — why the recipe did not run.
// A run that produced result lines marks the opening as carrying them (carryRecipeResult), so a
// cancel keeps it; a refusal does not, and a cancelled refused launch is scrapped as before.
// The inputs are in.RecipeInputs when StartRecipe bound them, else bound from the text after
// "/<id>".
func (a *Agent) launchRecipe(ctx context.Context, turn int, in domain.UserInput, recipe workflow.Recipe) string {
	inputs := in.RecipeInputs
	if inputs == nil {
		text := strings.TrimPrefix(strings.TrimLeft(in.Text, " \t\n"), "/"+recipe.ID)
		bound, err := a.bindRecipeInputs(ctx, recipe, text)
		if err != nil {
			return "\n\n" + a.recipeRefusal(turn, recipe.ID, err)
		}
		inputs = bound
	}
	call := domain.ToolCall{ID: fmt.Sprintf("recipe-%s-%d", recipe.ID, turn), Tool: recipeCallTool}
	asked := recipeCall{id: recipe.ID, inputs: inputs, launch: recipeLaunchKind(in, recipe.ID)}
	result, fellBack, err := a.runRecipe(ctx, turn, call, asked)
	if err != nil {
		return "\n\n" + a.recipeRefusal(turn, recipe.ID, err)
	}
	// The workflow ran, so the opening carries its result: a cancel now keeps the opening (settle).
	a.turns.carryRecipeResult()
	return "\n\n" + fmt.Sprintf(recipeResultFormat, recipe.ID, workflowAnswer(result, fellBack, asked.launch))
}

// recipeLaunchKind is how the user launched recipe id through in: from StartRecipe when it bound
// the inputs (in.RecipeInputs), else by the "/<id>" line they typed, kept trimmed.
func recipeLaunchKind(in domain.UserInput, id string) workflowLaunch {
	if in.RecipeInputs != nil {
		return workflowLaunch{kind: launchStartRecipe, recipe: id}
	}
	return workflowLaunch{kind: launchTypedRecipe, recipe: id, line: strings.TrimSpace(in.Text)}
}

// recipeRefusal reports a launch that could not run and returns the line the model reads it by.
func (a *Agent) recipeRefusal(turn int, id string, err error) string {
	a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: recipeSource, Err: err.Error()})
	return fmt.Sprintf(recipeRefusalFormat, id, err)
}

// bindRecipeInputs binds text to recipe's inputs (workflow.BindInputs) and asks the user for each
// required input left unbound, in declared order. With no Asker, or an empty answer, the first
// such input fails the launch with `missing input: <name>`.
func (a *Agent) bindRecipeInputs(ctx context.Context, recipe workflow.Recipe, text string) (map[string]string, error) {
	values, missing, err := workflow.BindInputs(recipe.Inputs, text)
	if err != nil {
		return nil, err
	}
	for _, name := range missing {
		if a.cfg.Asker == nil {
			return nil, fmt.Errorf(missingInputFormat, name)
		}
		answer, err := a.askUser(ctx, recipeQuestion(recipe, name), nil)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", fmt.Sprintf(missingInputFormat, name), err)
		}
		if answer == "" {
			return nil, fmt.Errorf(missingInputFormat, name)
		}
		values[name] = answer
	}
	return values, nil
}

// recipeQuestion is the question a missing required input is asked by: the recipe, the input,
// and what the input is for when the recipe says.
func recipeQuestion(recipe workflow.Recipe, name string) string {
	question := fmt.Sprintf(recipeQuestionLead, recipe.ID, name)
	for _, decl := range recipe.Inputs {
		if decl.Name == name && decl.Description != "" {
			return question + ": " + decl.Description
		}
	}
	return question
}

// askUser puts question to the human through Config.Asker, holding the prompt slot the running
// Agent designates so an Approval and a question are never on screen at once. The caller checks
// that an Asker is configured.
func (a *Agent) askUser(ctx context.Context, question string, choices []string) (string, error) {
	slot := domain.PromptSlotFor(ctx, a.prompts)
	if err := slot.Acquire(ctx); err != nil {
		return "", err
	}
	defer slot.Release()
	answer, err := a.cfg.Asker.Ask(ctx, domain.AskRequest{Question: question, Choices: choices})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(answer.Text), nil
}

// runRecipe runs the recipe asked names as a Workflow over its inputs — keyed by name, the ones
// left out taking their defaults — with its item children on the seat asked names, and returns its
// result. It opens no Exchange: the recipe launch (seatConfigured) and fan_out's recipe form both
// call it from inside one. call and turn stamp the children's phase events as a fan_out call's do.
// fellBack reports whether any item child asked for the Sub-agent server and ran on the session one
// (seatFellBack), the fact the answer's SeatFallbackNote line rides (workflowAnswer).
// The error is a recipe that could not run: an unknown id, an unknown or missing input, no scratch
// dir or workspace, or a run the Runner could not proceed with.
func (a *Agent) runRecipe(
	ctx context.Context,
	turn int,
	call domain.ToolCall,
	asked recipeCall,
) (result workflow.Result, fellBack bool, err error) {
	recipe, err := a.recipeByID(asked.id)
	if err != nil {
		return workflow.Result{}, false, err
	}
	inputs, err := completeInputs(recipe.Inputs, asked.inputs)
	if err != nil {
		return workflow.Result{}, false, err
	}
	plan := bindPlanInputs(recipe, inputs)
	runner, err := a.newRecipeRunner(turn, call, recipe, asked.seat)
	if err != nil {
		return workflow.Result{}, false, err
	}
	observer := a.observeWorkflow(runner, turn, plan)
	observer.call = call.ID
	observer.resume = resumeCommand(asked.launch)
	result, err = runner.Run(ctx, plan)
	observer.end(result, err)
	return result, seatFellBack(runner), err
}

// completeInputs checks keyed inputs against the declarations: an undeclared key is an error, an
// unset or empty input takes its default, and a required input with neither is `missing input`.
func completeInputs(decls []workflow.InputDecl, given map[string]string) (map[string]string, error) {
	names := make([]string, 0, len(decls))
	for _, decl := range decls {
		names = append(names, decl.Name)
	}
	for key := range given {
		if !slices.Contains(names, key) {
			return nil, fmt.Errorf("unknown input %q; the inputs are: %s", key, strings.Join(names, ", "))
		}
	}
	values := make(map[string]string, len(decls))
	var missing []string
	for _, decl := range decls {
		value := given[decl.Name]
		if value == "" {
			value = decl.Default
		}
		if value == "" && decl.Required {
			missing = append(missing, decl.Name)
			continue
		}
		values[decl.Name] = value
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf(missingInputFormat, strings.Join(missing, ", "))
	}
	return values, nil
}

// bindPlanInputs is recipe's plan with every `{<input>}` replaced by its value — in a stage's
// task, question, default, item source, output path, context files and pick file as written, in a
// script's command shell-quoted — and each prompt path kept relative to the skill's folder, where
// the spawner reads it (recipe.Files). The plan is named after the recipe when it has no name.
func bindPlanInputs(recipe workflow.Recipe, inputs map[string]string) workflow.Plan {
	plain, quoted := inputReplacers(inputs)
	plan := workflow.Plan{Name: recipe.Plan.Name, Stages: make([]workflow.Stage, len(recipe.Plan.Stages))}
	if plan.Name == "" {
		plan.Name = recipe.ID
	}
	for index, stage := range recipe.Plan.Stages {
		stage.Task = plain.Replace(stage.Task)
		stage.Question = plain.Replace(stage.Question)
		stage.Default = plain.Replace(stage.Default)
		stage.Out = plain.Replace(stage.Out)
		stage.File = plain.Replace(stage.File)
		stage.Run = quoted.Replace(stage.Run)
		stage.Context = replaceAll(plain, stage.Context)
		if stage.Over != nil {
			over := *stage.Over
			over.List = replaceAll(plain, over.List)
			over.Files, over.Lines, over.Split = plain.Replace(over.Files), plain.Replace(over.Lines), plain.Replace(over.Split)
			stage.Over = &over
		}
		stage.Prompt = folderRelativePrompt(stage.Prompt, recipe.Dir)
		plan.Stages[index] = stage
	}
	return plan
}

// folderRelativePrompt is prompt relative to the skill folder dir: a prompt already
// folder-relative (the RecipeSource contract) is returned as is, and one an embedder spelled under
// dir — joined with "/" or, from a Windows host path, `\` — has that prefix stripped, so the
// spawner opens it through the recipe's Files on every OS.
func folderRelativePrompt(prompt, dir string) string {
	if dir == "" {
		return prompt
	}
	for _, separator := range []string{"/", `\`} {
		if rel, under := strings.CutPrefix(prompt, dir+separator); under {
			return rel
		}
	}
	return prompt
}

// inputReplacers are the two renderings of `{<input>}`: the value as written, and the value
// shell-quoted for a script's command. A reserved brief placeholder is never an input's.
func inputReplacers(inputs map[string]string) (plain, quoted *strings.Replacer) {
	var plainPairs, quotedPairs []string
	for name, value := range inputs {
		if slices.Contains(reservedInputNames, name) {
			continue
		}
		placeholder := "{" + name + "}"
		plainPairs = append(plainPairs, placeholder, value)
		quotedPairs = append(quotedPairs, placeholder, shellQuote(value))
	}
	return strings.NewReplacer(plainPairs...), strings.NewReplacer(quotedPairs...)
}

// replaceAll is replacer applied to every entry of list, as a new list.
func replaceAll(replacer *strings.Replacer, list []string) []string {
	if list == nil {
		return nil
	}
	out := make([]string, len(list))
	for index, entry := range list {
		out[index] = replacer.Replace(entry)
	}
	return out
}

// shellQuote is value as one shell word: unchanged when it holds nothing a shell reads specially,
// else single-quoted for POSIX sh.
func shellQuote(value string) string {
	if shellSafeValue.MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// newRecipeRunner builds the Runner a recipe runs under, its item children built on seat: fan_out's
// store, workspace, split budget, width, second chances and clock (newWorkflowRunner — the budget and
// width sized for the seat), children whose prompt files are read from the skill's folder — the
// folder the Runner also reads them from to key the items (Runner.Prompts) — this Agent's script
// runner, and — when a human can be asked — its Asker. It names the recipe, so the folder's
// status.json records where a re-run reads those files from (Agent.RerunFailed).
func (a *Agent) newRecipeRunner(turn int, call domain.ToolCall, recipe workflow.Recipe, seat delegationSeat) (*workflow.Runner, error) {
	scratch := a.ScratchDir()
	if scratch == "" {
		return nil, errors.New("this session has no scratch directory to keep the workflow in")
	}
	if a.cfg.WorkspaceDir == "" {
		return nil, errors.New("this session has no workspace to read the items from")
	}
	store, err := workflow.NewStore(scratch)
	if err != nil {
		return nil, err
	}
	split := workflow.NewSplitBudget(a.workflowContextLimitOn(seat))
	spawner := a.newWorkflowSpawner(turn, call, recipe.Files)
	spawner.seat = seat
	runner := &workflow.Runner{
		Spawner:       spawner,
		Store:         store,
		Workspace:     os.DirFS(a.cfg.WorkspaceDir),
		Prompts:       recipe.Files,
		Split:         split,
		Width:         a.workflowWidthOn(seat),
		Retries:       a.cfg.Workflow.ResolvedRetries(),
		Continuations: a.cfg.Workflow.ResolvedContinuations(),
		Scripts:       &recipeScripts{agent: a, turn: turn, recipe: recipe, split: split},
		Recipe:        recipe.ID,
		Now:           a.now,
	}
	if a.cfg.Asker != nil {
		runner.Asker = recipeAsker{agent: a}
	}
	return runner, nil
}

// recipeAsker puts an `ask` stage's question to the human through the Agent's Asker.
type recipeAsker struct {
	agent *Agent
}

// Ask asks the question with its options offered as choices (workflow.Asker).
func (r recipeAsker) Ask(ctx context.Context, question workflow.Question) (string, error) {
	return r.agent.askUser(ctx, question.Text, question.Options)
}

// recipeScripts runs a recipe's script stages (workflow.ScriptRunner) as `terminal` calls of the
// Agent running the recipe, at the Turn that started it.
type recipeScripts struct {
	agent  *Agent
	turn   int
	recipe workflow.Recipe
	split  workflow.SplitBudget
}

// RunScript renders the stage's command and runs it through the Agent's terminal Resolution: a
// refused or denied command is an error (the stage ends blocked), one that ran reports its output
// and exit code. The call carries the stage's workflow folder, the one place a Plan-mode script
// may write (runScriptCall).
func (s *recipeScripts) RunScript(ctx context.Context, spec workflow.ScriptSpec) (workflow.ScriptOutput, error) {
	command, err := s.render(spec)
	if err != nil {
		return workflow.ScriptOutput{}, err
	}
	arguments, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		return workflow.ScriptOutput{}, err
	}
	call := domain.ToolCall{
		ID:        fmt.Sprintf("recipe-script-%s-%s", spec.Workflow, spec.Stage),
		Tool:      shellToolName,
		Arguments: arguments,
	}
	result, err := s.agent.runScriptCall(ctx, s.turn, call, spec.Dir)
	if err != nil {
		return workflow.ScriptOutput{}, err
	}
	return scriptOutput(result)
}

// render fills the command's run-time placeholders: {{SKILL_DIR}} (staging each file it names for
// a shipped skill), {workflow_dir} and {part_bytes}.
func (s *recipeScripts) render(spec workflow.ScriptSpec) (string, error) {
	command := spec.Command
	if strings.Contains(command, domain.SkillDirToken) {
		dir, err := s.skillDir(command, spec.Dir)
		if err != nil {
			return "", err
		}
		command = strings.ReplaceAll(command, domain.SkillDirToken, shellQuote(dir))
	}
	return strings.NewReplacer(
		workflowDirPlaceholder, shellQuote(spec.Dir),
		partBytesPlaceholder, strconv.Itoa(int(s.split)),
	).Replace(command), nil
}

// skillDir is the folder a command's {{SKILL_DIR}} names: the skill's own for one on disk, and for
// a shipped skill a folder in the workflow's, into which every `{{SKILL_DIR}}/<path>` the command
// names is copied first — the embedded tree has no host path a shell could run a script from.
func (s *recipeScripts) skillDir(command, workflowDir string) (string, error) {
	if filepath.IsAbs(s.recipe.Dir) {
		return s.recipe.Dir, nil
	}
	if s.recipe.Files == nil {
		return "", fmt.Errorf("the skill folder %s cannot be read", s.recipe.Dir)
	}
	staged := filepath.Join(workflowDir, stagedSkillDir)
	for _, match := range skillFileRef.FindAllStringSubmatch(command, -1) {
		if err := stageSkillFile(s.recipe.Files, match[1], staged); err != nil {
			return "", err
		}
	}
	return staged, nil
}

// stageSkillFile copies the skill folder's file rel into dest, keeping its path under the folder.
func stageSkillFile(files fs.FS, rel, dest string) error {
	clean := path.Clean(rel)
	if !fs.ValidPath(clean) {
		return fmt.Errorf("%s/%s is not a file inside the skill folder", domain.SkillDirToken, rel)
	}
	data, err := fs.ReadFile(files, clean)
	if err != nil {
		return fmt.Errorf("stage %s: %w", clean, err)
	}
	target := filepath.Join(dest, filepath.FromSlash(clean))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("stage %s: %w", clean, err)
	}
	if err := os.WriteFile(target, data, 0o700); err != nil {
		return fmt.Errorf("stage %s: %w", clean, err)
	}
	return nil
}

// runScriptCall runs call — a `terminal` call the engine built, never one a model sent — through
// the leaf half of the dispatch pipeline: the guard, the Resolution and the gate stage decide it
// exactly as they decide a model's call (prepareCall), and the verdict's arm runs it (runCall).
// No ToolCallEvent, Moment or history entry is produced: the call is the workflow's, reported
// through its stage. A refusal is the error.
//
// workflowDir is the stage's workflow folder. The Resolution is told it (workflowScriptDir), and
// it is the one difference from a model's call: in Plan, which refuses a model's terminal call,
// the stage runs confined with that folder as its only writable root, or is refused where nothing
// can confine it (ADR 0012 amendment 2026-09-27). Every other mode reads nothing from it.
func (a *Agent) runScriptCall(ctx context.Context, turn int, call domain.ToolCall, workflowDir string) (domain.ToolResult, error) {
	tool, ok := a.lookupTool(call.Tool)
	if !ok {
		return domain.ToolResult{}, fmt.Errorf("no %s tool is registered to run it", call.Tool)
	}
	guard := a.tightenForStagedSecrets(ctx, call, a.guards.PreExecute(call, tool, a.guardExemptions()))
	in := a.resolutionInput(tool, call, guard)
	in.workflowScriptDir = workflowDir
	verdict := a.applyGates(ctx, turn, call, resolve(in))
	var result domain.ToolResult
	switch verdict.kind {
	case resolveRefuse:
		return domain.ToolResult{}, errors.New(a.executeRefuse(turn, call, verdict).Content)
	case resolveGate:
		result, _ = a.executeGate(ctx, turn, tool, call, verdict)
	case resolveConfine:
		result, _ = a.executeConfine(ctx, turn, tool, call, verdict)
	default:
		result, _ = a.executeRun(ctx, turn, tool, call, verdict)
	}
	return result, nil
}

// scriptOutput reads a terminal result as a script's output: the text without the leading cwd
// line, and the exit code its failure marker names (0 for a success). A failed result with no
// marker — a denial, a command that could not parse — is a script that could not run.
func scriptOutput(result domain.ToolResult) (workflow.ScriptOutput, error) {
	stdout := result.Content
	if rest, found := strings.CutPrefix(stdout, domain.CwdLinePrefix); found {
		_, stdout, _ = strings.Cut(rest, "\n")
	}
	if !result.IsError {
		return workflow.ScriptOutput{Stdout: stdout}, nil
	}
	matches := exitCodeMarker.FindAllStringSubmatch(stdout, -1)
	if len(matches) == 0 {
		return workflow.ScriptOutput{}, errors.New(firstLineOf(result.Content))
	}
	code, err := strconv.Atoi(matches[len(matches)-1][1])
	if err != nil {
		return workflow.ScriptOutput{}, errors.New(firstLineOf(result.Content))
	}
	return workflow.ScriptOutput{Stdout: stdout, ExitCode: code}, nil
}

// firstLineOf is text's first line.
func firstLineOf(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}
