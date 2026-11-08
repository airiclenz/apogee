// Package sidepanel is the Driver-neutral fold behind the side panel (ADR 0110): it folds engine
// Events into per-run panel state and answers, for a run scope, each section's rows, its summary
// line and its live predicate. It renders nothing — the TUI paints what it answers, and any other
// Driver can fold the same stream (decision 4: every section renders from engine Events, so watch
// mode folds the journal and a resumed session folds its stored record through the same entry
// points the Event fold uses).
//
// It imports only internal/domain, internal/sanitize and the standard library, so a Driver reaches
// it without pulling in the TUI or the engine.
package sidepanel

import (
	"encoding/json"
	"slices"
	"strconv"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/sanitize"
)

const (
	// subAgentLinger is how long the sub-agents section stays live after the last run ends
	// (docs/layout/side-panel-layout.md, "Sections").
	subAgentLinger = 30 * time.Second
	// usageLivePercent is the context fill, in percent of the window, at which the usage section
	// turns live.
	usageLivePercent = 50
	// percentScale turns a fill ratio into a percentage.
	percentScale = 100
	// toolMenuRow and toolInstructionsRow are the two names a ContextCostRow carries for the tool
	// surface (domain.ContextCostRow: the native tools array, or the text menu a non-native
	// profile folds into the system channel); every other row is standing system content.
	toolMenuRow         = "tool menu"
	toolInstructionsRow = "tool instructions"
)

// RunKey names the run an Event came from, by the same identity the TUI's runRef uses: the
// engine-minted run id, falling back to the (depth, spawning call id) pair only for an event
// recorded before run ids existed. The zero RunKey is the top-level run — the human's own
// conversation. It is comparable, so "the same run" is one `==`.
type RunKey struct {
	depth int
	spawn string
	id    string
}

// RunOf reads the run an Event was emitted by off its base. A base carrying a run id is keyed by
// that id alone; one without falls back to its depth and spawning call id.
func RunOf(base domain.EventBase) RunKey {
	if base.RunID != "" {
		return RunKey{id: base.RunID}
	}
	return RunKey{depth: base.Depth, spawn: base.CallID}
}

// Phase is where a delegation stands in its life.
type Phase int

const (
	// PhaseQueued is a delegation whose call has arrived but whose child has not started.
	PhaseQueued Phase = iota
	// PhaseRunning is a delegation whose child has started and not finished.
	PhaseRunning
	// PhaseDone is a delegation that finished with a result that is not an error.
	PhaseDone
	// PhaseFailed is a delegation that finished with an error result.
	PhaseFailed
)

// isActive reports whether a delegation in phase p still holds the sub-agents section live.
func (p Phase) isActive() bool {
	return p == PhaseQueued || p == PhaseRunning
}

// Fill is a context fill: Used tokens of a Window, and whether Used is the engine's estimate
// rather than a server reading. A zero Window means no window is known.
type Fill struct {
	Used       int
	Window     int
	IsEstimate bool
}

// Percent is the fill as a whole percentage of the window, 0 when either side is unknown.
func (f Fill) Percent() int {
	if f.Used <= 0 || f.Window <= 0 {
		return 0
	}
	return f.Used * percentScale / f.Window
}

// View is one section's answer: its rows in display order, the summary its header carries
// (`2/5`, `62%`, `2 running`; "" when it has none) and whether its live rule holds.
type View[R any] struct {
	Rows    []R
	Summary string
	IsLive  bool
}

// SubAgentRow is one delegation. Name is its label, already escape-stripped and reduced to one
// line; Elapsed is measured from its start to its end, or to now while it runs, and is 0 while it
// is queued; Fill is its own latest reading against its own window.
type SubAgentRow struct {
	Run     RunKey
	Depth   int
	Name    string
	Phase   Phase
	Elapsed time.Duration
	Fill    Fill
}

// TaskRow is one task of the scoped run's list. IsCurrent marks the first unfinished task — the
// list stores only done or not done. Text is model text, stripped by the seam that paints it.
type TaskRow struct {
	Text      string
	IsDone    bool
	IsCurrent bool
}

// FileRow is one file the scope wrote, merged across its writes: Stat sums their Edit-region
// diffstats, HasStat says whether any write carried regions, and IsDelegated whether a sub-agent
// wrote it. Path is the write target as the Driver folded it — the TUI keys one inside the workspace
// relative to it, so a live write and a resumed record's agree; trimming it to the column is the
// painter's. Run and CallID name the latest of those writes — the run that landed it and its tool
// call — which is the tool block a click on the row opens (call ids are only unique within a run).
type FileRow struct {
	Path        string
	Stat        domain.DiffStat
	HasStat     bool
	IsDelegated bool
	Run         RunKey
	CallID      string
}

// UsageView is the usage section's answer. Fill is the scoped run's context fill (the status-line
// gauge's figure); System, Tools and History split it — the standing rows of the latest
// ContextCostEvent and the estimate minus them — and HasSplit says whether a cost report arrived.
// Totals is the scope's token and spend accounting: the whole session's at the top level, the
// run's own inside a run.
type UsageView struct {
	Fill     Fill
	System   int
	Tools    int
	History  int
	HasSplit bool
	Totals   domain.Usage
	Summary  string
	IsLive   bool

	standing int // the cost report's own estimate of its standing rows, history's subtrahend
}

// State is the folded panel state of one session. It is not safe for concurrent use. Its queries
// are nil-safe: a nil *State answers every section empty and not live.
type State struct {
	now            func() time.Time
	fallbackWindow int
	runs           map[RunKey]*runState
	delegations    []RunKey // delegated runs in the order they first appeared
	writes         []write  // landed writes in arrival order, every depth
}

// runState is everything folded for one run.
type runState struct {
	isDelegation bool
	depth        int
	name         string
	phase        Phase
	startedAt    time.Time
	endedAt      time.Time

	tasks []domain.TaskItem

	readUsed        int
	readWindow      int
	estimate        int
	isEstimateNewer bool
	cost            domain.ContextCost
	usageBase       domain.Usage // what a resumed record carried in (SetUsage)
	usageLatest     domain.Usage // the latest live Cumulative reading
}

// write is one landed file write.
type write struct {
	run    RunKey
	depth  int
	callID string
	path   string
	stat   *domain.DiffStat
}

// New returns an empty State reading the time from now — injected so elapsed time and the
// linger are testable without the wall clock.
func New(now func() time.Time) *State {
	return &State{now: now, runs: map[RunKey]*runState{}}
}

// Clone returns a copy of s that shares nothing a mutator writes: a holder that must never write a
// State another holder still reads (the TUI's value-copied Model, ADR 0011) clones it and writes
// the clone. A nil State clones to nil.
func (s *State) Clone() *State {
	if s == nil {
		return nil
	}
	runs := make(map[RunKey]*runState, len(s.runs))
	for key, state := range s.runs {
		// A runState's slices (tasks, cost.Rows) are replaced whole by every writer, never
		// written in place, so the shallow copy owns everything a later write touches.
		copied := *state
		runs[key] = &copied
	}
	return &State{
		now:            s.now,
		fallbackWindow: s.fallbackWindow,
		runs:           runs,
		delegations:    slices.Clone(s.delegations),
		writes:         slices.Clone(s.writes),
	}
}

// Reads reports whether Fold would change anything for event: false for every variant Fold
// ignores, and for a tool call that spawns no delegation. A copy-on-write holder asks it first, so
// the Events the panel never reads — every streamed text delta — cost it no Clone.
func Reads(event domain.Event) bool {
	switch e := event.(type) {
	case domain.ToolCallEvent:
		return e.SpawnRunID != ""
	case domain.SubAgentPhaseEvent, domain.SubAgentNamedEvent, domain.ToolResultEvent,
		domain.TaskListEvent, domain.UsageEvent, domain.ContextEstimateEvent, domain.ContextCostEvent:
		return true
	default:
		return false
	}
}

// SetFallbackWindow sets the window a fill is measured against when its reading names none
// (UsageEvent.ContextWindow 0) — the session's own bound window.
func (s *State) SetFallbackWindow(window int) {
	s.fallbackWindow = window
}

// Fold applies one engine Event. Variants the panel does not read are ignored.
func (s *State) Fold(event domain.Event) {
	switch e := event.(type) {
	case domain.ToolCallEvent:
		s.foldDelegationCall(e)
	case domain.SubAgentPhaseEvent:
		s.foldPhase(e)
	case domain.SubAgentNamedEvent:
		if name := firstStrippedLine(e.Name); name != "" {
			s.delegation(RunOf(e.EventBase), e.Depth).name = name
		}
	case domain.ToolResultEvent:
		s.foldToolResult(e)
	case domain.TaskListEvent:
		s.SetTasks(RunOf(e.EventBase), e.Items)
	case domain.UsageEvent:
		s.foldUsage(e)
	case domain.ContextEstimateEvent:
		if e.Tokens > 0 {
			run := s.run(RunOf(e.EventBase))
			run.estimate, run.isEstimateNewer = e.Tokens, true
		}
	case domain.ContextCostEvent:
		s.run(RunOf(e.EventBase)).cost = e.Cost
	}
}

// SetTasks replaces run's task list with items, in order.
func (s *State) SetTasks(run RunKey, items []domain.TaskItem) {
	s.run(run).tasks = append([]domain.TaskItem(nil), items...)
}

// AddWrite records a landed write to path by run at depth, made by the tool call callID. stat is
// the write's Edit-region diffstat, nil when the write carried no regions.
func (s *State) AddWrite(run RunKey, depth int, callID, path string, stat *domain.DiffStat) {
	s.writes = append(s.writes, write{run: run, depth: depth, callID: callID, path: path, stat: stat})
}

// SetUsage seeds run's accounting from a stored record: totals is what the record had spent, kept
// under every later live reading (the engine restarts its count at zero on resume), and used is
// the context fill it last observed.
func (s *State) SetUsage(run RunKey, totals domain.Usage, used int) {
	state := s.run(run)
	state.usageBase = totals
	if used > 0 {
		state.readUsed, state.isEstimateNewer = used, false
	}
}

// foldDelegationCall opens a queued row for a sub_agent call, named as title.DelegateLabel names
// it: the call's `name`, falling back to the task's first line, decided on the escape-stripped
// name so a name of nothing but control characters falls back to the task.
func (s *State) foldDelegationCall(e domain.ToolCallEvent) {
	if e.SpawnRunID == "" {
		return
	}
	child := domain.EventBase{Depth: e.Depth + 1, CallID: e.Call.ID, RunID: e.SpawnRunID}
	row := s.delegation(RunOf(child), child.Depth)
	if row.name == "" {
		row.name = delegationLabel(e.Call.Arguments)
	}
}

// foldPhase moves a delegation to running or finished. A zero At — a line written before the
// field existed — falls back to the injected clock.
func (s *State) foldPhase(e domain.SubAgentPhaseEvent) {
	at := e.At
	if at.IsZero() {
		at = s.now()
	}
	row := s.delegation(RunOf(e.EventBase), e.Depth)
	switch e.Phase {
	case domain.SubAgentStarted:
		row.phase, row.startedAt = PhaseRunning, at
	case domain.SubAgentFinished:
		row.finish(at, e.Result.IsError)
	}
}

// foldToolResult closes a delegation no phase ever reached and records a landed write. A refused
// or failed write still names its target (domain.ToolResultEvent), so only !IsError counts.
func (s *State) foldToolResult(e domain.ToolResultEvent) {
	if e.SpawnRunID != "" {
		child := domain.EventBase{Depth: e.Depth + 1, CallID: e.Result.CallID, RunID: e.SpawnRunID}
		row := s.delegation(RunOf(child), child.Depth)
		if row.phase == PhaseQueued {
			row.finish(s.now(), e.Result.IsError)
		}
	}
	if e.WriteTarget == "" || e.Result.IsError {
		return
	}
	var stat *domain.DiffStat
	if regions, ok := e.Result.Summary.(domain.EditRegions); ok {
		diff := regions.Stat()
		stat = &diff
	}
	s.AddWrite(RunOf(e.EventBase), e.Depth, e.Result.CallID, e.WriteTarget, stat)
}

// foldUsage folds one reading: the cumulative totals take every counted reading, latest-wins; the
// fill skips a Maintenance one, whose prompt is the summarizer's own.
func (s *State) foldUsage(e domain.UsageEvent) {
	run := s.run(RunOf(e.EventBase))
	run.usageLatest.Adopt(e.Cumulative)
	if e.Maintenance {
		return
	}
	total := e.TotalTokens
	if total == 0 {
		total = e.PromptTokens + e.CompletionTokens
	}
	if total > 0 {
		run.readUsed, run.readWindow, run.isEstimateNewer = total, e.ContextWindow, false
	}
}

// run returns key's state, creating it on first sight.
func (s *State) run(key RunKey) *runState {
	state, ok := s.runs[key]
	if !ok {
		state = &runState{}
		s.runs[key] = state
	}
	return state
}

// delegation returns key's state marked as a delegation at depth, listing it on first sight.
func (s *State) delegation(key RunKey, depth int) *runState {
	state := s.run(key)
	if !state.isDelegation {
		state.isDelegation, state.depth = true, depth
		s.delegations = append(s.delegations, key)
	}
	return state
}

// finish closes the delegation at the given moment.
func (r *runState) finish(at time.Time, isError bool) {
	r.endedAt, r.phase = at, PhaseDone
	if isError {
		r.phase = PhaseFailed
	}
}

// SubAgents answers the sub-agents section. It lists every delegation whatever the scope (ADR
// 0110 decision 12); a row's Run lets the painter mark the viewed one. It is live while any
// delegation is queued or running and for the linger after the last one ends.
func (s *State) SubAgents() View[SubAgentRow] {
	if s == nil {
		return View[SubAgentRow]{}
	}
	now := s.now()
	view := View[SubAgentRow]{Rows: make([]SubAgentRow, 0, len(s.delegations))}
	var running, queued int
	var lastEnd time.Time
	for _, key := range s.delegations {
		state := s.runs[key]
		view.Rows = append(view.Rows, SubAgentRow{
			Run: key, Depth: state.depth, Name: state.name, Phase: state.phase,
			Elapsed: state.elapsed(now), Fill: s.readingFill(state),
		})
		switch state.phase {
		case PhaseRunning:
			running++
		case PhaseQueued:
			queued++
		default:
			if state.endedAt.After(lastEnd) {
				lastEnd = state.endedAt
			}
		}
	}
	view.Summary = subAgentSummary(running, queued, len(view.Rows))
	view.IsLive = running+queued > 0 || lingerAfter(lastEnd, now).Left > 0
	return view
}

// Linger is when the sub-agents section's live rule next turns false on the clock alone: Until is
// the instant the linger after the last delegation's end runs out, and Left is the time from now
// to it on the State's own clock. Both are zero while no linger is running — while a delegation is
// queued or running (an Event, not the clock, ends that), before any delegation has ended, and once
// the linger has passed.
type Linger struct {
	Until time.Time
	Left  time.Duration
}

// SubAgentLinger answers when the sub-agents section stops being live with no further Event, so a
// Driver can repaint at that instant rather than on its next unrelated frame. It applies the same
// rule [State.SubAgents] judges IsLive by.
func (s *State) SubAgentLinger() Linger {
	if s == nil {
		return Linger{}
	}
	var lastEnd time.Time
	for _, key := range s.delegations {
		state := s.runs[key]
		if state.phase.isActive() {
			return Linger{}
		}
		if state.endedAt.After(lastEnd) {
			lastEnd = state.endedAt
		}
	}
	return lingerAfter(lastEnd, s.now())
}

// lingerAfter is the linger that follows the last delegation ending at lastEnd, read at now: zero
// when nothing has ended or the linger has already run out.
func lingerAfter(lastEnd, now time.Time) Linger {
	if lastEnd.IsZero() {
		return Linger{}
	}
	until := lastEnd.Add(subAgentLinger)
	if !now.Before(until) {
		return Linger{}
	}
	return Linger{Until: until, Left: until.Sub(now)}
}

// elapsed is the run's time from start to end, or to now while it runs; 0 while it is queued or
// when it ended without starting.
func (r *runState) elapsed(now time.Time) time.Duration {
	if r.startedAt.IsZero() {
		return 0
	}
	end := now
	if !r.phase.isActive() {
		end = r.endedAt
	}
	return max(end.Sub(r.startedAt), 0)
}

// subAgentSummary is the sub-agents header's summary: what runs, else how many ran.
func subAgentSummary(running, queued, total int) string {
	switch {
	case running > 0 && queued > 0:
		return strconv.Itoa(running) + " running · " + strconv.Itoa(queued) + " queued"
	case running > 0:
		return strconv.Itoa(running) + " running"
	case queued > 0:
		return strconv.Itoa(queued) + " queued"
	case total > 0:
		return strconv.Itoa(total) + " done"
	default:
		return ""
	}
}

// Tasks answers the tasks section for scope's list: the first unfinished task is current, the
// summary is done/total, and it is live while a task is unfinished.
func (s *State) Tasks(scope RunKey) View[TaskRow] {
	if s == nil || s.runs[scope] == nil {
		return View[TaskRow]{}
	}
	tasks := s.runs[scope].tasks
	view := View[TaskRow]{Rows: make([]TaskRow, 0, len(tasks))}
	done := 0
	for _, task := range tasks {
		row := TaskRow{Text: task.Text, IsDone: task.Done}
		if !task.Done && !view.IsLive {
			row.IsCurrent, view.IsLive = true, true
		}
		if task.Done {
			done++
		}
		view.Rows = append(view.Rows, row)
	}
	if len(tasks) > 0 {
		view.Summary = strconv.Itoa(done) + "/" + strconv.Itoa(len(tasks))
	}
	return view
}

// ChangedFiles answers the changed-files section: at the top level every write the session
// landed at any depth, inside a run that run's own writes — one row per path in first-write
// order. It is live once a write landed.
func (s *State) ChangedFiles(scope RunKey) View[FileRow] {
	if s == nil {
		return View[FileRow]{}
	}
	view := View[FileRow]{}
	index := map[string]int{}
	for _, w := range s.writes {
		if scope != (RunKey{}) && w.run != scope {
			continue
		}
		at, ok := index[w.path]
		if !ok {
			at = len(view.Rows)
			index[w.path] = at
			view.Rows = append(view.Rows, FileRow{Path: w.path})
		}
		view.Rows[at].add(w)
	}
	if len(view.Rows) > 0 {
		view.Summary, view.IsLive = strconv.Itoa(len(view.Rows)), true
	}
	return view
}

// add merges one write into the row.
func (f *FileRow) add(w write) {
	if w.stat != nil {
		f.Stat.Added += w.stat.Added
		f.Stat.Removed += w.stat.Removed
		f.HasStat = true
	}
	if w.depth > 0 {
		f.IsDelegated = true
	}
	f.Run, f.CallID = w.run, w.callID
}

// Usage answers the usage section for scope: its fill as the status-line gauge states it, the
// system / tools / history split of that fill, and its totals. It is live at a fill of
// usageLivePercent or more.
func (s *State) Usage(scope RunKey) UsageView {
	if s == nil {
		return UsageView{}
	}
	view := UsageView{Totals: s.totals(scope)}
	state := s.runs[scope]
	if state == nil {
		return view
	}
	if len(state.cost.Rows) > 0 {
		view.HasSplit = true
		view.System, view.Tools = splitStanding(state.cost.Rows)
		view.standing = state.cost.Tokens
	}
	return view.WithFill(s.scopeFill(scope, state))
}

// WithFill is v measured against fill instead: the history share, the summary and the live rule all
// follow it. A Driver that states a fill of its own — the TUI's status-line gauge — hands it in
// here, so its panel and its gauge never disagree.
func (v UsageView) WithFill(fill Fill) UsageView {
	v.Fill, v.Summary, v.IsLive = fill, "", false
	if v.HasSplit {
		v.History = max(fill.Used-v.standing, 0)
	}
	if fill.Window > 0 && fill.Used > 0 {
		v.Summary = strconv.Itoa(fill.Percent()) + "%"
		v.IsLive = fill.Used*percentScale >= usageLivePercent*fill.Window
	}
	return v
}

// scopeFill is the fill the status-line gauge states for scope: at the top level the engine's
// estimate wherever it is newer than the last reading (or no reading stands), inside a run that
// run's reading alone — exactly as the TUI's contextGauge reads it.
func (s *State) scopeFill(scope RunKey, state *runState) Fill {
	fill := s.readingFill(state)
	if scope == (RunKey{}) && (state.readUsed == 0 || state.isEstimateNewer) && state.estimate > 0 {
		fill.Used, fill.IsEstimate = state.estimate, true
	}
	return fill
}

// readingFill is the run's latest reading against the window it named, or the fallback window.
func (s *State) readingFill(state *runState) Fill {
	window := state.readWindow
	if window == 0 {
		window = s.fallbackWindow
	}
	return Fill{Used: state.readUsed, Window: window}
}

// totals is scope's accounting: the sum over every run at the top level, the run's own inside one.
func (s *State) totals(scope RunKey) domain.Usage {
	if scope != (RunKey{}) {
		if state := s.runs[scope]; state != nil {
			return domain.Sum(state.usageBase, state.usageLatest)
		}
		return domain.Usage{}
	}
	readings := make([]domain.Usage, 0, 2*len(s.runs))
	for _, state := range s.runs {
		readings = append(readings, state.usageBase, state.usageLatest)
	}
	return domain.Sum(readings...)
}

// splitStanding sums a cost report's rows into standing system content and the tool surface.
func splitStanding(rows []domain.ContextCostRow) (system, tools int) {
	for _, row := range rows {
		if row.Name == toolMenuRow || row.Name == toolInstructionsRow {
			tools += row.Tokens
			continue
		}
		system += row.Tokens
	}
	return system, tools
}

// delegationLabel names a delegation from its sub_agent call's arguments (see foldDelegationCall).
func delegationLabel(arguments json.RawMessage) string {
	var args map[string]any
	if err := json.Unmarshal(arguments, &args); err != nil {
		return "" // a malformed argument object names nothing; a later SubAgentNamedEvent still can
	}
	name, _ := args["name"].(string)
	if label := firstStrippedLine(name); label != "" {
		return label
	}
	task, _ := args["task"].(string)
	return firstStrippedLine(task)
}

// firstStrippedLine is model text reduced to one paintable line: escape-stripped first, so the
// emptiness a caller decides on is the form that will be painted (sanitize.FirstLine).
func firstStrippedLine(text string) string {
	return sanitize.FirstLine(sanitize.StripEscapes(text))
}
