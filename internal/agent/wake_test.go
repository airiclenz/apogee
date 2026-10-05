package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/workflow"
)

// Finish notes and the wake (ADR 0089 D3): each test launches the one-item "pair" recipe in the
// background over workflowResponder and lets it end at the point the test is about — while the
// agent is idle, between two Steps of an Exchange, or inside an Exchange's final Turn — then drives
// the parent the way a Driver does: TakeWorkflowNotes and Interject between Steps, Wake when idle,
// Submit otherwise. The parent's requests are told apart by their last user message: the human's
// "go ahead", or the note header a wake or a drained note opens with.

// wakeUserText is the human's message in every test that sends one.
const wakeUserText = "go ahead"

// pairNoteLine is the start of the finish note the one-item "pair" recipe leaves when its item ends
// ok.
const pairNoteLine = "workflow pair finished — items 1 · ok 1 · partial 0 · blocked 0 — items: "

// newPairParent builds a parent serving the one-item "pair" recipe over up, recording the last user
// message of every request.
func newPairParent(t *testing.T, sink *lockedSink, up *workflowResponder, tweak func(*domain.Config)) (*Agent, *requestLog) {
	t.Helper()
	cfg := recipeConfig(t, sink, sweepRecipe("pair", "alpha"))
	if tweak != nil {
		tweak(&cfg)
	}
	log := &requestLog{inner: up}
	return newBackgroundParent(t, cfg, log), log
}

// workflowEnds returns the end phases the sink saw for the workflow id, in order.
func workflowEnds(sink *lockedSink, id string) []domain.WorkflowPhase {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	var ends []domain.WorkflowPhase
	for _, e := range sink.events {
		event, ok := e.(domain.WorkflowPhaseEvent)
		if !ok || event.Workflow != id {
			continue
		}
		switch event.Phase {
		case domain.WorkflowFinished, domain.WorkflowStopped, domain.WorkflowFailed:
			ends = append(ends, event.Phase)
		}
	}
	return ends
}

// runToEnd runs a to the end of its Exchange and fails the test unless it completed.
func runToEnd(t *testing.T, a *Agent) {
	t.Helper()
	res, err := a.Run(context.Background())
	if err != nil || res.Status != domain.StatusExchangeComplete {
		t.Fatalf("Run = %+v, %v; want a completed Exchange", res, err)
	}
}

// assertNoteMessage fails the test unless content carries the note header and the pair note line.
func assertNoteMessage(t *testing.T, content string) {
	t.Helper()
	if !strings.Contains(content, workflowNoteHeader+"\n"+pairNoteLine) {
		t.Errorf("message = %q, want the note header and then %q", content, pairNoteLine)
	}
}

func TestWake_AFinishDuringAnExchangeReachesItAtTheNextBoundary(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	up := (&workflowResponder{}).
		route("sweep alpha", signalThenWait(make(chan struct{}), release), finishScript("f1", "alpha is fine")).
		route(wakeUserText, nil, toolCallScript("c1", "read_thing", `{}`)).
		route(workflowNoteHeader, nil, contentScript("noted"))
	a, log := newPairParent(t, newLockedSink(), up, nil)
	launchBackground(t, a, "pair")
	if err := a.Submit(domain.UserInput{Text: wakeUserText}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res, err := a.Step(context.Background()); err != nil || res.Status != domain.StatusTurnComplete {
		t.Fatalf("first Step = %+v, %v; want a Turn that leaves the Exchange open", res, err)
	}
	if _, ok := a.TakeWorkflowNotes(); ok {
		t.Fatal("TakeWorkflowNotes handed over a note before any workflow ended")
	}
	close(release)
	a.background.waitAll()

	woke, err := a.Wake(context.Background())
	note, ok := a.TakeWorkflowNotes()

	if err != nil || woke {
		t.Errorf("Wake mid-Exchange = %v, %v; want no wake while the Exchange runs", woke, err)
	}
	if !ok {
		t.Fatal("TakeWorkflowNotes found no note after the workflow ended mid-Exchange")
	}
	if err := a.Interject(context.Background(), note); err != nil {
		t.Fatalf("Interject(note): %v", err)
	}
	runToEnd(t, a)
	assertNoteMessage(t, log.first(t, workflowNoteHeader))
	if openings := exchangeOpenings(&a.conv); len(openings) != 1 {
		t.Errorf("Exchange openings = %v, want the note joined the running Exchange", openings)
	}
	if _, again := a.TakeWorkflowNotes(); again {
		t.Error("a second TakeWorkflowNotes handed the same note over again")
	}
}

func TestWake_AnIdleFinishIsReportedAndWakeOpensAnExchangeOnIt(t *testing.T) {
	t.Parallel()

	sink := newLockedSink()
	up := (&workflowResponder{}).
		route("sweep alpha", nil, finishScript("f1", "alpha is fine")).
		route(workflowNoteHeader, nil, contentScript("the sweep is done"))
	a, log := newPairParent(t, sink, up, nil)
	id := launchBackground(t, a, "pair")
	a.background.waitAll()

	woke, err := a.Wake(context.Background())

	if ends := workflowEnds(sink, id); len(ends) != 1 || ends[0] != domain.WorkflowFinished {
		t.Errorf("end events = %v, want one WorkflowFinished", ends)
	}
	if err != nil || !woke {
		t.Fatalf("Wake = %v, %v; want it to open an Exchange on the held note", woke, err)
	}
	runToEnd(t, a)
	assertNoteMessage(t, log.first(t, workflowNoteHeader))
	if again, err := a.Wake(context.Background()); err != nil || again {
		t.Errorf("a second Wake = %v, %v; want nothing left to wake on", again, err)
	}
}

func TestWake_OffLeavesTheNoteForTheNextMessage(t *testing.T) {
	t.Parallel()

	up := (&workflowResponder{}).
		route("sweep alpha", nil, finishScript("f1", "alpha is fine")).
		route(wakeUserText, nil, contentScript("done"))
	a, log := newPairParent(t, newLockedSink(), up, func(cfg *domain.Config) {
		off := false
		cfg.Workflow.Wake = &off
	})
	launchBackground(t, a, "pair")
	a.background.waitAll()

	woke, err := a.Wake(context.Background())

	if err != nil || woke {
		t.Fatalf("Wake under workflow-wake: off = %v, %v; want no wake", woke, err)
	}
	runInput(t, a, domain.UserInput{Text: wakeUserText})
	sent := log.first(t, wakeUserText)
	if !strings.HasPrefix(sent, wakeUserText) {
		t.Errorf("message = %q, want the human's text first", sent)
	}
	assertNoteMessage(t, sent)
}

func TestWake_AFinishDuringTheFinalTurnStillWakes(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var a *Agent
	endWorkflow := func(context.Context) {
		close(release)
		a.background.waitAll()
	}
	up := (&workflowResponder{}).
		route("sweep alpha", signalThenWait(make(chan struct{}), release), finishScript("f1", "alpha is fine")).
		route(workflowNoteHeader, nil, contentScript("the sweep is done")).
		route(wakeUserText, endWorkflow, contentScript("done"))
	a, log := newPairParent(t, newLockedSink(), up, nil)
	launchBackground(t, a, "pair")
	runInput(t, a, domain.UserInput{Text: wakeUserText})

	_, drained := a.TakeWorkflowNotes()
	woke, err := a.Wake(context.Background())

	if drained {
		t.Error("TakeWorkflowNotes handed a note over with no Exchange open")
	}
	if err != nil || !woke {
		t.Fatalf("Wake after the final Turn = %v, %v; want the held note to wake the agent", woke, err)
	}
	runToEnd(t, a)
	assertNoteMessage(t, log.first(t, workflowNoteHeader))
}

func TestWake_ASnapshotKeepsTheWakeOpenersNote(t *testing.T) {
	t.Parallel()

	up := (&workflowResponder{}).
		route("sweep alpha", nil, finishScript("f1", "alpha is fine")).
		route(workflowNoteHeader, nil, contentScript("the sweep is done"))
	a, _ := newPairParent(t, newLockedSink(), up, nil)
	launchBackground(t, a, "pair")
	a.background.waitAll()
	if woke, err := a.Wake(context.Background()); err != nil || !woke {
		t.Fatalf("Wake = %v, %v", woke, err)
	}
	runToEnd(t, a)

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	b, err := resumeAgent(a.cfg, snap, up)
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	t.Cleanup(func() { stopBackgroundAndWait(b) })

	openings := exchangeOpenings(&b.conv)
	if len(openings) != 1 {
		t.Fatalf("restored Exchange openings = %v, want the wake's one", openings)
	}
	assertNoteMessage(t, b.conv.At(openings[0]).Content)
}

func TestWake_RefusesWithoutAModelAndKeepsTheNote(t *testing.T) {
	t.Parallel()

	up := (&workflowResponder{}).route("sweep alpha", nil, finishScript("f1", "alpha is fine"))
	a, _ := newPairParent(t, newLockedSink(), up, nil)
	launchBackground(t, a, "pair")
	a.background.waitAll()
	a.cfg.Model = ""

	woke, err := a.Wake(context.Background())

	if woke || !errors.Is(err, errNoModelBound) {
		t.Errorf("Wake with no model = %v, %v; want errNoModelBound", woke, err)
	}
	if notes := a.background.takeNotes(); len(notes) != 1 {
		t.Errorf("held notes after the refusal = %q, want the one note kept", notes)
	}
}

func TestFinishNote_SaysHowTheWorkflowEnded(t *testing.T) {
	t.Parallel()

	fanout := func(phase workflow.Phase, tally workflow.Tally) workflow.StageResult {
		return workflow.StageResult{Name: "find", Kind: workflow.StageFanout, Phase: phase, Tally: tally}
	}
	for _, tc := range []struct {
		name     string
		result   workflow.Result
		err      error
		fellBack bool
		want     string
	}{
		{
			name: "finished with a report and verdicts",
			result: workflow.Result{
				Phase:   workflow.PhaseDone,
				Stages:  []workflow.StageResult{fanout(workflow.PhaseDone, workflow.Tally{OK: 2, Partial: 1, Confirmed: 1, Refuted: 1})},
				Report:  "/s/workflows/w1/report.md",
				Listing: "/s/workflows/w1/items.md",
			},
			want: "workflow audit finished — items 3 · ok 2 · partial 1 · blocked 0 · " +
				"confirmed 1 · refuted 1 · unclear 0 — report: /s/workflows/w1/report.md",
		},
		{
			name: "stopped, counted across fan-outs, a skipped one left out",
			result: workflow.Result{
				Phase: workflow.PhaseStopped,
				Stages: []workflow.StageResult{
					fanout(workflow.PhaseStopped, workflow.Tally{OK: 1, Unfinished: 2}),
					fanout(workflow.PhaseDone, workflow.Tally{Blocked: 1}),
					fanout(workflow.PhaseSkipped, workflow.Tally{OK: 9}),
				},
				Listing: "/s/workflows/w1/items.md",
			},
			want: "workflow audit stopped — items 4 · ok 1 · partial 0 · blocked 1 · unfinished 2 — items: /s/workflows/w1/items.md",
		},
		{
			name: "failed, its cause folded onto the line",
			err:  errors.New("workflow: store\nunwritable"),
			want: "workflow audit failed — workflow: store unwritable",
		},
		{
			name: "an item fell back from the sub-agents server, the note last",
			result: workflow.Result{
				Phase:   workflow.PhaseDone,
				Stages:  []workflow.StageResult{fanout(workflow.PhaseDone, workflow.Tally{OK: 1})},
				Listing: "/s/workflows/w1/items.md",
			},
			fellBack: true,
			want:     "workflow audit finished — items 1 · ok 1 · partial 0 · blocked 0 — items: /s/workflows/w1/items.md — " + SeatFallbackNote,
		},
		{
			name:     "failed, no seat note however its items ran",
			err:      errors.New("workflow: store unwritable"),
			fellBack: true,
			want:     "workflow audit failed — workflow: store unwritable",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := finishNote("", "audit\n", tc.result, tc.err, tc.fellBack)

			if got != tc.want {
				t.Errorf("finishNote = %q\nwant         %q", got, tc.want)
			}
		})
	}
}

// subAgentNoteItem lays out a background sub_agent's one-item workflow folder under a temp dir and
// returns its Result: report, when non-empty, as the item's output file and a transcript when
// withTranscript, beside the path the transcript has or would have.
func subAgentNoteItem(t *testing.T, phase workflow.Phase, report string, withTranscript bool) (workflow.Result, string) {
	t.Helper()
	dir := t.TempDir()
	key := strings.Repeat("a", 64)
	itemDir := filepath.Join(dir, "items", key)
	if err := os.MkdirAll(itemDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	output := filepath.Join(itemDir, "output.md")
	if report != "" {
		if err := os.WriteFile(output, []byte(report), 0o600); err != nil {
			t.Fatalf("write output: %v", err)
		}
	}
	transcript := filepath.Join(itemDir, "transcript.jsonl")
	if withTranscript {
		if err := os.WriteFile(transcript, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("write transcript: %v", err)
		}
	}
	item := workflow.ItemResult{Key: key, Label: "surveyor", Phase: phase, Output: output}
	return workflow.Result{
		Phase: phase,
		Dir:   dir,
		Stages: []workflow.StageResult{{
			Name: subAgentStageName, Kind: workflow.StageFanout, Phase: phase, Items: []workflow.ItemResult{item},
		}},
	}, transcript
}

// TestFinishNote_ASubAgentNoteCarriesItsReport pins ADR 0094 D3: a background sub_agent's note is
// its lead line — no item counts, the transcript named only when one was written — and the child's
// report under it, with SeatFallbackNote once however the fallback reached it.
func TestFinishNote_ASubAgentNoteCarriesItsReport(t *testing.T) {
	t.Parallel()

	const report = "The repo holds three modules.\nEach has its own tests."
	for _, tc := range []struct {
		name           string
		phase          workflow.Phase
		report         string
		withTranscript bool
		err            error
		fellBack       bool
		want           func(transcript string) string
	}{
		{
			name: "finished, its transcript and report", phase: workflow.PhaseDone, report: report + "\n", withTranscript: true,
			want: func(transcript string) string {
				return "sub_agent surveyor finished — transcript: " + transcript + "\n" + report
			},
		},
		{
			name: "faulted before it started, no transcript to name", phase: workflow.PhaseDone, report: "sub-agent failed: no model",
			want: func(string) string { return "sub_agent surveyor finished\nsub-agent failed: no model" },
		},
		{
			name: "stopped, no report kept", phase: workflow.PhaseStopped, withTranscript: true,
			want: func(transcript string) string { return "sub_agent surveyor stopped — transcript: " + transcript },
		},
		{
			name: "fell back, the report already carrying the seat note", phase: workflow.PhaseDone,
			report: report + "\n" + SeatFallbackNote, withTranscript: true, fellBack: true,
			want: func(transcript string) string {
				return "sub_agent surveyor finished — transcript: " + transcript + "\n" + report + "\n" + SeatFallbackNote
			},
		},
		{
			name: "fell back, the seat note appended once", phase: workflow.PhaseStopped, withTranscript: true, fellBack: true,
			want: func(transcript string) string {
				return "sub_agent surveyor stopped — transcript: " + transcript + "\n" + SeatFallbackNote
			},
		},
		{
			name: "failed, its cause folded onto the lead", err: errors.New("workflow: store\nunwritable"), fellBack: true,
			want: func(string) string { return "sub_agent surveyor failed — workflow: store unwritable" },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, transcript := subAgentNoteItem(t, tc.phase, tc.report, tc.withTranscript)

			got := finishNote(workflow.OriginSubAgent, "surveyor\n", result, tc.err, tc.fellBack)

			if want := tc.want(transcript); got != want {
				t.Errorf("finishNote = %q\nwant         %q", got, want)
			}
		})
	}
}

// TestFinishNote_ASubAgentReportOverTheCapIsCutLikeABlockingResult pins that the note holds the
// report to the blocking delegation result's cap (capDelegateResult), never a number of its own.
func TestFinishNote_ASubAgentReportOverTheCapIsCutLikeABlockingResult(t *testing.T) {
	t.Parallel()
	report := strings.Repeat("a", delegateResultMaxBytes) + strings.Repeat("z", delegateResultMaxBytes)
	result, transcript := subAgentNoteItem(t, workflow.PhaseDone, report, true)

	got := finishNote(workflow.OriginSubAgent, "surveyor", result, nil, false)

	want := "sub_agent surveyor finished — transcript: " + transcript + "\n" + capDelegateResult(report)
	if got != want {
		t.Errorf("finishNote is %d bytes, want the lead and the capped report (%d bytes)", len(got), len(want))
	}
}

// TestRenderWorkflowNotes_SetsNotesABlankLineApartOnceOneSpansLines pins that a sub_agent's report
// never runs into the note after it, while one-line notes keep one line each.
func TestRenderWorkflowNotes_SetsNotesABlankLineApartOnceOneSpansLines(t *testing.T) {
	t.Parallel()
	const (
		subAgent = "sub_agent surveyor finished — transcript: /s/t.jsonl\nThe repo holds three modules."
		fanOut   = "workflow audit finished — items 1 · ok 1 · partial 0 · blocked 0 — items: /s/items.md"
	)
	for _, tc := range []struct {
		name  string
		notes []string
		want  string
	}{
		{"a sub_agent note, then a fan_out one", []string{subAgent, fanOut}, workflowNoteHeader + "\n" + subAgent + "\n\n" + fanOut},
		{"one-line notes only", []string{fanOut, fanOut}, workflowNoteHeader + "\n" + fanOut + "\n" + fanOut},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := renderWorkflowNotes(tc.notes); got != tc.want {
				t.Errorf("renderWorkflowNotes = %q\nwant                  %q", got, tc.want)
			}
		})
	}
}

// subAgentNoteWant is the finish note the background sub_agent "surveyor" of workflow id on a
// leaves when its child completed with report: the lead naming its transcript, then the report.
func subAgentNoteWant(t *testing.T, a *Agent, id, report string) string {
	t.Helper()
	info := workflowInfo(t, a, id)
	key := info.Status.Stages[0].Items[0].Key
	transcript, found, err := workflow.ItemTranscriptPath(info.Dir, key)
	if err != nil || !found {
		t.Fatalf("ItemTranscriptPath = %q, %v, %v; want the child's transcript", transcript, found, err)
	}
	return "sub_agent surveyor finished — transcript: " + transcript + "\n" + report
}

// subAgentWakeReport is the report the background sub_agent of the wake tests hands back.
const subAgentWakeReport = "The repo holds three modules."

func TestWake_ABackgroundSubAgentsReportReachesTheExchangeAtTheNextBoundary(t *testing.T) {
	t.Parallel()

	started, release := make(chan struct{}), make(chan struct{})
	up := (&workflowResponder{}).
		route("please delegate", nil, toolCallScript("sa1", tools.SubAgentToolName, backgroundSubAgentArgsJSON(map[string]any{"name": "surveyor"}))).
		route(subAgentTask, signalThenWait(started, release), contentScript(subAgentWakeReport)).
		route(workflowNoteHeader, nil, contentScript("noted"))
	log := &requestLog{inner: up}
	a := newBackgroundParent(t, backgroundSubAgentConfig(t, newLockedSink()), log)
	if err := a.Submit(domain.UserInput{Text: "please delegate"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res, err := a.Step(context.Background()); err != nil || res.Status != domain.StatusTurnComplete {
		t.Fatalf("first Step = %+v, %v; want a Turn that leaves the Exchange open", res, err)
	}
	awaitClosed(t, started, "the background child")
	close(release)
	a.background.waitAll()

	note, ok := a.TakeWorkflowNotes()

	if !ok {
		t.Fatal("TakeWorkflowNotes found no note after the background sub_agent ended mid-Exchange")
	}
	if err := a.Interject(context.Background(), note); err != nil {
		t.Fatalf("Interject(note): %v", err)
	}
	runToEnd(t, a)
	want := workflowNoteHeader + "\n" + subAgentNoteWant(t, a, soleWorkflowID(t, a), subAgentWakeReport)
	if got := log.first(t, workflowNoteHeader); !strings.Contains(got, want) {
		t.Errorf("message = %q, want it to carry %q", got, want)
	}
}

func TestWake_AnIdleBackgroundSubAgentWakesTheAgentWithItsReport(t *testing.T) {
	t.Parallel()

	up := (&workflowResponder{}).
		route("please delegate", nil, toolCallScript("sa1", tools.SubAgentToolName, backgroundSubAgentArgsJSON(map[string]any{"name": "surveyor"}))).
		route("please delegate", nil, contentScript("started it")).
		route(subAgentTask, nil, contentScript(subAgentWakeReport)).
		route(workflowNoteHeader, nil, contentScript("the survey is in"))
	log := &requestLog{inner: up}
	a := newBackgroundParent(t, backgroundSubAgentConfig(t, newLockedSink()), log)
	runSubmitted(t, context.Background(), a, "please delegate")
	a.background.waitAll()

	woke, err := a.Wake(context.Background())

	if err != nil || !woke {
		t.Fatalf("Wake = %v, %v; want it to open an Exchange on the held note", woke, err)
	}
	runToEnd(t, a)
	want := workflowNoteHeader + "\n" + subAgentNoteWant(t, a, soleWorkflowID(t, a), subAgentWakeReport)
	if got := log.first(t, workflowNoteHeader); !strings.Contains(got, want) {
		t.Errorf("message = %q, want it to carry %q", got, want)
	}
}

// TestBackgroundWorkflow_EventsCarryTheMarkerADriverRoutesBy pins what a Driver keeps a background
// workflow's events apart by (ADR 0089): every phase of it is marked Background, and its item
// child's events carry the synthetic call id the domain names.
func TestBackgroundWorkflow_EventsCarryTheMarkerADriverRoutesBy(t *testing.T) {
	t.Parallel()

	sink := newLockedSink()
	up := (&workflowResponder{}).route("sweep alpha", nil, finishScript("f1", "alpha is fine"))
	a, _ := newPairParent(t, sink, up, nil)
	id := launchBackground(t, a, "pair")
	a.background.waitAll()

	sink.mu.Lock()
	defer sink.mu.Unlock()
	phases, childEvents := 0, 0
	for _, e := range sink.events {
		if event, ok := e.(domain.WorkflowPhaseEvent); ok && event.Workflow == id {
			phases++
			if !event.Background {
				t.Errorf("phase %s of a background workflow is not marked Background", event.Phase)
			}
		}
		if base := e.Identity(); base.Depth > 0 && base.CallID == domain.BackgroundWorkflowCallPrefix+id {
			childEvents++
		}
	}
	if phases == 0 || childEvents == 0 {
		t.Errorf("phases = %d, child events under %q = %d; want both", phases, domain.BackgroundWorkflowCallPrefix+id, childEvents)
	}
}
