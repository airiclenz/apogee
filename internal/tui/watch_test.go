package tui

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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
		id:          watchSessionID,
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

	_, cmd := stepCmd(t, m, watchBatchMsg{gen: m.watch.gen, offset: m.watch.offset, journal: m.watch.journal})
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

// ----------------------------------------------------------------------------
// Isolating the watcher's own session (item 9a)
// ----------------------------------------------------------------------------

// watchedTexts are the texts the isolation fixtures put in the watched session: none of them may
// ever reach the watcher's own record.
var watchedTexts = []string{"first ask", "first answer", "saved after the journal was born", "theirs"}

// isolationFixture is a watched session whose record head and journal both carry watchedTexts.
func isolationFixture(t *testing.T) *watchFixture {
	t.Helper()
	f := newWatchFixture(t, "watched", recordHeadEntries(), 3)
	f.prompt("theirs", false)
	return f
}

// ownerSession builds the watcher's own idle session over eng and host, with one exchange of its own
// in the transcript and a quiescent boundary cached, so a progress save has an engine half to pair.
func ownerSession(t *testing.T, eng *fakeEngine, host *fakeSessionHost) Model {
	t.Helper()
	m := newSessionModel(t, eng, host)
	m.transcript.addUser("my own question", nil)
	m.cacheBoundary(domain.Session{})
	return m
}

// enterAndFollow enters watch on f over m and folds the first read and one poll.
func enterAndFollow(t *testing.T, m Model, f *watchFixture) Model {
	t.Helper()
	m, cmd := m.enterWatch(f.src)
	if !m.watch.isActive {
		t.Fatal("setup: enterWatch did not enter watch mode")
	}
	return pollWatch(t, step(t, m, cmd()))
}

// assertSavesCarryOnlyTheOwner fails when a save the host took carries a watched entry, or misses
// the owner's own question.
func assertSavesCarryOnlyTheOwner(t *testing.T, host *fakeSessionHost) {
	t.Helper()
	host.mu.Lock()
	defer host.mu.Unlock()
	for i, save := range host.saves {
		entries, err := decodeTranscript(save.transcript)
		if err != nil {
			t.Fatalf("save %d: decode: %v", i, err)
		}
		var texts []string
		for _, e := range entries {
			texts = append(texts, e.text)
		}
		if !slices.Contains(texts, "my own question") {
			t.Errorf("save %d carries %q; want the owner's own question", i, texts)
		}
		for _, watched := range watchedTexts {
			if slices.Contains(texts, watched) {
				t.Errorf("save %d carries the watched entry %q: %q", i, watched, texts)
			}
		}
	}
}

// assertNoRetargets fails when the host was asked to rename, fork or rotate anything.
func assertNoRetargets(t *testing.T, host *fakeSessionHost) {
	t.Helper()
	host.mu.Lock()
	defer host.mu.Unlock()
	if len(host.renames) != 0 || len(host.forks) != 0 || host.rotates != 0 {
		t.Errorf("host renames=%v forks=%v rotates=%d; want none", host.renames, host.forks, host.rotates)
	}
}

// countToolCalls counts the transcript's tool-call entries.
func countToolCalls(m Model) int {
	n := 0
	for _, e := range m.transcript.entries {
		if e.kind == entryToolCall {
			n++
		}
	}
	return n
}

func TestWatchIsolatesOwnerEventsInTheParkedSession(t *testing.T) {
	t.Parallel()
	host := &fakeSessionHost{}
	m := enterAndFollow(t, ownerSession(t, watchEngine(), host), isolationFixture(t))
	watchedCalls := countToolCalls(m)
	delegation := domain.ToolCallEvent{EventBase: domain.EventBase{Turn: 1}, Call: domain.ToolCall{
		ID: "own-s1", Tool: subAgentToolName, Arguments: []byte(`{"task":"owner work"}`),
	}}

	m, cmd := stepCmd(t, m, eventMsg{Event: delegation})
	m = runWrites(t, m, cmd)

	if got := countToolCalls(m); got != watchedCalls {
		t.Errorf("tool calls in the watch view = %d, want %d: the owner's event reached the view", got, watchedCalls)
	}
	if !m.watch.isActive {
		t.Fatal("the owner's event ended the watch")
	}
	host.mu.Lock()
	saves := len(host.saves)
	host.mu.Unlock()
	if saves == 0 {
		t.Error("the owner's delegation made no progress save; the parked session's record fell behind")
	}
	assertSavesCarryOnlyTheOwner(t, host)

	m = step(t, m, keyEsc())

	if got := countToolCalls(m); got != 1 {
		t.Errorf("tool calls after esc = %d, want the owner's one delegation", got)
	}
	if got := entryTexts(m, entryUser); !slices.Equal(got, []string{"my own question"}) {
		t.Errorf("user entries after esc = %q; want only the owner's", got)
	}
	assertNoRetargets(t, host)
}

func TestWatchIsolatesQuitFromTheWatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		isRunning bool
		isFresh   bool
	}{
		{name: "idle", isRunning: false},
		{name: "while the watched run is running", isRunning: true},
		{name: "a fresh owner session", isFresh: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host := &fakeSessionHost{}
			var m Model
			if tc.isFresh {
				m = newSessionModel(t, watchEngine(), host)
			} else {
				m = ownerSession(t, watchEngine(), host)
			}
			f := isolationFixture(t)
			if !tc.isRunning {
				f.emit(domain.TurnEvent{EventBase: domain.EventBase{Turn: 1}, Status: domain.StatusExchangeComplete})
			}
			m = enterAndFollow(t, m, f)
			if got := m.state == stateRunning; got != tc.isRunning {
				t.Fatalf("setup: watched run running = %v, want %v", got, tc.isRunning)
			}

			m = step(t, m, keyCtrlC())
			m, cmd := stepCmd(t, m, keyCtrlC())

			if m.watch.isActive {
				t.Fatal("ctrl+c quit without leaving the watch first")
			}
			m = drainToQuit(t, m, cmd)
			host.mu.Lock()
			saves := len(host.saves)
			host.mu.Unlock()
			if tc.isFresh && saves != 0 {
				t.Errorf("a fresh owner session saved %d records on quit; want none", saves)
			}
			if !tc.isFresh && saves == 0 {
				t.Error("the owner's own session was not flushed on quit")
			}
			assertSavesCarryOnlyTheOwner(t, host)
			assertNoRetargets(t, host)
		})
	}
}

func TestWatchIsolatesTheEngineSetters(t *testing.T) {
	t.Parallel()
	eng := watchEngine()
	m := newTestModelEng(t, eng, testOpts)
	modeBefore := m.opts.Mode
	m = enterAndFollow(t, m, isolationFixture(t))
	refused := func(name string, m Model) {
		t.Helper()
		if m.flash != watchRefusedFlash {
			t.Errorf("%s: flash = %q, want the watch refusal", name, m.flash)
		}
	}

	m = step(t, m, keyShiftTab())
	refused("shift+tab", m)
	m.flash = ""
	m.picker = picker{open: true, kind: pickerMode}
	m = step(t, m, keyEnter())
	refused("the mode picker's accept", m)
	m.picker = picker{}
	for _, line := range []string{"/mode auto", "/effort high", "/confine off", "/model other", "/compact", "/clear"} {
		m.flash = ""
		m.input.SetValue(line)
		m = step(t, m, keyEnter())
		refused(line, m)
	}
	m = step(t, m, keyEsc())

	if m.opts.Mode != modeBefore {
		t.Errorf("opts.Mode = %q after the watch, want %q", m.opts.Mode, modeBefore)
	}
	if len(eng.modeSet) != 0 || len(eng.effortSet) != 0 || len(eng.confineSet) != 0 ||
		eng.compactCalls != 0 || eng.clearCalls != 0 {
		t.Errorf("an engine setter reached the watcher's engine: modes=%v efforts=%v confines=%v compacts=%d clears=%d",
			eng.modeSet, eng.effortSet, eng.confineSet, eng.compactCalls, eng.clearCalls)
	}
}

func TestWatchIsolatesARecordWriteInFlightAtEntry(t *testing.T) {
	t.Parallel()
	host := &fakeSessionHost{}
	m := ownerSession(t, watchEngine(), host)
	m, first := stepCmd(t, m, turnSnapshotMsg{Sess: domain.Session{}})
	m, queued := stepCmd(t, m, turnSnapshotMsg{Sess: domain.Session{}})
	if first == nil || queued != nil || len(m.pendingWrites) != 1 {
		t.Fatalf("setup: want one save in flight and one queued, got first=%v queued=%v pending=%d",
			first != nil, queued != nil, len(m.pendingWrites))
	}

	m = enterAndFollow(t, m, isolationFixture(t))
	m = runWrites(t, m, first)

	host.mu.Lock()
	saves := len(host.saves)
	host.mu.Unlock()
	if saves != 2 || m.writeBusy || len(m.pendingWrites) != 0 {
		t.Errorf("saves = %d busy=%v pending=%d; want both writes landed through the parked host",
			saves, m.writeBusy, len(m.pendingWrites))
	}
	assertSavesCarryOnlyTheOwner(t, host)
	if !m.watch.isActive {
		t.Error("the landed writes ended the watch")
	}
}

func TestWatchIsolatesOwnerStateChangesUntilEsc(t *testing.T) {
	t.Parallel()
	t.Run("a generated title names the owner's session", func(t *testing.T) {
		t.Parallel()
		m := enterAndFollow(t, ownerSession(t, watchEngine(), &fakeSessionHost{}), isolationFixture(t))

		m = step(t, m, autoTitleMsg{title: "Named by the model"})

		if m.sessionName != "" {
			t.Errorf("watch view sessionName = %q; the owner's title reached the view", m.sessionName)
		}
		m = step(t, m, keyEsc())
		if m.sessionName != "Named by the model" {
			t.Errorf("owner sessionName after esc = %q, want the generated title", m.sessionName)
		}
	})
	t.Run("a beat rebind lands on the owner's transcript", func(t *testing.T) {
		t.Parallel()
		rb := &fakeRebind{}
		m := wireRebind(t, testOpts, &fakeHeartbeat{}, rb)
		m = foldBeatMsg(t, m, upBeat("test-model", 32768))
		m = enterAndFollow(t, m, isolationFixture(t))
		changed := "model changed: test-model → new-model, context 32k → 16k"

		m = foldBeatMsg(t, m, upBeat("new-model", 16384))

		if len(rb.calls) != 2 {
			t.Fatalf("rebind calls = %+v; want the beat during the watch applied", rb.calls)
		}
		if n := countNotes(m, changed); n != 0 {
			t.Error("the owner's rebind note was painted into the watch view")
		}
		m = step(t, m, keyEsc())
		if n := countNotes(m, changed); n != 1 {
			t.Errorf("change notes after esc = %d, want 1; notes = %q", n, noteTexts(m))
		}
	})
	t.Run("a config reload lands on the owner's transcript", func(t *testing.T) {
		t.Parallel()
		rows := []SettingRow{{Path: "context-files.names", Section: "System prompt", Kind: SettingString,
			Value: "[AGENTS.md]", Editable: true}}
		edit := &externalEditLog{applied: []AppliedSetting{{Path: "context-files.names", Value: "[CLAUDE.md]"}}}
		m := enterAndFollow(t, configWatchModel(t, rows, &settingsWriteLog{}, edit), isolationFixture(t))

		m = step(t, m, configChangedMsg{alive: true})

		if edit.reloads != 1 {
			t.Fatalf("reloads = %d; want the report during the watch applied", edit.reloads)
		}
		if got := appliedNotes(m); len(got) != 0 {
			t.Errorf("the owner's reload note was painted into the watch view: %q", got)
		}
		m = step(t, m, keyEsc())
		if got := appliedNotes(m); len(got) != 1 {
			t.Errorf("applied notes after esc = %q, want the reload's one line", got)
		}
	})
}

func TestWatchIsolatesAPendingWakeUntilEsc(t *testing.T) {
	t.Parallel()
	eng := watchEngine()
	m := newTestModelEng(t, eng, testOpts)
	// Pending before the watch starts: enterWatch is called directly, so no fold can take it first.
	m.wakePending = true
	f := isolationFixture(t)
	f.emit(domain.TurnEvent{EventBase: domain.EventBase{Turn: 1}, Status: domain.StatusExchangeComplete})

	m, cmd := m.enterWatch(f.src)
	m = pollWatch(t, step(t, m, cmd()))
	if m.state != stateIdle {
		t.Fatalf("setup: watched run state = %v, want idle so only the watch holds the wake", m.state)
	}

	if !m.wakePending || eng.wakeCalls != 0 {
		t.Fatalf("wakePending=%v wakeCalls=%d during the watch; want the wake held", m.wakePending, eng.wakeCalls)
	}
	m = step(t, m, keyEsc())
	if eng.wakeCalls != 1 {
		t.Errorf("wake calls after esc = %d, want the held wake delivered to the owner's engine", eng.wakeCalls)
	}
}

func TestWatchIsolatesThePaintsOfTheRestoredTranscript(t *testing.T) {
	t.Parallel()
	m := newTestModelEng(t, watchEngine(), testOpts)
	m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 80})
	// The owner's user entry sits at index 3, where the watched transcript paints its own "theirs":
	// a cache the two shared would hand one's paint to the other (paintcache.go keys by index).
	m.transcript.addNote("my own note")
	m.transcript.addNote("my second note")
	m.transcript.addUser("my own question", nil)
	_ = m.View() // warm the owner's paint cache at the indices the watch will reuse

	m = enterAndFollow(t, m, isolationFixture(t))
	_ = m.View()
	m = step(t, m, keyEsc())

	screen := plain(m.View())
	for _, want := range []string{"my own question", "my own note", "my second note"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the restored view is missing %q:\n%s", want, screen)
		}
	}
	for _, watched := range watchedTexts {
		if strings.Contains(screen, watched) {
			t.Errorf("the restored view paints the watched %q:\n%s", watched, screen)
		}
	}
}

// packageMethods parses this package's production files and returns its Model methods by name.
func packageMethods(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	methods := map[string]*ast.FuncDecl{}
	fset := token.NewFileSet()
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv != nil && strings.TrimPrefix(types.ExprString(fn.Recv.List[0].Type), "*") == "Model" {
				methods[fn.Name.Name] = fn
			}
		}
	}
	return methods
}

// receiverName is the name a Model method calls its receiver.
func receiverName(fn *ast.FuncDecl) string {
	return fn.Recv.List[0].Names[0].Name
}

// modelField reports the Model field an expression is rooted at — m.transcript.paints is
// "transcript" — or "" when it is not rooted at the receiver's field.
func modelField(expr ast.Expr, receiver string) string {
	for {
		switch e := expr.(type) {
		case *ast.SelectorExpr:
			if id, ok := e.X.(*ast.Ident); ok && id.Name == receiver {
				return e.Sel.Name
			}
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.StarExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		default:
			return ""
		}
	}
}

// fieldsWritten is the set of Model fields method writes — assigns, or calls a method on — and so
// does every Model method it calls, transitively. A field it only reads is not in the set.
func fieldsWritten(methods map[string]*ast.FuncDecl, name string, seen map[string]bool) map[string]bool {
	written := map[string]bool{}
	fn, ok := methods[name]
	if !ok || seen[name] {
		return written
	}
	seen[name] = true
	receiver := receiverName(fn)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				if field := modelField(lhs, receiver); field != "" {
					written[field] = true
				}
			}
		case *ast.IncDecStmt:
			if field := modelField(n.X, receiver); field != "" {
				written[field] = true
			}
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == receiver {
				for field := range fieldsWritten(methods, sel.Sel.Name, seen) {
					written[field] = true
				}
				return true
			}
			if field := modelField(sel.X, receiver); field != "" {
				written[field] = true
			}
		}
		return true
	})
	return written
}

// fieldsRead is the set of Model fields method names anywhere in its body.
func fieldsRead(methods map[string]*ast.FuncDecl, name string) map[string]bool {
	read := map[string]bool{}
	fn := methods[name]
	receiver := receiverName(fn)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == receiver {
				read[sel.Sel.Name] = true
			}
		}
		return true
	})
	return read
}

// sortedKeys lists a set in order, for a readable failure.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func TestWatchIsolatesParksExactlyTheSessionViewResetSet(t *testing.T) {
	t.Parallel()
	methods := packageMethods(t)
	want := fieldsWritten(methods, "resetSessionView", map[string]bool{})
	// The engine resetSessionView asks for the context files is parked whole, as watch.owner; the
	// run state the watch moves is parked beside the reset set.
	delete(want, "eng")
	for _, moved := range []string{"state", "acts", "cursor"} {
		want[moved] = true
	}

	parked := fieldsRead(methods, "parkSession")
	restored := fieldsWritten(methods, "restoreSession", map[string]bool{})

	if !maps.Equal(parked, want) {
		t.Errorf("parkSession parks %v;\nwant resetSessionView's set plus the run state %v", sortedKeys(parked), sortedKeys(want))
	}
	if !maps.Equal(restored, want) {
		t.Errorf("restoreSession restores %v;\nwant %v", sortedKeys(restored), sortedKeys(want))
	}
}

// switchCases lists the case types of the first type switch in method's body.
func switchCases(t *testing.T, fn *ast.FuncDecl) map[string]bool {
	t.Helper()
	cases := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sw, ok := n.(*ast.TypeSwitchStmt)
		if !ok || len(cases) > 0 {
			return len(cases) == 0
		}
		for _, stmt := range sw.Body.List {
			for _, expr := range stmt.(*ast.CaseClause).List {
				cases[types.ExprString(expr)] = true
			}
		}
		return false
	})
	return cases
}

func TestWatchIsolatesRoutesEveryFoldedMsg(t *testing.T) {
	t.Parallel()
	methods := packageMethods(t)
	folded := switchCases(t, methods["fold"])
	if len(folded) == 0 {
		t.Fatal("found no case in Model.fold")
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "watch.go", nil, 0)
	if err != nil {
		t.Fatalf("parse watch.go: %v", err)
	}
	var routed map[string]bool
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "watchRouteOf" {
			routed = switchCases(t, fn)
		}
	}

	if !maps.Equal(folded, routed) {
		t.Errorf("watchRouteOf routes %v;\nwant every Msg Model.fold folds: %v", sortedKeys(routed), sortedKeys(folded))
	}
}

func TestWatchIsolatesHoldsTheOwnersSurfacesUntilEsc(t *testing.T) {
	t.Parallel()
	host := &fakeSessionHost{}
	m := enterAndFollow(t, ownerSession(t, watchEngine(), host), isolationFixture(t))
	listing := sessionListMsg{metas: []session.Meta{{ID: "s1", Title: "an older task"}}}

	m = step(t, m, listing)

	if m.sessionBrowser.open {
		t.Fatal("the owner's /sessions listing opened over the watch view")
	}
	m, cmd := stepCmd(t, m, keyEsc())
	if m.sessionBrowser.open {
		t.Fatal("the held listing opened before it was delivered again")
	}
	for _, msg := range expandBatch(cmd) {
		m = step(t, m, msg)
	}
	if !m.sessionBrowser.open {
		t.Error("the held listing was lost on the way out of the watch")
	}
}

// ----------------------------------------------------------------------------
// The owner ends (item 10)
// ----------------------------------------------------------------------------

// fakeProbe is a watch's hold probe the test answers: held until free is called.
type fakeProbe struct {
	isFree atomic.Bool
	calls  atomic.Int32
}

// probe answers the watch's question (holdProbe).
func (p *fakeProbe) probe() (bool, error) {
	p.calls.Add(1)
	return !p.isFree.Load(), nil
}

// free lets the session go, as an owner releasing its hold does.
func (p *fakeProbe) free() { p.isFree.Store(true) }

// probedFixture is a watched session whose hold a fakeProbe answers.
func probedFixture(t *testing.T) (*watchFixture, *fakeProbe) {
	t.Helper()
	f := newWatchFixture(t, "watched", recordHeadEntries(), 3)
	probe := &fakeProbe{}
	f.src.probe = probe.probe
	return f, probe
}

// finish closes the journal with run_finished, as an owner's release does before it lets go.
func (f *watchFixture) finish() { f.journal.Close() }

// probeWatch drives the follower through its next probing read: the probe rides every
// watchProbeEvery-th read, the first at entry included.
func probeWatch(t *testing.T, m Model) Model {
	t.Helper()
	for range m.watch.readsUntilProbe + 1 {
		m = pollWatch(t, m)
	}
	return m
}

// ownerHostFor is the watcher's own session host with the watched record stored in it, so the
// normal door can load it.
func ownerHostFor(t *testing.T, f *watchFixture) *fakeSessionHost {
	t.Helper()
	rec, err := session.NewStore(filepath.Dir(f.src.recordPath)).LoadPath(f.src.recordPath)
	if err != nil {
		t.Fatalf("read the fixture record: %v", err)
	}
	host := &fakeSessionHost{}
	host.seed(rec)
	return host
}

// endWatchedOwner enters watch on f over the watcher's own session, follows it, finishes the
// journal when isFinished, frees the probe and drives the follower to the probe that reads it.
func endWatchedOwner(t *testing.T, m Model, f *watchFixture, probe *fakeProbe, isFinished bool) Model {
	t.Helper()
	m = enterAndFollow(t, m, f)
	if isFinished {
		f.finish()
	}
	probe.free()
	return probeWatch(t, m)
}

func TestWatchOwnerFinishedResumesInTheOwnHome(t *testing.T) {
	t.Parallel()
	f, probe := probedFixture(t)
	eng := watchEngine()
	m := endWatchedOwner(t, ownerSession(t, eng, ownerHostFor(t, f)), f, probe, true)
	if want := fmt.Sprintf(watchResumableLegendFormat, "watched", watchFinishedBanner); m.legend() != want {
		t.Fatalf("legend = %q; want the finished banner with the ⏎ that resumes, %q", m.legend(), want)
	}

	m, cmd := stepCmd(t, m, keyEnter())

	if m.watch.isActive || m.eng != Engine(eng) {
		t.Fatal("⏎ did not leave the watch for the watcher's own engine before the load")
	}
	if cmd == nil {
		t.Fatal("⏎ on a finished watch started no load")
	}
	loaded, ok := cmd().(sessionLoadedMsg)
	if !ok || loaded.err != nil {
		t.Fatalf("the resume's Cmd answered %#v; want the record loaded", loaded)
	}
	m = step(t, m, loaded)
	if restores := eng.restoreCalls; len(restores) != 1 {
		t.Errorf("RestoreSession calls = %d; want the watched record restored once", len(restores))
	}
	if !slices.Contains(entryTexts(m, entryUser), "first ask") {
		t.Error("the resumed session did not repaint the record's own scrollback")
	}
}

func TestWatchOwnerFinishedInAWatchedHomeStaysRefused(t *testing.T) {
	t.Parallel()
	f, probe := probedFixture(t)
	f.src.isForeign = true
	eng := watchEngine()
	m := endWatchedOwner(t, ownerSession(t, eng, ownerHostFor(t, f)), f, probe, true)

	m, cmd := stepCmd(t, m, keyEnter())

	if want := fmt.Sprintf(watchEndedLegendFormat, "watched", watchFinishedBanner); m.legend() != want {
		t.Errorf("legend = %q; want the finished banner with no resume, %q", m.legend(), want)
	}
	if !m.watch.isActive || m.flash != watchRefusedFlash {
		t.Errorf("active %v, flash %q; want ⏎ refused with the watch kept", m.watch.isActive, m.flash)
	}
	if cmd != nil {
		if _, isLoad := cmd().(sessionLoadedMsg); isLoad {
			t.Error("⏎ in a watched home loaded the record")
		}
	}
	if len(eng.restoreCalls) != 0 {
		t.Errorf("RestoreSession calls = %d; want none", len(eng.restoreCalls))
	}
}

func TestWatchOwnerGoneShowsTheBannerAndStaysReadOnly(t *testing.T) {
	t.Parallel()
	f, probe := probedFixture(t)
	eng := watchEngine()
	m := endWatchedOwner(t, ownerSession(t, eng, ownerHostFor(t, f)), f, probe, false)

	if want := fmt.Sprintf(watchEndedLegendFormat, "watched", watchOwnerGoneBanner); m.legend() != want {
		t.Errorf("legend = %q; want the owner-gone banner, %q", m.legend(), want)
	}
	if m.state != stateIdle {
		t.Errorf("state = %v; want the vanished owner's run shown idle", m.state)
	}
	if _, read := stepCmd(t, m, watchPollMsg{gen: m.watch.gen}); read != nil {
		t.Error("the follower read on after the owner's end")
	}
	calls := probe.calls.Load()

	m = step(t, m, keyEnter())

	if !m.watch.isActive || m.flash != watchRefusedFlash || len(eng.restoreCalls) != 0 {
		t.Errorf("active %v, flash %q, restores %d; want ⏎ refused and the watch kept",
			m.watch.isActive, m.flash, len(eng.restoreCalls))
	}
	if probe.calls.Load() != calls {
		t.Error("the probe ran on after the owner's end")
	}
}

func TestWatchOwnerFinishedIsReadFromARemovedJournal(t *testing.T) {
	t.Parallel()
	f := newWatchFixture(t, "watched", recordHeadEntries(), 3)
	dir := filepath.Dir(f.src.recordPath)
	owner := session.NewStore(dir)
	release, err := owner.Hold(watchSessionID)
	if err != nil {
		t.Fatalf("the owner's Hold: %v", err)
	}
	watcher := session.NewStore(dir)
	f.src.probe = func() (bool, error) {
		isHeld, _, err := watcher.ProbeHold(watchSessionID)
		return isHeld, err
	}
	m := enterAndFollow(t, ownerSession(t, watchEngine(), ownerHostFor(t, f)), f)

	// The owner's release, in its order: the last words, the closing frame, the journal removed,
	// and only then the hold let go — all before the follower reads again.
	f.emit(domain.TokenEvent{EventBase: domain.EventBase{Turn: 1}, Text: "last words"},
		domain.MessageEvent{EventBase: domain.EventBase{Turn: 1}, Text: "last words"})
	f.finish()
	if err := f.file.Close(); err != nil {
		t.Fatalf("close the journal: %v", err)
	}
	if err := owner.RemoveJournal(watchSessionID); err != nil {
		t.Fatalf("RemoveJournal: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	m = probeWatch(t, m)

	if want := fmt.Sprintf(watchResumableLegendFormat, "watched", watchFinishedBanner); m.legend() != want {
		t.Errorf("legend = %q; want the finished banner, %q", m.legend(), want)
	}
	if !slices.Contains(entryTexts(m, entryAssistant), "last words") {
		t.Error("the lines written before the removal were not drained")
	}
}

func TestWatchOwnerResumeAsksTheSwitchConfirmWithAWorkflowRunning(t *testing.T) {
	t.Parallel()
	f, probe := probedFixture(t)
	eng := watchEngine()
	m := ownerSession(t, eng, ownerHostFor(t, f))
	m.workflows.live = map[string]backgroundWorkflow{"wf-own": {name: "nightly"}}
	m = endWatchedOwner(t, m, f, probe, true)

	m, cmd := stepCmd(t, m, keyEnter())

	if m.watch.isActive {
		t.Fatal("⏎ did not leave the watch before asking")
	}
	if !m.boundaryConfirmOpen() || m.picker.boundary.kind != boundarySwitch ||
		m.picker.boundary.sessionID != watchSessionID {
		t.Fatalf("picker %+v; want the switch confirm for %s", m.picker, watchSessionID)
	}
	if cmd != nil {
		if _, isLoad := cmd().(sessionLoadedMsg); isLoad {
			t.Error("the load ran before the confirm was answered")
		}
	}
}

// holdingHost is a session host whose Load takes the real live-instance hold, as the production
// host's does (Live.Park), so a resume meets the lock a watcher's probe shares.
type holdingHost struct {
	*fakeSessionHost
	store    *session.Store
	mu       sync.Mutex
	releases []func() error
}

// Load reads the record and holds it.
func (h *holdingHost) Load(id string) (session.Record, error) {
	rec, err := h.store.Load(id)
	if err != nil {
		return session.Record{}, err
	}
	release, err := h.store.Hold(id)
	if err != nil {
		return session.Record{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.releases = append(h.releases, release)
	return rec, nil
}

func TestWatchOwnerResumeWithAProbeInFlightIsNotRefusedAsHeld(t *testing.T) {
	t.Parallel()
	f := newWatchFixture(t, "watched", recordHeadEntries(), 3)
	dir := filepath.Dir(f.src.recordPath)
	releaseOwner, err := session.NewStore(dir).Hold(watchSessionID)
	if err != nil {
		t.Fatalf("the owner's Hold: %v", err)
	}
	store := session.NewStore(dir)
	f.src.probe = func() (bool, error) {
		isHeld, _, err := store.ProbeHold(watchSessionID)
		return isHeld, err
	}
	host := &holdingHost{fakeSessionHost: &fakeSessionHost{}, store: store}
	t.Cleanup(func() {
		for _, release := range host.releases {
			_ = release()
		}
	})
	eng := watchEngine()
	own := newSessionModel(t, eng, host)
	own.transcript.addUser("my own question", nil)
	m := enterAndFollow(t, own, f)
	f.finish()
	if err := releaseOwner(); err != nil {
		t.Fatalf("the owner's release: %v", err)
	}
	m = probeWatch(t, m)
	if !m.isWatchResumable() {
		t.Fatalf("legend %q; setup: the watch did not reach the resumable finish", m.legend())
	}
	stale := m
	stale.watch.readsUntilProbe = 0
	inFlight := stale.readJournalCmd() // a probing read the follower had in flight at ⏎

	m, load := stepCmd(t, m, keyEnter())
	probed := make(chan tea.Msg, 1)
	go func() { probed <- inFlight() }()
	loaded, ok := load().(sessionLoadedMsg)
	m = step(t, step(t, m, loaded), <-probed)

	if !ok || loaded.err != nil {
		t.Fatalf("the resume's load answered %#v; want the record held and loaded", loaded)
	}
	if len(eng.restoreCalls) != 1 || m.watch.isActive {
		t.Errorf("restores %d, watching %v; want the record resumed and the late probe dropped",
			len(eng.restoreCalls), m.watch.isActive)
	}
}

// A --resume start of a running session (Options.Watch) opens watching it over a fresh session: the
// watch is entered at construction, Init sends the follower's first read, the box names the frame's
// title — the id while the frame has none — and esc lands in the fresh session newModel built.
func TestResumeWatchOpensAtStartAndEscLandsFresh(t *testing.T) {
	t.Parallel()
	f := newWatchFixture(t, "", recordHeadEntries(), 0)
	eng := watchEngine()
	opts := testOpts
	opts.Watch = &LiveSession{
		ID:          f.src.id,
		RecordPath:  f.src.recordPath,
		JournalPath: f.src.journalPath,
	}

	m := newTestModelEng(t, eng, opts)

	if !m.watch.isActive || m.startupWatchRead == nil {
		t.Fatal("newModel did not open watching the live session")
	}
	if want := fmt.Sprintf(watchLegendFormat, watchSessionID); m.watchLegend() != want {
		t.Errorf("legend = %q; want %q", m.watchLegend(), want)
	}
	m = step(t, m, m.startupWatchRead())
	f.prompt("theirs", false)
	m = pollWatch(t, m)
	if got := entryTexts(m, entryUser); !slices.Contains(got, "theirs") {
		t.Errorf("user entries while watching = %q; want the journal's prompt", got)
	}

	m = step(t, m, keyEsc())

	if m.watch.isActive || m.eng != Engine(eng) || m.state != stateIdle {
		t.Fatal("esc did not leave the watch for the fresh session's engine")
	}
	if got := entryTexts(m, entryUser); len(got) != 0 {
		t.Errorf("user entries after esc = %q; want the fresh session's none", got)
	}
}
