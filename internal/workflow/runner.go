package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// outputName is the file an item's detail output lands in when its stage sets no `out:` — inside
// the item's own folder, next to its receipt.
const outputName = "output.md"

// redidFormat is the note a stage carries when it redid items an earlier run of its folder had
// finished (redidNote): the count, then "item" or "items".
const redidFormat = "redid %d finished %s: their inputs changed since they ran"

// The brief placeholders a stage's task is rendered with.
const (
	placeholderItem = "{item}"
	placeholderOut  = "{out}"
)

// Ending is how a child's run ended, as the Spawner read it off the child.
type Ending string

// The four endings.
const (
	// EndCompleted is a child that ended on its own: with a receipt when it called `finish`, without
	// one when it stopped talking first.
	EndCompleted Ending = "completed"
	// EndCapped is a child that reached its step cap; its closing Turn may still have left a receipt.
	EndCapped Ending = "capped"
	// EndFaulted is a child whose run failed: a provider fault, a Run error, a refused spawn.
	EndFaulted Ending = "faulted"
	// EndStopped is a child a cancel ended; its work is unfinished and restarts fresh on resume.
	EndStopped Ending = "stopped"
)

// Spawner runs one workflow child for an item. The agent implements it through the recursion
// point (ADR 0014); tests script a fake. Spawn blocks until the child ends and reports how, and
// honours ctx: a cancel ends the child and Spawn returns EndStopped (or ctx's error). An error is a
// child that could not run at all; the Runner treats it like a fault.
type Spawner interface {
	Spawn(ctx context.Context, spec ItemSpec) (Outcome, error)
}

// ItemSpec is everything one child of a stage needs: which workflow, stage and item it serves, the
// brief rendered for the item, where its detail output goes, and — on a continuation — the rounds
// before it.
type ItemSpec struct {
	// Workflow is the workflow's id (its folder name under the store).
	Workflow string
	// Stage is the stage the child runs for: its Returns, Context, Tools and Prompt are the child's.
	// A verify child's Returns is the engine's `verdict` field, never the author's.
	Stage Stage
	// Item is the child's share of the stage, and Key the item's folder name. Two items of one
	// stage may share a Key (a fanout list is not deduplicated); Index tells them apart.
	Item Item
	Key  string
	// Name is the item's short name (ItemName), the one a Driver names its child by; Item.Label
	// stays the item's identity.
	Name string
	// Index is the item's 0-based place in its stage.
	Index int
	// RepeatRound is the repeat round the stage runs in: 0 for its own run, n for a repeat's n-th
	// re-run of it.
	RepeatRound int
	// Brief is the stage's `task:` with {item} and {out} rendered. A verify or merge child's Brief
	// leads with the engine's own brief for the stage kind (verify refutes the item's claim, merge
	// writes the report), the stage's brief after it. When the stage names its brief as a `prompt:`
	// file instead, Brief holds only that engine lead (empty for a fanout), and the Spawner reads and
	// renders the file the same way and puts it after the lead.
	Brief string
	// Output is the path the child writes its detail output to: the rendered `out:` when the stage
	// sets one, else an absolute path inside the item's folder.
	Output string
	// Attempt is the 1-based number of the fresh child this spec starts (1 plus the retries used).
	Attempt int
	// Prior holds the earlier rounds of this attempt, oldest first, when the spec continues a
	// capped child; empty for a fresh start. The Spawner seeds the child with them the ADR 0086 way.
	Prior []Round
}

// Round is one earlier round of a continued item: the receipt it left (nil when none), the report
// the engine kept of it (the child's closing text or fold), and the output path it was writing.
type Round struct {
	Receipt *Receipt
	Report  string
	Output  string
}

// Outcome is how one child ended: its Ending, the receipt its `finish` call handed back (nil when
// it never called it), a report the engine kept of the run (the closing text or fold of a capped
// child, the cause of a fault), and the child's conversation for the item's transcript.
type Outcome struct {
	Ending     Ending
	Receipt    *Receipt
	Report     string
	Transcript []domain.Message
}

// Observer receives the Runner's stage and item phase changes, for a Driver to show. The Runner
// serialises its calls, so an Observer needs no locking of its own; a call must not block.
type Observer interface {
	StagePhase(event StageEvent)
	ItemPhase(event ItemEvent)
}

// StageEvent is one stage's phase change. RepeatRound is the repeat round the stage runs in (0 for
// its own run, n for a repeat's n-th re-run of it). Items is the stage's item count on the running
// phase of a stage that runs children — fanout, verify, merge — and 0 otherwise.
type StageEvent struct {
	Workflow    string
	Stage       string
	Kind        StageKind
	Phase       Phase
	RepeatRound int
	Items       int
}

// ItemEvent is one item's phase change. Attempt and Round are 1-based and zero for an item that
// never ran (pending, or skipped on resume) — Round is the continuation round within the attempt;
// RepeatRound is the stage's repeat round, as on StageEvent. Receipt is set once the item is done.
// Name is the item's short name (ItemName) for a Driver to show; Label stays its identity.
type ItemEvent struct {
	Workflow    string
	Stage       string
	Index       int
	Key         string
	Label       string
	Name        string
	Phase       Phase
	Attempt     int
	Round       int
	RepeatRound int
	Receipt     *Receipt
}

// ErrFolderMoved refuses an Open of a named folder the plan no longer leads to: its PlanHash finds
// no folder, or a different one — the items changed since the folder ran.
var ErrFolderMoved = errors.New("workflow: the plan no longer leads to the named folder")

// Runner runs a Workflow's stages over their items (ADR 0087): each item is one fresh child from
// the Spawner, at most Width at a time, its receipt and transcript saved in the workflow folder as
// it finishes, so a cancel keeps every finished item and a re-issue of the same plan skips them.
//
// Retries and Continuations bound the second chances, and they are configuration, never a plan's
// parameter (ADR 0087 D1). A capped child — with a partial receipt or none — is continued first,
// up to Continuations times, each continuation a fresh child seeded with the rounds before it; past
// that, and for a faulted child or one that ended without a receipt, the item restarts fresh up to
// Retries times. An item out of both ends on its last partial receipt, or on a `blocked` one the
// Runner writes saying why.
type Runner struct {
	// Spawner runs the children. Required.
	Spawner Spawner
	// Store is the session's workflow store. Required.
	Store *Store
	// Workspace is the workspace the items and context files are read from. Required.
	Workspace fs.FS
	// Prompts is where a stage's `prompt:` file is read from to key its items: the recipe skill's
	// folder (Recipe.Files), the same source its children read the file from. Nil means Workspace.
	Prompts fs.FS
	// Split is the part size a `split:` source cuts to (NewSplitBudget).
	Split SplitBudget
	// Width is how many children run at once; below 1 means one.
	Width int
	// Retries is how many times an item restarts fresh after a fault, a missing receipt, or a cap
	// its continuations did not clear.
	Retries int
	// Continuations is how many times a capped child is continued within one attempt.
	Continuations int
	// Scripts runs a script stage's command. Required when the plan has a script stage.
	Scripts ScriptRunner
	// Asker puts an ask stage's question to the user. Nil means no one is there to ask (an
	// unattended Driver): every ask stage takes its default and its result says so.
	Asker Asker
	// Observer, when set, receives every stage and item phase change.
	Observer Observer
	// Recipe is the id of the recipe skill the plan comes from, "" for a fan_out's plan. Run records
	// it in status.json (RunStatus.Recipe), so a later re-run of the folder finds the prompt files and
	// scripts its stages read.
	Recipe string
	// Now is the clock status.json is stamped with; nil means time.Now.
	Now func() time.Time
	// Admit, when set, is asked by Run whether it may run in the folder it found or created, with
	// that folder's id, before Run writes to a found folder; nil admits every folder. A refusal ends
	// Run with Admit's error and a zero Result, the found folder left as it was. A created folder is
	// already written when Admit is asked, so a guard for a live folder — always a found one — sees
	// every folder another run may be driving.
	Admit func(id string) error
}

// Result is a run's outcome: the workflow's id and folder, its final phase — done, or stopped when
// a cancel ended it — every stage's items and tallies in plan order, the merge stage's report, and
// the full item listing. Format renders it as the lines the parent reads.
type Result struct {
	ID     string
	Dir    string
	Phase  Phase
	Stages []StageResult
	// Report is the path of the report.md a merge stage wrote; empty when the plan has no merge or
	// the merge did not write one.
	Report string
	// ReportMissing says why a merge stage that ran left no report — its child blocked, stopped, or
	// claimed a report it never wrote. The items' results stand either way.
	ReportMissing string
	// Listing is the path of items.md, the full item listing the Runner writes as the run ends;
	// Format points to it once there are too many items to list.
	Listing string
}

// Stopped reports whether a cancel ended the run before every item finished.
func (r Result) Stopped() bool { return r.Phase == PhaseStopped }

// StageResult is one stage's items, in item order, and their tally. Phase is done or stopped;
// skipped for a stage its `when:` turned off; failed for a merge that left no report, a script
// that ended blocked, or a pick that could not read its file. A script or ask stage has one item,
// labelled with the stage's name, carrying its receipt; a pick has none (its items are the next
// fanout's). Note says in one line what the stage came to when its items do not — why it was
// skipped, what a pick picked, that an ask took its default, how many finished items a
// child-running stage redid because their inputs changed. Round is the repeat round the result
// comes from: 0 for the stage's own run, n for a repeat stage's n-th re-run of it.
type StageResult struct {
	Name  string
	Kind  StageKind
	Phase Phase
	Items []ItemResult
	Tally Tally
	Note  string
	Round int
}

// ItemResult is one item's end: its phase (done, or stopped/pending when a cancel left it
// unfinished), the receipt it ended on, where its detail output is, how many fresh children and
// continuations it took, whether an earlier run of the same workflow had already finished it, and
// — once a verify stage checked it — the verdict (empty when no verify stage selected it).
//
// older is the item under every older key scheme, keySchemes[1:] index for index: a verify or
// merge downstream keys its own items under each older scheme from these, so a folder an older
// build wrote resumes the whole chain (nil on a result no child-running stage keyed). redone marks
// an item the folder's last run had finished that no key scheme found a receipt for: its inputs
// changed since, so it runs again (endStage's redidNote counts it once it is done).
type ItemResult struct {
	Key           string
	Label         string
	Phase         Phase
	Receipt       *Receipt
	Output        string
	Attempts      int
	Continuations int
	Resumed       bool
	Verdict       Verdict
	older         []schemeItem
	redone        bool
}

// schemeItem is an item as one key scheme saw it: its key, its output path and — on a source item a
// verify stage checked — the verdict that scheme's rule reads off the verify receipt.
type schemeItem struct {
	key     string
	output  string
	verdict Verdict
}

// underScheme is the item as keySchemes[scheme] sees it: the result's own key, output and verdict
// for the current scheme, its older entry for an older one (zero when it has none).
func (r ItemResult) underScheme(scheme int) schemeItem {
	if scheme == 0 {
		return schemeItem{key: r.Key, output: r.Output, verdict: r.Verdict}
	}
	if scheme > len(r.older) {
		return schemeItem{}
	}
	return r.older[scheme-1]
}

// setVerdicts records the verdict every key scheme reads off verify, the verify item that checked
// r: Verdict by the current rule, each older entry's by its scheme's own.
func (r *ItemResult) setVerdicts(verify ItemResult) {
	r.Verdict = keySchemes[0].verdict(verify)
	for index := range r.older {
		r.older[index].verdict = keySchemes[index+1].verdict(verify)
	}
}

// Tally counts a stage's items by how they ended: receipts by status, Unfinished for items a
// cancel left without one, Resumed for the finished items an earlier run supplied, and the verify
// verdicts the items carry.
type Tally struct {
	OK         int
	Partial    int
	Blocked    int
	Unfinished int
	Resumed    int
	Confirmed  int
	Refuted    int
	Unclear    int
}

// Run runs plan. It validates the plan, expands every fanout's items, and resumes the newest
// workflow folder with the same PlanHash — skipping each item whose stored receipt is ok or
// partial — or creates a new one. A folder that had not finished also replays its recorded script
// and ask stages (StageRecord): a resume never runs a script that already produced a result, nor
// asks a question already answered. A cancelled ctx stops the running children, keeps the finished
// items' receipts, and returns a Result whose Phase is PhaseStopped. Either way the run ends by
// writing items.md (Store.WriteItems) into the folder. The error is for a run that
// could not proceed: an invalid plan, an unreadable item source, a store that cannot be written, a
// folder Admit refused.
func (r *Runner) Run(ctx context.Context, plan Plan) (Result, error) {
	if r.Spawner == nil || r.Store == nil || r.Workspace == nil {
		return Result{}, errors.New("workflow: runner needs a Spawner, a Store and a Workspace")
	}
	if r.Scripts == nil && slices.ContainsFunc(plan.Stages, func(stage Stage) bool { return stage.Kind == StageScript }) {
		return Result{}, errors.New("workflow: the plan has a script stage and the runner has no ScriptRunner")
	}
	status, found, stageItems, err := r.openFolder(plan, "")
	if err != nil {
		return Result{}, err
	}
	if r.Admit != nil {
		if err := r.Admit(status.ID); err != nil {
			return Result{}, err
		}
	}
	status, prior, replay, err := r.resetStatus(plan, status, found)
	if err != nil {
		return Result{}, err
	}
	dir, err := r.Store.Dir(status.ID)
	if err != nil {
		return Result{}, err
	}

	state := &runState{runner: r, status: status, prior: prior, replay: replay, dir: dir}
	result := Result{ID: status.ID, Dir: dir, Phase: PhaseDone, Stages: make([]StageResult, 0, len(plan.Stages))}
	for index := range plan.Stages {
		if ctx.Err() != nil {
			result.Phase = PhaseStopped
			break
		}
		stageResult, err := state.runStage(ctx, plan, index, stageItems, &result)
		if err != nil {
			return result, err
		}
		result.Stages = append(result.Stages, stageResult)
		if stageResult.Phase == PhaseStopped {
			result.Phase = PhaseStopped
			break
		}
	}
	if err := state.setWorkflowPhase(result.Phase); err != nil {
		return result, err
	}
	listing, err := r.Store.WriteItems(result.ID, result)
	if err != nil {
		return result, err
	}
	result.Listing = listing
	return result, nil
}

// expandStages expands every fanout's items up front, so an unreadable source fails the run before
// a folder exists and the PlanHash covers the work. It returns them per stage index and flattened.
// Only a fanout with a source of its own is expanded here: a fanout over a pick stage's items gets
// them when the pick has run, and every other kind works over earlier stages' results.
func (r *Runner) expandStages(plan Plan) (map[int][]Item, []Item, error) {
	perStage := make(map[int][]Item, len(plan.Stages))
	var all []Item
	for index, stage := range plan.Stages {
		if stage.Kind != StageFanout || stage.Over.Stage != "" {
			continue
		}
		items, err := Expand(*stage.Over, r.Workspace, r.Split)
		if err != nil {
			return nil, nil, fmt.Errorf("workflow: stage %q: %w", stage.Name, err)
		}
		perStage[index] = items
		all = append(all, items...)
	}
	return perStage, all, nil
}

// Open finds or creates the workflow folder Run will run plan in, the way Run does: the plan
// validated, every fanout with a source of its own expanded, the PlanHash over them, the newest
// folder with that hash, else a new one stamped by Now. A found folder is returned as it stands —
// Open writes nothing to it — so the Run that follows resumes it. A launch opens its folder ahead of
// Run when it needs the id and status path before the run starts (a queued background workflow).
// folder "" finds or creates by hash; a named folder is the one the plan must lead to, and a plan
// whose hash leads anywhere else — no folder, or another one — is refused with ErrFolderMoved
// before anything is created.
func (r *Runner) Open(plan Plan, folder string) (RunStatus, error) {
	status, _, _, err := r.openFolder(plan, folder)
	return status, err
}

// openFolder is the folder open Run and Open share: it validates plan, expands its items
// (expandStages), and finds the newest folder with the plan's PlanHash, or — unless folder names
// one the hash must lead to — creates one stamped by Now. found reports a folder that already
// existed; stageItems are the expanded items per stage index.
func (r *Runner) openFolder(plan Plan, folder string) (status RunStatus, found bool, stageItems map[int][]Item, err error) {
	if r.Store == nil || r.Workspace == nil {
		return RunStatus{}, false, nil, errors.New("workflow: runner needs a Store and a Workspace")
	}
	if problems := Validate(plan); len(problems) > 0 {
		return RunStatus{}, false, nil, fmt.Errorf("workflow: invalid plan: %s", joinProblems(problems))
	}
	stageItems, allItems, err := r.expandStages(plan)
	if err != nil {
		return RunStatus{}, false, nil, err
	}
	planHash, err := PlanHash(plan, nil, allItems)
	if err != nil {
		return RunStatus{}, false, nil, err
	}
	status, found, err = r.Store.Find(planHash)
	if err != nil {
		return RunStatus{}, false, nil, err
	}
	if folder != "" && (!found || status.ID != folder) {
		return RunStatus{}, false, nil, ErrFolderMoved
	}
	if !found {
		status, err = r.Store.Create(plan, planHash, r.now())
		if err != nil {
			return RunStatus{}, false, nil, err
		}
	}
	return status, found, stageItems, nil
}

// resetStatus readies the folder openFolder opened for a run: its stages reset to pending, its
// phase running, its recipe and Updated stamped, written back. prior is a found folder's stages as
// its last run left them, before the reset (nil for a created folder): the stale guard on an older
// key scheme's receipt reads them (adoptable). replay reports that the found folder had not
// finished — stopped, or cut off while running — so its recorded script and ask stages are replayed
// rather than run again (StageRecord). A re-issue of a workflow that ran to its end still skips its
// finished items, but runs its scripts and asks its questions afresh: it is a new run of finished
// work, not the resume of unfinished work.
func (r *Runner) resetStatus(plan Plan, status RunStatus, found bool) (_ RunStatus, prior []StageStatus, replay bool, err error) {
	if found {
		prior, replay = status.Stages, status.Phase != PhaseDone
	}
	status.Stages = make([]StageStatus, 0, len(plan.Stages))
	for _, stage := range plan.Stages {
		status.Stages = append(status.Stages, StageStatus{Name: stage.Name, Kind: stage.Kind, Phase: PhasePending})
	}
	status.Phase = PhaseRunning
	status.Recipe = r.Recipe
	status.Updated = r.now()
	return status, prior, replay, r.Store.WriteStatus(status)
}

// promptSource is where a stage's prompt file is read from: Prompts, else the workspace.
func (r *Runner) promptSource() fs.FS {
	if r.Prompts != nil {
		return r.Prompts
	}
	return r.Workspace
}

// now reads the Runner's clock.
func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// runState is one Run's shared state: the status.json it keeps current, under mu, which also
// serialises the Observer's calls, the resumed folder's stages as its last run left them (prior,
// nil for a new folder), and whether the run resumes an unfinished folder whose recorded
// script and ask stages it replays (resetStatus), and the workflow's folder, which item short names
// are read relative to (ItemName). prior, replay and dir are fixed before the first stage runs.
type runState struct {
	runner *Runner
	mu     sync.Mutex
	status RunStatus
	prior  []StageStatus
	replay bool
	dir    string
}

// runFanout runs one fanout stage's items in the given repeat round and returns the stage's result.
func (s *runState) runFanout(ctx context.Context, stageIndex, round int, stage Stage, items []Item) (StageResult, error) {
	drafts := make([]itemDraft, len(items))
	for index, item := range items {
		drafts[index] = itemDraft{item: item, round: round}
	}
	results, err := s.runItems(ctx, stageIndex, stage, drafts)
	if err != nil {
		return StageResult{}, err
	}
	return s.endStage(ctx, stageIndex, stage, results, PhaseDone)
}

// runItems runs a stage's items as fresh children, at most Width at a time in item order, and
// returns their results in item order — the wave path every child-running stage shares. A cancel
// stops the stage from starting more items; the running ones end stopped. The stage's phase is left
// running for the caller's endStage.
func (s *runState) runItems(ctx context.Context, stageIndex int, stage Stage, drafts []itemDraft) ([]ItemResult, error) {
	jobs, err := s.prepareItems(stageIndex, stage, drafts)
	if err != nil {
		return nil, err
	}
	if err := s.setStagePhase(stageIndex, stage, PhaseRunning, len(jobs)); err != nil {
		return nil, err
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	results := make([]ItemResult, len(jobs))
	var (
		wait     sync.WaitGroup
		errMu    sync.Mutex
		firstErr error
	)
	slots := make(chan struct{}, max(s.runner.Width, 1))
	for index, job := range jobs {
		if job.result.Resumed {
			results[index] = job.result
			continue
		}
		select {
		case slots <- struct{}{}:
		case <-runCtx.Done():
		}
		if runCtx.Err() != nil {
			results[index] = job.result
			continue
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			defer func() { <-slots }()
			itemResult, err := s.runItem(runCtx, stageIndex, stage, index, job)
			results[index] = itemResult
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				cancelRun()
			}
		}()
	}
	wait.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

// endStage settles a stage whose items have run: stopped when a cancel left an item unfinished,
// else finished — the phase the caller judged the stage to end in (done, or failed for a merge
// that left no report). It records the phase and the stage's note — the finished items it redid
// (redidNote), "" when none — and returns the stage's result with its tally and that note.
func (s *runState) endStage(ctx context.Context, stageIndex int, stage Stage, results []ItemResult, finished Phase) (StageResult, error) {
	phase := finished
	if ctx.Err() != nil && hasUnfinished(results) {
		phase = PhaseStopped
	}
	note := redidNote(results)
	s.mu.Lock()
	s.status.Stages[stageIndex].Note = note
	s.mu.Unlock()
	if err := s.setStagePhase(stageIndex, stage, phase, 0); err != nil {
		return StageResult{}, err
	}
	return StageResult{Name: stage.Name, Kind: stage.Kind, Phase: phase, Items: results, Tally: tallyOf(results), Note: note}, nil
}

// redidNote says how many finished items a stage redid — the redone results that ran to done — or
// "" when it redid none.
func redidNote(results []ItemResult) string {
	redid := 0
	for _, result := range results {
		if result.redone && result.Phase == PhaseDone {
			redid++
		}
	}
	if redid == 0 {
		return ""
	}
	noun := "items"
	if redid == 1 {
		noun = "item"
	}
	return fmt.Sprintf(redidFormat, redid, noun)
}

// itemDraft is an item before it is keyed: the item, the stage's repeat round, the text its key
// covers beside the stage's brief under a given key scheme (a verify's source key and claim, a
// merge's manifest, each as that scheme renders them; nil for a fanout), the engine's lead that
// comes before the stage's own rendered brief (a verify or merge; rendered once the output path is
// known, nil for a fanout), and a fixed output path (a merge's report; empty lets outputPath
// choose). prepareItems keys it through every key scheme.
type itemDraft struct {
	item      Item
	round     int
	keySuffix func(scheme int) string
	lead      func(output string) string
	output    string
}

// itemJob is one item made ready to run: its key, its short name, the brief its children get, and
// the result it starts from — pending, or done and Resumed when the store already holds its ok or partial
// receipt.
type itemJob struct {
	item   Item
	key    string
	name   string
	brief  string
	result ItemResult
}

// prepareItems keys every item through every key scheme — the current one (keySchemes[0]) is its
// key, the older ones are kept on its result for the stages downstream — looks for a receipt an
// earlier run of the workflow stored (resumeReceipt), resolves its output path — from the current
// key, after any adoption — and brief, and writes the stage's items into status.json. An item no
// scheme found a receipt for, which the folder's last run had finished (wasFinished), is marked
// redone.
func (s *runState) prepareItems(stageIndex int, stage Stage, drafts []itemDraft) ([]itemJob, error) {
	store, id := s.runner.Store, s.status.ID
	jobs := make([]itemJob, len(drafts))
	statuses := make([]ItemStatus, len(drafts))
	for index, draft := range drafts {
		item := draft.item
		keys, err := s.schemeKeys(stage, draft)
		if err != nil {
			return nil, err
		}
		key := keys[0]
		result := ItemResult{Key: key, Label: item.Label, Phase: PhasePending, older: s.olderItems(stage, draft, keys[1:])}
		receipt, found, err := s.resumeReceipt(stage, draft, keys)
		if err != nil {
			return nil, err
		}
		if found {
			result.Phase, result.Receipt, result.Resumed = PhaseDone, &receipt, true
		} else {
			result.redone = s.wasFinished(stage.Name, draft, keys)
		}
		output := draft.output
		if output == "" {
			if output, err = outputPath(store, id, stage, item, key); err != nil {
				return nil, err
			}
		}
		result.Output = output
		brief := renderBrief(stage.Task, item, output)
		if draft.lead != nil {
			brief = joinBrief(draft.lead(output), brief)
		}
		name := ItemName(item, s.dir, stage.Name)
		jobs[index] = itemJob{item: item, key: key, name: name, brief: brief, result: result}
		statuses[index] = ItemStatus{Key: key, Label: item.Label, Name: name, Phase: result.Phase, Receipt: result.Receipt}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Stages[stageIndex].Items = statuses
	for index, job := range jobs {
		s.notifyItem(stageIndex, stage, index, job, job.result.Phase, 0, 0, job.result.Receipt)
	}
	return jobs, s.writeStatus()
}

// schemeKeys keys draft's item in stage under every key scheme, keySchemes index for index.
func (s *runState) schemeKeys(stage Stage, draft itemDraft) ([]string, error) {
	keys := make([]string, len(keySchemes))
	for index, scheme := range keySchemes {
		key, err := scheme.key(s.keyInput(stage, draft, index))
		if err != nil {
			return nil, err
		}
		keys[index] = key
	}
	return keys, nil
}

// keyInput is what keySchemes[scheme] keys draft's item over in stage: the draft's suffix as that
// scheme renders it.
func (s *runState) keyInput(stage Stage, draft itemDraft, scheme int) keyInput {
	suffix := ""
	if draft.keySuffix != nil {
		suffix = draft.keySuffix(scheme)
	}
	return keyInput{
		stage: stage, round: draft.round, suffix: suffix, item: draft.item,
		prompts: s.runner.promptSource(), workspace: s.runner.Workspace,
	}
}

// olderItems is draft's item in stage under each older key scheme, given its olderKeys
// (keySchemes[1:] index for index). Each output path is where that scheme's build pointed the
// child: the draft's fixed output, the stage's `out:`, or output.md in the folder the older key
// names — a pure path, never created, so an adoption can still rename that folder.
func (s *runState) olderItems(stage Stage, draft itemDraft, olderKeys []string) []schemeItem {
	items := make([]schemeItem, len(olderKeys))
	for index, key := range olderKeys {
		output := draft.output
		switch {
		case output != "":
		case stage.Out != "":
			output = strings.ReplaceAll(stage.Out, placeholderItem, draft.item.Label)
		default:
			output = filepath.Join(s.dir, itemsDirName, key, outputName)
		}
		items[index] = schemeItem{key: key, output: output}
	}
	return items
}

// resumeReceipt looks for the ok or partial receipt an earlier run stored for draft's item, keyed
// keys by every scheme (keySchemes index for index): under the current key, else under the key
// each older scheme gives it, newest first — a verify's or merge's older key rendered from its
// source items' keys, outputs and verdicts under that same scheme. The first such receipt the
// stale guard admits (adoptable) is adopted: its folder is renamed to the current key
// (Store.AdoptItem) and the item resumes from it. found is false when the item must run.
func (s *runState) resumeReceipt(stage Stage, draft itemDraft, keys []string) (Receipt, bool, error) {
	store, id := s.runner.Store, s.status.ID
	key := keys[0]
	receipt, found, err := store.finishedReceipt(id, key)
	if err != nil || found {
		return receipt, found, err
	}
	tried := map[string]bool{key: true}
	for _, olderKey := range keys[1:] {
		if tried[olderKey] {
			continue
		}
		tried[olderKey] = true
		receipt, found, err := store.finishedReceipt(id, olderKey)
		if err != nil {
			return Receipt{}, false, err
		}
		if !found {
			continue
		}
		admitted, err := s.adoptable(stage.Name, draft, olderKey)
		if err != nil {
			return Receipt{}, false, err
		}
		if !admitted {
			continue
		}
		if err := store.AdoptItem(id, olderKey, key); err != nil {
			return Receipt{}, false, err
		}
		return receipt, true, nil
	}
	return Receipt{}, false, nil
}

// adoptable is the stale guard on the receipt an older key scheme stored for draft's item under
// olderKey. It admits the receipt when the folder's last run recorded no line for the item (same
// stage name, repeat round and label), or a line naming olderKey, or one naming a key whose
// folder holds no ok or partial receipt — the item never finished since. Any other line means the
// item last finished under that other key, after an input olderKey's receipt predates changed:
// the older receipt is stale and the item is redone.
func (s *runState) adoptable(stageName string, draft itemDraft, olderKey string) (bool, error) {
	priorLine, found := s.priorItem(stageName, draft.round, draft.item.Label)
	priorKey := priorLine.Key
	if !found || priorKey == olderKey {
		return true, nil
	}
	if !isValidKey(priorKey) {
		return false, nil
	}
	_, finished, err := s.runner.Store.finishedReceipt(s.status.ID, priorKey)
	return !finished, err
}

// wasFinished reports whether the folder's last run recorded draft's item (same stage name, repeat
// round and label) as finished — an ok or partial receipt — under a key none of its scheme keys
// (keys) matches: the item's inputs changed since it ran. A line under one of keys whose receipt
// is gone is not such a change.
func (s *runState) wasFinished(stageName string, draft itemDraft, keys []string) bool {
	line, found := s.priorItem(stageName, draft.round, draft.item.Label)
	if !found || line.Receipt == nil || slices.Contains(keys, line.Key) {
		return false
	}
	return line.Receipt.Status == StatusOK || line.Receipt.Status == StatusPartial
}

// priorItem is the line the folder's last run recorded for the item labelled label of the stage
// named stageName in the given repeat round; found is false when prior has no such line.
func (s *runState) priorItem(stageName string, round int, label string) (line ItemStatus, found bool) {
	for _, stage := range s.prior {
		if stage.Name != stageName || stage.Round != round {
			continue
		}
		for _, item := range stage.Items {
			if item.Label == label {
				return item, true
			}
		}
	}
	return ItemStatus{}, false
}

// runItem runs one item to its end: fresh children and continuations under the Runner's bounds,
// each child's transcript saved, the final receipt stored. The error is a store failure only; a
// cancel returns the item stopped.
func (s *runState) runItem(ctx context.Context, stageIndex int, stage Stage, index int, job itemJob) (ItemResult, error) {
	runner := s.runner
	result := job.result
	repeatRound := s.repeatRound(stageIndex)
	var (
		prior []Round
		last  Outcome
	)
	for attempt := 1; ; {
		if ctx.Err() != nil {
			return s.stopItem(stageIndex, stage, index, job, result)
		}
		result.Attempts = attempt
		round := len(prior) + 1
		if err := s.updateItem(stageIndex, stage, index, job, PhaseRunning, attempt, round, nil); err != nil {
			return result, err
		}
		spec := ItemSpec{
			Workflow: s.status.ID, Stage: stage, Item: job.item, Key: job.key, Name: job.name,
			Index: index, RepeatRound: repeatRound,
			Brief: job.brief, Output: result.Output,
			Attempt: attempt, Prior: append([]Round(nil), prior...),
		}
		outcome, err := runner.Spawner.Spawn(ctx, spec)
		if len(outcome.Transcript) > 0 {
			if writeErr := runner.Store.WriteTranscript(s.status.ID, job.key, outcome.Transcript); writeErr != nil {
				return result, writeErr
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return s.stopItem(stageIndex, stage, index, job, result)
			}
			outcome = Outcome{Ending: EndFaulted, Report: err.Error()}
		}
		if outcome.Ending == EndStopped {
			return s.stopItem(stageIndex, stage, index, job, result)
		}
		last = outcome

		if isFinal(outcome) {
			return s.finishItem(stageIndex, stage, index, job, result, round, *outcome.Receipt)
		}
		if ctx.Err() != nil {
			return s.stopItem(stageIndex, stage, index, job, result)
		}
		if outcome.Ending == EndCapped && len(prior) < runner.Continuations {
			prior = append(prior, Round{Receipt: outcome.Receipt, Report: outcome.Report, Output: result.Output})
			result.Continuations++
			continue
		}
		if attempt <= runner.Retries {
			attempt++
			prior = nil
			continue
		}
		return s.finishItem(stageIndex, stage, index, job, result, round, exhaustedReceipt(last, attempt))
	}
}

// isFinal reports whether an outcome ends the item as it stands: the receipt of a child that
// completed, or a capped child's receipt that is not partial (it finished the work, or declared it
// blocked, in its closing Turn). A capped partial receipt, a missing receipt and a fault all ask
// for a second chance.
func isFinal(outcome Outcome) bool {
	if outcome.Receipt == nil {
		return false
	}
	switch outcome.Ending {
	case EndCompleted:
		return true
	case EndCapped:
		return outcome.Receipt.Status != StatusPartial
	default:
		return false
	}
}

// exhaustedReceipt is the receipt an item ends on when its second chances run out: the last capped
// round's partial receipt when there is one, else a blocked receipt saying why the item has none.
func exhaustedReceipt(last Outcome, attempts int) Receipt {
	if last.Ending == EndCapped && last.Receipt != nil {
		return *last.Receipt
	}
	var cause string
	switch {
	case last.Ending == EndFaulted:
		cause = "the child faulted: " + FirstLine(last.Report)
	case last.Ending == EndCapped:
		cause = "the child reached its step cap without a receipt"
	default:
		cause = "the child ended without calling finish"
	}
	return Receipt{
		Status:  StatusBlocked,
		Summary: clampWords(fmt.Sprintf("after %d attempts, %s", attempts, cause), SummaryMaxWords),
	}
}

// finishItem stores the item's receipt and marks it done; round is the round it ended in.
func (s *runState) finishItem(stageIndex int, stage Stage, index int, job itemJob, result ItemResult, round int, receipt Receipt) (ItemResult, error) {
	if err := s.runner.Store.WriteReceipt(s.status.ID, job.key, receipt); err != nil {
		return result, err
	}
	result.Phase, result.Receipt = PhaseDone, &receipt
	return result, s.updateItem(stageIndex, stage, index, job, PhaseDone, result.Attempts, round, &receipt)
}

// stopItem marks an item a cancel ended unfinished: stopped when a child had started, still
// pending when none had. Its folder keeps no receipt, so a resume restarts it fresh.
func (s *runState) stopItem(stageIndex int, stage Stage, index int, job itemJob, result ItemResult) (ItemResult, error) {
	result.Phase = PhaseStopped
	if result.Attempts == 0 {
		result.Phase = PhasePending
	}
	return result, s.updateItem(stageIndex, stage, index, job, result.Phase, result.Attempts, 0, nil)
}

// updateItem records one item's phase change in status.json and tells the Observer.
func (s *runState) updateItem(stageIndex int, stage Stage, index int, job itemJob, phase Phase, attempt, round int, receipt *Receipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	line := &s.status.Stages[stageIndex].Items[index]
	line.Phase, line.Receipt = phase, receipt
	s.notifyItem(stageIndex, stage, index, job, phase, attempt, round, receipt)
	return s.writeStatus()
}

// setStagePhase records a stage's phase change in status.json and tells the Observer; items is the
// stage's item count, reported by a stage that runs children as it starts them (0 otherwise).
func (s *runState) setStagePhase(stageIndex int, stage Stage, phase Phase, items int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Stages[stageIndex].Phase = phase
	s.notifyStage(stageIndex, stage, phase, items)
	return s.writeStatus()
}

// repeatRound is the repeat round the stage at stageIndex runs in (startRound).
func (s *runState) repeatRound(stageIndex int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status.Stages[stageIndex].Round
}

// notifyStage tells the Observer of a stage's phase, in the repeat round status.json records for
// it. The caller holds mu.
func (s *runState) notifyStage(stageIndex int, stage Stage, phase Phase, items int) {
	if observer := s.runner.Observer; observer != nil {
		observer.StagePhase(StageEvent{
			Workflow: s.status.ID, Stage: stage.Name, Kind: stage.Kind, Phase: phase,
			RepeatRound: s.status.Stages[stageIndex].Round, Items: items,
		})
	}
}

// setWorkflowPhase records the workflow's final phase in status.json.
func (s *runState) setWorkflowPhase(phase Phase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Phase = phase
	return s.writeStatus()
}

// notifyItem tells the Observer of an item's phase, in its stage's repeat round. The caller holds
// mu.
func (s *runState) notifyItem(stageIndex int, stage Stage, index int, job itemJob, phase Phase, attempt, round int, receipt *Receipt) {
	observer := s.runner.Observer
	if observer == nil {
		return
	}
	observer.ItemPhase(ItemEvent{
		Workflow: s.status.ID, Stage: stage.Name, Index: index, Key: job.key, Label: job.item.Label,
		Name: job.name, Phase: phase, Attempt: attempt, Round: round, RepeatRound: s.status.Stages[stageIndex].Round,
		Receipt: receipt,
	})
}

// writeStatus stamps and writes status.json. The caller holds mu.
func (s *runState) writeStatus() error {
	s.status.Updated = s.runner.now()
	return s.runner.Store.WriteStatus(s.status)
}

// stageKeyBrief is the current key scheme's (scheme 2's) stage brief: every field of the stage a child's work
// depends on, the task as a template, the contents of the stage's prompt file read from prompts,
// and the repeat round. The rendered brief cannot serve — its {out} is inside the item's folder,
// which the key names. The prompt file's contents, not only its path, go in, so an edit to the
// file between two runs gives the stage's items new keys and they are redone rather than resumed;
// a prompt file that cannot be read is an error. The round (0 for the stage's own run, left out of
// the encoding) gives a repeat's re-run of the stage keys of its own, so it spawns its items
// afresh instead of finding the earlier round's receipts, while a resume of the same round still
// finds them.
func stageKeyBrief(stage Stage, round int, prompts fs.FS) (string, error) {
	promptBody, err := readStagePrompt(stage, prompts)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(struct {
		Stage      string      `json:"stage"`
		Task       string      `json:"task,omitempty"`
		Prompt     string      `json:"prompt,omitempty"`
		PromptBody string      `json:"prompt_body,omitempty"`
		Out        string      `json:"out,omitempty"`
		Returns    ReceiptSpec `json:"returns,omitempty"`
		Tools      []string    `json:"tools,omitempty"`
		Round      int         `json:"round,omitempty"`
	}{stage.Name, stage.Task, stage.Prompt, promptBody, stage.Out, stage.Returns, stage.Tools, round})
	if err != nil {
		return "", fmt.Errorf("workflow: stage %q: encode the item key brief: %w", stage.Name, err)
	}
	return string(encoded), nil
}

// readStagePrompt reads the stage's prompt file from prompts, cleaned and checked the way the
// spawner opens it for the child: a path inside that source, never absolute and never climbing
// out. A stage without a prompt file reads nothing.
func readStagePrompt(stage Stage, prompts fs.FS) (string, error) {
	if stage.Prompt == "" {
		return "", nil
	}
	clean := path.Clean(filepath.ToSlash(stage.Prompt))
	if !fs.ValidPath(clean) {
		return "", fmt.Errorf("workflow: stage %q: prompt file %q is not a path inside its folder", stage.Name, stage.Prompt)
	}
	if prompts == nil {
		return "", fmt.Errorf("workflow: stage %q: prompt file %q: no folder to read it from", stage.Name, stage.Prompt)
	}
	body, err := fs.ReadFile(prompts, clean)
	if err != nil {
		return "", fmt.Errorf("workflow: stage %q: prompt file %q: %w", stage.Name, stage.Prompt, err)
	}
	return string(body), nil
}

// outputPath is where an item's child writes its detail output: the stage's `out:` with {item}
// rendered as the item's label, or output.md inside the item's own folder.
func outputPath(store *Store, id string, stage Stage, item Item, key string) (string, error) {
	if stage.Out != "" {
		return strings.ReplaceAll(stage.Out, placeholderItem, item.Label), nil
	}
	return store.Path(id, itemsDirName+"/"+key+"/"+outputName)
}

// renderBrief fills a task template's {item} with the item's entries and {out} with its output
// path. An empty task (the stage names a prompt file) renders empty.
func renderBrief(task string, item Item, output string) string {
	if task == "" {
		return ""
	}
	return strings.NewReplacer(
		placeholderItem, strings.Join(item.Units, ", "),
		placeholderOut, output,
	).Replace(task)
}

// joinBrief puts the engine's lead before the stage's rendered brief, a blank line between them;
// either may be empty.
func joinBrief(lead, brief string) string {
	if lead == "" || brief == "" {
		return lead + brief
	}
	return lead + "\n\n" + brief
}

// hasUnfinished reports whether any item ended without a receipt.
func hasUnfinished(results []ItemResult) bool {
	for _, result := range results {
		if result.Phase != PhaseDone {
			return true
		}
	}
	return false
}

// tallyOf counts a stage's items by how they ended.
func tallyOf(results []ItemResult) Tally {
	var tally Tally
	for _, result := range results {
		if result.Resumed {
			tally.Resumed++
		}
		switch result.Verdict {
		case VerdictConfirmed:
			tally.Confirmed++
		case VerdictRefuted:
			tally.Refuted++
		case VerdictUnclear:
			tally.Unclear++
		}
		tally.countOutcome(result.Phase, result.Receipt)
	}
	return tally
}

// joinProblems renders a plan's problems as one line.
func joinProblems(problems []Problem) string {
	lines := make([]string, len(problems))
	for i, problem := range problems {
		lines[i] = problem.String()
	}
	return strings.Join(lines, "; ")
}

// FirstLine is s up to its first line break, trimmed: the summary line every surface shows of a
// receipt, a report or an error, whatever whitespace leads or pads it.
func FirstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

// clampWords keeps at most limit words of s, joined by single spaces.
func clampWords(s string, limit int) string {
	words := strings.Fields(s)
	if len(words) > limit {
		words = words[:limit]
	}
	return strings.Join(words, " ")
}
