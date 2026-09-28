package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/workflow"
)

// ----------------------------------------------------------------------------
// The /workflows view (workflows.go)
// ----------------------------------------------------------------------------

// testWorkflowKey is an item key of the store's own shape: a SHA-256 in lower-case hex.
var testWorkflowKey = strings.Repeat("a1", 32)

// workflowsFixture writes one workflow folder to a temp scratch dir — a fan-out stage with a finished
// item (its output file and its conversation saved) and a running one, then a script stage whose
// one line runs no child — and returns the listing Engine.Workflows would read of it.
func workflowsFixture(t *testing.T) []workflow.Info {
	t.Helper()
	store, err := workflow.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	plan := workflow.Plan{Name: "audit", Stages: []workflow.Stage{
		{Name: "find", Kind: workflow.StageFanout, Task: "look at {item}"},
		{Name: "flags", Kind: workflow.StageScript, Run: "true"},
	}}
	status, err := store.Create(plan, "hash", time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	status.Phase = workflow.PhaseRunning
	status.Stages[0].Phase = workflow.PhaseRunning
	status.Stages[0].Items = []workflow.ItemStatus{
		{Key: testWorkflowKey, Label: "internal/a.go", Phase: workflow.PhaseDone, Receipt: &workflow.Receipt{
			Status: workflow.StatusOK, Summary: "found it", Fields: map[string]any{"findings": 2},
		}},
		{Key: strings.Repeat("b2", 32), Label: "internal/b.go", Phase: workflow.PhaseRunning},
	}
	status.Stages[1].Items = []workflow.ItemStatus{
		{Label: "flags", Phase: workflow.PhaseDone, Receipt: &workflow.Receipt{Status: workflow.StatusOK, Summary: "ran"}},
	}
	if err := store.WriteStatus(status); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteTranscript(status.ID, testWorkflowKey, []domain.Message{
		{Role: domain.RoleUser, Content: "audit internal/a.go"},
		{Role: domain.RoleAssistant, Content: "reading it", ToolCalls: []domain.ToolCall{
			{ID: "c1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"internal/a.go"}`)},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	output, err := store.Path(status.ID, "items/"+testWorkflowKey+"/output.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("# findings\n\x1b[31mone\x1b[0m\ttwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, err := store.Dir(status.ID)
	if err != nil {
		t.Fatal(err)
	}
	return []workflow.Info{{Status: status, Dir: dir, Background: true}}
}

// openWorkflowsLine runs `/workflows` on m and folds every message its Cmd produces.
func openWorkflowsLine(t *testing.T, m Model) Model {
	t.Helper()
	m.input.SetValue("/workflows")
	m, cmd := stepCmd(t, m, keyEnter())
	return foldWorkflowMsgs(t, m, cmd)
}

// foldWorkflowMsgs runs cmd and folds the view's own messages it produced, and any Cmd those folds
// hand back, in order.
func foldWorkflowMsgs(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range cmdMsgs(cmd) {
		switch msg.(type) {
		case workflowsListMsg, workflowItemMsg:
			var next tea.Cmd
			m, next = stepCmd(t, m, msg)
			m = foldWorkflowMsgs(t, m, next)
		}
	}
	return m
}

// workflowsKeyStep presses key on m and folds what the press asked the view to read.
func workflowsKeyStep(t *testing.T, m Model, key tea.KeyPressMsg) Model {
	t.Helper()
	m, cmd := stepCmd(t, m, key)
	return foldWorkflowMsgs(t, m, cmd)
}

// workflowsPaneModel is the fixture's workflow open in the view at the given level, with a screenful
// of transcript behind it: the list, the workflow's detail, or its first item's.
func workflowsPaneModel(t *testing.T, level workflowsLevel) Model {
	t.Helper()
	eng := &fakeEngine{workflowInfos: workflowsFixture(t)}
	m := openWorkflowsLine(t, streamOneScreen(t, newTestModelEng(t, eng, testOpts)))
	if level >= workflowsAtDetail {
		m = workflowsKeyStep(t, m, keyEnter())
	}
	if level >= workflowsAtItem {
		m = workflowsKeyStep(t, m, keyDown()) // off the stage's own row, onto its first item
		m = workflowsKeyStep(t, m, keyEnter())
	}
	if m.workflowsPane.level != level {
		t.Fatalf("precondition: the view is at level %d, want %d", m.workflowsPane.level, level)
	}
	m.layout()
	return m
}

// assertPaneHas fails for every want the painted pane does not carry.
func assertPaneHas(t *testing.T, m Model, wants ...string) {
	t.Helper()
	painted := strip(m.renderWorkflows())
	for _, want := range wants {
		if !strings.Contains(painted, want) {
			t.Errorf("the pane does not carry %q:\n%s", want, painted)
		}
	}
}

// /workflows lists the session's workflows with their state and item counts; ⏎ opens one to its
// stages and items, ⏎ on an item opens its receipt, output and conversation, and esc walks back up
// one level at a time until the pane closes.
func TestWorkflowsViewWalksDownToAnItemAndEscGoesBackUp(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{workflowInfos: workflowsFixture(t)}
	m := newTestModelEng(t, eng, testOpts)
	m = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 60})

	m = openWorkflowsLine(t, m)
	if !m.workflowsPane.open || m.workflowsPane.level != workflowsAtList {
		t.Fatalf("pane = %+v, want the list open", m.workflowsPane)
	}
	assertPaneHas(t, m, "workflows  (1)", "audit", "· running", "· 1/2 items", "· "+eng.workflowInfos[0].Status.ID)

	m = workflowsKeyStep(t, m, keyEnter())
	if m.workflowsPane.level != workflowsAtDetail {
		t.Fatalf("level = %d after ⏎ on the list, want the detail", m.workflowsPane.level)
	}
	assertPaneHas(t, m, "workflow  audit  (running)", "find", "· fanout · running",
		"#1 internal/a.go", "· ok — found it", "#2 internal/b.go", "flags", "· script · pending")

	m = workflowsKeyStep(t, m, keyEnter()) // on the stage's own row: nothing to open
	if m.workflowsPane.level != workflowsAtDetail {
		t.Fatalf("level = %d after ⏎ on a stage row, want the detail still", m.workflowsPane.level)
	}
	m = workflowsKeyStep(t, m, keyDown())
	m = workflowsKeyStep(t, m, keyEnter())
	if m.workflowsPane.level != workflowsAtItem {
		t.Fatalf("level = %d after ⏎ on an item, want the item", m.workflowsPane.level)
	}
	output := filepath.Join(eng.workflowInfos[0].Dir, "items", testWorkflowKey, "output.md")
	if !slices.Contains(m.workflowsPane.lines, "output: "+output) {
		t.Errorf("the item's rows %q do not name its output file %s", m.workflowsPane.lines, output)
	}
	assertPaneHas(t, m, "find  #1  internal/a.go", "status: ok — found it", "findings: 2", "# findings", "one", "conversation: 2 messages", "user:", "audit internal/a.go",
		`→ read_file {"path":"internal/a.go"}`)
	if painted := m.renderWorkflows(); strings.Contains(painted, "\x1b[31m") {
		t.Errorf("the output file's own escape reached the pane: %q", painted)
	}

	for _, want := range []workflowsLevel{workflowsAtDetail, workflowsAtList} {
		m = workflowsKeyStep(t, m, keyEsc())
		if !m.workflowsPane.open || m.workflowsPane.level != want {
			t.Fatalf("pane = open %v level %d after esc, want level %d", m.workflowsPane.open, m.workflowsPane.level, want)
		}
	}
	m = workflowsKeyStep(t, m, keyEsc())
	if m.workflowsPane.open {
		t.Errorf("the pane is still open after esc on the list")
	}
}

// esc from an item returns to the row the human opened it from, not to the top of the workflow.
func TestWorkflowsViewEscKeepsTheDetailRow(t *testing.T) {
	t.Parallel()
	m := workflowsPaneModel(t, workflowsAtItem)

	m = workflowsKeyStep(t, m, keyEsc())

	if got := m.workflowsPane.detail.selected; got != 1 {
		t.Errorf("detail highlight = %d after esc, want 1 (the item that was open)", got)
	}
}

// An item whose stage runs no child — a script or ask stage's one line — opens to its receipt and
// says it has no output and no conversation.
func TestWorkflowsViewAScriptItemHasNoChild(t *testing.T) {
	t.Parallel()
	m := workflowsPaneModel(t, workflowsAtDetail)
	m = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 60})
	for range 4 { // the stage row, two items, then the script stage's row and its one line
		m = workflowsKeyStep(t, m, keyDown())
	}

	m = workflowsKeyStep(t, m, keyEnter())

	assertPaneHas(t, m, "status: ok — ran", workflowItemNoChild)
}

// A session with no workflows opens nothing and says so, and a listing that fails is noted with the
// engine's error.
func TestWorkflowsViewEmptyOrFailedListingOpensNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		eng  *fakeEngine
		note string
	}{
		{"empty", &fakeEngine{}, "no workflows in this session"},
		{"error", &fakeEngine{workflowsErr: errors.New("disk gone")}, "could not list workflows: disk gone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := openWorkflowsLine(t, newTestModelEng(t, tc.eng, testOpts))

			if m.workflowsPane.open {
				t.Errorf("the pane opened: %+v", m.workflowsPane)
			}
			assertLastNote(t, m, tc.note)
		})
	}
}

// /workflows answers while a Turn runs: the pane opens over the running Exchange, takes the keys,
// and its esc closes it without stopping the worker.
func TestWorkflowsViewOpensWhileATurnRuns(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{workflowInfos: workflowsFixture(t)}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("open the exchange")
	m, _ = stepCmd(t, m, keyEnter())
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v, want running", m.state)
	}

	m = openWorkflowsLine(t, m)
	if !m.workflowsPane.open {
		t.Fatal("the pane did not open while the Turn runs")
	}
	m = workflowsKeyStep(t, m, keyEnter())
	if m.workflowsPane.level != workflowsAtDetail {
		t.Errorf("⏎ did not reach the pane mid-Turn: level = %d", m.workflowsPane.level)
	}
	m = workflowsKeyStep(t, m, keyEsc())
	m = workflowsKeyStep(t, m, keyEsc())

	if m.workflowsPane.open || m.state != stateRunning {
		t.Errorf("after esc×2: pane open %v, state %v; want closed and still running", m.workflowsPane.open, m.state)
	}
}

// A workflow phase re-reads the listing while the pane is up — keeping the level it is at — and reads
// nothing while it is closed.
func TestWorkflowsViewRefreshesOnAWorkflowPhase(t *testing.T) {
	t.Parallel()
	m := workflowsPaneModel(t, workflowsAtDetail)
	eng := m.eng.(*fakeEngine)
	infos := workflowsFixture(t)
	infos[0].Status.Stages[0].Items[1].Phase = workflow.PhaseDone
	infos[0].Status.Stages[0].Items[1].Receipt = &workflow.Receipt{Status: workflow.StatusPartial, Summary: "half"}
	eng.mu.Lock()
	eng.workflowInfos, eng.workflowListings = infos, 0
	eng.mu.Unlock()
	phase := eventMsg{Event: domain.WorkflowPhaseEvent{Phase: domain.WorkflowItemFinished, Workflow: "w", Background: true}}

	m, cmd := stepCmd(t, m, phase)
	m = foldWorkflowMsgs(t, m, cmd)

	if m.workflowsPane.level != workflowsAtDetail {
		t.Errorf("level = %d after the refresh, want the detail kept", m.workflowsPane.level)
	}
	assertPaneHas(t, m, "· partial — half")

	m = workflowsKeyStep(t, m, keyEsc())
	m = workflowsKeyStep(t, m, keyEsc())
	eng.mu.Lock()
	eng.workflowListings = 0
	eng.mu.Unlock()
	_, cmd = stepCmd(t, m, phase)
	cmdMsgs(cmd)
	if eng.workflowListings != 0 {
		t.Errorf("a phase with the pane closed read the listing %d times, want none", eng.workflowListings)
	}
}

// A listing a newer read has overtaken folds nothing: the older read lands last and would otherwise
// put back what the newer one replaced.
func TestWorkflowsViewDropsAnOvertakenListing(t *testing.T) {
	t.Parallel()
	m := workflowsPaneModel(t, workflowsAtList)
	stale := m.listWorkflows(false)
	fresh := m.listWorkflows(false)
	staleMsg := stale().(workflowsListMsg)
	staleMsg.infos = nil

	m = step(t, m, fresh())
	m = step(t, m, staleMsg)

	if len(m.workflowsPane.infos) != 1 {
		t.Errorf("listing = %d workflows after the overtaken read landed, want the newer read's 1", len(m.workflowsPane.infos))
	}
}

// The pointer answers the view as the browser's rule does: a click on an item seats the highlight, a
// second click on it is the ⏎ that opens it, and a click outside the pane closes it.
func TestWorkflowsViewClickOpensAnItemAndAClickOutsideCloses(t *testing.T) {
	t.Parallel()
	m := workflowsPaneModel(t, workflowsAtDetail)
	x, y := frameCell(t, m, "#2 internal/b.go")

	m = step(t, m, leftClick(x, y))
	if m.workflowsPane.detail.selected != 2 || m.workflowsPane.level != workflowsAtDetail {
		t.Fatalf("after one click: highlight %d at level %d, want row 2 seated at the detail",
			m.workflowsPane.detail.selected, m.workflowsPane.level)
	}
	m, cmd := stepCmd(t, m, leftClick(x, y))
	m = foldWorkflowMsgs(t, m, cmd)
	if m.workflowsPane.level != workflowsAtItem || m.workflowsPane.ref.item != 1 {
		t.Fatalf("after the second click: level %d on item %d, want the second item open", m.workflowsPane.level, m.workflowsPane.ref.item)
	}

	m = step(t, m, leftClick(0, 0))

	if m.workflowsPane.open {
		t.Error("a click outside the pane left it open")
	}
}

// workflowsVerbStep presses key on m and folds every message its Cmd produced — a stop's answer, a
// re-run's launch answer — and what those folds hand back.
func workflowsVerbStep(t *testing.T, m Model, key tea.KeyPressMsg) Model {
	t.Helper()
	m, cmd := stepCmd(t, m, key)
	for _, msg := range cmdMsgs(cmd) {
		m, _ = stepCmd(t, m, msg)
	}
	return m
}

// In a workflow's detail ^x stops the workflow it shows, and the pane stays where it was; a stop the
// engine refuses is noted in its own words.
func TestWorkflowsViewCtrlXStopsTheShownWorkflow(t *testing.T) {
	t.Parallel()
	m := workflowsPaneModel(t, workflowsAtDetail)
	eng := m.eng.(*fakeEngine)
	id := m.workflowsPane.shown

	m = workflowsVerbStep(t, m, keyCtrl('x'))

	if stops, reruns := eng.workflowActions(); !slices.Equal(stops, []string{id}) || len(reruns) != 0 {
		t.Errorf("stops = %v, reruns = %v; want one stop of %s and no re-run", stops, reruns, id)
	}
	if !m.workflowsPane.open || m.workflowsPane.level != workflowsAtDetail {
		t.Errorf("after ^x: pane open %v at level %d, want the detail kept", m.workflowsPane.open, m.workflowsPane.level)
	}

	eng.mu.Lock()
	eng.stopWorkflowFn = func(string) error { return errors.New("apogee: no background workflow is running") }
	eng.mu.Unlock()
	m = workflowsVerbStep(t, m, keyCtrl('x'))
	if note := lastNote(m); note != "apogee: no background workflow is running" {
		t.Errorf("a refused stop noted %q, want the engine's refusal", note)
	}
}

// In a workflow's detail ^r re-runs its failed items: at idle the launch goes off the loop under the
// /bg latch and folds as a /bg launch does; while a Turn runs it is refused with a note and the
// engine is not reached.
func TestWorkflowsViewCtrlRRerunsTheFailedItems(t *testing.T) {
	t.Parallel()
	if workflowRerunKey != "ctrl+r" || workflowStopKey != "ctrl+x" {
		t.Fatalf("the detail's chords are %q and %q, want ctrl+r and ctrl+x", workflowRerunKey, workflowStopKey)
	}
	m := workflowsPaneModel(t, workflowsAtDetail)
	eng := m.eng.(*fakeEngine)
	id := m.workflowsPane.shown

	pressed, cmd := stepCmd(t, m, keyCtrl('r'))
	if !pressed.bgLaunching {
		t.Error("^r did not latch the launch while the re-run reads the engine")
	}
	for _, msg := range cmdMsgs(cmd) {
		pressed, _ = stepCmd(t, pressed, msg)
	}
	if stops, reruns := eng.workflowActions(); !slices.Equal(reruns, []string{id}) || len(stops) != 0 {
		t.Errorf("reruns = %v, stops = %v; want one re-run of %s and no stop", reruns, stops, id)
	}
	if pressed.bgLaunching {
		t.Error("the re-run's answer did not release the launch latch")
	}
	if note, want := lastNote(pressed), "started "+id+" in the background"; note != want {
		t.Errorf("the re-run noted %q, want %q", note, want)
	}

	busy := m
	busy.state = stateRunning
	busy = workflowsVerbStep(t, busy, keyCtrl('r'))
	if _, reruns := eng.workflowActions(); len(reruns) != 1 {
		t.Errorf("a ^r mid-Turn reached the engine: reruns = %v", reruns)
	}
	if note := lastNote(busy); note != workflowRerunNotIdle {
		t.Errorf("a ^r mid-Turn noted %q, want %q", note, workflowRerunNotIdle)
	}
}

// The verbs are chords, never letters, and only a workflow's detail answers them: a bare r or x does
// nothing there, and ^x and ^r on the list or an item's reading reach no engine call.
func TestWorkflowsViewVerbsAreDetailChordsOnly(t *testing.T) {
	t.Parallel()
	for _, level := range []workflowsLevel{workflowsAtList, workflowsAtDetail, workflowsAtItem} {
		m := workflowsPaneModel(t, level)
		eng := m.eng.(*fakeEngine)
		keys := []tea.KeyPressMsg{keyRune('r'), keyRune('x')}
		if level != workflowsAtDetail {
			keys = append(keys, keyCtrl('x'), keyCtrl('r'))
		}
		for _, key := range keys {
			m = workflowsVerbStep(t, m, key)
		}
		if stops, reruns := eng.workflowActions(); len(stops) != 0 || len(reruns) != 0 {
			t.Errorf("level %d: stops = %v, reruns = %v; want no engine call", level, stops, reruns)
		}
		if !m.workflowsPane.open || m.workflowsPane.level != level || m.bgLaunching {
			t.Errorf("level %d: pane open %v at level %d, latch %v; want it untouched", level, m.workflowsPane.open, m.workflowsPane.level, m.bgLaunching)
		}
	}
}

// ----------------------------------------------------------------------------
// ^s — saving a fan_out's workflow as a recipe skill
// ----------------------------------------------------------------------------

// savePaneModel is one workflow of plan, run for recipe ("" for a fan_out's), open in the view's
// detail, with an apogee home to save into and a catalog that already serves the skill `audit`. It
// returns the home and the count of skill re-scans the save fires.
func savePaneModel(t *testing.T, plan workflow.Plan, recipe string) (Model, string, *int) {
	t.Helper()
	store, err := workflow.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	status, err := store.Create(plan, "hash", time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	status.Recipe = recipe
	dir, err := store.Dir(status.ID)
	if err != nil {
		t.Fatal(err)
	}
	opts, reloads := reloadOpts()
	opts.ConfigHome = t.TempDir()
	current := []skills.Skill{{ID: "audit", DisplayName: "Audit", Summary: "audit", Body: "AUDIT"}}
	opts.Skills = reloadableCatalog{skills: &current}
	eng := &fakeEngine{workflowInfos: []workflow.Info{{Status: status, Dir: dir}}}
	m := openWorkflowsLine(t, newTestModelEng(t, eng, opts))
	m = workflowsKeyStep(t, m, keyEnter())
	if m.workflowsPane.level != workflowsAtDetail {
		t.Fatalf("precondition: the view is at level %d, want the detail", m.workflowsPane.level)
	}
	m.layout()
	return m, opts.ConfigHome, reloads
}

// saveFanOutPlan is a plan of a fan_out's shape, its items a files glob.
func saveFanOutPlan() workflow.Plan {
	return workflow.Plan{Name: "scan the handlers", Stages: []workflow.Stage{
		{Name: "fanout", Kind: workflow.StageFanout, Task: "scan {item}", Over: &workflow.ItemSource{Files: "internal/**/*.go"}},
		{Name: "merge", Kind: workflow.StageMerge, Task: "merge the findings"},
	}}
}

// typeName types name into the open name row a key at a time and presses ⏎, running the save the
// press starts and every Cmd its fold hands back.
func typeName(t *testing.T, m Model, name string) Model {
	t.Helper()
	for _, r := range name {
		m = step(t, m, keyRune(r))
	}
	m, cmd := stepCmd(t, m, keyEnter())
	for cmd != nil {
		var next tea.Cmd
		for _, msg := range cmdMsgs(cmd) {
			var more tea.Cmd
			m, more = stepCmd(t, m, msg)
			next = tea.Batch(next, more)
		}
		cmd = next
	}
	return m
}

// errorTexts is every error entry of m's transcript, in order.
func errorTexts(m Model) []string {
	var out []string
	for _, e := range m.transcript.entries {
		if e.kind == entryError {
			out = append(out, e.text)
		}
	}
	return out
}

// ^s in a fan_out's detail opens the name row; the name typed into it — a bare s included — is
// the skill the workflow is saved as: <home>/skills/<name>/SKILL.md, a recipe of the workflow's
// stages with its files glob as the scope input, loadable at once through the re-scan the save fires.
func TestWorkflowsViewCtrlSSavesAFanOutAsARecipe(t *testing.T) {
	t.Parallel()
	if workflowSaveKey != "ctrl+s" {
		t.Fatalf("the save chord is %q, want ctrl+s", workflowSaveKey)
	}
	plan := saveFanOutPlan()
	m, home, reloads := savePaneModel(t, plan, "")

	m = step(t, m, keyCtrlS())
	if !m.workflowsPane.naming {
		t.Fatal("^s did not open the name row")
	}
	m = step(t, m, keyRune('s'))
	assertPaneHas(t, m, workflowSavePrompt+"s", workflowsNamingHint)
	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = typeName(t, m, "scan-handlers")

	if m.workflowsPane.naming || !m.workflowsPane.open || m.workflowsPane.level != workflowsAtDetail {
		t.Errorf("after the save: naming %v, open %v, level %d; want the detail back", m.workflowsPane.naming, m.workflowsPane.open, m.workflowsPane.level)
	}
	dir := filepath.Join(home, "skills", "scan-handlers")
	if note, want := lastNote(m), `recipe "scan-handlers" written to `+dir+" — /scan-handlers runs it"; note != want {
		t.Errorf("the save noted %q, want %q (errors: %v)", note, want, errorTexts(m))
	}
	if *reloads != 1 {
		t.Errorf("the save fired %d skill re-scans, want 1", *reloads)
	}
	catalog, _ := skills.Load(skills.Sources{Home: home})
	skill, ok := catalog.Get("scan-handlers")
	if !ok || skill.Recipe == nil {
		t.Fatal("the saved skill does not load as a recipe")
	}
	if got := skill.Recipe.Stages; len(got) != 2 || got[0].Over == nil || got[0].Over.Files != "{scope}" ||
		got[0].Task != plan.Stages[0].Task || got[1].Task != plan.Stages[1].Task {
		t.Errorf("the saved recipe runs %+v; want the plan's stages over {scope}", got)
	}
	if len(skill.Inputs) != 1 || skill.Inputs[0].Default != "internal/**/*.go" {
		t.Errorf("the saved recipe declares %+v; want scope defaulting to the glob", skill.Inputs)
	}
}

// A bare s in the detail is no verb: it opens no name row and writes nothing. esc closes an open
// name row and saves nothing.
func TestWorkflowsViewBareSOrEscSavesNothing(t *testing.T) {
	t.Parallel()
	m, home, _ := savePaneModel(t, saveFanOutPlan(), "")

	m = step(t, m, keyRune('s'))
	if m.workflowsPane.naming {
		t.Error("a bare s opened the name row; the verb is the ^s chord")
	}
	m = step(t, m, keyCtrlS())
	for _, r := range "kept" {
		m = step(t, m, keyRune(r))
	}
	m = step(t, m, keyEsc())
	if m.workflowsPane.naming || m.workflowsPane.level != workflowsAtDetail {
		t.Errorf("esc: naming %v at level %d; want the name row closed on the detail", m.workflowsPane.naming, m.workflowsPane.level)
	}
	if _, err := os.Stat(filepath.Join(home, "skills")); !os.IsNotExist(err) {
		t.Errorf("nothing was saved, yet the library exists: %v", err)
	}
}

// A name that is a path or carries a space, one a skill or a command already answers to, and one a
// folder in the library already holds are each refused with an error entry, and nothing is
// overwritten.
func TestWorkflowsViewSaveRefusesATakenOrMalformedName(t *testing.T) {
	t.Parallel()
	m, home, _ := savePaneModel(t, saveFanOutPlan(), "")
	existing := filepath.Join(home, "skills", "mine", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(existing), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, want string }{
		{"../x", "not a skill name"},
		{"two words", "not a skill name"},
		{"audit", `a skill is already named "audit"`},
		{"workflows", `a command is already named "workflows"`},
		{"mine", "already exists"},
	} {
		m = step(t, m, keyCtrlS())
		m = typeName(t, m, tc.name)
		errs := errorTexts(m)
		if len(errs) == 0 || !strings.Contains(errs[len(errs)-1], tc.want) {
			t.Errorf("saving as %q: errors %v, want the last to say %q", tc.name, errs, tc.want)
		}
	}
	if data, _ := os.ReadFile(existing); string(data) != "edited" {
		t.Errorf("the existing skill now holds %q; it was overwritten", data)
	}
	if _, err := os.Stat(filepath.Join(home, "x")); !os.IsNotExist(err) {
		t.Errorf("../x reached outside the library: %v", err)
	}
	for _, name := range []string{"audit", "workflows", "two words"} {
		if _, err := os.Stat(filepath.Join(home, "skills", name)); !os.IsNotExist(err) {
			t.Errorf("a refused %q left a folder: %v", name, err)
		}
	}
}

// A recipe's workflow is already a recipe: ^s says which and opens no name row.
func TestWorkflowsViewSaveRefusesARecipesWorkflow(t *testing.T) {
	t.Parallel()
	m, _, _ := savePaneModel(t, saveFanOutPlan(), "audit")

	m = step(t, m, keyCtrlS())
	if m.workflowsPane.naming {
		t.Error("^s opened the name row on a recipe's workflow")
	}
	if note, want := lastNote(m), fmt.Sprintf(workflowSaveRecipe, "audit"); note != want {
		t.Errorf("^s noted %q, want %q", note, want)
	}
}
