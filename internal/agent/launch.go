package agent

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/workflow"
)

// launchMode is whether a Workflow runs on the Turn that launched it or in the background.
type launchMode int

const (
	// launchModeBlocking runs the Workflow inside the call or opening that launched it — the zero value.
	launchModeBlocking launchMode = iota
	// launchModeBackground hands the Workflow to the background manager (ADR 0089).
	launchModeBackground
)

// workflowLaunch is one Workflow launch, the single value its Runner is built from (buildLaunch).
//
// Its source is one of three: a folder already in a store (folder set — a resume or a re-run, the
// plan read back from it, recipe naming the recipe it came from, "" for a fan_out's plan), a recipe
// (recipe.SkillID set — the recipe's id, its keyed inputs in inputs, completed by the builder), or
// a fan_out's plan (neither). recipe.Background is never read: mode says where the launch runs.
//
// kind, recipe and line are also how a blocking launch was launched, the one input its stopped
// answer's resume line (resumeHint) and its started phase's resume command (resumeCommand) are
// derived from: for a typed launch, line is the user's line, trimmed.
type workflowLaunch struct {
	kind   launchKind
	line   string
	mode   launchMode
	plan   workflow.Plan
	recipe domain.RecipeLaunch
	inputs map[string]string
	folder *launchFolder
	// seat is the Delegation seat the item children run on (a fan_out call's `run_on`;
	// seatConfigured, the zero, for every other launch), turn the Turn that launched it.
	seat delegationSeat
	turn int
	// call is the call a blocking launch's item children are bracketed under; a background launch
	// brackets them under the `workflow-<id>` call its folder names instead.
	call domain.ToolCall
}

// launchFolder is a launch's folder source: the folder id plan.json is read back from, the store it
// lives in (the session's, or a kept workflow's Home — entryScratch), and whether the launch must
// run in that very folder (a re-run, refused with rerunMovedFormat when the plan's hash now leads
// elsewhere) rather than find or create one by hash (a resume, which a moved source runs anew).
type launchFolder struct {
	id     string
	store  *workflow.Store
	pinned bool
}

// launchRefusal is a launch the session cannot keep a Workflow for (no scratch directory, no
// workspace), worded as the fan_out answer that refuses it; a fan_out call answers it as is.
type launchRefusal string

// Error is the refusal's text.
func (r launchRefusal) Error() string { return string(r) }

// builtLaunch is a launch's Runner, wired, with what it runs: the plan (inputs bound, or read back
// from its folder), the recipe it comes from ("" for a fan_out's plan), the folder id (a background
// launch's, opened by the builder), the Agent its children are spawned off and its observer.
type builtLaunch struct {
	plan     workflow.Plan
	recipe   string
	prompts  fs.FS
	tool     string
	id       string
	runner   *workflow.Runner
	host     *Agent
	observer *workflowObserver
}

// buildLaunch builds and wires the Runner launch runs under. A background launch is refused on a
// delegate (errDelegateBackground), opens its folder at once — so a queued workflow already has the
// id and status path it is listed and stopped by, and the Run that follows resumes that very folder
// — and runs off the launch-time host (backgroundHost); a blocking one runs off this Agent under
// launch.call. The error is a source that cannot be read or resolved, a refusal (launchRefusal), or
// a folder the Runner cannot open.
func (a *Agent) buildLaunch(launch workflowLaunch) (builtLaunch, error) {
	built, err := a.launchRunner(launch)
	if err != nil {
		return builtLaunch{}, err
	}
	host, call := a, launch.call
	if launch.mode == launchModeBackground {
		if a.isDelegate() {
			return builtLaunch{}, errDelegateBackground
		}
		folder := ""
		if launch.folder != nil && launch.folder.pinned {
			folder = launch.folder.id
		}
		status, err := built.runner.Open(built.plan, folder)
		if errors.Is(err, workflow.ErrFolderMoved) {
			return builtLaunch{}, fmt.Errorf(rerunMovedFormat, folder)
		}
		if err != nil {
			return builtLaunch{}, err
		}
		built.id, host = status.ID, a.backgroundHost()
		call = domain.ToolCall{ID: backgroundCallPrefix + status.ID, Tool: built.tool}
	}
	built.host = host
	built.observer = a.wireLaunch(launch, built, call)
	return built, nil
}

// launchRunner builds the unwired Runner launch's source runs under: a fan_out's Runner for a plan,
// a recipe's for a recipe — its inputs completed and bound into its plan unless the plan is read
// back from a folder — over the folder's own store for a folder source.
func (a *Agent) launchRunner(launch workflowLaunch) (builtLaunch, error) {
	built := builtLaunch{plan: launch.plan, tool: tools.FanOutToolName}
	if launch.folder != nil {
		plan, err := launch.folder.store.ReadPlan(launch.folder.id)
		if err != nil {
			return builtLaunch{}, err
		}
		built.plan = plan
	}
	if launch.recipe.SkillID == "" {
		runner, refusal := a.newWorkflowRunner(launch.turn, launch.call, launch.seat)
		if refusal != "" {
			return builtLaunch{}, launchRefusal(refusal)
		}
		built.runner = runner
	} else {
		recipe, err := a.recipeByID(launch.recipe.SkillID)
		if err != nil {
			return builtLaunch{}, err
		}
		if launch.folder == nil {
			inputs, err := completeInputs(recipe.Inputs, launch.inputs)
			if err != nil {
				return builtLaunch{}, err
			}
			built.plan = bindPlanInputs(recipe, inputs)
		}
		runner, err := a.newRecipeRunner(launch.turn, launch.call, recipe, launch.seat)
		if err != nil {
			return builtLaunch{}, err
		}
		built.recipe, built.runner, built.prompts, built.tool = recipe.ID, runner, recipe.Files, recipeCallTool
	}
	if launch.folder != nil {
		built.runner.Store = launch.folder.store // a kept workflow's Home moves it off the session's
	}
	return built, nil
}

// wireLaunch wires built's Runner for launch's mode and returns its observer: its children spawned
// off built.host and bracketed under call yet addressable through this Agent, on launch's seat; a
// recipe's scripts run through the host's Resolution; its phases reported by an observer. A
// blocking launch's started phase carries its resume command; a background launch's events are
// marked background, and its ask stages put through the manager's queue — set after
// observeWorkflow, which would wrap the Asker to report the question before it is queued: a
// background question is reported once it waits in the queue (backgroundScope.announce).
func (a *Agent) wireLaunch(launch workflowLaunch, built builtLaunch, call domain.ToolCall) *workflowObserver {
	host, runner := built.host, built.runner
	spawner := host.newWorkflowSpawner(launch.turn, call, built.prompts)
	spawner.children = &a.children
	spawner.seat = launch.seat
	runner.Spawner = spawner
	if scripts, ok := runner.Scripts.(*recipeScripts); ok {
		scripts.agent = host
	}
	observer := host.observeWorkflow(runner, launch.turn, built.plan)
	observer.call = call.ID
	if launch.mode == launchModeBlocking {
		observer.resume = resumeCommand(launch)
		return observer
	}
	observer.background = true
	if runner.Asker != nil {
		runner.Asker = backgroundAsker{scope: backgroundScope{manager: &a.background, workflow: built.id, observer: observer}}
	}
	return observer
}
