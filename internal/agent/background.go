package agent

// The BACKGROUND WORKFLOW MANAGER (ADR 0089): the Workflows that run outside any Turn while the
// conversation goes on. A background workflow is launched at an idle boundary (StartRecipe with
// Background set) and runs on a goroutine of its own, under a context of its own, so the
// conversation's `esc` never reaches it (ADR 0089 D5). Its folder is opened at launch — found by
// plan hash, or created — so its id and status path exist before its first child does.
//
// Capacity (ADR 0089 D2). A background workflow runs at the Parallel-agents width of the server its
// children run on minus one (backgroundWidth), so one slot stays free for the conversation; on a
// width-1 server it runs at width 1 and its children and the conversation's requests take turns on
// the one slot. The server is the Delegation target's when one is latched, else the session's
// (backgroundServer), and only ONE background workflow runs per server at a time: a second one
// launched onto a busy server waits in line (queued) and starts when the one ahead of it ends.
//
// A launch-time snapshot (backgroundHost). A background workflow runs beside the conversation, so
// what it reads of the Agent must not be what the idle-only mutators write — Rebind, SwitchUpstream,
// SwapTools, SetProfile, SetJournal and the session boundary's context-file reload assume no Step
// runs, and a background workflow's children and script stages are exactly that. So the workflow
// never runs off the live Agent: at launch it takes a host, a detached Agent holding the Config,
// Upstream, tool set, profile parsers, context files, journal and every live setting as they stand,
// and its Runner's children, scripts, phase events and width all read that host. The handles the
// tree shares on purpose stay shared — the run-id minter, the Consoles, the Delegation-target latch,
// the prompt slot and the approver seam, and the registry its running children are addressable in —
// and the live mode is a tighten-only view of the top-level Agent's, as a delegate's is (ADR 0013).
// A move the human makes after the launch therefore reaches the next workflow, not a running one:
// it finishes on the model, server and tools it started with, as a delegate does.
//
// Every background workflow's child and script stage runs under a context that marks it as
// background (withBackgroundPrompts). A gate one of them reaches, and a question an `ask` stage
// puts, never reach the Driver's prompt directly — the conversation may be showing one of its own —
// they wait in the manager's queue (backgroundPrompt) until the Driver takes them up, and a stop
// withdraws them. Each one queued is reported by a WorkflowWaiting phase event emitted once it is in
// the queue, never before, so a Driver reacting to the event always finds it there; the Driver
// lists the queue (Agent.WorkflowPrompts) and answers from it (Agent.AnswerWorkflowPrompt) when its
// human is free to answer.
//
// Lifetime. Agent.StopWorkflow stops one workflow and keeps its finished items (ADR 0088);
// Agent.RerunFailed launches a finished one again in its own folder, as a new run of the same
// workflow that skips every item whose receipt is ok or partial, so only its blocked and faulted
// items run again;
// Agent.Close stops them all, on the top-level Agent only (a finishing delegate's Close must never
// reach them); ClearContext and RestoreSession stop the outgoing session's set too — unless the
// Driver called Agent.KeepWorkflows just before, the human's "keep them" answer, when that one
// boundary leaves the set and its held notes running into the new conversation. The live set rides
// the session snapshot as the additive `workflows` key — identifiers only (workflowEntryJSON), with
// the sibling session scratch directory a kept workflow's folder lives under when it is not the
// current one — which a restore loads and validates without starting anything;
// Agent.ResumeWorkflows starts it once the Driver has bound the engine (it reads the live scratch
// directory and recipe catalog, which a Resume has not re-supplied yet when restoreState runs).
//
// Finish notes and the wake (ADR 0089 D3). A background workflow's end — finished, stopped or
// failed — leaves one line for the parent (finishNote), which the manager HOLDS before the
// WorkflowPhaseEvent that ends the workflow is emitted, so a Driver reacting to that event always
// finds it. The engine never delivers it on its own; the held note is consumed by whichever comes
// first: the Driver's between-Steps drain while an Exchange runs (Agent.TakeWorkflowNotes, committed
// through Interject), the wake that opens an Exchange on it while the agent is idle (Agent.Wake —
// refused under `workflow-wake: off`), or the next Exchange a message opens, whose opening message
// carries it after the human's text (step). Every form reads the same: a fixed plain header, then
// one line per note (renderWorkflowNotes) — recorded text, so a snapshot keeps it. A note held and
// not yet delivered rides the snapshot as the additive `workflow_notes` key, so a session saved
// with one — the record saved at quit, before Close stops the set — hands it to the resumed
// conversation: a restore loads the notes beside the `workflows` set and ResumeWorkflows adopts
// them as held. Stopping the whole set (Close, and a ClearContext or RestoreSession not told to
// keep it) drops the held notes with the session they were meant for, and a stop that drops them
// leaves none behind to wake on — though a ClearContext with no workflow live stops nothing and so
// drops nothing: the notes it finds held reach the new conversation.
//
// Resume replays, by decision (plan 2026-09-27 - 00, item 30). A resumed workflow re-opens its
// folder and skips every fan-out item whose receipt is already there, and it replays the script and
// `ask` stages the earlier run settled (workflow.StageRecord): a script that already produced a
// result is not run again, and a question already answered is not asked again.

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/workflow"
)

// backgroundCallPrefix leads the synthetic call id a background workflow's children are bracketed
// under in their phase events (the workflow's id follows it). It is never put in history. It is the
// domain's constant because a Driver reads it too, to keep those children's events apart from the
// conversation's own delegations.
const backgroundCallPrefix = domain.BackgroundWorkflowCallPrefix

// The refusals of the manager's public calls.
const (
	unknownWorkflowFormat        = "apogee: no background workflow %q is running or queued"
	workflowAlreadyRunningFormat = "apogee: workflow %s is already running in the background"
	rerunUnfinishedFormat        = "apogee: workflow %s has not finished (%s) — a re-run takes the blocked and faulted items of a finished workflow"
	rerunNothingFormat           = "apogee: workflow %s has no blocked or faulted items to re-run"
	rerunMovedFormat             = "apogee: workflow %s no longer matches its items — launch it afresh"
)

// The words a finish note (finishNote) and its delivery (renderWorkflowNotes) are built from. The
// header is plain text, not an engine-note fence, because the note is recorded history: it must
// survive the session record, which strips every fence (domain.AdviceSpan).
const (
	workflowNoteHeader    = "Background workflow report (a note from apogee, not from the user):"
	finishLeadFormat      = "workflow %s %s"
	finishFailedFormat    = "workflow %s failed — %s"
	finishSeparator       = " — "
	finishTallySeparator  = " · "
	finishReportPrefix    = "report: "
	finishListingPrefix   = "items: "
	finishPhaseFinished   = "finished"
	finishPhaseStopped    = "stopped"
	workflowNoteSeparator = "\n\n"
)

// errDelegateBackground refuses a background launch on a delegate: background workflows belong to
// the top-level Agent, whose Close is the one that stops them.
var errDelegateBackground = errors.New("apogee: a sub-agent cannot start a background workflow")

// The bounds a snapshot's workflow entry is held to (checkRestoredWorkflows): a workflow id is the
// store's own folder name — lower-case letters, digits and dashes — and a recipe id a single name.
const (
	maxRestoredWorkflowIDBytes = 128
	maxRestoredRecipeIDBytes   = 128
)

// WorkflowInfo is one Workflow of the session as Agent.Workflows lists it: its status.json (every
// stage and item with the receipts so far), its folder, and whether this session's manager holds it
// as a background workflow — running now, or Queued behind another on its server. A workflow neither
// flag marks is not live: a blocking fan_out's, or a background one that has ended. It is
// [workflow.Info], named there so the TUI's Engine seam can spell Workflows' result without
// importing this package (ADR 0010).
type WorkflowInfo = workflow.Info

// workflowEntryJSON is one live background workflow as the session snapshot spells it: its
// folder's id under `<scratch>/workflows/` and, for a recipe's, the recipe skill's id its prompt
// files and scripts are read from on resume. Identifiers only — the plan is read back from the
// folder.
//
// Home is set only for a workflow kept across a session boundary (KeepWorkflows): its folder stays
// under the scratch directory of the session that launched it, and Home names that directory — a
// sibling of the current one, by its base name — so a resume finds the folder where it is. Empty
// means the current session's own scratch directory.
type workflowEntryJSON struct {
	ID     string `json:"id"`
	Recipe string `json:"recipe,omitempty"`
	Home   string `json:"home,omitempty"`
}

// backgroundManager is one top-level Agent's background workflows: the live ones (running, or
// queued behind another on their server) in launch order, the snapshot's set and held notes a
// restore loaded and ResumeWorkflows has not started or adopted yet, the questions and approvals
// the running ones wait on, the finish notes of the ended ones no one has taken yet, oldest first,
// every workflow launched since the set was last stopped whole with the store its folder is in (so
// one kept across a session boundary stays listed after it ends), and whether the next session
// boundary keeps the set (KeepWorkflows).
// The zero value is ready to use; mu guards every field, and is never held while a workflow runs.
type backgroundManager struct {
	mu            sync.Mutex
	runs          []*backgroundRun
	restored      []workflowEntryJSON
	restoredNotes []string
	prompts       []*backgroundPrompt
	promptID      uint64 // the last id minted for a queued prompt (backgroundPrompt.id)
	notes         []string
	launched      []keptRun
	keep          bool
}

// backgroundRun is one live background workflow. Everything but running and cancel is fixed at
// launch — host is the snapshot of the Agent it runs off (backgroundHost); those two are set when it
// starts, under the manager's lock. done closes when it has ended — run to its end, stopped, or
// dropped from the queue.
type backgroundRun struct {
	id       string
	recipe   string
	server   string
	seat     delegationSeat // the seat its children run on, which its width is read for (startRunLocked)
	plan     workflow.Plan
	runner   *workflow.Runner
	host     *Agent
	turn     int
	observer *workflowObserver // reports its phases, and each prompt it queues (backgroundScope.announce)

	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// backgroundLaunch is what startBackground needs of a workflow: its plan (inputs already bound),
// the recipe it comes from ("" for a fan_out's plan), the Runner built for it, where its stages'
// prompt files are read from (nil: the workspace), the tool its children are bracketed under, the
// Turn that launched it, the Delegation seat its children run on (the fan_out call's `run_on`;
// seatConfigured, the zero, for every other launch), and — for a re-run — the folder it must run in
// again ("" finds or creates one, openWorkflowFolder).
type backgroundLaunch struct {
	plan    workflow.Plan
	recipe  string
	runner  *workflow.Runner
	prompts fs.FS
	tool    string
	turn    int
	seat    delegationSeat
	folder  string
}

// Workflows lists the session's Workflows — every folder in its `<scratch>/workflows/` store, read
// from its status.json, oldest first — with the background ones this Agent runs or queues marked,
// and with every live one kept across a session boundary (KeepWorkflows) listed too, from the folder
// of the session that launched it. A folder whose status.json cannot be read is left out; a session
// with no scratch directory, no store yet and nothing kept has none.
func (a *Agent) Workflows() ([]WorkflowInfo, error) {
	scratch := a.ScratchDir()
	if scratch == "" {
		return nil, nil
	}
	store, err := workflow.NewStore(scratch)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(store.Root())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("apogee: list workflows: %w", err)
	}
	live := a.background.liveStates()
	infos := make([]WorkflowInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if info, ok := listedWorkflow(store, entry.Name(), live); ok {
			infos = append(infos, info)
		}
	}
	for _, kept := range a.background.storesElsewhere(store.Root()) {
		if info, ok := listedWorkflow(kept.store, kept.id, live); ok {
			infos = append(infos, info)
		}
	}
	slices.SortFunc(infos, func(x, y WorkflowInfo) int {
		if c := x.Status.Created.Compare(y.Status.Created); c != 0 {
			return c
		}
		return cmp.Compare(x.Status.ID, y.Status.ID)
	})
	return infos, nil
}

// listedWorkflow reads the folder id of store as Workflows lists it, marked live from live; ok is
// false for a folder whose status.json cannot be read or names another id.
func listedWorkflow(store *workflow.Store, id string, live map[string]bool) (WorkflowInfo, bool) {
	status, err := store.ReadStatus(id)
	if err != nil || status.ID != id {
		return WorkflowInfo{}, false
	}
	dir, err := store.Dir(status.ID)
	if err != nil {
		return WorkflowInfo{}, false
	}
	queued, isLive := live[status.ID]
	return WorkflowInfo{Status: status, Dir: dir, Background: isLive, Queued: isLive && queued}, true
}

// keptRun is a launched workflow's id and the store its folder is in.
type keptRun struct {
	id    string
	store *workflow.Store
}

// storesElsewhere lists the workflows launched since the set was last stopped whole whose folder is
// not under root — the ones kept across a session boundary, running or ended in the store of the
// session that launched them — each once.
func (m *backgroundManager) storesElsewhere(root string) []keptRun {
	m.mu.Lock()
	defer m.mu.Unlock()
	var kept []keptRun
	for _, run := range m.launched {
		if run.store.Root() != root && !slices.ContainsFunc(kept, func(k keptRun) bool { return k.id == run.id }) {
			kept = append(kept, run)
		}
	}
	return kept
}

// StopWorkflow stops the background workflow id and keeps its finished items (ADR 0088): a running
// one's children are cancelled and its Runner settles it stopped — StopWorkflow does not wait for
// that, the WorkflowPhaseEvent that ends it reports it — and a queued one is dropped from the line
// and its folder marked stopped. An id this Agent neither runs nor queues is an error.
func (a *Agent) StopWorkflow(id string) error {
	m := &a.background
	m.mu.Lock()
	run := m.liveLocked(id)
	if run == nil {
		m.mu.Unlock()
		return fmt.Errorf(unknownWorkflowFormat, id)
	}
	if run.running {
		run.cancel()
		m.mu.Unlock()
		return nil
	}
	m.dropLocked(run)
	m.mu.Unlock()
	return markStopped(run)
}

// RerunFailed runs the finished workflow id again, in the background and in its own folder, as a new
// run of the same workflow: the Runner skips every fan-out item whose receipt is ok or partial, so
// only the blocked ones — a child that declared itself blocked, or one whose faults outlasted its
// retries — run again, and the script and ask stages run afresh as any re-issue of finished work's
// do (workflow.Runner.Run). It is launched as a resumed one is (resumeBackground) — the plan read
// back from the folder, a recipe's prompt files and scripts from the recipe skill its status.json
// names — and waits in line behind another workflow on its server like any background launch. It
// does not wait for the run: the WorkflowPhaseEvents report it.
//
// Refused: a workflow this Agent runs or queues, one that has not finished (stopped, or cut off
// while running — its unfinished items would run too), one with nothing blocked, and one whose items
// no longer hash to its folder (the workspace moved under a source), which would otherwise run
// everything in a new folder. Like StartRecipe's background launch it reads the Agent for the
// launch-time snapshot, so a Driver calls it only at idle.
func (a *Agent) RerunFailed(id string) error {
	if a.background.isLive(id) {
		return fmt.Errorf(workflowAlreadyRunningFormat, id)
	}
	store, err := workflow.NewStore(a.ScratchDir())
	if err != nil {
		return err
	}
	status, err := store.ReadStatus(id)
	if err != nil {
		return fmt.Errorf("apogee: re-run workflow %s: %w", id, err)
	}
	if status.Phase != workflow.PhaseDone {
		return fmt.Errorf(rerunUnfinishedFormat, id, status.Phase)
	}
	if !hasBlockedItem(status) {
		return fmt.Errorf(rerunNothingFormat, id)
	}
	return a.resumeBackground(workflowEntryJSON{ID: id, Recipe: status.Recipe}, id)
}

// hasBlockedItem reports whether any item of status ended on a blocked receipt — the items a re-run
// runs again.
func hasBlockedItem(status workflow.RunStatus) bool {
	for _, stage := range status.Stages {
		for _, item := range stage.Items {
			if item.Receipt != nil && item.Receipt.Status == workflow.StatusBlocked {
				return true
			}
		}
	}
	return false
}

// ResumeWorkflows starts the background workflows the restored snapshot carried (its `workflows`
// key), each from its folder under the live `<scratch>/workflows/`: the plan is read back from the
// folder, a recipe's prompt files and scripts from the recipe skill the entry names, and the
// workflow is launched as a new one would be — so the finished fan-out items are skipped, and the
// settled script and ask stages replayed rather than run or asked again (see the file comment),
// which is what a session resume relies on. A workflow the manager
// already runs is skipped — one kept across the boundary (KeepWorkflows) that the incoming snapshot
// names too. The finish notes the snapshot held (its `workflow_notes` key) are adopted as held
// here too, after any the manager already holds, and are consumed like any held note: by the next
// Exchange's opening message, a Wake, or TakeWorkflowNotes. The Driver calls it after Bind or
// RestoreSession, once the scratch directory, catalog and tools are the session's; it is never
// called from a restore itself. The set is taken whole: an entry that cannot resume is reported in
// the joined error and dropped.
func (a *Agent) ResumeWorkflows() error {
	m := &a.background
	m.mu.Lock()
	entries := m.restored
	m.restored = nil
	m.notes = append(m.notes, m.restoredNotes...)
	m.restoredNotes = nil
	m.mu.Unlock()

	var errs []error
	for _, entry := range entries {
		if m.isLive(entry.ID) {
			continue
		}
		if err := a.resumeBackground(entry, ""); err != nil {
			errs = append(errs, fmt.Errorf("apogee: resume workflow %s: %w", entry.ID, err))
		}
	}
	return errors.Join(errs...)
}

// resumeBackground launches the workflow entry names from its folder — under the scratch directory
// its Home names (entryScratch). folder is the folder the launch must run in (a re-run,
// RerunFailed), or "" to find or create it by plan hash (a resume).
func (a *Agent) resumeBackground(entry workflowEntryJSON, folder string) error {
	store, err := workflow.NewStore(a.entryScratch(entry))
	if err != nil {
		return err
	}
	plan, err := store.ReadPlan(entry.ID)
	if err != nil {
		return err
	}
	turn := a.turns.snapshot().index
	launch := backgroundLaunch{plan: plan, tool: tools.FanOutToolName, turn: turn, folder: folder}
	if entry.Recipe == "" {
		runner, refusal := a.newWorkflowRunner(turn, domain.ToolCall{}, seatConfigured)
		if refusal != "" {
			return errors.New(refusal)
		}
		launch.runner = runner
	} else {
		recipe, err := a.recipeByID(entry.Recipe)
		if err != nil {
			return err
		}
		runner, err := a.newRecipeRunner(turn, domain.ToolCall{}, recipe, seatConfigured)
		if err != nil {
			return err
		}
		launch.recipe, launch.runner, launch.prompts, launch.tool = recipe.ID, runner, recipe.Files, recipeCallTool
	}
	launch.runner.Store = store // the folder's own store, which a kept workflow's Home moves off the session's
	_, err = a.startBackground(launch)
	return err
}

// entryScratch is the scratch directory entry's folder lives under: the session's own, or — for a
// workflow kept across a session boundary — the sibling directory its Home names.
func (a *Agent) entryScratch(entry workflowEntryJSON) string {
	scratch := a.ScratchDir()
	if entry.Home == "" || scratch == "" {
		return scratch
	}
	return filepath.Join(filepath.Dir(scratch), entry.Home)
}

// homeOf is the Home a live run's snapshot entry spells against the session scratch directory
// scratch: "" when its store is the session's own, the base name of its session scratch directory
// when that is a sibling of scratch (a workflow kept across a session boundary). A store that is
// neither cannot be spelled as a Home and is written as "", which a resume reports as a missing
// folder.
func homeOf(store *workflow.Store, scratch string) string {
	if scratch == "" {
		return ""
	}
	home := filepath.Dir(store.Root())
	if home == filepath.Clean(scratch) || filepath.Dir(home) != filepath.Dir(filepath.Clean(scratch)) {
		return ""
	}
	return filepath.Base(home)
}

// KeepWorkflows makes the next session boundary — the next ClearContext or RestoreSession, and
// only that one — keep the background workflows and their held finish notes rather than stopping
// the set: the Driver calls it just before the boundary when its human answered "keep them" (ADR
// 0089 D5). The kept workflows run on in the folders of the session that launched them, their
// finish notes reach the new conversation, and they ride its snapshot. The mark is consumed by the
// boundary whether or not it succeeds. A delegate runs no background workflow; it is a no-op there.
// Call it at an idle boundary, from the goroutine that calls the boundary.
func (a *Agent) KeepWorkflows() {
	if a.isDelegate() {
		return
	}
	m := &a.background
	m.mu.Lock()
	m.keep = true
	m.mu.Unlock()
}

// takeKeep reports and clears the KeepWorkflows mark.
func (m *backgroundManager) takeKeep() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	keep := m.keep
	m.keep = false
	return keep
}

// endSessionWorkflows is ClearContext's stop of the outgoing session's workflows: the snapshot's set
// and notes a restore loaded and ResumeWorkflows never took up are dropped — they belong to the
// session the clear ends — and, when a workflow is live, every live one is stopped with the held
// notes dropped (stopAllBackground). With nothing live the clear stops nothing, so it drops nothing
// held either: the finish notes of workflows that already ended survive it and reach the new
// conversation, as they would any clear the human was never asked about.
func (a *Agent) endSessionWorkflows() {
	m := &a.background
	m.mu.Lock()
	m.restored, m.restoredNotes = nil, nil
	live := len(m.runs) > 0
	if !live {
		m.launched = nil
	}
	m.mu.Unlock()
	if live {
		a.stopAllBackground()
	}
}

// startBackgroundRecipe launches recipe in the background over the inputs text binds, and returns
// the workflow's id. No one is asked for a missing required input — the conversation may be busy —
// so one is refused as `missing input: <name>`.
func (a *Agent) startBackgroundRecipe(recipe workflow.Recipe, text string) (string, error) {
	values, missing, err := workflow.BindInputs(recipe.Inputs, text)
	if err != nil {
		return "", err
	}
	if len(missing) > 0 {
		return "", fmt.Errorf(missingInputFormat, missing[0])
	}
	return a.startKeyedBackgroundRecipe(recipe, values, seatConfigured)
}

// startKeyedBackgroundRecipe launches recipe in the background over keyed inputs — the ones the
// user's text bound, or the ones a model's `fan_out{recipe, inputs, background}` named — with its
// children on seat, and returns the workflow's id. An undeclared key or a required input left unset
// is refused, never asked.
func (a *Agent) startKeyedBackgroundRecipe(recipe workflow.Recipe, given map[string]string, seat delegationSeat) (string, error) {
	inputs, err := completeInputs(recipe.Inputs, given)
	if err != nil {
		return "", err
	}
	turn := a.turns.snapshot().index
	runner, err := a.newRecipeRunner(turn, domain.ToolCall{}, recipe, seat)
	if err != nil {
		return "", err
	}
	return a.startBackground(backgroundLaunch{
		plan: bindPlanInputs(recipe, inputs), recipe: recipe.ID, runner: runner,
		prompts: recipe.Files, tool: recipeCallTool, turn: turn, seat: seat,
	})
}

// startBackground opens launch's workflow folder and hands the workflow to the manager: it starts at
// once when no background workflow runs on its server, and waits in line otherwise. It returns the
// workflow's id. The Runner is rebuilt around the folder's id and the launch-time host
// (backgroundHost) — its children spawned off the host and bracketed under the `workflow-<id>` call
// yet addressable through this Agent, its scripts run through the host's Resolution, its ask stages
// put through the manager's queue — and gets its width when it starts. A workflow already live under
// the same id (the same plan launched twice) is refused. It runs where the idle-only mutators cannot
// (an idle boundary, or a Step), which is what lets the host be read off this Agent unguarded.
func (a *Agent) startBackground(launch backgroundLaunch) (string, error) {
	if a.isDelegate() {
		return "", errDelegateBackground
	}
	status, err := openWorkflowFolder(launch.runner, launch.plan, launch.folder)
	if err != nil {
		return "", err
	}
	host := a.backgroundHost()
	call := domain.ToolCall{ID: backgroundCallPrefix + status.ID, Tool: launch.tool}
	spawner := host.newWorkflowSpawner(launch.turn, call, launch.prompts)
	spawner.children = &a.children
	spawner.seat = launch.seat
	launch.runner.Spawner = spawner
	if scripts, ok := launch.runner.Scripts.(*recipeScripts); ok {
		scripts.agent = host
	}
	observer := host.observeWorkflow(launch.runner, launch.turn, launch.plan)
	observer.background, observer.call = true, call.ID
	// Set after observeWorkflow, which would wrap it to report the question before it is queued: a
	// background question is reported once it waits in the queue (backgroundScope.announce).
	if launch.runner.Asker != nil {
		launch.runner.Asker = backgroundAsker{scope: backgroundScope{manager: &a.background, workflow: status.ID, observer: observer}}
	}
	run := &backgroundRun{
		id: status.ID, recipe: launch.recipe, server: host.backgroundServer(launch.seat), seat: launch.seat,
		plan: launch.plan, runner: launch.runner, host: host, turn: launch.turn, observer: observer,
		done: make(chan struct{}),
	}

	m := &a.background
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.liveLocked(run.id) != nil {
		return "", fmt.Errorf(workflowAlreadyRunningFormat, run.id)
	}
	m.runs = append(m.runs, run)
	m.launched = append(m.launched, keptRun{id: run.id, store: run.runner.Store})
	if !m.serverBusyLocked(run.server) {
		a.startRunLocked(run)
	}
	return run.id, nil
}

// openWorkflowFolder finds or creates the folder launch's Runner will run plan in, the way the
// Runner's own Run does (workflow.Runner.Run: every fanout with a source of its own expanded, the
// PlanHash over them, the newest folder with that hash, else a new one), so the Run that follows
// resumes this very folder. It is opened at launch rather than at start so a queued workflow already
// has the id and status path it is listed and stopped by. A non-empty folder is the one a re-run
// must find: a plan whose hash leads anywhere else — no folder, or a newer one — is refused, and
// nothing is created.
func openWorkflowFolder(runner *workflow.Runner, plan workflow.Plan, folder string) (workflow.RunStatus, error) {
	if problems := workflow.Validate(plan); len(problems) > 0 {
		texts := make([]string, 0, len(problems))
		for _, problem := range problems {
			texts = append(texts, problem.String())
		}
		return workflow.RunStatus{}, fmt.Errorf("workflow: invalid plan: %s", strings.Join(texts, "; "))
	}
	var items []workflow.Item
	for _, stage := range plan.Stages {
		if stage.Kind != workflow.StageFanout || stage.Over == nil || stage.Over.Stage != "" {
			continue
		}
		expanded, err := workflow.Expand(*stage.Over, runner.Workspace, runner.Split)
		if err != nil {
			return workflow.RunStatus{}, fmt.Errorf("workflow: stage %q: %w", stage.Name, err)
		}
		items = append(items, expanded...)
	}
	planHash, err := workflow.PlanHash(plan, nil, items)
	if err != nil {
		return workflow.RunStatus{}, err
	}
	status, found, err := runner.Store.Find(planHash)
	if err != nil {
		return status, err
	}
	if folder != "" && (!found || status.ID != folder) {
		return workflow.RunStatus{}, fmt.Errorf(rerunMovedFormat, folder)
	}
	if found {
		return status, nil
	}
	return runner.Store.Create(plan, planHash, time.Now())
}

// startRunLocked starts run on a goroutine of its own, at the background width its host states,
// under a context that nothing but the manager cancels and that routes its gates and questions to
// the manager's queue. The caller holds the manager's lock; it may be a previous workflow's
// goroutine (endBackground), which is why nothing here reads this Agent beyond the manager.
func (a *Agent) startRunLocked(run *backgroundRun) {
	scope := backgroundScope{manager: &a.background, workflow: run.id, observer: run.observer}
	ctx, cancel := context.WithCancel(withBackgroundPrompts(context.Background(), scope))
	run.running, run.cancel = true, cancel
	run.runner.Width = run.host.backgroundWidth(run.seat)
	go a.driveBackground(ctx, run)
}

// driveBackground runs one background workflow to its end, reporting its phases through its host as
// a blocking workflow's are reported, then hands its server to the next in line. The finish note is
// held before the end is reported, so a Driver that wakes on that event finds it.
func (a *Agent) driveBackground(ctx context.Context, run *backgroundRun) {
	result, err := run.runner.Run(ctx, run.plan)
	a.background.hold(finishNote(run.plan.Name, result, err, seatFellBack(run.runner)))
	run.observer.end(result, err)
	a.endBackground(run)
}

// endBackground retires a run that ended and starts the next workflow queued on its server.
func (a *Agent) endBackground(run *backgroundRun) {
	m := &a.background
	m.mu.Lock()
	defer m.mu.Unlock()
	run.cancel()
	m.dropLocked(run)
	for _, next := range m.runs {
		if !next.running && next.server == run.server {
			a.startRunLocked(next)
			return
		}
	}
}

// stopAllBackground stops every background workflow — the queued ones dropped and marked stopped,
// the running ones cancelled — and waits for them all to end. Their finished items are kept.
func (a *Agent) stopAllBackground() {
	m := &a.background
	m.mu.Lock()
	var dropped []*backgroundRun
	var ending []chan struct{}
	for _, run := range slices.Clone(m.runs) {
		if run.running {
			run.cancel()
			ending = append(ending, run.done)
			continue
		}
		m.dropLocked(run)
		dropped = append(dropped, run)
	}
	m.mu.Unlock()
	for _, run := range dropped {
		_ = markStopped(run) // best-effort, like every stop on the way out: nobody is left to tell
	}
	for _, done := range ending {
		<-done
	}
	// The whole set stops only when its session ends (Close, and a ClearContext or RestoreSession not
	// told to keep it), so the notes held for that session — the ones these stops just left included
	// — have no one left to read them. Taking them here, after every run has ended, is also what
	// leaves a Driver that wakes on those runs' end events nothing to wake on.
	a.background.takeNotes()
	m.mu.Lock()
	m.launched = nil
	m.mu.Unlock()
}

// backgroundHost is the snapshot of this top-level Agent a background workflow runs off (see the file
// comment): a detached Agent that nothing mutates once it is returned. The idle-only fields — the
// Config, the Upstream, the tool set, the profile's parsers, the context files, the journal and the
// effort dialect — are copied as they stand; the lock-guarded live settings are read through their
// accessors and frozen, all but the mode, which the host holds as a tighten-only view of this
// Agent's (liveMode), the way a delegate holds its parent's. The tree's shared handles are shared.
// The host owns no Upstream and no tool set (it never closes or recomposes either), runs no Turn of
// its own — its conversation stays empty and its Turn lifecycle idle — and starts its own token
// estimator, so a Step calibrating this Agent's never races it. Call it where the idle-only
// mutators cannot run: at an idle boundary or inside a Step.
func (a *Agent) backgroundHost() *Agent {
	a.parallelAgentsMu.RLock()
	parallelAgents, farWidth := a.parallelAgents, a.farWidth
	a.parallelAgentsMu.RUnlock()
	host := &Agent{
		cfg:                a.cfg,
		upstream:           a.upstream,
		dial:               a.dial,
		builtins:           a.builtinLadder(),
		armed:              a.armed,
		tools:              a.tools,
		guards:             a.guards,
		textParser:         a.textParser,
		stripper:           a.stripper,
		mode:               a.Mode(),
		confineToWorkspace: a.ConfineToWorkspace(),
		scratchDir:         a.ScratchDir(),
		gen:                a.Generation(),
		compaction:         a.compactionEnabled(),
		prune:              a.pruneEnabled(),
		contextFileNames:   a.contextFileList(),
		parallelAgents:     parallelAgents,
		farWidth:           farWidth,
		delegation:         a.delegation,
		seat:               a.subAgentsSeat(),
		effortOverride:     a.effortOverrideValue(),
		effortDialect:      a.effortDialect,
		liveMode:           a.effectiveMode,
		tokens:             apogeectx.NewTokenEstimator(),
		prompts:            a.prompts,
		now:                a.now,
		contextFiles:       a.contextFiles,
		journal:            a.journal,
		undoNote:           a.undoNote,
		consoles:           a.consoles,
		runIDs:             a.runIDs,
		tasks:              a.tasks,
		tree:               a.tree,
		depth:              a.depth,
		cancelFoldBound:    a.cancelFoldBound,
	}
	host.turns = &turnLifecycle{conv: &host.conv, observer: host}
	return host
}

// backgroundWidth is how many children a background workflow on seat runs at once: the width of the
// server they run on minus the one slot the conversation keeps, and never below one (ADR 0089 D2).
// A session-seated workflow runs on the session server whatever is latched, so its width is that
// server's cap.
func (a *Agent) backgroundWidth(seat delegationSeat) int {
	width := a.delegationCap()
	if seat == seatSession {
		width = a.parallelAgentsCap()
	}
	return max(width-1, 1)
}

// backgroundServer names the server a background workflow's children on seat run on, which is what
// the one-at-a-time line is kept per: the session's endpoint for a session-seated workflow, else
// the latched Delegation target's endpoint, else — nothing latched, where a sub-agents-server ask
// falls back too — the session's.
func (a *Agent) backgroundServer(seat delegationSeat) string {
	if seat == seatSession {
		return a.cfg.Endpoint
	}
	if target := a.delegationTarget(); target != nil {
		return target.Endpoint
	}
	return a.cfg.Endpoint
}

// markStopped writes a queued run's folder as stopped: it never started, so no Runner will.
func markStopped(run *backgroundRun) error {
	status, err := run.runner.Store.ReadStatus(run.id)
	if err != nil {
		return err
	}
	status.Phase = workflow.PhaseStopped
	status.Updated = time.Now()
	return run.runner.Store.WriteStatus(status)
}

// liveLocked returns the live run id names, or nil. The caller holds the lock.
func (m *backgroundManager) liveLocked(id string) *backgroundRun {
	for _, run := range m.runs {
		if run.id == id {
			return run
		}
	}
	return nil
}

// isLive reports whether a run under id is running or queued.
func (m *backgroundManager) isLive(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.liveLocked(id) != nil
}

// liveStates maps every live run's id to whether it is queued.
func (m *backgroundManager) liveStates() map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	states := make(map[string]bool, len(m.runs))
	for _, run := range m.runs {
		states[run.id] = !run.running
	}
	return states
}

// serverBusyLocked reports whether a background workflow is running on server. The caller holds
// the lock.
func (m *backgroundManager) serverBusyLocked(server string) bool {
	return slices.ContainsFunc(m.runs, func(run *backgroundRun) bool { return run.running && run.server == server })
}

// dropLocked removes run from the live set and closes its done. The caller holds the lock.
func (m *backgroundManager) dropLocked(run *backgroundRun) {
	index := slices.Index(m.runs, run)
	if index < 0 {
		return
	}
	m.runs = slices.Delete(m.runs, index, index+1)
	close(run.done)
}

// entries is the manager's part of the session snapshot: the live runs in launch order — each with
// the Home its folder is under, spelled against the session scratch directory scratch (homeOf) —
// then the restored set not resumed yet, so a snapshot taken before ResumeWorkflows loses nothing.
// Nil for none, so a session with no background workflow writes no `workflows` key.
func (m *backgroundManager) entries(scratch string) []workflowEntryJSON {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []workflowEntryJSON
	for _, run := range m.runs {
		out = append(out, workflowEntryJSON{ID: run.id, Recipe: run.recipe, Home: homeOf(run.runner.Store, scratch)})
	}
	for _, entry := range m.restored {
		if m.liveLocked(entry.ID) == nil {
			out = append(out, entry)
		}
	}
	return out
}

// load replaces the restored set and notes with a snapshot's, which checkRestoredWorkflows and
// checkRestoredWorkflowNotes have already held to their shape. Nothing starts and no note is held:
// ResumeWorkflows does both.
func (m *backgroundManager) load(entries []workflowEntryJSON, notes []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.restored = slices.Clone(entries)
	m.restoredNotes = slices.Clone(notes)
}

// heldNotes is the manager's other part of the session snapshot: the notes held and not yet
// delivered, oldest first, then the restored ones ResumeWorkflows has not adopted yet. Nil for none,
// so a session with nothing held writes no `workflow_notes` key.
func (m *backgroundManager) heldNotes() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.notes)+len(m.restoredNotes) == 0 {
		return nil
	}
	return append(slices.Clone(m.notes), m.restoredNotes...)
}

// waitAll waits for every live run to end.
func (m *backgroundManager) waitAll() {
	m.mu.Lock()
	dones := make([]chan struct{}, 0, len(m.runs))
	for _, run := range m.runs {
		dones = append(dones, run.done)
	}
	m.mu.Unlock()
	for _, done := range dones {
		<-done
	}
}

// checkRestoredWorkflows holds a snapshot's `workflows` entries (agentState.Workflows) to what the
// manager writes: each id a single folder name of the store's alphabet — so it can only resolve to
// a folder directly under `<scratch>/workflows/` — named once, and each recipe id a single name.
// Each refusal wraps ErrSnapshotRefused.
func checkRestoredWorkflows(entries []workflowEntryJSON) error {
	seen := make(map[string]bool, len(entries))
	for i, entry := range entries {
		refuse := func(format string, args ...any) error {
			return fmt.Errorf("apogee: decode session state: workflow %d "+format+": %w",
				append(append([]any{i}, args...), ErrSnapshotRefused)...)
		}
		switch {
		case !isStoreFolderName(entry.ID):
			return refuse("has the id %q, which names no folder under the workflow store", entry.ID)
		case seen[entry.ID]:
			return refuse("repeats the id %q", entry.ID)
		case entry.Recipe != "" && !isRecipeName(entry.Recipe):
			return refuse("names the recipe %q, which is not a skill id", entry.Recipe)
		case entry.Home != "" && !isSessionDirName(entry.Home):
			return refuse("names the home %q, which is not a session scratch directory", entry.Home)
		}
		seen[entry.ID] = true
	}
	return nil
}

// checkRestoredWorkflowNotes checks the held finish notes a snapshot carries (the `workflow_notes`
// key). A note reaches the model as recorded text in an Exchange's opening message, so it is held to
// the rules of the text a restore submits: one line each — a finish note is always one (oneLine) —
// opening with no fence apogee never commits, and all of them together within the byte bound a
// restored message is held to. Anything else refuses the whole payload.
func checkRestoredWorkflowNotes(notes []string) error {
	total := 0
	for i, note := range notes {
		refuse := func(format string, args ...any) error {
			return fmt.Errorf("apogee: decode session state: workflow note %d "+format+": %w",
				append(append([]any{i}, args...), ErrSnapshotRefused)...)
		}
		total += len(note)
		switch {
		case strings.ContainsAny(note, "\r\n"):
			return refuse("spans more than one line")
		case total > maxRestoredMessageBytes:
			return refuse("takes the notes past the %d-byte limit", maxRestoredMessageBytes)
		}
		if fence, forged := forgesRestoredStructure(note); forged {
			return refuse("opens with %q, which apogee never commits", fence)
		}
	}
	return nil
}

// isStoreFolderName reports whether id is a folder name of the workflow store's alphabet: lower-case
// letters, digits and dashes, not led by a dash, within maxRestoredWorkflowIDBytes. No such name is
// a separator, a `..` or a volume.
func isStoreFolderName(id string) bool {
	if id == "" || len(id) > maxRestoredWorkflowIDBytes || id[0] == '-' {
		return false
	}
	return !strings.ContainsFunc(id, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-')
	})
}

// isSessionDirName reports whether name can be a session scratch directory's base name — a session
// id, `YYYYMMDDTHHMMSSZ-<hex>`: ASCII letters of either case, digits and dashes, not led by a dash,
// within maxRestoredWorkflowIDBytes. No such name is a separator, a `..` or a volume, so a Home
// resolves to a sibling of the session's scratch directory and nowhere else.
func isSessionDirName(name string) bool {
	if name == "" || len(name) > maxRestoredWorkflowIDBytes || name[0] == '-' {
		return false
	}
	return !strings.ContainsFunc(name, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-')
	})
}

// isRecipeName reports whether id is a single skill name: printable, within
// maxRestoredRecipeIDBytes, no separator, no leading dot.
func isRecipeName(id string) bool {
	if len(id) > maxRestoredRecipeIDBytes || strings.HasPrefix(id, ".") {
		return false
	}
	return !strings.ContainsFunc(id, func(r rune) bool {
		return r <= ' ' || r == 0x7f || r == '/' || r == '\\' || r == ':'
	})
}

// backgroundPrompt is one approval or question a background workflow waits on: the id the manager
// minted for it as it was queued, the workflow's id, the request or the question, and the channel
// its answer arrives on (buffered, so answering never blocks).
type backgroundPrompt struct {
	id       uint64
	workflow string
	approval *domain.ApprovalRequest
	question *workflow.Question
	reply    chan backgroundAnswer
}

// backgroundAnswer answers a backgroundPrompt: the decision an approval takes, the text a question
// takes, or why neither could be had.
type backgroundAnswer struct {
	decision domain.ApprovalDecision
	text     string
	err      error
}

// backgroundScope is the manager a background workflow's prompts queue in, the workflow's id, and
// the observer that reports each prompt once it is queued.
type backgroundScope struct {
	manager  *backgroundManager
	workflow string
	observer *workflowObserver
}

// backgroundPromptsKey is the context key withBackgroundPrompts sets.
type backgroundPromptsKey struct{}

// withBackgroundPrompts marks ctx as a background workflow's: every approval raised under it
// (queuedApprover) waits in scope's manager instead of reaching the Driver.
func withBackgroundPrompts(ctx context.Context, scope backgroundScope) context.Context {
	return context.WithValue(ctx, backgroundPromptsKey{}, scope)
}

// backgroundPromptsFrom returns the background scope ctx carries, if any.
func backgroundPromptsFrom(ctx context.Context) (backgroundScope, bool) {
	scope, ok := ctx.Value(backgroundPromptsKey{}).(backgroundScope)
	return scope, ok
}

// approve queues req and waits for its decision. Cancelled while waiting, it answers what a
// cancelled visible prompt answers: deny, with ctx's error.
func (s backgroundScope) approve(ctx context.Context, req domain.ApprovalRequest) (domain.ApprovalDecision, error) {
	answer, err := s.manager.wait(ctx, &backgroundPrompt{workflow: s.workflow, approval: &req}, s.announce)
	if err != nil {
		return domain.ApprovalDeny, err
	}
	return answer.decision, answer.err
}

// backgroundAsker puts a background workflow's `ask` stage questions in the manager's queue
// (workflow.Asker).
type backgroundAsker struct {
	scope backgroundScope
}

// Ask queues the question and waits for its answer.
func (b backgroundAsker) Ask(ctx context.Context, question workflow.Question) (string, error) {
	answer, err := b.scope.manager.wait(ctx, &backgroundPrompt{workflow: b.scope.workflow, question: &question}, b.scope.announce)
	if err != nil {
		return "", err
	}
	return answer.text, answer.err
}

// announce reports prompt as waiting (a WorkflowWaiting phase event): the question's text for an
// `ask` stage, the tool a gate asks about for an approval.
func (s backgroundScope) announce(prompt *backgroundPrompt) {
	if s.observer == nil {
		return
	}
	switch {
	case prompt.question != nil:
		s.observer.waitingOn(s.workflow, prompt.question.Stage, prompt.question.Text)
	case prompt.approval != nil:
		s.observer.waitingOn(s.workflow, "", approvalWaitingPrefix+prompt.approval.Tool)
	}
}

// approvalWaitingPrefix leads a waiting approval's WorkflowWaiting Detail; the tool's name follows.
const approvalWaitingPrefix = "approve "

// wait queues prompt under a fresh id, announces it once it is queued, and blocks until it is
// answered or ctx ends, when it is withdrawn.
func (m *backgroundManager) wait(ctx context.Context, prompt *backgroundPrompt, announce func(*backgroundPrompt)) (backgroundAnswer, error) {
	prompt.reply = make(chan backgroundAnswer, 1)
	m.mu.Lock()
	m.promptID++
	prompt.id = m.promptID
	m.prompts = append(m.prompts, prompt)
	m.mu.Unlock()
	if announce != nil {
		announce(prompt)
	}
	select {
	case answer := <-prompt.reply:
		return answer, nil
	case <-ctx.Done():
		m.withdraw(prompt)
		return backgroundAnswer{}, ctx.Err()
	}
}

// waiting returns the prompts waiting for an answer, oldest first.
func (m *backgroundManager) waiting() []*backgroundPrompt {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.prompts)
}

// answer answers prompt and reports whether it was still waiting.
func (m *backgroundManager) answer(prompt *backgroundPrompt, answer backgroundAnswer) bool {
	if !m.withdraw(prompt) {
		return false
	}
	prompt.reply <- answer
	return true
}

// WorkflowPrompts lists the approvals and questions this session's background workflows wait on,
// oldest first, each with the id AnswerWorkflowPrompt takes (ADR 0089). An `ask` stage's question
// offers its options as the request's Choices; an answer outside them takes the stage's default.
// A delegate runs no background workflow and lists none. Safe from any goroutine.
func (a *Agent) WorkflowPrompts() []domain.WorkflowPrompt {
	m := &a.background
	m.mu.Lock()
	defer m.mu.Unlock()
	prompts := make([]domain.WorkflowPrompt, 0, len(m.prompts))
	for _, prompt := range m.prompts {
		listed := domain.WorkflowPrompt{ID: prompt.id, Workflow: prompt.workflow}
		if run := m.liveLocked(prompt.workflow); run != nil {
			listed.Name = run.plan.Name
		}
		switch {
		case prompt.approval != nil:
			request := *prompt.approval
			listed.Approval = &request
		case prompt.question != nil:
			listed.Question = &domain.AskRequest{
				Question: prompt.question.Text,
				Choices:  slices.Clone(prompt.question.Options),
			}
		}
		prompts = append(prompts, listed)
	}
	return prompts
}

// AnswerWorkflowPrompt answers the waiting prompt id — an approval with answer.Decision, a question
// with answer.Text — and reports whether it was still waiting: false for an id already answered,
// withdrawn by its workflow's stop, or never minted. Safe from any goroutine.
func (a *Agent) AnswerWorkflowPrompt(id uint64, answer domain.WorkflowPromptAnswer) bool {
	m := &a.background
	m.mu.Lock()
	index := slices.IndexFunc(m.prompts, func(prompt *backgroundPrompt) bool { return prompt.id == id })
	var prompt *backgroundPrompt
	if index >= 0 {
		prompt = m.prompts[index]
	}
	m.mu.Unlock()
	if prompt == nil {
		return false
	}
	return m.answer(prompt, backgroundAnswer{decision: answer.Decision, text: answer.Text})
}

// withdraw takes prompt off the queue and reports whether it was on it.
func (m *backgroundManager) withdraw(prompt *backgroundPrompt) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := slices.Index(m.prompts, prompt)
	if index < 0 {
		return false
	}
	m.prompts = slices.Delete(m.prompts, index, index+1)
	return true
}

// Wake opens an Exchange on the finish notes of the background workflows that ended while the agent
// was idle — the wake (ADR 0089 D3, ADR 0007 as amended): the notes are taken and queued as the
// Exchange's opening message (renderWorkflowNotes), which the Driver then Steps exactly as it Steps
// a Submitted one, so the reply is bounded by the Mode and the approval rules like any other. It
// reports whether it opened one.
//
// It opens nothing, and leaves the notes held, when there is nothing to wake on or no reason to
// wake: ctx is already done (its error is returned); `workflow-wake: off` is set (the notes ride
// the next message instead); this is a delegate; no note is held; or an Exchange is running or
// input is queued — the running Exchange takes the notes through the Driver's drain
// (TakeWorkflowNotes), the queued input when it opens its Exchange. With no model bound it refuses
// as Submit does. Call it from the goroutine that Submits, at an idle boundary — typically on the
// WorkflowPhaseEvent that ends a background workflow, or when an Exchange ends with notes held.
func (a *Agent) Wake(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !a.cfg.Workflow.ResolvedWake() || a.isDelegate() {
		return false, nil
	}
	if a.cfg.Model == "" {
		return false, errNoModelBound
	}
	notes := a.background.takeNotes()
	if len(notes) == 0 {
		return false, nil
	}
	if err := a.turns.submit(domain.UserInput{Text: renderWorkflowNotes(notes)}); err != nil {
		a.background.putBack(notes)
		if errors.Is(err, domain.ErrInputPending) {
			return false, nil // the running Exchange or the queued input takes them instead
		}
		return false, err
	}
	return true, nil
}

// finishNote is the one line a background workflow's end leaves for the parent (ADR 0089 D3): its
// name and how it ended, its items counted by status across its fan-outs (with the unfinished ones
// and the verify verdicts when there are any), and where to read more — the report a merge wrote,
// else the full item listing. A run that could not proceed says so with its cause instead. When any
// item child asked for the Sub-agent server and ran on the session one (fellBack, seatFellBack), a
// finished or stopped run's note ends on SeatFallbackNote, once — the answer-level line a blocking
// workflow's call gets from workflowAnswer, since this note is what the model reads of a background
// run (ADR 0069 decision 9). It joins as one more part, so the note stays one line.
func finishNote(name string, result workflow.Result, runErr error, fellBack bool) string {
	name = oneLine(name)
	if runErr != nil {
		return fmt.Sprintf(finishFailedFormat, name, oneLine(runErr.Error()))
	}
	phase := finishPhaseFinished
	if result.Stopped() {
		phase = finishPhaseStopped
	}
	parts := []string{fmt.Sprintf(finishLeadFormat, name, phase), finishTally(result)}
	switch {
	case result.Report != "":
		parts = append(parts, finishReportPrefix+result.Report)
	case result.Listing != "":
		parts = append(parts, finishListingPrefix+result.Listing)
	}
	if fellBack {
		parts = append(parts, SeatFallbackNote)
	}
	return strings.Join(parts, finishSeparator)
}

// finishTally counts result's fan-out items by status — `items N · ok A · partial B · blocked C` —
// adding the unfinished count and the verify verdicts only when there are any.
func finishTally(result workflow.Result) string {
	var tally workflow.Tally
	for _, stage := range result.Stages {
		if stage.Kind != workflow.StageFanout || stage.Phase == workflow.PhaseSkipped {
			continue
		}
		tally.OK += stage.Tally.OK
		tally.Partial += stage.Tally.Partial
		tally.Blocked += stage.Tally.Blocked
		tally.Unfinished += stage.Tally.Unfinished
		tally.Confirmed += stage.Tally.Confirmed
		tally.Refuted += stage.Tally.Refuted
		tally.Unclear += stage.Tally.Unclear
	}
	total := tally.OK + tally.Partial + tally.Blocked + tally.Unfinished
	parts := []string{
		fmt.Sprintf("items %d", total),
		fmt.Sprintf("ok %d", tally.OK),
		fmt.Sprintf("partial %d", tally.Partial),
		fmt.Sprintf("blocked %d", tally.Blocked),
	}
	if tally.Unfinished > 0 {
		parts = append(parts, fmt.Sprintf("unfinished %d", tally.Unfinished))
	}
	if tally.Confirmed+tally.Refuted+tally.Unclear > 0 {
		parts = append(parts,
			fmt.Sprintf("confirmed %d", tally.Confirmed),
			fmt.Sprintf("refuted %d", tally.Refuted),
			fmt.Sprintf("unclear %d", tally.Unclear),
		)
	}
	return strings.Join(parts, finishTallySeparator)
}

// oneLine folds every run of whitespace in text, line breaks included, into one space, so a note
// stays the one line ADR 0089 D3 promises whatever a name or an error spells.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// renderWorkflowNotes is the recorded text notes reach the model as — the fixed header, then one
// line per note — whether it opens a wake, is interjected into a running Exchange, or follows the
// human's own message.
func renderWorkflowNotes(notes []string) string {
	return workflowNoteHeader + "\n" + strings.Join(notes, "\n")
}

// hold keeps note until the Driver's drain, a wake or the next opening message takes it.
func (m *backgroundManager) hold(note string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notes = append(m.notes, note)
}

// takeNotes takes every held note, oldest first; nil when none is held.
func (m *backgroundManager) takeNotes() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	notes := m.notes
	m.notes = nil
	return notes
}

// putBack returns notes a taker could not deliver to the front of the held ones, ahead of any held
// since, so the order they ended in is kept.
func (m *backgroundManager) putBack(notes []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notes = append(slices.Clone(notes), m.notes...)
}
