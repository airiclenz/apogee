package agent

// The BLOCKING WORKFLOW CALL (ADR 0087 D1, ADR 0088 D3): a model's `fan_out` call, run by the
// engine to its end inside the Turn that asked for it. Dispatch recognises the call (resolve's
// Workflow verdict), keeps it in the leaf group, and runs it here: the arguments become a
// workflow.Plan — one fanout, then at most one verify and one merge — checked by
// workflow.ValidateModelPlan, whose problems come back as one tool error naming each argument to
// fix. A call naming a `recipe` instead runs that Recipe (ADR 0087 D2) through the core the recipe
// launch shares (runRecipe, recipe.go) with its keyed `inputs`: an unknown recipe is answered with
// the recipes there are, and a recipe beside any argument that describes a fan-out is refused
// before anything runs. A valid plan runs through a workflow.Runner over the session's `<scratch>/workflows/` store,
// its item children spawned through the recursion point (workflowspawn.go), its progress reported
// as WorkflowPhaseEvents (workflowObserver), and the call is answered with workflow.Format's result
// lines. The same call again finds the stored workflow by
// its plan hash and skips the items already finished.
//
// A cancel stops the running items and keeps every finished one on disk: the call is answered with
// `stopped by the user: K of N done` and the path of the item listing written so far, and dispatch
// settles the Turn on it. A stopped item child is never folded — the Runner restarts it fresh on
// resume and keeps no report of it — so the only folds under the cancel are those of the item
// children's own delegations, which foldStoppedChild holds to the inherited cancel bound and skips
// on a quit or a daemon's shutdown.
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
	"fmt"
	"os"
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

// fanOutArgs is a fan_out call's arguments as the tool publishes them (tools.fanOutSchemaTemplate).
// `run_on` is read by nothing here yet: the item children run on the configured seat. `recipe` and
// `inputs`, the recipe form, are read by parseFanOutRecipe, and `background` by asksBackground.
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
// for fan-out arguments: a model that fills every field in with an empty value asked for nothing.
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
	background := asksBackground(call.Arguments) && a.offersBackground()
	if recipe, inputs, refusal, isRecipe := parseFanOutRecipe(call.Arguments); isRecipe {
		if refusal != "" {
			return errorToolResult(call.ID, refusal)
		}
		return a.recipeCallResult(ctx, turn, call, recipe, inputs, background)
	}
	plan, refusal := parseFanOutPlan(call.Arguments)
	if refusal != "" {
		return errorToolResult(call.ID, refusal)
	}
	if problems := workflow.ValidateModelPlan(plan); len(problems) > 0 {
		return errorToolResult(call.ID, fanOutProblemsText(problems))
	}
	runner, refusal := a.newWorkflowRunner(turn, call)
	if refusal != "" {
		return errorToolResult(call.ID, refusal)
	}
	if background {
		id, err := a.startBackground(backgroundLaunch{plan: plan, runner: runner, tool: tools.FanOutToolName, turn: turn})
		return a.backgroundCallResult(call.ID, id, err)
	}
	observer := a.observeWorkflow(runner, turn, plan.Name)
	outcome, err := runner.Run(ctx, plan)
	observer.end(outcome, err)
	if err != nil {
		return errorToolResult(call.ID, fanOutRunFailedPrefix+err.Error())
	}
	return domain.ToolResult{CallID: call.ID, Content: workflowAnswer(outcome)}
}

// recipeCallResult runs the recipe id one fan_out call names over its keyed inputs and renders its
// answer: the result lines, the recipes there are for an unknown id, or why the recipe could not
// run (an unknown or missing input among them). With background set it starts the recipe in the
// background instead and answers at once.
func (a *Agent) recipeCallResult(
	ctx context.Context,
	turn int,
	call domain.ToolCall,
	id string,
	inputs map[string]string,
	background bool,
) domain.ToolResult {
	source := a.recipeSource()
	if source == nil {
		return errorToolResult(call.ID, fmt.Sprintf(fanOutUnknownRecipe, id, "none"))
	}
	recipe, ok := source.Recipe(id)
	if !ok {
		known := "none"
		if ids := source.RecipeIDs(); len(ids) > 0 {
			known = strings.Join(ids, ", ")
		}
		return errorToolResult(call.ID, fmt.Sprintf(fanOutUnknownRecipe, id, known))
	}
	if background {
		workflowID, err := a.startKeyedBackgroundRecipe(recipe, inputs)
		return a.backgroundCallResult(call.ID, workflowID, err)
	}
	result, err := a.runRecipe(ctx, turn, call, id, inputs)
	if err != nil {
		return errorToolResult(call.ID, fmt.Sprintf(fanOutRecipeFailed, id, err))
	}
	return domain.ToolResult{CallID: call.ID, Content: workflowAnswer(result)}
}

// parseFanOutRecipe reads a fan_out call that names a recipe: the recipe id and its inputs, or the
// refusal a recipe beside fan-out arguments, or inputs that are not name: text pairs, earn.
// isRecipe is false — and the call is an ordinary fan-out — when the arguments name no recipe or
// are not a JSON object (parseFanOutPlan reports that).
func parseFanOutRecipe(raw json.RawMessage) (id string, inputs map[string]string, refusal string, isRecipe bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", nil, "", false
	}
	if err := json.Unmarshal(fields["recipe"], &id); err != nil || id == "" {
		return "", nil, "", false
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

// newWorkflowRunner builds the Runner one fan_out call runs under, or the refusal that keeps it
// from running: the session's workflow store under its scratch directory, the workspace the items
// are read from, the split budget a `split:` source cuts to, and the dispatch width. Width is this
// Agent's delegation width — the cap of the server the children run on, 1 on a delegate — and the
// Runner never runs more children than a stage has items, so the width in effect is min(width, N).
func (a *Agent) newWorkflowRunner(turn int, call domain.ToolCall) (*workflow.Runner, string) {
	scratch := a.ScratchDir()
	if scratch == "" {
		return nil, fanOutNoScratch
	}
	if a.cfg.WorkspaceDir == "" {
		return nil, fanOutNoWorkspace
	}
	store, err := workflow.NewStore(scratch)
	if err != nil {
		return nil, fanOutRunFailedPrefix + err.Error()
	}
	return &workflow.Runner{
		Spawner:       a.newWorkflowSpawner(turn, call, nil),
		Store:         store,
		Workspace:     os.DirFS(a.cfg.WorkspaceDir),
		Split:         workflow.NewSplitBudget(a.workflowContextLimit()),
		Width:         a.delegationWidth(),
		Retries:       a.cfg.Workflow.ResolvedRetries(),
		Continuations: a.cfg.Workflow.ResolvedContinuations(),
	}, ""
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
// lines, and — for a stopped one — the item listing written so far, when Format has not named it.
func workflowAnswer(result workflow.Result) string {
	text := workflow.Format(result)
	if !result.Stopped() || result.Listing == "" {
		return text
	}
	line := fmt.Sprintf(fanOutListingLineFormat, result.Listing)
	if strings.Contains(text, line) {
		return text
	}
	return text + "\n" + line
}

// workflowObserver turns one Workflow run's Runner notifications into the WorkflowPhaseEvents this
// Agent emits (domain.WorkflowPhaseEvent), stamped with this Agent's own identity at the Turn that
// started the Workflow: started on the first notification that names the Workflow's id, a stage
// started for each stage that begins running, an item finished for each item that ends on a
// receipt, waiting while an `ask` stage's question is out, and — from end — finished, stopped or
// failed. The Runner serialises its Observer calls, but an ask and the run's end arrive from the
// Run caller, so mu guards the started state.
type workflowObserver struct {
	agent *Agent
	turn  int
	name  string
	// background marks every event a background workflow's observer emits (domain.
	// WorkflowPhaseEvent.Background); driveBackground sets it before the run starts.
	background bool

	mu sync.Mutex
	id string // the Workflow's id, once started was emitted
}

// observeWorkflow installs a workflowObserver on runner — as its Observer, and around its Asker
// when it has one, so an `ask` stage's question is reported before it is put — and returns it for
// the caller to end once Run returns. name is the Workflow's name the events carry.
func (a *Agent) observeWorkflow(runner *workflow.Runner, turn int, name string) *workflowObserver {
	observer := &workflowObserver{agent: a, turn: turn, name: name}
	runner.Observer = observer
	if runner.Asker != nil {
		runner.Asker = observedAsker{inner: runner.Asker, observer: observer}
	}
	return observer
}

// StagePhase reports a stage that began running; a stage's other phases add nothing the item and
// end phases do not.
func (o *workflowObserver) StagePhase(event workflow.StageEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.startLocked(event.Workflow)
	if event.Phase != workflow.PhaseRunning {
		return
	}
	o.emitLocked(domain.WorkflowPhaseEvent{Phase: domain.WorkflowStageStarted, Stage: event.Stage})
}

// ItemPhase reports an item that ended on a receipt — a resumed one, which never ran, included.
func (o *workflowObserver) ItemPhase(event workflow.ItemEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.startLocked(event.Workflow)
	if event.Phase != workflow.PhaseDone || event.Receipt == nil {
		return
	}
	o.emitLocked(domain.WorkflowPhaseEvent{
		Phase: domain.WorkflowItemFinished, Stage: event.Stage, Item: event.Label, Index: event.Index,
		Resumed: event.Attempt == 0, Receipt: event.Receipt.Domain(),
	})
}

// waiting reports an `ask` stage's question as it is put to the user.
func (o *workflowObserver) waiting(question workflow.Question) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.startLocked(question.Workflow)
	o.emitLocked(domain.WorkflowPhaseEvent{Phase: domain.WorkflowWaiting, Stage: question.Stage, Detail: question.Text})
}

// end reports how the run Run returned ended: failed when it returned an error after the Workflow
// started, else stopped or finished by the result's phase. A run that failed before its Workflow
// had an id started nothing and reports nothing.
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
		o.emitLocked(domain.WorkflowPhaseEvent{Phase: domain.WorkflowStopped})
	default:
		o.emitLocked(domain.WorkflowPhaseEvent{Phase: domain.WorkflowFinished})
	}
}

// startLocked emits started the first time a notification names the Workflow's id. The caller
// holds mu.
func (o *workflowObserver) startLocked(id string) {
	if o.id != "" || id == "" {
		return
	}
	o.id = id
	o.emitLocked(domain.WorkflowPhaseEvent{Phase: domain.WorkflowStarted})
}

// emitLocked stamps event with this Agent's identity, the Workflow's id and name, and emits it.
// The caller holds mu.
func (o *workflowObserver) emitLocked(event domain.WorkflowPhaseEvent) {
	event.EventBase = o.agent.base(o.turn)
	event.Workflow, event.Name, event.Background = o.id, o.name, o.background
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
// and a message addresses it: its run id, its item name and the workflow it runs in.
type runningItem struct {
	runID    string
	name     string
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

// workflowMessageResult queues args.Text for the one running item args.Item names — by run id, or by
// item name when no run id matches — narrowed to the workflow args.ID when it is set. It rides the
// item child's mailbox exactly as a human Interjection does (InterjectChild, ADR 0063): the message
// lands at the child's next between-Steps boundary and grants it nothing.
func (a *Agent) workflowMessageResult(callID string, args workflowControlArgs) domain.ToolResult {
	if args.Item == "" || strings.TrimSpace(args.Text) == "" {
		return errorToolResult(callID, workflowControlNeedsItem)
	}
	running := a.runningItems(args.ID)
	var named []runningItem
	for _, item := range running {
		if item.runID == args.Item {
			named = []runningItem{item}
			break
		}
		if item.name == args.Item {
			named = append(named, item)
		}
	}
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

// runningItems lists the item children of this Agent's background workflows that are running now —
// of the workflow id only, when id is set — ordered by run id. A background workflow's children are
// registered on this Agent (startBackground) under the call id `workflow-<id>`, which is how they are
// told from the conversation's own delegations; a child's run id and call id are fixed at its
// construction, so reading them here races nothing.
func (a *Agent) runningItems(id string) []runningItem {
	var items []runningItem
	for _, child := range a.children.all() {
		workflowID, ok := strings.CutPrefix(child.callID, backgroundCallPrefix)
		if !ok || (id != "" && workflowID != id) {
			continue
		}
		items = append(items, runningItem{runID: child.runID, name: child.displayName(), workflow: workflowID})
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
		done, total := itemCounts(info.Status)
		lines = append(lines, fmt.Sprintf(workflowStatusLineFormat,
			info.Status.ID, oneLine(info.Status.Name), workflowState(info), done, total))
	}
	lines = append(lines, workflowStatusDetailHint)
	return strings.Join(lines, "\n")
}

// workflowDetail is one workflow in detail: its line, its folder, every stage with its items and
// their receipts so far, and the items running now.
func workflowDetail(info WorkflowInfo, running []runningItem) string {
	done, total := itemCounts(info.Status)
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

// workflowState is how a workflow stands: running or queued in the background when this session's
// manager holds it, else the phase its status.json records.
func workflowState(info WorkflowInfo) string {
	switch {
	case info.Queued:
		return workflowStatusQueued
	case info.Background:
		return workflowStatusRunning
	default:
		return string(info.Status.Phase)
	}
}

// itemCounts counts the items of a workflow's fan-out stages that are done, and all of them.
func itemCounts(status workflow.RunStatus) (done, total int) {
	for _, stage := range status.Stages {
		if stage.Kind != workflow.StageFanout {
			continue
		}
		for _, item := range stage.Items {
			total++
			if item.Phase == workflow.PhaseDone {
				done++
			}
		}
	}
	return done, total
}

// itemStatusText is one item's outcome so far: `<status> — <summary>[ k=v…]` once its receipt is in,
// its phase until then.
func itemStatusText(item workflow.ItemStatus) string {
	if item.Receipt == nil {
		return string(item.Phase)
	}
	receipt := item.Receipt.Domain()
	text := receipt.Status + finishSeparator + oneLine(receipt.Summary)
	keys := make([]string, 0, len(receipt.Fields))
	for key := range receipt.Fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		text += " " + key + "=" + oneLine(receipt.Fields[key])
	}
	return text
}
