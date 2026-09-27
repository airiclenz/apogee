package tui

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/agent"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/workflow"
)

// ----------------------------------------------------------------------------
// The recipe line (ADR 0087 D6) — a leading "/<recipe-skill>" launches, it never attaches
// ----------------------------------------------------------------------------

// testWorkflowID is the Workflow id the block tests fold their phases under.
const testWorkflowID = "20260927-101500-ab12"

// recipeOpts is skillOpts plus "audit", a skill that carries a Recipe.
func recipeOpts() Options {
	o := testOpts
	o.Skills = fakeSkillCatalog{skills: []skills.Skill{
		{ID: "review", DisplayName: "Review", Summary: "review a diff", Body: "REVIEW IT"},
		{ID: "audit", DisplayName: "Audit", Summary: "audit a tree", Body: "AUDIT IT",
			Recipe: &workflow.Plan{Name: "audit"}},
	}}
	return o
}

// A line opening with a recipe skill is marked as that recipe's launch: its id leaves skillIDs (the
// body is never attached) and the send spells the launch the engine keys on — the id FIRST in
// SkillIDs, the text opening with its token — with the line's other skills riding behind it.
func TestRecipeLineSubmitsAsALaunch(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, recipeOpts())
	m.input.SetValue("/audit internal/ /review")

	parsed := m.submitLine()
	if parsed.recipe != "audit" || !reflect.DeepEqual(parsed.skillIDs, []string{"review"}) {
		t.Fatalf("parsed recipe = %q, skillIDs = %v; want the launch marked and its id out of skillIDs", parsed.recipe, parsed.skillIDs)
	}

	m, cmd := stepCmd(t, m, keyEnter())
	if m.state != stateRunning {
		t.Fatalf("state = %v, want running", m.state)
	}
	drainCmd(t, m, cmd)
	if len(eng.submitted) != 1 {
		t.Fatalf("Submit calls = %d, want 1", len(eng.submitted))
	}
	in := eng.submitted[0]
	if in.Text != "/audit internal/ /review" || !reflect.DeepEqual(in.SkillIDs, []string{"audit", "review"}) {
		t.Errorf("submitted %+v; want the text as typed and SkillIDs [audit review]", in)
	}
}

// Everything that is not a leading recipe token is left as it was: a recipe named later in the line
// and a skill with no recipe both attach their bodies, and parseInput itself marks nothing.
func TestOnlyALeadingRecipeTokenIsALaunch(t *testing.T) {
	t.Parallel()
	m := newTestModelEng(t, &fakeEngine{}, recipeOpts())
	for _, tc := range []struct {
		line string
		ids  []string
	}{
		{line: "please /audit this", ids: []string{"audit"}},
		{line: "/review the diff", ids: []string{"review"}},
	} {
		m.input.SetValue(tc.line)
		parsed := m.submitLine()
		if parsed.recipe != "" || !reflect.DeepEqual(parsed.userInput().SkillIDs, tc.ids) {
			t.Errorf("%q: recipe = %q, SkillIDs = %v; want no launch and %v attached", tc.line, parsed.recipe, parsed.userInput().SkillIDs, tc.ids)
		}
	}
	if parsed := parseInput("/audit internal/", m.knownSkillID); parsed.recipe != "" {
		t.Errorf("parseInput marked a recipe (%q); the mark is the send path's alone", parsed.recipe)
	}
}

// A recipe line sent while rows are held is refused, never merged behind them — seated after the
// rows it would no longer lead, and would attach instead of launching. Nothing is sent and the line
// and the rows stay exactly where they were.
func TestRecipeLineWithHeldRowsIsRefused(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, recipeOpts())
	m.pendingInterjections = []queuedInterjection{staged(1, "held row")}

	m = stageRow(t, m, "/audit internal/")

	if len(eng.submitted) != 0 || m.state != stateIdle {
		t.Fatalf("submitted %v in state %v; want nothing sent", eng.submitted, m.state)
	}
	if got := m.input.Value(); got != "/audit internal/" {
		t.Errorf("input = %q; want the recipe line left in the box", got)
	}
	if n := len(m.pendingInterjections); n != 1 {
		t.Errorf("held rows = %d; want the one row still held", n)
	}
	assertLastNote(t, m, recipeHeldNote("audit"))
}

// A recipe line typed while a worker runs is refused, never staged: the engine would receive it as
// an interjection, which cannot launch a Workflow.
func TestRecipeLineWhileRunningIsRefused(t *testing.T) {
	t.Parallel()
	m := newTestModelEng(t, &fakeEngine{}, recipeOpts())
	startStubWorker(t, &m)

	m = stageRow(t, m, "/audit internal/")

	if n := len(m.pendingInterjections); n != 0 {
		t.Fatalf("staged rows = %d; want the recipe line refused, not staged", n)
	}
	if m.worker.box.pending() {
		t.Error("the mailbox holds a row; want nothing pushed to the worker")
	}
	if got := m.input.Value(); got != "/audit internal/" {
		t.Errorf("input = %q; want the recipe line left in the box", got)
	}
	assertLastNote(t, m, recipeBusyNote("audit"))
}

// A recipe line typed in a running child's view is refused too: the child is inside an Exchange, and
// a delegate never launches a Workflow.
func TestRecipeLineToAChildIsRefused(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, recipeOpts())
	m.input.SetValue("survey the repo")
	m, _ = stepCmd(t, m, keyEnter())
	run := stampedDelegation(&m.transcript, runRef{}, "s1", "run-s1", "repo-scout")
	stampedPhase(&m.transcript, run, domain.SubAgentStarted, "")
	m.refreshViewport()
	m = enterOnLastBlock(t, m)
	if !m.inRunView() {
		t.Fatal("setup: no run view is open")
	}

	m = stageRow(t, m, "/audit internal/")

	if len(eng.childInterjected) != 0 {
		t.Fatalf("child interjections = %v; want the recipe line refused", eng.childInterjected)
	}
	if got := m.input.Value(); got != "/audit internal/" {
		t.Errorf("input = %q; want the recipe line left in the box", got)
	}
	assertLastNote(t, m, recipeBusyNote("audit"))
}

// assertLastNote fails unless the transcript's newest note reads want.
func assertLastNote(t *testing.T, m Model, want string) {
	t.Helper()
	for i := len(m.transcript.entries) - 1; i >= 0; i-- {
		if e := m.transcript.entries[i]; e.kind == entryNote {
			if e.text != want {
				t.Errorf("last note = %q, want %q", e.text, want)
			}
			return
		}
	}
	t.Errorf("no note in the transcript; want %q", want)
}

// ----------------------------------------------------------------------------
// The workflow block
// ----------------------------------------------------------------------------

// workflowPhase is one of testWorkflowID's phases, emitted by the human's own agent.
func workflowPhase(phase domain.WorkflowPhase) domain.WorkflowPhaseEvent {
	return domain.WorkflowPhaseEvent{Phase: phase, Workflow: testWorkflowID, Name: "audit"}
}

// itemFinished is one item of the "items" stage finishing on a receipt.
func itemFinished(index int, item, status, summary string, fields map[string]string) domain.WorkflowPhaseEvent {
	e := workflowPhase(domain.WorkflowItemFinished)
	e.Stage, e.Item, e.Index = "items", item, index
	e.Receipt = domain.WorkflowReceipt{Status: status, Summary: summary, Fields: fields}
	return e
}

// workflowEntries returns the transcript's workflow blocks.
func workflowEntries(m Model) []entry {
	var out []entry
	for _, e := range m.transcript.entries {
		if e.kind == entryWorkflow {
			out = append(out, e)
		}
	}
	return out
}

// A Workflow's phases grow ONE block in place: the stage running, each item's result line as it
// finishes, an `ask` stage's question while it waits, then the end and the totals.
func TestWorkflowBlockShowsProgressAndResultLines(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	stage := workflowPhase(domain.WorkflowStageStarted)
	stage.Stage = "items"
	for _, e := range []domain.Event{
		workflowPhase(domain.WorkflowStarted),
		stage,
		itemFinished(0, "alpha", "ok", "alpha is fine", map[string]string{"count": "3", "note": "two words"}),
	} {
		m.transcript.apply(e)
	}

	live := plainTranscript(m)
	for _, want := range []string{
		"Workflow audit — running",
		"stage: items",
		`#1 alpha — ok — alpha is fine count=3 note="two words"`,
		"items 1 · ok 1 · partial 0 · blocked 0",
	} {
		if !strings.Contains(live, want) {
			t.Errorf("live block missing %q:\n%s", want, live)
		}
	}

	waiting := workflowPhase(domain.WorkflowWaiting)
	waiting.Detail = "which findings matter?"
	m.transcript.apply(waiting)
	if got := plainTranscript(m); !strings.Contains(got, "Workflow audit — waiting for you") ||
		!strings.Contains(got, "waiting for your answer: which findings matter?") {
		t.Errorf("waiting block does not show the question:\n%s", got)
	}

	m.transcript.apply(itemFinished(1, "beta", "blocked", "beta could not be read", nil))
	m.transcript.apply(workflowPhase(domain.WorkflowFinished))

	blocks := workflowEntries(m)
	if len(blocks) != 1 {
		t.Fatalf("workflow blocks = %d, want one grown in place", len(blocks))
	}
	if !blocks[0].done {
		t.Error("the finished block is not done")
	}
	ended := plainTranscript(m)
	for _, want := range []string{
		"Workflow audit — finished",
		"#2 beta — blocked — beta could not be read",
		"items 2 · ok 1 · partial 0 · blocked 1",
	} {
		if !strings.Contains(ended, want) {
			t.Errorf("finished block missing %q:\n%s", want, ended)
		}
	}
	for _, gone := range []string{"stage: items", "waiting for your answer"} {
		if strings.Contains(ended, gone) {
			t.Errorf("finished block still shows %q:\n%s", gone, ended)
		}
	}
}

// A failed Workflow says so and names its cause; a stopped one says it stopped.
func TestWorkflowBlockNamesHowItEnded(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		phase  domain.WorkflowPhase
		detail string
		want   []string
	}{
		{phase: domain.WorkflowStopped, want: []string{"Workflow audit — stopped"}},
		{phase: domain.WorkflowFailed, detail: "disk full\nmore", want: []string{"Workflow audit — failed", "failed: disk full"}},
	} {
		m := newTestModel(t)
		m.transcript.apply(workflowPhase(domain.WorkflowStarted))
		end := workflowPhase(tc.phase)
		end.Detail = tc.detail
		m.transcript.apply(end)
		got := plainTranscript(m)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: block missing %q:\n%s", tc.phase, want, got)
			}
		}
	}
}

// Past forty items only the items that did not end ok are listed, as the result lines list them;
// the totals still count every item.
func TestWorkflowBlockListsOnlyTroubleOnALargeRun(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.transcript.apply(workflowPhase(domain.WorkflowStarted))
	for i := range workflowListedItems {
		m.transcript.apply(itemFinished(i, "file"+strconv.Itoa(i), "ok", "fine", nil))
	}
	m.transcript.apply(itemFinished(workflowListedItems, "broken", "partial", "half done", nil))

	text := workflowEntries(m)[0].text
	if strings.Contains(text, "#1 file0") {
		t.Errorf("an ok item is still listed past the cap:\n%s", text)
	}
	if !strings.Contains(text, "#41 broken — partial — half done") {
		t.Errorf("the partial item is not listed:\n%s", text)
	}
	if !strings.Contains(text, "items 41 · ok 40 · partial 1 · blocked 0") {
		t.Errorf("the totals do not count every item:\n%s", text)
	}
}

// A Workflow a fan_out call started is that call's block's: its phases draw no workflow block.
func TestFanOutWorkflowDrawsNoBlock(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "f1", Tool: fanOutToolName, Arguments: []byte(`{"task":"check {item}"}`)}})
	m.transcript.apply(workflowPhase(domain.WorkflowStarted))
	m.transcript.apply(itemFinished(0, "alpha", "ok", "fine", nil))

	if n := len(workflowEntries(m)); n != 0 {
		t.Errorf("workflow blocks = %d; want none beside the fan_out call", n)
	}
}

// The block survives the session record: its text is its record, so the replayed block paints as
// the live one last stood — and, carrying no view, no later event reaches it.
func TestWorkflowBlockSurvivesTheRecord(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.transcript.apply(workflowPhase(domain.WorkflowStarted))
	m.transcript.apply(itemFinished(0, "alpha", "ok", "alpha is fine", nil))
	m.transcript.apply(workflowPhase(domain.WorkflowFinished))
	live := workflowPaint(m)
	if live == "" {
		t.Fatalf("the live transcript paints no workflow block:\n%s", plainTranscript(m))
	}

	data, err := encodeTranscript(&m.transcript)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	entries, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	replayed := newTestModel(t)
	replayed.transcript.entries = entries
	replayed.transcript.touch()
	if got := workflowPaint(replayed); got != live {
		t.Errorf("replayed block paints differently:\nlive:\n%s\nreplayed:\n%s", live, got)
	}
	if got := workflowEntries(replayed); len(got) != 1 || got[0].workflow.id != "" {
		t.Fatalf("replayed workflow blocks = %+v; want one carrying no live view", got)
	}
	replayed.transcript.apply(itemFinished(1, "beta", "ok", "late", nil))
	if got := workflowPaint(replayed); got != live {
		t.Errorf("a late event moved a replayed block:\n%s", got)
	}
}

// workflowPaint is the plain transcript from the workflow block's header on — what the block paints,
// without the start-up box a fresh Model opens with.
func workflowPaint(m Model) string {
	_, block, _ := strings.Cut(plainTranscript(m), glyphAssistant+" "+workflowTitle)
	return block
}

// ----------------------------------------------------------------------------
// End to end: a missing required input opens the ask pane
// ----------------------------------------------------------------------------

// recipeSkill is a recipe skill whose one input is required, so a bare "/review-tree" leaves it
// unbound and the engine asks the human for it.
const recipeSkill = "---\nid: review-tree\nsummary: review a tree\n" +
	"inputs:\n  - name: scope\n    required: true\n    description: the folder to review\n" +
	"recipe:\n  - name: items\n    kind: fanout\n    over:\n      list: [alpha, beta]\n    task: check {item} in {scope}\n" +
	"---\nReview the tree."

// TestE2ERecipeMissingInputOpensTheAskPane drives the whole chain through the real seam: the line
// the TUI parses launches the recipe in a real Agent, whose missing required input is put to the
// human through the TUI's own Asker — the ask pane — and whose empty answer reaches the model as
// the launch's refusal line.
func TestE2ERecipeMissingInputOpensTheAskPane(t *testing.T) {
	t.Parallel()
	home, workspace := t.TempDir(), t.TempDir()
	dir := filepath.Join(home, "skills", "review-tree")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(recipeSkill), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, err := skills.Load(skills.Sources{Home: home})
	if err != nil {
		t.Fatalf("skills.Load: %v", err)
	}
	srv := stubllm.New(t, stubllm.Script{Model: "test-model", Turns: []stubllm.Turn{
		{When: &stubllm.Match{LastMessage: `recipe /review-tree could not run: missing input: scope`}, Text: "The review needs a folder."},
	}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge := NewBridge()
	h := newUIHarness()
	bridge.Bind(h)
	eng, err := agent.New(domain.Config{
		Endpoint:     srv.URL,
		Model:        "test-model",
		Mode:         domain.ModeAskBefore,
		Events:       bridge.Sink(),
		Approver:     bridge.Approver(),
		Asker:        bridge.Asker(),
		Skills:       catalog,
		Tools:        tools.NewDefaultRegistry(workspace),
		WorkspaceDir: workspace,
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	opts := e2eOptions(srv.URL, workspace)
	opts.Skills = catalog
	m := step(t, newModel(ctx, eng, opts, nil), tea.WindowSizeMsg{Width: 100, Height: 30})
	m.input.SetValue("/review-tree")
	parsed := m.submitLine()
	if parsed.recipe != "review-tree" {
		t.Fatalf("the line is not a recipe launch: %+v", parsed)
	}

	in := parsed.userInput()
	m.transcript.addUser(in.Text, parsed.skillSpans)
	cmd, stop := startExchange(ctx, eng, in, nil, nil, nil)
	defer stop(nil)
	m.worker.start(stop, nil)
	m.state = stateRunning
	m.refreshViewport()
	go func() { h.Send(cmd()) }()

	asked := ""
	var term tea.Msg
	for term == nil {
		msg := <-h.inbox
		m, _ = stepCmd(t, m, msg)
		switch msg := msg.(type) {
		case askReqMsg:
			if m.state != stateAwaitingAsk {
				t.Fatalf("after askReqMsg state = %v, want the ask pane open", m.state)
			}
			asked = msg.Request.Question
			m = step(t, m, approvalArmedMsg{seq: m.approvalSeq})
			m = step(t, m, keyEnter()) // an empty answer leaves the input unbound
		case exchangeDoneMsg, cancelledMsg, errMsg:
			term = msg
		}
	}

	if want := "/review-tree needs scope: the folder to review"; asked != want {
		t.Errorf("question = %q, want %q", asked, want)
	}
	if done, ok := term.(exchangeDoneMsg); !ok || done.Result.Status != domain.StatusExchangeComplete {
		t.Fatalf("terminal Msg = %#v; want the Exchange to complete on the refusal line", term)
	}
	if got := plainTranscript(m); !strings.Contains(got, "The review needs a folder.") {
		t.Errorf("the model's answer to the refusal is missing:\n%s", got)
	}
}
