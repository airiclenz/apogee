package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/eventjson"
	"github.com/airiclenz/apogee/internal/gitstatus"
	"github.com/airiclenz/apogee/internal/scheme"
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
	// now is the clock reopen hands the journal; nil is the journal's own.
	now func() time.Time
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
// does (Store.OpenJournal). The frame names no binding.
func (f *watchFixture) reopen(title string, head int) { f.reopenBound(title, head, "", 0) }

// reopenBound is reopen with a frame that names the owner's binding: model and context window.
func (f *watchFixture) reopenBound(title string, head int, model string, window int) {
	f.t.Helper()
	file, err := os.Create(f.src.journalPath)
	if err != nil {
		f.t.Fatalf("open journal: %v", err)
	}
	f.t.Cleanup(func() { _ = file.Close() })
	f.file = file
	f.journal = eventjson.NewJournal(file, eventjson.JournalOptions{
		Session:       watchSessionID,
		Title:         title,
		Entries:       head,
		Model:         model,
		ContextWindow: window,
		Now:           f.now,
		Window:        time.Hour,
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

// Every door into a watch carries the watched session's workspace root — the journal frame's, else
// the record's, "" when neither names one — and leaving the watch drops it.
func TestWatchCarriesTheWatchedWorkspace(t *testing.T) {
	t.Parallel()
	const browserWorkspace = "/ws/a"
	cases := []struct {
		name  string
		enter func(t *testing.T, f *watchFixture) Model
		want  string
	}{
		{
			name: "a live browser row takes its frame's workspace",
			enter: func(t *testing.T, f *watchFixture) Model {
				host := &fakeSessionHost{live: []LiveSession{liveRowWith(f, "headless run", browserWorkspace, time.Now())}}
				return openBrowser(t, newBrowserModel(t, &fakeEngine{}, host, browserWorkspace))
			},
			want: browserWorkspace,
		},
		{
			name: "a live browser row whose frame names none takes the record's",
			enter: func(t *testing.T, f *watchFixture) Model {
				host := &fakeSessionHost{}
				storeMeta(host, f.src.id, "running elsewhere", browserWorkspace, time.Now(), 0, nil)
				host.live = []LiveSession{liveRowWith(f, "", "", time.Now())}
				return openBrowser(t, newBrowserModel(t, &fakeEngine{}, host, browserWorkspace))
			},
			want: browserWorkspace,
		},
		{
			name: "a finished row of a watched home takes the record's",
			enter: func(t *testing.T, f *watchFixture) Model {
				host := &fakeSessionHost{watched: []WatchedSession{
					watchedFinished(f, "bobs finished run", browserWorkspace, time.Now()),
				}}
				return openBrowser(t, newBrowserModel(t, &fakeEngine{}, host, browserWorkspace))
			},
			want: browserWorkspace,
		},
		{
			name: "the startup watch takes its frame's workspace",
			enter: func(t *testing.T, f *watchFixture) Model {
				opts := testOpts
				opts.Watch = &LiveSession{
					ID:          f.src.id,
					Workspace:   "/ws/watched",
					RecordPath:  f.src.recordPath,
					JournalPath: f.src.journalPath,
				}
				return newTestModelEng(t, watchEngine(), opts)
			},
			want: "/ws/watched",
		},
		{
			name: "a startup watch whose frame names none stays empty",
			enter: func(t *testing.T, f *watchFixture) Model {
				opts := testOpts
				opts.Watch = &LiveSession{ID: f.src.id, RecordPath: f.src.recordPath, JournalPath: f.src.journalPath}
				return newTestModelEng(t, watchEngine(), opts)
			},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newWatchFixture(t, "headless run", recordHeadEntries(), 3)
			m := tc.enter(t, f)

			if m.sessionBrowser.open {
				m = step(t, m, keyEnter())
			}
			workspace, isWatching := m.watchedWorkspace()
			left := step(t, m, keyEsc())
			leftWorkspace, isStillWatching := left.watchedWorkspace()

			if !isWatching || workspace != tc.want {
				t.Errorf("watchedWorkspace() = %q, %v; want %q, true", workspace, isWatching, tc.want)
			}
			if isStillWatching || leftWorkspace != "" {
				t.Errorf("after esc watchedWorkspace() = %q, %v; want \"\", false", leftWorkspace, isStillWatching)
			}
		})
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

// The two workspaces the watch-footer cases tell apart, the watched one deep enough for the fit to
// elide, and the branch each one's git answers.
const (
	watchFooterHostWorkspace    = "/ws/host"
	watchFooterWatchedWorkspace = "/srv/watched/deep/nested/project"
	watchFooterHostBranch       = "host-branch"
	watchFooterWatchedBranch    = "watched-branch"
)

// watchFooterOpts is a git-wired Options over the host workspace, its footer laid out in rows (""
// for the default layout), whose git answers each workspace its own branch.
func watchFooterOpts(rows domain.FooterRows) Options {
	git := newFakeGitHost(gitstatus.Snapshot{Branch: watchFooterHostBranch})
	git.byRoot = map[string]gitstatus.Snapshot{watchFooterWatchedWorkspace: {Branch: watchFooterWatchedBranch}}
	opts := testOpts
	opts.UI.FooterRows = rows
	opts.Workspace = watchFooterHostWorkspace
	opts.GitStatus = git
	opts.Mode = domain.ModeAuto
	return opts
}

// watchFooterModel builds an idle model over opts in a window width wide, its effort word showing
// and the host's first git read landed.
func watchFooterModel(t *testing.T, opts Options, width int) Model {
	t.Helper()
	m := newModel(context.Background(), watchEngine(), opts, nil)
	m = step(t, m, tea.WindowSizeMsg{Width: width, Height: 30})
	m.hb.effort = domain.EffortSupport{Supported: true}
	m, _ = land(t, m)
	return m
}

// watchFooterInto enters a watch of a fixture whose session runs in workspace ("" for an unknown
// one) and folds the follower's first read and the watched git read.
func watchFooterInto(t *testing.T, m Model, workspace string) Model {
	t.Helper()
	f := newWatchFixture(t, "watched", nil, 0)
	f.src.workspace = workspace
	m, cmd := m.enterWatch(f.src)
	if !m.watch.isActive {
		t.Fatal("setup: enterWatch did not enter watch mode")
	}
	return foldAll(t, m, expandBatch(cmd))
}

// plainFooter is the footer as painted in the model's own window, escapes stripped.
func plainFooter(m Model) string {
	return ansiPattern.ReplaceAllString(m.footerContent(m.width), "")
}

// TestWatchFooterNamesTheWatchedWorkspace: while watching, the footer's workdir is the watched
// session's, led by the `watching` marker, and the git segment is the watched workspace's; the
// host and effort stay the watcher's own. Leaving puts the host's folder and branch back unmarked.
func TestWatchFooterNamesTheWatchedWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("entered from a live browser row", func(t *testing.T) {
		t.Parallel()
		f := newWatchFixture(t, "headless run", nil, 0)
		sessions := &fakeSessionHost{}
		sessions.live = []LiveSession{liveRowWith(f, "headless run", watchFooterWatchedWorkspace, time.Now())}
		opts := watchFooterOpts("")
		opts.Sessions = sessions
		m := watchFooterModel(t, opts, 160)
		facts := func(m Model) string {
			in := m.footerLeftText()
			return in.segment(footerSegHost) + "|" + in.segment(footerSegEffort)
		}
		before := facts(m)
		sep := " " + m.th.glyphs.assistant + " "

		m = openBrowser(t, m)
		m = step(t, m, keyCtrl('a')) // the live row is another folder's: widen to every workspace
		m, cmd := stepCmd(t, m, keyEnter())
		if !m.watch.isActive {
			t.Fatal("⏎ on the live row did not enter watch mode")
		}
		m = foldAll(t, m, expandBatch(cmd))

		footer := plainFooter(m)
		if want := watchingLabel + sep + watchFooterWatchedWorkspace + sep + watchFooterWatchedBranch; !strings.Contains(footer, want) {
			t.Errorf("watched footer lacks %q:\n%s", want, footer)
		}
		for _, host := range []string{watchFooterHostWorkspace, watchFooterHostBranch} {
			if strings.Contains(footer, host) {
				t.Errorf("watched footer still names the host's %q:\n%s", host, footer)
			}
		}
		if got := facts(m); got != before || strings.HasSuffix(before, "|") {
			t.Errorf("host|effort %q while watching; want %q unchanged (and an effort word)", got, before)
		}

		m, cmd = stepCmd(t, m, keyEsc())
		if m.watch.isActive {
			t.Fatal("esc did not leave the watch")
		}
		m = foldAll(t, m, expandBatch(cmd))
		footer = plainFooter(m)
		if want := watchFooterHostWorkspace + sep + watchFooterHostBranch; !strings.Contains(footer, want) {
			t.Errorf("footer after esc lacks %q:\n%s", want, footer)
		}
		for _, watched := range []string{watchingLabel, watchFooterWatchedWorkspace, watchFooterWatchedBranch} {
			if strings.Contains(footer, watched) {
				t.Errorf("footer after esc still names %q:\n%s", watched, footer)
			}
		}
	})

	t.Run("a narrowing window elides the path and keeps the marker", func(t *testing.T) {
		t.Parallel()
		m := watchFooterInto(t, watchFooterModel(t, watchFooterOpts(""), 160), watchFooterWatchedWorkspace)
		var sawElided, sawMarkerAlone bool
		for w := 160; w >= 10; w-- {
			for row := range m.footerLines() {
				layout := m.footerRow(row, w)
				path, hasPath := layout.span(footerSegWorkdir)
				marker, hasMarker := layout.span(footerSegWatching)
				if hasPath && !hasMarker {
					t.Errorf("width %d row %d: the path %q shows without the marker", w, row, path.text)
				}
				if hasMarker && marker.text != watchingLabel {
					t.Errorf("width %d row %d: the marker reads %q, want %q whole", w, row, marker.text, watchingLabel)
				}
				sawElided = sawElided || hasPath && hasMarker && path.text != watchFooterWatchedWorkspace
				sawMarkerAlone = sawMarkerAlone || hasMarker && !hasPath
			}
		}
		if !sawElided {
			t.Error("no width elided the watched path beside the marker")
		}
		if !sawMarkerAlone {
			t.Error("no width dropped the path while keeping the marker")
		}
	})

	t.Run("a configured layout seats the marker before the workdir", func(t *testing.T) {
		t.Parallel()
		opts := watchFooterOpts("[[workdir, git], [host, model, effort, spend, mode]]")
		m := watchFooterInto(t, watchFooterModel(t, opts, 160), watchFooterWatchedWorkspace)
		sep := " " + m.th.glyphs.assistant + " "
		rows := strings.Split(plainFooter(m), "\n")
		if want := bodyIndent + watchingLabel + sep + watchFooterWatchedWorkspace + sep + watchFooterWatchedBranch; !strings.HasPrefix(rows[0], want) {
			t.Errorf("row 0 = %q; want it to open with %q", rows[0], want)
		}

		opts = watchFooterOpts("[[host, model, effort, mode], [git, spend]]")
		m = watchFooterInto(t, watchFooterModel(t, opts, 160), watchFooterWatchedWorkspace)
		footer := plainFooter(m)
		if strings.Contains(footer, watchingLabel) {
			t.Errorf("a layout naming no workdir shows the marker:\n%s", footer)
		}
		if !strings.Contains(footer, watchFooterWatchedBranch) {
			t.Errorf("a layout naming no workdir lacks the watched branch:\n%s", footer)
		}
	})

	t.Run("an unknown workspace shows the marker alone", func(t *testing.T) {
		t.Parallel()
		m := watchFooterInto(t, watchFooterModel(t, watchFooterOpts(""), 160), "")
		footer := plainFooter(m)
		if !strings.Contains(footer, watchingLabel) {
			t.Errorf("footer lacks the marker:\n%s", footer)
		}
		for _, other := range []string{watchFooterHostWorkspace, watchFooterHostBranch, watchFooterWatchedBranch} {
			if strings.Contains(footer, other) {
				t.Errorf("footer names %q while watching an unknown workspace:\n%s", other, footer)
			}
		}
		if _, _, _, ok := m.footerGitSpan(m.width); ok {
			t.Error("a git segment shows while watching an unknown workspace")
		}
	})

	t.Run("a workdir colour override recolours neither the marker nor the watched path", func(t *testing.T) {
		t.Parallel()
		for _, name := range scheme.Discover("") {
			s, _ := scheme.Resolve(name, "")
			host := withFooterColors(t, watchFooterModel(t, watchFooterOpts(""), 160), s, "{workdir: user-text}")
			userText := sgrOpener(t, host.th.footerText.Foreground(lipgloss.Color(s.UserText)))
			accent := sgrOpener(t, host.th.footerAccent)
			if userText == accent {
				continue
			}
			m := watchFooterInto(t, host, watchFooterWatchedWorkspace)
			footer := m.footerContent(m.width)
			for _, run := range []string{accent + watchingLabel, accent + watchFooterWatchedWorkspace} {
				if !strings.Contains(footer, run) {
					t.Errorf("watched footer under %q lacks the accent run %q: %q", name, run, footer)
				}
			}
			if strings.Contains(footer, userText+watchFooterWatchedWorkspace) {
				t.Errorf("the override recoloured the watched path under %q: %q", name, footer)
			}
			if unwatched := host.footerContent(host.width); !strings.Contains(unwatched, userText+watchFooterHostWorkspace) {
				t.Errorf("the override no longer paints the host workdir under %q: %q", name, unwatched)
			}
		}
	})
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

func TestWatchRepaintsOnATruncationRegrownPastTheOffset(t *testing.T) {
	t.Parallel()
	later := func() time.Time { return time.Date(2030, 1, 2, 3, 4, 5, 123456789, time.UTC) }
	cases := []struct {
		name  string
		title string
		// now is the reopened journal's clock; nil keeps the fixture's.
		now func() time.Time
		// isReplaced removes the journal first, so the reopen writes a new file under the path.
		isReplaced bool
		wantLegend string
	}{
		{name: "a new title", title: "again", wantLegend: "watching again"},
		{name: "only the frame's time differs", title: "watched", now: later, wantLegend: "watching watched"},
		{name: "a replaced journal", title: "again", isReplaced: true, wantLegend: "watching again"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newWatchFixture(t, "watched", nil, 0)
			m := enterWatchOn(t, watchEngine(), f)
			f.prompt("before", false)
			m = pollWatch(t, m)

			if tc.isReplaced {
				if err := os.Remove(f.src.journalPath); err != nil {
					t.Fatalf("remove journal: %v", err)
				}
			}
			f.now = tc.now
			f.reopen(tc.title, 0)
			f.prompt("after the owner reopened the record, a prompt long enough to regrow the journal", false)
			info, err := os.Stat(f.src.journalPath)
			if err != nil {
				t.Fatalf("stat journal: %v", err)
			}
			if info.Size() <= m.watch.offset {
				t.Fatalf("setup: the reopened journal is %d bytes, not past the follower's offset %d",
					info.Size(), m.watch.offset)
			}
			m = pollWatch(t, m)

			want := []string{"after the owner reopened the record, a prompt long enough to regrow the journal"}
			if got := entryTexts(m, entryUser); !slices.Equal(got, want) {
				t.Errorf("user entries = %q; want only the reopened journal's", got)
			}
			if got := m.legend(); !strings.Contains(got, tc.wantLegend) {
				t.Errorf("legend = %q; want it to contain %q", got, tc.wantLegend)
			}

			f.prompt("and it grows", false)
			m = pollWatch(t, m)
			if got := entryTexts(m, entryUser); !slices.Equal(got, append(want, "and it grows")) {
				t.Errorf("after growth, user entries = %q; want the reopened journal's read on from its offset", got)
			}
		})
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

// seedRecordCtxUsed rewrites the fixture's record with ctxUsed as its last observed fill
// (Meta.CtxUsed), leaving the rest of it — and newWatchFixture's own signature — as they are.
func seedRecordCtxUsed(t *testing.T, f *watchFixture, ctxUsed int) {
	t.Helper()
	raw, err := os.ReadFile(f.src.recordPath)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var record session.Record
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	record.Meta.CtxUsed = ctxUsed
	if raw, err = json.Marshal(record); err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	if err := os.WriteFile(f.src.recordPath, raw, 0o600); err != nil {
		t.Fatalf("write record: %v", err)
	}
}

func TestWatchLightsTheGaugeFromTheWatchedRun(t *testing.T) {
	t.Parallel()
	f := newWatchFixture(t, "watched", recordHeadEntries(), 3)
	seedRecordCtxUsed(t, f, 8000)
	eng := watchEngine()
	m := newTestModelEng(t, eng, testOpts)
	m.ctxUsed = 4000 // the watcher's own reading, parked across the watch
	m, cmd := m.enterWatch(f.src)
	m = step(t, m, cmd())

	if got := statusText(t, m); !strings.Contains(got, "8k/32k") {
		t.Errorf("status line on entry = %q; want the record's fill against the watcher's window (8k/32k)", got)
	}

	// A sub-agent's and a maintenance reading name other windows; neither moves the gauge.
	f.emit(
		domain.UsageEvent{EventBase: domain.EventBase{Depth: 1, RunID: "r1", Turn: 1},
			PromptTokens: 900, TotalTokens: 1000, ContextWindow: 50000},
		domain.UsageEvent{EventBase: domain.EventBase{Turn: 1}, Maintenance: true,
			PromptTokens: 900, TotalTokens: 1000, ContextWindow: 70000},
	)
	m = pollWatch(t, m)
	if got := statusText(t, m); !strings.Contains(got, "8k/32k") {
		t.Errorf("status line after sub-agent and maintenance readings = %q; want 8k/32k unmoved", got)
	}

	f.emit(domain.UsageEvent{EventBase: domain.EventBase{Turn: 1},
		PromptTokens: 64536, CompletionTokens: 1000, TotalTokens: 65536, ContextWindow: 131072,
		Cumulative: domain.Usage{Calls: 1, PromptTokens: 64536, CompletionTokens: 1000, TotalTokens: 65536}})
	m = pollWatch(t, m)
	if got := statusText(t, m); !strings.Contains(got, "64k/128k 50%") {
		t.Errorf("status line after the usage line = %q; want the watched fill against the watched window (64k/128k 50%%)", got)
	}
	rows := m.usageRows()
	if len(rows) < 2 {
		t.Fatalf("/usage rows = %q; want a header and the main row", rows)
	}
	if main := rows[1]; main[len(main)-1] != "50%" {
		t.Errorf("/usage main row = %q; want its fill against the gauge's window (50%%)", main)
	}

	// A top-level context_estimate line moves the gauge to `~` over that reading; a delegate's does not.
	f.emit(
		domain.ContextEstimateEvent{EventBase: domain.EventBase{Depth: 1, RunID: "r1", Turn: 1}, Tokens: 120000},
		domain.ContextEstimateEvent{EventBase: domain.EventBase{Turn: 1}, Tokens: 98304, Calibrated: true},
	)
	m = pollWatch(t, m)
	if got := statusText(t, m); !strings.Contains(got, "~96k/128k 75%") {
		t.Errorf("status line after the estimate line = %q; want the watched estimate (~96k/128k 75%%)", got)
	}

	m = step(t, m, keyEsc())

	if m.watch.isActive {
		t.Fatal("esc did not leave the watch")
	}
	if got := statusText(t, m); !strings.Contains(got, "4k/32k") || strings.Contains(got, "~") {
		t.Errorf("status line after esc = %q; want the watcher's own reading and window (4k/32k), no estimate", got)
	}
	if m.opts.ContextWindow != testOpts.ContextWindow || m.topLevelWindow() != testOpts.ContextWindow {
		t.Errorf("window after esc = %d (opts %d); want the watcher's own %d",
			m.topLevelWindow(), m.opts.ContextWindow, testOpts.ContextWindow)
	}
}

// TestWatchBindsTheFramesModelAndWindowAtEntry pins the journal frame's binding: a frame that
// names a model and a context window binds the watch to them from its fold — before any usage
// line, and at head 0, where the record is not read — while a frame without the members (an older
// apogee's journal) keeps the record-and-watcher fallback.
func TestWatchBindsTheFramesModelAndWindowAtEntry(t *testing.T) {
	t.Parallel()
	opts := testOpts
	opts.Model = "model-a" // the watcher's own binding, with testOpts' 32768 window
	enter := func(t *testing.T, f *watchFixture) Model {
		t.Helper()
		m, cmd := newTestModelEng(t, watchEngine(), opts).enterWatch(f.src)
		if !m.watch.isActive || cmd == nil {
			t.Fatal("setup: enterWatch did not enter watch mode")
		}
		return step(t, m, cmd())
	}

	t.Run("the frame's binding lights the gauge and binds the children before any usage line", func(t *testing.T) {
		t.Parallel()
		f := newWatchFixture(t, "watched", recordHeadEntries(), 3)
		seedRecordCtxUsed(t, f, 8000)
		f.reopenBound("watched", 3, "model-b", 131072)
		m := enter(t, f)

		if got := statusText(t, m); !strings.Contains(got, "8k/128k") {
			t.Errorf("status line on entry = %q; want the record's fill against the frame's window (8k/128k)", got)
		}
		unrouted := runRef{depth: 1, spawn: "s1", id: "run-s1"}
		f.emit(delegationEvents(unrouted, "scout-one")...)
		f.emit(childUsageOn(unrouted, "model-b", 12000))
		m = pollWatch(t, m)
		if one := subAgentRowOf(m, "scout-one"); strings.Contains(one, "model-b") || !strings.Contains(one, "12k/128k") {
			t.Errorf("unrouted child's row = %q; want no model-b cell and its fill against the frame's window (12k/128k)", one)
		}
	})

	t.Run("at head 0 the frame alone binds the watch", func(t *testing.T) {
		t.Parallel()
		f := newWatchFixture(t, "watched", nil, 0)
		f.reopenBound("watched", 0, "model-b", 131072)
		m := enter(t, f)

		if got := m.topLevelWindow(); got != 131072 {
			t.Errorf("topLevelWindow at head 0 = %d; want the frame's 131072", got)
		}
		if got := m.topLevelModel(); got != "model-b" {
			t.Errorf("topLevelModel at head 0 = %q; want the frame's model-b", got)
		}
	})

	t.Run("a frame without the members keeps the fallback", func(t *testing.T) {
		t.Parallel()
		f := newWatchFixture(t, "watched", recordHeadEntries(), 3)
		seedRecordCtxUsed(t, f, 8000)
		frame := `{"event":"run_started","v":2,"seq":1,"time":"2026-10-17T12:00:00Z","session":"` +
			watchSessionID + `","turn":null,"depth":null,"call_id":null,"run_id":null,"data":{"session":"` +
			watchSessionID + `","workspace":"/work","title":"watched","entries":3}}` + "\n"
		if err := os.WriteFile(f.src.journalPath, []byte(frame), 0o600); err != nil {
			t.Fatalf("write journal: %v", err)
		}
		m := enter(t, f)

		if got := statusText(t, m); !strings.Contains(got, "8k/32k") {
			t.Errorf("status line on entry = %q; want the record's fill against the watcher's window (8k/32k)", got)
		}
		if got := m.topLevelModel(); got != "model-a" {
			t.Errorf("topLevelModel = %q; want the watcher's own model-a", got)
		}
	})
}

// seedRecordModel rewrites the fixture's record with model as its bound model (Meta.Model), leaving
// the rest of it — and newWatchFixture's own signature — as they are.
func seedRecordModel(t *testing.T, f *watchFixture, model string) {
	t.Helper()
	raw, err := os.ReadFile(f.src.recordPath)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var record session.Record
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	record.Meta.Model = model
	if raw, err = json.Marshal(record); err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	if err := os.WriteFile(f.src.recordPath, raw, 0o600); err != nil {
		t.Fatalf("write record: %v", err)
	}
}

// delegationEvents are the depth-0 sub_agent call that spawns run (its SpawnRunID set) under name,
// the started phase its child stamps, and the child's first tool call, which gives its row the
// summary line its fill and model cells ride on.
func delegationEvents(run runRef, name string) []domain.Event {
	return []domain.Event{
		domain.ToolCallEvent{EventBase: domain.EventBase{Turn: 1}, SpawnRunID: run.id, Call: domain.ToolCall{
			ID: run.spawn, Tool: subAgentToolName, Arguments: []byte(`{"task":"work","name":"` + name + `"}`),
		}},
		domain.SubAgentPhaseEvent{EventBase: stampedBase(run), Phase: domain.SubAgentStarted},
		domain.ToolCallEvent{EventBase: stampedBase(run), Call: domain.ToolCall{
			ID: run.id + "-r1", Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`),
		}},
	}
}

// childUsageOn is run's usage reading: total tokens filled on model, naming no window of its own.
func childUsageOn(run runRef, model string, total int) domain.UsageEvent {
	base := stampedBase(run)
	base.Turn = 1
	return domain.UsageEvent{EventBase: base, Model: model, PromptTokens: total, TotalTokens: total,
		Cumulative: domain.Usage{Calls: 1, PromptTokens: total, TotalTokens: total}}
}

// subAgentRowOf is the rendered line of the transcript that names a sub-agent run, or "" when none
// does.
func subAgentRowOf(m Model, name string) string {
	for _, line := range strings.Split(plainTranscript(m), "\n") {
		if strings.Contains(line, name) {
			return line
		}
	}
	return ""
}

// TestWatchMeasuresSubAgentsAgainstTheWatchedRun pins a watch's sub-agent rows to the watched run's
// binding: a child is measured against the model and window the watched run is bound to — its
// depth-0 usage line's, else the record's model — never against the watcher's own, while an owner
// delegation folded mid-watch keeps the watcher's.
func TestWatchMeasuresSubAgentsAgainstTheWatchedRun(t *testing.T) {
	t.Parallel()
	opts := testOpts
	opts.Model = "model-a" // the watcher's own binding, with testOpts' 32768 window
	unrouted := runRef{depth: 1, spawn: "s1", id: "run-s1"}
	routed := runRef{depth: 1, spawn: "s2", id: "run-s2"}
	watchedUsage := domain.UsageEvent{EventBase: domain.EventBase{Turn: 1}, Model: "model-b",
		ContextWindow: 131072, PromptTokens: 20000, TotalTokens: 20000,
		Cumulative: domain.Usage{Calls: 1, PromptTokens: 20000, TotalTokens: 20000}}
	enter := func(t *testing.T, f *watchFixture) Model {
		t.Helper()
		m, cmd := newTestModelEng(t, watchEngine(), opts).enterWatch(f.src)
		if !m.watch.isActive || cmd == nil {
			t.Fatal("setup: enterWatch did not enter watch mode")
		}
		return step(t, m, cmd())
	}

	t.Run("the watched run's depth-0 usage line binds its children", func(t *testing.T) {
		t.Parallel()
		f := newWatchFixture(t, "watched", nil, 0)
		m := enter(t, f)
		f.emit(watchedUsage)
		f.emit(delegationEvents(unrouted, "scout-one")...)
		f.emit(delegationEvents(routed, "scout-two")...)
		f.emit(childUsageOn(unrouted, "model-b", 12000), childUsageOn(routed, "model-a", 9000))
		m = pollWatch(t, m)

		one := subAgentRowOf(m, "scout-one")
		if strings.Contains(one, "model-b") || !strings.Contains(one, "/128k") {
			t.Errorf("unrouted child's row = %q; want no model-b cell and a fill against the watched window (/128k)", one)
		}
		if two := subAgentRowOf(m, "scout-two"); !strings.Contains(two, "model-a") {
			t.Errorf("routed child's row = %q; want the model-a cell — the watcher's model is not the watched run's", two)
		}
	})

	t.Run("the record's model binds them before any usage line names one", func(t *testing.T) {
		t.Parallel()
		f := newWatchFixture(t, "watched", recordHeadEntries(), 3)
		seedRecordModel(t, f, "model-b")
		m := enter(t, f)
		f.emit(delegationEvents(unrouted, "scout-one")...)
		f.emit(childUsageOn(unrouted, "model-b", 12000))
		m = pollWatch(t, m)

		if one := subAgentRowOf(m, "scout-one"); !strings.Contains(one, "12k/32k") || strings.Contains(one, "model-b") {
			t.Errorf("unrouted child's row = %q; want its fill (12k/32k) and no model-b cell — the record binds the watched run to model-b", one)
		}
	})

	t.Run("an owner delegation folded mid-watch keeps the watcher's binding", func(t *testing.T) {
		t.Parallel()
		f := newWatchFixture(t, "watched", nil, 0)
		m := enter(t, f)
		f.emit(watchedUsage)
		m = pollWatch(t, m)
		owner := runRef{depth: 1, spawn: "own-s1", id: "own-run"}
		for _, e := range append(delegationEvents(owner, "owner-scout"), childUsageOn(owner, opts.Model, 6000)) {
			m = step(t, m, eventMsg{Event: e})
		}

		m = step(t, m, keyEsc())

		if m.watch.isActive {
			t.Fatal("esc did not leave the watch")
		}
		row := subAgentRowOf(m, "owner-scout")
		if strings.Contains(row, "model-a") || !strings.Contains(row, "6k/32k") {
			t.Errorf("owner's row = %q; want no model cell and a fill against the watcher's own window (6k/32k)", row)
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

	_, cmd := stepCmd(t, m, watchBatchMsg{gen: m.watch.gen, journalCursor: m.watch.journalCursor})
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

// Every swap of the session view forgets the painted frame (swapSessionView, which every swap goes
// through, calls forgetPaintedFrame): settle names a transcript by its generation, and the parked
// and the watched transcript count generations apart, so a swap that lands on an equal count would
// otherwise keep the other view's lines on screen. The fixture lines the generations up at each of
// the three swaps — entry, an owner fold, esc — which is the case the rule exists for: with the
// counts apart, settle repaints on its own.
//
// The entry is enterWatch called directly and settled by a view Msg that folds nothing
// (tea.FocusMsg): the browser's ⏎, the one Update that enters, also closes the browser, and the
// transcript's regained rows lay it out whatever the key says.
func TestWatchRepaintsOnEverySessionViewSwap(t *testing.T) {
	t.Parallel()
	const ownText = "my own question"
	owner := func() Model {
		return step(t, ownerSession(t, watchEngine(), &fakeSessionHost{}), tea.FocusMsg{})
	}
	f := isolationFixture(t)
	f.emit(domain.TurnEvent{EventBase: domain.EventBase{Turn: 1}, Status: domain.StatusExchangeComplete})
	m := owner()
	if !strings.Contains(plain(m.View()), ownText) {
		t.Fatalf("setup: the owner's view does not paint %q", ownText)
	}

	// The entry: the watch view's reset moves the generation by as many touches as clearWatchView
	// makes, so the owner's transcript is set that far below its painted key and the watched one
	// lands on the very generation the owner's lines were painted under.
	g := m.painted.generation
	probe := m
	probe.transcript.paints = newPaintCache()
	probe.clearWatchView()
	m.transcript.generation = g - (probe.transcript.generation - m.transcript.generation)

	m, read := m.enterWatch(f.src)
	if !m.watch.isActive || read == nil {
		t.Fatal("setup: enterWatch did not enter watch mode")
	}
	m = step(t, m, tea.FocusMsg{})
	if m.transcript.generation != g {
		t.Fatalf("setup: watched generation = %d, want the owner's painted %d", m.transcript.generation, g)
	}
	if screen := plain(m.View()); strings.Contains(screen, ownText) {
		t.Errorf("entering the watch left the owner's %q on screen:\n%s", ownText, screen)
	}

	m = step(t, m, read())
	// The owner's fold: an exchangeDoneMsg lays the owner's view out (finishWorker), so the frame on
	// record is the owner's when the watch view comes back. Its parked transcript is set below the
	// watched generation by what the fold touches, measured on a twin that folds it unwatched.
	twin := owner()
	before := twin.transcript.generation
	twin = step(t, twin, exchangeDoneMsg{})
	m.watch.parked.transcript.generation = m.transcript.generation - (twin.transcript.generation - before)

	m = step(t, m, exchangeDoneMsg{})
	if !m.watch.isActive {
		t.Fatal("the owner's exchangeDoneMsg ended the watch")
	}
	if m.watch.parked.transcript.generation != m.transcript.generation {
		t.Fatalf("setup: parked generation = %d, want the watched %d",
			m.watch.parked.transcript.generation, m.transcript.generation)
	}
	screen := plain(m.View())
	if strings.Contains(screen, ownText) || !strings.Contains(screen, "first ask") {
		t.Errorf("after the owner's fold the screen is not the watched view:\n%s", screen)
	}

	m = step(t, m, keyEsc())
	if m.watch.isActive {
		t.Fatal("esc did not leave the watch")
	}
	if got, want := m.View().Content, twin.View().Content; got != want {
		t.Errorf("the view after esc is not the owner's unwatched view byte for byte:\n%s\nwant:\n%s",
			plain(m.View()), plain(twin.View()))
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

// sortedKeys lists a set in order, for a readable failure.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// The two session boundaries the view resets at — /clear and /new (resetSessionView) and an in-TUI
// /sessions restore (resumeLoaded) — write only the session view, which is the one value a watch
// parks and swaps (swapSessionView). A field either boundary writes on the Model itself would stand
// across a watch unparked: the watched run would paint the owner's, and the owner's would come back
// changed by the watch. So every Model field the two write, through every Model method they call,
// is a sessionView field — direct, or promoted out of the embedded liveStats (ctxUsed) — or is on
// the allow-list below with the reason it belongs to the Model rather than the view.
func TestSessionViewHoldsEveryFieldTheBoundariesReset(t *testing.T) {
	t.Parallel()
	allowed := map[string]string{
		"eng":           "the engine is the Model's wiring; a watch parks it beside the view, as watch.owner",
		"resumePending": "the restore's queued resume of the record's workflows (resumeAfterFold) is engine work",
		"writeBusy":     "the record-write single-flight (scheduleWrite) serialises the session host's writes",
		"pendingWrites": "the record writes queued behind that single-flight (scheduleWrite), not the view's",
	}
	methods := packageMethods(t)
	view := map[string]bool{}
	for _, field := range reflect.VisibleFields(reflect.TypeFor[sessionView]()) {
		view[field.Name] = true
	}

	written := map[string]bool{}
	for _, boundary := range []string{"resetSessionView", "resumeLoaded"} {
		for field := range fieldsWritten(methods, boundary, map[string]bool{}) {
			written[field] = true
		}
	}
	for _, field := range sortedKeys(written) {
		if !view[field] && allowed[field] == "" {
			t.Errorf("a session boundary writes the Model's %s, which no watch parks: move it into sessionView "+
				"or name on the allow-list why it is the Model's", field)
		}
	}
	for field := range allowed {
		if view[field] || !written[field] {
			t.Errorf("allow-list entry %s is stale: it is a sessionView field or neither boundary writes it", field)
		}
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

func TestWatchRoutesGitActionLandingsToTheOwner(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		msg  gitActionDoneMsg
		want watchRoute
	}{
		{"an action's result", gitActionDoneMsg{action: gitActionPull, result: "pull: already up to date"}, watchRouteOwner},
		{"an action's failure", gitActionDoneMsg{action: gitActionPush, err: fmt.Errorf("git push: rejected")}, watchRouteOwner},
		{"a failed branch listing", gitActionDoneMsg{action: gitActionSwitch, listing: true, err: fmt.Errorf("git switch: no repo")}, watchRouteOwner},
		{"a branch listing that opens the picker", gitActionDoneMsg{action: gitActionSwitch, listing: true, branches: []string{"main"}}, watchRouteHeld},
	}
	for _, tc := range cases {
		if got := watchRouteOf(tc.msg); got != tc.want {
			t.Errorf("%s: watchRouteOf = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestWatchRefusesTheGitActionsPicker drives a click on the footer's git segment through the one
// opener's gate (openGitActions): while watching, the click opens no picker and raises the
// watch-refused flash; after esc the same click opens the git actions picker.
func TestWatchRefusesTheGitActionsPicker(t *testing.T) {
	t.Parallel()
	base, git := gitActionsModelWith(t, tracked(), &fakeGitActionsHost{}, func(*Options) {})
	git.byRoot = map[string]gitstatus.Snapshot{watchFooterWatchedWorkspace: untracked()}
	f := newWatchFixture(t, "watched", recordHeadEntries(), 3)
	f.src.workspace = watchFooterWatchedWorkspace
	m, cmd := base.enterWatch(f.src)
	if !m.watch.isActive || cmd == nil {
		t.Fatal("setup: enterWatch did not enter watch mode")
	}
	m = foldAll(t, m, expandBatch(cmd))
	if !m.watch.hasGitSnap || m.watch.gitSnap.Branch != untracked().Branch {
		t.Fatalf("setup: the watched git read did not land (%+v), so the footer shows no watched git segment", m.watch.gitSnap)
	}

	y, first, _ := footerGitCells(t, m)
	watched := step(t, m, leftClick(first, y))
	if watched.picker.open {
		t.Errorf("a click on the git segment while watching opened picker %+v, want none", watched.picker)
	}
	if watched.flash != watchRefusedFlash {
		t.Errorf("flash = %q, want the watch-refused flash %q", watched.flash, watchRefusedFlash)
	}

	left := step(t, m, keyEsc())
	if left.watch.isActive {
		t.Fatal("setup: esc did not leave the watch")
	}
	y, first, _ = footerGitCells(t, left)
	if live := step(t, left, leftClick(first, y)); !live.picker.open || live.picker.kind != pickerGitActions {
		t.Errorf("a click on the git segment after esc gave picker %+v, want the git actions picker", live.picker)
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
	loaded, ok := untickedMsg(t, cmd).(sessionLoadedMsg)
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
		if _, isLoad := untickedMsg(t, cmd).(sessionLoadedMsg); isLoad {
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
		if _, isLoad := untickedMsg(t, cmd).(sessionLoadedMsg); isLoad {
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
	loaded, ok := untickedMsg(t, load).(sessionLoadedMsg)
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

// The schedule Gate holds a due Firing on THIS session's activity (ADR 0033). While watching, the
// view mirrors the watched owner's run, and that run going busy must not defer the watcher's own
// Firings — so nothing is reported, during the watch or on the way out of it.
func TestWatchReportsTheWatchersOwnActivity(t *testing.T) {
	t.Parallel()
	var reports []bool
	m := activityModel(t, &reports)
	f := newWatchFixture(t, "watched", nil, 0)
	m, cmd := m.enterWatch(f.src)
	if !m.watch.isActive || cmd == nil {
		t.Fatal("setup: enterWatch did not enter watch mode")
	}
	m = step(t, m, cmd())

	f.prompt("theirs", false)
	m = pollWatch(t, m)
	if m.state != stateRunning {
		t.Fatalf("state = %v; want running while the watched Exchange is open", m.state)
	}
	if len(reports) != 0 {
		t.Errorf("reports while the watched run runs = %v; want none — this session is idle", reports)
	}

	m = step(t, m, keyEsc())
	if m.watch.isActive {
		t.Fatal("esc did not leave the watch")
	}
	if len(reports) != 0 {
		t.Errorf("reports after esc = %v; want none — this session never went busy", reports)
	}
}

// stampedWatchFixture is an empty watched session whose journal stamps every line with *stamp,
// which the test moves between appends: a journal written on a clock the test sets line by line.
// The opening frame is stamped with *stamp as it stands at the call.
func stampedWatchFixture(t *testing.T, stamp *time.Time) *watchFixture {
	t.Helper()
	f := newWatchFixture(t, "watched", nil, 0)
	f.now = func() time.Time { return *stamp }
	f.reopen("watched", 0)
	return f
}

// presentUsage is a depth-0 usage reading: a line that moves no activity slot (foldActivity), so a
// journal ending with one stamped at the present has its newest line say "now" and nothing more.
func presentUsage() domain.UsageEvent {
	return domain.UsageEvent{EventBase: domain.EventBase{Turn: 1}, PromptTokens: 10, TotalTokens: 20}
}

// statusClocks matches the running status line's phrase clock and its [total …] readout.
var statusClocks = regexp.MustCompile(`· ([\d:]+s) \[total ([\d:]+s)\]`)

// watchedClocks reads the phrase clock and the total off m's status line, as totalSuffix and
// formatElapsed write them.
func watchedClocks(t *testing.T, m Model) (phrase, total time.Duration) {
	t.Helper()
	got := statusText(t, m)
	match := statusClocks.FindStringSubmatch(got)
	if match == nil {
		t.Fatalf("status line = %q; want a running phrase clock and a [total …] readout", got)
	}
	return parseElapsed(t, match[1]), parseElapsed(t, match[2])
}

// parseElapsed reads back formatElapsed's "12s", "03:15s" and "01:02:03s".
func parseElapsed(t *testing.T, s string) time.Duration {
	t.Helper()
	var secs int
	for _, part := range strings.Split(strings.TrimSuffix(s, "s"), ":") {
		n, err := strconv.Atoi(part)
		if err != nil {
			t.Fatalf("elapsed %q does not parse: %v", s, err)
		}
		secs = secs*secondsPerMinute + n
	}
	return time.Duration(secs) * time.Second
}

// TestWatchTotalCountsFromTheWatchedPrompt pins the total's origin in a watch: the watched
// prompt's own journal line, not the moment the watcher attached. A prompt stamped five minutes
// before the journal's newest line shows a total of at least five minutes at once.
func TestWatchTotalCountsFromTheWatchedPrompt(t *testing.T) {
	t.Parallel()
	stamp := time.Now()
	f := stampedWatchFixture(t, &stamp)
	stamp = time.Now().Add(-5 * time.Minute)
	f.prompt("theirs", false)
	stamp = time.Now()
	f.emit(presentUsage())

	m := enterWatchOn(t, watchEngine(), f)

	if m.state != stateRunning {
		t.Fatalf("state = %v; want running while the watched Exchange is open", m.state)
	}
	if _, total := watchedClocks(t, m); total < 5*time.Minute {
		t.Errorf("total = %v; want at least 5m — the prompt's line is five minutes before the newest", total)
	}
}

// TestWatchPhraseClockCountsFromItsJournalLine pins the phrase clock's origin in a watch: the line
// that set the phrase, not the read that folded it.
func TestWatchPhraseClockCountsFromItsJournalLine(t *testing.T) {
	t.Parallel()
	stamp := time.Now()
	f := stampedWatchFixture(t, &stamp)
	stamp = time.Now().Add(-3 * time.Minute)
	f.prompt("theirs", false)
	stamp = time.Now().Add(-2 * time.Minute)
	f.emit(domain.ToolCallEvent{EventBase: domain.EventBase{Turn: 1}, Call: domain.ToolCall{
		ID: "r1", Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`),
	}})
	stamp = time.Now()
	f.emit(presentUsage())

	m := enterWatchOn(t, watchEngine(), f)

	phrase, total := watchedClocks(t, m)
	if phrase < 2*time.Minute {
		t.Errorf("phrase clock = %v; want at least 2m — the call's line is two minutes before the newest", phrase)
	}
	if total < 3*time.Minute {
		t.Errorf("total = %v; want at least 3m — the prompt's line is three minutes before the newest", total)
	}
}

// TestWatchClockIgnoresOwnerClockOffset pins the clock mapping: a watch measures only distances
// between the journal's own stamps, so an owner whose clock runs hours ahead of or behind the
// watcher's still shows the five minutes its prompt is old — never a negative, epoch-sized or
// hours-large total.
func TestWatchClockIgnoresOwnerClockOffset(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		offset time.Duration
	}{
		{name: "owner ahead", offset: 3 * time.Hour},
		{name: "owner behind", offset: -3 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stamp := time.Now().Add(tc.offset)
			f := stampedWatchFixture(t, &stamp)
			stamp = time.Now().Add(tc.offset - 5*time.Minute)
			f.prompt("theirs", false)
			stamp = time.Now().Add(tc.offset)
			f.emit(presentUsage())

			m := enterWatchOn(t, watchEngine(), f)

			if _, total := watchedClocks(t, m); total < 5*time.Minute || total >= 6*time.Minute {
				t.Errorf("total = %v; want about 5m whatever the owner's clock says", total)
			}
		})
	}
}

// TestWatchInterjectionKeepsTheTotalOrigin pins that an Interjection never moves the total's
// origin: the Exchange is still the one the top-level prompt opened.
func TestWatchInterjectionKeepsTheTotalOrigin(t *testing.T) {
	t.Parallel()
	stamp := time.Now()
	f := stampedWatchFixture(t, &stamp)
	stamp = time.Now().Add(-5 * time.Minute)
	f.prompt("theirs", false)
	stamp = time.Now().Add(-1 * time.Minute)
	f.prompt("and also this", true)
	stamp = time.Now()
	f.emit(presentUsage())

	m := enterWatchOn(t, watchEngine(), f)

	if _, total := watchedClocks(t, m); total < 5*time.Minute {
		t.Errorf("total = %v; want at least 5m — the Interjection must not restart it", total)
	}
}
