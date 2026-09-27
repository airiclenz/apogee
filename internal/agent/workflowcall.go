package agent

// The BLOCKING WORKFLOW CALL (ADR 0087 D1, ADR 0088 D3): a model's `fan_out` call, run by the
// engine to its end inside the Turn that asked for it. Dispatch recognises the call (resolve's
// Workflow verdict), keeps it in the leaf group, and runs it here: the arguments become a
// workflow.Plan — one fanout, then at most one verify and one merge — checked by
// workflow.ValidateModelPlan, whose problems come back as one tool error naming each argument to
// fix. A valid plan runs through a workflow.Runner over the session's `<scratch>/workflows/` store,
// its item children spawned through the recursion point (workflowspawn.go), and the call is
// answered with workflow.Format's result lines. The same call again finds the stored workflow by
// its plan hash and skips the items already finished.
//
// A cancel stops the running items and keeps every finished one on disk: the call is answered with
// `stopped by the user: K of N done` and the path of the item listing written so far, and dispatch
// settles the Turn on it. A stopped item child is never folded — the Runner restarts it fresh on
// resume and keeps no report of it — so the only folds under the cancel are those of the item
// children's own delegations, which foldStoppedChild holds to the inherited cancel bound and skips
// on a quit or a daemon's shutdown.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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
	fanOutProblemsHead      = "fan_out was not run — fix these arguments and call it again:"
	fanOutArgumentsFormat   = "fan_out was not run: its arguments are not valid JSON (%v); send an object with task and over"
	fanOutOverFormat        = "fan_out was not run: over must be an array of strings or an object with one of files, lines or split (%v)"
	fanOutRecipeUnavailable = "fan_out was not run: recipes cannot be started through fan_out yet; describe the fan-out with task and over instead"
	fanOutNoScratch         = "fan_out was not run: this session has no scratch directory to keep the workflow in"
	fanOutNoWorkspace       = "fan_out was not run: this session has no workspace to read the items from"
	fanOutRunFailedPrefix   = "fan_out could not run: "
)

// fanOutListingLineFormat is the line a stopped workflow's answer ends on when Format has not
// already named the item listing: the report so far, in Format's own `items:` spelling so the model
// reads one name for the one file.
const fanOutListingLineFormat = "items: %s"

// fanOutArgs is a fan_out call's arguments as the tool publishes them (tools.fanOutSchemaTemplate).
// `run_on` and `background` are read by nothing here yet: the item children run on the configured
// seat, and the call blocks.
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
	Recipe  string               `json:"recipe"`
}

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

// workflowCallResult parses, checks and runs one fan_out call's Workflow and renders its answer.
func (a *Agent) workflowCallResult(ctx context.Context, turn int, call domain.ToolCall) domain.ToolResult {
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
	outcome, err := runner.Run(ctx, plan)
	if err != nil {
		return errorToolResult(call.ID, fanOutRunFailedPrefix+err.Error())
	}
	return domain.ToolResult{CallID: call.ID, Content: workflowAnswer(outcome)}
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
	if args.Recipe != "" {
		return workflow.Plan{}, fanOutRecipeUnavailable
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
