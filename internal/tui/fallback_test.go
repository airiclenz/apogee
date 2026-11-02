package tui

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// The TUI half of a Server fallback (ADR 0105): the engine already rehomed a seat onto a fallback
// server mid-Turn and announced it with one UpstreamSwitchedEvent. Every seat's switch is one note;
// the session seat's is also followed — the host moves its Monitor and the display adopts the
// fallback's server, model and window — so the heartbeat beats the fallback from then on without a
// second "model changed" note, and `/server` back to the primary is a real move.

// fallbackWindow is the window the fake host says the engine was bound to on the fallback.
const fallbackWindow = 8192

// sessionSwitch is the session seat's switch from the seeded session's server onto "remote".
func sessionSwitch(depth int) domain.UpstreamSwitchedEvent {
	return domain.UpstreamSwitchedEvent{
		EventBase:  domain.EventBase{Depth: depth},
		FromServer: "test-host", FromModel: "test-model",
		ToServer: "remote", ToModel: "remote-model",
		Fault: "status 503", Waited: 2 * time.Second,
		Seat: domain.UpstreamSeatSession,
	}
}

// fakeFollow stands in for the composition root's follow of a committed fallback: it records every
// server it was asked to follow and answers with what the binary would have adopted.
type fakeFollow struct {
	calls []string
	err   error
}

func (f *fakeFollow) follow(name string) (ServerFallbackResult, error) {
	f.calls = append(f.calls, name)
	if f.err != nil {
		return ServerFallbackResult{}, f.err
	}
	return ServerFallbackResult{
		Endpoint:       "http://remote:8080",
		HostAlias:      name,
		Model:          "remote-model",
		ContextWindow:  fallbackWindow,
		ObservedWindow: fallbackWindow,
	}, nil
}

// seededFallback is a ready, idle session on test-host with the switch and the follow seams wired.
func seededFallback(t *testing.T, follow *fakeFollow) (Model, *fakeSwitch, *fakeRebind) {
	t.Helper()
	sw := &fakeSwitch{}
	opts := testOpts
	seams := serverSeams(&opts)
	seams.list, seams.switchTo, seams.follow = staticServers(twoServers), sw.switchTo, follow.follow
	m, rb := seededPicker(t, opts)
	return m, sw, rb
}

// The session seat's switch, whichever agent on the seat hit the fault — the top-level one or an
// unrouted child — writes one note and is followed: the display adopts the fallback, the next beat
// off it is no change, and `/server test-host` afterwards is a move back rather than "already on".
func TestFallbackSessionSeatSwitchIsNotedAndFollowed(t *testing.T) {
	t.Parallel()
	for _, depth := range []int{0, 1} {
		t.Run("depth "+strconv.Itoa(depth), func(t *testing.T) {
			t.Parallel()
			follow := &fakeFollow{}
			m, sw, rb := seededFallback(t, follow)
			notesBefore := len(noteTexts(m))

			m, cmd := stepCmd(t, m, eventMsg{Event: sessionSwitch(depth)})

			if want := []string{"remote"}; !reflect.DeepEqual(follow.calls, want) {
				t.Fatalf("follow calls = %v, want %v", follow.calls, want)
			}
			want := "server fallback: test-host (test-model) failed (status 503); " +
				"continuing on remote (remote-model) after waiting 2s"
			if got := noteTexts(m)[notesBefore:]; !reflect.DeepEqual(got, []string{want}) {
				t.Errorf("new notes = %q, want only %q", got, want)
			}
			if m.opts.Endpoint != "http://remote:8080" || m.opts.HostAlias != "remote" ||
				m.opts.Model != "remote-model" || m.opts.ContextWindow != fallbackWindow {
				t.Errorf("opts = {%q %q %q %d}, want the fallback adopted", m.opts.Endpoint,
					m.opts.HostAlias, m.opts.Model, m.opts.ContextWindow)
			}
			if box := m.transcript.entries[0]; box.kind != entryStartup || box.startup.Host != "remote" {
				t.Errorf("start-up box = %+v, want it restated on the fallback", box.startup)
			}
			if cmd == nil {
				t.Fatal("the follow armed no beat — a beat in flight against the server left would stay live")
			}

			notesBefore = len(noteTexts(m))
			m = foldBeatMsg(t, m, upBeat("remote-model", fallbackWindow))

			if len(rb.calls) != 0 {
				t.Errorf("rebind calls = %v, want none — the fallback's beat is no change", rb.calls)
			}
			if got := noteTexts(m)[notesBefore:]; len(got) != 0 {
				t.Errorf("the fallback's first beat wrote %q, want no second note", got)
			}

			m, _ = typeCommand(t, m, "/server test-host")

			if want := []string{"test-host"}; !reflect.DeepEqual(sw.calls, want) {
				t.Errorf("switch calls = %v, want %v — the session is on remote now", sw.calls, want)
			}
			if m.opts.HostAlias != "test-host" {
				t.Errorf("HostAlias = %q, want the session moved back to test-host", m.opts.HostAlias)
			}
		})
	}
}

// A background workflow child on the session's live holder that hit the fault moved the session's
// own seat: its switch is workflow-owned, yet it is noted once and followed like any other.
func TestFallbackBackgroundSessionSeatSwitchIsFollowed(t *testing.T) {
	t.Parallel()
	follow := &fakeFollow{}
	m, _, _ := seededFallback(t, follow)
	m = foldEvents(t, m, bgPhase(domain.WorkflowStarted))
	switched := sessionSwitch(1)
	switched.EventBase = bgChildBase("run-bg-1")
	if !m.workflows.owns(switched) {
		t.Fatal("the background child's switch is not workflow-owned; the test would not reach the owned path")
	}
	notesBefore := len(noteTexts(m))

	m, cmd := stepCmd(t, m, eventMsg{Event: switched})

	if want := []string{"remote"}; !reflect.DeepEqual(follow.calls, want) {
		t.Fatalf("follow calls = %v, want %v", follow.calls, want)
	}
	want := "server fallback: test-host (test-model) failed (status 503); " +
		"continuing on remote (remote-model) after waiting 2s"
	if got := noteTexts(m)[notesBefore:]; !reflect.DeepEqual(got, []string{want}) {
		t.Errorf("new notes = %q, want only %q", got, want)
	}
	if m.opts.HostAlias != "remote" || m.opts.Model != "remote-model" || m.opts.ContextWindow != fallbackWindow {
		t.Errorf("opts = {%q %q %d}, want the fallback adopted", m.opts.HostAlias, m.opts.Model, m.opts.ContextWindow)
	}
	if cmd == nil {
		t.Error("the follow armed no beat — a beat in flight against the server left would stay live")
	}
}

// A Sub-agents seat's switch is noted, naming that seat's servers, and moves no part of the session:
// nothing is followed and the display stays on the session's server.
func TestFallbackSubAgentsSeatSwitchIsNotedAndNotFollowed(t *testing.T) {
	t.Parallel()
	follow := &fakeFollow{}
	m, _, _ := seededFallback(t, follow)
	before := m.opts
	notesBefore := len(noteTexts(m))
	switched := domain.UpstreamSwitchedEvent{
		EventBase:  domain.EventBase{Depth: 1},
		FromServer: "delegate", FromModel: "small",
		ToServer: "delegate-backup", ToModel: "smaller",
		Fault: "connection", Waited: 0,
		Seat: domain.UpstreamSeatSubAgents,
	}

	m = step(t, m, eventMsg{Event: switched})

	if len(follow.calls) != 0 {
		t.Errorf("follow calls = %v, want none — a Sub-agents seat is not the session's", follow.calls)
	}
	want := "sub-agents server fallback: delegate (small) failed (connection); " +
		"continuing on delegate-backup (smaller) after waiting 0s"
	if got := noteTexts(m)[notesBefore:]; !reflect.DeepEqual(got, []string{want}) {
		t.Errorf("new notes = %q, want only %q", got, want)
	}
	if m.opts.Endpoint != before.Endpoint || m.opts.HostAlias != before.HostAlias || m.opts.Model != before.Model {
		t.Errorf("opts moved to {%q %q %q}, want the session's server kept", m.opts.Endpoint,
			m.opts.HostAlias, m.opts.Model)
	}
}

// A watcher of another session's journal folds the switch to its note and follows nothing: the
// server it would follow is not this process's.
func TestFallbackWatchedSwitchIsNotedAndNotFollowed(t *testing.T) {
	t.Parallel()
	follow := &fakeFollow{}
	m, _, _ := seededFallback(t, follow)

	m = m.foldWatchedEvent(sessionSwitch(0))

	if len(follow.calls) != 0 {
		t.Errorf("follow calls = %v, want none for a watched journal", follow.calls)
	}
	if n := countNotes(m, "server fallback: test-host"); n != 1 {
		t.Errorf("fallback notes = %d, want 1; notes = %q", n, noteTexts(m))
	}
	if m.opts.HostAlias != "test-host" {
		t.Errorf("HostAlias = %q, want the display left on test-host", m.opts.HostAlias)
	}
}

// A host with nothing to follow says so once beside the fallback note, and the display stays put.
func TestFallbackFollowRefusedLeavesTheDisplay(t *testing.T) {
	t.Parallel()
	follow := &fakeFollow{err: errors.New("the seat never landed there")}
	m, _, _ := seededFallback(t, follow)

	m = step(t, m, eventMsg{Event: sessionSwitch(0)})

	if m.opts.HostAlias != "test-host" || m.opts.Model != "test-model" {
		t.Errorf("opts = {%q %q}, want the display left on test-host", m.opts.HostAlias, m.opts.Model)
	}
	notes := noteTexts(m)
	if len(notes) == 0 || !strings.Contains(notes[len(notes)-1], "could not follow remote: the seat never landed there") {
		t.Errorf("notes = %q, want the refusal said last", notes)
	}
}

// The note names a server with no known model by its name alone.
func TestFallbackNoteNamesAServerWithNoModelAlone(t *testing.T) {
	t.Parallel()
	switched := sessionSwitch(0)
	switched.FromModel = ""

	got := fallbackNote(switched)

	want := "server fallback: test-host failed (status 503); continuing on remote (remote-model) after waiting 2s"
	if got != want {
		t.Errorf("fallbackNote = %q, want %q", got, want)
	}
}
