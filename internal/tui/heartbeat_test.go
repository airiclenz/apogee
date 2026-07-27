package tui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/heartbeat"
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
	opts.Heartbeat = hb.beat
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

// firstBeat runs Init's Cmd and returns the beatMsg it yields. Init batches the first beat with
// the input's focus Cmd, and that one parks for the cursor's blink interval when executed, so the
// batch's Cmds run concurrently here and the beat — which lands at once — is taken from whichever
// finishes first.
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

// An unwired monitor is completely inert: no chain is armed, Init returns the focus Cmd alone, a
// stray beat is not folded, and nothing is ever blocked. This is what keeps every hand-built test
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

// assertNoBatch proves cmd is not a tea.Batch. The lone focus Cmd is the cursor blink, which parks
// for its blink interval rather than returning a Msg, while a batch resolves to its BatchMsg at
// once — so a short wait tells the two apart without executing the blink.
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
			t.Error("Init batched a Cmd onto the focus Cmd; an unwired monitor must start no chain")
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
	opts.Heartbeat = (&fakeHeartbeat{}).beat
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
// place of the model and its window while no model is bound yet.
func TestFooterShowsOfflineAndConnecting(t *testing.T) {
	t.Parallel()

	offline := wireHeartbeat(t, testOpts, &fakeHeartbeat{})
	offline = foldBeatMsg(t, offline, downBeat("connection refused"))
	if got := plain(offline.View()); !strings.Contains(got, offlineLabel) {
		t.Errorf("the view does not say %q while offline:\n%s", offlineLabel, got)
	}
	if got := ansiPattern.ReplaceAllString(offline.footerContent(80), ""); !strings.Contains(got, "test-model") {
		t.Errorf("footer = %q, want the bound model still named beside the offline marker", got)
	}

	opts := testOpts
	opts.Model = ""
	connecting := wireHeartbeat(t, opts, &fakeHeartbeat{})
	footer := ansiPattern.ReplaceAllString(connecting.footerContent(80), "")
	if !strings.Contains(footer, connectingLabel) {
		t.Errorf("footer = %q, want %q while no model is bound", footer, connectingLabel)
	}
	if strings.Contains(footer, formatTokens(opts.ContextWindow)) {
		t.Errorf("footer = %q, want the window dropped with the model — it is not a fact about a model nobody has named", footer)
	}
	if got := plain(connecting.View()); !strings.Contains(got, connectingLabel) {
		t.Errorf("the view does not say %q before the first bind:\n%s", connectingLabel, got)
	}
}

// The gauge's percentage is clamped exactly like its bar: a conversation carried across a switch
// to a smaller window overfills the new limit, and a full bar labelled "137%" is a rendering bug.
func TestGaugePercentClamped(t *testing.T) {
	t.Parallel()

	got := ansiPattern.ReplaceAllString(contextUsage{Used: 45000, Limit: 32768}.view(newTheme()), "")
	if !strings.Contains(got, "100%") {
		t.Errorf("gauge = %q, want the percentage clamped to 100%%", got)
	}
	if strings.Contains(got, "137%") {
		t.Errorf("gauge = %q, want no over-100%% reading beside an already-clamped bar", got)
	}
	if !strings.Contains(got, formatTokens(45000)) {
		t.Errorf("gauge = %q, want the unclamped token count still shown", got)
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
