package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/sanitize"
	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/workflow"
)

// ----------------------------------------------------------------------------
// The /workflows view: the session's workflows, one workflow, one item
// ----------------------------------------------------------------------------
//
// A modal list pane the human opens with /workflows, in BOTH live states (its verb is whileRunning):
// the session's Workflows — every folder of its `<scratch>/workflows/` store, as Engine.Workflows
// reads their status.json (ADR 0087 D5) — one row each, with its state and item counts. ⏎ opens one
// to its stages and their items; ⏎ on an item opens that item's detail: its receipt, its detail
// output and its child's conversation, read-only. esc goes one level up, and closes the pane from the
// list. In a workflow's detail two chords act on it (workflowsVerb): ^x stops it, keeping its
// finished items, and ^r re-runs its blocked and faulted items as a new run of the same workflow.
// They are chords by the rule the /sessions browser ratified (sessionBrowserKey): no letter of a
// list pane is a verb. ^a answers the question or approval it waits on: it closes the pane and opens
// the workflow's oldest waiting prompt, one esc sent back included, at idle only (answerShownWorkflow).
// Another, ^s, saves a fan_out's workflow as a recipe skill: it asks for a name
// on the pane's own name row (workflowsNameKey) and writes <ConfigHome>/skills/<name>/SKILL.md off
// the Update loop (saveWorkflowRecipe), refusing a name a skill or a command already answers to and
// never overwriting a folder that is there. Every read runs off the Update loop through a tea.Cmd and folds into plain values on the Model
// (ADR 0011) — the listing on open and again on every WorkflowPhaseEvent while the pane is up, the
// item's detail on open and again with each re-list — so render and height read Model state only.

// maxWorkflowRows is the list and detail levels' taste in rows; a longer list scrolls a window
// around the highlight, and [Model.popupBudget] cuts the taste down to what the frame can seat.
const maxWorkflowRows = 10

// maxWorkflowItemRows is the item level's taste in rows: it is a reading, so it asks for more.
const maxWorkflowItemRows = 16

// The legends at the foot of each level.
const (
	workflowsListHint   = "↑/↓ select · ⏎ open · esc close"
	workflowsDetailHint = "↑/↓ select · ⏎ open item · ^x stop · ^r re-run failed · ^s save as recipe · esc back"
	workflowsAnswerHint = "↑/↓ select · ⏎ open item · ^a answer · ^x stop · ^r re-run failed · ^s save as recipe · esc back"
	workflowsNamingHint = "type a skill name · ⏎ save · esc cancel"
	workflowsItemHint   = "↑/↓ scroll · esc back"
)

// The notes and prose rows the view words itself with.
const (
	workflowsNone         = "no workflows in this session"
	workflowsListFailed   = "could not list workflows: "
	workflowsGone         = "this workflow is no longer listed"
	workflowsNoStages     = "no stages yet"
	workflowItemLoading   = "loading…"
	workflowItemGone      = "this item is no longer listed"
	workflowItemNoChild   = "this stage runs no child: no output and no conversation"
	workflowOutputMissing = " — not written"
	workflowOutputFailed  = " — could not read: "
	workflowNoTranscript  = "conversation: none saved yet"
	workflowTranscriptBad = "conversation: could not read: "
	workflowRerunNotIdle  = "a re-run starts only while the agent is idle — press ^r again once it is"
	workflowAnswerNotIdle = "a question opens only while the agent is idle — press ^a again once it is"
	workflowSavePrompt    = "save as recipe — skill name: "
	workflowSaveRecipe    = "only a fan_out workflow saves as a recipe — this one already runs the recipe /%s"
	workflowSaveTaken     = "a %s is already named %q — pick another name"
	workflowSavedNote     = "recipe %q written to %s — /%s runs it"
)

// workflowSaveCaret is the glyph the save row's name field draws where the next keystroke lands —
// the narrow bar the /sessions rename row draws, the other name typed inside a list pane.
const workflowSaveCaret = "▏"

// The chords a workflow's detail answers (workflowsVerb).
const (
	workflowStopKey   = "ctrl+x"
	workflowRerunKey  = "ctrl+r"
	workflowSaveKey   = "ctrl+s"
	workflowAnswerKey = "ctrl+a"
)

// The two queued/running states the engine's manager holds a background workflow in, and the
// running one's state while a prompt of it waits on the human; every other workflow reads the phase
// its status.json records.
const (
	workflowStateQueued  = "queued"
	workflowStateRunning = "running"
	workflowStateWaiting = "waiting for you"
)

// workflowOutputReadCap bounds how much of an item's detail output the item level reads: it is a
// reading on a pane, and an output file may be anything a child wrote.
const workflowOutputReadCap = 64 << 10

// workflowItemLineCap bounds the item level's rows, so a long conversation cannot make every frame
// measure thousands of rows; the last row then says how many were left off.
const workflowItemLineCap = 2000

// workflowToolArgsCap clips a tool call's arguments to one short line in the conversation.
const workflowToolArgsCap = 160

// workflowsLevel is which of the view's three levels is on screen.
type workflowsLevel uint8

const (
	workflowsAtList   workflowsLevel = iota // the session's workflows
	workflowsAtDetail                       // one workflow's stages and items
	workflowsAtItem                         // one item's receipt, output and conversation
)

// workflowItemRef names one item of one workflow: the workflow's id, and the stage and item
// indexes in its status.json. Script and ask items have no key, so the indexes are the identity.
type workflowItemRef struct {
	workflow string
	stage    int
	item     int
}

// workflowsPane is the /workflows view's state. It is plain values — the listing as its last load
// folded it, the three levels' cursors (so esc returns to the row the human left), and the item
// level's rows as its load composed them — and its zero value is "closed", so it lives inline in the
// value-copied Model (ADR 0011). listSeq and itemSeq number the loads in flight, so a slower, older
// read never folds over a newer one.
type workflowsPane struct {
	open    bool
	level   workflowsLevel
	infos   []workflow.Info
	list    listCursor
	detail  listCursor
	item    listCursor
	shown   string          // the workflow the detail and item levels show
	ref     workflowItemRef // the item the item level shows
	lines   []string        // that item's rows; nil until its load lands
	naming  bool            // the detail's ^s name row is up and takes every key
	nameBuf lineEditor      // the skill name typed so far; the zero field while not naming
	listSeq uint64
	itemSeq uint64
}

// workflowsListMsg carries an off-loop Engine.Workflows read back to the Update loop. opening is
// true for the /workflows verb's own read, false for a refresh of the open pane.
type workflowsListMsg struct {
	infos   []workflow.Info
	err     error
	opening bool
	seq     uint64
}

// workflowItemMsg carries an item's detail, composed off the loop from its folder, back to the
// Update loop.
type workflowItemMsg struct {
	ref   workflowItemRef
	lines []string
	seq   uint64
}

// workflowStoppedMsg carries an off-loop Engine.StopWorkflow answer back to the Update loop: nil once
// the stop is under way (the WorkflowPhaseEvent that ends the workflow refreshes the pane), else the
// engine's refusal.
type workflowStoppedMsg struct {
	err error
}

// workflowSavedMsg carries an off-loop save of a workflow as a recipe skill back to the Update loop:
// the name it was saved under and the folder written, or why nothing was.
type workflowSavedMsg struct {
	name string
	dir  string
	err  error
}

// Compile-time assertions that the view's Msgs are valid tea.Msgs (mirroring messages.go).
var (
	_ tea.Msg = workflowsListMsg{}
	_ tea.Msg = workflowItemMsg{}
	_ tea.Msg = workflowStoppedMsg{}
	_ tea.Msg = workflowSavedMsg{}
)

// openWorkflows is the /workflows verb: it reads the listing off the Update loop, and the pane opens
// when it lands (foldWorkflowsList).
func (m Model) openWorkflows() (tea.Model, tea.Cmd) {
	return m, m.listWorkflows(true)
}

// listWorkflows builds the Cmd that reads Engine.Workflows off the Update loop, numbering the read.
// It captures the engine by value, so the closure holds no pointer into the value-copied Model.
func (m *Model) listWorkflows(opening bool) tea.Cmd {
	m.workflowsPane.listSeq++
	eng, seq := m.eng, m.workflowsPane.listSeq
	return func() tea.Msg {
		infos, err := eng.Workflows()
		return workflowsListMsg{infos: infos, err: err, opening: opening, seq: seq}
	}
}

// refreshWorkflows re-reads the listing while the pane is up and e is a workflow's phase — a stage
// or an item moved, or a workflow started or ended — and is nil otherwise.
func (m *Model) refreshWorkflows(e domain.Event) tea.Cmd {
	if _, isPhase := e.(domain.WorkflowPhaseEvent); !isPhase || !m.workflowsPane.open {
		return nil
	}
	return m.listWorkflows(false)
}

// foldWorkflowsList folds a listing. The verb's own read opens the pane on the newest workflow, or
// notes an empty session or an error and opens nothing; a refresh of the open pane replaces the
// listing and keeps the level and every cursor, and re-reads the item the item level shows. A
// refresh for a closed pane, and one a newer read has overtaken, fold nothing.
func (m *Model) foldWorkflowsList(msg workflowsListMsg) tea.Cmd {
	pane := &m.workflowsPane
	if msg.seq != pane.listSeq || (!msg.opening && !pane.open) {
		return nil
	}
	if msg.err != nil {
		m.transcript.addNote(workflowsListFailed + msg.err.Error())
		return nil
	}
	if msg.opening {
		if len(msg.infos) == 0 {
			m.transcript.addNote(workflowsNone)
			return nil
		}
		*pane = workflowsPane{open: true, infos: msg.infos, listSeq: pane.listSeq}
		pane.list.seat(len(msg.infos)-1, len(msg.infos))
		return nil
	}
	pane.infos = msg.infos
	if pane.level == workflowsAtItem {
		return m.loadWorkflowItem(pane.ref)
	}
	return nil
}

// foldWorkflowItem folds an item's detail when the item level still shows that item and no newer
// read of it is in flight.
func (m *Model) foldWorkflowItem(msg workflowItemMsg) {
	pane := &m.workflowsPane
	if !pane.open || pane.level != workflowsAtItem || pane.ref != msg.ref || msg.seq != pane.itemSeq {
		return
	}
	pane.lines = msg.lines
	pane.item.clampSelection(len(msg.lines))
}

// shownInfo is the workflow the detail and item levels show, as the last listing read it; ok is
// false when the listing no longer holds it.
func (p workflowsPane) shownInfo() (workflow.Info, bool) {
	i := slices.IndexFunc(p.infos, func(info workflow.Info) bool { return info.Status.ID == p.shown })
	if i < 0 {
		return workflow.Info{}, false
	}
	return p.infos[i], true
}

// itemAt is the stage and item ref names in the listing; ok is false when either index is gone.
func (p workflowsPane) itemAt(ref workflowItemRef) (workflow.Info, workflow.StageStatus, workflow.ItemStatus, bool) {
	i := slices.IndexFunc(p.infos, func(info workflow.Info) bool { return info.Status.ID == ref.workflow })
	if i < 0 || ref.stage >= len(p.infos[i].Status.Stages) {
		return workflow.Info{}, workflow.StageStatus{}, workflow.ItemStatus{}, false
	}
	stage := p.infos[i].Status.Stages[ref.stage]
	if ref.item >= len(stage.Items) {
		return workflow.Info{}, workflow.StageStatus{}, workflow.ItemStatus{}, false
	}
	return p.infos[i], stage, stage.Items[ref.item], true
}

// loadWorkflowItem builds the Cmd that composes ref's detail off the Update loop from its folder —
// its output file and its saved conversation — numbering the read. An item the listing no longer
// holds folds as one prose row.
func (m *Model) loadWorkflowItem(ref workflowItemRef) tea.Cmd {
	m.workflowsPane.itemSeq++
	seq := m.workflowsPane.itemSeq
	info, stage, item, ok := m.workflowsPane.itemAt(ref)
	if !ok {
		return func() tea.Msg { return workflowItemMsg{ref: ref, lines: []string{workflowItemGone}, seq: seq} }
	}
	workspace := m.opts.Workspace
	return func() tea.Msg {
		return workflowItemMsg{ref: ref, lines: workflowItemLines(info.Dir, stage.Name, item, workspace), seq: seq}
	}
}

// cursor is the cursor of the level on screen. The pointer is into the caller's own copy and never
// outlives the call (the [listCursor.key] posture, ADR 0011).
func (p *workflowsPane) cursor() *listCursor {
	switch p.level {
	case workflowsAtDetail:
		return &p.detail
	case workflowsAtItem:
		return &p.item
	}
	return &p.list
}

// workflowsKey routes a keypress while the pane is up: ↑/↓ walk the level's rows, ⏎ opens what the
// highlight names, esc goes one level up, and a workflow's detail answers its chords (workflowsVerb). It swallows every other key — the pane is modal.
func (m Model) workflowsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.workflowsPane.naming {
		return m.workflowsNameKey(msg)
	}
	wrap := listWrapsAround
	if m.workflowsPane.level == workflowsAtItem {
		wrap = listStopsAtEnds // a reading scrolls; it does not cycle
	}
	switch m.workflowsPane.cursor().key(msg, m.workflowsChoiceCount(), wrap) {
	case listCloses:
		return m.workflowsBack(), nil
	case listAccepts:
		return m.workflowsAccept()
	case listUnclaimed:
		if m.workflowsPane.level == workflowsAtDetail {
			return m.workflowsVerb(msg)
		}
	case listSwallowed:
		// Spent by the cursor.
	}
	return m, nil
}

// workflowsVerb answers a chord in a workflow's detail, on the workflow it shows. ^x stops it off the
// Update loop (a queued one's stop writes its status.json); the engine refuses one it neither runs
// nor queues, and that refusal is noted. ^r re-runs its failed items: the launch reads the engine
// for its snapshot, so it goes only at idle, off the loop and under the /bg launch latch
// (bgLaunching), and folds as a /bg launch does (foldBgStarted); while an actuation is in flight it
// is refused through the predicate that refuses /bg (actuationBlocked), with the latch's own note. ^s opens the name row that saves it
// as a recipe (openWorkflowSave). ^a opens the prompt it waits on (answerShownWorkflow). Any other
// key is swallowed.
func (m Model) workflowsVerb(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	id, eng := m.workflowsPane.shown, m.eng
	switch msg.String() {
	case workflowStopKey:
		return m, func() tea.Msg { return workflowStoppedMsg{err: eng.StopWorkflow(id)} }
	case workflowRerunKey:
		if m.actuation.inFlight && actuationBlocked("bg") {
			m.transcript.addNote(m.actuationBlockNote())
			return m, nil
		}
		if !m.engineHolds().canLaunchBg() {
			m.transcript.addNote(workflowRerunNotIdle)
			return m, nil
		}
		m.bgLaunching = true
		return m, func() tea.Msg { return bgStartedMsg{id: id, err: eng.RerunFailed(id)} }
	case workflowSaveKey:
		return m.openWorkflowSave(), nil
	case workflowAnswerKey:
		return m.answerShownWorkflow()
	}
	return m, nil
}

// answerShownWorkflow is ^a in a workflow's detail: it closes the pane and opens the shown workflow's
// oldest waiting prompt — one esc sent back to the queue included, whose dismissal it clears — in
// the approval or the ask pane, through the route the idle offer takes (openWorkflowPrompt), so the
// answer returns to idle and resumes no worker. A decision pane mid-Turn belongs to the running
// Exchange, so it goes only when the session is idle as canWake reads it with this pane taken as
// closed, and otherwise notes why and opens nothing. A workflow with nothing waiting — by the folded
// count, or by the engine's queue once read — takes nothing.
func (m Model) answerShownWorkflow() (tea.Model, tea.Cmd) {
	id := m.workflowsPane.shown
	if !m.workflows.waits(id) {
		return m, nil
	}
	closed := m
	closed.workflowsPane = workflowsPane{}
	if !closed.canWake() {
		m.transcript.addNote(workflowAnswerNotIdle)
		return m, nil
	}
	prompts := m.eng.WorkflowPrompts()
	m.workflows = m.workflows.synced(prompts)
	i := slices.IndexFunc(prompts, func(prompt domain.WorkflowPrompt) bool { return prompt.Workflow == id })
	if i < 0 {
		return m, nil
	}
	closed.workflows = m.workflows.withoutDismissed(prompts[i].ID)
	return closed.openWorkflowPrompt(prompts[i])
}

// openWorkflowSave is ^s in a workflow's detail: it opens the name row the workflow is saved under.
// Only a fan_out's workflow saves — a recipe's already is one, and saying which is the answer — and
// only with an apogee home resolved to hold the library.
func (m Model) openWorkflowSave() Model {
	info, ok := m.workflowsPane.shownInfo()
	switch {
	case !ok:
		return m
	case info.Status.Recipe != "":
		m.transcript.addNote(fmt.Sprintf(workflowSaveRecipe, sanitize.StripEscapesToLine(info.Status.Recipe)))
		return m
	case m.opts.ConfigHome == "":
		m.transcript.addError(skillsSource, noSkillExporterNote, runRef{})
		return m
	}
	m.workflowsPane.naming = true
	m.workflowsPane.nameBuf = newPopupField(m.opts.CursorShape, m.th.surface, workflowSaveCaret, "")
	return m
}

// workflowsNameKey drives the save row's name field: printable text and backspace edit it, esc
// closes it and saves nothing, and ⏎ saves the workflow under the name typed (an empty one is a
// no-op). The field takes every other key too — it is a modal surface of its own.
func (m Model) workflowsNameKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pane := &m.workflowsPane
	switch msg.String() {
	case "esc":
		pane.naming, pane.nameBuf = false, lineEditor{}
		return m, nil
	case "enter":
		name := strings.TrimSpace(pane.nameBuf.value())
		pane.naming, pane.nameBuf = false, lineEditor{}
		if name == "" {
			return m, nil
		}
		return m.saveWorkflowRecipe(name)
	case "backspace":
		return m, pane.nameBuf.editKey(msg)
	}
	if msg.Text != "" { // a printable keypress carries its rune(s) in Text
		return m, pane.nameBuf.editKey(msg)
	}
	return m, nil
}

// saveWorkflowRecipe saves the shown workflow's plan as the recipe skill name. A name a skill of the
// catalog or a command verb already answers to is refused here — the new `/name` would be shadowed
// by one or shadow the other; the rest runs off the Update loop: plan.json read, the recipe
// rendered (workflow.PlanToRecipe), and the folder claimed and written by skills.WriteNew, which
// refuses a name that is a path and never overwrites a folder already there.
func (m Model) saveWorkflowRecipe(name string) (tea.Model, tea.Cmd) {
	info, ok := m.workflowsPane.shownInfo()
	if !ok {
		m.transcript.addNote(workflowsGone)
		return m, nil
	}
	if _, isCommand := commandByName(name); isCommand {
		m.transcript.addError(skillsSource, fmt.Sprintf(workflowSaveTaken, "command", name), runRef{})
		return m, nil
	}
	if m.opts.Skills != nil {
		if _, isSkill := m.opts.Skills.Get(name); isSkill {
			m.transcript.addError(skillsSource, fmt.Sprintf(workflowSaveTaken, "skill", name), runRef{})
			return m, nil
		}
	}
	dir, library := info.Dir, filepath.Join(m.opts.ConfigHome, "skills")
	return m, func() tea.Msg {
		plan, err := workflow.ReadFolderPlan(dir)
		if err != nil {
			return workflowSavedMsg{name: name, err: err}
		}
		content, err := workflow.PlanToRecipe(plan)
		if err != nil {
			return workflowSavedMsg{name: name, err: err}
		}
		written, err := skills.WriteNew(name, library, content)
		return workflowSavedMsg{name: name, dir: written, err: err}
	}
}

// foldWorkflowSaved reports a save: the refusal as an error entry, since nothing was written, or the
// folder written and the command that now runs it — with the catalog re-scanned off the loop, so the
// new skill is loadable at once.
func (m *Model) foldWorkflowSaved(msg workflowSavedMsg) tea.Cmd {
	if msg.err != nil {
		m.transcript.addError(skillsSource, msg.err.Error(), runRef{})
		return nil
	}
	m.transcript.addNote(fmt.Sprintf(workflowSavedNote, msg.name, msg.dir, msg.name))
	return m.reloadSkillsCmd()
}

// foldWorkflowStopped notes a refused stop; a stop under way needs no word — the pane re-reads as the
// workflow's end is reported.
func (m *Model) foldWorkflowStopped(msg workflowStoppedMsg) {
	if msg.err != nil {
		m.transcript.addNote(msg.err.Error())
	}
}

// workflowsBack is esc: the item level returns to its workflow, the workflow to the list, and the
// list closes the pane.
func (m Model) workflowsBack() Model {
	pane := &m.workflowsPane
	switch pane.level {
	case workflowsAtItem:
		pane.level, pane.ref, pane.lines, pane.item = workflowsAtDetail, workflowItemRef{}, nil, listCursor{}
	case workflowsAtDetail:
		pane.level, pane.shown, pane.detail = workflowsAtList, "", listCursor{}
	default:
		m.workflowsPane = workflowsPane{}
	}
	return m
}

// workflowsAccept is ⏎ on the highlighted row — the keyboard's and a second click's alike: a
// workflow opens to its stages and items, an item opens to its detail (read off the loop), and a
// stage's own row or the item level's reading takes nothing.
func (m Model) workflowsAccept() (tea.Model, tea.Cmd) {
	pane := &m.workflowsPane
	switch pane.level {
	case workflowsAtList:
		if pane.list.selected >= len(pane.infos) {
			return m, nil
		}
		pane.level, pane.shown, pane.detail = workflowsAtDetail, pane.infos[pane.list.selected].Status.ID, listCursor{}
	case workflowsAtDetail:
		info, ok := pane.shownInfo()
		if !ok {
			return m, nil
		}
		_, targets := workflowDetailRows(info)
		if pane.detail.selected >= len(targets) || targets[pane.detail.selected].item < 0 {
			return m, nil
		}
		target := targets[pane.detail.selected]
		pane.level, pane.lines, pane.item = workflowsAtItem, nil, listCursor{}
		pane.ref = workflowItemRef{workflow: pane.shown, stage: target.stage, item: target.item}
		return m, m.loadWorkflowItem(pane.ref)
	}
	return m, nil
}

// workflowsChoiceCount is how many rows the level on screen offers the cursor: 0 where it shows
// prose — an empty workflow, one the listing lost, an item still loading.
func (m Model) workflowsChoiceCount() int {
	rows, choices := m.workflowsRows()
	if !choices {
		return 0
	}
	return len(rows)
}

// workflowsRows is the level on screen's rows, and whether they are choices or prose.
func (m Model) workflowsRows() ([]popupRow, bool) {
	pane := m.workflowsPane
	switch pane.level {
	case workflowsAtDetail:
		info, ok := pane.shownInfo()
		if !ok {
			return singleCellRows([]string{workflowsGone}), false
		}
		if pane.naming {
			return singleCellRows([]string{workflowSavePrompt + pane.nameBuf.textWithCaret()}), false
		}
		rows, _ := workflowDetailRows(info)
		if len(rows) == 0 {
			return singleCellRows([]string{workflowsNoStages}), false
		}
		return rows, true
	case workflowsAtItem:
		if pane.lines == nil {
			return singleCellRows([]string{workflowItemLoading}), false
		}
		return singleCellRows(pane.lines), true
	}
	rows := make([]popupRow, 0, len(pane.infos))
	for _, info := range pane.infos {
		rows = append(rows, workflowListRow(info, m.workflows.waits(info.Status.ID)))
	}
	return rows, len(rows) > 0
}

// workflowsListContent is everything the pane says about itself to the shared list surface for the
// level on screen; ok is false with the pane closed.
func (m Model) workflowsListContent() (listContent, bool) {
	pane := m.workflowsPane
	if !pane.open {
		return listContent{}, false
	}
	rows, choices := m.workflowsRows()
	c := listContent{pane: paneWorkflows, rows: rows, selected: -1}
	if choices {
		c.selected = pane.cursor().highlight(len(rows))
	}
	switch pane.level {
	case workflowsAtList:
		c.title, c.hint, c.rowCap = fmt.Sprintf("workflows  (%d)", len(pane.infos)), workflowsListHint, maxWorkflowRows
	case workflowsAtDetail:
		c.title, c.hint, c.rowCap = "workflow  "+sanitize.StripEscapesToLine(pane.shown), workflowsDetailHint, maxWorkflowRows
		if m.workflows.waits(pane.shown) {
			c.hint = workflowsAnswerHint
		}
		if info, ok := pane.shownInfo(); ok {
			c.title = "workflow  " + workflowName(info) + "  (" + workflowState(info, m.workflows.waits(pane.shown)) + ")"
		}
		if pane.naming {
			c.hint = workflowsNamingHint
		}
	default:
		c.title, c.hint, c.rowCap = workflowItemTitle(pane), workflowsItemHint, maxWorkflowItemRows
	}
	return c, true
}

// renderWorkflows paints the pane, or "" when it is closed or the frame cannot seat it.
func (m Model) renderWorkflows() string {
	view, _ := m.renderWorkflowsPlaced()
	return view
}

// renderWorkflowsPlaced is that paint with the painter's placement beside it, for the pointer
// (popupPaneHit).
func (m Model) renderWorkflowsPlaced() (string, popupPlacement) {
	c, ok := m.workflowsListContent()
	if !ok {
		return "", popupPlacement{}
	}
	view, place, _ := m.renderListPlaced(c)
	return view, place
}

// workflowsHeight is the rows renderWorkflows paints the pane in, answered without painting it
// (listHeight); 0 where it paints nothing.
func (m Model) workflowsHeight() int {
	c, ok := m.workflowsListContent()
	if !ok {
		return 0
	}
	return m.listHeight(c)
}

// handleWorkflowsClick answers a left-click while the pane is up, by the browser's rule
// (handleBrowserClick, mouse.go): outside the box the pane is dismissed and the click spent; a click
// on a row seats the highlight, and a second click on that row is the ⏎.
func (m Model) handleWorkflowsClick(pre Model, msg tea.MouseClickMsg) (Model, tea.Cmd, bool) {
	if !m.workflowsPane.open || !pre.workflowsPane.open {
		return m, nil, false
	}
	row, top, inRect, onRow := popupPaneHit(pre, paneWorkflows, pre.renderWorkflowsPlaced, msg.Y)
	if !inRect {
		m.workflowsPane = workflowsPane{}
		return m, nil, true
	}
	count := m.workflowsChoiceCount()
	if !onRow || count == 0 {
		return m, nil, true // the pane's chrome, or prose: claimed, with no row to take
	}
	if m.clickArmed.holds(paneWorkflows, row) {
		m.clickArmed = clickArm{}
		next, cmd := acceptedModel(m.workflowsAccept())
		return next, cmd, true
	}
	m.workflowsPane.cursor().seat(row, count)
	m.clickArmed = clickArm{pane: paneWorkflows, row: row, top: top, ok: true}
	return m, nil, true
}

// workflowsWheel walks the level's highlight one row per notch while the pointer is over the pane,
// and is always handled there — the pane is modal.
func (m Model) workflowsWheel(msg tea.MouseWheelMsg) (Model, bool) {
	if !m.workflowsPane.open {
		return m, false
	}
	y0, h, ok := m.frameSpans().pane(paneWorkflows)
	if !ok || msg.Y < y0 || msg.Y >= y0+h {
		return m, false
	}
	m.workflowsPane.cursor().wheel(msg, m.workflowsChoiceCount())
	return m, true
}

// workflowName is the name a workflow's row leads with: its own, or its id where it has none.
func workflowName(info workflow.Info) string {
	if info.Status.Name != "" {
		return sanitize.StripEscapesToLine(info.Status.Name)
	}
	return sanitize.StripEscapesToLine(info.Status.ID)
}

// workflowState is how a workflow stands, as the engine's StateOf reads it: queued or running when
// the session's manager holds it in the background — waiting for you, running, when a prompt of it
// waits on the human (the folded backgroundWorkflows count, never the engine) — else the phase its
// status.json records.
func workflowState(info workflow.Info, waiting bool) string {
	state := workflow.StateOf(info)
	switch {
	case state.Kind == workflow.StateQueued:
		return workflowStateQueued
	case state.Kind == workflow.StateBackground && waiting:
		return workflowStateWaiting
	case state.Kind == workflow.StateBackground:
		return workflowStateRunning
	}
	return sanitize.StripEscapesToLine(string(state.Phase))
}

// workflowListRow is one workflow's row: its name, its state (waiting says a prompt of it waits on
// the human), its fan-out items done of all (the engine's TallyOfStatus — verify and merge items
// are not items, a skipped fan-out is left out), its id.
func workflowListRow(info workflow.Info, waiting bool) popupRow {
	tally := workflow.TallyOfStatus(info.Status)
	return popupRow{
		workflowName(info),
		"· " + workflowState(info, waiting),
		fmt.Sprintf("· %d/%d items", tally.Total()-tally.Unfinished, tally.Total()),
		"· " + sanitize.StripEscapesToLine(info.Status.ID),
	}
}

// workflowRowTarget is what a detail row stands for: a stage's own row (item −1) or one of its items.
type workflowRowTarget struct {
	stage int
	item  int
}

// workflowDetailRows is one workflow's detail rows — each stage's row (name, kind and phase, and
// its note), then its items (number, shown name, and status and summary) — with what each row stands
// for.
func workflowDetailRows(info workflow.Info) ([]popupRow, []workflowRowTarget) {
	var (
		rows    []popupRow
		targets []workflowRowTarget
	)
	for s, stage := range info.Status.Stages {
		head := popupRow{
			sanitize.StripEscapesToLine(stage.Name),
			"· " + sanitize.StripEscapesToLine(string(stage.Kind)) + " · " + sanitize.StripEscapesToLine(string(stage.Phase)),
		}
		if stage.Note != "" {
			head = append(head, "— "+sanitize.StripEscapesToLine(stage.Note))
		}
		rows = append(rows, head)
		targets = append(targets, workflowRowTarget{stage: s, item: -1})
		for i, item := range stage.Items {
			rows = append(rows, popupRow{
				"  #" + strconv.Itoa(i+1) + " " + workflowItemName(item),
				"· " + workflowItemStatus(item),
			})
			targets = append(targets, workflowRowTarget{stage: s, item: i})
		}
	}
	return rows, targets
}

// workflowItemStatus is one item's outcome so far: `<status> — <summary>` once its receipt is in,
// its phase until then — the status word the engine's (workflow.ItemStatusWord), escape-stripped:
// status.json is read from disk.
func workflowItemStatus(item workflow.ItemStatus) string {
	status := sanitize.StripEscapesToLine(workflow.ItemStatusWord(item.Phase, item.Receipt))
	if item.Receipt == nil {
		return status
	}
	if item.Receipt.Summary == "" {
		return status
	}
	return status + " — " + sanitize.StripEscapesToLine(item.Receipt.Summary)
}

// workflowItemTitle is the item level's title: the stage, the item's number and its shown name.
func workflowItemTitle(pane workflowsPane) string {
	_, stage, item, ok := pane.itemAt(pane.ref)
	if !ok {
		return "item"
	}
	return sanitize.StripEscapesToLine(stage.Name) + "  #" + strconv.Itoa(pane.ref.item+1) + "  " + workflowItemName(item)
}

// workflowItemName is the escape-stripped name an item is shown by: its short name (item.Name), else
// its label — a script or ask stage's line records no short name, nor does a status.json written
// before items had one. The label stays the item's identity: its output path is spelled from it.
func workflowItemName(item workflow.ItemStatus) string {
	if item.Name != "" {
		return sanitize.StripEscapesToLine(item.Name)
	}
	return sanitize.StripEscapesToLine(item.Label)
}

// workflowItemLines composes one item's detail from its folder dir: its outcome and receipt fields,
// then — for an item a child ran — its detail output (a relative `out:` path is the workspace's) and
// its saved conversation. It reads the disk, so it runs off the Update loop (loadWorkflowItem). Every
// line is escape-stripped: an output file and a conversation are untrusted text.
func workflowItemLines(dir, stage string, item workflow.ItemStatus, workspace string) []string {
	lines := []string{"status: " + workflowItemStatus(item)}
	if item.Receipt != nil {
		keys := make([]string, 0, len(item.Receipt.Fields))
		for key := range item.Receipt.Fields {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			lines = append(lines, "  "+sanitize.StripEscapesToLine(key)+": "+sanitize.StripEscapesToLine(fmt.Sprint(item.Receipt.Fields[key])))
		}
	}
	if item.Key == "" {
		return append(lines, "", workflowItemNoChild)
	}
	lines = append(lines, "")
	lines = append(lines, workflowOutputLines(dir, stage, item, workspace)...)
	lines = append(lines, "")
	lines = append(lines, workflowConversationLines(dir, item.Key)...)
	if len(lines) > workflowItemLineCap {
		dropped := len(lines) - workflowItemLineCap + 1
		lines = append(lines[:workflowItemLineCap-1], fmt.Sprintf("… %d more lines", dropped))
	}
	return lines
}

// workflowOutputLines is the item's detail output: an `output: <path>` line, then the file's lines
// (at most workflowOutputReadCap bytes of it), or why there are none.
func workflowOutputLines(dir, stage string, item workflow.ItemStatus, workspace string) []string {
	path, err := workflow.ItemOutputPath(dir, stage, item.Key, item.Label)
	if err != nil {
		return []string{"output:" + workflowOutputFailed + sanitize.StripEscapesToLine(err.Error())}
	}
	head := "output: " + sanitize.StripEscapesToLine(path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	text, truncated, err := readCapped(path, workflowOutputReadCap)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return []string{head + workflowOutputMissing}
	case err != nil:
		return []string{head + workflowOutputFailed + sanitize.StripEscapesToLine(err.Error())}
	}
	lines := append([]string{head}, indentedLines(text)...)
	if truncated {
		lines = append(lines, fmt.Sprintf("  … output cut at %d KiB", workflowOutputReadCap>>10))
	}
	return lines
}

// readCapped reads at most limit bytes of the file at path, and reports whether there was more.
func readCapped(path string, limit int) (text string, truncated bool, err error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = file.Close() }() // read-only: a close error loses nothing
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return "", false, err
	}
	if len(data) > limit {
		return string(data[:limit]), true, nil
	}
	return string(data), false, nil
}

// workflowConversationLines is the item child's saved conversation, one block per message — its
// role, then its text and its tool calls indented — or why there is none.
func workflowConversationLines(dir, key string) []string {
	messages, found, err := workflow.ReadItemTranscript(dir, key)
	switch {
	case err != nil:
		return []string{workflowTranscriptBad + sanitize.StripEscapesToLine(err.Error())}
	case !found:
		return []string{workflowNoTranscript}
	}
	lines := []string{fmt.Sprintf("conversation: %d messages", len(messages))}
	for _, message := range messages {
		lines = append(lines, sanitize.StripEscapesToLine(string(message.Role))+":")
		lines = append(lines, indentedLines(message.Content)...)
		for _, call := range message.ToolCalls {
			lines = append(lines, "  → "+sanitize.StripEscapesToLine(call.Tool)+" "+workflowToolArgs(call.Arguments))
		}
	}
	return lines
}

// workflowToolArgs is a tool call's arguments on one clipped line.
func workflowToolArgs(arguments json.RawMessage) string {
	line := sanitize.StripEscapesToLine(string(arguments))
	if runes := []rune(line); len(runes) > workflowToolArgsCap {
		return string(runes[:workflowToolArgsCap]) + "…"
	}
	return line
}

// indentedLines splits text into escape-stripped lines indented two cells; blank text gives none.
func indentedLines(text string) []string {
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	split := strings.Split(text, "\n")
	lines := make([]string, 0, len(split))
	for _, line := range split {
		lines = append(lines, "  "+sanitize.StripEscapesToLine(line))
	}
	return lines
}
