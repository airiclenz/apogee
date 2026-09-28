package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
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
