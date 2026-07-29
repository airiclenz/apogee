package tui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
	actResult ActuationResult // what /unload and /stop report back
	actErr    error
	panics    bool // the seam falls over mid-verb; the latch must still release

	mu    sync.Mutex
	lists int
	loads []string
	acts  []string
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

// actuated reports the endpoints /unload or /stop were called with, in order.
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

// wireLauncher builds a ready, idle model with the heartbeat, the rebind AND the four launcher seams
// wired, one beat folded — the state a human is in when they type /model on a launcher host.
func wireLauncher(t *testing.T, fake *fakeLauncher) (Model, *fakeRebind) {
	t.Helper()
	opts := testOpts
	opts.LaunchProfiles = fake.list
	opts.LoadProfile = fake.load
	opts.UnloadServer = fake.act
	opts.StopServer = fake.act
	return seededPicker(t, opts)
}

// pumpTimeout is how long a test waits for one item off an actuation's channel. It is a deadlock
// guard, not a timing assumption: every fake answers immediately.
const pumpTimeout = 5 * time.Second

// msgWithin runs cmd on its own goroutine — the listen Cmd BLOCKS on the pump, exactly as it does in
// the program — and returns the Msg it yielded.
func msgWithin(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	out := make(chan tea.Msg, 1)
	go func() { out <- cmd() }()
	select {
	case msg := <-out:
		return msg
	case <-time.After(pumpTimeout):
		t.Fatal("the actuation pump produced nothing — the latch would never release")
		return nil
	}
}

// pumpItem takes the next item the actuation has for the Update loop. The FIRST Cmd is the batch of
// producer and listener (the producer's own Msg is nil, so the item is whichever member carries
// one); every re-arm after that is the listener alone.
func pumpItem(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("the actuation stopped pumping before the latch released")
	}
	msg := msgWithin(t, cmd)
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return msg // a re-arm: one listen Cmd, one item
	}
	out := make(chan tea.Msg, len(batch))
	for _, c := range batch {
		go func(c tea.Cmd) { out <- c() }(c)
	}
	for range batch {
		select {
		case landed := <-out:
			if landed != nil {
				return landed
			}
		case <-time.After(pumpTimeout):
			t.Fatal("the actuation batch produced nothing")
		}
	}
	t.Fatal("the actuation batch carried no pump item")
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
// refused with one note, and nothing is driven. The latch IS the per-address serialization the
// facade's contract demands of its caller, so a second lifecycle verb can never overlap the first.
func TestActuationLatchRefusesEveryMoveWhileHeld(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	m, rb := wireLauncher(t, fake)
	sw := &fakeSwitch{}
	m.opts.Servers, m.opts.SwitchServer = twoServers, sw.switchTo
	eng := m.eng.(*fakeEngine)
	m, _ = startLoad(t, m, "alpha")
	want := "profile load in flight — alpha"

	for _, line := range []string{"/model other-model", "/server remote", "/continue", "/compact", "hello"} {
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
	if len(sw.calls) != 0 || len(rb.calls) != 0 || len(eng.submitted) != 0 {
		t.Errorf("a refused verb still drove a seam: switches %v, rebinds %v, submits %d",
			sw.calls, rb.calls, len(eng.submitted))
	}
}

// Esc does not cancel an actuation: the facade offers no mid-flight cancel — its own cancel is
// /stop, available the moment the verb returns — so the key that stops a worker leaves the latch
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
	m2, _ := m.startServerActuation(verbStop, m.opts.StopServer)
	if got, want := m2.(Model).actuationBlockNote(), "stop in flight"; got != want {
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

	// The unbound cold start would say "connecting…"; the actuation is the more specific truth.
	unboundModel := m
	unboundModel.opts.Model = ""
	if got := unboundModel.upstreamSegments(); len(got) != 1 || got[0] != "loading alpha…" {
		t.Errorf("upstream segments = %v, want the actuation to outrank %q", got, connectingLabel)
	}

	m, _ = wireLauncher(t, newLauncher())
	stopping, _ := m.startServerActuation(verbStop, m.opts.StopServer)
	if got := stopping.(Model).upstreamSegments(); len(got) != 1 || got[0] != "stop…" {
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
// down mid-restart. Once the latch releases the ordinary debounce applies again, unchanged — the
// downtime after a /stop is real and crosses offline the ordinary way.
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
// news, and suppressing it would only delay the binding the load exists to produce.
func TestActuationDoesNotShadowLandedBeats(t *testing.T) {
	t.Parallel()

	m, rb := wireLauncher(t, newLauncher())
	m, _ = startLoad(t, m, "alpha")

	m = foldBeatMsg(t, m, upBeat("other-model", 16384))

	want := []rebindCall{{model: "other-model", window: 16384}}
	if !reflect.DeepEqual(rb.calls, want) {
		t.Errorf("rebind calls = %v, want %v — a landed beat still binds under the latch", rb.calls, want)
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
	beforeGen := m.hb.gen
	m, cmd := startLoad(t, m, "alpha")

	m, cmd = driveActuation(t, m, cmd)

	if m.actuation.inFlight {
		t.Fatal("the latch was not released by the completion")
	}
	if got := fake.loaded(); !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Errorf("loads = %v, want the picked profile activated once", got)
	}
	if m.opts.Endpoint != before.Endpoint || m.opts.Model != before.Model || m.hb.gen != beforeGen {
		t.Errorf("the session moved on a same-server load: endpoint %q, model %q, gen %d",
			m.opts.Endpoint, m.opts.Model, m.hb.gen)
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

// A profile that lives on another server is FOLLOWED: the composition root already re-pointed the
// wire, so the completion takes the /server fold whole — display adopted, model unbound, a fresh
// heartbeat generation, and the first beat of the new chain fired now.
func TestProfileLoadMovedTakesTheSwitchFold(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	fake.result = ProfileLoadResult{Moved: true, Switch: ServerSwitchResult{
		Endpoint:      "http://localhost:8081",
		HostAlias:     "beta",
		ContextWindow: remoteWindow,
	}}
	m, rb := wireLauncher(t, fake)
	oldGen := m.hb.gen
	m, cmd := startLoad(t, m, "beta")

	m, cmd = driveActuation(t, m, cmd)

	if m.actuation.inFlight {
		t.Fatal("the latch was not released by the completion")
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

// A failed load says the launcher's own words and releases the latch. Notices recorded before the
// failure still travel: a load that failed after warning about the config still warned.
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
	if cmd != nil {
		t.Error("a failed load fired a beat; nothing moved to observe")
	}
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

// The two verbs that act on the session's own server report one note per recorded step and then
// leave the display to the heartbeat. The steps travel even beside an error — how far a stop got
// before it failed is exactly what the human needs.
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

			started, cmd := m.startServerActuation(verbStop, m.opts.StopServer)
			m, cmd = driveActuation(t, started.(Model), cmd)

			if m.actuation.inFlight {
				t.Fatal("the latch was not released")
			}
			if cmd != nil {
				t.Error("an actuation on the session's own server fired a beat of its own")
			}
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
// /unload and /stop (ADR 0029 D3)
// ----------------------------------------------------------------------------

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
		{line: "/unload", verb: verbUnload, unloads: []string{testOpts.Endpoint}},
		{line: "/stop", verb: verbStop, stops: []string{testOpts.Endpoint}},
	} {
		t.Run(tc.line, func(t *testing.T) {
			t.Parallel()

			unloader, stopper := newLauncher(), newLauncher()
			m, _ := wireLauncher(t, newLauncher())
			m.opts.UnloadServer, m.opts.StopServer = unloader.act, stopper.act

			m, cmd := typeCommand(t, m, tc.line)

			if !m.actuation.inFlight || m.actuation.verb != tc.verb {
				t.Fatalf("actuation = {inFlight:%v verb:%q}, want the %s latch held",
					m.actuation.inFlight, m.actuation.verb, tc.verb)
			}
			for _, refused := range []string{"/model alpha", "/unload", "/stop"} {
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
			if cmd != nil {
				t.Error("an actuation on the session's own server fired a beat of its own")
			}
			if got := unloader.actuated(); !reflect.DeepEqual(got, tc.unloads) {
				t.Errorf("unload seam called with %v, want %v", got, tc.unloads)
			}
			if got := stopper.actuated(); !reflect.DeepEqual(got, tc.stops) {
				t.Errorf("stop seam called with %v, want %v", got, tc.stops)
			}
		})
	}
}

// /unload owes one sentence its steps cannot carry: whether freeing the model also took the server
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

			m, cmd := typeCommand(t, m, "/unload")
			m, _ = driveActuation(t, m, cmd)

			if got := noteTexts(m)[notesBefore:]; !reflect.DeepEqual(got, tc.want) {
				t.Errorf("notes = %v, want %v", got, tc.want)
			}
		})
	}
}

// /stop heads the launcher's steps with what it was stopping. The steps themselves are terse and
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

			m, cmd := typeCommand(t, m, "/stop")
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

	for _, line := range []string{"/unload", "/stop"} {
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
	for _, line := range []string{"/unload", "/stop"} {
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

// After a /stop the downtime is REAL, and the display says so the ordinary way: the latch released
// with the completion, so the beat shadow is gone and the same debounce that narrates a server dying
// on its own crosses this session offline.
func TestStopThenBeatFailuresCrossOffline(t *testing.T) {
	t.Parallel()

	fake := newLauncher()
	fake.actResult = ActuationResult{
		Steps: []string{"Sending stop signal"}, ServerStopped: true, Backend: "llamacpp", Addr: "localhost:1234",
	}
	m, _ := wireLauncher(t, fake)
	m, cmd := typeCommand(t, m, "/stop")
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
