package tui

import (
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The workflow block (ADR 0087 — a Recipe run in the transcript)
// ----------------------------------------------------------------------------
//
// A Recipe the human launches with "/<recipe-skill> <text>" runs as a Workflow BEFORE the model's
// first request of the Exchange, so nothing the model does can show it: there is no tool call to
// hang its progress on. The engine reports its life as WorkflowPhaseEvents instead, and this file
// folds them into ONE block per Workflow (entryWorkflow) that grows in place as the run goes — one
// row per stage of its Plan, shown from the start (pending, running, waiting on the human, or how it
// ended), a line for each item whose receipt is not ok, the question an `ask` stage is waiting on,
// and the totals and the end it came to.
//
// A Workflow a fan_out call started is the call block's business instead: that block already stands
// in the transcript, its children nest under it, and its result IS the result lines. So only a
// Workflow no open fan_out call of the same run accounts for opens a block here.
//
// Either way the block heads the Workflow's item runs. Each item run gets a run head of its own
// (entryWorkflowItem) when its WorkflowItemStarted folds: it is seated at the block's own depth
// under the block's call (domain.WorkflowPhaseEvent.Call), inside the block's span
// (entry.seatsItemHead), and the item child's entries land behind it (entry.headsRunFor, by the
// run id the phase names). That head is a delegation's in every way the view asks — it paints as a
// delegation row eliding its run, opens as the run's view, and answers ^x, a message and the gauge —
// but it never groups into a "✦ Sub-Agent (N)" list. The item's receipt folds onto it as its report.
// A retried item keeps ONE row: each attempt has its own head, and the ones a later attempt
// superseded are stepped over by the paint (retiredAttempts). Under a live workflow block the item
// heads are not painted at all: the block's stage rows stand for them (each marked targetStage), so
// the walk steps over the block's span whole (transcript.resolveBlock). A fan_out card has one stage,
// and paints its item rows. The block itself never collapses, and a fan_out card's own fold hides
// its body alone.
//
// The block's text is its whole record and its view its live paint: every fold re-renders the text
// from the view (workflowView), the painter draws the view's stage rows while the view lives, and
// the record keeps the text alone. A block replayed from a record carries no view, so it paints its
// text, and the item rows its span holds stay painted as the way into its runs. Because the view
// and the text MOVE after the entry is committed, the kind is never memoised by the paint cache
// (entryKindRules).

// fanOutToolName is the fan_out tool's name: a Workflow its call started is that call's block's.
const fanOutToolName = "fan_out"

// The words the block is built from. The item line and the totals line mirror the result lines the
// engine hands the model (internal/workflow's Format), so the human reads what the model reads.
const (
	workflowTitle        = "Workflow "
	workflowLineSep      = " — "
	workflowTotalSep     = " · "
	workflowStagePrefix  = "stage: "
	workflowAskPrefix    = "waiting for your answer: "
	workflowFailedPrefix = "failed: "
	workflowNoSummary    = "(no summary)"
	workflowBodyIndent   = "  "
	workflowRunningWord  = "running"
	workflowWaitingWord  = "waiting for you"
	workflowListedItems  = 40 // past it only the items that did not end ok are listed, as Format lists them
	workflowStatusOK     = "ok"
	workflowStatusParts  = "partial"
	workflowStatusBlock  = "blocked"
	workflowPendingWord  = "pending"
	workflowDoneWord     = "done"
	workflowRoundWord    = "round "
	workflowRoundSep     = "/"
	workflowRowSep       = " · " // what a stage row's slot and a trouble line join their parts with
)

// workflowView is the live state of one Workflow's block, folded from its WorkflowPhaseEvents. It is
// view-only and never persisted: the entry's text, rendered from it on every fold, is what the
// record keeps.
type workflowView struct {
	id        string               // the Workflow's id — what tells two Workflows' events apart
	name      string               // the Workflow's name (a Recipe's name)
	stage     string               // the stage running now; "" before the first and once it ended
	stages    []workflowStage      // one row per stage of the Plan, in its order (workflowStagesOf)
	items     []workflowItem       // the items that finished, in the order they finished
	question  string               // an `ask` stage's question while it waits; "" otherwise
	waitingOn string               // the stage the question waits in
	end       domain.WorkflowPhase // finished, stopped or failed; "" while it runs
	cause     string               // a failed Workflow's cause
}

// live reports whether the view was folded in this session. A block replayed from a record carries
// the zero view, and paints its text.
func (v workflowView) live() bool { return v.id != "" }

// workflowItem is one finished item: the stage it belongs to, its label, its receipt's status and
// summary, and its result line.
type workflowItem struct {
	stage   string
	label   string
	status  string
	summary string
	line    string
}

// workflowStageState is where one stage row stands in the round it shows.
type workflowStageState int

const (
	stagePending workflowStageState = iota // nothing has started the stage yet
	stageRunning                           // started, and not yet ended
	stageDone                              // ended with every item it ran finished
	stageFailed                            // the Workflow failed while the stage ran
	stageStopped                           // a cancel ended it with an item unfinished
)

// workflowStage is one stage row: the stage's name and the round it shows — a stage a repeat
// re-runs keeps ONE row, which moves on to each new round as its first phase arrives — with that
// round's counts and state. A stage is keyed by (name, round), so a phase of an earlier round than
// the row shows folds nothing.
type workflowStage struct {
	name     string
	round    int  // the 1-based round the row shows; 0 before any phase named the stage
	rounds   int  // the most rounds a repeat can run the stage (the phase's Rounds), 0 when none re-runs it
	items    int  // the round's item count (the phase's Items), 0 for a stage that runs no child
	finished int  // the round's items that ended on a receipt
	troubled bool // one of those receipts is not ok
	entered  bool // one of the round's item runs has started, so the row has a run to open
	state    workflowStageState
}

// workflowStagesOf is the pending row of every stage the Plan names, in its order.
func workflowStagesOf(names []string) []workflowStage {
	stages := make([]workflowStage, 0, len(names))
	for _, name := range names {
		stages = append(stages, workflowStage{name: stripEscapes(name)})
	}
	return stages
}

// addWorkflowPhase folds one WorkflowPhaseEvent. A started phase opens the block — unless an open
// fan_out call of the emitting run accounts for the Workflow — and every later phase finds the
// block by the Workflow's id and re-renders it. A phase for a Workflow with no block (a fan_out's,
// or one whose start this view never saw) folds nothing.
func (t *transcript) addWorkflowPhase(e domain.WorkflowPhaseEvent) {
	switch e.Phase {
	case domain.WorkflowItemStarted:
		t.addWorkflowItem(e)
	case domain.WorkflowItemFinished:
		t.finishWorkflowItem(e)
	}
	run := runOf(e.EventBase)
	if e.Phase == domain.WorkflowStarted {
		if t.fanOutOpen(run) || t.workflowAt(e.Workflow) >= 0 {
			return
		}
		view := workflowView{id: e.Workflow, name: stripEscapes(e.Name), stages: workflowStagesOf(e.Stages)}
		// The call its item children are bracketed under is the block's own: it is what those
		// children's entries find it by as their head (entry.headsWorkflowRuns), and it is kept
		// in the record, so the nesting holds after a save and reopen.
		t.place(inRun(entry{kind: entryWorkflow, callID: e.Call, text: view.text(), workflow: view}, run))
		return
	}
	i := t.workflowAt(e.Workflow)
	if i < 0 {
		return
	}
	en := &t.entries[i]
	en.workflow = en.workflow.fold(e)
	en.text = en.workflow.text()
	en.done = en.workflow.end != ""
	t.touch()
}

// fanOutOpen reports whether run holds a fan_out call still waiting for its result — the call a
// Workflow starting now in that run belongs to.
func (t *transcript) fanOutOpen(run runRef) bool {
	for i := len(t.entries) - 1; i >= 0; i-- {
		e := t.entries[i]
		if e.kind == entryToolCall && !e.done && e.tool.name == fanOutToolName && e.run() == run {
			return true
		}
	}
	return false
}

// workflowAt is the index of the live block of the Workflow id names, or −1. A block replayed from
// a record carries no view, so it is never found: its Workflow is not running in this session.
func (t *transcript) workflowAt(id string) int {
	if id == "" {
		return -1
	}
	for i := len(t.entries) - 1; i >= 0; i-- {
		if e := t.entries[i]; e.kind == entryWorkflow && e.workflow.id == id {
			return i
		}
	}
	return -1
}

// fold returns the view with one phase applied. The view's slices are copied before they change, so
// an earlier Model copy holding the same entry never sees a phase it did not fold (ADR 0011).
func (v workflowView) fold(e domain.WorkflowPhaseEvent) workflowView {
	switch e.Phase {
	case domain.WorkflowStageStarted:
		v.stage, v.question = stripEscapes(e.Stage), ""
		v = v.withStage(e, func(s *workflowStage) {
			s.state, s.items, s.rounds = stageRunning, e.Items, e.Rounds
		})
	case domain.WorkflowItemStarted:
		// The engine starts a stage before its items; a started item still says the stage runs.
		v = v.withStage(e, func(s *workflowStage) {
			s.entered = true
			if s.state == stagePending {
				s.state = stageRunning
			}
		})
	case domain.WorkflowItemFinished:
		item := workflowItem{
			stage:   stripEscapes(e.Stage),
			label:   stripEscapes(e.Item),
			status:  stripEscapes(e.Receipt.Status),
			summary: stripEscapes(firstLine(e.Receipt.Summary)),
			line:    workflowItemLine(e),
		}
		v.items = append(slices.Clip(v.items), item)
		v = v.withStage(e, func(s *workflowStage) {
			s.finished++
			s.troubled = s.troubled || item.status != workflowStatusOK
		})
	case domain.WorkflowStageFinished:
		if v.waitingOn == stripEscapes(e.Stage) {
			v.question = ""
		}
		v = v.withStage(e, func(s *workflowStage) { s.state = s.endState() })
	case domain.WorkflowWaiting:
		v.question, v.waitingOn = stripEscapes(firstLine(e.Detail)), stripEscapes(e.Stage)
	case domain.WorkflowFinished, domain.WorkflowStopped, domain.WorkflowFailed:
		v.end, v.stage, v.question = e.Phase, "", ""
		switch e.Phase {
		case domain.WorkflowFailed:
			v.cause = stripEscapes(firstLine(e.Detail))
			v.stages = endRunningStages(v.stages, stageFailed)
		case domain.WorkflowStopped:
			v.stages = endRunningStages(v.stages, stageStopped)
		}
	}
	return v
}

// withStage returns the view with apply run on the row of the stage e names, in e's round: a row
// showing an earlier round moves on to e's, starting it afresh, and a phase of an earlier round than
// the row shows applies nothing. A stage the Plan did not name (a Workflow whose start carried no
// stage list) gets a row of its own at the end. The row list is copied before it changes.
func (v workflowView) withStage(e domain.WorkflowPhaseEvent, apply func(*workflowStage)) workflowView {
	name := stripEscapes(e.Stage)
	if name == "" {
		return v
	}
	stages := slices.Clone(v.stages)
	i := slices.IndexFunc(stages, func(s workflowStage) bool { return s.name == name })
	if i < 0 {
		stages = append(stages, workflowStage{name: name})
		i = len(stages) - 1
	}
	row := &stages[i]
	switch {
	case e.Round < row.round:
		return v
	case e.Round > row.round:
		*row = workflowStage{name: name, round: e.Round, rounds: row.rounds}
	}
	apply(row)
	v.stages = stages
	return v
}

// endState is how a stage that ended stands: stopped when an item it counted never finished — the
// engine ends a stage stopped only on a cancel that left one unfinished — and done otherwise.
func (s workflowStage) endState() workflowStageState {
	if s.finished < s.items {
		return stageStopped
	}
	return stageDone
}

// endRunningStages returns stages with every row still running set to state: the stage a Workflow
// that failed or stopped was in the middle of. The list is copied before it changes.
func endRunningStages(stages []workflowStage, state workflowStageState) []workflowStage {
	out := slices.Clone(stages)
	for i := range out {
		if out[i].state == stageRunning {
			out[i].state = state
		}
	}
	return out
}

// workflowItemLine renders one finished item the way the result lines do:
// `#<n> <item> — <status> — <summary>[ k=v…]`, fields in key order.
func workflowItemLine(e domain.WorkflowPhaseEvent) string {
	summary := firstLine(e.Receipt.Summary)
	if summary == "" {
		summary = workflowNoSummary
	}
	line := "#" + strconv.Itoa(e.Index+1) + " " + e.Item + workflowLineSep + e.Receipt.Status + workflowLineSep + summary
	keys := make([]string, 0, len(e.Receipt.Fields))
	for key := range e.Receipt.Fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		line += " " + key + "=" + workflowFieldValue(e.Receipt.Fields[key])
	}
	return stripEscapes(line)
}

// workflowFieldValue quotes a field value that would blur its pair — empty, or holding a space or
// an `=` — as the result lines do.
func workflowFieldValue(value string) string {
	if value == "" || strings.ContainsAny(value, " \t=") {
		return strconv.Quote(value)
	}
	return value
}

// text renders the block: its header line, then its body — the stage running, the item lines (past
// workflowListedItems only those that did not end ok), an `ask` stage's question, the totals, and a
// failure's cause.
func (v workflowView) text() string {
	lines := []string{v.header()}
	if v.stage != "" {
		lines = append(lines, workflowStagePrefix+v.stage)
	}
	capped := len(v.items) > workflowListedItems
	headed := v.spansStages()
	stage := ""
	for i, item := range v.items {
		if headed && (i == 0 || item.stage != stage) {
			lines = append(lines, item.stage+":")
		}
		stage = item.stage
		if capped && item.status == workflowStatusOK {
			continue
		}
		lines = append(lines, item.line)
	}
	if v.question != "" {
		lines = append(lines, workflowAskPrefix+v.question)
	}
	if len(v.items) > 0 {
		lines = append(lines, v.totals())
	}
	if v.cause != "" {
		lines = append(lines, workflowFailedPrefix+v.cause)
	}
	return strings.Join(lines, "\n")
}

// header names the Workflow and where it stands: running, waiting for the human, or how it ended.
func (v workflowView) header() string {
	state := workflowRunningWord
	switch {
	case v.end != "":
		state = string(v.end)
	case v.question != "":
		state = workflowWaitingWord
	}
	return workflowTitle + v.name + workflowLineSep + state
}

// spansStages reports whether the finished items belong to more than one stage, which is when each
// stage's items are headed by its name.
func (v workflowView) spansStages() bool {
	for _, item := range v.items {
		if item.stage != v.items[0].stage {
			return true
		}
	}
	return false
}

// totals is the totals line: `items N · ok a · partial b · blocked c`.
func (v workflowView) totals() string {
	counts := map[string]int{}
	for _, item := range v.items {
		counts[item.status]++
	}
	return strings.Join([]string{
		"items " + strconv.Itoa(len(v.items)),
		workflowStatusOK + " " + strconv.Itoa(counts[workflowStatusOK]),
		workflowStatusParts + " " + strconv.Itoa(counts[workflowStatusParts]),
		workflowStatusBlock + " " + strconv.Itoa(counts[workflowStatusBlock]),
	}, workflowTotalSep)
}

// renderWorkflowBlock paints a workflow block: from its view while the view lives
// (renderWorkflowStages), and from its text for a block replayed from a record, whose view was never
// persisted (renderWorkflowText).
func renderWorkflowBlock(th theme, view workflowView, text string, width int) blockPaint {
	if view.live() {
		return renderWorkflowStages(th, view, width)
	}
	return plainPaint(renderWorkflowText(th, text, width))
}

// renderWorkflowStages paints a live workflow block: the header under the star in the tool label's
// tone, one row per stage (workflowView.stageRow) marked as that stage's surface, then the body lines
// (workflowView.stageBody) hung beneath them in the detail tone.
func renderWorkflowStages(th theme, v workflowView, width int) blockPaint {
	var out blockPaint
	out.add(hangingWrap(th, th.toolLabel, glyphAssistant+" ", v.header(), width), targetNone)
	room := toolRowCells(th, width)
	for i := range v.stages {
		out.addStage(i, []string{v.stageRow(th, i, width, room)})
	}
	for _, line := range v.stageBody() {
		out.add(hangingWrap(th, th.toolDetail, workflowBodyIndent, line, width), targetNone)
	}
	return out
}

// stageRow paints the i'th stage row in the leader row a delegation's wears: the branch marker, the
// stage's name — with ✓ once it is done with every item ok — the dotted leader, and its state in the
// outcome slot (workflowStage.slot), failed in red. A pending row is painted dim whole and opens
// nothing; a row one of whose item runs has started wears ▶ at the block's edge.
func (v workflowView) stageRow(th theme, i, width, room int) string {
	stage := v.stages[i]
	waiting := v.question != "" && v.waitingOn == stage.name
	succeeded := stage.succeeded()
	summary := branchSummary{
		detailLine: detailLine{Text: stage.slot(waiting)},
		failed:     stage.state == stageFailed,
		succeeded:  succeeded,
	}
	view := toolView{Target: stage.name, finished: succeeded, Summary: summary}
	row := leaderRow(th, view, branchMarker(i == len(v.stages)-1), room, false, noRemainder)
	switch {
	case stage.state == stagePending && !waiting:
		return th.toolLeader.Render(ansi.Strip(row))
	case stage.entered:
		return indicatorRow(th, row, width, glyphCollapsed)
	}
	return row
}

// succeeded reports whether the stage earned the ✓: it is done, and every item it ran ended ok.
func (s workflowStage) succeeded() bool {
	return s.state == stageDone && !s.troubled
}

// slot is the stage row's outcome slot: `pending`, `running` (`n/m · running` for a stage of more
// than one item), `waiting for you`, `done`, `failed` or `stopped` — led, on a round a repeat re-ran,
// by `round n/m` (`round n` when the most rounds are not known).
func (s workflowStage) slot(waiting bool) string {
	word := workflowPendingWord
	switch {
	case waiting:
		word = workflowWaitingWord
	case s.state == stageRunning && s.items > 1:
		word = strconv.Itoa(s.finished) + workflowRoundSep + strconv.Itoa(s.items) + workflowRowSep + workflowRunningWord
	case s.state == stageRunning:
		word = workflowRunningWord
	case s.state == stageDone:
		word = workflowDoneWord
	case s.state == stageFailed:
		word = string(domain.WorkflowFailed)
	case s.state == stageStopped:
		word = string(domain.WorkflowStopped)
	}
	if s.round <= 1 {
		return word
	}
	round := workflowRoundWord + strconv.Itoa(s.round)
	if s.rounds > 0 {
		round += workflowRoundSep + strconv.Itoa(s.rounds)
	}
	return round + workflowRowSep + word
}

// stageBody is what a live block says beneath its stage rows: one line per item whose receipt is not
// ok (`stage · item — status — summary`), an `ask` stage's question, and — once it has ended — the
// totals and a failure's cause.
func (v workflowView) stageBody() []string {
	var lines []string
	for _, item := range v.items {
		if item.status == workflowStatusOK {
			continue
		}
		summary := item.summary
		if summary == "" {
			summary = workflowNoSummary
		}
		lines = append(lines, item.stage+workflowRowSep+item.label+workflowLineSep+item.status+workflowLineSep+summary)
	}
	if v.question != "" {
		lines = append(lines, workflowAskPrefix+v.question)
	}
	if v.end != "" && len(v.items) > 0 {
		lines = append(lines, v.totals())
	}
	if v.cause != "" {
		lines = append(lines, workflowFailedPrefix+v.cause)
	}
	return lines
}

// renderWorkflowText paints a workflow block from its text: the header under the star in the tool
// label's tone, each body line hung beneath it in the detail tone. It reads the text alone, which is
// what lets a replayed block — whose view was never persisted — paint as the live one's record did.
func renderWorkflowText(th theme, text string, width int) []string {
	header, body, _ := strings.Cut(text, "\n")
	lines := hangingWrap(th, th.toolLabel, glyphAssistant+" ", header, width)
	if body == "" {
		return lines
	}
	for _, line := range strings.Split(body, "\n") {
		lines = append(lines, hangingWrap(th, th.toolDetail, workflowBodyIndent, line, width)...)
	}
	return lines
}

// workflowItemPlace is where one item run's head stands in its Workflow: the stage it belongs to, the
// 1-based round of that stage, the item's 0-based place in it, and the 1-based attempt the run is
// (domain.WorkflowPhaseEvent). It is what tells one item's attempts apart from another item's
// (retiredAttempts), and the record keeps it (session.WorkflowItem).
type workflowItemPlace struct {
	stage   string
	round   int
	index   int
	attempt int
}

// addWorkflowItem seats the run head of the item run a WorkflowItemStarted names: an
// entryWorkflowItem in the run that started the Workflow, under the Workflow's call, heading the
// item child's run by its run id. It is placed at the end of that item run ([transcript.placeBehind])
// — which, with no head of its own standing yet, is the end of the Workflow's block span
// ([spanHeadAt]) — so a later stage's item head lands inside the span even after a host note has
// landed below it.
//
// It seats nothing for a phase naming no run or no call, for a background workflow's (whose runs
// stay out of the conversation, ADR 0089), and for a Workflow no block in this view heads — an item
// head outside any block would stand for nothing the reader launched.
func (t *transcript) addWorkflowItem(e domain.WorkflowPhaseEvent) {
	if e.Run == "" || e.Call == "" || e.Background {
		return
	}
	item := runRef{depth: e.Depth + 1, spawn: e.Call, id: e.Run}
	if _, ok := spanHeadAt(t.entries, item); !ok {
		return
	}
	label := stripEscapes(e.Item)
	head := inRun(entry{
		kind:       entryWorkflowItem,
		callID:     e.Call,
		spawnRunID: e.Run,
		tool:       workflowItemView(label),
		item: workflowItemPlace{
			stage:   stripEscapes(e.Stage),
			round:   e.Round,
			index:   e.Index,
			attempt: e.Attempt,
		},
	}, runOf(e.EventBase))
	t.placeBehind(head, item)
}

// workflowItemView is the card an item run's head wears: a delegation's label under the item's
// label, which is also the run's name — what the breadcrumb, the status line and the view's legend
// call it (usageAgentName). It carries no task row: the phase names the item, not the instructions
// its child was handed.
func workflowItemView(label string) toolView {
	delegation := toolRegistry[subAgentToolName]
	return toolView{Label: delegation.label, Verb: delegation.verb, Target: label, agentName: label}
}

// finishWorkflowItem folds an item's receipt onto the head of the run the WorkflowItemFinished
// names — the item's latest attempt — as that run's report: the summary as the row's gist, the
// status as its verdict. It closes the head, which pairs no ToolResultEvent. A phase naming no run
// (a resumed item, one whose child was never built) or a run with no head folds nothing here.
func (t *transcript) finishWorkflowItem(e domain.WorkflowPhaseEvent) {
	i := workflowItemHeadAt(t.entries, e.Run)
	if i < 0 {
		return
	}
	en := &t.entries[i]
	status := stripEscapes(e.Receipt.Status)
	en.tool.stat = plainStat(status)
	en.tool.Summary = workflowItemSummary(status, stripEscapes(firstLine(e.Receipt.Summary)))
	en.done = true
	t.touch()
}

// workflowItemHeadAt is the index of the item head of the run id names, or −1.
func workflowItemHeadAt(entries []entry, id string) int {
	if id == "" {
		return -1
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if e := entries[i]; e.kind == entryWorkflowItem && e.spawnRunID == id {
			return i
		}
	}
	return -1
}

// workflowItemSummary is the outcome slot a receipt words: its summary quoted, since the words are
// the child's, and the verdict its status carries — `ok` the finished green and done ✓
// (subAgentFinished), `blocked` the failure red. It is re-derived on decode from the status the
// record keeps as the stat, so a replayed row reads as the live one did.
func workflowItemSummary(status, summary string) branchSummary {
	s := quotedSummary(detailLine{Text: summary})
	s.succeeded = status == workflowStatusOK
	s.failed = status == workflowStatusBlock
	return s
}

// workflowItemKey names ONE item of a Workflow across its attempts: the run its head stands in, the
// Workflow's call, and the item's stage, round and place.
type workflowItemKey struct {
	run  runRef
	call string
	workflowItemPlace
}

// retiredAttempts marks, by index, every item head a later attempt of the same item superseded —
// the heads the paint steps over, so an item keeps the one row its latest attempt heads. It answers
// nil when no item was ever retried, which is every transcript but one holding a retry.
func retiredAttempts(entries []entry) map[int]bool {
	var latest map[workflowItemKey]int
	var retired map[int]bool
	for i := range entries {
		e := &entries[i]
		if e.kind != entryWorkflowItem {
			continue
		}
		key := workflowItemKey{run: e.run(), call: e.callID, workflowItemPlace: e.item}
		key.attempt = 0
		if latest == nil {
			latest = make(map[workflowItemKey]int)
		}
		prev, seen := latest[key]
		switch {
		case !seen:
			latest[key] = i
			continue
		case entries[prev].item.attempt > e.item.attempt:
			prev = i
		default:
			latest[key] = i
		}
		if retired == nil {
			retired = make(map[int]bool)
		}
		retired[prev] = true
	}
	return retired
}
