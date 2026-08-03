package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
)

// ----------------------------------------------------------------------------
// Automatic session naming (session-auto-titling plan, item 5)
// ----------------------------------------------------------------------------
//
// These pin the state machine, not the completion: the seam is a recorder, so what is proved is
// WHEN a session is named, whether the answer is applied, and — just as load-bearing — that every
// failure path leaves the conversation exactly as it was.

// titleSeam is a recording [Options.GenerateTitle]: it captures the WINDOW each naming call was
// made about and answers with a scripted reply or error, so a test can prove both that the call
// fired and what it was asked. The whole window is kept rather than its first entry, so a caller
// that starts sending more than one prompt fails an assertion instead of passing silently. It is
// concurrency-safe because the naming Cmd runs on its own goroutine.
type titleSeam struct {
	mu    sync.Mutex
	calls [][]string
	reply string
	err   error
}

func (s *titleSeam) generate(_ context.Context, prompts []string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, append([]string(nil), prompts...))
	return s.reply, s.err
}

// asked returns the windows the seam was called with, one per call, in order.
func (s *titleSeam) asked() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]string(nil), s.calls...)
}

// titlingOpts are the Options a naming test runs under: a persistence host to name a record in, a
// workspace, and the toggle plus seam under test. A nil seam models an unwired generator.
func titlingOpts(host SessionHost, seam *titleSeam, autoTitle bool) Options {
	opts := Options{Sessions: host, Workspace: "/ws", AutoTitle: autoTitle}
	if seam != nil {
		opts.GenerateTitle = seam.generate
	}
	return opts
}

// newTitlingModel builds a ready, idle model wired for naming over an engine whose every Step
// completes the Exchange at once — so a test may run submit's whole batch, worker Cmd and all,
// without the drive blocking or an unscripted Step panicking.
func newTitlingModel(t *testing.T, host SessionHost, seam *titleSeam, autoTitle bool) Model {
	t.Helper()
	eng := &fakeEngine{stepFn: func(context.Context, int) (domain.StepResult, error) {
		return domain.StepResult{Status: domain.StatusExchangeComplete}, nil
	}}
	m := newModel(context.Background(), eng, titlingOpts(host, seam, autoTitle), nil)
	return step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
}

// sendPrompt types text and presses ⏎, returning the model and the Cmd submit batched.
func sendPrompt(t *testing.T, m Model, text string) (Model, tea.Cmd) {
	t.Helper()
	m.input.SetValue(text)
	return stepCmd(t, m, keyEnter())
}

// namingCallDeadline bounds how long a batch is given to yield its naming call. It only has to
// outlast the recorder seam (which answers instantly) and the spinner's parked tick.
const namingCallDeadline = 2 * time.Second

// namingCall runs cmd and every Cmd it batches, each on its own goroutine, and returns the
// autoTitleMsg one of them produced. Submit's batch carries the worker drive and a parked spinner
// tick beside the naming call, so the members are run concurrently and abandoned rather than
// waited on in order: a batch with no naming call in it costs one short wait instead of a hang.
func namingCall(t *testing.T, cmd tea.Cmd) (autoTitleMsg, bool) {
	t.Helper()
	if cmd == nil {
		return autoTitleMsg{}, false
	}
	out := make(chan tea.Msg, 16)
	pending := 1
	go func() { out <- cmd() }()
	deadline := time.After(namingCallDeadline)
	for pending > 0 {
		select {
		case msg := <-out:
			pending--
			switch v := msg.(type) {
			case autoTitleMsg:
				return v, true
			case tea.BatchMsg:
				for _, c := range v {
					if c == nil {
						continue
					}
					pending++
					go func() { out <- c() }()
				}
			}
		case <-deadline:
			return autoTitleMsg{}, false
		}
	}
	return autoTitleMsg{}, false
}

// The first prompt of a fresh session fires exactly one naming call, carrying the submitted text;
// the next prompt of the same session fires none — one Session record, one cosmetic call.
func TestAutoTitleFiresOnceOnTheFirstPrompt(t *testing.T) {
	t.Parallel()

	seam := &titleSeam{reply: "fix the broken parser"}
	host := &fakeSessionHost{}
	m := newTitlingModel(t, host, seam, true)

	m, cmd := sendPrompt(t, m, "please fix the broken parser in tokenizer.go")
	if !m.autoTitleFired {
		t.Fatal("the first prompt left autoTitleFired unset; the session would name itself twice")
	}
	msg, ok := namingCall(t, cmd)
	if !ok {
		t.Fatal("the first prompt batched no naming call")
	}
	if msg.title != "fix the broken parser" {
		t.Errorf("naming call yielded %q, want the seam's reply", msg.title)
	}
	// One call, and its window is the submitted prompt ALONE: the automatic form names a session
	// from the one request that exists when it fires.
	want := [][]string{{"please fix the broken parser in tokenizer.go"}}
	if !reflect.DeepEqual(seam.asked(), want) {
		t.Errorf("seam asked about %q, want the submitted prompt %q", seam.asked(), want)
	}

	// Back to idle, then a second prompt: the latch holds, so nothing new is asked.
	m = step(t, m, exchangeDoneMsg{})
	m, cmd = sendPrompt(t, m, "now add a test for it")
	if _, ok := namingCall(t, cmd); ok {
		t.Error("the second prompt fired a naming call; a Session record earns exactly one")
	}
	if got := len(seam.asked()); got != 1 {
		t.Errorf("seam called %d times, want 1 (the first prompt only)", got)
	}
}

// The three ways naming stays quiet at submit: the config toggle off, an unwired seam, and a
// resumed record (which already has a name). Each is proved on the gate itself, so the assertion is
// "no Cmd was produced" rather than "no Msg turned up in time".
func TestAutoTitleDoesNotFire(t *testing.T) {
	t.Parallel()

	resumedOpts := titlingOpts(&fakeSessionHost{}, &titleSeam{}, true)
	resumedOpts.Resumed = &ResumedSession{Title: "an older task"}

	cases := []struct {
		name string
		opts Options
	}{
		{"auto-title off", titlingOpts(&fakeSessionHost{}, &titleSeam{}, false)},
		{"seam unwired", titlingOpts(&fakeSessionHost{}, nil, true)},
		{"no persistence host", titlingOpts(nil, &titleSeam{}, true)},
		{"resumed session", resumedOpts},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := newModel(context.Background(), &fakeEngine{}, tc.opts, nil)
			if cmd := m.maybeAutoTitle("fix the parser"); cmd != nil {
				t.Error("a naming call fired; want none")
			}
		})
	}
}

// A result that lands once the record has an id renames it straight away — Rename is the only
// writer of a stored title, because Save fixes the title at create.
func TestAutoTitleAppliesThroughRename(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	host.Activate(session.Meta{ID: "s1", Title: "please fix the broken parser in token…"})
	m := newTitlingModel(t, host, &titleSeam{}, true)

	m, cmd := stepCmd(t, m, autoTitleMsg{title: "  Title: \"Fix the broken parser\"  "})
	if cmd == nil {
		t.Fatal("a landed title with an active record dispatched no rename")
	}
	// The write reports back so the queue can release its latch, but it carries NO re-list: a
	// sessionListMsg would open the browser over a title nobody asked to see.
	done, ok := cmdMsg(cmd).(recordWriteDoneMsg)
	if !ok {
		t.Fatalf("the rename Cmd yielded %T; want a recordWriteDoneMsg releasing the write latch", cmdMsg(cmd))
	}
	if done.write.relist || len(done.list.metas) != 0 {
		t.Errorf("the quiet rename carried a re-list (%+v); it must add no UI chrome", done.list)
	}
	m = step(t, m, done)
	if m.pendingTitle != "" {
		t.Errorf("pendingTitle = %q, want empty (the title was applied, not stashed)", m.pendingTitle)
	}
	want := []renameCall{{id: "s1", title: "Fix the broken parser"}}
	if got := host.renamedTitles(); !reflect.DeepEqual(got, want) {
		t.Errorf("renames = %+v, want %+v (sanitized: label and quotes stripped)", got, want)
	}
	if m.sessionBrowser.open {
		t.Error("applying a generated title opened the /sessions browser; naming adds no UI chrome")
	}
}

// A result that beats the first Save has no id to rename yet, so it is stashed — and applied at the
// save-complete that put the record on disk.
func TestAutoTitleStashedUntilTheFirstSave(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	m := newTitlingModel(t, host, &titleSeam{}, true)

	m, cmd := stepCmd(t, m, autoTitleMsg{title: "fix the broken parser"})
	if cmd != nil {
		t.Error("a title that arrived before any id dispatched a rename; there is nothing to rename")
	}
	if m.pendingTitle != "fix the broken parser" {
		t.Fatalf("pendingTitle = %q, want the sanitized title stashed", m.pendingTitle)
	}
	if got := host.renamedTitles(); len(got) != 0 {
		t.Fatalf("renames = %+v, want none before the record exists", got)
	}

	// The first Save mints the id (as the real host does, before its saveDoneMsg lands).
	if err := host.Save(domain.Session{}, nil, "heuristic title", 1, 0); err != nil {
		t.Fatalf("seeding the first Save: %v", err)
	}
	m, cmd = stepCmd(t, m, saveDoneMsg{})
	if cmd == nil {
		t.Fatal("the first successful save-complete dispatched no rename for the stashed title")
	}
	cmdMsg(cmd)
	if m.pendingTitle != "" {
		t.Errorf("pendingTitle = %q, want cleared once applied", m.pendingTitle)
	}
	want := []renameCall{{id: "s1", title: "fix the broken parser"}}
	if got := host.renamedTitles(); !reflect.DeepEqual(got, want) {
		t.Errorf("renames = %+v, want %+v", got, want)
	}
}

// A save that FAILED has not put the record on disk, so the stash is held for the next save rather
// than spent on a rename the store would drop.
func TestAutoTitleStashSurvivesAFailedSave(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	host.Activate(session.Meta{ID: "s1"})
	m := newTitlingModel(t, host, &titleSeam{}, true)
	m.pendingTitle = "fix the broken parser"

	m, cmd := stepCmd(t, m, saveDoneMsg{Err: errors.New("disk full")})
	cmdMsg(cmd)
	if m.pendingTitle != "fix the broken parser" {
		t.Errorf("pendingTitle = %q, want the stash held past a failed save", m.pendingTitle)
	}
	if got := host.renamedTitles(); len(got) != 0 {
		t.Errorf("renames = %+v, want none: the record was never written", got)
	}
}

// A title landing while a per-Turn save is in flight WAITS. Dispatching it there is what let a
// record write roll a Turn off disk: Store.Rename re-reads the record and writes it back whole, so
// one running beside a Save overwrites it with the pre-save version (audit 2026-08-01). The
// assertions are at the fold layer — which Cmd went out, and what is still queued — because the
// recording host serialises its own calls and so cannot witness the collision.
func TestAutoTitleQueuesBehindAnInFlightSave(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	host.Activate(session.Meta{ID: "s1"})
	m := newTitlingModel(t, host, &titleSeam{}, true)
	m.transcript.addUser("fix the broken parser in tokenizer.go", nil)

	// A per-Turn save goes out and is left in flight: its Cmd is held rather than run.
	m, saveCmd := stepCmd(t, m, turnSnapshotMsg{Sess: domain.Session{State: []byte(`{"turn":2}`)}})
	if saveCmd == nil {
		t.Fatal("the per-Turn snapshot scheduled no save")
	}

	m, titleCmd := stepCmd(t, m, autoTitleMsg{title: "fix the broken parser"})
	if titleCmd != nil {
		t.Fatal("a title landing during an in-flight save dispatched a parallel record write")
	}
	if got := host.renamedTitles(); len(got) != 0 {
		t.Fatalf("renames = %+v, want none until the save has finished", got)
	}
	if len(m.pendingWrites) != 1 || m.pendingWrites[0].kind != writeRename {
		t.Fatalf("pending writes = %+v, want the title rename queued behind the save", m.pendingWrites)
	}

	// The save completes. Its fold dispatches the queued rename as ONE Cmd — never a tea.Batch,
	// whose members would run on separate goroutines and put the two writes back in a race.
	m, next := stepCmd(t, m, cmdMsg(saveCmd))
	if next == nil {
		t.Fatal("the save-complete dispatched no queued write")
	}
	msg := cmdMsg(next)
	if _, batched := msg.(tea.BatchMsg); batched {
		t.Fatal("the save-complete batched its writes; batch members run on separate goroutines")
	}
	if _, ok := msg.(recordWriteDoneMsg); !ok {
		t.Fatalf("the save-complete dispatched %T, want the queued rename", msg)
	}
	m = step(t, m, msg)

	if calls := host.savedCalls(); len(calls) != 1 {
		t.Errorf("Save calls = %d, want the one in-flight save", len(calls))
	}
	want := []renameCall{{id: "s1", title: "fix the broken parser"}}
	if got := host.renamedTitles(); !reflect.DeepEqual(got, want) {
		t.Errorf("renames = %+v, want %+v after the save, never beside it", got, want)
	}
	if len(m.pendingWrites) != 0 {
		t.Errorf("pending writes = %+v, want an empty queue", m.pendingWrites)
	}
}

// The early window: the apply path branches on ActiveID(), which the host mints at the START of the
// first Save — before the atomic write has put the file on disk — so a title answering inside that
// window renames a record that is not there. It used to be discarded on the spot; now it goes back
// on the stash and lands at the next successful save.
func TestTitleSurvivesARenameOfARecordNotOnDiskYet(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	host.Activate(session.Meta{ID: "s1"}) // an id exists; the file behind it does not yet
	host.failRenames(errors.New("no such file or directory"))
	m := newTitlingModel(t, host, &titleSeam{}, true)

	m, cmd := stepCmd(t, m, autoTitleMsg{title: "fix the broken parser"})
	if cmd == nil {
		t.Fatal("a landed title with an active id dispatched no rename")
	}
	m = step(t, m, cmdMsg(cmd)) // the rename runs, hits ENOENT, and folds back
	if m.pendingTitle != "fix the broken parser" || m.pendingSource != titleAutomatic {
		t.Fatalf("stash = %q from %v, want the unwritable title held for the next save", m.pendingTitle, m.pendingSource)
	}

	// The record reaches disk; the next successful save re-applies the title it could not write.
	host.failRenames(nil)
	m.transcript.addUser("fix the broken parser in tokenizer.go", nil)
	m, cmd = stepCmd(t, m, turnSnapshotMsg{Sess: domain.Session{}})
	if cmd == nil {
		t.Fatal("the per-Turn snapshot scheduled no save")
	}
	m = runWrites(t, m, cmd)

	want := []renameCall{
		{id: "s1", title: "fix the broken parser"}, // the attempt that found no record
		{id: "s1", title: "fix the broken parser"}, // the retry that stuck
	}
	if got := host.renamedTitles(); !reflect.DeepEqual(got, want) {
		t.Errorf("renames = %+v, want %+v (the failed write retried, not dropped)", got, want)
	}
	if m.pendingTitle != "" {
		t.Errorf("pendingTitle = %q, want cleared once the retry landed", m.pendingTitle)
	}
}

// A human who names the session first wins: the browser's `r` sets the never-clobber flag and a
// naming call landing afterwards is dropped without a word.
func TestAutoTitleDroppedAfterABrowserRename(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	host.Activate(session.Meta{ID: "s1", Title: "old"})
	storeMeta(host, "s1", "old", "/ws", time.Now(), 0, nil)
	m := newTitlingModel(t, host, &titleSeam{}, true)
	m = openBrowser(t, m)

	m = step(t, m, keyRune('r'))
	m.sessionBrowser.renameBuf = "my own name"
	m, cmd := stepCmd(t, m, keyEnter())
	cmdMsg(cmd) // run the browser's rename+re-list off the loop
	if !m.titleTouched {
		t.Fatal("committing a browser rename left titleTouched unset")
	}

	before := host.renamedTitles()
	m, cmd = stepCmd(t, m, autoTitleMsg{title: "a generated name"})
	if cmd != nil {
		t.Error("a generated title was applied over a session the human had just named")
	}
	if m.pendingTitle != "" {
		t.Errorf("pendingTitle = %q, want empty (the dropped title is not stashed either)", m.pendingTitle)
	}
	if got := host.renamedTitles(); !reflect.DeepEqual(got, before) {
		t.Errorf("renames = %+v, want only the human's %+v", got, before)
	}
}

// Every failure path is silent: a failed call and a reply nothing survives sanitizing both leave the
// transcript untouched, nothing stashed, and the stored title alone.
func TestAutoTitleFailuresAreSilent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		msg  autoTitleMsg
	}{
		{"call failed", autoTitleMsg{err: errors.New("upstream refused")}},
		{"reply unusable", autoTitleMsg{title: "```\n```"}},
		{"reply empty", autoTitleMsg{title: "   "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			host := &fakeSessionHost{}
			host.Activate(session.Meta{ID: "s1", Title: "heuristic title"})
			m := newTitlingModel(t, host, &titleSeam{}, true)
			before := append([]entry(nil), m.transcript.entries...)

			m, cmd := stepCmd(t, m, tc.msg)
			if cmd != nil {
				t.Error("a failed naming call dispatched a rename")
			}
			if m.pendingTitle != "" {
				t.Errorf("pendingTitle = %q, want empty", m.pendingTitle)
			}
			if got := host.renamedTitles(); len(got) != 0 {
				t.Errorf("renames = %+v, want none", got)
			}
			if !reflect.DeepEqual(m.transcript.entries, before) {
				t.Error("a failed naming call wrote to the transcript; it must never nag")
			}
		})
	}
}

// /new rotates to a fresh Session record, and a fresh record names itself: the latch and both
// never-clobber fields reset, so the next first prompt fires again.
func TestAutoTitleFiresAgainAfterNewSession(t *testing.T) {
	t.Parallel()

	seam := &titleSeam{reply: "the second task"}
	host := &fakeSessionHost{}
	m := newTitlingModel(t, host, seam, true)

	m, cmd := sendPrompt(t, m, "the first task")
	if _, ok := namingCall(t, cmd); !ok {
		t.Fatal("the first prompt batched no naming call")
	}
	m = step(t, m, exchangeDoneMsg{})
	m.titleTouched = true // a rename during the outgoing session must not follow it into the next

	m, _ = sendPrompt(t, m, "/new")
	if m.autoTitleFired || m.titleTouched || m.pendingTitle != "" {
		t.Fatalf("/new left naming state behind: fired=%v touched=%v pending=%q",
			m.autoTitleFired, m.titleTouched, m.pendingTitle)
	}

	m, cmd = sendPrompt(t, m, "the second task, described")
	if _, ok := namingCall(t, cmd); !ok {
		t.Fatal("the first prompt of the rotated session fired no naming call")
	}
	if got := len(seam.asked()); got != 2 {
		t.Errorf("seam called %d times, want 2 (one per session)", got)
	}
}

// Naming is cosmetic and out-of-band: a landed title changes nothing about the CONVERSATION. Two
// models fed identical input differ only in the naming Msg, and their transcripts stay identical
// entry for entry.
//
// The rendered view is no longer identical, and that is the design rather than a leak: the top rule
// wears the session's name (sessionrule.go), so a landed title lands on that ONE row — where the
// model that never named itself keeps the heuristic name its opening request gave it. Every other
// row must still match, which is what "the naming call is not a Turn" now means for the frame.
func TestAutoTitleLeavesTheTranscriptUntouched(t *testing.T) {
	t.Parallel()

	named := &fakeSessionHost{}
	named.Activate(session.Meta{ID: "s1"})
	bare := &fakeSessionHost{}
	bare.Activate(session.Meta{ID: "s1"})

	withTitle := newTitlingModel(t, named, &titleSeam{}, true)
	without := newTitlingModel(t, bare, &titleSeam{}, false)

	withTitle, _ = sendPrompt(t, withTitle, "fix the broken parser in tokenizer.go")
	without, _ = sendPrompt(t, without, "fix the broken parser in tokenizer.go")
	withTitle, cmd := stepCmd(t, withTitle, autoTitleMsg{title: "fix the broken parser"})
	cmdMsg(cmd)

	if got := named.renamedTitles(); len(got) != 1 {
		t.Fatalf("renames = %+v, want the one generated title (the setup must actually land it)", got)
	}
	if !reflect.DeepEqual(withTitle.transcript.entries, without.transcript.entries) {
		t.Error("a landed auto-title changed the transcript; the naming call is not a Turn")
	}
	withRows := strings.Split(plain(withTitle.View()), "\n")
	withoutRows := strings.Split(plain(without.View()), "\n")
	if len(withRows) != len(withoutRows) {
		t.Fatalf("views are %d and %d rows; a landed title changed the frame's row count",
			len(withRows), len(withoutRows))
	}
	ruleRow := -1
	for i := range withRows {
		if strings.Contains(withRows[i], "▔") {
			ruleRow = i
			break
		}
	}
	if ruleRow < 0 {
		t.Fatalf("no ▔ top rule row in the rendered view")
	}
	for i := range withRows {
		if i != ruleRow && withRows[i] != withoutRows[i] {
			t.Errorf("row %d differs with a landed auto-title:\n%q\n%q", i, withRows[i], withoutRows[i])
		}
	}
	if got := withRows[ruleRow]; !strings.Contains(got, "fix the broken parser") ||
		strings.Contains(got, "tokenizer.go") {
		t.Errorf("top rule = %q, want the landed title in place of the heuristic name", got)
	}
	if got := withoutRows[ruleRow]; !strings.Contains(got, "tokenizer.go") {
		t.Errorf("unnamed top rule = %q, want the heuristic name from the opening request", got)
	}
	if withTitle.ctxUsed != without.ctxUsed || withTitle.tokPerSec != without.tokPerSec {
		t.Error("a landed auto-title moved the token accounting; it emits no Usage event")
	}
}

// Resuming a stored session through the /sessions browser latches naming off for the rest of the
// run — the resumed record already has a name — and drops any title stashed for the session it
// replaced.
func TestAutoTitleLatchedByAResumedSession(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	storeMeta(host, "s1", "an older task", "/ws", time.Now(), 0, nil)
	m := newTitlingModel(t, host, &titleSeam{}, true)
	m.pendingTitle = "a name for the session being replaced"

	m = openBrowser(t, m)
	m, cmd := stepCmd(t, m, keyEnter()) // ⏎ resumes: the load runs off the loop…
	if cmd == nil {
		t.Fatal("resuming the selected session dispatched no load")
	}
	m = step(t, m, cmdMsg(cmd)) // …and its sessionLoadedMsg is what restores the record

	if !m.autoTitleFired {
		t.Error("a resumed session left naming unlatched; it would rename a record that has a name")
	}
	if m.pendingTitle != "" {
		t.Errorf("pendingTitle = %q, want dropped: it was stashed for the outgoing session", m.pendingTitle)
	}
}

// ----------------------------------------------------------------------------
// /rename — the human's half of the naming machinery (item 6)
// ----------------------------------------------------------------------------

// lastNote returns the transcript's most recent note. /rename answers in notes — unlike the
// automatic call it was asked for — so every case below reads the one it left behind.
func lastNote(m Model) string {
	notes := noteTexts(m)
	if len(notes) == 0 {
		return ""
	}
	return notes[len(notes)-1]
}

// idle returns the model to stateIdle after a submitted prompt, so the idle-only /rename may run.
// The idle boundary schedules a save, and that save is driven to completion here: a record write
// left in flight holds the single-flight latch, so a title scheduled afterwards would still be
// waiting in the queue rather than at the host (model.go).
func idle(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := stepCmd(t, m, exchangeDoneMsg{})
	return runWrites(t, m, cmd)
}

// `/rename <text>` names the session outright: the tokens re-join with single spaces, run through
// the same sanitizer a generated title does, and land through Rename — with titleTouched set, so
// nothing generated may overwrite what the human typed.
func TestRenameWithArgumentsNamesTheSession(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	host.Activate(session.Meta{ID: "s1", Title: "please fix the broken parser in token…"})
	m := newTitlingModel(t, host, &titleSeam{}, true)

	m, cmd := sendPrompt(t, m, `/rename   the "parser"   rewrite.`)
	cmdMsg(cmd) // the quiet rename runs off the loop

	if !m.titleTouched {
		t.Error("a manual rename left titleTouched unset; a generated title could still overwrite it")
	}
	want := []renameCall{{id: "s1", title: `the "parser" rewrite`}}
	if got := host.renamedTitles(); !reflect.DeepEqual(got, want) {
		t.Errorf("renames = %+v, want %+v (joined on single spaces, sanitized)", got, want)
	}
	if got, want := lastNote(m), `session renamed: the "parser" rewrite`; got != want {
		t.Errorf("note = %q, want %q (the record exists, so it really was renamed)", got, want)
	}
	if m.sessionBrowser.open {
		t.Error("/rename opened the /sessions browser; it names this session in place")
	}
}

// A name that sanitizes away to nothing is refused with the form that always works, and changes
// nothing — the same posture a mistyped /confine gets, for the same reason.
func TestRenameWithAnEmptyNameIsRefused(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	host.Activate(session.Meta{ID: "s1", Title: "heuristic title"})
	m := newTitlingModel(t, host, &titleSeam{}, true)

	m, cmd := sendPrompt(t, m, "/rename ```")
	if cmd != nil {
		t.Error("a refused rename dispatched a Cmd")
	}
	if m.titleTouched {
		t.Error("a refused rename set titleTouched; nothing was named")
	}
	if got := lastNote(m); !strings.Contains(got, renameUsage) {
		t.Errorf("note = %q, want the usage line", got)
	}
	if got := host.renamedTitles(); len(got) != 0 {
		t.Errorf("renames = %+v, want none", got)
	}
}

// The two ways a bare /rename cannot ask the model. Both SAY so: the automatic call is silent
// because nobody asked for it, and this one was asked for by name.
func TestRenameBareRefusals(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		seam   *titleSeam
		prompt string
		want   string
	}{
		{"seam unwired", nil, "fix the broken parser", "title generation is not available"},
		{"nothing said yet", &titleSeam{reply: "a title"}, "", "nothing to name yet"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			host := &fakeSessionHost{}
			host.Activate(session.Meta{ID: "s1", Title: "heuristic title"})
			m := newTitlingModel(t, host, tc.seam, false)
			if tc.prompt != "" {
				m, _ = sendPrompt(t, m, tc.prompt)
				m = idle(t, m)
			}

			m, cmd := sendPrompt(t, m, "/rename")
			if cmd != nil {
				t.Error("a refused /rename dispatched a naming call")
			}
			if got := lastNote(m); !strings.Contains(got, tc.want) {
				t.Errorf("note = %q, want it to carry %q", got, tc.want)
			}
			if got := host.renamedTitles(); len(got) != 0 {
				t.Errorf("renames = %+v, want none", got)
			}
		})
	}
}

// Without a persistence host there is no Session record, so there is no name to change — and
// /rename says that rather than reporting a title it could not store.
func TestRenameWithoutPersistenceSaysSo(t *testing.T) {
	t.Parallel()

	m := newTitlingModel(t, nil, &titleSeam{reply: "a generated name"}, true)

	m, cmd := sendPrompt(t, m, "/rename the parser rewrite")
	if cmd != nil {
		t.Error("/rename dispatched a Cmd with no session to name")
	}
	if got := lastNote(m); !strings.Contains(got, "not being saved") {
		t.Errorf("note = %q, want it to say the session is not being saved", got)
	}
	if m.titleTouched || m.pendingTitle != "" {
		t.Errorf("naming state moved: touched=%v pending=%q", m.titleTouched, m.pendingTitle)
	}
}

// Bare /rename regenerates on demand — including with auto-titling switched OFF, which gates only
// the automatic firing (Ratified design 7) — and its answer applies OVER a name the human set by
// hand a moment ago: the request is the newer instruction.
func TestRenameBareRegeneratesOverAManualName(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	host.Activate(session.Meta{ID: "s1"})
	seam := &titleSeam{reply: "Title: the generated name"}
	m := newTitlingModel(t, host, seam, false)
	m, _ = sendPrompt(t, m, "fix the broken parser in tokenizer.go")
	m = idle(t, m)

	m, cmd := sendPrompt(t, m, "/rename my own name")
	m = runWrites(t, m, cmd)

	m, cmd = sendPrompt(t, m, "/rename")
	if cmd == nil {
		t.Fatal("bare /rename dispatched no naming call")
	}
	msg, ok := cmdMsg(cmd).(manualTitleMsg)
	if !ok {
		t.Fatal("bare /rename did not yield a manualTitleMsg")
	}
	m, cmd = stepCmd(t, m, msg)
	m = runWrites(t, m, cmd)

	want := []renameCall{{id: "s1", title: "my own name"}, {id: "s1", title: "the generated name"}}
	if got := host.renamedTitles(); !reflect.DeepEqual(got, want) {
		t.Errorf("renames = %+v, want %+v (the asked-for title wins over the typed one)", got, want)
	}
	if !m.titleTouched {
		t.Error("a regenerated title left titleTouched unset; an automatic one could overwrite it")
	}
	if got := lastNote(m); !strings.Contains(got, "the generated name") {
		t.Errorf("note = %q, want the new title reported back", got)
	}
	// The window is asserted whole, not just its first entry. It holds one prompt here because this
	// session made exactly one REQUEST: a slash command is driven, never submitted as a user message,
	// so the `/rename my own name` above is not part of the user side that bare /rename reads
	// (TestRenameBareSendsTheWholeUserSide is where the multi-request window is pinned).
	wantWindow := [][]string{{"fix the broken parser in tokenizer.go"}}
	if asked := seam.asked(); !reflect.DeepEqual(asked, wantWindow) {
		t.Errorf("seam asked about %q, want the session's one request %q", asked, wantWindow)
	}
}

// A bare /rename typed LATE names the session from its whole user side, oldest first — not from the
// opening request alone. That is the point of the verb: a session that started on one task and moved
// to another has to be nameable for where it ended up, and only the later requests say where that
// is. What the naming call then does with the window (bounding it, excerpting it, asking for the
// dominant thread) is title.Prompt's business, not the renderer's; all the seam is owed here is
// every request, in the order they were made.
func TestRenameBareSendsTheWholeUserSide(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	host.Activate(session.Meta{ID: "s1"})
	seam := &titleSeam{reply: "rework the token cache"}
	m := newTitlingModel(t, host, seam, false) // auto-title off: only /rename reaches the seam

	// A session that opens on the parser and moves on to the cache — the case the widening exists
	// for, since naming it from prompt 1 alone would file it under the task it left.
	prompts := []string{
		"fix the broken parser in tokenizer.go",
		"now rework the token cache",
		"make the cache eviction LRU",
	}
	for _, p := range prompts {
		m, _ = sendPrompt(t, m, p)
		m = idle(t, m)
	}

	m, cmd := sendPrompt(t, m, "/rename")
	if cmd == nil {
		t.Fatal("bare /rename dispatched no naming call")
	}
	if _, ok := cmdMsg(cmd).(manualTitleMsg); !ok {
		t.Fatal("bare /rename did not yield a manualTitleMsg")
	}
	want := [][]string{prompts}
	if got := seam.asked(); !reflect.DeepEqual(got, want) {
		t.Errorf("seam asked about %q, want every user message in order %q", got, want)
	}
}

// The automatic call is untouched by the widening (Ratified design 6): it hands the seam exactly ONE
// request even when the transcript already holds several, because its window is the prompt being
// submitted rather than anything read back off the transcript. The latch is cleared by hand instead
// of through /new because /new also empties the scrollback (startNewSession) — and a transcript full
// of requests is exactly the condition this has to hold under.
func TestAutoTitleWindowIsTheSubmittedPromptAlone(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	host.Activate(session.Meta{ID: "s1"})
	seam := &titleSeam{reply: "a generated name"}
	m := newTitlingModel(t, host, seam, true)

	m, cmd := sendPrompt(t, m, "fix the broken parser in tokenizer.go")
	if _, ok := namingCall(t, cmd); !ok {
		t.Fatal("the first prompt batched no naming call")
	}
	m = idle(t, m)
	m, _ = sendPrompt(t, m, "now rework the token cache")
	m = idle(t, m)

	m.autoTitleFired = false // a fresh Session record unlatches naming; its scrollback is not fresh
	m, cmd = sendPrompt(t, m, "make the cache eviction LRU")
	if _, ok := namingCall(t, cmd); !ok {
		t.Fatal("the unlatched record fired no naming call")
	}

	want := [][]string{{"fix the broken parser in tokenizer.go"}, {"make the cache eviction LRU"}}
	if got := seam.asked(); !reflect.DeepEqual(got, want) {
		t.Errorf("seam asked about %q, want one submitted prompt per call %q", got, want)
	}
}

// Interjections are not requests, so they stay out of the window — the line firstUserText already
// draws, kept for the same reason: a remark steering work already under way corrects a task instead
// of asking for a new one, and letting a mid-Exchange "wrong file" count as a request would let it
// outweigh the task it was correcting.
func TestRenameBareExcludesInterjections(t *testing.T) {
	t.Parallel()

	t.Run("beside requests", func(t *testing.T) {
		t.Parallel()

		host := &fakeSessionHost{}
		host.Activate(session.Meta{ID: "s1"})
		seam := &titleSeam{reply: "a generated name"}
		m := newTitlingModel(t, host, seam, false)

		m, _ = sendPrompt(t, m, "fix the broken parser in tokenizer.go")
		// The delivery fold records a mid-Exchange remark exactly this way (interject.go).
		m.transcript.addInterjected("no, the other tokenizer", nil)
		m = idle(t, m)
		m, _ = sendPrompt(t, m, "now rework the token cache")
		m = idle(t, m)

		m, cmd := sendPrompt(t, m, "/rename")
		if cmd == nil {
			t.Fatal("bare /rename dispatched no naming call")
		}
		cmdMsg(cmd)
		want := [][]string{{"fix the broken parser in tokenizer.go", "now rework the token cache"}}
		if got := seam.asked(); !reflect.DeepEqual(got, want) {
			t.Errorf("seam asked about %q, want the requests without the interjection %q", got, want)
		}
	})

	t.Run("alone", func(t *testing.T) {
		t.Parallel()

		host := &fakeSessionHost{}
		host.Activate(session.Meta{ID: "s1", Title: "heuristic title"})
		m := newTitlingModel(t, host, &titleSeam{reply: "a generated name"}, false)
		m.transcript.addInterjected("no, the other tokenizer", nil)

		m, cmd := sendPrompt(t, m, "/rename")
		if cmd != nil {
			t.Error("a session that has asked for nothing dispatched a naming call")
		}
		if got := lastNote(m); !strings.Contains(got, "nothing to name yet") {
			t.Errorf("note = %q, want the nothing-to-name refusal: an interjection is not a request", got)
		}
		if got := host.renamedTitles(); len(got) != 0 {
			t.Errorf("renames = %+v, want none", got)
		}
	})
}

// A bare /rename that comes back with nothing usable says so and leaves the stored title alone —
// quietly, with the form that always works.
func TestRenameBareFailureNotesAndKeepsTheTitle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		seam *titleSeam
	}{
		{"call failed", &titleSeam{err: errors.New("upstream refused")}},
		{"reply unusable", &titleSeam{reply: "```\n```"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			host := &fakeSessionHost{}
			host.Activate(session.Meta{ID: "s1", Title: "heuristic title"})
			m := newTitlingModel(t, host, tc.seam, false)
			m, _ = sendPrompt(t, m, "fix the broken parser")
			m = idle(t, m)

			m, cmd := sendPrompt(t, m, "/rename")
			if cmd == nil {
				t.Fatal("bare /rename dispatched no naming call")
			}
			m, cmd = stepCmd(t, m, cmdMsg(cmd))
			if cmd != nil {
				t.Error("a failed /rename dispatched a rename")
			}
			if got := lastNote(m); !strings.Contains(got, renameUsage) {
				t.Errorf("note = %q, want the usage line", got)
			}
			if got := host.renamedTitles(); len(got) != 0 {
				t.Errorf("renames = %+v, want none", got)
			}
			if m.titleTouched {
				t.Error("a failed /rename set titleTouched; nothing was named")
			}
		})
	}
}

// Naming a session BEFORE it has been saved stashes the title until the first Save mints an id —
// which is what lets a session be named before it has said anything — and that stash is the
// human's, so it flushes even though titleTouched is set.
func TestRenameBeforeTheFirstSaveStashesTheName(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{} // no active record yet
	m := newTitlingModel(t, host, &titleSeam{}, true)

	m, cmd := sendPrompt(t, m, "/rename the parser rewrite")
	if cmd != nil {
		t.Error("a rename dispatched one with no record to rename")
	}
	if m.pendingTitle != "the parser rewrite" || m.pendingSource != titleManual {
		t.Fatalf("stash = %q from %v, want the typed title from the human", m.pendingTitle, m.pendingSource)
	}
	if !m.titleTouched {
		t.Error("a stashed manual rename left titleTouched unset")
	}
	// Nothing on disk carries that name yet, so the note may report the name but not a rename.
	note := lastNote(m)
	if !strings.Contains(note, "the parser rewrite") {
		t.Errorf("note = %q, want the stashed name reported back", note)
	}
	if strings.Contains(note, "renamed") {
		t.Errorf("note = %q, claims a rename before any record existed to rename", note)
	}

	if err := host.Save(domain.Session{}, nil, "heuristic title", 1, 0); err != nil {
		t.Fatalf("seeding the first Save: %v", err)
	}
	m, cmd = stepCmd(t, m, saveDoneMsg{})
	cmdMsg(cmd)

	want := []renameCall{{id: "s1", title: "the parser rewrite"}}
	if got := host.renamedTitles(); !reflect.DeepEqual(got, want) {
		t.Errorf("renames = %+v, want %+v (the human's stash flushes over the heuristic title)", got, want)
	}
	if m.pendingTitle != "" {
		t.Errorf("pendingTitle = %q, want cleared once applied", m.pendingTitle)
	}
}

// The never-clobber rule holds ACROSS the stash: a generated title waiting for an id is dropped at
// the flush when a human named a session while it waited, rather than landing on the record the
// first Save has just minted.
func TestAutoTitleStashDroppedWhenAHumanNamesFirst(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{} // no active record: the naming call beat the first Save
	storeMeta(host, "old1", "an older task", "/ws", time.Now(), 0, nil)
	m := newTitlingModel(t, host, &titleSeam{}, true)

	m, cmd := stepCmd(t, m, autoTitleMsg{title: "a generated name"})
	if cmd != nil {
		t.Fatal("a title that arrived before any id dispatched a rename")
	}
	if m.pendingTitle != "a generated name" || m.pendingSource != titleAutomatic {
		t.Fatalf("stash = %q from %v, want the generated title from the naming call", m.pendingTitle, m.pendingSource)
	}

	// The human names a session by hand while the generated one waits for an id.
	m = openBrowser(t, m)
	m = step(t, m, keyRune('r'))
	m.sessionBrowser.renameBuf = "my own name"
	m, cmd = stepCmd(t, m, keyEnter())
	cmdMsg(cmd)
	if !m.titleTouched {
		t.Fatal("committing a browser rename left titleTouched unset")
	}

	// The first Save mints the id the stash was waiting for — and it must not be spent.
	if err := host.Save(domain.Session{}, nil, "heuristic title", 1, 0); err != nil {
		t.Fatalf("seeding the first Save: %v", err)
	}
	m, cmd = stepCmd(t, m, saveDoneMsg{})
	cmdMsg(cmd)

	if m.pendingTitle != "" {
		t.Errorf("pendingTitle = %q, want dropped once a human had named the session", m.pendingTitle)
	}
	want := []renameCall{{id: "old1", title: "my own name"}}
	if got := host.renamedTitles(); !reflect.DeepEqual(got, want) {
		t.Errorf("renames = %+v, want only the human's %+v", got, want)
	}
}

// ----------------------------------------------------------------------------
// The name the session goes by (session-name plan, item 1)
// ----------------------------------------------------------------------------
//
// sessionName is the naming machinery's DISPLAY side: what the human last saw decided, and what the
// frame renders. It is deliberately not the store's truth, so none of these can
// be answered by asking the host what it holds — what they pin is that the field follows every route
// that DECIDES a name, keeps standing when a route throws one away, and empties when the session it
// named goes into history.

// Every naming route feeds the field, and it feeds it from the decision rather than from the write
// that follows: a title stashed for an id the first Save has not minted yet is already the name this
// session goes by, so the frame carries it before anything on disk does. Both routes are driven at
// both moments — with a record to rename, and before one exists.
func TestSessionNameFollowsAppliedTitle(t *testing.T) {
	t.Parallel()

	routes := []struct {
		name  string
		drive func(t *testing.T, m Model) (Model, tea.Cmd)
		want  string // the name each route's raw input sanitizes down to
	}{
		{"an automatic naming call", func(t *testing.T, m Model) (Model, tea.Cmd) {
			t.Helper()
			return stepCmd(t, m, autoTitleMsg{title: `  Title: "Fix the broken parser"  `})
		}, "Fix the broken parser"},
		{"/rename <text>", func(t *testing.T, m Model) (Model, tea.Cmd) {
			t.Helper()
			return sendPrompt(t, m, `/rename   the "parser"   rewrite.`)
		}, `the "parser" rewrite`},
	}
	for _, r := range routes {
		for _, active := range []bool{true, false} {
			when := "with a record to rename"
			if !active {
				when = "before the first save"
			}
			t.Run(r.name+", "+when, func(t *testing.T) {
				t.Parallel()

				host := &fakeSessionHost{}
				if active {
					host.Activate(session.Meta{ID: "s1", Title: "heuristic title"})
				}
				m := newTitlingModel(t, host, &titleSeam{}, true)

				m, cmd := r.drive(t, m)
				m = runWrites(t, m, cmd)

				if m.sessionName != r.want {
					t.Errorf("sessionName = %q, want the sanitized name the route decided (%q)", m.sessionName, r.want)
				}
				if active {
					return
				}
				// The stash branch is the one no write can vouch for: nothing was renamed, because
				// there is no record to rename yet — the name exists on the Model alone.
				if m.pendingTitle != r.want {
					t.Errorf("pendingTitle = %q, want the name held for the id the first save mints", m.pendingTitle)
				}
				if got := host.renamedTitles(); len(got) != 0 {
					t.Errorf("renames = %+v, want none: the session was named before any record existed", got)
				}
			})
		}
	}
}

// The never-clobber rule DROPS a stashed automatic title at the flush when a human named the session
// while it waited (flushPendingTitle), and the frame must not be left wearing the name that was
// thrown away — a name nothing on disk ever carried. The human's /rename passed through applyTitle
// when it landed, so the field already holds theirs; what this pins is that the drop leaves it alone
// instead of spending the stash on its way out.
func TestSessionNameSurvivesDroppedAutoTitle(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{} // no active record: the naming call beat the first Save
	m := newTitlingModel(t, host, &titleSeam{}, true)

	m, cmd := stepCmd(t, m, autoTitleMsg{title: "a generated name"})
	if cmd != nil {
		t.Fatal("a title that arrived before any id dispatched a rename")
	}
	if m.sessionName != "a generated name" || m.pendingTitle != "a generated name" {
		t.Fatalf("sessionName = %q, stash = %q; want the generated name in both", m.sessionName, m.pendingTitle)
	}

	// The first Save mints the id (as the real host does, before its saveDoneMsg lands) and the human
	// names the session inside that window, so their name goes straight at the record while the
	// generated one is still on the stash waiting for a flush.
	if err := host.Save(domain.Session{}, nil, "heuristic title", 1, 0); err != nil {
		t.Fatalf("seeding the first Save: %v", err)
	}
	m, cmd = sendPrompt(t, m, "/rename my own name")
	m = runWrites(t, m, cmd)
	if !m.titleTouched {
		t.Fatal("a manual rename left titleTouched unset; the stash below would not be dropped at all")
	}

	m, cmd = stepCmd(t, m, saveDoneMsg{})
	m = runWrites(t, m, cmd)

	if m.pendingTitle != "" {
		t.Fatalf("pendingTitle = %q, want the generated stash dropped once a human had named the session", m.pendingTitle)
	}
	if m.sessionName != "my own name" {
		t.Errorf("sessionName = %q, want the human's name: the dropped title named nothing", m.sessionName)
	}
	want := []renameCall{{id: "s1", title: "my own name"}}
	if got := host.renamedTitles(); !reflect.DeepEqual(got, want) {
		t.Errorf("renames = %+v, want only the human's %+v", got, want)
	}
}

// The drop's OTHER path, and the one that can leave the frame lying: titleTouched is set by a
// browser rename of ANY row, so a human who renames some stored session while a generated title
// waits for an id trips the never-clobber rule without ever naming THIS one. The stash is dropped,
// the record keeps the heuristic title the first Save stamped — and the frame, which took the
// generated name when it was stashed, must give it up rather than wear a name nothing carries. It
// falls back to the heuristic, which is exactly what the record now says.
func TestSessionNameGivesUpADroppedAutoTitle(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{} // no active record: the naming call beat the first Save
	storeMeta(host, "old1", "an older task", "/ws", time.Now(), 0, nil)
	m := newTitlingModel(t, host, &titleSeam{}, true)

	m, _ = sendPrompt(t, m, "please fix the broken parser")
	m = step(t, m, exchangeDoneMsg{})
	m, cmd := stepCmd(t, m, autoTitleMsg{title: "a generated name"})
	m = runWrites(t, m, cmd)
	if m.sessionName != "a generated name" {
		t.Fatalf("sessionName = %q, want the stashed generated name", m.sessionName)
	}

	// The human renames a DIFFERENT, stored session in the browser: titleTouched is set, but nothing
	// about this session was named.
	m = openBrowser(t, m)
	m = step(t, m, keyRune('r'))
	m.sessionBrowser.renameBuf = "some other session"
	m, cmd = stepCmd(t, m, keyEnter())
	m = runWrites(t, m, cmd)
	if m.sessionName != "a generated name" {
		t.Fatalf("sessionName = %q, want it untouched by a rename of another row", m.sessionName)
	}

	// The first Save mints the id the stash was waiting for, and the flush drops it.
	if err := host.Save(domain.Session{}, nil, "heuristic title", 1, 0); err != nil {
		t.Fatalf("seeding the first Save: %v", err)
	}
	m, cmd = stepCmd(t, m, saveDoneMsg{})
	m = runWrites(t, m, cmd)

	if m.pendingTitle != "" {
		t.Fatalf("pendingTitle = %q, want the generated stash dropped", m.pendingTitle)
	}
	if m.sessionName != "" {
		t.Errorf("sessionName = %q, want the dropped name given up so the display falls back to the heuristic", m.sessionName)
	}
}

// /clear rotates to a fresh Session record, and a record nothing has been said in has no name — so
// the field empties with the naming fields beside it and the frame stops naming the session that
// just went into history.
func TestSessionNameResetByClear(t *testing.T) {
	t.Parallel()

	host := &fakeSessionHost{}
	host.Activate(session.Meta{ID: "s1", Title: "heuristic title"})
	m := newTitlingModel(t, host, &titleSeam{}, true)

	m, cmd := sendPrompt(t, m, "/rename the parser rewrite")
	m = runWrites(t, m, cmd)
	if m.sessionName != "the parser rewrite" {
		t.Fatalf("sessionName = %q, want the session named before /clear", m.sessionName)
	}

	m, cmd = sendPrompt(t, m, "/clear")
	m = runWrites(t, m, cmd)

	if m.sessionName != "" {
		t.Errorf("sessionName = %q after /clear, want empty: the record it opened has no name", m.sessionName)
	}
}
