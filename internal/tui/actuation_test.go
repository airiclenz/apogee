package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/format"
	"github.com/airiclenz/apogee/internal/testtime"
)

// ----------------------------------------------------------------------------
// Launcher harness (ADR 0029 D5/D6)
// ----------------------------------------------------------------------------
//
// The four launcher seams are faked exactly as the heartbeat and switch seams are: closures over a
// scripted value, driven through Update with synthetic Msgs. Nothing here starts a process, opens a
// socket, or reads a file — the launcher itself is proven in its own repo, and what these tests are
// about is the LATCH, the pump's ordering, and the two shapes a completion folds into.

// twoProfiles is the profile fixture: a profile on the very server this session is talking to
// (testOpts.Endpoint), beside one that lives on another port and is running right now.
var twoProfiles = []LaunchProfileChoice{
	{Name: "alpha", Backend: "llamacpp", Addr: "localhost:1234", ContextWindow: 32768},
	{Name: "beta", Backend: "ollama", Addr: "localhost:8081", ContextWindow: 8192, Running: true},
}

// fakeLauncher stands in for the composition root's four launcher closures — the CI-side fake ADR
// 0029's consequences name. It scripts what the launcher would have done (which profiles the config
// defines, what a load narrates before it answers, what each verb reports) and records what it was
// asked to do. The lifecycle verbs are called from a Cmd goroutine, so the records are guarded.
type fakeLauncher struct {
	profiles  []LaunchProfileChoice
	listErr   error
	steps     []string          // the narration a load emits, in order, before it answers
	result    ProfileLoadResult // what a load reports back
	loadErr   error
	actResult ActuationResult // what /unload-model and /stop-server report back
	actErr    error
	panics    bool // the seam falls over mid-verb; the latch must still release

	mu    sync.Mutex
	lists int
	loads []string
	acts  []string
	moves int
}

// follows scripts a load that has to MOVE the session: the seam resolves the move and hands it back
// as the call [ProfileLoadResult.Move] declares, answering with switched — or refusing with err —
// when it is finally committed. The commit is COUNTED, which is what lets a test say where it ran:
// the seam resolving it runs on a Cmd goroutine, the fold committing it on the Update goroutine.
func (f *fakeLauncher) follows(switched ServerSwitchResult, err error) {
	f.result.Move = func() (ServerSwitchResult, error) {
		f.mu.Lock()
		f.moves++
		f.mu.Unlock()
		return switched, err
	}
}

// committed reports how many times the resolved move has been committed.
func (f *fakeLauncher) committed() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.moves
}

// newLauncher is the fake most tests open on: two profiles defined and every verb succeeding.
func newLauncher() *fakeLauncher {
	return &fakeLauncher{profiles: twoProfiles}
}

func (f *fakeLauncher) list() ([]LaunchProfileChoice, error) {
	f.mu.Lock()
	f.lists++
	f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.profiles, nil
}

func (f *fakeLauncher) load(name string, progress func(step string)) (ProfileLoadResult, error) {
	f.mu.Lock()
	f.loads = append(f.loads, name)
	f.mu.Unlock()
	if f.panics {
		panic("the launcher fell over")
	}
	for _, step := range f.steps {
		progress(step)
	}
	return f.result, f.loadErr
}

func (f *fakeLauncher) act(endpoint string) (ActuationResult, error) {
	f.mu.Lock()
	f.acts = append(f.acts, endpoint)
	f.mu.Unlock()
	return f.actResult, f.actErr
}

// loaded reports the profiles the fake was asked to activate, in order.
func (f *fakeLauncher) loaded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.loads...)
}

// actuated reports the endpoints /unload-model or /stop-server were called with, in order.
func (f *fakeLauncher) actuated() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.acts...)
}

// listCount reports how many times the picker asked for rows — the freshness assertion.
func (f *fakeLauncher) listCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lists
}

// fakeLauncherHost is the per-family stand-in for [LauncherHost] (ADR 0054 decision 6): one func per
// act, and the DOCUMENTED unwired answer for every act a test leaves nil. That is what keeps the
// per-member degrades provable one at a time — "the launcher is here but this host answers no boot
// restore", "it lists profiles and records nothing" — without a fake per combination of wired halves.
//
// It is a pointer receiver for [fakeServerHost]'s reasons: the spies behind its funcs are, and
// [launcherSeams] adds one member at a time to a host a helper wired before it.
type fakeLauncherHost struct {
	enabled  func() bool
	profiles func() ([]LaunchProfileChoice, error)
	load     func(name string, progress func(step string)) (ProfileLoadResult, error)
	unload   func(endpoint string) (ActuationResult, error)
	stop     func(endpoint string) (ActuationResult, error)
	record   func(profile string) (bool, error)
	restore  func() (ProfileRestore, error)
}

// Acts reports the one flag [LauncherActs] carries, from the member behind it: a fake with no restore
// func answers no start-up check at all, which is the posture of every hand-built Options.
func (h *fakeLauncherHost) Acts() LauncherActs { return LauncherActs{CanRestore: h.restore != nil} }

// Enabled answers TRUE when nothing was wired: a host that cannot tell is not evidence that the
// integration is off, and the verbs are let through to answer for themselves.
func (h *fakeLauncherHost) Enabled() bool {
	if h.enabled == nil {
		return true
	}
	return h.enabled()
}

// Profiles answers [ErrNoLauncher] when nothing was wired — "there is no launcher here", which
// `/model` reads as "offer the models the server itself advertises".
func (h *fakeLauncherHost) Profiles() ([]LaunchProfileChoice, error) {
	if h.profiles == nil {
		return nil, ErrNoLauncher
	}
	return h.profiles()
}

// Load answers as Profiles does, and nothing is activated.
func (h *fakeLauncherHost) Load(name string, progress func(step string)) (ProfileLoadResult, error) {
	if h.load == nil {
		return ProfileLoadResult{}, ErrNoLauncher
	}
	return h.load(name, progress)
}

// Unload and Stop answer as Load does, and the server is left exactly as it was.
func (h *fakeLauncherHost) Unload(endpoint string) (ActuationResult, error) {
	if h.unload == nil {
		return ActuationResult{}, ErrNoLauncher
	}
	return h.unload(endpoint)
}

func (h *fakeLauncherHost) Stop(endpoint string) (ActuationResult, error) {
	if h.stop == nil {
		return ActuationResult{}, ErrNoLauncher
	}
	return h.stop(endpoint)
}

// RecordProfile answers "not recorded, and nothing went wrong" — the silent skip the toggle off
// earns, and the whole answer of a host that records nothing.
func (h *fakeLauncherHost) RecordProfile(profile string) (bool, error) {
	if h.record == nil {
		return false, nil
	}
	return h.record(profile)
}

// Restore answers "do nothing, say nothing"; Acts reports false for it, so nothing asks.
func (h *fakeLauncherHost) Restore() (ProfileRestore, error) {
	if h.restore == nil {
		return ProfileRestore{}, nil
	}
	return h.restore()
}

// launcherSeams is the [LauncherHost] fake opts carries, created on the spot when it carries none —
// [serverSeams] for this family, so a test that adds the recording or the boot check to a host
// launcherOpts already wired finds the members that are there instead of replacing them.
func launcherSeams(opts *Options) *fakeLauncherHost {
	host, ok := opts.Launcher.(*fakeLauncherHost)
	if !ok {
		host = &fakeLauncherHost{}
		opts.Launcher = host
	}
	return host
}

// wireLauncher builds a ready, idle model with the heartbeat, the rebind AND the launcher host
// wired, one beat folded — the state a human is in when they type /model on a launcher host.
func wireLauncher(t *testing.T, fake *fakeLauncher) (Model, *fakeRebind) {
	t.Helper()
	return seededPicker(t, launcherOpts(fake))
}

// launcherOpts is the seam wiring alone, separated from the model build so a test can say what the
// integration ANSWERS before the session starts: since `llama-launcher:` became editable the host is
// wired for the life of the session and the on/off answer moved inside it (ADR 0037), so "wired" and
// "on" are two different things a test may need to set apart.
func launcherOpts(fake *fakeLauncher) Options {
	opts := testOpts
	host := launcherSeams(&opts)
	host.profiles, host.load, host.unload, host.stop = fake.list, fake.load, fake.act, fake.act
	return opts
}

// pumpTimeout is how long a test waits for one item off an actuation's channel. It is a deadlock
// guard, not a timing assumption: every fake answers immediately.
var pumpTimeout = testtime.Budget(5 * time.Second)

// pumpItem takes the next item the actuation has for the Update loop. The FIRST Cmd is the batch of
// producer and listener (the producer's own Msg is nil, so the item is whichever member carries
// one) — itself batched beside the spinner's first tick when the verb began an idle wait
// (busyAfterFold); every re-arm after that is the listener alone. untickedMsgs runs the tree as the
// program does and leaves the tick out, so the item is the first Msg it yields.
func pumpItem(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("the actuation stopped pumping before the latch released")
	}
	select {
	case msg, open := <-untickedMsgs(cmd):
		if !open {
			t.Fatal("the actuation's Cmd carried no pump item")
		}
		return msg
	case <-time.After(pumpTimeout):
		t.Fatal("the actuation pump produced nothing — the latch would never release")
	}
	return nil
}

// driveActuation folds an in-flight actuation to completion the way the program does: one pump item
// at a time, each fold re-arming the next, until the latch releases. It returns the model and the
// completion's own Cmd (the immediate beat, when the fold fired one).
func driveActuation(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	if !m.actuation.inFlight {
		t.Fatal("nothing to drive: the latch was not taken")
	}
	for range 100 {
		m, cmd = stepCmd(t, m, pumpItem(t, cmd))
		if !m.actuation.inFlight {
			return m, cmd
		}
	}
	t.Fatal("the actuation never completed")
	return m, nil
}

// startLoad types "/model <profile>" and returns the model with the latch held and the actuation's Cmd
// unrun — the in-flight window every latch assertion is made in.
func startLoad(t *testing.T, m Model, name string) (Model, tea.Cmd) {
	t.Helper()
	m, cmd := typeCommand(t, m, "/model "+name)
	if !m.actuation.inFlight {
		t.Fatalf("/model %s did not take the latch", name)
	}
	return m, cmd
}

// ----------------------------------------------------------------------------
// The latch
// ----------------------------------------------------------------------------

// While a profile load is in flight every path that would open an Exchange or move the session is
// refused with one note, and nothing is driven — /bg among them, whose launch reads the engine the
// completion may re-point. The latch IS the per-address serialization the
// facade's contract demands of its caller, so a second lifecycle verb can never overlap the first.
func TestActuationLatchRefusesEveryMoveWhileHeld(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	m, rb := wireLauncher(t, fake)
	sw := &fakeSwitch{}
	seams := serverSeams(&m.opts)
	seams.list, seams.switchTo = staticServers(twoServers), sw.switchTo
	eng := m.eng.(*fakeEngine)
	m, _ = startLoad(t, m, "alpha")
	want := "profile load in flight — alpha"

	for _, line := range []string{"/model other-model", "/server remote", "/continue", "/compact", "/bg /audit internal/", "hello"} {
		t.Run(line, func(t *testing.T) {
			next, cmd := typeCommand(t, m, line)

			if cmd != nil {
				t.Errorf("%q returned a Cmd while the latch was held", line)
			}
			if next.picker.open {
				t.Errorf("%q opened an overlay while the latch was held", line)
			}
			if next.state != stateIdle {
				t.Errorf("%q moved the state machine to %v", line, next.state)
			}
			if got := noteTexts(next); len(got) == 0 || got[len(got)-1] != want {
				t.Errorf("%q: notes = %v, want %q", line, got, want)
			}
		})
	}

	if m.actuation.profile != "alpha" {
		t.Errorf("the latch now names %q, want the verb it was taken for", m.actuation.profile)
	}
	if got := fake.listCount(); got != 1 {
		t.Errorf("the profile list was read %d times, want only the /model that took the latch", got)
	}
	if len(sw.calls) != 0 || len(rb.calls) != 0 || len(eng.submitted) != 0 || len(eng.launches()) != 0 {
		t.Errorf("a refused verb still drove a seam: switches %v, rebinds %v, submits %d, launches %v",
			sw.calls, rb.calls, len(eng.submitted), eng.launches())
	}
}

// Esc does not cancel an actuation: the facade offers no mid-flight cancel — its own cancel is
// /stop-server, available the moment the verb returns — so the key that stops a worker leaves the latch
// exactly where it is (ADR 0029 D6).
func TestActuationEscDoesNotCancel(t *testing.T) {
	t.Parallel()

	m, _ := wireLauncher(t, newLauncher())
	m, _ = startLoad(t, m, "alpha")
	notesBefore := len(noteTexts(m))

	m = step(t, m, keyEsc())

	if !m.actuation.inFlight {
		t.Error("esc released the actuation latch; there is no mid-flight cancel")
	}
	if got := noteTexts(m); len(got) != notesBefore {
		t.Errorf("notes = %v, want esc to say nothing", got)
	}
}

// The latch's own words name what is being waited on for a load, and name the verb for the two
// that act on the session's server instead of on a profile.
func TestActuationBlockNoteNamesTheVerb(t *testing.T) {
	t.Parallel()

	m, _ := wireLauncher(t, newLauncher())
	m, _ = startLoad(t, m, "alpha")
	if got, want := m.actuationBlockNote(), "profile load in flight — alpha"; got != want {
		t.Errorf("load block note = %q, want %q", got, want)
	}

	m, _ = wireLauncher(t, newLauncher())
	m2, _ := m.startServerActuation(verbStop)
	if got, want := m2.(Model).actuationBlockNote(), "stop-server in flight"; got != want {
		t.Errorf("stop block note = %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// The footer
// ----------------------------------------------------------------------------

// While the latch is held the footer's model slot says what is happening, and it OUTRANKS
// "connecting…" — the session is not merely waiting for a server, it is waiting for this profile.
func TestActuationFooterNarratesTheVerb(t *testing.T) {
	t.Parallel()

	m, _ := wireLauncher(t, newLauncher())
	m, _ = startLoad(t, m, "alpha")

	if got, want := m.upstreamSegments(), []string{"loading alpha…"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("upstream segments = %v, want %v", got, want)
	}
	if view := plain(m.View()); !strings.Contains(view, "loading alpha…") {
		t.Errorf("the footer does not say what is loading:\n%s", view)
	}
	// The verb replaces the model segment alone: the host it is loading on stays named beside it,
	// and so would the workspace — a local fact no launcher verb touches.
	footer := ansiPattern.ReplaceAllString(m.footerContent(80), "")
	if !strings.Contains(footer, "test-host") {
		t.Errorf("footer = %q, want the host kept beside the verb", footer)
	}
	if strings.Contains(footer, format.Tokens(m.opts.ContextWindow)) {
		t.Errorf("footer = %q, want no context window in it — the gauge states it now", footer)
	}

	// The unbound cold start would say "connecting…"; the actuation is the more specific truth.
	unboundModel := m
	unboundModel.opts.Model = ""
	if got := unboundModel.upstreamSegments(); len(got) != 1 || got[0] != "loading alpha…" {
		t.Errorf("upstream segments = %v, want the actuation to outrank %q", got, connectingLabel)
	}

	m, _ = wireLauncher(t, newLauncher())
	stopping, _ := m.startServerActuation(verbStop)
	if got := stopping.(Model).upstreamSegments(); len(got) != 1 || got[0] != "stop-server…" {
		t.Errorf("upstream segments = %v, want the stop verb named", got)
	}
}

// ----------------------------------------------------------------------------
// The pump
// ----------------------------------------------------------------------------

// Every launcher progress step lands as one transcript note AS IT HAPPENS, in the order the
// launcher emitted it, and the completion closes the sequence (ADR 0029 D6).
func TestActuationPumpOrdersStepsIntoNotes(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	fake.steps = []string{"stopping llamacpp on :1234", "starting llamacpp", "waiting for health"}
	fake.result = ProfileLoadResult{Notices: []string{"config: 2 profiles share a port"}}
	m, _ := wireLauncher(t, fake)
	m, cmd := startLoad(t, m, "alpha")
	notesBefore := len(noteTexts(m))

	m, _ = driveActuation(t, m, cmd)

	want := append(append([]string(nil), fake.steps...),
		"config: 2 profiles share a port",
		"profile alpha loaded — waiting for the beat")
	if got := noteTexts(m)[notesBefore:]; !reflect.DeepEqual(got, want) {
		t.Errorf("notes = %v, want %v", got, want)
	}
}

// A pump item from a released latch is inert — the heartbeat's generation guard, applied to the
// actuation — so nothing still in flight can be folded into a session that has moved on.
func TestActuationStaleGenerationIsInert(t *testing.T) {
	t.Parallel()

	m, _ := wireLauncher(t, newLauncher())
	m, cmd := startLoad(t, m, "alpha")
	staleGen := m.actuation.gen
	m, _ = driveActuation(t, m, cmd)
	notesBefore := len(noteTexts(m))

	m, cmd = stepCmd(t, m, actuationMsg{gen: staleGen, ev: actuationEvent{step: "a step from the last verb"}})

	if cmd != nil {
		t.Error("a stale pump item re-armed the listener")
	}
	if got := noteTexts(m); len(got) != notesBefore {
		t.Errorf("notes = %v, want nothing added by a retired actuation", got)
	}
}

// ----------------------------------------------------------------------------
// The beat shadow (ADR 0029 D5)
// ----------------------------------------------------------------------------

// A failed beat while an actuation is in flight counts toward NOTHING: the server is expected to be
// down mid-restart. Once the latch releases the failures count again — the downtime after a
// /stop-server is real and crosses offline (at the first failure, since the release primed the count:
// TestFailedOwedBeatHoldsTheFlush).
func TestActuationShadowsFailedBeats(t *testing.T) {
	t.Parallel()

	m, _ := wireLauncher(t, newLauncher())
	m, cmd := startLoad(t, m, "alpha")

	for range 3 {
		m = foldBeatMsg(t, m, downBeat("connection refused"))
	}

	if m.hb.offline || m.hb.failures != 0 {
		t.Errorf("hb = {offline:%v failures:%d}, want failed beats ignored under the latch",
			m.hb.offline, m.hb.failures)
	}
	if n := countNotes(m, "server offline"); n != 0 {
		t.Errorf("offline notes = %d, want none while the server is expected to be down", n)
	}

	m, _ = driveActuation(t, m, cmd)
	for range offlineFailureThreshold {
		m = foldBeatMsg(t, m, downBeat("connection refused"))
	}

	if !m.hb.offline {
		t.Error("the offline crossing is still suppressed after the latch released")
	}
}

// A beat that LANDS in the shadow is folded normally: a server answering mid-actuation is harmless
// news, and suppressing it would only delay the binding the load exists to produce. What IS deferred
// is the binding itself — the completion may be about to re-point the whole session, and a rebind
// driven into the engine beside that move is the unsynchronized pair the latch exists to prevent —
// so the observation is stashed and the completion fold is the boundary it lands at, exactly as
// finishWorker is for one observed mid-Exchange.
func TestActuationDefersABindingObservedUnderTheLatch(t *testing.T) {
	t.Parallel()

	m, rb := wireLauncher(t, newLauncher())
	m, cmd := startLoad(t, m, "alpha")

	m = foldBeatMsg(t, m, upBeat("other-model", 16384))

	if len(rb.calls) != 0 {
		t.Fatalf("rebind calls = %v, want none while a launcher verb owns the server", rb.calls)
	}
	want := rebindIntent{model: "other-model", window: 16384}
	if m.hb.pendingRebind == nil || !reflect.DeepEqual(*m.hb.pendingRebind, want) {
		t.Fatalf("pendingRebind = %+v, want the observation stashed (%+v)", m.hb.pendingRebind, want)
	}
	if !m.hb.everOnline || m.hb.offline {
		t.Errorf("hb = {everOnline:%v offline:%v}, want the landed beat itself folded normally",
			m.hb.everOnline, m.hb.offline)
	}

	m, _ = driveActuation(t, m, cmd)

	if got := ([]rebindCall{{model: "other-model", window: 16384}}); !reflect.DeepEqual(rb.calls, got) {
		t.Errorf("rebind calls = %v, want the stashed change applied exactly once at the completion", rb.calls)
	}
	if m.hb.pendingRebind != nil {
		t.Errorf("pendingRebind = %+v, want it cleared by the apply", m.hb.pendingRebind)
	}
	if m.opts.Model != "other-model" {
		t.Errorf("opts.Model = %q, want the deferred binding bound by the completion", m.opts.Model)
	}
}

// ----------------------------------------------------------------------------
// The completion folds
// ----------------------------------------------------------------------------

// A profile that loaded into the very server this session is talking to moves nothing: the latch
// releases, the note says the load landed, and the immediate beat is what completes it (the
// ordinary rebind path then words the model change).
func TestProfileLoadSameServerWaitsForTheBeat(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	m, rb := wireLauncher(t, fake)
	before := m.opts
	m, cmd := startLoad(t, m, "alpha")

	m, cmd = driveActuation(t, m, cmd)

	if m.actuation.inFlight {
		t.Fatal("the latch was not released by the completion")
	}
	if got := fake.loaded(); !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Errorf("loads = %v, want the picked profile activated once", got)
	}
	if m.opts.Endpoint != before.Endpoint || m.opts.Model != before.Model {
		t.Errorf("the session moved on a same-server load: endpoint %q, model %q",
			m.opts.Endpoint, m.opts.Model)
	}
	if len(rb.calls) != 0 {
		t.Errorf("rebind calls = %v, want none — only a Beat binds", rb.calls)
	}
	want := "profile alpha loaded — waiting for the beat"
	if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != want {
		t.Errorf("notes = %v, want %q", got, want)
	}
	if cmd == nil {
		t.Fatal("the completion fired no beat — the load would not be observed for a full Interval")
	}
	beat, ok := cmd().(beatMsg)
	if !ok || beat.gen != m.hb.gen {
		t.Errorf("the completion's Cmd yielded %T, want an immediate beat on generation %d", cmd(), m.hb.gen)
	}
}

// A profile that lives on another server is FOLLOWED: the completion commits the move the load
// resolved and then takes the /server fold whole — display adopted, model unbound, a fresh heartbeat
// generation, and the first beat of the new chain fired now.
func TestProfileLoadMovedTakesTheSwitchFold(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	fake.follows(ServerSwitchResult{
		Endpoint:      "http://localhost:8081",
		HostAlias:     "beta",
		ContextWindow: remoteWindow,
	}, nil)
	m, rb := wireLauncher(t, fake)
	oldGen := m.hb.gen
	m, cmd := startLoad(t, m, "beta")

	m, cmd = driveActuation(t, m, cmd)

	if m.actuation.inFlight {
		t.Fatal("the latch was not released by the completion")
	}
	if n := fake.committed(); n != 1 {
		t.Errorf("the resolved move was committed %d times, want exactly once — by the fold", n)
	}
	if m.opts.Endpoint != "http://localhost:8081" || m.opts.HostAlias != "beta" {
		t.Errorf("opts = {%q %q}, want the profile's server adopted", m.opts.Endpoint, m.opts.HostAlias)
	}
	if m.opts.Model != "" {
		t.Errorf("opts.Model = %q, want the model unbound until the new server's first beat", m.opts.Model)
	}
	if m.hb.gen == oldGen || !m.hb.switched {
		t.Errorf("hb = %+v, want a fresh generation and the switched mark", m.hb)
	}
	want := "switching server: test-host → beta (http://localhost:8081)"
	if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != want {
		t.Errorf("notes = %v, want %q", got, want)
	}
	if len(rb.calls) != 0 {
		t.Errorf("rebind calls = %v, want none — the first beat binds, not the load", rb.calls)
	}
	if cmd == nil {
		t.Fatal("the moved completion fired no beat")
	}
	if beat, ok := cmd().(beatMsg); !ok || beat.gen != m.hb.gen {
		t.Errorf("the completion's Cmd yielded %T, want the new chain's first beat", cmd())
	}
}

// A followed profile is not a startup CHOICE: its server is an address the launcher picked, named by
// no `servers:` entry, so the fold records nothing and claims nothing (ADR 0036 decision 2). The
// recording seam is wired for this test precisely so an unwanted call would be caught.
func TestProfileLoadMoveRecordsNoStartupChoice(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	fake.follows(ServerSwitchResult{Endpoint: "http://localhost:8081", HostAlias: "beta"}, nil)
	rec := &fakeRecorder{saved: true} // would report a write if it were ever called
	opts := launcherOpts(fake)
	serverSeams(&opts).record = rec.record
	m, _ := seededPicker(t, opts)
	m, cmd := startLoad(t, m, "beta")

	m, _ = driveActuation(t, m, cmd)

	if len(rec.names) != 0 {
		t.Errorf("recorded %v, want nothing — a profile's server is no entry a session could start on", rec.names)
	}
	want := "switching server: test-host → beta (http://localhost:8081)"
	if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != want {
		t.Errorf("notes = %v, want %q with no saved clause", got, want)
	}
}

// The move a load resolves is committed by the FOLD, never by the seam that resolved it. The seam
// blocks for minutes on a Cmd goroutine while the move re-points the engine, and the Update loop is
// the only boundary that orders such a mutation against the heartbeat's own rebinds — so a load that
// has answered must have changed nothing about the session until its completion is folded.
func TestProfileLoadCommitsTheMoveOnTheUpdateGoroutine(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	fake.follows(ServerSwitchResult{Endpoint: "http://localhost:8081", HostAlias: "beta"}, nil)
	m, _ := wireLauncher(t, fake)
	m, cmd := startLoad(t, m, "beta")

	// Running the pump's producer runs the whole seam on its own goroutine: the load has answered and
	// its completion is sitting on the channel, unfolded.
	item := pumpItem(t, cmd)

	if got := fake.loaded(); !reflect.DeepEqual(got, []string{"beta"}) {
		t.Fatalf("loads = %v, want the profile activated by the Cmd goroutine", got)
	}
	if n := fake.committed(); n != 0 {
		t.Errorf("the move was committed %d times off the Update goroutine; the seam resolves it and stops", n)
	}

	m, _ = stepCmd(t, m, item)

	if n := fake.committed(); n != 1 {
		t.Errorf("the move was committed %d times by the fold, want exactly once", n)
	}
	if m.opts.Endpoint != "http://localhost:8081" {
		t.Errorf("opts.Endpoint = %q, want the fold to have adopted what the committed move answered",
			m.opts.Endpoint)
	}
}

// A move the engine refuses leaves the session exactly where it was — Agent.SwitchUpstream is
// validate-then-commit — and the note says both halves of the truth: the profile IS loaded, and the
// session did not follow it. The one beat fired is the one every release owes ([Model.armOwedBeat]),
// on the server the session never left.
func TestProfileLoadMoveRefusedKeepsTheSession(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	fake.follows(ServerSwitchResult{}, errors.New("an exchange is still open"))
	m, _ := wireLauncher(t, fake)
	before := m.opts
	m, cmd := startLoad(t, m, "beta")

	m, cmd = driveActuation(t, m, cmd)

	if m.actuation.inFlight {
		t.Fatal("a refused move left the latch held")
	}
	if m.opts.Endpoint != before.Endpoint || m.opts.Model != before.Model {
		t.Errorf("the session moved on a refused move: endpoint %q, model %q", m.opts.Endpoint, m.opts.Model)
	}
	assertArmsOwedBeat(t, m, cmd)
	want := "profile beta loaded, but the session could not follow it: an exchange is still open"
	if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != want {
		t.Errorf("notes = %v, want %q", got, want)
	}
}

// A completion's immediate beat ARMS a chain rather than merely issuing one on the running
// generation: the chain that was live when the load started is retired by the bump, so exactly one
// chain polls the server afterwards. Two would double the /v1/models traffic against the single-slot
// local server this product targets and halve the offline debounce (doc.go's tick-chain invariant).
func TestProfileLoadLeavesExactlyOneBeatChain(t *testing.T) {
	t.Parallel()

	m, _ := wireLauncher(t, newLauncher())
	retired := m.hb.gen
	m, cmd := startLoad(t, m, "alpha")

	m, cmd = driveActuation(t, m, cmd)

	if m.hb.gen == retired {
		t.Fatalf("hb.gen = %d, want the completion's immediate beat to open a fresh chain", m.hb.gen)
	}
	if cmd == nil {
		t.Fatal("the completion fired no beat")
	}
	if beat, ok := cmd().(beatMsg); !ok || beat.gen != m.hb.gen {
		t.Fatalf("the completion's Cmd yielded %T, want the armed chain's first beat", cmd())
	}
	if _, stale := stepCmd(t, m, heartbeatTickMsg{gen: retired}); stale != nil {
		t.Error("a tick from the chain the load displaced still scheduled a beat — two chains poll one server")
	}
	if _, live := stepCmd(t, m, heartbeatTickMsg{gen: m.hb.gen}); live == nil {
		t.Error("the armed chain's own tick scheduled nothing — the load left no live chain at all")
	}
}

// A failed load says the launcher's own words and releases the latch. Notices recorded before the
// failure still travel: a load that failed after warning about the config still warned. A failed
// launch may still have stopped the server it was restarting, so the release fires the beat it owes.
func TestProfileLoadFailureReleasesTheLatch(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	fake.steps = []string{"starting llamacpp"}
	fake.result = ProfileLoadResult{Notices: []string{"config: unknown key 'thraeds'"}}
	fake.loadErr = errors.New("model file /models/alpha.gguf not found")
	m, _ := wireLauncher(t, fake)
	m, cmd := startLoad(t, m, "alpha")
	notesBefore := len(noteTexts(m))

	m, cmd = driveActuation(t, m, cmd)

	if m.actuation.inFlight {
		t.Fatal("a failed load left the latch held")
	}
	assertArmsOwedBeat(t, m, cmd)
	want := []string{
		"starting llamacpp",
		"config: unknown key 'thraeds'",
		"model file /models/alpha.gguf not found",
	}
	if got := noteTexts(m)[notesBefore:]; !reflect.DeepEqual(got, want) {
		t.Errorf("notes = %v, want %v", got, want)
	}
}

// The health-wait timeout is the one failure with a coda, because it is the one that may still come
// good: the launcher leaves the server running, and the unsuppressed landed-beat path binds it if it
// comes up.
func TestProfileLoadTimeoutCarriesTheCoda(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	fake.loadErr = fmt.Errorf("llamacpp did not become healthy within 30s (pid 4711, log /tmp/a.log): %w",
		ErrStartupTimeout)
	m, _ := wireLauncher(t, fake)
	m, cmd := startLoad(t, m, "alpha")

	m, _ = driveActuation(t, m, cmd)

	got := noteTexts(m)
	last := got[len(got)-1]
	if !strings.HasSuffix(last, startupTimeoutCoda) {
		t.Errorf("last note = %q, want the timeout coda appended", last)
	}
	if !strings.HasPrefix(last, "llamacpp did not become healthy") {
		t.Errorf("last note = %q, want the launcher's own words kept", last)
	}
	if m.actuation.inFlight {
		t.Error("a timed-out load left the latch held")
	}
}

// A seam that PANICS still releases the latch: the completion is sent from a defer, so there is no
// path out of the verb that leaves the session unable to act (or to send) again.
func TestProfileLoadPanicReleasesTheLatch(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	fake.panics = true
	m, _ := wireLauncher(t, fake)
	m, cmd := startLoad(t, m, "alpha")

	m, _ = driveActuation(t, m, cmd)

	if m.actuation.inFlight {
		t.Fatal("a panicking seam stranded the latch")
	}
	if got := noteTexts(m); len(got) == 0 || !strings.Contains(got[len(got)-1], "the launcher fell over") {
		t.Errorf("notes = %v, want the panic reported", got)
	}
}

// ----------------------------------------------------------------------------
// Recording the loaded profile — the `launch-profile:` key (remember-model)
// ----------------------------------------------------------------------------

// recordingLoad is wireLauncher with the profile-recording seam wired too — the state a human is in
// when `remember-model:` is on and a profile load is also the choice their server comes back on.
func recordingLoad(t *testing.T, fake *fakeLauncher, rec *fakeRecorder) Model {
	t.Helper()
	opts := launcherOpts(fake)
	launcherSeams(&opts).record = rec.record
	m, _ := seededPicker(t, opts)
	return m
}

// Both shapes of a COMMITTED load record the profile exactly once and say so: the one that landed on
// the server this session is already on, and the one the session had to follow onto another. What the
// pointer is written onto is the binary's business — the renderer offers the profile name and states
// the answer.
func TestProfileLoadCommitRecordsTheProfile(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		profile string
		moves   bool
	}{
		{name: "loaded into the session's own server", profile: "alpha"},
		{name: "followed onto another server", profile: "beta", moves: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newLauncher()
			if tc.moves {
				fake.follows(ServerSwitchResult{Endpoint: "http://localhost:8081", HostAlias: "beta"}, nil)
			}
			rec := &fakeRecorder{saved: true}
			m, cmd := startLoad(t, recordingLoad(t, fake, rec), tc.profile)

			m, _ = driveActuation(t, m, cmd)

			if want := []string{tc.profile}; !reflect.DeepEqual(rec.names, want) {
				t.Fatalf("recorded profiles = %v, want %v — once, at the commit", rec.names, want)
			}
			if n := countNotes(m, launchProfileSavedNote); n != 1 {
				t.Errorf("notes = %v, want exactly one %q", noteTexts(m), launchProfileSavedNote)
			}
		})
	}
}

// Nothing that is not a load COMMIT touches the pointer: a load that failed, a load the session could
// not follow, and the two verbs that free the GPU. The seam is wired for this test precisely so an
// unwanted call would be caught — `/unload-model` and `/stop-server` mean "free it now", not "forget
// which model this server runs".
func TestNothingButACommitRecordsTheProfile(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		line    string
		arrange func(*fakeLauncher)
	}{
		{
			name: "a load that failed",
			line: "/model alpha",
			arrange: func(f *fakeLauncher) {
				f.loadErr = errors.New("model file /models/alpha.gguf not found")
			},
		},
		{
			name: "a move the session could not follow",
			line: "/model beta",
			arrange: func(f *fakeLauncher) {
				f.follows(ServerSwitchResult{}, errors.New("the engine is busy"))
			},
		},
		{name: "/unload-model", line: "/unload-model"},
		{name: "/stop-server", line: "/stop-server"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newLauncher()
			if tc.arrange != nil {
				tc.arrange(fake)
			}
			rec := &fakeRecorder{saved: true} // would report a write if it were ever called
			m, cmd := typeCommand(t, recordingLoad(t, fake, rec), tc.line)
			if !m.actuation.inFlight {
				t.Fatalf("%q took no latch", tc.line)
			}

			m, _ = driveActuation(t, m, cmd)

			if len(rec.names) != 0 {
				t.Errorf("recorded %v, want nothing — the key names what the launcher was made to serve", rec.names)
			}
			if n := countNotes(m, launchProfileSavedNote); n != 0 {
				t.Errorf("notes = %v, want no saved line", noteTexts(m))
			}
		})
	}
}

// The renderer cannot tell a recordable load from one the binary skips — the toggle off, no actuating
// entry to write onto — so it offers every commit and believes the answer: false with no error is
// announced as nothing at all, and a write that could not land is a footnote that undoes nothing.
func TestProfileRecordingAnswerIsBelieved(t *testing.T) {
	t.Parallel()

	t.Run("a silent skip claims nothing", func(t *testing.T) {
		t.Parallel()

		rec := &fakeRecorder{} // the binary's silent skip: no write, no error
		m, cmd := startLoad(t, recordingLoad(t, newLauncher(), rec), "alpha")

		m, _ = driveActuation(t, m, cmd)

		if want := []string{"alpha"}; !reflect.DeepEqual(rec.names, want) {
			t.Fatalf("recorded profiles = %v, want %v — the binary decides, the renderer asks", rec.names, want)
		}
		want := "profile alpha loaded — waiting for the beat"
		if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != want {
			t.Errorf("notes = %v, want %q last and no saved line", got, want)
		}
	})

	t.Run("a failed write is a footnote", func(t *testing.T) {
		t.Parallel()

		rec := &fakeRecorder{err: errors.New("config.yaml is a directory")}
		m, cmd := startLoad(t, recordingLoad(t, newLauncher(), rec), "alpha")

		m, cmd = driveActuation(t, m, cmd)

		if m.actuation.inFlight {
			t.Fatal("a failed recording stranded the latch")
		}
		if cmd == nil {
			t.Error("a failed recording swallowed the completion's beat")
		}
		want := "could not record the launch profile: config.yaml is a directory"
		if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != want {
			t.Errorf("notes = %v, want %q last", got, want)
		}
	})
}

// The two verbs that act on the session's own server report one note per recorded step and then
// leave the display to the heartbeat, firing the beat the release owes. The steps travel even beside
// an error — how far a stop got before it failed is exactly what the human needs.
func TestServerActuationNotesEveryStep(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		want []string
	}{
		{name: "success", want: []string{"asked llamacpp to stop", "process exited"}},
		{
			name: "failure",
			err:  errors.New("port 1234 still listening"),
			want: []string{"asked llamacpp to stop", "process exited", "port 1234 still listening"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newLauncher()
			fake.actResult = ActuationResult{Steps: []string{"asked llamacpp to stop", "process exited"}}
			fake.actErr = tc.err
			m, _ := wireLauncher(t, fake)
			notesBefore := len(noteTexts(m))

			started, cmd := m.startServerActuation(verbStop)
			m, cmd = driveActuation(t, started.(Model), cmd)

			if m.actuation.inFlight {
				t.Fatal("the latch was not released")
			}
			assertArmsOwedBeat(t, m, cmd)
			if got := fake.actuated(); !reflect.DeepEqual(got, []string{testOpts.Endpoint}) {
				t.Errorf("actuated = %v, want the session's endpoint", got)
			}
			if got := noteTexts(m)[notesBefore:]; !reflect.DeepEqual(got, tc.want) {
				t.Errorf("notes = %v, want %v", got, tc.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// /unload-model and /stop-server (ADR 0029 D3)
// ----------------------------------------------------------------------------

// The old spellings name nothing any more. They were removed outright rather than aliased: an alias
// would put the ambiguous names — "/stop" reads as "stop the running turn", "/unload" names no object
// at all — back in the PARSER, which is the one place the rename exists to take them out of. So a
// typed "/unload" or "/stop" earns the sole-token typo guard's refusal and acts on nothing, exactly
// as "/load" does since the launcher's profiles became /model's offering.
func TestTheOldVerbSpellingsAreGone(t *testing.T) {
	t.Parallel()

	for _, old := range []string{"unload", "stop"} {
		t.Run(old, func(t *testing.T) {
			t.Parallel()

			if spec, ok := commandByName(old); ok {
				t.Errorf("commandSpecs still carries %+v; the verbs name their object now", spec)
			}
			m, _ := wireLauncher(t, newLauncher())

			m, cmd := typeCommand(t, m, "/"+old)

			if cmd != nil {
				t.Errorf("/%s returned a Cmd; the word names nothing", old)
			}
			if m.actuation.inFlight {
				t.Errorf("/%s still acted: actuation %+v", old, m.actuation)
			}
			want := unknownSlashNote("/" + old)
			if got := noteTexts(m); len(got) == 0 || got[len(got)-1] != want {
				t.Errorf("notes = %v, want the unknown-slash refusal %q", got, want)
			}
		})
	}
}

// Both verbs are typed with no argument — there is nothing to choose, they act on the server this
// session is talking to and on nothing else — and both run through the ONE latch: while either is in
// flight all three launcher verbs are refused, which is the per-address serialization the facade
// demands of its caller now covering the whole trio.
func TestUnloadAndStopActOnTheSessionsServer(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		line    string
		verb    string
		unloads []string
		stops   []string
	}{
		{line: "/unload-model", verb: verbUnload, unloads: []string{testOpts.Endpoint}},
		{line: "/stop-server", verb: verbStop, stops: []string{testOpts.Endpoint}},
	} {
		t.Run(tc.line, func(t *testing.T) {
			t.Parallel()

			unloader, stopper := newLauncher(), newLauncher()
			m, _ := wireLauncher(t, newLauncher())
			launcherSeams(&m.opts).unload, launcherSeams(&m.opts).stop = unloader.act, stopper.act

			m, cmd := typeCommand(t, m, tc.line)

			if !m.actuation.inFlight || m.actuation.verb != tc.verb {
				t.Fatalf("actuation = {inFlight:%v verb:%q}, want the %s latch held",
					m.actuation.inFlight, m.actuation.verb, tc.verb)
			}
			for _, refused := range []string{"/model alpha", "/unload-model", "/stop-server"} {
				next, blocked := typeCommand(t, m, refused)
				if blocked != nil {
					t.Errorf("%q returned a Cmd while %s was in flight", refused, tc.verb)
				}
				want := tc.verb + " in flight"
				if got := noteTexts(next); len(got) == 0 || got[len(got)-1] != want {
					t.Errorf("%q: notes = %v, want %q", refused, got, want)
				}
			}

			m, cmd = driveActuation(t, m, cmd)

			if m.actuation.inFlight {
				t.Fatal("the latch was not released by the completion")
			}
			assertArmsOwedBeat(t, m, cmd)
			if got := unloader.actuated(); !reflect.DeepEqual(got, tc.unloads) {
				t.Errorf("unload seam called with %v, want %v", got, tc.unloads)
			}
			if got := stopper.actuated(); !reflect.DeepEqual(got, tc.stops) {
				t.Errorf("stop seam called with %v, want %v", got, tc.stops)
			}
		})
	}
}

// /unload-model owes one sentence its steps cannot carry: whether freeing the model also took the server
// with it. On a managed backend an unload IS a stop — the session's server is gone, which is what the
// human needs before wondering why the beat went quiet — while an external backend keeps serving. A
// FAILED unload claims neither: the launcher's error is the last word.
func TestUnloadWordsTheManagedSemantic(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		res  ActuationResult
		err  error
		want []string
	}{
		{
			name: "managed backend",
			res:  ActuationResult{Steps: []string{"Unloading model"}, ServerStopped: true, Backend: "llamacpp"},
			want: []string{"Unloading model", "model unloaded — this stopped llamacpp"},
		},
		{
			name: "external backend",
			res:  ActuationResult{Steps: []string{"Unloading model"}, Backend: "ollama"},
			want: []string{"Unloading model", "model unloaded — server still up"},
		},
		{
			name: "backend unnamed",
			res:  ActuationResult{ServerStopped: true},
			want: []string{"model unloaded — this stopped the server"},
		},
		{
			name: "failed",
			res:  ActuationResult{Steps: []string{"Unloading model"}, Backend: "ollama"},
			err:  errors.New("the model is still loaded"),
			want: []string{"Unloading model", "the model is still loaded"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newLauncher()
			fake.actResult, fake.actErr = tc.res, tc.err
			m, _ := wireLauncher(t, fake)
			notesBefore := len(noteTexts(m))

			m, cmd := typeCommand(t, m, "/unload-model")
			m, _ = driveActuation(t, m, cmd)

			if got := noteTexts(m)[notesBefore:]; !reflect.DeepEqual(got, tc.want) {
				t.Errorf("notes = %v, want %v", got, tc.want)
			}
		})
	}
}

// /stop-server heads the launcher's steps with what it was stopping. The steps themselves are terse and
// subject-less ("Sending stop signal"), and the renderer cannot derive the subject — the session holds
// an endpoint URL, not the address the launcher manages nor the name of the server answering there —
// so the heading is the only line that says what just went down. A launcher that named neither earns
// no heading rather than an empty one.
func TestStopHeadsTheStepsWithWhatItStopped(t *testing.T) {
	t.Parallel()

	steps := []string{"Sending stop signal", "Waiting for shutdown"}
	for _, tc := range []struct {
		name string
		res  ActuationResult
		want []string
	}{
		{
			name: "backend and address",
			res:  ActuationResult{Steps: steps, ServerStopped: true, Backend: "llamacpp", Addr: "localhost:1234"},
			want: []string{"stopping llamacpp (localhost:1234)", "Sending stop signal", "Waiting for shutdown"},
		},
		{
			name: "address alone",
			res:  ActuationResult{Steps: steps, ServerStopped: true, Addr: "localhost:1234"},
			want: []string{"stopping localhost:1234", "Sending stop signal", "Waiting for shutdown"},
		},
		{
			name: "nothing named",
			res:  ActuationResult{Steps: steps, ServerStopped: true},
			want: steps,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newLauncher()
			fake.actResult = tc.res
			m, _ := wireLauncher(t, fake)
			notesBefore := len(noteTexts(m))

			m, cmd := typeCommand(t, m, "/stop-server")
			m, _ = driveActuation(t, m, cmd)

			if got := noteTexts(m)[notesBefore:]; !reflect.DeepEqual(got, tc.want) {
				t.Errorf("notes = %v, want %v", got, tc.want)
			}
		})
	}
}

// An endpoint the launcher does not manage is refused by the bridge, and the refusal reaches the human
// verbatim: nothing was acted on, and naming the endpoint is what makes that actionable (a remote
// server's local half is the launcher's MCP adapter, not this verb). The latch releases all the same.
func TestServerActuationReportsTheNotManagedRefusal(t *testing.T) {
	t.Parallel()

	for _, line := range []string{"/unload-model", "/stop-server"} {
		t.Run(line, func(t *testing.T) {
			t.Parallel()

			want := "the launcher doesn't manage " + testOpts.Endpoint
			fake := newLauncher()
			fake.actErr = errors.New(want)
			m, _ := wireLauncher(t, fake)
			notesBefore := len(noteTexts(m))

			m, cmd := typeCommand(t, m, line)
			m, _ = driveActuation(t, m, cmd)

			if m.actuation.inFlight {
				t.Fatal("a refused actuation left the latch held")
			}
			if got := noteTexts(m)[notesBefore:]; !reflect.DeepEqual(got, []string{want}) {
				t.Errorf("notes = %v, want the bridge's refusal alone %q", got, want)
			}
		})
	}
}

// Without the launcher wired both verbs degrade to the one line every launcher verb owes, and neither
// latches: there is nothing in flight to serialize.
func TestUnloadAndStopWithoutTheLauncher(t *testing.T) {
	t.Parallel()

	m, _ := seededPicker(t, testOpts) // no launcher seams wired
	for _, line := range []string{"/unload-model", "/stop-server"} {
		next, cmd := typeCommand(t, m, line)

		if cmd != nil {
			t.Errorf("%q returned a Cmd with no launcher wired", line)
		}
		if next.actuation.inFlight {
			t.Errorf("%q took the latch with no launcher wired", line)
		}
		if got := noteTexts(next); len(got) == 0 || got[len(got)-1] != noLauncherNote {
			t.Errorf("%q: notes = %v, want %q", line, got, noLauncherNote)
		}
	}
}

// With the seams wired but the integration switched OFF — the state ADR 0037 created by making
// `llama-launcher:` editable mid-session — both verbs say the same sentence, and say it on the
// keypress. Latching first and letting the seam's own refusal come back through the pump would show
// a frame of "unloading…" in the footer for a verb that never ran, where the unwired session above
// answered instantly; the transient state is the defect, not the words.
func TestUnloadAndStopWithTheLauncherSwitchedOff(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	opts := launcherOpts(fake)
	launcherSeams(&opts).enabled = func() bool { return false }
	m, _ := seededPicker(t, opts)

	for _, line := range []string{"/unload-model", "/stop-server"} {
		next, cmd := typeCommand(t, m, line)

		if cmd != nil {
			t.Errorf("%q returned a Cmd with the launcher switched off", line)
		}
		if next.actuation.inFlight {
			t.Errorf("%q took the latch with the launcher switched off", line)
		}
		if label := next.actuationLabel(); label != "" {
			t.Errorf("%q put %q in the footer for a verb that never ran", line, label)
		}
		if got := noteTexts(next); len(got) == 0 || got[len(got)-1] != noLauncherNote {
			t.Errorf("%q: notes = %v, want %q", line, got, noLauncherNote)
		}
	}
	if got := fake.actuated(); len(got) != 0 {
		t.Errorf("the verbs ran against %v; want the refusal to have come before the seam", got)
	}
}

// After a /stop-server the downtime is REAL, and the display says so: the latch released with the
// completion, so the beat shadow is gone and the failed beats cross this session offline, narrated
// once however many follow.
func TestStopThenBeatFailuresCrossOffline(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	fake.actResult = ActuationResult{
		Steps: []string{"Sending stop signal"}, ServerStopped: true, Backend: "llamacpp", Addr: "localhost:1234",
	}
	m, _ := wireLauncher(t, fake)
	m, cmd := typeCommand(t, m, "/stop-server")
	m, _ = driveActuation(t, m, cmd)

	for range offlineFailureThreshold {
		m = foldBeatMsg(t, m, downBeat("connection refused"))
	}

	if !m.hb.offline {
		t.Error("the session did not cross offline after its own server was stopped")
	}
	if n := countNotes(m, "server offline"); n != 1 {
		t.Errorf("offline notes = %d, want the crossing narrated once", n)
	}
}

// ----------------------------------------------------------------------------
// The automatic openers wait out every refusing hold (adr-0029-amend-1123)
// ----------------------------------------------------------------------------

// stopServerMidRun opens an Exchange on a launcher host, queues /stop-server and stages one row
// while it runs, then completes the Exchange: the drain runs the queued verb, which takes the latch,
// so the completion flush meets it. It returns the model with the latch held and the Cmd carrying
// the verb's pump.
func stopServerMidRun(t *testing.T) (Model, tea.Cmd) {
	t.Helper()
	return launcherVerbMidRun(t, "/stop-server")
}

// launcherVerbMidRun is [stopServerMidRun] for either verb that acts on the session's own server,
// `/stop-server` or `/unload-model`, typed as line.
func launcherVerbMidRun(t *testing.T, line string) (Model, tea.Cmd) {
	t.Helper()
	fake := newLauncher()
	fake.actResult = ActuationResult{
		Steps: []string{"Sending stop signal"}, ServerStopped: true, Backend: "llamacpp", Addr: "localhost:1234",
	}
	m, _ := wireLauncher(t, fake)
	m = stageRow(t, m, "open the exchange")
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v, want running", m.state)
	}
	m = stageRow(t, m, line)
	m = stageRow(t, m, "and the docs")
	if len(m.deferredCommands) != 1 || len(m.pendingInterjections) != 1 {
		t.Fatalf("precondition: %d commands queued, %d rows staged; want one of each",
			len(m.deferredCommands), len(m.pendingInterjections))
	}
	m, cmd := stepCmd(t, m, exchangeDoneMsg{})
	if !m.actuation.inFlight {
		t.Fatalf("precondition: the drained %s did not take the latch", line)
	}
	return m, cmd
}

// assertFlushHeld fails unless the staged row is still staged and no Exchange is running.
func assertFlushHeld(t *testing.T, m Model, when string) {
	t.Helper()
	if m.state != stateIdle || m.busy() {
		t.Errorf("%s: state = %v, want idle — no Exchange may open yet", when, m.state)
	}
	if len(m.pendingInterjections) != 1 {
		t.Errorf("%s: %d rows staged, want the row still waiting", when, len(m.pendingInterjections))
	}
}

// assertFlushed fails unless the staged row went out as the opening of a new Exchange.
func assertFlushed(t *testing.T, m Model, want string) {
	t.Helper()
	if !m.busy() {
		t.Fatalf("state = %v, want the retried flush running", m.state)
	}
	if len(m.pendingInterjections) != 0 || m.flushPending {
		t.Errorf("staged rows %d, retry pending %v; want both cleared by the flush", len(m.pendingInterjections), m.flushPending)
	}
	if got := lastPrompt(t, m); got.text != want {
		t.Errorf("last prompt = %q, want the flushed row %q", got.text, want)
	}
}

// A completion whose drained /stop-server took the latch does not flush into the server being
// stopped: the row stays staged under the latch's note, and the release alone does not send it —
// the heartbeat has ignored every failure under the latch, so it still calls the stopped server
// online. Only a beat folded after the release may let the retry go.
func TestCompletionFlushWaitsOutTheActuationLatch(t *testing.T) {
	t.Parallel()
	m, cmd := stopServerMidRun(t)

	assertFlushHeld(t, m, "under the latch")
	assertLastNote(t, m, "stop-server in flight")
	if !m.flushPending {
		t.Error("the turned-away flush left no retry pending")
	}

	m, _ = driveActuation(t, m, cmd)
	assertFlushHeld(t, m, "at the release")
	m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24}) // any fold that is not a beat
	assertFlushHeld(t, m, "at a fold after the release with no beat")
	if n := countNotes(m, "stop-server in flight"); n != 1 {
		t.Errorf("latch notes = %d, want the hold said once", n)
	}

	m = foldBeatMsg(t, m, twoModelBeat())

	assertFlushed(t, m, "and the docs")
}

// assertArmsOwedBeat fails unless a released latch fired the beat it owes on a FRESH chain: the Cmd is
// that chain's first beat, and a tick of the chain that ran under the latch schedules nothing — so a
// beat sent before the release is inert, and exactly one chain polls the server afterwards.
func assertArmsOwedBeat(t *testing.T, m Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("the release fired no beat; the one it owes would wait out a whole Interval")
	}
	if beat, ok := cmd().(beatMsg); !ok || beat.gen != m.hb.gen {
		t.Fatalf("the release's Cmd yielded %T, want the armed chain's first beat", cmd())
	}
	if _, stale := stepCmd(t, m, heartbeatTickMsg{gen: m.hb.gen - 1}); stale != nil {
		t.Error("a tick from the chain that ran under the latch still scheduled a beat — two chains poll one server")
	}
}

// A beat that was already on its way when a /stop-server or /unload-model released the latch does not
// pay the beat the release owes: it was sent while the server was still going down, so it says
// nothing about what the verb left behind. Only a beat sent after the release lets the staged row go.
func TestStaleBeatDoesNotPayTheOwedBeat(t *testing.T) {
	t.Parallel()
	for _, line := range []string{"/stop-server", "/unload-model"} {
		t.Run(strings.TrimPrefix(line, "/"), func(t *testing.T) {
			t.Parallel()
			m, cmd := launcherVerbMidRun(t, line)
			sentUnderTheLatch := m.hb.gen // the chain a beat in flight across the release was sent on

			m, cmd = driveActuation(t, m, cmd)
			assertArmsOwedBeat(t, m, cmd)
			m = step(t, m, beatMsg{gen: sentUnderTheLatch, beat: twoModelBeat()})

			assertFlushHeld(t, m, "after a beat sent before the release")
			if !m.actuation.beatOwed {
				t.Error("a beat sent before the release paid the owed beat")
			}

			m = foldBeatMsg(t, m, twoModelBeat())

			assertFlushed(t, m, "and the docs")
		})
	}
}

// A completion under a session load leaves the row staged with the load's note and flushes it at
// the fold that lands the record.
func TestCompletionFlushWaitsOutASessionLoad(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{}
	m, loadCmd := loadingBrowserModel(t, eng)
	startStubWorker(t, &m) // forced: an Exchange running when the load began
	m = stageRow(t, m, "and the docs")
	m = step(t, m, exchangeDoneMsg{})

	assertFlushHeld(t, m, "under the load")
	assertLastNote(t, m, sessionLoadHoldNote)

	m = step(t, m, untickedMsg(t, loadCmd)) // the record lands; its writes are not this test's subject

	assertFlushed(t, m, "and the docs")
}

// A background workflow that ends during a profile load does not wake the agent into the server
// being restarted: the wake waits, silently, for the release and a beat after it.
func TestWakeWaitsOutTheActuationLatch(t *testing.T) {
	t.Parallel()
	fake := newLauncher()
	m, _ := wireLauncher(t, fake)
	eng := m.eng.(*fakeEngine)
	eng.wakeFn = func(context.Context) (bool, error) { return true, nil }
	m, cmd := startLoad(t, m, "alpha")
	notes := len(noteTexts(m))

	m = finishBackground(t, m)
	if eng.wakes() != 0 || !m.wakePending {
		t.Fatalf("Wake calls %d, pending %v; want the wake held under the latch", eng.wakes(), m.wakePending)
	}

	m, _ = driveActuation(t, m, cmd)
	if eng.wakes() != 0 || !m.wakePending {
		t.Fatalf("Wake calls %d, pending %v; want the wake held until a beat folds after the release", eng.wakes(), m.wakePending)
	}
	for _, note := range noteTexts(m)[notes:] {
		if strings.Contains(note, "in flight") {
			t.Errorf("the held wake said %q, want it to wait silently", note)
		}
	}

	m = foldBeatMsg(t, m, twoModelBeat())

	if eng.wakes() != 1 || m.wakePending {
		t.Errorf("Wake calls %d, pending %v; want one wake once the beat folded", eng.wakes(), m.wakePending)
	}
}

// The beat a /stop-server or /unload-model release owes is believed when it fails: the server the
// human just stopped is not given the offline debounce's second chance, so the session crosses
// offline at once and the staged row stays staged instead of opening into it. A beat that lands
// afterwards brings the server back and lets the row go.
func TestFailedOwedBeatHoldsTheFlush(t *testing.T) {
	t.Parallel()
	for _, line := range []string{"/stop-server", "/unload-model"} {
		t.Run(strings.TrimPrefix(line, "/"), func(t *testing.T) {
			t.Parallel()
			m, cmd := launcherVerbMidRun(t, line)
			m, _ = driveActuation(t, m, cmd)

			m = foldBeatMsg(t, m, downBeat("connection refused"))

			assertFlushHeld(t, m, "after the owed beat failed")
			if !m.hb.offline {
				t.Error("the failed owed beat left the session online; the debounce outlived the verb")
			}
			if n := countNotes(m, "server offline"); n != 1 {
				t.Errorf("offline notes = %d, want the crossing narrated once", n)
			}

			m = foldBeatMsg(t, m, twoModelBeat())

			assertFlushed(t, m, "and the docs")
		})
	}
}

// The release's belief lasts until a beat lands: a server the verb left answering earns the ordinary
// debounce back, so one failure after that landing does not cross offline.
func TestLandedBeatAfterReleaseRestoresTheDebounce(t *testing.T) {
	t.Parallel()
	fake := newLauncher()
	fake.actResult = ActuationResult{Steps: []string{"Unloaded"}, Backend: "llamacpp", Addr: "localhost:1234"}
	m, _ := wireLauncher(t, fake)
	m, cmd := typeCommand(t, m, "/unload-model")
	m, _ = driveActuation(t, m, cmd)

	m = foldBeatMsg(t, m, twoModelBeat())
	m = foldBeatMsg(t, m, downBeat("discovery timed out"))

	if m.hb.offline {
		t.Error("one failure after a landed beat crossed offline; the release's belief outlived the landing")
	}
	m = foldBeatMsg(t, m, downBeat("discovery timed out"))
	if !m.hb.offline {
		t.Error("the ordinary debounce did not cross offline at its threshold")
	}
}

// A background workflow that ends while /stop-server runs does not wake the agent on the failed beat
// the release owes: the session has crossed offline, so the wake waits until a beat lands.
func TestFailedOwedBeatHoldsTheWake(t *testing.T) {
	t.Parallel()
	fake := newLauncher()
	fake.actResult = ActuationResult{ServerStopped: true, Backend: "llamacpp", Addr: "localhost:1234"}
	m, _ := wireLauncher(t, fake)
	eng := m.eng.(*fakeEngine)
	eng.wakeFn = func(context.Context) (bool, error) { return true, nil }
	m, cmd := typeCommand(t, m, "/stop-server")
	m = finishBackground(t, m)
	m, _ = driveActuation(t, m, cmd)

	m = foldBeatMsg(t, m, downBeat("connection refused"))

	if eng.wakes() != 0 || !m.wakePending {
		t.Fatalf("Wake calls %d, pending %v; want the wake held after the owed beat failed", eng.wakes(), m.wakePending)
	}

	m = foldBeatMsg(t, m, twoModelBeat())

	if eng.wakes() != 1 || m.wakePending {
		t.Errorf("Wake calls %d, pending %v; want one wake once a beat landed", eng.wakes(), m.wakePending)
	}
}

// Outside any launcher verb, a wake asks the same question a human send does: while the footer says
// offline it waits, silently, and it goes at the beat that brings the server back.
func TestWakeWaitsWhileOffline(t *testing.T) {
	t.Parallel()
	m, _ := wireLauncher(t, newLauncher())
	eng := m.eng.(*fakeEngine)
	eng.wakeFn = func(context.Context) (bool, error) { return true, nil }
	for range offlineFailureThreshold {
		m = foldBeatMsg(t, m, downBeat("connection refused"))
	}
	if !m.hb.offline {
		t.Fatal("precondition: the session did not cross offline")
	}
	notes := len(noteTexts(m))

	m = finishBackground(t, m)

	if eng.wakes() != 0 || !m.wakePending {
		t.Fatalf("Wake calls %d, pending %v; want the wake held while offline", eng.wakes(), m.wakePending)
	}
	for _, note := range noteTexts(m)[notes:] {
		if strings.Contains(note, "offline") {
			t.Errorf("the held wake said %q, want it to wait silently", note)
		}
	}

	m = foldBeatMsg(t, m, twoModelBeat())

	if eng.wakes() != 1 || m.wakePending {
		t.Errorf("Wake calls %d, pending %v; want one wake once the server came back", eng.wakes(), m.wakePending)
	}
}

// Rows a stop held wait for ⏎, never for a latch's release: a /stop-server run after the cancel
// releases its latch and a beat folds, and the held row is still held.
func TestHeldQueueAfterACancelIsNotRetriedAtTheLatchRelease(t *testing.T) {
	t.Parallel()
	fake := newLauncher()
	fake.actResult = ActuationResult{ServerStopped: true, Backend: "llamacpp", Addr: "localhost:1234"}
	m, _ := wireLauncher(t, fake)
	m = stageRow(t, m, "open the exchange")
	m = stageRow(t, m, "and the docs")
	m = step(t, m, cancelledMsg{})
	assertFlushHeld(t, m, "after the cancel")

	m, cmd := typeCommand(t, m, "/stop-server")
	m, _ = driveActuation(t, m, cmd)
	m = foldBeatMsg(t, m, twoModelBeat())

	assertFlushHeld(t, m, "after the latch released and a beat folded")
	if m.flushPending {
		t.Error("a held queue was marked for a retry")
	}
}

// ----------------------------------------------------------------------------
// The start-up restore (remember-model's boot half)
// ----------------------------------------------------------------------------

// restoreOpts wires the boot-restore seam onto the launcher harness, answering with one scripted
// decision and counting the asks — so a test can say both what the renderer DID with an answer and
// whether it asked at all.
func restoreOpts(fake *fakeLauncher, answer ProfileRestore, err error) (Options, *int) {
	opts := launcherOpts(fake)
	asks := 0
	launcherSeams(&opts).restore = func() (ProfileRestore, error) {
		asks++
		return answer, err
	}
	return opts, &asks
}

// firstRestore drives Init's batch and returns the restore check's answer, the way firstBeat returns
// the first observation: the check is one of the start-up Cmds, and running the batch is the only
// honest way to prove it went out at all.
func firstRestore(t *testing.T, cmd tea.Cmd) restoreMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("Init returned no Cmd — the restore check never went out")
	}
	switch msg := cmd().(type) {
	case restoreMsg:
		return msg
	case tea.BatchMsg:
		out := make(chan tea.Msg, len(msg))
		for _, c := range msg {
			go func() { out <- c() }()
		}
		deadline := time.After(pumpTimeout)
		for range msg {
			select {
			case landed := <-out:
				if restore, ok := landed.(restoreMsg); ok {
					return restore
				}
			case <-deadline:
				t.Fatal("no restoreMsg after Init — the restore check never landed")
			}
		}
		t.Fatal("Init's batch carried no restoreMsg — the restore check never went out")
	default:
		t.Fatalf("Init's Cmd yielded %T, want a batch carrying the restore check", msg)
	}
	return restoreMsg{}
}

// The whole point of the feature: the binary says nothing is serving and names the profile the entry
// remembers, and the session opens by loading it — through the ordinary latch, with the ordinary
// completion, exactly as if the human had picked it from `/model`.
func TestStartupRestoreActuatesTheRecordedProfile(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	opts, asks := restoreOpts(fake, ProfileRestore{Load: "beta"}, nil)
	m, _ := seededPicker(t, opts)
	notesBefore := len(noteTexts(m))

	answer := firstRestore(t, m.Init())
	if *asks != 1 {
		t.Fatalf("the seam was asked %d times; want exactly one check per session", *asks)
	}
	m, cmd := stepCmd(t, m, answer)

	if !m.actuation.inFlight || m.actuation.verb != verbLoad || m.actuation.profile != "beta" {
		t.Fatalf("latch = %+v; want a load of %q held exactly as a picked profile holds it",
			m.actuation, "beta")
	}
	m, _ = driveActuation(t, m, cmd)

	if got := fake.loaded(); !reflect.DeepEqual(got, []string{"beta"}) {
		t.Errorf("loads = %v; want the recorded profile loaded once", got)
	}
	want := []string{"profile beta loaded — waiting for the beat"}
	if got := noteTexts(m)[notesBefore:]; !reflect.DeepEqual(got, want) {
		t.Errorf("notes = %v; want the ordinary load completion %v — a restore is not a special case", got, want)
	}
}

// The three answers that load NOTHING, and what each one says. A refusal the binary worded reaches the
// human verbatim; a decision it made silently stays silent; and no answer at all ever reaches the load
// seam, because a restore that did not happen must not narrate one.
func TestStartupRestoreWithoutALoad(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		answer ProfileRestore
		err    error
		want   []string
	}{
		{
			name:   "the recorded profile is gone from the launcher's config",
			answer: ProfileRestore{Note: "launch-profile: beta is not in the launcher's config — nothing restored"},
			want:   []string{"launch-profile: beta is not in the launcher's config — nothing restored"},
		},
		{
			name:   "another profile is already serving",
			answer: ProfileRestore{Note: "the launcher is already serving alpha — beta not restored"},
			want:   []string{"the launcher is already serving alpha — beta not restored"},
		},
		{
			// The recorded profile is what runs, the toggle is off, no pointer was recorded: one zero
			// answer for every case where the start-up bind is already the whole story.
			name:   "nothing to do",
			answer: ProfileRestore{},
		},
		{
			name: "the launcher config could not be read",
			err:  errors.New("open /etc/llama-launcher/config.yaml: no such file or directory"),
			want: []string{"open /etc/llama-launcher/config.yaml: no such file or directory"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newLauncher()
			opts, _ := restoreOpts(fake, tc.answer, tc.err)
			m, _ := seededPicker(t, opts)
			notesBefore := len(noteTexts(m))

			m, cmd := stepCmd(t, m, firstRestore(t, m.Init()))

			if m.actuation.inFlight {
				t.Errorf("latch = %+v; want nothing actuated for an answer that named no profile", m.actuation)
			}
			if cmd != nil {
				t.Error("the fold returned a Cmd for an answer that named no profile")
			}
			if got := fake.loaded(); len(got) != 0 {
				t.Errorf("loads = %v; want the load seam never reached", got)
			}
			if got := noteTexts(m)[notesBefore:]; !reflect.DeepEqual(got, tc.want) {
				t.Errorf("notes = %v; want %v", got, tc.want)
			}
		})
	}
}

// A session that has moved on keeps what it chose. The answer was decided against the session as it
// stood when the check went out, so a latch already held — the human was faster than the discovery
// sweep — is never taken a second time: doing so would strand the verb holding it, and the restore is
// the half of the pair with nothing to lose.
func TestStartupRestoreYieldsToAVerbAlreadyInFlight(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	opts, _ := restoreOpts(fake, ProfileRestore{Load: "beta"}, nil)
	m, _ := seededPicker(t, opts)
	answer := firstRestore(t, m.Init())

	m, cmd := typeCommand(t, m, "/model alpha")
	if !m.actuation.inFlight || m.actuation.profile != "alpha" {
		t.Fatalf("latch = %+v; want the human's own load in flight", m.actuation)
	}
	held := m.actuation

	m, restoreCmd := stepCmd(t, m, answer)
	if m.actuation != held {
		t.Errorf("latch = %+v; want the human's load untouched %+v", m.actuation, held)
	}
	if restoreCmd != nil {
		t.Error("the restore fold returned a Cmd over a verb already in flight")
	}

	m, _ = driveActuation(t, m, cmd)
	if got := fake.loaded(); !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Errorf("loads = %v; want only the profile the human picked", got)
	}
}

// With the seam unwired nothing is asked and nothing is issued — the posture of every hand-built
// Options, and of every Driver that is not the interactive TUI. Init is the ONLY issuer, which is what
// makes the boot restore unreachable from a headless run by construction: that driver builds no Model.
func TestStartupRestoreIsSilentWhenUnwired(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	m, _ := seededPicker(t, launcherOpts(fake)) // launcher wired, the restore member nil

	if cmd := m.restoreCmd(); cmd != nil {
		t.Error("an unwired restore seam still issued a start-up Cmd")
	}
	if got := fake.loaded(); len(got) != 0 {
		t.Errorf("loads = %v; want nothing actuated with no restore seam wired", got)
	}
}

// Taking the latch for a profile load or a server verb only changes what the footer says and returns:
// the footer is painted every frame, and any re-lay is Update's tail's (doc.go, "an arm mutates").
func TestActuationStartsAreSettledByTheTail(t *testing.T) {
	t.Parallel()

	for _, line := range []string{"/model alpha", "/stop-server"} {
		t.Run(line, func(t *testing.T) {
			t.Parallel()

			m, _ := wireLauncher(t, newLauncher())
			m, _ = typeCommand(t, m, line)
			if !m.actuation.inFlight {
				t.Fatalf("precondition: %s did not take the latch", line)
			}
			assertSettled(t, m)
		})
	}
}
