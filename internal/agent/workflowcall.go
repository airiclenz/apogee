package agent

// The BLOCKING WORKFLOW CALL (ADR 0087 D1, ADR 0088 D3): a model's `fan_out` call, run by the
// engine to its end inside the Turn that asked for it. Dispatch recognises the call (resolve's
// Workflow verdict), keeps it in the leaf group, and runs it here: the arguments become a
// workflow.Plan — one fanout, then at most one verify and one merge — checked by
// workflow.ValidateModelPlan, whose problems come back as one tool error naming each argument to
// fix. A call naming a `recipe` instead runs that Recipe (ADR 0087 D2) through the core the recipe
// launch shares (runBlocking, launch.go) with its keyed `inputs`: an unknown recipe is answered with
// the recipes there are, and a recipe beside any argument that describes a fan-out is refused
// before anything runs. A valid plan runs through a workflow.Runner over the session's `<scratch>/workflows/` store,
// its item children spawned through the recursion point (workflowspawn.go), its progress reported
// as WorkflowPhaseEvents (workflowObserver), and the call is answered with workflow.Format's result
// lines. The same call again finds the stored workflow by
// its plan hash and skips the items already finished — unless a background run still drives that
// folder, when the call is refused with workflowAlreadyRunningFormat naming it (admitBlocking).
//
// A cancel stops the running items and keeps every finished one on disk: the call is answered with
// `stopped by the user: K of N done`, the path of the item listing written so far and the line
// telling the model how to resume it (resumeHint), and dispatch settles the Turn on it. A stopped
// item child is never folded — the Runner restarts it fresh on resume and keeps no report of it —
// so the only folds under the cancel are those of the item children's own delegations, which
// foldStoppedChild holds to the inherited cancel bound and skips on a quit or a daemon's shutdown.
//
// A call setting `background` (ADR 0089 D1) hands the same plan — or the same recipe — to the
// background manager (background.go) and is answered at once with the workflow's id and status
// path, but only where this Agent's fan_out published that switch (offersBackground) and this Agent
// is the top-level one: everywhere else — a headless run, a daemon firing, a delegate — the call
// runs blocking, as if the switch were unset, because with no conversation to go on there is
// nothing for a background workflow to run beside.
//
// The WORKFLOW CONTROL CALL (ADR 0089 D4) is answered here too: `workflow{action}` is a placeholder
// dispatch answers itself (resolve's Workflow verdict, spawning nothing) — status reads the session's
// workflow folders (Agent.Workflows), stop is Agent.StopWorkflow, and message queues a note for one
// running background item's child through InterjectChild, addressed by the run id or item name the
// status listing shows.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/sanitize"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/workflow"
)

// The stage names a fan_out plan's three stages take. The fanout stage's problems name the call's
// top-level arguments; verify's and merge's are prefixed with the argument they sit under.
const (
	fanOutStageName = "items"
	verifyStageName = "verify"
	mergeStageName  = "merge"
)

// The model-facing refusals a fan_out call can take before any child runs. Each is the whole tool
// error, so it says what to do instead.
const (
	fanOutProblemsHead       = "fan_out was not run — fix these arguments and call it again:"
	fanOutArgumentsFormat    = "fan_out was not run: its arguments are not valid JSON (%v); send an object with task and over"
	fanOutOverFormat         = "fan_out was not run: over must be an array of strings or an object with one of files, lines or split (%v)"
	fanOutRecipeTypeFormat   = "fan_out was not run: recipe must be the name of a recipe (a string), got %s"
	fanOutRecipeMixedFormat  = "fan_out was not run: recipe runs a recipe's own stages, so it cannot be combined with %s; call it again with only recipe and inputs, or without recipe to describe the fan-out yourself"
	fanOutUnknownRecipe      = "fan_out was not run: %q is not a recipe; the recipes are: %s"
	fanOutRecipeInputsFormat = "fan_out was not run: inputs must be an object of name: text pairs (%v)"
	fanOutRecipeFailed       = "fan_out could not run recipe %s: %v"
	fanOutNoScratch          = "fan_out was not run: this session has no scratch directory to keep the workflow in"
	fanOutNoWorkspace        = "fan_out was not run: this session has no workspace to read the items from"
	fanOutRunFailedPrefix    = "fan_out could not run: "
	fanOutBackgroundFailed   = "fan_out could not start the workflow in the background: %v"
)

// The answer a background fan_out call gets at once (ADR 0089 D1): the workflow's id and name,
// whether it started or waits in line behind another on its server, its status path, and how the
// model hears of it again.
const (
	fanOutBackgroundStarted = "workflow %s started in the background: %s"
	fanOutBackgroundQueued  = "workflow %s queued in the background behind another workflow on its server: %s"
	fanOutBackgroundStatus  = "status: %s"
	fanOutBackgroundHint    = "You are woken with its result when it ends. Meanwhile " +
		`workflow{action: "status", id: "%[1]s"} checks on it and workflow{action: "stop", id: "%[1]s"} stops it.`
)

// workflowStatusFileName is the file a workflow's folder keeps its status in — the store's own
// status.json (internal/workflow's Store), named here so a background answer can hand the model
// the path it may read.
const workflowStatusFileName = "status.json"

// fanOutListingLineFormat is the line a stopped workflow's answer ends on when Format has not
// already named the item listing: the report so far, in Format's own `items:` spelling so the model
// reads one name for the one file.
const fanOutListingLineFormat = "items: %s"

// The resume lines a stopped workflow's answer carries after its listing line (resumeHint): the
// same launch again finds the stored workflow by its plan hash and keeps the finished items.
const (
	resumeTypedLineFormat   = "to resume: re-run `%s` — finished items are kept"
	resumeStartRecipeFormat = "to resume: run `/%s` again with the same inputs — finished items are kept"
	resumeFanOutLine        = "to resume: call fan_out again with the same arguments — finished items are kept"
)

// The resume command a recipe launch's started phase carries for the user (resumeCommand,
// domain.WorkflowPhaseEvent.Resume): the same launch facts resumeHint reads, worded for a person.
const (
	resumeCommandTypedFormat = "re-run `%s` to resume"
	resumeCommandStartFormat = "run `/%s` again with the same inputs to resume"
)

// launchKind is how a blocking Workflow was launched, the one fact its resume line is read from.
type launchKind int

const (
	// The zero value is a model's fan_out call, plain or naming a recipe.
	_ launchKind = iota
	// launchTypedRecipe is a recipe the user launched by typing its "/<id>" line.
	launchTypedRecipe
	// launchStartRecipe is a recipe StartRecipe launched with its inputs already bound.
	launchStartRecipe
)

// resumeHint is the line that tells the model how to resume a stopped workflow launched as launch.
func resumeHint(launch workflowLaunch) string {
	switch launch.kind {
	case launchTypedRecipe:
		return fmt.Sprintf(resumeTypedLineFormat, launch.line)
	case launchStartRecipe:
		return fmt.Sprintf(resumeStartRecipeFormat, launch.recipe.SkillID)
	default:
		return resumeFanOutLine
	}
}

// resumeCommand is the text telling the user how to resume a workflow launched as launch, carried
// on its started phase (domain.WorkflowPhaseEvent.Resume): the typed line or the recipe's id for a
// recipe launch, "" for a fan_out, which the user never typed.
func resumeCommand(launch workflowLaunch) string {
	switch launch.kind {
	case launchTypedRecipe:
		return fmt.Sprintf(resumeCommandTypedFormat, launch.line)
	case launchStartRecipe:
		return fmt.Sprintf(resumeCommandStartFormat, launch.recipe.SkillID)
	default:
		return ""
	}
}

// fanOutArgs is a fan_out call's arguments as the tool publishes them (tools.fanOutSchemaTemplate).
// `run_on` is read by fanOutSeat, `recipe` and `inputs`, the recipe form, by parseFanOutRecipe, and
// `background` by asksBackground.
type fanOutArgs struct {
	Task    string               `json:"task"`
	Over    json.RawMessage      `json:"over"`
	Batch   int                  `json:"batch"`
	Context []string             `json:"context"`
	Returns workflow.ReceiptSpec `json:"returns"`
	Out     string               `json:"out"`
	Verify  *fanOutVerifyArgs    `json:"verify"`
	Merge   *fanOutMergeArgs     `json:"merge"`
	Tools   []string             `json:"tools"`
}

// fanOutPlanFields are the fan_out arguments that describe a fan-out, in schema order: none of
// them may be set beside `recipe`, whose stages the recipe itself declares.
var fanOutPlanFields = []string{"task", "over", "batch", "context", "returns", "out", "verify", "merge", "tools"}

// emptyJSONValues are the argument values that count as unset when fan_out checks a recipe call
// for fan-out arguments, and a `recipe` for a value at all: a model that fills every field in with
// an empty value asked for nothing.
var emptyJSONValues = []string{"null", `""`, "[]", "{}", "0", "false"}

// fanOutVerifyArgs is fan_out's `verify` object: which items to check and what to look at.
type fanOutVerifyArgs struct {
	When string `json:"when"`
	Task string `json:"task"`
}

// fanOutMergeArgs is fan_out's `merge` object: the brief of the one child that writes the report.
type fanOutMergeArgs struct {
	Task string `json:"task"`
}

// fanOutSourceArgs is fan_out's `over` in its object form. Unknown keys are refused, so a
// misspelled source is named rather than read as no source at all.
type fanOutSourceArgs struct {
	Files string `json:"files"`
	Lines string `json:"lines"`
	Split string `json:"split"`
}

// isFanOutCall reports whether call asks for a Workflow through fan_out.
func isFanOutCall(call domain.ToolCall) bool {
	return call.Tool == tools.FanOutToolName
}

// isWorkflowControlCall reports whether call is a `workflow` control call (ADR 0089 D4).
func isWorkflowControlCall(call domain.ToolCall) bool {
	return call.Tool == tools.WorkflowToolName
}

// asksBackground reports whether a fan_out call's arguments set `background` true. Arguments that
// are not an object, or a `background` that is not a boolean, ask for nothing: the call runs
// blocking, and parseFanOutPlan reports what it cannot read.
func asksBackground(raw json.RawMessage) bool {
	var args struct {
		Background bool `json:"background"`
	}
	return json.Unmarshal(raw, &args) == nil && args.Background
}

// offersBackground reports whether a fan_out call on this Agent may start its workflow in the
// background: this is the top-level Agent — background workflows belong to it (startBackground) —
// and the fan_out on its menu published the switch, which only a Driver that offers background
// workflows lets it do (tools.HostTools.OffersBackground; ADR 0089 D1). Anywhere else a
// `background: true` that reaches dispatch runs blocking.
func (a *Agent) offersBackground() bool {
	if a.isDelegate() {
		return false
	}
	tool, ok := a.lookupTool(tools.FanOutToolName)
	if !ok {
		return false
	}
	fanOut, ok := tool.(*tools.FanOut)
	return ok && fanOut.OffersBackground()
}

// backgroundCallResult answers a fan_out call whose workflow was handed to the background manager:
// the workflow's id and name, whether it started or waits in line, its status path and how the model
// hears of it again — or, when it could not start, why.
func (a *Agent) backgroundCallResult(callID, id string, err error) domain.ToolResult {
	if err != nil {
		return errorToolResult(callID, fmt.Sprintf(fanOutBackgroundFailed, err))
	}
	lead := fanOutBackgroundStarted
	if queued, live := a.background.liveStates()[id]; live && queued {
		lead = fanOutBackgroundQueued
	}
	name, statusPath := "", ""
	if store, err := workflow.NewStore(a.ScratchDir()); err == nil {
		if status, err := store.ReadStatus(id); err == nil {
			name = status.Name
		}
		if dir, err := store.Dir(id); err == nil {
			statusPath = filepath.Join(dir, workflowStatusFileName)
		}
	}
	lines := []string{fmt.Sprintf(lead, id, oneLine(name))}
	if statusPath != "" {
		lines = append(lines, fmt.Sprintf(fanOutBackgroundStatus, statusPath))
	}
	lines = append(lines, fmt.Sprintf(fanOutBackgroundHint, id))
	return domain.ToolResult{CallID: callID, Content: strings.Join(lines, "\n")}
}

// runWorkflowCall runs the Workflow one fan_out call asks for and answers the call: the formatted
// result lines when it ran, a tool error the model can act on when it could not. The outcome is
// dispatchCancelled exactly when the user's cancel reached the dispatch — the answer is then the
// stopped result (or the not-run text, for a call the cancel reached before it started), which
// dispatch commits as it settles the Turn (settleCancelledLeaf) — and dispatchDone otherwise, the
// audit record booked here as a leaf arm books its own. A human's stop of one item child is not a
// cancel of the call: the item ends stopped and the answer says so.
func (a *Agent) runWorkflowCall(ctx context.Context, turn int, slot *dispatchSlot) (domain.ToolResult, dispatchOutcome) {
	call := slot.call
	if ctx.Err() != nil {
		return errorToolResult(call.ID, notRunCancelledContent), dispatchCancelled
	}
	result := a.workflowCallResult(ctx, turn, call)
	if ctx.Err() != nil {
		return result, dispatchCancelled
	}
	a.recordExecutedTrip(turn, call, slot.verdict, result)
	return result, dispatchDone
}

// workflowCallResult parses, checks and runs one fan_out call's Workflow and renders its answer —
// or, for a workflow control call, answers that. A fan_out call asking for the background where it
// may have it is started there and answered at once.
func (a *Agent) workflowCallResult(ctx context.Context, turn int, call domain.ToolCall) domain.ToolResult {
	if isWorkflowControlCall(call) {
		return a.workflowControlResult(call)
	}
	seat, refusal := a.fanOutSeat(call.Arguments)
	if refusal != "" {
		return errorToolResult(call.ID, refusal)
	}
	background := asksBackground(call.Arguments) && a.offersBackground()
	if recipe, inputs, refusal, isRecipe := parseFanOutRecipe(call.Arguments); isRecipe {
		if refusal != "" {
			return errorToolResult(call.ID, refusal)
		}
		launch := workflowLaunch{recipe: domain.RecipeLaunch{SkillID: recipe}, inputs: inputs, seat: seat, turn: turn, call: call}
		return a.recipeCallResult(ctx, launch, background)
	}
	plan, refusal := parseFanOutPlan(call.Arguments)
	if refusal != "" {
		return errorToolResult(call.ID, refusal)
	}
	if problems := workflow.ValidateModelPlan(plan); len(problems) > 0 {
		return errorToolResult(call.ID, fanOutProblemsText(problems))
	}
	launch := workflowLaunch{plan: plan, seat: seat, turn: turn, call: call}
	if background {
		id, err := a.startBackground(launch)
		if refusal := launchRefusal(""); errors.As(err, &refusal) {
			return errorToolResult(call.ID, string(refusal))
		}
		return a.backgroundCallResult(call.ID, id, err)
	}
	outcome, fellBack, err := a.runBlocking(ctx, launch)
	if refusal := launchRefusal(""); errors.As(err, &refusal) {
		return errorToolResult(call.ID, string(refusal))
	}
	if err != nil {
		return errorToolResult(call.ID, fanOutRunFailedPrefix+err.Error())
	}
	return domain.ToolResult{CallID: call.ID, Content: workflowAnswer(outcome, fellBack, launch)}
}

// recipeCallResult runs the recipe one fan_out call's launch names over its keyed inputs, on the
// seat the call named, and renders its answer: the result lines, the recipes there are for an
// unknown id, or why the recipe could not run (an unknown or missing input among them). With
// background set it starts the recipe in the background instead and answers at once.
func (a *Agent) recipeCallResult(ctx context.Context, launch workflowLaunch, background bool) domain.ToolResult {
	id, callID := launch.recipe.SkillID, launch.call.ID
	source := a.recipeSource()
	if source == nil {
		return errorToolResult(callID, fmt.Sprintf(fanOutUnknownRecipe, id, "none"))
	}
	recipe, ok := source.Recipe(id)
	if !ok {
		known := "none"
		if ids := source.RecipeIDs(); len(ids) > 0 {
			known = strings.Join(ids, ", ")
		}
		return errorToolResult(callID, fmt.Sprintf(fanOutUnknownRecipe, id, known))
	}
	if background {
		workflowID, err := a.startKeyedBackgroundRecipe(recipe, launch.inputs, launch.seat)
		return a.backgroundCallResult(callID, workflowID, err)
	}
	result, fellBack, err := a.runBlocking(ctx, launch)
	if err != nil {
		return errorToolResult(callID, fmt.Sprintf(fanOutRecipeFailed, id, err))
	}
	return domain.ToolResult{CallID: callID, Content: workflowAnswer(result, fellBack, launch)}
}

// parseFanOutRecipe reads a fan_out call that names a recipe: the recipe id and its inputs, or the
// refusal a recipe that is not a string, a recipe beside fan-out arguments, or inputs that are not
// name: text pairs, earn. isRecipe is false — and the call is an ordinary fan-out — when the
// arguments leave `recipe` unset or empty (emptyJSONValues) or are not a JSON object
// (parseFanOutPlan reports that). A `recipe` set to anything else is a recipe call, so a mistyped
// one is refused rather than run as the plain fan-out it never asked for.
func parseFanOutRecipe(raw json.RawMessage) (id string, inputs map[string]string, refusal string, isRecipe bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", nil, "", false
	}
	named, set := fields["recipe"]
	if !set || slices.Contains(emptyJSONValues, string(bytes.TrimSpace(named))) {
		return "", nil, "", false
	}
	if err := json.Unmarshal(named, &id); err != nil {
		return "", nil, fmt.Sprintf(fanOutRecipeTypeFormat, bytes.TrimSpace(named)), true
	}
	var mixed []string
	for _, name := range fanOutPlanFields {
		if value, set := fields[name]; set && !slices.Contains(emptyJSONValues, string(bytes.TrimSpace(value))) {
			mixed = append(mixed, name)
		}
	}
	if len(mixed) > 0 {
		return id, nil, fmt.Sprintf(fanOutRecipeMixedFormat, strings.Join(mixed, ", ")), true
	}
	if value, set := fields["inputs"]; set {
		if err := json.Unmarshal(value, &inputs); err != nil {
			return id, nil, fmt.Sprintf(fanOutRecipeInputsFormat, err), true
		}
	}
	return id, inputs, "", true
}

// workflowContextLimit is the working context ceiling an item child will run in, which a `split:`
// source sizes its parts to (workflow.NewSplitBudget): with a Delegation target latched, the one
// its binding gives a routed child (the target's window, else the session's, under the target's
// working bound); with none, this session's own Budget.ContextLimit — the window an unrouted child
// inherits.
func (a *Agent) workflowContextLimit() int {
	if target := a.delegationTarget(); target != nil {
		return workingLimit(target.binding().applyTo(a.cfg).Context)
	}
	return a.budget().ContextLimit
}

// workflowContextLimitOn is workflowContextLimit for item children built on seat: a session-seated
// child never reads the latch (newChildAgentOn), so it runs in this session's own window whatever
// is latched; every other seat reads the latch as workflowContextLimit does, which is also where a
// sub-agents-server ask with nothing latched falls back to the session's window.
func (a *Agent) workflowContextLimitOn(seat delegationSeat) int {
	if seat == seatSession {
		return a.budget().ContextLimit
	}
	return a.workflowContextLimit()
}

// workflowWidthOn is how many item children a workflow on seat runs at once: the cap of the server
// they run on (seatCap, delegationwidth.go) — the session server's for a session-seated workflow
// even with a target latched, else the latched target's, else the session's — floored at 1, and 1
// on a delegate either way.
func (a *Agent) workflowWidthOn(seat delegationSeat) int {
	if a.isDelegate() {
		return 1
	}
	return max(a.seatCap(seat), 1)
}

// fanOutSeat resolves the Delegation seat a fan_out call's `run_on` names, or the refusal an
// invalid one earns — sub_agent's own text (parseDelegationSeat), which names the two spellings. It
// is read only where this Agent's fan_out published `run_on` (seatChoosingFanOut), the rule
// runSubAgent applies to sub_agent: anywhere else the argument was never offered and is ignored,
// and the call runs on seatConfigured. Arguments that are not an object name no seat either;
// parseFanOutPlan reports them.
func (a *Agent) fanOutSeat(raw json.RawMessage) (delegationSeat, string) {
	if _, published := seatChoosingFanOut(a.tools); !published {
		return seatConfigured, ""
	}
	var args struct {
		RunOn json.RawMessage `json:"run_on"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || len(args.RunOn) == 0 {
		return seatConfigured, ""
	}
	var name string
	if err := json.Unmarshal(args.RunOn, &name); err != nil {
		// Not a string: named as written, so the refusal shows the model what it sent.
		name = string(bytes.TrimSpace(args.RunOn))
	}
	seat, err := parseDelegationSeat(name)
	if err != nil {
		return seatConfigured, err.Error()
	}
	return seat, ""
}

// parseFanOutPlan turns a fan_out call's arguments into the Plan they describe — the fanout stage,
// then a verify and a merge stage when the call asks for them — or the refusal naming what could
// not be read. The Plan is not checked here; ValidateModelPlan does that.
func parseFanOutPlan(raw json.RawMessage) (workflow.Plan, string) {
	var args fanOutArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return workflow.Plan{}, fmt.Sprintf(fanOutArgumentsFormat, err)
	}
	source, err := parseFanOutSource(args.Over, args.Batch)
	if err != nil {
		return workflow.Plan{}, fmt.Sprintf(fanOutOverFormat, err)
	}

	stages := []workflow.Stage{{
		Name:    fanOutStageName,
		Kind:    workflow.StageFanout,
		Task:    args.Task,
		Over:    source,
		Returns: args.Returns,
		Out:     args.Out,
		Context: args.Context,
		Tools:   args.Tools,
	}}
	if args.Verify != nil {
		stages = append(stages, workflow.Stage{
			Name: verifyStageName, Kind: workflow.StageVerify, When: args.Verify.When, Task: args.Verify.Task,
		})
	}
	if args.Merge != nil {
		stages = append(stages, workflow.Stage{Name: mergeStageName, Kind: workflow.StageMerge, Task: args.Merge.Task})
	}
	return workflow.Plan{Name: sanitize.FirstLine(args.Task), Stages: stages}, ""
}

// parseFanOutSource reads fan_out's `over` — an array of item strings, or an object naming one
// source — with batch grouping the items that many per child. An absent `over` is nil, which
// ValidateModelPlan reports as the missing items.
func parseFanOutSource(raw json.RawMessage, batch int) (*workflow.ItemSource, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var list []string
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return nil, err
		}
		return &workflow.ItemSource{List: list, Batch: batch}, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var object fanOutSourceArgs
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	return &workflow.ItemSource{Files: object.Files, Lines: object.Lines, Split: object.Split, Batch: batch}, nil
}

// fanOutProblemsText renders ValidateModelPlan's problems as the one tool error the model reads:
// a head saying the call did not run, then one line per problem naming the argument to fix.
func fanOutProblemsText(problems []workflow.Problem) string {
	lines := make([]string, 0, len(problems)+1)
	lines = append(lines, fanOutProblemsHead)
	for _, problem := range problems {
		lines = append(lines, "- "+fanOutArgumentPath(problem)+": "+problem.Message)
	}
	return strings.Join(lines, "\n")
}

// fanOutArgumentPath names the fan_out argument a problem sits in: the field itself for the fanout
// stage and the plan as a whole, `verify.<field>` or `merge.<field>` for the stages those objects
// describe.
func fanOutArgumentPath(problem workflow.Problem) string {
	switch {
	case problem.Stage == "" || problem.Stage == fanOutStageName:
		return problem.Field
	case problem.Field == "":
		return problem.Stage
	default:
		return problem.Stage + "." + problem.Field
	}
}

// workflowAnswer is the text a finished or stopped Workflow answers its call with: Format's result
// lines, for a stopped one the item listing written so far when Format has not named it and the
// line telling the model how to resume it (resumeHint, from how launch started it), and —
// when any item child asked for the Sub-agent server and ran on the session one (fellBack,
// seatFellBack) — SeatFallbackNote once, last, because that note is for the MODEL (ADR 0069
// decision 9) and the model reads this answer, not the items' phase results. A background run
// answers through its finish note instead, which carries the same line (finishNote).
func workflowAnswer(result workflow.Result, fellBack bool, launch workflowLaunch) string {
	text := workflow.Format(result)
	if result.Stopped() {
		if result.Listing != "" {
			if line := fmt.Sprintf(fanOutListingLineFormat, result.Listing); !strings.Contains(text, line) {
				text += "\n" + line
			}
		}
		text += "\n" + resumeHint(launch)
	}
	if fellBack {
		text += "\n" + SeatFallbackNote
	}
	return text
}

// seatFellBack reports whether any item child runner's spawner built fell back from the Sub-agent
// server to the session one (workflowSpawner.fellBack); false for any other Spawner.
func seatFellBack(runner *workflow.Runner) bool {
	spawner, ok := runner.Spawner.(*workflowSpawner)
	return ok && spawner.fellBack.Load()
}

// workflowObserver turns one Workflow run's Runner notifications into the WorkflowPhaseEvents this
// Agent emits (domain.WorkflowPhaseEvent), stamped with this Agent's own identity at the Turn that
// started the Workflow: started — with the plan's stage names — on the first notification that
// names the Workflow's id, a stage started and finished for each stage that begins and ends, an
// item started for each item child the spawner mints a run for (itemStarted), an item finished for
// each item that ends on a receipt, waiting while an `ask` stage's question is out, and — from end
// — finished, stopped or failed. The Runner serialises its Observer calls, but an ask, the run's
// end and every item child's start arrive from other goroutines, so mu guards the observer's state.
type workflowObserver struct {
	agent *Agent
	turn  int
	name  string
	// stages is the plan's stage names in order (domain.WorkflowPhaseEvent.Stages), and rounds the
	// most rounds each stage a repeat re-runs can run, its own included (…Rounds).
	stages []string
	rounds map[string]int
	// background marks every event a background workflow's observer emits (domain.
	// WorkflowPhaseEvent.Background); driveBackground sets it before the run starts.
	background bool
	// call is the id of the call the Workflow's item children are bracketed under
	// (domain.WorkflowPhaseEvent.Call); every caller of observeWorkflow sets it before the run
	// starts.
	call string
	// resume is the resume command the started phase carries (domain.WorkflowPhaseEvent.Resume):
	// wireLaunch sets it from a blocking launch's resumeCommand before the run starts; "" otherwise.
	resume string

	mu sync.Mutex
	id string // the Workflow's id, once started was emitted
	// runs is each item's runs so far, keyed by its place — never by its Key, which two items of
	// one stage can share.
	runs map[itemPlace]itemRuns
}

// itemPlace names one item of one round of one stage.
type itemPlace struct {
	stage       string
	repeatRound int
	index       int
}

// itemRuns is how many runs of one item have started and the run id of the latest, "" when none.
type itemRuns struct {
	attempts int
	run      string
}

// observeWorkflow installs a workflowObserver on runner — as its Observer, on its Spawner when that
// is this package's (so each item child's start is reported with its run id), and around its Asker
// when it has one, so an `ask` stage's question is reported before it is put — and returns it for
// the caller to end once Run returns. plan is the Workflow's plan: its name and its stages' names
// are what the events carry.
func (a *Agent) observeWorkflow(runner *workflow.Runner, turn int, plan workflow.Plan) *workflowObserver {
	observer := &workflowObserver{
		agent: a, turn: turn, name: plan.Name,
		stages: stageNames(plan), rounds: repeatedRounds(plan), runs: map[itemPlace]itemRuns{},
	}
	runner.Observer = observer
	if spawner, ok := runner.Spawner.(*workflowSpawner); ok {
		spawner.observer = observer
	}
	if runner.Asker != nil {
		runner.Asker = observedAsker{inner: runner.Asker, observer: observer}
	}
	return observer
}

// stageNames is plan's stage names in order.
func stageNames(plan workflow.Plan) []string {
	names := make([]string, len(plan.Stages))
	for index, stage := range plan.Stages {
		names[index] = stage.Name
	}
	return names
}

// repeatedRounds maps each stage a repeat stage re-runs to the most rounds it can run: its own run
// plus the repeat's `max:`, the larger when two repeats name it. A repeat with no cap adds nothing.
func repeatedRounds(plan workflow.Plan) map[string]int {
	rounds := map[string]int{}
	for _, stage := range plan.Stages {
		if stage.Kind == workflow.StageRepeat && stage.Max > 0 {
			rounds[stage.Repeat] = max(rounds[stage.Repeat], stage.Max+1)
		}
	}
	return rounds
}

// StagePhase reports a stage that began running, with its item count, and one that ended with how
// it ended — done, failed, stopped or skipped — each in its round.
func (o *workflowObserver) StagePhase(event workflow.StageEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.startLocked(event.Workflow)
	round := event.RepeatRound + 1
	switch event.Phase {
	case workflow.PhasePending:
		return
	case workflow.PhaseRunning:
		o.emitLocked(domain.WorkflowPhaseEvent{
			Phase: domain.WorkflowStageStarted, Stage: event.Stage, Round: round,
			Items: event.Items, Rounds: o.rounds[event.Stage],
		})
	default:
		o.emitLocked(domain.WorkflowPhaseEvent{
			Phase: domain.WorkflowStageFinished, Stage: event.Stage, Round: round,
			Outcome: stageOutcomes[event.Phase],
		})
	}
}

// stageOutcomes is how a stage ended, by the phase the Runner settled it in.
var stageOutcomes = map[workflow.Phase]domain.WorkflowStageOutcome{
	workflow.PhaseDone:    domain.WorkflowStageDone,
	workflow.PhaseFailed:  domain.WorkflowStageFailed,
	workflow.PhaseStopped: domain.WorkflowStageStopped,
	workflow.PhaseSkipped: domain.WorkflowStageSkipped,
}

// ItemPhase reports an item that ended on a receipt — a resumed one, which never ran, included —
// naming its latest run, "" when none began.
func (o *workflowObserver) ItemPhase(event workflow.ItemEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.startLocked(event.Workflow)
	if event.Phase != workflow.PhaseDone || event.Receipt == nil {
		return
	}
	runs := o.runs[itemPlace{stage: event.Stage, repeatRound: event.RepeatRound, index: event.Index}]
	o.emitLocked(domain.WorkflowPhaseEvent{
		Phase: domain.WorkflowItemFinished, Stage: event.Stage, Item: event.Label, Index: event.Index,
		ItemName: event.Name, Resumed: event.Attempt == 0, Receipt: event.Receipt.Domain(),
		Round: event.RepeatRound + 1, Run: runs.run, Attempt: runs.attempts,
	})
}

// itemStarted reports one run of an item beginning: the spawner calls it once it has minted the
// child's run id and before the child's own started phase. Every call is a new attempt of the item,
// a retry's and a continuation's alike.
func (o *workflowObserver) itemStarted(spec workflow.ItemSpec, run string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.startLocked(spec.Workflow)
	place := itemPlace{stage: spec.Stage.Name, repeatRound: spec.RepeatRound, index: spec.Index}
	runs := o.runs[place]
	runs.attempts++
	runs.run = run
	o.runs[place] = runs
	o.emitLocked(domain.WorkflowPhaseEvent{
		Phase: domain.WorkflowItemStarted, Stage: spec.Stage.Name, Item: spec.Item.Label, Index: spec.Index,
		ItemName: spec.Name, Round: spec.RepeatRound + 1, Run: run, Attempt: runs.attempts,
	})
}

// waiting reports an `ask` stage's question as it is put to the user.
func (o *workflowObserver) waiting(question workflow.Question) {
	o.waitingOn(question.Workflow, question.Stage, question.Text)
}

// waitingOn reports workflow id as waiting on the user: an `ask` stage's question (stage names it,
// detail is its text), or — for a background workflow — an approval one of its runs is gated on
// (no stage; detail names the tool).
func (o *workflowObserver) waitingOn(id, stage, detail string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.startLocked(id)
	o.emitLocked(domain.WorkflowPhaseEvent{Phase: domain.WorkflowWaiting, Stage: stage, Detail: detail})
}

// end reports how the run Run returned ended: failed when it returned an error after the Workflow
// started, else stopped or finished by the result's phase, carrying the result's item tally — the
// one the model's note reports. A run that failed before its Workflow had an id started nothing
// and reports nothing.
func (o *workflowObserver) end(result workflow.Result, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.startLocked(result.ID)
	if o.id == "" {
		return
	}
	switch {
	case err != nil:
		o.emitLocked(domain.WorkflowPhaseEvent{Phase: domain.WorkflowFailed, Detail: err.Error()})
	case result.Stopped():
		o.emitLocked(domain.WorkflowPhaseEvent{Phase: domain.WorkflowStopped, Tally: eventTally(result)})
	default:
		o.emitLocked(domain.WorkflowPhaseEvent{Phase: domain.WorkflowFinished, Tally: eventTally(result)})
	}
}

// eventTally is result's item tally (workflow.TallyOf) in the shape an end phase carries it.
func eventTally(result workflow.Result) *domain.WorkflowTally {
	tally := workflow.TallyOf(result)
	return &domain.WorkflowTally{OK: tally.OK, Partial: tally.Partial, Blocked: tally.Blocked, Unfinished: tally.Unfinished}
}

// startLocked emits started, with the stage names and the resume command, the first time a
// notification names the Workflow's id. The caller holds mu.
func (o *workflowObserver) startLocked(id string) {
	if o.id != "" || id == "" {
		return
	}
	o.id = id
	o.emitLocked(domain.WorkflowPhaseEvent{Phase: domain.WorkflowStarted, Stages: slices.Clone(o.stages), Resume: o.resume})
}

// emitLocked stamps event with this Agent's identity, the Workflow's id and name, and emits it.
// The caller holds mu.
func (o *workflowObserver) emitLocked(event domain.WorkflowPhaseEvent) {
	event.EventBase = o.agent.base(o.turn)
	event.Workflow, event.Name, event.Background, event.Call = o.id, o.name, o.background, o.call
	o.agent.cfg.Events.Emit(event)
}

// observedAsker reports an `ask` stage's question as waiting before it puts the question through
// the Asker it wraps.
type observedAsker struct {
	inner    workflow.Asker
	observer *workflowObserver
}

// Ask reports the question, then asks it.
func (a observedAsker) Ask(ctx context.Context, question workflow.Question) (string, error) {
	a.observer.waiting(question)
	return a.inner.Ask(ctx, question)
}

// The workflow control call's answers (ADR 0089 D4). Each refusal is the whole tool error, so it
// says what to send instead.
const (
	workflowControlArgumentsFormat = "workflow was not run: its arguments are not valid JSON (%v); send an object with action"
	workflowControlActionFormat    = "workflow was not run: action %q is not one of status, stop, message"
	workflowControlDelegate        = "workflow was not run: only the main agent controls the session's background workflows"
	workflowControlNeedsID         = "workflow was not run: stop needs the id of the workflow to stop, as status lists it"
	workflowControlNeedsItem       = "workflow was not run: message needs item (a running item's run id or name, as status lists it) and text"
	workflowControlListFailed      = "workflow could not list the session's workflows: %v"
	workflowControlUnknownFormat   = "workflow: no workflow %q in this session; the workflows are: %s"
	workflowControlStopFailed      = "workflow could not stop %s: %v"
	workflowControlStopped         = "workflow %s is stopping; its finished items are kept, and its result reaches you when it ends"
	workflowControlNoItemFormat    = "workflow: no running item %q; the running items are: %s"
	workflowControlAmbiguousFormat = "workflow: %d running items are named %q; send item as one of their run ids: %s"
	workflowControlGoneFormat      = "workflow: item %s ended before the message could be queued"
	workflowControlQueuedFormat    = "message queued for %s (%s); it reaches the helper between its steps"
)

// The words the status listing is built from.
const (
	workflowStatusNone        = "no workflows in this session"
	workflowStatusHeadFormat  = "workflows (%d):"
	workflowStatusLineFormat  = "%s — %s — %s — items %d/%d done"
	workflowStatusDetailHint  = `workflow{action: "status", id: "<id>"} shows one workflow's items and the run ids to message`
	workflowStatusFolder      = "folder: "
	workflowStatusStageFormat = "stage %s (%s) — %s"
	workflowStatusItemFormat  = "  #%d %s — %s"
	workflowStatusRunningHead = "running items (message one by run id or name):"
	workflowStatusRunning     = "running in the background"
	workflowStatusQueued      = "queued in the background"
	workflowNamesNone         = "none"
)

// workflowControlArgs is a workflow control call's arguments as the tool publishes them.
type workflowControlArgs struct {
	Action string `json:"action"`
	ID     string `json:"id"`
	Item   string `json:"item"`
	Text   string `json:"text"`
}

// runningItem is one running item child of a background workflow, as the status listing names it
// and a message addresses it: its run id, its item's short name (the child's name), its item's full
// label and the workflow it runs in.
type runningItem struct {
	runID    string
	name     string
	label    string
	workflow string
}

// workflowControlResult answers one workflow control call. Only the top-level Agent holds the
// session's background workflows, so a delegate's call is refused.
func (a *Agent) workflowControlResult(call domain.ToolCall) domain.ToolResult {
	if a.isDelegate() {
		return errorToolResult(call.ID, workflowControlDelegate)
	}
	var args workflowControlArgs
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return errorToolResult(call.ID, fmt.Sprintf(workflowControlArgumentsFormat, err))
	}
	args.ID, args.Item = strings.TrimSpace(args.ID), strings.TrimSpace(args.Item)
	switch args.Action {
	case tools.WorkflowActionStatus:
		return a.workflowStatusResult(call.ID, args.ID)
	case tools.WorkflowActionStop:
		return a.workflowStopResult(call.ID, args.ID)
	case tools.WorkflowActionMessage:
		return a.workflowMessageResult(call.ID, args)
	default:
		return errorToolResult(call.ID, fmt.Sprintf(workflowControlActionFormat, args.Action))
	}
}

// workflowStatusResult lists every workflow of the session, one line each, or — with id — that one
// in detail: its stages, every item with its receipt so far, and the running items a message can
// address.
func (a *Agent) workflowStatusResult(callID, id string) domain.ToolResult {
	infos, err := a.Workflows()
	if err != nil {
		return errorToolResult(callID, fmt.Sprintf(workflowControlListFailed, err))
	}
	if id == "" {
		return domain.ToolResult{CallID: callID, Content: workflowListing(infos)}
	}
	for _, info := range infos {
		if info.Status.ID == id {
			return domain.ToolResult{CallID: callID, Content: workflowDetail(info, a.runningItems(id))}
		}
	}
	return errorToolResult(callID, fmt.Sprintf(workflowControlUnknownFormat, id, workflowIDs(infos)))
}

// workflowStopResult stops the background workflow id, keeping its finished items (ADR 0088).
func (a *Agent) workflowStopResult(callID, id string) domain.ToolResult {
	if id == "" {
		return errorToolResult(callID, workflowControlNeedsID)
	}
	if err := a.StopWorkflow(id); err != nil {
		return errorToolResult(callID, fmt.Sprintf(workflowControlStopFailed, id, err))
	}
	return domain.ToolResult{CallID: callID, Content: fmt.Sprintf(workflowControlStopped, id)}
}

// workflowMessageResult queues args.Text for the one running item args.Item names — by run id, else
// by short name, else by full label (namedItems) — narrowed to the workflow args.ID when it is set.
// Two items may share a short name (/a/x.go and /b/x.go both read x.go); the full label or the run
// id then tells them apart. It rides the
// item child's mailbox exactly as a human Interjection does (InterjectChild, ADR 0063): the message
// lands at the child's next between-Steps boundary and grants it nothing. It is always the ordinary
// send, never InterjectChildNow, so the model never pre-empts the child's grandchildren (ADR 0025,
// amended 2026-10-05).
func (a *Agent) workflowMessageResult(callID string, args workflowControlArgs) domain.ToolResult {
	if args.Item == "" || strings.TrimSpace(args.Text) == "" {
		return errorToolResult(callID, workflowControlNeedsItem)
	}
	running := a.runningItems(args.ID)
	named := namedItems(running, args.Item)
	switch len(named) {
	case 0:
		return errorToolResult(callID, fmt.Sprintf(workflowControlNoItemFormat, args.Item, runningItemNames(running)))
	case 1:
	default:
		return errorToolResult(callID, fmt.Sprintf(workflowControlAmbiguousFormat, len(named), args.Item, runningItemNames(named)))
	}
	target := named[0]
	if err := a.InterjectChild(target.runID, domain.UserInput{Text: args.Text}); err != nil {
		return errorToolResult(callID, fmt.Sprintf(workflowControlGoneFormat, target.runID))
	}
	return domain.ToolResult{CallID: callID, Content: fmt.Sprintf(workflowControlQueuedFormat, target.name, target.runID)}
}

// namedItems is the running items target addresses: the one whose run id it is, else every one
// whose short name it is, else every one whose full label it is — so a short name wins over a label
// that happens to read the same, and a label still reaches an item whose short name differs.
func namedItems(running []runningItem, target string) []runningItem {
	for _, item := range running {
		if item.runID == target {
			return []runningItem{item}
		}
	}
	for _, match := range []func(runningItem) bool{
		func(item runningItem) bool { return item.name == target },
		func(item runningItem) bool { return item.label == target },
	} {
		var named []runningItem
		for _, item := range running {
			if match(item) {
				named = append(named, item)
			}
		}
		if len(named) > 0 {
			return named
		}
	}
	return nil
}

// runningItems lists the item children of this Agent's background workflows that are running now —
// of the workflow id only, when id is set — ordered by run id. A background workflow's children are
// registered on this Agent (startBackground) under the call id `workflow-<id>`, which is how they are
// told from the conversation's own delegations; a child's run id and call id are fixed at its
// construction, and so is its item's label (workflowChild), so reading them here races nothing.
func (a *Agent) runningItems(id string) []runningItem {
	var items []runningItem
	for _, child := range a.children.all() {
		workflowID, ok := strings.CutPrefix(child.callID, backgroundCallPrefix)
		if !ok || (id != "" && workflowID != id) {
			continue
		}
		item := runningItem{runID: child.runID, name: child.displayName(), workflow: workflowID}
		if child.workflowItem != nil {
			item.label = child.workflowItem.label
		}
		items = append(items, item)
	}
	slices.SortFunc(items, func(x, y runningItem) int { return strings.Compare(x.runID, y.runID) })
	return items
}

// runningItemNames renders running items as `<run id> <name>` pairs, or "none".
func runningItemNames(items []runningItem) string {
	if len(items) == 0 {
		return workflowNamesNone
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.runID+" "+item.name)
	}
	return strings.Join(names, ", ")
}

// workflowIDs renders the session's workflow ids, or "none".
func workflowIDs(infos []WorkflowInfo) string {
	if len(infos) == 0 {
		return workflowNamesNone
	}
	ids := make([]string, 0, len(infos))
	for _, info := range infos {
		ids = append(ids, info.Status.ID)
	}
	return strings.Join(ids, ", ")
}

// workflowListing is the status of every workflow, one line each, oldest first, then how to see one
// in detail.
func workflowListing(infos []WorkflowInfo) string {
	if len(infos) == 0 {
		return workflowStatusNone
	}
	lines := make([]string, 0, len(infos)+2)
	lines = append(lines, fmt.Sprintf(workflowStatusHeadFormat, len(infos)))
	for _, info := range infos {
		done, total := itemProgress(info.Status)
		lines = append(lines, fmt.Sprintf(workflowStatusLineFormat,
			info.Status.ID, oneLine(info.Status.Name), workflowState(info), done, total))
	}
	lines = append(lines, workflowStatusDetailHint)
	return strings.Join(lines, "\n")
}

// workflowDetail is one workflow in detail: its line, its folder, every stage with its items and
// their receipts so far, and the items running now.
func workflowDetail(info WorkflowInfo, running []runningItem) string {
	done, total := itemProgress(info.Status)
	lines := []string{
		fmt.Sprintf(workflowStatusLineFormat, info.Status.ID, oneLine(info.Status.Name), workflowState(info), done, total),
		workflowStatusFolder + info.Dir,
	}
	for _, stage := range info.Status.Stages {
		head := fmt.Sprintf(workflowStatusStageFormat, stage.Name, stage.Kind, stage.Phase)
		if stage.Note != "" {
			head += finishSeparator + oneLine(stage.Note)
		}
		lines = append(lines, head)
		for index, item := range stage.Items {
			lines = append(lines, fmt.Sprintf(workflowStatusItemFormat, index+1, oneLine(item.Label), itemStatusText(item)))
		}
	}
	if len(running) > 0 {
		lines = append(lines, workflowStatusRunningHead)
		for _, item := range running {
			lines = append(lines, "  "+item.runID+" "+item.name)
		}
	}
	return strings.Join(lines, "\n")
}

// workflowState words how the engine says a workflow stands (workflow.StateOf): queued or running
// in the background when this session's manager holds it, else the phase its status.json records.
func workflowState(info WorkflowInfo) string {
	state := workflow.StateOf(info)
	switch state.Kind {
	case workflow.StateQueued:
		return workflowStatusQueued
	case workflow.StateBackground:
		return workflowStatusRunning
	default:
		return string(state.Phase)
	}
}

// itemProgress is a workflow's finished items and all of them, by the engine's status.json tally
// (workflow.TallyOfStatus).
func itemProgress(status workflow.RunStatus) (done, total int) {
	tally := workflow.TallyOfStatus(status)
	return tally.Total() - tally.Unfinished, tally.Total()
}

// itemStatusText is one item's outcome so far: `<status> — <summary>[ k=v…]` once its receipt is in,
// its phase until then — the status word the engine's (workflow.ItemStatusWord), each field value
// rendered as Format renders it (workflow.FieldValue: a value that would blur its pair is quoted),
// then folded onto one line.
func itemStatusText(item workflow.ItemStatus) string {
	status := workflow.ItemStatusWord(item.Phase, item.Receipt)
	if item.Receipt == nil {
		return status
	}
	text := status + finishSeparator + oneLine(item.Receipt.Summary)
	keys := make([]string, 0, len(item.Receipt.Fields))
	for key := range item.Receipt.Fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		text += " " + key + "=" + oneLine(workflow.FieldValue(item.Receipt.Fields[key]))
	}
	return text
}
