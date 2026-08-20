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

	"github.com/airiclenz/apogee/internal/format"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/scheme"
)

// ----------------------------------------------------------------------------
// Heartbeat harness
// ----------------------------------------------------------------------------

// The tick chain is proven the way the spinner's is (TestSpinnerTickChainGeneration): synthetic
// Msgs into Update, asserting the state, the transcript, and whether a Cmd was scheduled. Only the
// two Init tests actually RUN a Cmd; every fold test feeds the beat directly, so nothing waits on
// a real ten-second interval.

// fakeHeartbeat is a scripted beat source. Each call yields the next beat in the script and the
// last one repeats forever, so "down, down, then up" can be beaten as many times as a test likes.
// An empty script beats a reachable server. It is called from a Cmd goroutine, so its counter is
// guarded.
type fakeHeartbeat struct {
	mu     sync.Mutex
	calls  int
	script []heartbeat.Beat
}

func (f *fakeHeartbeat) beat(context.Context) heartbeat.Beat {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.script) == 0 {
		return heartbeat.Beat{Reachable: true, ActiveModel: "test-model", ContextWindow: 32768}
	}
	return f.script[min(f.calls-1, len(f.script)-1)]
}

// count reports how many beats have been asked for.
func (f *fakeHeartbeat) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// wireHeartbeat builds a ready, idle model over opts with hb wired into the monitor seam.
func wireHeartbeat(t *testing.T, opts Options, hb *fakeHeartbeat) Model {
	t.Helper()
	serverSeams(&opts).beat = hb.beat
	return newTestModelEng(t, &fakeEngine{}, opts)
}

// upBeat is one observation of a reachable server serving model in window tokens.
func upBeat(model string, window int) heartbeat.Beat {
	return heartbeat.Beat{
		Reachable:       true,
		ActiveModel:     model,
		ContextWindow:   window,
		AvailableModels: []heartbeat.ModelSummary{{ID: model, ContextWindow: window}},
	}
}

// downBeat is one observation that could not read the server — a finding, never an error.
func downBeat(failure string) heartbeat.Beat { return heartbeat.Beat{Failure: failure} }

// foldBeatMsg feeds one landed beat of the model's LIVE generation through Update.
func foldBeatMsg(t *testing.T, m Model, beat heartbeat.Beat) Model {
	t.Helper()
	return step(t, m, beatMsg{gen: m.hb.gen, beat: beat})
}

// fakeRebind stands in for the composition root's rebind closure: it records every (model, window)
// it was asked to bind and answers with what the binary would have bound — by default exactly what
// was observed, which is the unpinned case. It is called synchronously from the beat fold, on the
// test's own goroutine, so it needs no guard.
type fakeRebind struct {
	calls  []rebindCall
	answer func(model string, window int) (RebindResult, error)
}

// rebindCall is one binding the TUI asked the composition root to resolve and apply.
type rebindCall struct {
	model  string
	window int
}

func (f *fakeRebind) rebind(model string, window int) (RebindResult, error) {
	f.calls = append(f.calls, rebindCall{model: model, window: window})
	if f.answer != nil {
		return f.answer(model, window)
	}
	return RebindResult{Model: model, ContextWindow: window}, nil
}

// wireRebind builds a ready, idle model with BOTH upstream seams wired: hb observes, rb applies.
func wireRebind(t *testing.T, opts Options, hb *fakeHeartbeat, rb *fakeRebind) Model {
	t.Helper()
	serverSeams(&opts).rebind = rb.rebind
	return wireHeartbeat(t, opts, hb)
}

// unbound is opts with no model or window bound at launch — the async cold start the binary hands
// the TUI once startup discovery is the first beat.
func unbound(opts Options) Options {
	opts.Model, opts.ContextWindow = "", 0
	return opts
}

// noteTexts returns every transcript note in display order, so a whole beat script's narration can
// be asserted as one sequence rather than as a bag of substrings.
func noteTexts(m Model) []string {
	var out []string
	for _, e := range m.transcript.entries {
		if e.kind == entryNote {
			out = append(out, e.text)
		}
	}
	return out
}

// countNotes counts the transcript notes containing want.
func countNotes(m Model, want string) int {
	n := 0
	for _, e := range m.transcript.entries {
		if e.kind == entryNote && strings.Contains(e.text, want) {
			n++
		}
	}
	return n
}

// firstBeat runs Init's Cmd and returns the beatMsg it yields. With the virtual cursor retired
// there is no blink Cmd to batch the beat with, so Init's Cmd IS the beat and lands directly; the
// batch arm is kept because a Cmd batched onto it later must not silently stop the chain — its
// members are run concurrently and the beat taken from whichever finishes first.
func firstBeat(t *testing.T, cmd tea.Cmd) beatMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("Init returned no Cmd — the heartbeat chain never started")
	}
	switch msg := cmd().(type) {
	case beatMsg:
		return msg
	case tea.BatchMsg:
		out := make(chan tea.Msg, len(msg))
		for _, c := range msg {
			go func() { out <- c() }()
		}
		deadline := time.After(5 * time.Second)
		for range msg {
			select {
			case landed := <-out:
				if beat, ok := landed.(beatMsg); ok {
					return beat
				}
			case <-deadline:
				t.Fatal("no beatMsg five seconds after Init — the first beat never fired")
			}
		}
		t.Fatal("Init's batch carried no beatMsg — the first beat never fired")
	default:
		t.Fatalf("Init's Cmd yielded %T, want a batch carrying the first beat", msg)
	}
	return beatMsg{}
}

// ----------------------------------------------------------------------------
// The tick chain
// ----------------------------------------------------------------------------

// The first beat fires from Init, not one interval later: startup discovery IS that beat now, so
// the footer must stop saying "connecting…" as soon as the server answers rather than ten seconds
// after the TUI paints.
func TestInitFiresImmediateBeat(t *testing.T) {
	t.Parallel()

	fake := &fakeHeartbeat{script: []heartbeat.Beat{upBeat("served-model", 16384)}}
	m := wireHeartbeat(t, testOpts, fake)

	if m.hb.gen != 1 {
		t.Fatalf("hb.gen = %d, want 1 — a wired monitor arms the chain at construction", m.hb.gen)
	}
	landed := firstBeat(t, m.Init())
	if landed.gen != 1 {
		t.Errorf("first beat carries generation %d, want the armed 1", landed.gen)
	}
	if !landed.beat.Reachable || landed.beat.ActiveModel != "served-model" {
		t.Errorf("first beat = %+v, want the scripted reachable observation", landed.beat)
	}
	if got := fake.count(); got != 1 {
		t.Errorf("monitor beaten %d times from Init, want exactly 1", got)
	}
}

// The chain re-arms from the LANDED beat (never a fixed clock, so beats cannot overlap), and both
// heartbeat Msgs carry the spinner's generation guard: a Msg from a retired chain changes nothing
// and schedules nothing, so two chains can never beat at once.
func TestBeatChainReArmsAndStaleGenIsInert(t *testing.T) {
	t.Parallel()

	m := wireHeartbeat(t, testOpts, &fakeHeartbeat{})

	live, cmd := stepCmd(t, m, beatMsg{gen: m.hb.gen, beat: upBeat("test-model", 32768)})
	if cmd == nil {
		t.Fatal("a landed beat scheduled no tick — the heartbeat would beat once and stop")
	}
	if _, cmd := stepCmd(t, live, heartbeatTickMsg{gen: live.hb.gen}); cmd == nil {
		t.Error("a live tick issued no beat — the chain died at the first interval")
	}

	stale, cmd := stepCmd(t, live, beatMsg{gen: live.hb.gen + 1, beat: downBeat("boom")})
	if cmd != nil {
		t.Error("a beat from a retired chain re-armed the chain — two chains would beat at once")
	}
	if stale.hb.offline || stale.hb.failures != live.hb.failures || stale.hb.lastFailure != live.hb.lastFailure {
		t.Errorf("a retired chain's beat was folded: %+v, want %+v untouched", stale.hb, live.hb)
	}
	if _, cmd := stepCmd(t, live, heartbeatTickMsg{gen: live.hb.gen + 1}); cmd != nil {
		t.Error("a tick from a retired chain issued a beat")
	}
}

// An unwired monitor is completely inert: no chain is armed, Init returns nothing to run, a stray
// beat is not folded, and nothing is ever blocked. This is what keeps every hand-built test
// Options — and any embedder that wires no monitor — behaving exactly as before the heartbeat.
func TestHeartbeatUnwiredIsInert(t *testing.T) {
	t.Parallel()

	m := newTestModel(t) // testOpts wires no Heartbeat
	if m.hb.gen != 0 {
		t.Errorf("hb.gen = %d, want 0 — an unwired monitor arms no chain", m.hb.gen)
	}
	if m.beatCmd() != nil {
		t.Error("beatCmd built a Cmd with no monitor wired")
	}
	assertNoBatch(t, m.Init())

	after, cmd := stepCmd(t, m, beatMsg{gen: 0, beat: downBeat("boom")})
	if cmd != nil {
		t.Error("a stray beat started a chain on an unwired model")
	}
	if after.hb.offline || after.hb.failures != 0 {
		t.Errorf("a stray beat was folded into an unwired model: %+v", after.hb)
	}
	if after.blockedUpstream() {
		t.Error("an unwired model blocks the upstream; with nothing observing the server it has no standing to refuse")
	}
}

// assertNoBatch proves cmd starts no chain. Init does carry start-up Cmds now — the first beat and
// the prompt-recall load, batched — but tea.Batch drops nils, and this model wires neither seam, so
// the batch collapses back to nil and that nil is the whole proof here. The batched arm covers what
// a wired seam would hand us: a batch resolves to its BatchMsg at once, while a Cmd that parks (a
// timer, say) never answers, so a short wait tells the two apart without executing what it holds.
func assertNoBatch(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	out := make(chan tea.Msg, 1)
	go func() { out <- cmd() }()
	select {
	case msg := <-out:
		if _, ok := msg.(tea.BatchMsg); ok {
			t.Error("Init batched a Cmd onto its start-up Cmd; an unwired monitor must start no chain")
		}
	case <-time.After(250 * time.Millisecond):
	}
}

// ----------------------------------------------------------------------------
// The offline state and its debounce
// ----------------------------------------------------------------------------

// Before any beat has landed there is nothing to weigh a failure against, so a cold start against
// a server that is not running says so on the first failed beat — no debounce, one note.
func TestColdStartFailureIsOfflineImmediately(t *testing.T) {
	t.Parallel()

	const failure = "apogee: model discovery: dial tcp 127.0.0.1:1: connect: connection refused"
	m := wireHeartbeat(t, testOpts, &fakeHeartbeat{})
	m = foldBeatMsg(t, m, downBeat(failure))

	if !m.hb.offline {
		t.Fatal("the first-ever failed beat left the model online; a cold start must say so at once")
	}
	if n := countNotes(m, "server offline"); n != 1 {
		t.Errorf("offline notes = %d, want exactly 1", n)
	}
	if n := countNotes(m, failure); n != 1 {
		t.Errorf("the offline note does not quote the failure; entries = %+v", m.transcript.entries)
	}
}

// Once a beat has landed, one failure is not evidence: discovery's timeout can elapse on a merely
// saturated server, and a footer that flickers offline mid-session would be worse than useless.
// The state crosses on the second consecutive idle failure, and notes it exactly once.
func TestOfflineDebouncesAtIdle(t *testing.T) {
	t.Parallel()

	m := wireHeartbeat(t, testOpts, &fakeHeartbeat{})
	m = foldBeatMsg(t, m, upBeat("test-model", 32768))

	m = foldBeatMsg(t, m, downBeat("timeout"))
	if m.hb.offline {
		t.Fatal("one failed beat after a success flipped the footer offline; the debounce is the whole point")
	}
	if n := countNotes(m, "server offline"); n != 0 {
		t.Errorf("offline notes after one failure = %d, want 0", n)
	}
	if m.hb.failures != 1 {
		t.Errorf("failure counter = %d, want 1", m.hb.failures)
	}

	m = foldBeatMsg(t, m, downBeat("timeout"))
	if !m.hb.offline {
		t.Fatal("two consecutive idle failures did not flip the footer offline")
	}
	if n := countNotes(m, "server offline"); n != 1 {
		t.Errorf("offline notes after two failures = %d, want exactly 1", n)
	}

	m = foldBeatMsg(t, m, downBeat("timeout"))
	if n := countNotes(m, "server offline"); n != 1 {
		t.Errorf("a further failed beat noted the crossing again (%d notes); the transcript would fill at one line per ten seconds", n)
	}
}

// A failed beat while an Exchange is in flight is IGNORED — state, counter and words untouched. A
// streaming reply is stronger evidence that the server is there than a timed-out /v1/models on a
// single-slot server busy serving that very stream.
func TestBusyFailureNeverFlipsOffline(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		everOnline bool
	}{
		{"after a landed beat", true},
		{"before any beat has landed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := wireHeartbeat(t, testOpts, &fakeHeartbeat{})
			if tc.everOnline {
				m = foldBeatMsg(t, m, upBeat("test-model", 32768))
			}
			m.state = stateRunning

			busy, cmd := stepCmd(t, m, beatMsg{gen: m.hb.gen, beat: downBeat("timeout")})
			if cmd == nil {
				t.Error("a failed beat while busy killed the chain; the monitor must keep beating")
			}
			if busy.hb.offline {
				t.Error("a failed beat while busy flipped the footer offline mid-exchange")
			}
			if busy.hb.failures != 0 {
				t.Errorf("failure counter = %d, want 0 — a busy-time failure is not counted", busy.hb.failures)
			}
			if busy.hb.lastFailure != "" {
				t.Errorf("lastFailure = %q, want it untouched", busy.hb.lastFailure)
			}
			if busy.state != stateRunning {
				t.Errorf("state = %v, want the Exchange left running", busy.state)
			}
			if n := countNotes(busy, "server offline"); n != 0 {
				t.Errorf("a busy-time failure wrote %d offline notes, want 0", n)
			}
		})
	}
}

// Recovery crosses back exactly once: the first successful beat after an offline stretch notes it,
// every further success is silent, and the debounce counter is reset for the next stretch.
func TestRecoveryNotesOnce(t *testing.T) {
	t.Parallel()

	m := wireHeartbeat(t, testOpts, &fakeHeartbeat{})
	m = foldBeatMsg(t, m, downBeat("connection refused"))
	if !m.hb.offline {
		t.Fatal("the cold-start failure did not flip the footer offline")
	}

	m = foldBeatMsg(t, m, upBeat("test-model", 32768))
	if m.hb.offline {
		t.Fatal("a successful beat left the model offline")
	}
	if n := countNotes(m, onlineNote); n != 1 {
		t.Errorf("recovery notes = %d, want exactly 1", n)
	}
	if m.hb.failures != 0 || m.hb.lastFailure != "" {
		t.Errorf("a landed beat left stale failure evidence: %+v", m.hb)
	}
	if len(m.hb.models) != 1 {
		t.Errorf("the advertised model list was not stashed: %+v", m.hb.models)
	}

	m = foldBeatMsg(t, m, upBeat("test-model", 32768))
	if n := countNotes(m, onlineNote); n != 1 {
		t.Errorf("a further healthy beat noted the recovery again (%d notes)", n)
	}
}

// ----------------------------------------------------------------------------
// The submit block
// ----------------------------------------------------------------------------

// A send while the server is offline is refused at the boundary with a clear note — and the typed
// message STAYS IN THE BOX: the human wrote it, the server's absence is not their mistake. The two
// commands that would open an Exchange are refused the same way, while the local verbs stay live.
func TestSubmitBlockedOfflineKeepsInput(t *testing.T) {
	t.Parallel()

	opts := testOpts
	serverSeams(&opts).beat = (&fakeHeartbeat{}).beat
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, opts)
	m = foldBeatMsg(t, m, downBeat("connection refused"))

	m.input.SetValue("what is the capital of France?")
	m, cmd := stepCmd(t, m, keyEnter())

	if cmd != nil {
		t.Error("an offline submit launched a worker")
	}
	if m.state != stateIdle {
		t.Errorf("state = %v, want idle after a refused send", m.state)
	}
	if got, want := m.input.Value(), "what is the capital of France?"; got != want {
		t.Errorf("input = %q, want the typed message preserved (%q)", got, want)
	}
	if len(eng.submitted) != 0 {
		t.Errorf("the engine was handed %d inputs while offline, want none", len(eng.submitted))
	}
	if n := countNotes(m, "cannot send — server offline"); n != 1 {
		t.Fatalf("refusal notes = %d, want exactly 1; entries = %+v", n, m.transcript.entries)
	}
	if n := countNotes(m, opts.Endpoint); n != 1 {
		t.Error("the refusal note does not name the endpoint")
	}
	if n := countNotes(m, "connection refused"); n < 1 {
		t.Error("the refusal note does not quote why the server could not be read")
	}

	// The two Exchange-opening commands answer to the same gate…
	m.input.SetValue("/continue")
	m, cmd = stepCmd(t, m, keyEnter())
	if cmd != nil || m.state != stateIdle {
		t.Errorf("/continue ran while offline (state %v)", m.state)
	}
	m.input.SetValue("/compact")
	m, cmd = stepCmd(t, m, keyEnter())
	if cmd != nil || m.state != stateIdle {
		t.Errorf("/compact ran while offline (state %v)", m.state)
	}
	if eng.compactCalls != 0 || len(eng.submitted) != 0 {
		t.Errorf("an Exchange-opening command reached the engine while offline (compact=%d submit=%d)",
			eng.compactCalls, len(eng.submitted))
	}

	// …while the purely local verbs stay live: /clear still starts a fresh session.
	m.input.SetValue("/clear")
	m = step(t, m, keyEnter())
	if n := len(m.transcript.entries); n != 1 || m.transcript.entries[0].kind != entryStartup {
		t.Errorf("/clear did not reset the view while offline: %d entries", n)
	}
	if eng.clearCalls != 1 {
		t.Errorf("ClearContext calls = %d, want 1 — /clear must stay live while the server is away", eng.clearCalls)
	}
}

// Before the first beat binds a model there is nothing to send TO either, and the honest words are
// different: the server has not answered yet rather than gone away. The typed message is preserved
// exactly as in the offline case.
func TestSubmitBlockedBeforeFirstBind(t *testing.T) {
	t.Parallel()

	opts := testOpts
	opts.Model = "" // the async cold start: no model is bound until the first beat lands
	m := wireHeartbeat(t, opts, &fakeHeartbeat{})

	m.input.SetValue("hello")
	m, cmd := stepCmd(t, m, keyEnter())

	if cmd != nil {
		t.Error("a pre-bind submit launched a worker")
	}
	if got := m.input.Value(); got != "hello" {
		t.Errorf("input = %q, want the typed message preserved", got)
	}
	if n := countNotes(m, "still connecting to "+opts.Endpoint); n != 1 {
		t.Fatalf("connecting notes = %d, want exactly 1; entries = %+v", n, m.transcript.entries)
	}
	if n := countNotes(m, "server offline"); n != 0 {
		t.Error("the pre-bind refusal claimed the server is offline; nothing has been observed yet")
	}
}

// ----------------------------------------------------------------------------
// Display
// ----------------------------------------------------------------------------

// The footer says both upstream states in words: "offline" beside the model, and "connecting…" in
// place of the model while no model is bound yet. The stand-in word replaces the MODEL SEGMENT
// ALONE — the host is where the session is still trying to reach, and the workspace is a local
// fact no upstream state can touch, so both stay put behind the word.
func TestFooterShowsOfflineAndConnecting(t *testing.T) {
	t.Parallel()

	base := testOpts
	base.Workspace = "/ws/proj"

	offline := wireHeartbeat(t, base, &fakeHeartbeat{})
	offline = foldBeatMsg(t, offline, downBeat("connection refused"))
	if got := plain(offline.View()); !strings.Contains(got, offlineLabel) {
		t.Errorf("the view does not say %q while offline:\n%s", offlineLabel, got)
	}
	if got := ansiPattern.ReplaceAllString(offline.footerContent(80), ""); !strings.Contains(got, "test-model") {
		t.Errorf("footer = %q, want the bound model still named beside the offline marker", got)
	}

	opts := base
	opts.Model = ""
	connecting := wireHeartbeat(t, opts, &fakeHeartbeat{})
	footer := ansiPattern.ReplaceAllString(connecting.footerContent(80), "")
	if !strings.Contains(footer, connectingLabel) {
		t.Errorf("footer = %q, want %q while no model is bound", footer, connectingLabel)
	}
	for _, want := range []string{"test-host", "/ws/proj"} {
		if !strings.Contains(footer, want) {
			t.Errorf("footer = %q, want %q kept — %q replaces the model segment alone", footer, want, connectingLabel)
		}
	}
	if strings.Contains(footer, format.Tokens(opts.ContextWindow)) {
		t.Errorf("footer = %q, want the window nowhere in it — the gauge states it now", footer)
	}
	if got := plain(connecting.View()); !strings.Contains(got, connectingLabel) {
		t.Errorf("the view does not say %q before the first bind:\n%s", connectingLabel, got)
	}
}

// The gauge's percentage is clamped exactly like its bar: a conversation carried across a switch
// to a smaller window overfills the new limit, and a full bar labelled "137%" is a rendering bug.
func TestGaugePercentClamped(t *testing.T) {
	t.Parallel()

	got := ansiPattern.ReplaceAllString(contextUsage{Used: 45000, Limit: 32768}.view(newTheme(scheme.Default())), "")
	if !strings.Contains(got, "100%") {
		t.Errorf("gauge = %q, want the percentage clamped to 100%%", got)
	}
	if strings.Contains(got, "137%") {
		t.Errorf("gauge = %q, want no over-100%% reading beside an already-clamped bar", got)
	}
	// The unclamped Used stands beside the window it overfills, so the overrun reads as the plain
	// contradiction it is — "44k/32k 100%" — rather than as an impossible percentage.
	if want := format.Tokens(45000) + "/" + format.Tokens(32768) + " 100%"; !strings.Contains(got, want) {
		t.Errorf("gauge = %q, want the unclamped count beside its window: %q", got, want)
	}
}

// No model bound renders as nothing at all — filepath.Base("") is ".", and with the model bound
// late by the first beat there is now a real window at every cold start in which a lone "." would
// be shown as the model.
func TestDisplayModelEmpty(t *testing.T) {
	t.Parallel()

	if got := displayModel(""); got != "" {
		t.Errorf("displayModel(%q) = %q, want the empty string", "", got)
	}
}

// ----------------------------------------------------------------------------
// Rebind orchestration
// ----------------------------------------------------------------------------

// The cold start binds LATE: the binary launches with no model at all and the first landed beat is
// startup discovery. It runs the ordinary rebind path from empty — the seam is asked for the
// observed model, the display adopts what came back, and both places that name a model (the footer
// and the start-up box seeded before any of this was known) say it.
//
// And it says nothing else: this is FIRST CONTACT — a server that was already up when apogee
// started — so the restated box IS the statement and no "connected:" line is printed under it.
func TestLateSeedBindsThroughRebind(t *testing.T) {
	t.Parallel()

	rb := &fakeRebind{}
	m := wireRebind(t, unbound(testOpts), &fakeHeartbeat{}, rb)

	m = foldBeatMsg(t, m, upBeat("served-model", 16384))

	if want := []rebindCall{{model: "served-model", window: 16384}}; !reflect.DeepEqual(rb.calls, want) {
		t.Fatalf("rebind calls = %+v, want %+v — the observation is what the binary is asked to bind", rb.calls, want)
	}
	if m.opts.Model != "served-model" || m.opts.ContextWindow != 16384 {
		t.Errorf("bound display = %q/%d, want the rebind's own result adopted", m.opts.Model, m.opts.ContextWindow)
	}
	if n := countNotes(m, "connected:"); n != 0 {
		t.Errorf("connected notes = %d, want none — first contact is quiet; notes = %q", n, noteTexts(m))
	}
	footer := ansiPattern.ReplaceAllString(m.footerContent(80), "")
	if !strings.Contains(footer, "served-model") || strings.Contains(footer, connectingLabel) {
		t.Errorf("footer = %q, want the bound model in place of %q", footer, connectingLabel)
	}
	box := m.transcript.entries[0]
	if box.kind != entryStartup || box.startup.Model != "served-model" || box.startup.Context != "16k" {
		t.Errorf("start-up box = %+v, want it restated in place with the bound model and window", box.startup)
	}
	if m.blockedUpstream() {
		t.Error("the upstream is still blocked after a model was bound")
	}
}

// A connection that had to be WAITED for is news, and says so exactly once: the cold start against
// a stopped server is offline immediately, so the seed that follows is no longer first contact. The
// same line doubles as the recovery statement — "connected: <model>" already implies the server
// answered, so the weaker "server back online" stays suppressed. The window-less variant pins the
// seed's other wording: a binding whose window nobody could name carries no context clause.
func TestDelayedConnectNotesOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		window int
		want   []string
	}{
		{
			name:   "window reported",
			window: 16384,
			want:   []string{offlineNote("connection refused"), "connected: served-model, context 16k"},
		},
		{
			name:   "window unknown",
			window: 0,
			want:   []string{offlineNote("connection refused"), "connected: served-model", unknownWindowNote},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rb := &fakeRebind{}
			m := wireRebind(t, unbound(testOpts), &fakeHeartbeat{}, rb)

			m = foldBeatMsg(t, m, downBeat("connection refused"))
			m = foldBeatMsg(t, m, upBeat("served-model", tc.window))

			if got := noteTexts(m); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("transcript notes =\n%q\nwant\n%q", got, tc.want)
			}
			if n := countNotes(m, onlineNote); n != 0 {
				t.Errorf("recovery notes = %d, want none — the connected line is the single statement", n)
			}
		})
	}
}

// A server that was up at launch but had nothing loaded is the other case first contact must not
// swallow: the first beat lands, observes no model, and binds nothing — and when a model finally
// appears, that IS news. everOnline alone defeats the quiet seed, with no failure needed.
func TestModellessStartSeedsWithNote(t *testing.T) {
	t.Parallel()

	rb := &fakeRebind{}
	m := wireRebind(t, unbound(testOpts), &fakeHeartbeat{}, rb)

	m = foldBeatMsg(t, m, upBeat("", 0)) // the server answers; nothing is loaded

	if len(rb.calls) != 0 {
		t.Fatalf("rebind calls = %+v, want none — a server serving nothing is not a change", rb.calls)
	}
	if !m.blockedUpstream() {
		t.Error("the submit gate opened with no model bound")
	}

	m = foldBeatMsg(t, m, upBeat("served-model", 16384))

	if n := countNotes(m, "connected: served-model, context 16k"); n != 1 {
		t.Errorf("connected notes = %d, want exactly 1 — a model appearing is news; notes = %q", n, noteTexts(m))
	}
}

// The reported bug: a model switched behind apogee's back left the display — and the bindings —
// frozen at launch. An observed change now rebinds and says so in one line carrying both models and
// both windows.
func TestModelChangeRebindsWithNotice(t *testing.T) {
	t.Parallel()

	rb := &fakeRebind{}
	m := wireRebind(t, testOpts, &fakeHeartbeat{}, rb)
	m = foldBeatMsg(t, m, upBeat("test-model", 32768)) // the first beat observes what is already bound

	m = foldBeatMsg(t, m, upBeat("new-model", 16384))

	if len(rb.calls) != 2 || rb.calls[1] != (rebindCall{model: "new-model", window: 16384}) {
		t.Fatalf("rebind calls = %+v, want the second one carrying the newly served model", rb.calls)
	}
	if m.opts.Model != "new-model" || m.opts.ContextWindow != 16384 {
		t.Errorf("bound display = %q/%d, want the newly served model", m.opts.Model, m.opts.ContextWindow)
	}
	if n := countNotes(m, "model changed: test-model → new-model, context 32k → 16k"); n != 1 {
		t.Errorf("change notes = %d, want exactly 1; notes = %q", n, noteTexts(m))
	}
	if n := countNotes(m, "connected:"); n != 0 {
		t.Error("a model change was narrated as a first connection")
	}
	if footer := ansiPattern.ReplaceAllString(m.footerContent(80), ""); !strings.Contains(footer, "new-model") {
		t.Errorf("footer = %q, want the newly bound model", footer)
	}
}

// The same model reloaded with a different -c is a real change too: the gauge's denominator and the
// engine's Budget both move, so it rebinds — and the note says only what moved.
func TestWindowOnlyChangeRebinds(t *testing.T) {
	t.Parallel()

	rb := &fakeRebind{}
	m := wireRebind(t, testOpts, &fakeHeartbeat{}, rb)
	m = foldBeatMsg(t, m, upBeat("test-model", 32768))

	m = foldBeatMsg(t, m, upBeat("test-model", 16384))

	if len(rb.calls) != 2 || rb.calls[1] != (rebindCall{model: "test-model", window: 16384}) {
		t.Fatalf("rebind calls = %+v, want a second call for the resized window", rb.calls)
	}
	if m.opts.ContextWindow != 16384 {
		t.Errorf("bound window = %d, want the resized 16384", m.opts.ContextWindow)
	}
	if n := countNotes(m, "context window changed: 32k → 16k"); n != 1 {
		t.Errorf("window notes = %d, want exactly 1; notes = %q", n, noteTexts(m))
	}
	if n := countNotes(m, "model changed"); n != 0 {
		t.Errorf("a window-only change claimed the model moved; notes = %q", noteTexts(m))
	}
}

// A beat that cannot name a window reports 0, which is the ABSENCE of an observation rather than an
// observation of absence: it must never be mistaken for the window having changed, or a server with
// no /props would rebind every ten seconds forever.
func TestZeroWindowBeatIsNotAChange(t *testing.T) {
	t.Parallel()

	rb := &fakeRebind{}
	m := wireRebind(t, testOpts, &fakeHeartbeat{}, rb)
	m = foldBeatMsg(t, m, upBeat("test-model", 32768))

	m = foldBeatMsg(t, m, heartbeat.Beat{Reachable: true, ActiveModel: "test-model"})

	if len(rb.calls) != 1 {
		t.Errorf("rebind calls = %+v, want the window-less beat to have asked for nothing", rb.calls)
	}
	if m.opts.ContextWindow != 32768 {
		t.Errorf("bound window = %d, want the known 32768 kept", m.opts.ContextWindow)
	}
	if notes := noteTexts(m); len(notes) != 0 {
		t.Errorf("notes = %q, want silence", notes)
	}
}

// A switch observed mid-answer waits: Agent.Rebind is idle-only, so the intent is stashed and
// applied in the exchange-terminal fold — the same boundary the idle save uses. Two switches during
// one Exchange collapse to the latest; binding an intermediate model nobody is serving any more
// would be worse than binding nothing.
func TestRebindDeferredWhileBusy(t *testing.T) {
	t.Parallel()

	rb := &fakeRebind{}
	m := wireRebind(t, testOpts, &fakeHeartbeat{}, rb)
	m = foldBeatMsg(t, m, upBeat("test-model", 32768)) // the baseline binding, at idle
	m.state = stateRunning

	m = foldBeatMsg(t, m, upBeat("model-a", 32768))
	m = foldBeatMsg(t, m, upBeat("model-b", 16384))

	if len(rb.calls) != 1 {
		t.Fatalf("rebind calls = %+v, want nothing bound while a worker owns the engine", rb.calls)
	}
	if m.opts.Model != "test-model" {
		t.Errorf("bound model = %q, want the mid-Exchange binding untouched", m.opts.Model)
	}
	if want := (rebindIntent{model: "model-b", window: 16384}); m.hb.pendingRebind == nil || *m.hb.pendingRebind != want {
		t.Fatalf("pendingRebind = %+v, want the LATEST observation stashed (%+v)", m.hb.pendingRebind, want)
	}

	m = step(t, m, exchangeDoneMsg{})

	if len(rb.calls) != 2 || rb.calls[1] != (rebindCall{model: "model-b", window: 16384}) {
		t.Fatalf("rebind calls = %+v, want exactly the latest applied at the boundary", rb.calls)
	}
	if m.opts.Model != "model-b" || m.opts.ContextWindow != 16384 {
		t.Errorf("bound display = %q/%d, want the deferred change applied", m.opts.Model, m.opts.ContextWindow)
	}
	if m.hb.pendingRebind != nil {
		t.Error("the applied intent is still stashed; it would be re-applied at the next boundary")
	}
	if n := countNotes(m, "model changed: test-model → model-b, context 32k → 16k"); n != 1 {
		t.Errorf("change notes = %d, want exactly 1; notes = %q", n, noteTexts(m))
	}
}

// A `context-window:` pin outranks whatever the server reports, and the binary — not the renderer —
// is what knows that. The TUI adopts the BOUND window, and because it measures change against the
// OBSERVATION, the server's own differing window never re-triggers a rebind on every beat. The seed
// is delayed by a failed first beat so that the wording has a note to be read from at all.
func TestPinnedWindowResultSticks(t *testing.T) {
	t.Parallel()

	rb := &fakeRebind{answer: func(model string, _ int) (RebindResult, error) {
		return RebindResult{Model: model, ContextWindow: 8192}, nil // the pin wins in the composition root
	}}
	opts := unbound(testOpts)
	opts.ContextWindow = 8192 // the pin the binary already applied at launch
	m := wireRebind(t, opts, &fakeHeartbeat{}, rb)

	m = foldBeatMsg(t, m, downBeat("connection refused")) // the seed is delayed, so it is not quiet
	m = foldBeatMsg(t, m, upBeat("served-model", 32768))
	m = foldBeatMsg(t, m, upBeat("served-model", 32768))
	m = foldBeatMsg(t, m, upBeat("served-model", 32768))

	if len(rb.calls) != 1 {
		t.Fatalf("rebind calls = %+v, want exactly one — identical beats are not changes", rb.calls)
	}
	if m.opts.ContextWindow != 8192 {
		t.Errorf("bound window = %d, want the pinned 8192 the rebind reported", m.opts.ContextWindow)
	}
	if n := countNotes(m, "connected: served-model, context 8k"); n != 1 {
		t.Errorf("connected notes = %d, want exactly 1 naming the PINNED window; notes = %q", n, noteTexts(m))
	}
	if n := countNotes(m, "context window changed"); n != 0 {
		t.Errorf("the pinned window narrated a change it suppressed; notes = %q", noteTexts(m))
	}
}

// A rebind that cannot be resolved leaves every binding where it was and says so — once per target.
// The monitor beats every ten seconds; repeating one refusal at that rate would bury the
// conversation it annotates.
func TestRebindFailureNotedOnce(t *testing.T) {
	t.Parallel()

	rb := &fakeRebind{answer: func(model string, _ int) (RebindResult, error) {
		return RebindResult{}, errors.New("no validated set for " + model)
	}}
	m := wireRebind(t, testOpts, &fakeHeartbeat{}, rb)

	m = foldBeatMsg(t, m, upBeat("model-a", 32768))
	if m.opts.Model != "test-model" || m.opts.ContextWindow != 32768 {
		t.Fatalf("bound display = %q/%d, want it untouched by a failed rebind", m.opts.Model, m.opts.ContextWindow)
	}
	if n := countNotes(m, "model change detected (test-model → model-a) but rebind failed"); n != 1 {
		t.Fatalf("failure notes = %d, want exactly 1; notes = %q", n, noteTexts(m))
	}
	if n := countNotes(m, "still bound to test-model"); n != 1 {
		t.Errorf("the refusal does not say what apogee is still talking to; notes = %q", noteTexts(m))
	}

	// An identical beat is not a change at all: nothing is asked, nothing is said.
	m = foldBeatMsg(t, m, upBeat("model-a", 32768))
	if len(rb.calls) != 1 || len(noteTexts(m)) != 1 {
		t.Errorf("an identical beat re-ran the failed rebind (calls %+v, notes %q)", rb.calls, noteTexts(m))
	}

	// The same target seen in a new window IS asked again — and stays quiet, having already spoken.
	m = foldBeatMsg(t, m, upBeat("model-a", 16384))
	if len(rb.calls) != 2 {
		t.Errorf("rebind calls = %+v, want the retry a genuinely new observation triggers", rb.calls)
	}
	if n := len(noteTexts(m)); n != 1 {
		t.Errorf("notes = %q, want the refusal still stated exactly once for this target", noteTexts(m))
	}

	// A different target is a different fact, and is worth saying.
	m = foldBeatMsg(t, m, upBeat("model-b", 16384))
	if n := countNotes(m, "→ model-b) but rebind failed"); n != 1 {
		t.Errorf("a new target's refusal was swallowed; notes = %q", noteTexts(m))
	}
}

// With no rebind seam the heartbeat is display-frozen by design (an embedder that wires only the
// monitor): beats still light the offline state, but no binding moves and nothing claims one did.
func TestRebindNilIsDisplayFrozen(t *testing.T) {
	t.Parallel()

	m := wireHeartbeat(t, testOpts, &fakeHeartbeat{}) // no Rebind wired
	before := m.opts

	m = foldBeatMsg(t, m, upBeat("other-model", 16384))

	if m.opts.Model != before.Model || m.opts.ContextWindow != before.ContextWindow {
		t.Errorf("bound display = %q/%d, want the launch values (%q/%d) with no seam to move them",
			m.opts.Model, m.opts.ContextWindow, before.Model, before.ContextWindow)
	}
	if m.hb.pendingRebind != nil {
		t.Error("a change was stashed with nothing to apply it through")
	}
	if notes := noteTexts(m); len(notes) != 0 {
		t.Errorf("notes = %q, want silence — no binding moved", notes)
	}
	if footer := ansiPattern.ReplaceAllString(m.footerContent(80), ""); !strings.Contains(footer, "test-model") {
		t.Errorf("footer = %q, want the launch-time model still shown", footer)
	}
}

// The whole narration, read end to end: a cold start against a stopped server, the server coming up
// with model A, an identical beat, then a switch to B. Four beats, three lines — the transcript is
// the feature's user-visible contract, and its enemy is duplication at one line every ten seconds.
func TestBeatScriptNarratesEachChangeOnce(t *testing.T) {
	t.Parallel()

	rb := &fakeRebind{}
	m := wireRebind(t, unbound(testOpts), &fakeHeartbeat{}, rb)

	m = foldBeatMsg(t, m, downBeat("connection refused"))
	m = foldBeatMsg(t, m, upBeat("model-a", 32768))
	m = foldBeatMsg(t, m, upBeat("model-a", 32768))
	m = foldBeatMsg(t, m, upBeat("model-b", 16384))

	want := []string{
		"server offline — connection refused",
		"connected: model-a, context 32k",
		"model changed: model-a → model-b, context 32k → 16k",
	}
	if got := noteTexts(m); !reflect.DeepEqual(got, want) {
		t.Errorf("transcript notes =\n%q\nwant\n%q", got, want)
	}
}

// A bound model whose window nobody can name is honest about the consequence: the Budget and
// automatic compaction both bind against the window, so with none known they simply do nothing.
// The line rides the rebind because that is when the fact is established — at launch it would fire
// on every cold start and be wrong a second later. It survives the quiet first-contact seed: the
// suppression is aimed at connection narration the restated box already carries, and this is
// actionable news the box says nothing about.
func TestUnknownWindowNotedOnBind(t *testing.T) {
	t.Parallel()

	rb := &fakeRebind{}
	m := wireRebind(t, unbound(testOpts), &fakeHeartbeat{}, rb)

	m = foldBeatMsg(t, m, heartbeat.Beat{Reachable: true, ActiveModel: "served-model"})

	if n := countNotes(m, unknownWindowNote); n != 1 {
		t.Errorf("unknown-window notes = %d, want exactly 1; notes = %q", n, noteTexts(m))
	}
	if n := countNotes(m, "connected:"); n != 0 {
		t.Errorf("connected notes = %d, want none — first contact is quiet; notes = %q", n, noteTexts(m))
	}
}

// The notices the composition root produced (a validated set that matched the new model, say) ride
// the same rebind into the transcript, after the line that says what moved — and they survive a
// quiet first-contact seed, which suppresses only the connection narration itself.
func TestRebindNoticesSurfaceAsNotes(t *testing.T) {
	t.Parallel()

	rb := &fakeRebind{answer: func(model string, window int) (RebindResult, error) {
		return RebindResult{Model: model, ContextWindow: window, Notices: []string{"validated set: strict-json"}}, nil
	}}
	m := wireRebind(t, unbound(testOpts), &fakeHeartbeat{}, rb)

	m = foldBeatMsg(t, m, upBeat("served-model", 32768))

	want := []string{"validated set: strict-json"}
	if got := noteTexts(m); !reflect.DeepEqual(got, want) {
		t.Errorf("transcript notes =\n%q\nwant\n%q", got, want)
	}
}
