package tui

import (
	"context"
	"errors"
	"reflect"
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

// titleSeam is a recording [Options.GenerateTitle]: it captures the text each naming call was made
// about and answers with a scripted reply or error, so a test can prove both that the call fired
// and what it was asked. It is concurrency-safe because the naming Cmd runs on its own goroutine.
type titleSeam struct {
	mu    sync.Mutex
	calls []string
	reply string
	err   error
}

func (s *titleSeam) generate(_ context.Context, firstUserText string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, firstUserText)
	return s.reply, s.err
}

// asked returns the prompts the seam was called about, in order.
func (s *titleSeam) asked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
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
	if want := []string{"please fix the broken parser in tokenizer.go"}; !reflect.DeepEqual(seam.asked(), want) {
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
	host := &fakeSessionHost{}
	host.Activate(session.Meta{ID: "s1", Title: "please fix the broken parser in token…"})
	m := newTitlingModel(t, host, &titleSeam{}, true)

	m, cmd := stepCmd(t, m, autoTitleMsg{title: "  Title: \"Fix the broken parser\"  "})
	if cmd == nil {
		t.Fatal("a landed title with an active record dispatched no rename")
	}
	if msg := cmdMsg(cmd); msg != nil {
		t.Errorf("the rename Cmd yielded %T; a quiet rename must report nothing (a sessionListMsg opens the browser)", msg)
	}
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

// A human who names the session first wins: the browser's `r` sets the never-clobber flag and a
// naming call landing afterwards is dropped without a word.
func TestAutoTitleDroppedAfterABrowserRename(t *testing.T) {
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

// Naming is cosmetic and out-of-band: a landed title changes nothing the human can see in the
// conversation. Two models fed identical input differ only in the naming Msg, and their transcripts
// stay identical entry for entry.
func TestAutoTitleLeavesTheTranscriptUntouched(t *testing.T) {
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
	if plain(withTitle.View()) != plain(without.View()) {
		t.Error("a landed auto-title changed the rendered view; titles surface only in the browser")
	}
	if withTitle.ctxUsed != without.ctxUsed || withTitle.tokPerSec != without.tokPerSec {
		t.Error("a landed auto-title moved the token accounting; it emits no Usage event")
	}
}

// Resuming a stored session through the /sessions browser latches naming off for the rest of the
// run — the resumed record already has a name — and drops any title stashed for the session it
// replaced.
func TestAutoTitleLatchedByAResumedSession(t *testing.T) {
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
