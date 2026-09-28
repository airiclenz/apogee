package tui

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

// itemFinished is one item of the "items" stage finishing on a receipt, in the stage's first round.
func itemFinished(index int, item, status, summary string, fields map[string]string) domain.WorkflowPhaseEvent {
	e := workflowPhase(domain.WorkflowItemFinished)
	e.Stage, e.Item, e.Index, e.Round = "items", item, index, 1
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

// startedWith is testWorkflowID's started phase naming the Plan's stages.
func startedWith(stages ...string) domain.WorkflowPhaseEvent {
	e := workflowPhase(domain.WorkflowStarted)
	e.Stages = stages
	return e
}

// stageStarted is one stage of testWorkflowID starting in round, with its item count and — for a
// stage a repeat re-runs — the most rounds it can run.
func stageStarted(stage string, round, items, rounds int) domain.WorkflowPhaseEvent {
	e := workflowPhase(domain.WorkflowStageStarted)
	e.Stage, e.Round, e.Items, e.Rounds = stage, round, items, rounds
	return e
}

// stageFinished is one stage of testWorkflowID ending in round.
func stageFinished(stage string, round int) domain.WorkflowPhaseEvent {
	e := workflowPhase(domain.WorkflowStageFinished)
	e.Stage, e.Round = stage, round
	return e
}

// stageItemStarted is the index'th item of stage starting its first attempt in round, naming no run.
func stageItemStarted(stage string, round, index int) domain.WorkflowPhaseEvent {
	e := workflowPhase(domain.WorkflowItemStarted)
	e.Stage, e.Item, e.Index, e.Round, e.Attempt = stage, "item"+strconv.Itoa(index), index, round, 1
	return e
}

// stageItemFinished is the index'th item of stage ending in round on a receipt of status.
func stageItemFinished(stage string, round, index int, status string) domain.WorkflowPhaseEvent {
	e := itemFinished(index, "item"+strconv.Itoa(index), status, "item"+strconv.Itoa(index)+" says so", nil)
	e.Stage, e.Round = stage, round
	return e
}

// waitingIn is an `ask` stage of testWorkflowID putting question to the human.
func waitingIn(stage, question string) domain.WorkflowPhaseEvent {
	e := workflowPhase(domain.WorkflowWaiting)
	e.Stage, e.Detail = stage, question
	return e
}

// stageRowIn is the painted row of the stage named name — the line its branch marker leads — or ""
// when the paint has none.
func stageRowIn(painted, name string) string {
	for _, ln := range strings.Split(painted, "\n") {
		if strings.Contains(ln, glyphBranch+" "+name+" ") || strings.Contains(ln, glyphBranchLast+" "+name+" ") {
			return ln
		}
	}
	return ""
}

// stageSlot is a painted stage row's outcome slot and whether the row wears ▶ and ✓.
func stageSlot(row string) (slot string, opens, done bool) {
	opens = strings.Contains(row, glyphCollapsed)
	done = strings.Contains(row, " "+glyphDone+" ")
	rest := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(row), glyphCollapsed))
	_, slot, _ = strings.Cut(rest, glyphLeaderDot+" ")
	return slot, opens, done
}

// assertStageRow fails unless the paint's row of stage reads slot in its outcome slot, wearing ▶
// only when opens and ✓ only when done.
func assertStageRow(t *testing.T, painted, stage, slot string, opens, done bool) {
	t.Helper()
	row := stageRowIn(painted, stage)
	if row == "" {
		t.Fatalf("no row for stage %q:\n%s", stage, painted)
	}
	gotSlot, gotOpens, gotDone := stageSlot(row)
	if gotSlot != slot || gotOpens != opens || gotDone != done {
		t.Errorf("stage %q row %q = slot %q, ▶ %v, ✓ %v; want %q, %v, %v",
			stage, row, gotSlot, gotOpens, gotDone, slot, opens, done)
	}
}

// A Workflow's phases grow ONE block in place: a row per stage of its Plan from the start, a line for
// each item whose receipt is not ok, an `ask` stage's question while it waits, then the end and the
// totals. An ok item gets no line of its own, and the stage running is its row, not a line.
func TestWorkflowBlockShowsProgressAndResultLines(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	for _, e := range []domain.Event{
		startedWith("items", "ask"),
		stageStarted("items", 1, 2, 0),
		itemFinished(0, "alpha", "ok", "alpha is fine", map[string]string{"count": "3"}),
	} {
		m.transcript.apply(e)
	}

	live := plainTranscript(m)
	if !strings.Contains(live, "Workflow audit — running") {
		t.Errorf("live block missing its header:\n%s", live)
	}
	assertStageRow(t, live, "items", "1/2 · running", false, false)
	assertStageRow(t, live, "ask", "pending", false, false)
	for _, gone := range []string{"stage: items", "alpha", "items 1 · ok 1"} {
		if strings.Contains(live, gone) {
			t.Errorf("live block shows %q:\n%s", gone, live)
		}
	}

	m.transcript.apply(itemFinished(1, "beta", "blocked", "beta could not be read", nil))
	m.transcript.apply(stageFinished("items", 1))
	m.transcript.apply(stageStarted("ask", 1, 0, 0))
	m.transcript.apply(waitingIn("ask", "which findings matter?"))
	waiting := plainTranscript(m)
	if !strings.Contains(waiting, "Workflow audit — waiting for you") ||
		!strings.Contains(waiting, "waiting for your answer: which findings matter?") {
		t.Errorf("waiting block does not show the question:\n%s", waiting)
	}
	assertStageRow(t, waiting, "ask", "waiting for you", false, false)

	m.transcript.apply(stageFinished("ask", 1))
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
		"items · beta — blocked — beta could not be read",
		"items 2 · ok 1 · partial 0 · blocked 1",
	} {
		if !strings.Contains(ended, want) {
			t.Errorf("finished block missing %q:\n%s", want, ended)
		}
	}
	assertStageRow(t, ended, "items", "done", false, false)
	assertStageRow(t, ended, "ask", "done", false, true)
	for _, gone := range []string{"stage: items", "waiting for your answer", "alpha"} {
		if strings.Contains(ended, gone) {
			t.Errorf("finished block still shows %q:\n%s", gone, ended)
		}
	}
}

// Each stage row says where its stage stands in the round it shows: pending until it starts, its
// item count while it runs, waiting on the human, done — ✓ when every item ended ok — or the
// failed or stopped word; ▶ once one of its item runs has started, never on a pending row.
func TestWorkflowStageRowsSayWhereEachStageStands(t *testing.T) {
	t.Parallel()
	running := []domain.Event{
		startedWith("scan", "fix"),
		stageStarted("scan", 1, 3, 0),
		stageItemStarted("scan", 1, 0),
		stageItemStarted("scan", 1, 1),
		stageItemStarted("scan", 1, 2),
		stageItemFinished("scan", 1, 0, "ok"),
		stageItemFinished("scan", 1, 1, "ok"),
	}
	for _, tc := range []struct {
		name   string
		events []domain.Event
		stage  string
		slot   string
		opens  bool
		done   bool
	}{
		{name: "a stage not started is pending", events: running, stage: "fix", slot: "pending"},
		{name: "a running stage counts its items", events: running, stage: "scan", slot: "2/3 · running", opens: true},
		{
			name:   "a stage of one item runs uncounted",
			events: []domain.Event{startedWith("scan"), stageStarted("scan", 1, 1, 0), stageItemStarted("scan", 1, 0)},
			stage:  "scan", slot: "running", opens: true,
		},
		{
			name:   "a stage whose items all ended ok is done with the mark",
			events: slices.Concat(running, []domain.Event{stageItemFinished("scan", 1, 2, "ok"), stageFinished("scan", 1)}),
			stage:  "scan", slot: "done", opens: true, done: true,
		},
		{
			name:   "a stage with an item not ok is done without the mark",
			events: slices.Concat(running, []domain.Event{stageItemFinished("scan", 1, 2, "partial"), stageFinished("scan", 1)}),
			stage:  "scan", slot: "done", opens: true,
		},
		{
			name:   "a stage the Workflow failed in is failed",
			events: slices.Concat(running, []domain.Event{workflowPhase(domain.WorkflowFailed)}),
			stage:  "scan", slot: "failed", opens: true,
		},
		{
			name:   "a stage a cancel left unfinished is stopped",
			events: slices.Concat(running, []domain.Event{stageFinished("scan", 1), workflowPhase(domain.WorkflowStopped)}),
			stage:  "scan", slot: "stopped", opens: true,
		},
		{
			name:   "an ask stage waits for the human",
			events: []domain.Event{startedWith("ask", "scan"), stageStarted("ask", 1, 0, 0), waitingIn("ask", "which?")},
			stage:  "ask", slot: "waiting for you",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tr := &transcript{}
			for _, e := range tc.events {
				tr.apply(e)
			}

			painted := plainRender(tr)

			assertStageRow(t, painted, tc.stage, tc.slot, tc.opens, tc.done)
		})
	}
}

// A stage a repeat re-runs keeps ONE row, which moves on to each round as it starts and reads
// `round n/m` from the round's most rounds — `round n` when they are not known — and a late phase of
// an earlier round folds nothing onto it.
func TestARepeatedStageKeepsOneRow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		rounds int
		slot   string
	}{
		{rounds: 3, slot: "round 2/3 · running"},
		{rounds: 0, slot: "round 2 · running"},
	} {
		tr := &transcript{}
		for _, e := range []domain.Event{
			startedWith("fix", "check", "again"),
			stageStarted("fix", 1, 1, tc.rounds),
			stageItemStarted("fix", 1, 0),
			stageItemFinished("fix", 1, 0, "partial"),
			stageFinished("fix", 1),
			stageStarted("fix", 2, 1, tc.rounds),
			stageItemStarted("fix", 2, 0),
		} {
			tr.apply(e)
		}

		painted := plainRender(tr)
		assertStageRow(t, painted, "fix", tc.slot, true, false)
		if n := strings.Count(painted, glyphBranch+" fix ") + strings.Count(painted, glyphBranchLast+" fix "); n != 1 {
			t.Errorf("rounds %d: the repeated stage paints %d rows, want one:\n%s", tc.rounds, n, painted)
		}

		tr.apply(stageFinished("fix", 1))
		assertStageRow(t, plainRender(tr), "fix", tc.slot, true, false)
	}
}

// A failed Workflow says so, names its cause and fails the stage it was in; a stopped one says it
// stopped, and so does the stage it stopped in.
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
		m.transcript.apply(startedWith("items"))
		m.transcript.apply(stageStarted("items", 1, 2, 0))
		end := workflowPhase(tc.phase)
		end.Detail = tc.detail
		m.transcript.apply(end)
		got := plainTranscript(m)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: block missing %q:\n%s", tc.phase, want, got)
			}
		}
		assertStageRow(t, got, "items", string(tc.phase), false, false)
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

// The block survives the session record whole: the replayed block carries its view's structure —
// though no Workflow id, so it is not live — and paints the same stage rows, trouble lines and totals
// the live one painted. No later event reaches it.
func TestWorkflowBlockSurvivesTheRecord(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.transcript.apply(startedWith("items", "check"))
	m.transcript.apply(stageStarted("items", 1, 2, 0))
	m.transcript.apply(itemFinished(0, "alpha", "ok", "alpha is fine", nil))
	m.transcript.apply(itemFinished(1, "beta", "partial", "beta is half done", nil))
	m.transcript.apply(stageFinished("items", 1))
	m.transcript.apply(workflowPhase(domain.WorkflowFinished))
	live := workflowPaint(m)
	if stageRowIn(live, "items") == "" || stageRowIn(live, "check") == "" {
		t.Fatalf("the live block paints no stage rows:\n%s", plainTranscript(m))
	}

	replayed := newTestModel(t)
	replayed.transcript.entries = roundTrip(t, &m.transcript)
	replayed.transcript.touch()

	if got := workflowEntries(replayed); len(got) != 1 || got[0].workflow.live() || !got[0].workflow.drawsStages() {
		t.Fatalf("replayed workflow blocks = %+v; want one carrying a replayed view", got)
	}
	painted := workflowPaint(replayed)
	if painted != live {
		t.Errorf("the replayed block paints\n%s\nwhere the live one painted\n%s", painted, live)
	}
	for _, line := range []string{"items · beta — partial — beta is half done", "items 2 · ok 1 · partial 1 · blocked 0"} {
		if !strings.Contains(painted, line) {
			t.Errorf("the replayed block does not paint %q:\n%s", line, painted)
		}
	}
	replayed.transcript.apply(itemFinished(2, "gamma", "ok", "late", nil))
	if got := workflowPaint(replayed); got != painted {
		t.Errorf("a late event moved a replayed block:\n%s", got)
	}
}

// A block replayed from a record written before the structure was kept comes back as its text: it
// carries no view, paints the item lines and totals its record's text holds, and no stage row.
func TestWorkflowBlockFromAnOlderRecordPaintsItsText(t *testing.T) {
	t.Parallel()
	const text = "Workflow audit — finished\n#1 alpha — ok — alpha is fine\nitems 1 · ok 1 · partial 0 · blocked 0"
	blob := `{"version":1,"entries":[{"kind":"workflow","callID":"w1","done":true,"text":` + strconv.Quote(text) + `}]}`

	entries, err := decodeTranscript([]byte(blob))

	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	m := newTestModel(t)
	m.transcript.entries = entries
	m.transcript.touch()
	if got := workflowEntries(m); len(got) != 1 || got[0].workflow.drawsStages() {
		t.Fatalf("replayed workflow blocks = %+v; want one carrying no view", got)
	}
	painted := workflowPaint(m)
	for _, line := range strings.Split(text, "\n")[1:] {
		if !strings.Contains(painted, line) {
			t.Errorf("the replayed block does not paint its record's line %q:\n%s", line, painted)
		}
	}
	if row := stageRowIn(painted, "items"); row != "" {
		t.Errorf("the replayed block paints a stage row %q with no view to paint it from", row)
	}
}

// A record whose end is not one of the three ends a Workflow comes to — an escape sequence planted
// in the file — replays as a stopped block: the recorded string never reaches the header as text.
func TestWorkflowBlockFromARecordWithAForeignEndPaintsItStopped(t *testing.T) {
	t.Parallel()
	const planted = `"\u001b]0;pwned\u0007\u001b[31mowned"` // JSON-escaped, as a record file carries it
	blob := `{"version":1,"entries":[{"kind":"workflow","callID":"w1","done":true,` +
		`"text":"Workflow audit — finished","workflow":{"name":"audit","end":` + planted +
		`,"stages":[{"name":"items","round":1,"items":1,"finished":1,"state":"done"}]}}]}`

	entries, err := decodeTranscript([]byte(blob))

	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	m := newTestModel(t)
	m.transcript.entries = entries
	m.transcript.touch()
	got := workflowEntries(m)
	if len(got) != 1 || !got[0].workflow.drawsStages() {
		t.Fatalf("replayed workflow blocks = %+v; want one carrying a replayed view", got)
	}
	if end := got[0].workflow.end; end != domain.WorkflowStopped {
		t.Errorf("the replayed view's end = %q; want %q", end, domain.WorkflowStopped)
	}
	header := got[0].workflow.header()
	if strings.ContainsRune(header, '\x1b') || strings.Contains(header, "owned") {
		t.Errorf("the replayed header %q carries the recorded end", header)
	}
	painted := workflowPaint(m)
	if strings.Contains(painted, "owned") || !strings.Contains(painted, workflowLineSep+string(domain.WorkflowStopped)) {
		t.Errorf("the replayed block paints\n%s\nwant a stopped header and none of the recorded end", painted)
	}
}

// resumedRecipe is modelWithRecipeStages' Workflow run to its end — beta ends partial — saved to the
// session record, and replayed into a fresh idle model as a resume replays it.
func resumedRecipe(t *testing.T) Model {
	t.Helper()
	m := modelWithRecipeStages(t, &fakeEngine{})
	tr := &m.transcript
	tr.apply(itemFinishedUnder(recipeCall, "run.a", "build", "alpha", 0, "ok", "built alpha"))
	tr.apply(itemFinishedUnder(recipeCall, "run.b", "build", "beta", 1, "partial", "half of beta"))
	tr.apply(stageFinished("build", 1))
	tr.apply(workflowPhase(domain.WorkflowFinished))
	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	resumed := newTestModelEng(t, &fakeEngine{}, recallOpts(&fakeRecallHost{}))
	resumed.transcript.reset()
	resumed.replayScrollback(data, "audit", false)
	resumed.refreshViewport()
	return resumed
}

// A resumed block's stage rows open read-only views over the replayed item entries: a multi-item
// stage opens its stage view listing both items, each of which opens its run, and a one-item stage
// opens its item's run directly. No item's own output paints at the top level.
func TestResumedStageRowsOpenTheirViews(t *testing.T) {
	t.Parallel()
	m := resumedRecipe(t)
	if painted := plainTranscript(m); strings.Contains(painted, "is at work") ||
		!strings.Contains(painted, "build · beta — partial — half of beta") {
		t.Fatalf("the resumed top level paints item output, or no trouble line:\n%s", painted)
	}
	block := slices.IndexFunc(m.transcript.entries, func(e entry) bool { return e.kind == entryWorkflow })

	m = clickLine(t, m, stageRowLine(t, m, 1))

	if got, want := m.viewedStage(), (stageLevel{call: recipeCall, place: 2, block: block + 1}); got != want {
		t.Fatalf("the resumed build row opened stage %+v; want %+v", got, want)
	}
	alpha, beta := workflowItemHeadAt(m.transcript.entries, "run.a"), workflowItemHeadAt(m.transcript.entries, "run.b")
	if got := openedHeads(m); !slices.Equal(got, []int{alpha, beta}) {
		t.Errorf("the resumed stage view offers %v to open; want its two items %v", got, []int{alpha, beta})
	}
	m = clickLine(t, m, headerLineOf(t, m, beta))
	if got := m.viewedRun(); got != itemRef("run.b") {
		t.Errorf("the beta row opened %+v; want its run", got)
	}
	if painted := plainTranscript(m); !strings.Contains(painted, "beta is at work") {
		t.Errorf("the resumed item view does not paint the item's replayed output:\n%s", painted)
	}

	m = step(t, step(t, m, keyEsc()), keyEsc())
	m = clickLine(t, m, stageRowLine(t, m, 0))
	if got := m.viewedRun(); got != itemRef("run.p") {
		t.Errorf("the resumed one-item plan row opened %+v; want the planner's run", got)
	}
}

// After a resume, a run of the same Workflow id under the same call opens a block of its own: no
// phase of it reaches the replayed block, whose stage rows still open the replayed stage.
func TestResumedBlockIgnoresARunOfTheSameWorkflow(t *testing.T) {
	t.Parallel()
	m := resumedRecipe(t)
	before := workflowEntries(m)[0].workflow
	started := startedUnder(recipeCall)
	started.Stages = []string{"plan", "build", "check"}

	m.transcript.apply(started)
	m.transcript.apply(stageStarted("plan", 1, 1, 0))
	startItem(&m.transcript, "run.q", "plan", "planner", 1, 0, 1)
	m.transcript.apply(itemFinishedUnder(recipeCall, "run.q", "plan", "planner", 0, "blocked", "no plan"))
	m.refreshViewport()

	blocks := workflowEntries(m)
	if len(blocks) != 2 || !blocks[1].workflow.live() {
		t.Fatalf("workflow blocks = %d; want the replayed one and a live one", len(blocks))
	}
	if !reflect.DeepEqual(blocks[0].workflow, before) {
		t.Errorf("the new run moved the replayed block's view:\n%+v\nwant\n%+v", blocks[0].workflow, before)
	}
	block := slices.IndexFunc(m.transcript.entries, func(e entry) bool { return e.kind == entryWorkflow })
	m = clickLine(t, m, stageRowLine(t, m, 1))
	if at, ok := stageBlockAt(m.transcript.entries, m.viewedRun(), m.viewedStage()); !ok || at != block {
		t.Errorf("the replayed build row opened the stage of the block at %d; want the replayed block at %d", at, block)
	}
}

// ----------------------------------------------------------------------------
// A Workflow's item runs nest under the block that started it
// ----------------------------------------------------------------------------

// itemBase is the identity every event of one Workflow item child carries: one level below the
// agent that runs the Workflow, bracketed under the Workflow's call, under the child's own run id.
func itemBase(call, runID string) domain.EventBase {
	return domain.EventBase{Depth: 1, CallID: call, RunID: runID}
}

// itemSays folds one item child's committed narration.
func itemSays(tr *transcript, call, runID, text string) {
	tr.apply(domain.MessageEvent{EventBase: itemBase(call, runID), Text: text})
}

// itemReads folds one item child's read_file call and its result.
func itemReads(tr *transcript, call, runID, id, path string) {
	tr.apply(domain.ToolCallEvent{
		EventBase: itemBase(call, runID),
		Call:      domain.ToolCall{ID: id, Tool: "read_file", Arguments: []byte(`{"path":"` + path + `"}`)},
	})
	tr.apply(domain.ToolResultEvent{EventBase: itemBase(call, runID), Result: domain.ToolResult{CallID: id, Content: "1 - 10"}})
}

// startedUnder is testWorkflowID's started phase naming call as the call its item children run
// under.
func startedUnder(call string) domain.WorkflowPhaseEvent {
	e := workflowPhase(domain.WorkflowStarted)
	e.Call = call
	return e
}

// itemStartedUnder is one item run of testWorkflowID's stage starting under call: the item child's
// run id, its label, its place in the stage and the attempt it is, in the stage's first round.
func itemStartedUnder(call, runID, stage, item string, index, attempt int) domain.WorkflowPhaseEvent {
	e := workflowPhase(domain.WorkflowItemStarted)
	e.Call, e.Run, e.Stage, e.Item, e.Index, e.Round, e.Attempt = call, runID, stage, item, index, 1, attempt
	return e
}

// itemFinishedUnder is that item ending on a receipt, naming the run of its latest attempt.
func itemFinishedUnder(call, runID, stage, item string, index int, status, summary string) domain.WorkflowPhaseEvent {
	e := workflowPhase(domain.WorkflowItemFinished)
	e.Call, e.Run, e.Stage, e.Item, e.Index, e.Round = call, runID, stage, item, index, 1
	e.Receipt = domain.WorkflowReceipt{Status: status, Summary: summary}
	return e
}

// feedTwoItems folds two item runs of the Workflow under call — alpha (run.1) and beta (run.2),
// each started before its child says a thing — interleaved as siblings running at once arrive, with
// a host note landing between them mid-run.
func feedTwoItems(tr *transcript, call string) {
	tr.apply(itemStartedUnder(call, "run.1", "items", "alpha", 0, 1))
	tr.apply(itemStartedUnder(call, "run.2", "items", "beta", 1, 1))
	itemSays(tr, call, "run.1", "alpha first")
	itemSays(tr, call, "run.2", "beta first")
	tr.addNote("a host note mid-run")
	itemReads(tr, call, "run.1", "a1", "alpha.go")
	itemSays(tr, call, "run.2", "beta second")
	itemSays(tr, call, "run.1", "alpha second")
}

// assertHeadsItemRuns fails unless the entry at head is followed, inside its span, by every entry
// of both item runs — each run's in the order it arrived — with the host note after the span.
func assertHeadsItemRuns(t *testing.T, entries []entry, head int) {
	t.Helper()
	span := subAgentSpan(entries, head)
	inside := map[string]int{}
	for i := head + 1; i <= head+span; i++ {
		inside[entries[i].text] = i
	}
	for _, text := range []string{"alpha first", "beta first", "beta second", "alpha second"} {
		if _, ok := inside[text]; !ok {
			t.Errorf("%q is not inside the head's span (head %d, span %d): %+v", text, head, span, entries)
		}
	}
	if inside["alpha first"] > inside["alpha second"] || inside["beta first"] > inside["beta second"] {
		t.Errorf("an item run's entries are out of arrival order: %v", inside)
	}
	reads := 0
	for i := head + 1; i <= head+span; i++ {
		if entries[i].kind == entryToolCall && entries[i].callID == "a1" {
			reads++
		}
	}
	if reads != 1 {
		t.Errorf("alpha's read_file is inside the span %d times, want once", reads)
	}
	for i := head + 1; i <= head+span; i++ {
		if entries[i].kind == entryNote {
			t.Errorf("the host note split the span: it stands at %d inside %d..%d", i, head+1, head+span)
		}
	}
}

// roundTrip saves the transcript's entries to the session record and reads them back.
func roundTrip(t *testing.T, tr *transcript) []entry {
	t.Helper()
	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	entries, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return entries
}

// assertItemRunsBehindTheirHeads fails unless each of feedTwoItems' item runs has its own run head
// seated beside the block at head — at its depth, under call, heading the run by its id — with the
// run's entries inside that head's span in the order they arrived.
func assertItemRunsBehindTheirHeads(t *testing.T, entries []entry, head int, call string) {
	t.Helper()
	for runID, texts := range map[string][]string{
		"run.1": {"alpha first", "alpha second"},
		"run.2": {"beta first", "beta second"},
	} {
		at := workflowItemHeadAt(entries, runID)
		if at < 0 {
			t.Fatalf("no item head for %s in %+v", runID, entries)
		}
		item := entries[at]
		if item.depth != entries[head].depth || item.callID != call || item.run() != entries[head].run() {
			t.Errorf("%s's head = depth %d, call %q, run %+v; want the block's depth %d, call %q and run %+v",
				runID, item.depth, item.callID, item.run(), entries[head].depth, call, entries[head].run())
		}
		if at <= head || at > head+subAgentSpan(entries, head) {
			t.Errorf("%s's head at %d is outside the block's span (%d + %d)", runID, at, head, subAgentSpan(entries, head))
		}
		last := at
		for _, text := range texts {
			i := slices.IndexFunc(entries, func(e entry) bool { return e.text == text })
			if i <= last || i > at+subAgentSpan(entries, at) {
				t.Errorf("%q at %d is out of order or outside %s's span (%d + %d)", text, i, runID, at, subAgentSpan(entries, at))
			}
			last = i
		}
	}
}

// assertItemRowsOnly fails unless the paint shows no item child's own output and paints each item
// as a row wearing ▶ — the run it heads is read in its own view.
func assertItemRowsOnly(t *testing.T, painted string, labels ...string) {
	t.Helper()
	for _, text := range []string{"alpha first", "beta first", "beta second", "alpha second", "alpha.go"} {
		if strings.Contains(painted, text) {
			t.Errorf("an item child's output %q is painted at the top level:\n%s", text, painted)
		}
	}
	lines := strings.Split(painted, "\n")
	for _, label := range labels {
		if !slices.ContainsFunc(lines, func(ln string) bool {
			return strings.Contains(ln, " "+label+" ") && strings.Contains(ln, glyphCollapsed)
		}) {
			t.Errorf("item %q paints no row wearing %s:\n%s", label, glyphCollapsed, painted)
		}
	}
}

// assertStageRowsOnly fails unless the paint shows no item child's own output and no item row, and
// paints each stage as a row wearing ▶ — a live workflow block's stage rows stand for its item runs.
func assertStageRowsOnly(t *testing.T, painted string, stages ...string) {
	t.Helper()
	for _, text := range []string{"alpha first", "beta first", "beta second", "alpha second", "alpha.go"} {
		if strings.Contains(painted, text) {
			t.Errorf("an item child's output %q is painted at the top level:\n%s", text, painted)
		}
	}
	for _, label := range []string{"alpha", "beta", "gamma"} {
		if strings.Contains(painted, " "+label+" ") {
			t.Errorf("item %q paints a row at the top level:\n%s", label, painted)
		}
	}
	for _, stage := range stages {
		if row := stageRowIn(painted, stage); !strings.Contains(row, glyphCollapsed) {
			t.Errorf("stage %q paints no row wearing %s:\n%s", stage, glyphCollapsed, painted)
		}
	}
}

// A Recipe launch's item runs each get a run head of their own inside its workflow block's span —
// live and after a save and reopen — with the item's work behind it. The live block paints one row
// per stage and steps over the item heads, and so does the reopened one, whose record kept its
// stages. A later stage's item started after a host note has landed still seats inside the
// span, with the note below it.
func TestWorkflowBlockSeatsARunHeadPerItem(t *testing.T) {
	t.Parallel()
	const call = "recipe-audit-1"
	tr := &transcript{}
	tr.addUser("/audit src", nil)
	tr.apply(startedUnder(call))
	feedTwoItems(tr, call)

	head := slices.IndexFunc(tr.entries, func(e entry) bool { return e.kind == entryWorkflow })
	if head < 0 || tr.entries[head].callID != call {
		t.Fatalf("workflow block at %d; want one recording call %q in %+v", head, call, tr.entries)
	}
	assertHeadsItemRuns(t, tr.entries, head)
	assertItemRunsBehindTheirHeads(t, tr.entries, head, call)
	assertStageRowsOnly(t, plainRender(tr), "items")

	tr.apply(itemStartedUnder(call, "run.3", "review", "gamma", 0, 1))
	third := workflowItemHeadAt(tr.entries, "run.3")
	note := slices.IndexFunc(tr.entries, func(e entry) bool { return e.kind == entryNote })
	if span := subAgentSpan(tr.entries, head); third <= head || third > head+span {
		t.Errorf("the second stage's item head at %d is outside the block's span (%d + %d)", third, head, span)
	}
	if note <= third {
		t.Errorf("the host note at %d stands above the second stage's item head at %d", note, third)
	}
	assertStageRowsOnly(t, plainRender(tr), "items", "review")

	reopened := &transcript{entries: roundTrip(t, tr)}
	reopened.touch()
	assertItemRunsBehindTheirHeads(t, reopened.entries, head, call)
	assertStageRowsOnly(t, plainRender(reopened), "items", "review")
}

// A fan_out call's item runs each get a run head of their own inside its card's span the same way.
// The card stays the call it is — its own closed top-level call with nothing folded in — and its
// item rows never fold into a "✦ Sub-Agent (N)" list: they are the card's own body.
func TestFanOutCardSeatsARunHeadPerItem(t *testing.T) {
	t.Parallel()
	const call = "f1"
	tr := &transcript{}
	tr.addUser("fan it out", nil)
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: call, Tool: fanOutToolName, Arguments: []byte(`{"task":"check {item}"}`)}})
	tr.apply(startedUnder(call))
	feedTwoItems(tr, call)
	for i, run := range []string{"run.1", "run.2"} {
		stampedPhase(tr, runRef{depth: 1, spawn: call, id: run}, domain.SubAgentFinished, "item report "+run)
		tr.apply(itemFinishedUnder(call, run, "items", []string{"alpha", "beta"}[i], i, "ok", "fine"))
	}
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: call, Content: "#1 alpha — ok — fine"}})

	head := slices.IndexFunc(tr.entries, func(e entry) bool { return e.kind == entryToolCall && e.tool.name == fanOutToolName })
	if head < 0 {
		t.Fatalf("no fan_out card in %+v", tr.entries)
	}
	card := tr.entries[head]
	if !card.done || !card.run().isTop() || card.spawnRunID != "" || card.tool.agentName != "" {
		t.Errorf("fan_out card = done %v, run %+v, spawnRunID %q, agentName %q; want its own closed top-level call with nothing folded in",
			card.done, card.run(), card.spawnRunID, card.tool.agentName)
	}
	if n := len(workflowEntries(Model{transcript: *tr})); n != 0 {
		t.Errorf("workflow blocks = %d; want none beside the fan_out card", n)
	}
	assertHeadsItemRuns(t, tr.entries, head)
	assertItemRunsBehindTheirHeads(t, tr.entries, head, call)
	painted := plainRender(tr)
	assertItemRowsOnly(t, painted, "alpha", "beta")
	if strings.Contains(painted, subAgentGroupLabel+" (") {
		t.Errorf("the fan_out card's item rows folded into a sub-agent list:\n%s", painted)
	}
	if strings.Contains(painted, "item report") {
		t.Errorf("an item child's report reached the paint:\n%s", painted)
	}

	reopened := &transcript{entries: roundTrip(t, tr)}
	reopened.touch()
	assertItemRunsBehindTheirHeads(t, reopened.entries, head, call)
}

// A retried item keeps ONE row: each attempt has its own head, the earlier one closes on its run's
// finished phase and carries no receipt, the receipt folds onto the latest attempt's head, the row
// opens that attempt — and the earlier head replays closed after a save and reopen. The row is a
// fan_out card's, whose item rows stand in the transcript; a Recipe block's stand behind its stage
// rows.
func TestARetriedItemKeepsOneRow(t *testing.T) {
	t.Parallel()
	const call = "f1"
	m := newTestModel(t)
	m.transcript.reset()
	tr := &m.transcript
	tr.addUser("fan it out", nil)
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: call, Tool: fanOutToolName, Arguments: []byte(`{"task":"check {item}"}`)}})
	tr.apply(startedUnder(call))
	tr.apply(itemStartedUnder(call, "run.1", "items", "alpha", 0, 1))
	itemSays(tr, call, "run.1", "first try")
	stampedPhase(tr, runRef{depth: 1, spawn: call, id: "run.1"}, domain.SubAgentFinished, "gave up")
	tr.apply(itemStartedUnder(call, "run.2", "items", "alpha", 0, 2))
	itemSays(tr, call, "run.2", "second try")
	stampedPhase(tr, runRef{depth: 1, spawn: call, id: "run.2"}, domain.SubAgentFinished, "fixed it")
	tr.apply(itemFinishedUnder(call, "run.2", "items", "alpha", 0, "ok", "all fixed"))
	m.refreshViewport()

	first, second := workflowItemHeadAt(tr.entries, "run.1"), workflowItemHeadAt(tr.entries, "run.2")
	if e := tr.entries[first]; !e.done || !e.tool.stat.blank() {
		t.Errorf("the first attempt's head = done %v, verdict %q; want closed with no receipt", e.done, e.tool.stat.spell())
	}
	if e := tr.entries[second]; !e.done || e.tool.stat.spell() != "ok" || e.tool.Summary.Text != "all fixed" {
		t.Errorf("the latest attempt's head = done %v, verdict %q, gist %q; want the receipt folded onto it",
			e.done, e.tool.stat.spell(), e.tool.Summary.Text)
	}
	painted := plainRender(tr)
	if n := strings.Count(painted, glyphCollapsed); n != 1 {
		t.Errorf("the retried item paints %d rows, want one:\n%s", n, painted)
	}
	if m = enterOnLastBlock(t, m); m.viewedRun().id != "run.2" {
		t.Errorf("the item row opened %+v; want the latest attempt, run.2", m.viewedRun())
	}

	reopened := roundTrip(t, tr)
	if e := reopened[first]; e.kind != entryWorkflowItem || !e.done || e.item.attempt != 1 {
		t.Errorf("the replayed first attempt = kind %v, done %v, attempt %d; want a closed attempt-1 item head",
			e.kind, e.done, e.item.attempt)
	}
	if e := reopened[second]; !e.tool.Summary.succeeded || e.item.attempt != 2 {
		t.Errorf("the replayed latest attempt = succeeded %v, attempt %d; want the ok verdict on attempt 2",
			e.tool.Summary.succeeded, e.item.attempt)
	}
}

// A delegation beside a fan_out keeps its own head: its child's entries land in its span, not the
// card's, and the card's item runs land in the card's span though the delegation's call stands
// between them and the end of the list. Collapsing the delegation still elides its run while the
// card's item runs stay painted.
func TestSubAgentBesideAFanOutKeepsItsOwnHead(t *testing.T) {
	t.Parallel()
	tr := &transcript{}
	tr.addUser("do both", nil)
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "f1", Tool: fanOutToolName, Arguments: []byte(`{"task":"check {item}"}`)}})
	subAgentCall(tr, "s1", "survey the tests", 0)
	tr.apply(startedUnder("f1"))
	itemSays(tr, "f1", "run.1", "item one speaks")
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s1", RunID: "run.3"}, Text: "delegate speaks"})
	itemSays(tr, "f1", "run.2", "item two speaks")

	at := func(text string) int {
		for i, e := range tr.entries {
			if e.text == text {
				return i
			}
		}
		t.Fatalf("no entry %q in %+v", text, tr.entries)
		return -1
	}
	sub, card := -1, -1
	for i, e := range tr.entries {
		switch {
		case e.headsRun():
			sub = i
		case e.kind == entryToolCall && e.tool.name == fanOutToolName:
			card = i
		}
	}
	if d := at("delegate speaks"); d <= sub || d > sub+subAgentSpan(tr.entries, sub) {
		t.Errorf("the delegate's entry at %d is outside its head's span (%d + %d)", d, sub, subAgentSpan(tr.entries, sub))
	}
	for _, text := range []string{"item one speaks", "item two speaks"} {
		if i := at(text); i <= card || i > card+subAgentSpan(tr.entries, card) {
			t.Errorf("%q at %d is outside the fan_out card's span (%d + %d)", text, i, card, subAgentSpan(tr.entries, card))
		}
	}
	painted := plainRender(tr)
	if strings.Contains(painted, "delegate speaks") {
		t.Errorf("the collapsed delegation did not elide its run:\n%s", painted)
	}
	if !strings.Contains(painted, "item one speaks") || !strings.Contains(painted, "item two speaks") {
		t.Errorf("the fan_out card's item runs are not painted:\n%s", painted)
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

// A Workflow's item run is climbed through the block that started it: a collapsed delegation that
// called fan_out elides its items' streaming tails, while the items of a top-level workflow block
// or fan_out card — which never elide what they head — are inside no collapsed run.
func TestItemRunsAreClimbedThroughTheirWorkflowHead(t *testing.T) {
	t.Parallel()
	tr := &transcript{}
	subAgentCall(tr, "s1", "fan it out", 0)
	tr.apply(domain.ToolCallEvent{
		EventBase: domain.EventBase{Depth: 1, CallID: "s1"},
		Call:      domain.ToolCall{ID: "f1", Tool: fanOutToolName, Arguments: []byte(`{"task":"check {item}"}`)},
	})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "f2", Tool: fanOutToolName, Arguments: []byte(`{"task":"check {item}"}`)}})

	nested := runRef{depth: 2, spawn: "f1", id: "run.1"}
	if !insideCollapsedRun(tr.entries, nested, runRef{}) {
		t.Error("an item of a fan_out inside a collapsed delegation is not inside a collapsed run")
	}
	delegation := tr.entries[0].spawned()
	if !runUnder(tr.entries, nested, delegation) {
		t.Error("an item of a fan_out the delegation called is not under the delegation's view")
	}
	if top := (runRef{depth: 1, spawn: "f2", id: "run.2"}); insideCollapsedRun(tr.entries, top, runRef{}) {
		t.Error("an item of a top-level fan_out card is inside a collapsed run; the card's fold hides its own body, never its item rows")
	}
}
