package tui

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/testtime"
)

// ----------------------------------------------------------------------------
// The tick-stripping helper
// ----------------------------------------------------------------------------

// untickedMsgs runs cmd the way the program runs a Cmd — every member of a batch on its own goroutine,
// a nested batch flattened into its members — and streams each Msg the tree yields other than nil and
// a spinnerTickMsg. It is the one way a test reads a Cmd that a busy wait may have batched the
// spinner's first tick onto (busyAfterFold): the tick is the indicator's own business, so a test
// about the launch, the pump or a refusal sees exactly what it saw before the wait spun.
//
// The channel closes once every member has returned. A member that never returns — an actuation
// listener whose producer has already handed its item to a sibling — leaves it open, so a reader takes
// what it needs and bounds its own wait; the parked goroutine is the test's to leak, as the program
// would have dropped it with the Cmd.
func untickedMsgs(cmd tea.Cmd) <-chan tea.Msg {
	out := make(chan tea.Msg)
	var members sync.WaitGroup
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		defer members.Done()
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case nil, spinnerTickMsg:
		case tea.BatchMsg:
			for _, member := range msg {
				members.Add(1)
				go run(member)
			}
		default:
			out <- msg
		}
	}
	members.Add(1)
	go run(cmd)
	go func() {
		members.Wait()
		close(out)
	}()
	return out
}

// untickedAll drains untickedMsgs to its end and returns what it yielded — for a Cmd whose every
// member returns, such as a refusal's, whose only member may be the spinner's tick. A Cmd still
// unfinished after the budget fails the test: it holds a member that parks, which no refusal does.
func untickedAll(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	var got []tea.Msg
	msgs := untickedMsgs(cmd)
	deadline := time.After(testtime.Budget(5 * time.Second))
	for {
		select {
		case msg, open := <-msgs:
			if !open {
				return got
			}
			got = append(got, msg)
		case <-deadline:
			t.Fatalf("the Cmd was still running after the budget; it yielded %v so far", got)
			return got
		}
	}
}

// untickedMsg runs cmd as untickedAll does and returns the one Msg it carries beside the spinner's
// ticks — nil when it carries none. It stands in for `cmd()` where a wait the busy indicator narrates
// (busy.go) batched its first tick beside a Cmd that used to stand alone. More than one Msg is a
// test's misreading of the Cmd, and fails it.
func untickedMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	msgs := untickedAll(t, cmd)
	switch len(msgs) {
	case 0:
		return nil
	case 1:
		return msgs[0]
	}
	t.Fatalf("the Cmd carried %d Msgs beside its ticks, want at most one: %v", len(msgs), msgs)
	return nil
}

// ----------------------------------------------------------------------------
// The status line during a wait
// ----------------------------------------------------------------------------

// plainGlyph is the spinner's glyph at m's current frame, styling stripped — what the status line's
// busy form leads with in a plain frame.
func plainGlyph(m Model) string {
	return ansiPattern.ReplaceAllString(m.spin.view(m.th, m.opts.UI.Spinner, false), "")
}

// A /model load an idle session is waiting on is narrated in the running row's shape — the glyph, the
// footer's own word for the wait and a clock from the moment it began — and the chain that turns the
// glyph is running.
func TestBusyLoadShowsSpinnerPhraseClock(t *testing.T) {
	t.Parallel()

	m, _ := wireLauncher(t, newLauncher())
	m, _ = startLoad(t, m, "alpha")
	if m.state != stateIdle {
		t.Fatalf("precondition: state = %v during a load, want idle", m.state)
	}

	want := plainGlyph(m) + " loading alpha… · "
	if frame := plain(m.View()); !strings.Contains(frame, want) {
		t.Errorf("the frame does not narrate the load as %q:\n%s", want, frame)
	}
	if !m.spin.isLive {
		t.Fatal("the load began with no spinner chain alive")
	}
	if m.busySince.since.IsZero() {
		t.Fatal("the load's clock was never started")
	}
	later := ansiPattern.ReplaceAllString(m.busyPhrase(m.busySince.since.Add(64*time.Second)), "")
	if !strings.Contains(later, "loading alpha… · 01:04s") {
		t.Errorf("busy phrase a minute and four seconds in = %q, want the clock at 01:04s", later)
	}

	ticked, cmd := stepCmd(t, m, spinnerTickMsg{gen: m.spin.gen})
	if cmd == nil {
		t.Error("a live tick during the load did not schedule the next — the glyph would freeze")
	}
	if got, want := ticked.spin.frame, m.spin.frame+1; got != want {
		t.Errorf("frame after a tick during the load = %d, want %d", got, want)
	}
}

// A wired heartbeat that has not bound a model yet is a wait too: the status line says "connecting…"
// beside the turning glyph, and the beat that binds a model ends it.
func TestBusyConnectingShowsSpinner(t *testing.T) {
	t.Parallel()

	m := wireRebind(t, unbound(testOpts), &fakeHeartbeat{}, &fakeRebind{})
	want := plainGlyph(m) + " " + connectingLabel + " · "
	if left := ansiPattern.ReplaceAllString(m.statusLeft(), ""); !strings.Contains(left, want) {
		t.Errorf("status line = %q, want it to carry %q before the first bind", left, want)
	}
	if !m.spin.isLive {
		t.Fatal("the cold start's first frame armed no spinner chain")
	}

	bound := foldBeatMsg(t, m, upBeat("served-model", 32768))
	if bound.opts.Model == "" {
		t.Fatal("precondition: the beat bound no model")
	}
	if left := ansiPattern.ReplaceAllString(bound.statusLeft(), ""); strings.Contains(left, connectingLabel) {
		t.Errorf("status line = %q after the bind, want the wait gone from it", left)
	}
}

// The clock belongs to ONE wait: a load handing over to "connecting…" is a new wait whose clock starts
// again, while a wait that is still the same one keeps its origin however many folds pass.
func TestBusyClockRestartsOnHandover(t *testing.T) {
	t.Parallel()

	m := wireHeartbeat(t, unbound(testOpts), &fakeHeartbeat{})
	if m.busySince.phrase != connectingLabel || m.busySince.since.IsZero() {
		t.Fatalf("precondition: busy clock = %+v, want it stamped for %q", m.busySince, connectingLabel)
	}

	loadAt := m.busySince.since.Add(time.Minute)
	loading := m
	loading.actuation.inFlight, loading.actuation.verb, loading.actuation.profile = true, verbLoad, "big"
	loading.restampBusy(loadAt)
	if got, want := loading.busySince, (busyClock{phrase: "loading big…", since: loadAt}); got != want {
		t.Fatalf("busy clock as the load began = %+v, want %+v", got, want)
	}
	loading.restampBusy(loadAt.Add(time.Minute))
	if got := loading.busySince.since; !got.Equal(loadAt) {
		t.Errorf("the same wait was restamped at %v, want its origin %v kept", got, loadAt)
	}

	handedAt := loadAt.Add(2 * time.Minute)
	handed := loading
	handed.actuation.inFlight = false
	handed.restampBusy(handedAt)
	if got, want := handed.busySince, (busyClock{phrase: connectingLabel, since: handedAt}); got != want {
		t.Errorf("busy clock after the hand-over = %+v, want %+v — a new wait, a new clock", got, want)
	}

	// Update's tail is what restamps in the program: any fold that changes the wait moves the clock.
	unstamped := m
	unstamped.actuation = loading.actuation
	folded := step(t, unstamped, tea.FocusMsg{})
	if folded.busySince.phrase != "loading big…" || folded.busySince.since.Before(m.busySince.since) {
		t.Errorf("busy clock after a fold of the hand-over = %+v, want it stamped for the load", folded.busySince)
	}
}

// Once the wait ends the slot is exactly what it was before it began, and the clock is cleared.
func TestBusyEndsClearsSlot(t *testing.T) {
	t.Parallel()

	m, _ := wireLauncher(t, newLauncher())
	before := m.statusLeft()
	m, cmd := startLoad(t, m, "alpha")
	m, _ = driveActuation(t, m, cmd)

	if got := m.statusLeft(); got != before {
		t.Errorf("status line after the load = %q, want it back to %q", got, before)
	}
	if m.busySince != (busyClock{}) {
		t.Errorf("busy clock after the load = %+v, want it cleared", m.busySince)
	}
}

// The chain the wait armed dies with the wait: the first tick to land after it ends is dropped, marks
// the chain dead, and no later fold of an idle session re-arms it.
func TestBusyChainStopsWhenWaitEnds(t *testing.T) {
	t.Parallel()

	m, _ := wireLauncher(t, newLauncher())
	m, cmd := startLoad(t, m, "alpha")
	m, _ = driveActuation(t, m, cmd)
	if !m.spin.isLive {
		t.Fatal("precondition: the load's chain was not live when it ended")
	}

	m, cmd = stepCmd(t, m, spinnerTickMsg{gen: m.spin.gen})
	if cmd != nil {
		t.Error("a tick after the wait ended scheduled another — the chain outlived the wait")
	}
	if m.spin.isLive {
		t.Error("the dropped tick left the chain marked live")
	}
	if m, cmd = stepCmd(t, m, tea.FocusMsg{}); cmd != nil || m.spin.isLive {
		t.Errorf("an idle fold with no wait re-armed the chain (cmd=%v, live=%v)", cmd != nil, m.spin.isLive)
	}
}

// An offline session is refused, not waiting, and a pre-bound one has no server a beat could reach
// (ADR 0036): neither narrates "connecting…" in the status line nor runs a single tick.
func TestBusyOfflineAndPreboundRunNoTicks(t *testing.T) {
	t.Parallel()

	offline := wireHeartbeat(t, unbound(testOpts), &fakeHeartbeat{})
	offline = foldBeatMsg(t, offline, downBeat("connection refused"))
	if !offline.hb.offline {
		t.Fatal("precondition: a cold-start failure did not take the session offline")
	}
	if phrase, ok := offline.busyWait(); ok {
		t.Errorf("an offline session is waiting on %q, want no wait", phrase)
	}
	offline, cmd := stepCmd(t, offline, spinnerTickMsg{gen: offline.spin.gen})
	if cmd != nil || offline.spin.isLive {
		t.Errorf("an offline session kept its chain (cmd=%v, live=%v)", cmd != nil, offline.spin.isLive)
	}
	if offline, cmd = stepCmd(t, offline, tea.FocusMsg{}); cmd != nil || offline.spin.isLive {
		t.Errorf("an offline idle fold armed the chain (cmd=%v, live=%v)", cmd != nil, offline.spin.isLive)
	}
	if left := ansiPattern.ReplaceAllString(offline.statusLeft(), ""); strings.Contains(left, connectingLabel) {
		t.Errorf("offline status line = %q, want no busy form", left)
	}

	prebound, _ := preboundModel(t, PreboundFirstBoot, "", &fakeBind{}, &fakeRecorder{})
	if phrase, ok := prebound.busyWait(); ok {
		t.Errorf("a pre-bound session is waiting on %q, want no wait", phrase)
	}
	if prebound.spin.isLive {
		t.Error("a pre-bound session armed the spinner chain")
	}
	prebound = step(t, prebound, tea.FocusMsg{})
	if prebound.spin.isLive || prebound.spin.gen != 0 {
		t.Errorf("a pre-bound idle fold armed the chain (live=%v, generation %d)", prebound.spin.isLive, prebound.spin.gen)
	}
}

// ----------------------------------------------------------------------------
// The chain's liveness and the live star
// ----------------------------------------------------------------------------

// Only the drop of the CURRENT generation marks the chain dead. A tick left over from a retired chain
// says nothing about the chain that replaced it — were it to clear liveness, Update's tail would
// re-arm, retire the chain just armed, and its own tick would land stale in turn: a spinner frozen at
// frame 0.
func TestSpinnerStaleTickKeepsChainLive(t *testing.T) {
	t.Parallel()

	running := newTestModel(t)
	running.input.SetValue("hello")
	running = step(t, running, keyEnter())
	if !running.spin.isLive {
		t.Fatal("submit armed a chain it did not mark live")
	}
	retired := running.spin.gen
	if tick := running.resumeRunning(); tick == nil {
		t.Fatal("resume scheduled no tick")
	}

	stale := step(t, running, spinnerTickMsg{gen: retired})
	if !stale.spin.isLive {
		t.Error("a stale tick marked the running chain dead")
	}

	idle := running
	idle.state = stateIdle
	if left := step(t, idle, spinnerTickMsg{gen: retired}); !left.spin.isLive {
		t.Error("a stale tick in idle marked the current chain dead — only the current one's drop may")
	}
	if died := step(t, idle, spinnerTickMsg{gen: idle.spin.gen}); died.spin.isLive {
		t.Error("the current chain's dropped tick left it marked live")
	}
}

// An idle busy wait advances the frame for the status line's glyph, but the live star says "still
// going under the running Turn": a running workflow block in idle never flips, whichever phase the
// wait's ticks carry the frame through.
func TestLiveStarHoldsOutsideRunning(t *testing.T) {
	t.Parallel()

	m := wireHeartbeat(t, unbound(testOpts), &fakeHeartbeat{})
	m.transcript.apply(startedWith("scan"))
	if !m.transcript.hasLiveStar() {
		t.Fatal("precondition: the running workflow block is not a live header")
	}
	m.refreshViewport()
	painted := strings.Join(m.lines, "\n")

	for range 2 * m.spin.framesPerBlinkHalf(m.opts.UI.Spinner) {
		var cmd tea.Cmd
		m, cmd = stepCmd(t, m, spinnerTickMsg{gen: m.spin.gen})
		if cmd == nil {
			t.Fatal("the connecting wait's chain stopped ticking")
		}
		if m.liveStarBlink() || m.painted.blink {
			t.Fatalf("at frame %d the live star took the blinked-out phase outside a running Turn", m.spin.frame)
		}
		if got := strings.Join(m.lines, "\n"); got != painted {
			t.Fatalf("at frame %d a busy tick repainted the transcript:\n%s", m.spin.frame, got)
		}
	}
}

// ----------------------------------------------------------------------------
// The waits held under a hold or a latch of their own
// ----------------------------------------------------------------------------

// busyLeft is the status line's left side, styling stripped.
func busyLeft(m Model) string {
	return ansiPattern.ReplaceAllString(m.statusLeft(), "")
}

// A /sessions record load is a busy wait from the ⏎ that issues it until its record lands: the status
// line carries the glyph, "loading session…" and the clock, and the restore clears the slot.
func TestBusySessionLoad(t *testing.T) {
	t.Parallel()

	m, _ := filterableBrowser(t)
	m, cmd := stepCmd(t, m, keyEnter())
	if !m.holds.has(holdSessionLoad) {
		t.Fatal("precondition: ⏎ in the browser took no session load hold")
	}

	if want := plainGlyph(m) + " " + sessionLoadPhrase + " · "; !strings.Contains(busyLeft(m), want) {
		t.Errorf("status line = %q during the load, want it to carry %q", busyLeft(m), want)
	}
	if !m.spin.isLive {
		t.Fatal("the load began with no spinner chain alive")
	}
	if m.busySince.phrase != sessionLoadPhrase || m.busySince.since.IsZero() {
		t.Errorf("busy clock = %+v, want it stamped for %q", m.busySince, sessionLoadPhrase)
	}

	loaded := untickedMsg(t, cmd)
	if _, ok := loaded.(sessionLoadedMsg); !ok {
		t.Fatalf("the load's Cmd answered %T, want a sessionLoadedMsg", loaded)
	}
	m = step(t, m, loaded)
	if left := busyLeft(m); strings.Contains(left, sessionLoadPhrase) {
		t.Errorf("status line = %q once the record landed, want the wait gone from it", left)
	}
	if m.busySince != (busyClock{}) {
		t.Errorf("busy clock after the load = %+v, want it cleared", m.busySince)
	}
}

// A footer git action is a busy wait worded as its start note words it, `git <verb>…`, until it
// lands. Switch branch's first step, the branch listing, is held as a git action too but is no
// `git switch…`: it switches nothing, so the status line stays quiet until a branch is chosen.
func TestBusyGitAction(t *testing.T) {
	t.Parallel()

	t.Run("pull", func(t *testing.T) {
		t.Parallel()
		m, _ := gitActionsModel(t, tracked(), &fakeGitActionsHost{}, false)
		m, cmd := acceptGitRow(t, m, gitActionPull)

		if want := plainGlyph(m) + " git pull… · "; !strings.Contains(busyLeft(m), want) {
			t.Errorf("status line = %q during the pull, want it to carry %q", busyLeft(m), want)
		}
		if got := lastNote(m); got != gitActionPull.runningNote() {
			t.Errorf("start note = %q, want the busy phrase's own words %q", got, gitActionPull.runningNote())
		}
		if !m.spin.isLive {
			t.Fatal("the pull began with no spinner chain alive")
		}

		m = step(t, m, awaitGitMsg[gitActionDoneMsg](t, cmd, "git pull"))
		if left := busyLeft(m); strings.Contains(left, "git pull…") {
			t.Errorf("status line = %q once the pull landed, want the wait gone from it", left)
		}
		if m.busySince != (busyClock{}) {
			t.Errorf("busy clock after the pull = %+v, want it cleared", m.busySince)
		}
	})

	t.Run("switch branch", func(t *testing.T) {
		t.Parallel()
		m, _ := gitActionsModel(t, tracked(), &fakeGitActionsHost{branches: []string{"main", "topic"}}, false)
		listing, cmd := acceptGitRow(t, m, gitActionSwitch)
		if !listing.holds.has(holdGitAction) {
			t.Fatal("precondition: the branch listing runs without the git action hold")
		}
		if phrase, ok := listing.busyWait(); ok {
			t.Errorf("the branch listing is a busy wait %q, want none — it is no `git switch…`", phrase)
		}
		if left := busyLeft(listing); strings.Contains(left, "git switch…") {
			t.Errorf("status line = %q during the branch listing, want no `git switch…`", left)
		}

		m = step(t, listing, awaitGitMsg[gitActionDoneMsg](t, cmd, "the branch listing"))
		m, cmd = stepCmd(t, m, keyEnter())
		if want := plainGlyph(m) + " git switch… · "; !strings.Contains(busyLeft(m), want) {
			t.Errorf("status line = %q during the switch, want it to carry %q", busyLeft(m), want)
		}
		m = step(t, m, awaitGitMsg[gitActionDoneMsg](t, cmd, "git switch"))
		if left := busyLeft(m); strings.Contains(left, "git switch…") {
			t.Errorf("status line = %q once the switch landed, want the wait gone from it", left)
		}
	})
}

// A bare /rename is a busy wait from the naming call it issues until that call's reply folds: the
// status line says "naming this session…" beside the glyph and the clock, and the reply clears it —
// a failed reply as much as a usable one.
func TestBusyManualRename(t *testing.T) {
	t.Parallel()

	for _, fails := range []bool{false, true} {
		seam := &titleSeam{reply: "Title: the generated name"}
		if fails {
			seam = &titleSeam{err: errors.New("server gone")}
		}
		host := &fakeSessionHost{}
		host.Activate(session.Meta{ID: "s1"})
		m := newTitlingModel(t, host, seam, false)
		m, _ = sendPrompt(t, m, "fix the broken parser in tokenizer.go")
		m = idle(t, m)

		m, cmd := sendPrompt(t, m, "/rename")
		if !m.naming {
			t.Fatalf("fails %v: bare /rename issued its naming call with no naming latch set", fails)
		}
		if want := plainGlyph(m) + " " + namingPhrase + " · "; !strings.Contains(busyLeft(m), want) {
			t.Errorf("fails %v: status line = %q while naming, want it to carry %q", fails, busyLeft(m), want)
		}

		named := untickedMsg(t, cmd)
		if _, ok := named.(manualTitleMsg); !ok {
			t.Fatalf("fails %v: bare /rename's Cmd answered %T, want a manualTitleMsg", fails, named)
		}
		m, cmd = stepCmd(t, m, named)
		m = runWrites(t, m, cmd)
		if m.naming {
			t.Errorf("fails %v: the naming latch outlived the reply", fails)
		}
		if left := busyLeft(m); strings.Contains(left, namingPhrase) {
			t.Errorf("fails %v: status line = %q once the reply folded, want the wait gone from it", fails, left)
		}
	}
}
