package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/eventjson"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/testtime"
)

// Watch mode's suite (watch.go, ADR 0103 decision 5): a model is put into watch over a fixture
// record and a fixture journal written by the real eventjson.Journal, lines are appended while the
// test runs, and each follower read is driven through the Model's own poll message.

// watchSessionID is the fixture session's id: a valid store id, so the record decodes.
const watchSessionID = "20261017T120000Z-abcdef12"

// watchFixture is one watched session on disk: its record and its live journal.
type watchFixture struct {
	t       *testing.T
	src     watchSource
	file    *os.File
	journal *eventjson.Journal
}

// newWatchFixture writes a record holding entries and opens a journal whose frame names title and
// claims the record's first head entries. The journal only flushes deltas on the next event, so
// what a test appends is on disk the moment the call returns.
func newWatchFixture(t *testing.T, title string, entries []session.Entry, head int) *watchFixture {
	t.Helper()
	dir := t.TempDir()
	recordPath := filepath.Join(dir, watchSessionID+".json")
	blob, err := session.EncodeTranscript(entries)
	if err != nil {
		t.Fatalf("EncodeTranscript: %v", err)
	}
	record, err := json.Marshal(session.Record{
		RecordVersion: session.RecordVersion,
		Meta:          session.Meta{ID: watchSessionID, Title: title},
		Transcript:    blob,
	})
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	if err := os.WriteFile(recordPath, record, 0o600); err != nil {
		t.Fatalf("write record: %v", err)
	}
	f := &watchFixture{t: t, src: watchSource{
		recordPath:  recordPath,
		journalPath: filepath.Join(dir, watchSessionID+".events.jsonl"),
		title:       title,
	}}
	f.reopen(title, head)
	return f
}

// reopen truncates the journal and writes a fresh frame into it, as an owner reopening the record
// does (Store.OpenJournal).
func (f *watchFixture) reopen(title string, head int) {
	f.t.Helper()
	file, err := os.Create(f.src.journalPath)
	if err != nil {
		f.t.Fatalf("open journal: %v", err)
	}
	f.t.Cleanup(func() { _ = file.Close() })
	f.file = file
	f.journal = eventjson.NewJournal(file, eventjson.JournalOptions{
		Session: watchSessionID,
		Title:   title,
		Entries: head,
		Window:  time.Hour,
	})
}

// prompt appends a prompt line, top-level or an Interjection.
func (f *watchFixture) prompt(text string, interjection bool) { f.journal.Prompt(text, interjection) }

// emit appends Events.
func (f *watchFixture) emit(events ...domain.Event) {
	for _, e := range events {
		f.journal.Emit(e)
	}
}

// watchEngine is the watcher's own engine: one that records every call a watch must never make, and
// whose getters answer something other than the inert engine's zero values.
func watchEngine() *fakeEngine {
	return &fakeEngine{confine: true, effortOverride: domain.ThinkingEffort("high")}
}

// enterWatchOn builds an idle model over eng, enters watch on f and folds the first read.
func enterWatchOn(t *testing.T, eng *fakeEngine, f *watchFixture) Model {
	t.Helper()
	m := newTestModelEng(t, eng, testOpts)
	m, cmd := m.enterWatch(f.src)
	if !m.watch.isActive || cmd == nil {
		t.Fatal("setup: enterWatch did not enter watch mode")
	}
	return step(t, m, cmd())
}

// pollWatch drives one follower read through the Model's own poll message.
func pollWatch(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := stepCmd(t, m, watchPollMsg{gen: m.watch.gen})
	if cmd == nil {
		t.Fatal("the poll scheduled no read")
	}
	return step(t, m, cmd())
}

// entryTexts lists the texts of the entries of kind.
func entryTexts(m Model, kind entryKind) []string {
	var texts []string
	for _, e := range m.transcript.entries {
		if e.kind == kind {
			texts = append(texts, e.text)
		}
	}
	return texts
}

// recordHeadEntries is a record whose first three entries are the head the journal claims — the
// third an OPEN call, as a progress save stores one — and whose fourth was saved after.
func recordHeadEntries() []session.Entry {
	return []session.Entry{
		{Kind: session.EntryKindUser, Text: "first ask"},
		{Kind: session.EntryKindAssistant, Text: "first answer"},
		{
			Kind:   session.EntryKindToolCall,
			CallID: "c0",
			Tool:   &session.ToolView{Name: "read_file", Args: json.RawMessage(`{"path":"z.go"}`)},
		},
		{Kind: session.EntryKindUser, Text: "saved after the journal was born"},
	}
}

func TestWatchPaintsTheRecordHeadWithoutResumeNotices(t *testing.T) {
	t.Parallel()
	f := newWatchFixture(t, "fix the parser", recordHeadEntries(), 3)

	m := enterWatchOn(t, watchEngine(), f)

	if got := entryTexts(m, entryUser); !slices.Equal(got, []string{"first ask"}) {
		t.Errorf("user entries = %q; want only the record's head", got)
	}
	if got := entryTexts(m, entryAssistant); !slices.Equal(got, []string{"first answer"}) {
		t.Errorf("assistant entries = %q; want the head's answer", got)
	}
	for _, e := range m.transcript.entries {
		if e.kind == entryToolCall && !e.done {
			t.Error("the head's open call paints running; it is closed as a resume closes it")
		}
		if e.kind == entryNote && (strings.HasPrefix(e.text, "resumed:") ||
			e.text == progressSavedNote || e.text == interruptedNote) {
			t.Errorf("a resume notice was painted: %q", e.text)
		}
	}
}

func TestWatchFollowsTheJournalLikeALiveSession(t *testing.T) {
	t.Parallel()
	f := newWatchFixture(t, "fix the parser", nil, 0)
	m := enterWatchOn(t, watchEngine(), f)

	f.prompt("second ask", false)
	turn := domain.EventBase{Turn: 1}
	child := domain.EventBase{Turn: 1, Depth: 1, CallID: "s1", RunID: "run-s1"}
	f.emit(
		domain.ReasoningEvent{EventBase: turn, Text: "Pondering the parser."},
		domain.TokenEvent{EventBase: turn, Text: "Streamed "},
		domain.TokenEvent{EventBase: turn, Text: "answer."},
		domain.MessageEvent{EventBase: turn, Text: "Streamed answer."},
		domain.ToolCallEvent{EventBase: turn, Call: domain.ToolCall{
			ID: "r1", Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`),
		}},
		domain.ToolResultEvent{EventBase: turn, Tool: "read_file", Result: domain.ToolResult{
			CallID: "r1", Content: "[File: a.go, 5 lines total, showing lines 1-5]\n…",
			Summary: domain.ReadSpan{Start: 1, End: 5, Total: 5},
		}},
		domain.ToolCallEvent{EventBase: turn, SpawnRunID: "run-s1", Call: domain.ToolCall{
			ID: "s1", Tool: "sub_agent", Arguments: []byte(`{"task":"work","name":"repo-scout"}`),
		}},
		domain.SubAgentPhaseEvent{EventBase: child, Phase: domain.SubAgentStarted},
	)
	f.prompt("and its tests", true)
	m = pollWatch(t, m)

	t.Run("the prompt lines paint as the owner's user and interjection entries", func(t *testing.T) {
		if got := entryTexts(m, entryUser); !slices.Equal(got, []string{"second ask"}) {
			t.Errorf("user entries = %q", got)
		}
		if got := entryTexts(m, entryInterjected); !slices.Equal(got, []string{"and its tests"}) {
			t.Errorf("interjection entries = %q", got)
		}
	})
	t.Run("streamed text, the tool card with its summary and the run", func(t *testing.T) {
		got := plainTranscript(m)
		if !strings.Contains(got, "Streamed answer.") {
			t.Errorf("the streamed answer is missing:\n%s", got)
		}
		ref := newTestModel(t)
		ref.transcript.reset()
		readCall(&ref.transcript, "r1", "a.go", 1, 5, 0)
		card := strings.TrimSpace(plainTranscript(ref))
		if card == "" || !strings.Contains(got, card) {
			t.Errorf("the read card %q, folded with its summary, is missing:\n%s", card, got)
		}
		if _, ok := runHeadAt(m.transcript.entries, runRef{depth: 1, spawn: "s1", id: "run-s1"}); !ok {
			t.Error("the delegation heads no run a view can open")
		}
	})
	t.Run("the watched run shows running and the box shows the legend", func(t *testing.T) {
		if m.state != stateRunning {
			t.Errorf("state = %v; want running while the watched Exchange is open", m.state)
		}
		if got, want := m.legend(), "watching fix the parser · read-only · esc back"; got != want {
			t.Errorf("legend = %q; want %q", got, want)
		}
	})
	t.Run("thinking reaches /thinking and the transcript holds none of it", func(t *testing.T) {
		m := typeText(t, m, "/thinking")
		m = step(t, m, keyEnter())
		if !m.thinkingPane.open {
			t.Fatal("/thinking did not open its pane while watching")
		}
		var thought string
		for _, rec := range m.scopedThinking() {
			thought += rec.text
		}
		if !strings.Contains(thought, "Pondering the parser.") {
			t.Errorf("the thinking board holds %q; want the watched reasoning", thought)
		}
		if strings.Contains(plainTranscript(m), "Pondering") {
			t.Error("thinking was painted inline")
		}
	})
	t.Run("a depth-0 Turn that ends the Exchange shows the run idle", func(t *testing.T) {
		f.emit(domain.TurnEvent{EventBase: turn, Status: domain.StatusExchangeComplete})
		if m := pollWatch(t, m); m.state != stateIdle {
			t.Errorf("state = %v; want idle once the watched Exchange ended", m.state)
		}
	})
}

func TestWatchGateRefusesEverythingButTheViewVerbs(t *testing.T) {
	t.Parallel()

	t.Run("⏎ at idle flashes and sends nothing", func(t *testing.T) {
		t.Parallel()
		eng := watchEngine()
		m := enterWatchOn(t, eng, newWatchFixture(t, "watched", nil, 0))
		m.input.SetValue("hello")
		before := len(m.transcript.entries)

		m = step(t, m, keyEnter())

		if m.flash != watchRefusedFlash || len(m.transcript.entries) != before || m.input.Value() != "hello" {
			t.Errorf("flash %q, entries %d→%d, box %q; want the refusal flash, no entry and the box kept",
				m.flash, before, len(m.transcript.entries), m.input.Value())
		}
		if len(eng.submitted) != 0 {
			t.Errorf("Submit calls = %v; want none", eng.submitted)
		}
	})
	t.Run("ctrl+g at idle flashes and adds no entry", func(t *testing.T) {
		t.Parallel()
		m := enterWatchOn(t, watchEngine(), newWatchFixture(t, "watched", nil, 0))
		m.input.SetValue("now please")
		before := len(m.transcript.entries)

		m = step(t, m, keyCtrlG())

		if m.flash != watchRefusedFlash || len(m.transcript.entries) != before {
			t.Errorf("flash %q, entries %d→%d; want the refusal and no entry", m.flash, before, len(m.transcript.entries))
		}
	})
	t.Run("a verb typed while the watched run runs flashes and is never queued", func(t *testing.T) {
		t.Parallel()
		f := newWatchFixture(t, "watched", nil, 0)
		m := enterWatchOn(t, watchEngine(), f)
		f.prompt("go", false)
		m = pollWatch(t, m)
		if m.state != stateRunning {
			t.Fatalf("setup: state = %v; want running", m.state)
		}
		m.input.SetValue("/clear")

		m = step(t, m, keyEnter())

		if m.flash != watchRefusedFlash || len(m.deferredCommands) != 0 || len(m.pendingInterjections) != 0 {
			t.Errorf("flash %q, queued %d commands and %d messages; want the refusal and nothing queued",
				m.flash, len(m.deferredCommands), len(m.pendingInterjections))
		}
	})
	t.Run("a dropdown-accepted verb outside the set flashes and is never queued", func(t *testing.T) {
		t.Parallel()
		f := newWatchFixture(t, "watched", nil, 0)
		m := enterWatchOn(t, watchEngine(), f)
		f.prompt("go", false)
		m = pollWatch(t, m)
		m.input.SetValue("note /clear")
		m.input.MoveToEnd()
		m, _ = m.recomputeAutocomplete()
		if !m.autocomplete.active {
			t.Fatal("setup: the menu did not open on the trailing token")
		}

		m = step(t, m, keyTab())

		if m.flash != watchRefusedFlash || len(m.deferredCommands) != 0 {
			t.Errorf("flash %q, queued %d commands; want the refusal and nothing queued", m.flash, len(m.deferredCommands))
		}
	})
	t.Run("the view verbs run, idle and running", func(t *testing.T) {
		t.Parallel()
		f := newWatchFixture(t, "watched", nil, 0)
		m := enterWatchOn(t, watchEngine(), f)
		m.input.SetValue("/version")
		if m = step(t, m, keyEnter()); m.flash == watchRefusedFlash {
			t.Error("/version was refused while watching")
		}
		f.prompt("go", false)
		m = pollWatch(t, m)
		m = step(t, typeText(t, m, "/thinking"), keyEnter())
		if !m.thinkingPane.open || m.flash == watchRefusedFlash {
			t.Error("/thinking did not run while the watched run runs")
		}
	})
}

func TestWatchStripsEscapesFromWhatItReads(t *testing.T) {
	t.Parallel()
	f := newWatchFixture(t, "\x1b[2Jtitled", nil, 0)
	m := enterWatchOn(t, watchEngine(), f)

	f.prompt("ask \x1b[31mred", false)
	turn := domain.EventBase{Turn: 1}
	f.emit(
		domain.TokenEvent{EventBase: turn, Text: "tok\x1b]0;evil\x07en"},
		domain.MessageEvent{EventBase: turn, Text: "tok\x1b]0;evil\x07en"},
	)
	m = pollWatch(t, m)

	for _, got := range []string{m.legend(), strings.Join(entryTexts(m, entryUser), ""), plainTranscript(m)} {
		if strings.ContainsAny(got, "\x1b\x07") {
			t.Errorf("an escape reached the view: %q", got)
		}
	}
	if got := m.legend(); !strings.Contains(got, "watching titled") {
		t.Errorf("legend = %q; want the stripped title", got)
	}
}

func TestWatchKeepsBackgroundWorkflowEventsOutOfTheConversation(t *testing.T) {
	t.Parallel()
	f := newWatchFixture(t, "watched", nil, 0)
	m := enterWatchOn(t, watchEngine(), f)
	before := len(m.transcript.entries)

	f.emit(domain.WorkflowPhaseEvent{
		Phase: domain.WorkflowStarted, Workflow: "wf-1", Name: "nightly", Background: true,
	})
	m = pollWatch(t, m)

	if len(m.transcript.entries) != before {
		t.Errorf("entries %d→%d; a background workflow's event was painted inline", before, len(m.transcript.entries))
	}
	if _, ok := m.workflows.live["wf-1"]; !ok {
		t.Error("the background workflow did not reach the watch's own workflow state")
	}
}

func TestWatchLeavesTheFooterUnchanged(t *testing.T) {
	t.Parallel()
	f := newWatchFixture(t, "watched", nil, 0)
	opts := testOpts
	opts.Mode = domain.ModeAuto
	m := newTestModelEng(t, watchEngine(), opts)
	m.hb.effort = domain.EffortSupport{Supported: true}
	footer := func(m Model) string {
		return m.footerLeftText().effort + "|" + confinementWord(m.opts.Confinement, m.opts.Mode, m.eng.ConfineToWorkspace())
	}
	before := footer(m)

	m, _ = m.enterWatch(f.src)

	if got := footer(m); got != before || before == "|" {
		t.Errorf("footer facts %q across enter; want %q unchanged (and non-empty)", got, before)
	}
}

func TestWatchRepaintsOnTruncation(t *testing.T) {
	t.Parallel()
	f := newWatchFixture(t, "watched", nil, 0)
	m := enterWatchOn(t, watchEngine(), f)
	f.prompt("before the owner reopened the record, a long prompt line", false)
	m = pollWatch(t, m)

	f.reopen("again", 0)
	f.prompt("after", false)
	m = pollWatch(t, m)

	if got := entryTexts(m, entryUser); !slices.Equal(got, []string{"after"}) {
		t.Errorf("user entries = %q; want only the reopened journal's", got)
	}
	if got := m.legend(); !strings.Contains(got, "watching again") {
		t.Errorf("legend = %q; want the reopened journal's title", got)
	}
}

func TestWatchRefusesTheStopKeyInRunViewsAndTheBlockCursor(t *testing.T) {
	t.Parallel()
	watchedRun := func(t *testing.T, eng *fakeEngine) Model {
		t.Helper()
		f := newWatchFixture(t, "watched", nil, 0)
		m := enterWatchOn(t, eng, f)
		f.prompt("survey the repo", false)
		f.emit(
			domain.ToolCallEvent{EventBase: domain.EventBase{Turn: 1}, SpawnRunID: "run-s1", Call: domain.ToolCall{
				ID: "s1", Tool: "sub_agent", Arguments: []byte(`{"task":"work","name":"repo-scout"}`),
			}},
			domain.SubAgentPhaseEvent{
				EventBase: domain.EventBase{Turn: 1, Depth: 1, CallID: "s1", RunID: "run-s1"},
				Phase:     domain.SubAgentStarted,
			},
		)
		return pollWatch(t, m)
	}

	t.Run("in a run view", func(t *testing.T) {
		t.Parallel()
		eng := watchEngine()
		m := enterOnLastBlock(t, watchedRun(t, eng))
		if !m.inRunView() {
			t.Fatal("setup: ⏎ on the watched delegation opened no run view")
		}

		m = step(t, m, keyCtrlX())

		if got := eng.stoppedChildren(); len(got) != 0 {
			t.Errorf("StopChild calls = %v; want none", got)
		}
		if m.flash != watchRefusedFlash || !m.inRunView() {
			t.Errorf("flash %q, in view %v; want the refusal and the view still open", m.flash, m.inRunView())
		}
	})
	t.Run("on the block cursor's run row", func(t *testing.T) {
		t.Parallel()
		eng := watchEngine()
		m := cursorOnRun(t, watchedRun(t, eng), "run-s1")

		m = step(t, m, keyCtrlX())

		if got := eng.stoppedChildren(); len(got) != 0 {
			t.Errorf("StopChild calls = %v; want none", got)
		}
		if m.flash != watchRefusedFlash {
			t.Errorf("flash = %q; want the refusal", m.flash)
		}
	})
}

func TestWatchMakesNoEngineCallAndEscGoesBack(t *testing.T) {
	t.Parallel()
	eng := watchEngine()
	f := newWatchFixture(t, "watched", nil, 0)
	m := newTestModelEng(t, eng, testOpts)
	m.transcript.addUser("my own question", nil)
	m, cmd := m.enterWatch(f.src)
	m = step(t, m, cmd())
	f.prompt("theirs", false)
	f.emit(domain.TokenEvent{EventBase: domain.EventBase{Turn: 1}, Text: "working"},
		domain.MessageEvent{EventBase: domain.EventBase{Turn: 1}, Text: "working"})
	m = pollWatch(t, m)
	if _, isInert := m.eng.(inertEngine); !isInert {
		t.Fatalf("m.eng is %T while watching; want the inert engine", m.eng)
	}
	m.input.SetValue("try to send")
	m = step(t, m, keyEnter())
	m = step(t, m, keyCtrlG())

	m = step(t, m, keyEsc())

	if m.watch.isActive || m.eng != Engine(eng) {
		t.Fatal("esc did not leave the watch for the parked engine")
	}
	if got := entryTexts(m, entryUser); !slices.Equal(got, []string{"my own question"}) {
		t.Errorf("user entries after esc = %q; want the watcher's own session back", got)
	}
	if m.state != stateIdle {
		t.Errorf("state after esc = %v; want the parked idle", m.state)
	}
	if len(eng.submitted) != 0 || eng.stepCalls != 0 || eng.clearCalls != 0 || len(eng.interjected) != 0 ||
		len(eng.childStops) != 0 || len(eng.modeSet) != 0 || eng.compactCalls != 0 || len(eng.restoreCalls) != 0 {
		t.Errorf("the watcher's engine was driven during the watch: %+v", eng)
	}
}

func TestWatchDropsBatchesAfterEsc(t *testing.T) {
	t.Parallel()
	f := newWatchFixture(t, "watched", nil, 0)
	m := newTestModelEng(t, watchEngine(), testOpts)
	m, cmd := m.enterWatch(f.src)
	f.prompt("theirs", false)
	late := cmd() // the read was in flight when esc landed

	m = step(t, step(t, m, keyEsc()), late)

	if got := entryTexts(m, entryUser); len(got) != 0 {
		t.Errorf("user entries = %q; a read for a watch already left was folded", got)
	}
}

func TestWatchFollowerPollsOnItsOwn(t *testing.T) {
	t.Parallel()
	f := newWatchFixture(t, "watched", nil, 0)
	m := enterWatchOn(t, watchEngine(), f)

	_, cmd := stepCmd(t, m, watchBatchMsg{gen: m.watch.gen, offset: m.watch.offset, file: m.watch.file})
	polled := make(chan bool, 1)
	go func() { polled <- containsPoll(cmd(), m.watch.gen) }()

	var isPolled bool
	testtime.Eventually(t, testtime.Budget(5*time.Second), func() bool {
		select {
		case isPolled = <-polled:
			return true
		default:
			return false
		}
	}, "the follower's next poll")
	if !isPolled {
		t.Error("a folded batch scheduled no next poll for this watch")
	}
}

// containsPoll reports whether msg — a batch of Cmds' messages, run one by one — holds this watch's
// poll. Only the poll's tick is run to completion; it is the one Cmd the batch must carry.
func containsPoll(msg tea.Msg, gen int) bool {
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		poll, isPoll := msg.(watchPollMsg)
		return isPoll && poll.gen == gen
	}
	for _, cmd := range batch {
		if cmd == nil {
			continue
		}
		if poll, isPoll := cmd().(watchPollMsg); isPoll && poll.gen == gen {
			return true
		}
	}
	return false
}
