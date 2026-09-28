package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// outputName is the file an item's detail output lands in when its stage sets no `out:` — inside
// the item's own folder, next to its receipt.
const outputName = "output.md"

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
	// Item is the child's share of the stage, and Key the item's folder name.
	Item Item
	Key  string
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

// StageEvent is one stage's phase change.
type StageEvent struct {
	Workflow string
	Stage    string
	Kind     StageKind
	Phase    Phase
}

// ItemEvent is one item's phase change. Attempt and Round are 1-based and zero for an item that
// never ran (pending, or skipped on resume); Receipt is set once the item is done.
type ItemEvent struct {
	Workflow string
	Stage    string
	Index    int
	Key      string
	Label    string
	Phase    Phase
	Attempt  int
	Round    int
	Receipt  *Receipt
}

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
	// Now is the clock status.json is stamped with; nil means time.Now.
	Now func() time.Time
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
// skipped, what a pick picked, that an ask took its default. Round is the repeat round the result
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
// could not proceed: an invalid plan, an unreadable item source, a store that cannot be written.
func (r *Runner) Run(ctx context.Context, plan Plan) (Result, error) {
	if r.Spawner == nil || r.Store == nil || r.Workspace == nil {
		return Result{}, errors.New("workflow: runner needs a Spawner, a Store and a Workspace")
	}
	if problems := Validate(plan); len(problems) > 0 {
		return Result{}, fmt.Errorf("workflow: invalid plan: %s", joinProblems(problems))
	}
	if r.Scripts == nil && slices.ContainsFunc(plan.Stages, func(stage Stage) bool { return stage.Kind == StageScript }) {
		return Result{}, errors.New("workflow: the plan has a script stage and the runner has no ScriptRunner")
	}
	stageItems, allItems, err := r.expandStages(plan)
	if err != nil {
		return Result{}, err
	}
	planHash, err := PlanHash(plan, nil, allItems)
	if err != nil {
		return Result{}, err
	}
	status, replay, err := r.openStatus(plan, planHash)
	if err != nil {
		return Result{}, err
	}
	dir, err := r.Store.Dir(status.ID)
	if err != nil {
		return Result{}, err
	}

	state := &runState{runner: r, status: status, replay: replay}
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

// openStatus resumes the newest workflow carrying planHash, its stages reset to pending, or creates
// a new one. replay reports that the folder it resumes had not finished — stopped, or cut off while
// running — so its recorded script and ask stages are replayed rather than run again (StageRecord).
// A re-issue of a workflow that ran to its end still skips its finished items, but runs its scripts
// and asks its questions afresh: it is a new run of finished work, not the resume of unfinished work.
func (r *Runner) openStatus(plan Plan, planHash string) (status RunStatus, replay bool, err error) {
	now := r.now()
	status, found, err := r.Store.Find(planHash)
	if err != nil {
		return RunStatus{}, false, err
	}
	if found {
		replay = status.Phase != PhaseDone
	} else {
		status, err = r.Store.Create(plan, planHash, now)
		if err != nil {
			return RunStatus{}, false, err
		}
	}
	status.Stages = make([]StageStatus, 0, len(plan.Stages))
	for _, stage := range plan.Stages {
		status.Stages = append(status.Stages, StageStatus{Name: stage.Name, Kind: stage.Kind, Phase: PhasePending})
	}
	status.Phase = PhaseRunning
	status.Updated = now
	return status, replay, r.Store.WriteStatus(status)
}

// now reads the Runner's clock.
func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// runState is one Run's shared state: the status.json it keeps current, under mu, which also
// serialises the Observer's calls, and whether the run resumes an unfinished folder whose recorded
// script and ask stages it replays (openStatus). replay is fixed before the first stage runs.
type runState struct {
	runner *Runner
	mu     sync.Mutex
	status RunStatus
	replay bool
}

// runFanout runs one fanout stage's items in the given repeat round and returns the stage's result.
func (s *runState) runFanout(ctx context.Context, stageIndex, round int, stage Stage, items []Item) (StageResult, error) {
	keyBrief, err := stageKeyBrief(stage, round)
	if err != nil {
		return StageResult{}, err
	}
	drafts := make([]itemDraft, len(items))
	for index, item := range items {
		drafts[index] = itemDraft{item: item, keyBrief: keyBrief}
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
	if err := s.setStagePhase(stageIndex, stage, PhaseRunning); err != nil {
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
// that left no report). It records the phase and returns the stage's result with its tally.
func (s *runState) endStage(ctx context.Context, stageIndex int, stage Stage, results []ItemResult, finished Phase) (StageResult, error) {
	phase := finished
	if ctx.Err() != nil && hasUnfinished(results) {
		phase = PhaseStopped
	}
	if err := s.setStagePhase(stageIndex, stage, phase); err != nil {
		return StageResult{}, err
	}
	return StageResult{Name: stage.Name, Kind: stage.Kind, Phase: phase, Items: results, Tally: tallyOf(results)}, nil
}

// itemDraft is an item before it is keyed: the item, the text its key covers beside the item and
// the stage's context files, the engine's lead that comes before the stage's own rendered brief
// (a verify or merge; rendered once the output path is known, nil for a fanout), and a fixed output
// path (a merge's report; empty lets outputPath choose).
type itemDraft struct {
	item     Item
	keyBrief string
	lead     func(output string) string
	output   string
}

// itemJob is one item made ready to run: its key, the brief its children get, and the result it
// starts from — pending, or done and Resumed when the store already holds its ok or partial
// receipt.
type itemJob struct {
	item   Item
	key    string
	brief  string
	result ItemResult
}

// prepareItems keys every item, resolves its output path and brief, looks for a receipt an earlier
// run of the workflow stored, and writes the stage's items into status.json.
func (s *runState) prepareItems(stageIndex int, stage Stage, drafts []itemDraft) ([]itemJob, error) {
	store, id := s.runner.Store, s.status.ID
	jobs := make([]itemJob, len(drafts))
	statuses := make([]ItemStatus, len(drafts))
	for index, draft := range drafts {
		item := draft.item
		key, err := ItemKey(draft.keyBrief, item, stage.Context, s.runner.Workspace)
		if err != nil {
			return nil, fmt.Errorf("workflow: stage %q, item %q: %w", stage.Name, item.Label, err)
		}
		output := draft.output
		if output == "" {
			if output, err = outputPath(store, id, stage, item, key); err != nil {
				return nil, err
			}
		}
		result := ItemResult{Key: key, Label: item.Label, Phase: PhasePending, Output: output}
		receipt, found, err := store.ReadReceipt(id, key)
		if err != nil {
			return nil, err
		}
		if found && (receipt.Status == StatusOK || receipt.Status == StatusPartial) {
			result.Phase, result.Receipt, result.Resumed = PhaseDone, &receipt, true
		}
		brief := renderBrief(stage.Task, item, output)
		if draft.lead != nil {
			brief = joinBrief(draft.lead(output), brief)
		}
		jobs[index] = itemJob{item: item, key: key, brief: brief, result: result}
		statuses[index] = ItemStatus{Key: key, Label: item.Label, Phase: result.Phase, Receipt: result.Receipt}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Stages[stageIndex].Items = statuses
	for index, job := range jobs {
		s.notifyItem(stage, index, job, job.result.Phase, 0, 0, job.result.Receipt)
	}
	return jobs, s.writeStatus()
}

// runItem runs one item to its end: fresh children and continuations under the Runner's bounds,
// each child's transcript saved, the final receipt stored. The error is a store failure only; a
// cancel returns the item stopped.
func (s *runState) runItem(ctx context.Context, stageIndex int, stage Stage, index int, job itemJob) (ItemResult, error) {
	runner := s.runner
	result := job.result
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
			Workflow: s.status.ID, Stage: stage, Item: job.item, Key: job.key,
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
		cause = "the child faulted: " + firstLine(last.Report)
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
	s.notifyItem(stage, index, job, phase, attempt, round, receipt)
	return s.writeStatus()
}

// setStagePhase records a stage's phase change in status.json and tells the Observer.
func (s *runState) setStagePhase(stageIndex int, stage Stage, phase Phase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Stages[stageIndex].Phase = phase
	s.notifyStage(stage, phase)
	return s.writeStatus()
}

// notifyStage tells the Observer of a stage's phase. The caller holds mu.
func (s *runState) notifyStage(stage Stage, phase Phase) {
	if observer := s.runner.Observer; observer != nil {
		observer.StagePhase(StageEvent{Workflow: s.status.ID, Stage: stage.Name, Kind: stage.Kind, Phase: phase})
	}
}

// setWorkflowPhase records the workflow's final phase in status.json.
func (s *runState) setWorkflowPhase(phase Phase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Phase = phase
	return s.writeStatus()
}

// notifyItem tells the Observer of an item's phase. The caller holds mu.
func (s *runState) notifyItem(stage Stage, index int, job itemJob, phase Phase, attempt, round int, receipt *Receipt) {
	observer := s.runner.Observer
	if observer == nil {
		return
	}
	observer.ItemPhase(ItemEvent{
		Workflow: s.status.ID, Stage: stage.Name, Index: index, Key: job.key, Label: job.item.Label,
		Phase: phase, Attempt: attempt, Round: round, Receipt: receipt,
	})
}

// writeStatus stamps and writes status.json. The caller holds mu.
func (s *runState) writeStatus() error {
	s.status.Updated = s.runner.now()
	return s.runner.Store.WriteStatus(s.status)
}

// stageKeyBrief is the brief an item key is taken over: every field of the stage a child's work
// depends on, the task as a template, and the repeat round. The rendered brief cannot serve — its
// {out} is inside the item's folder, which the key names. The round (0 for the stage's own run,
// left out of the encoding) gives a repeat's re-run of the stage keys of its own, so it spawns its
// items afresh instead of finding the earlier round's receipts, while a resume of the same round
// still finds them.
func stageKeyBrief(stage Stage, round int) (string, error) {
	encoded, err := json.Marshal(struct {
		Stage   string      `json:"stage"`
		Task    string      `json:"task,omitempty"`
		Prompt  string      `json:"prompt,omitempty"`
		Out     string      `json:"out,omitempty"`
		Returns ReceiptSpec `json:"returns,omitempty"`
		Tools   []string    `json:"tools,omitempty"`
		Round   int         `json:"round,omitempty"`
	}{stage.Name, stage.Task, stage.Prompt, stage.Out, stage.Returns, stage.Tools, round})
	if err != nil {
		return "", fmt.Errorf("workflow: stage %q: encode the item key brief: %w", stage.Name, err)
	}
	return string(encoded), nil
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
		if result.Receipt == nil || result.Phase != PhaseDone {
			tally.Unfinished++
			continue
		}
		switch result.Receipt.Status {
		case StatusOK:
			tally.OK++
		case StatusPartial:
			tally.Partial++
		default:
			tally.Blocked++
		}
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

// firstLine is s up to its first line break, trimmed.
func firstLine(s string) string {
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
