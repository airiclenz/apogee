package main

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/testtime"
	"github.com/airiclenz/apogee/internal/tui"
)

// fallbackTestGrace is the grace window the resolver tests wait out: short, because what they
// claim is that a window runs and ends, not how long it is.
var fallbackTestGrace = testtime.Fixed(30 * time.Millisecond)

// fallbackAnswerBudget bounds a wait for one Resolve to come back.
var fallbackAnswerBudget = testtime.Budget(30 * time.Second)

// fakeSeat is the two network acts of one seat, scripted: whether the primary answers a beat, and
// which fallbacks answer theirs. It counts every act so a test can say how many grace windows and
// walks a set of reports cost.
type fakeSeat struct {
	isPrimaryUp atomic.Bool
	beats       atomic.Int32
	mu          sync.Mutex
	up          map[string]bool
	landed      []string
	// landGate, when set, holds every land until it closes; landEntered closes on the first land.
	landGate    chan struct{}
	landEntered chan struct{}
	enterOnce   sync.Once
}

func newFakeSeat(up ...string) *fakeSeat {
	seat := &fakeSeat{up: make(map[string]bool), landEntered: make(chan struct{})}
	for _, name := range up {
		seat.up[name] = true
	}
	return seat
}

func (s *fakeSeat) recovered(context.Context) bool {
	s.beats.Add(1)
	return s.isPrimaryUp.Load()
}

func (s *fakeSeat) land(ctx context.Context, name string) (apogee.FallbackAnswer, bool) {
	s.enterOnce.Do(func() { close(s.landEntered) })
	if s.landGate != nil {
		select {
		case <-s.landGate:
		case <-ctx.Done():
			return apogee.FallbackAnswer{}, false
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.landed = append(s.landed, name)
	if !s.up[name] {
		return apogee.FallbackAnswer{}, false
	}
	return apogee.FallbackAnswer{Server: name, Rebind: apogee.RebindSpec{Model: name + "-model"}}, true
}

func (s *fakeSeat) lands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.landed...)
}

func (s *fakeSeat) resolver(names ...string) *fallbackResolver {
	r := newFallbackResolver("primary", names, fallbackTestGrace, s.recovered, s.land)
	r.poll = testtime.Fixed(5 * time.Millisecond)
	return r
}

// resolveAsync runs one Resolve on its own goroutine and hands its answer back on the channel.
func resolveAsync(ctx context.Context, r *fallbackResolver, report apogee.FallbackReport) <-chan apogee.FallbackAnswer {
	out := make(chan apogee.FallbackAnswer, 1)
	go func() { out <- r.Resolve(ctx, report) }()
	return out
}

func awaitAnswer(t *testing.T, ch <-chan apogee.FallbackAnswer) apogee.FallbackAnswer {
	t.Helper()
	select {
	case answer := <-ch:
		return answer
	case <-time.After(fallbackAnswerBudget):
		t.Fatalf("Resolve did not come back within %s", fallbackAnswerBudget)
		return apogee.FallbackAnswer{}
	}
}

func primaryReport() apogee.FallbackReport {
	return apogee.FallbackReport{Server: "primary", Model: "primary-model"}
}

// TestFallbackResolverSharesOneRunAcrossCallers: concurrent and later reports of a fault on the
// same failed upstream cost one grace window and one walk, all read the same answer, and only the
// report that ran it is told First.
func TestFallbackResolverSharesOneRunAcrossCallers(t *testing.T) {
	seat := newFakeSeat("backup")
	seat.landGate = make(chan struct{})
	r := seat.resolver("backup")

	const concurrent = 4
	answers := make([]<-chan apogee.FallbackAnswer, 0, concurrent+1)
	answers = append(answers, resolveAsync(context.Background(), r, primaryReport()))
	<-seat.landEntered
	for range concurrent - 1 {
		answers = append(answers, resolveAsync(context.Background(), r, primaryReport()))
	}
	close(seat.landGate)
	got := make([]apogee.FallbackAnswer, 0, concurrent+1)
	for _, ch := range answers {
		got = append(got, awaitAnswer(t, ch))
	}
	got = append(got, r.Resolve(context.Background(), primaryReport()))

	firsts := 0
	for i, answer := range got {
		if answer.Verdict != apogee.FallbackSwitch || answer.Server != "backup" {
			t.Errorf("answer %d = %+v, want the switch to backup", i, answer)
		}
		if answer.First {
			firsts++
		}
	}
	if firsts != 1 {
		t.Errorf("%d answers carried First, want exactly 1", firsts)
	}
	if lands := seat.lands(); len(lands) != 1 {
		t.Errorf("the fallback was landed %d times (%v), want one walk", len(lands), lands)
	}
	if got[0].Waited < fallbackTestGrace {
		t.Errorf("Waited = %s, want at least the grace window %s", got[0].Waited, fallbackTestGrace)
	}
}

// TestFallbackResolverPrimaryBackWithinGraceIsRecovered: a primary that answers inside the window
// is recovered, no fallback is beaten, and the episode ends with its answer — the next failure of
// the same server is a fresh one with a window of its own.
func TestFallbackResolverPrimaryBackWithinGraceIsRecovered(t *testing.T) {
	seat := newFakeSeat("backup")
	seat.isPrimaryUp.Store(true)
	r := seat.resolver("backup")

	answer := r.Resolve(context.Background(), primaryReport())

	if answer.Verdict != apogee.FallbackRecovered || !answer.First {
		t.Errorf("answer = %+v, want a First recovered", answer)
	}
	if lands := seat.lands(); len(lands) != 0 {
		t.Errorf("fallbacks landed %v, want none — the primary came back", lands)
	}
	again := r.Resolve(context.Background(), primaryReport())
	if again.Verdict != apogee.FallbackRecovered || !again.First || seat.beats.Load() != 2 {
		t.Errorf("a later failure got %+v after %d beats, want a fresh First recovered on a second beat",
			again, seat.beats.Load())
	}
}

// TestFallbackResolverRecoveredSpentSkipsGrace: a fault after the Turn's one recovered resend goes
// straight to the walk — the primary is not beaten again, and nothing is waited.
func TestFallbackResolverRecoveredSpentSkipsGrace(t *testing.T) {
	seat := newFakeSeat("backup")
	seat.isPrimaryUp.Store(true)
	r := seat.resolver("backup")
	report := primaryReport()
	report.RecoveredSpent = true

	answer := r.Resolve(context.Background(), report)

	if answer.Verdict != apogee.FallbackSwitch || answer.Server != "backup" || answer.Waited != 0 {
		t.Errorf("answer = %+v, want a switch to backup with no wait", answer)
	}
	if beats := seat.beats.Load(); beats != 0 {
		t.Errorf("the primary was beaten %d times, want 0 — the grace window is skipped", beats)
	}
}

// TestFallbackResolverWalksForwardNeverBack: the first walk skips a fallback that is down; a fault
// on the fallback it moved to walks the rest of the list with no grace window, and one past the end
// of the list abandons without walking back to anything already tried.
func TestFallbackResolverWalksForwardNeverBack(t *testing.T) {
	seat := newFakeSeat("second", "third")
	r := seat.resolver("first", "second", "third")

	moved := r.Resolve(context.Background(), primaryReport())
	if moved.Verdict != apogee.FallbackSwitch || moved.Server != "second" {
		t.Fatalf("first answer = %+v, want a switch to second", moved)
	}
	beatsAfterGrace := seat.beats.Load()

	onward := r.Resolve(context.Background(), apogee.FallbackReport{Server: "second"})
	if onward.Verdict != apogee.FallbackSwitch || onward.Server != "third" || onward.Waited != 0 {
		t.Errorf("a fault on second got %+v, want a switch to third with no wait", onward)
	}
	exhausted := r.Resolve(context.Background(), apogee.FallbackReport{Server: "third"})
	if exhausted.Verdict != apogee.FallbackNone {
		t.Errorf("a fault on the last fallback got %+v, want none", exhausted)
	}
	if beats := seat.beats.Load(); beats != beatsAfterGrace {
		t.Errorf("the primary was beaten again after the seat fell over (%d → %d)", beatsAfterGrace, beats)
	}
	if lands, want := seat.lands(), []string{"first", "second", "third"}; !slices.Equal(lands, want) {
		t.Errorf("landed %v, want %v — forward only", lands, want)
	}
	late := r.Resolve(context.Background(), primaryReport())
	if late.Verdict != apogee.FallbackSwitch || late.Server != "second" || late.First {
		t.Errorf("a late report on the primary got %+v, want the cached switch to second, not First", late)
	}
}

// TestFallbackResolverAllDownAbandons: with every fallback down the answer is none, and the list is
// not spent — a later failure walks it again.
func TestFallbackResolverAllDownAbandons(t *testing.T) {
	seat := newFakeSeat()
	r := seat.resolver("backup")

	if answer := r.Resolve(context.Background(), primaryReport()); answer.Verdict != apogee.FallbackNone {
		t.Errorf("answer = %+v, want none", answer)
	}
	r.Resolve(context.Background(), primaryReport())
	if lands := seat.lands(); len(lands) != 2 {
		t.Errorf("landed %v, want backup tried once per failure", lands)
	}
}

// TestFallbackResolverRunOutlivesTheFirstCallersCancel: the caller that started the run is
// cancelled mid-run and comes back with nothing, a waiter on its own live ctx still gets the run's
// answer, and a waiter whose own ctx is cancelled stops waiting.
func TestFallbackResolverRunOutlivesTheFirstCallersCancel(t *testing.T) {
	seat := newFakeSeat("backup")
	seat.landGate = make(chan struct{})
	r := seat.resolver("backup")
	report := primaryReport()
	report.RecoveredSpent = true

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	first := resolveAsync(firstCtx, r, report)
	<-seat.landEntered
	cancelFirst()
	if answer := awaitAnswer(t, first); answer.Verdict != apogee.FallbackNone || answer.First {
		t.Errorf("the cancelled first caller got %+v, want the zero answer", answer)
	}

	gone, cancelGone := context.WithCancel(context.Background())
	cancelGone()
	if answer := r.Resolve(gone, report); answer.Verdict != apogee.FallbackNone {
		t.Errorf("a waiter whose own ctx is cancelled got %+v, want the zero answer", answer)
	}

	waiter := resolveAsync(context.Background(), r, report)
	close(seat.landGate)
	answer := awaitAnswer(t, waiter)
	if answer.Verdict != apogee.FallbackSwitch || answer.Server != "backup" || answer.First {
		t.Errorf("the waiter got %+v, want the run's switch to backup, not First", answer)
	}
	if lands := seat.lands(); len(lands) != 1 {
		t.Errorf("landed %v, want the one run", lands)
	}
}

// TestFallbackLanderBindsTheFallbackEntry: a move onto a live fallback states that entry's server
// name, endpoint, its own stream-idle window over the root's, its window pin, and the model its
// beat observed — never the bare binding's idle cut of 0 and empty name.
func TestFallbackLanderBindsTheFallbackEntry(t *testing.T) {
	backup := stubllm.New(t, stubllm.Script{Model: "backup-model", Turns: []stubllm.Turn{{Text: "unused"}}})
	down := stubllm.New(t, stubllm.Script{Model: "down-model", Turns: []stubllm.Turn{{Text: "unused"}}})
	down.Close()
	opts := config.Options{
		StreamIdleTimeout: testtime.Fixed(5 * time.Minute),
		Servers: []config.ServerEntry{
			{Name: "primary", Endpoint: "http://127.0.0.1:1", Fallback: config.NewFallbackList("backup")},
			{Name: "backup", Endpoint: backup.URL, Description: "the spare box", StreamIdleTimeout: "45s",
				ContextWindow: 16384},
			{Name: "down", Endpoint: down.URL},
		},
	}
	lander := fallbackLander{
		opts:  opts,
		roots: stateRoots{config: t.TempDir(), probe: t.TempDir()},
		key:   func(config.ServerEntry) (string, error) { return "", nil },
		beat: func(ctx context.Context, dial upstreamBinding) heartbeat.Beat {
			return dial.Monitor().Beat(ctx)
		},
	}

	answer, landed := lander.land(context.Background(), "backup")

	if !landed {
		t.Fatal("the live backup did not land")
	}
	up := answer.Upstream
	if up.ServerName != "backup" || up.ServerDescription != "the spare box" || up.Endpoint != backup.URL {
		t.Errorf("upstream = %+v, want the backup entry's name, description and endpoint", up)
	}
	if up.StreamIdleTimeout == nil || *up.StreamIdleTimeout != testtime.Fixed(45*time.Second) {
		t.Errorf("stream idle = %v, want the entry's own 45s stated", up.StreamIdleTimeout)
	}
	if up.MaxContextTokens != 16384 || up.ServerEphemeral {
		t.Errorf("upstream window %d, ephemeral %v; want the entry's pin, a configured entry",
			up.MaxContextTokens, up.ServerEphemeral)
	}
	if answer.Rebind.Model != "backup-model" || answer.Rebind.MaxContextTokens != 16384 || answer.Server != "backup" {
		t.Errorf("rebind = %+v (server %q), want the beat's model on the pinned window", answer.Rebind, answer.Server)
	}
	if _, landed := lander.land(context.Background(), "down"); landed {
		t.Error("a fallback that does not answer landed")
	}
	if _, landed := lander.land(context.Background(), "nowhere"); landed {
		t.Error("a name no entry carries landed")
	}
}

// TestFallbackLanderRoutedBindsAsTheDelegationTarget: under root pins of 131072 / 65536 / 0.4 / 5m
// and a fallback entry that pins nothing, a routed landing's effective binding is the target
// resolveDelegationTarget latches for a spawn on that fallback — its observed window, no working
// bound, the parent's reserve share where the entry states none, the root idle window — and a beat
// that names no window binds the parent's, never 0. The session seat's landing still ranks the
// entry over the root pins.
func TestFallbackLanderRoutedBindsAsTheDelegationTarget(t *testing.T) {
	t.Parallel()
	idleRoot := testtime.Fixed(5 * time.Minute)
	parent := delegationParent{window: 200000, reserve: 0.25}
	cases := []struct {
		name         string
		isRouted     bool
		beatWindow   int
		wantWindow   int
		wantWorking  int
		wantReserve  float64
		isTargetPair bool
	}{
		{name: "routed takes the target's binding", isRouted: true, beatWindow: 32768,
			wantWindow: 32768, wantWorking: 0, wantReserve: parent.reserve, isTargetPair: true},
		{name: "routed with no observed window takes the parent's", isRouted: true, beatWindow: 0,
			wantWindow: parent.window, wantWorking: 0, wantReserve: parent.reserve, isTargetPair: true},
		{name: "session seat resolves over the root pins", isRouted: false, beatWindow: 32768,
			wantWindow: 131072, wantWorking: 65536, wantReserve: 0.4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			backup := config.ServerEntry{Name: "backup", Endpoint: "http://127.0.0.1:4444"}
			beat := heartbeat.Beat{Reachable: true, ActiveModel: "backup-model", ContextWindow: tc.beatWindow}
			lander := fallbackLander{
				opts: config.Options{ContextWindow: 131072, WorkingWindow: 65536, ResponseReserve: 0.4,
					StreamIdleTimeout: idleRoot, Servers: []config.ServerEntry{backup}},
				roots:  stateRoots{config: t.TempDir(), probe: t.TempDir()},
				key:    func(config.ServerEntry) (string, error) { return "", nil },
				beat:   func(context.Context, upstreamBinding) heartbeat.Beat { return beat },
				routed: tc.isRouted,
				parent: parent,
			}

			answer, landed := lander.land(context.Background(), "backup")

			if !landed {
				t.Fatal("the live backup did not land")
			}
			reserve := answer.Rebind.ResponseReserveFraction
			if reserve == nil || *reserve != tc.wantReserve {
				t.Errorf("reserve = %v, want %v stated", reserve, tc.wantReserve)
			}
			if answer.Rebind.MaxContextTokens != tc.wantWindow || answer.Upstream.WorkingWindow != tc.wantWorking {
				t.Errorf("window %d, working %d; want %d, %d", answer.Rebind.MaxContextTokens,
					answer.Upstream.WorkingWindow, tc.wantWindow, tc.wantWorking)
			}
			idle := answer.Upstream.StreamIdleTimeout
			if idle == nil || *idle != idleRoot {
				t.Errorf("stream idle = %v, want the root's %v stated", idle, idleRoot)
			}
			if !tc.isTargetPair {
				return
			}
			target := resolveDelegationTarget(backup, "", beat, nil, idleRoot)
			targetWindow := target.ContextWindow
			if targetWindow <= 0 {
				targetWindow = parent.window
			}
			if answer.Rebind.MaxContextTokens != targetWindow || answer.Upstream.WorkingWindow != target.WorkingWindow ||
				*idle != *target.StreamIdleTimeout {
				t.Errorf("rehome binds window %d, working %d, idle %v; a spawn's target binds %d, %d, %v",
					answer.Rebind.MaxContextTokens, answer.Upstream.WorkingWindow, *idle,
					targetWindow, target.WorkingWindow, *target.StreamIdleTimeout)
			}
		})
	}
}

// TestHeadlessRoutedFallbackTakesNoRootPins: an unattended run's Sub-agents seat lands on its
// fallback over the window and reserve share the run bound, never under the run's root
// `context-window:` / `working-window:` / `response-reserve:` pins — the fallback here pins nothing
// and its beat names no window, so the run's own window is the one a routed child keeps.
func TestHeadlessRoutedFallbackTakesNoRootPins(t *testing.T) {
	t.Parallel()
	spare := stubllm.New(t, stubllm.Script{Model: "spare-model", Turns: []stubllm.Turn{{Text: "unused"}}})
	grunt := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:1", Model: "grunt-model",
		Fallback: config.NewFallbackList("spare")}
	in := firingInputs{
		opts: config.Options{ContextWindow: 131072, WorkingWindow: 65536, ResponseReserve: 0.4,
			Servers: []config.ServerEntry{grunt, {Name: "spare", Endpoint: spare.URL}}},
		roots: firingRoots(t),
	}
	parent := delegationParent{window: 200000, reserve: 0.25}
	resolver := newFiringSeatFallback(in, config.NewKeyResolver(""), newSubAgentServer(grunt), "", parent)
	if resolver == nil {
		t.Fatal("a Sub-agent server naming a fallback got no resolver")
	}

	answer, landed := resolver.land(context.Background(), "spare")

	if !landed {
		t.Fatal("the live spare did not land")
	}
	if answer.Rebind.MaxContextTokens != parent.window || answer.Upstream.WorkingWindow != 0 {
		t.Errorf("window %d, working %d; want the run's %d and no working bound",
			answer.Rebind.MaxContextTokens, answer.Upstream.WorkingWindow, parent.window)
	}
	reserve := answer.Rebind.ResponseReserveFraction
	if reserve == nil || *reserve != parent.reserve {
		t.Errorf("reserve = %v; want the run's %v stated", reserve, parent.reserve)
	}
}

// TestTUIFallbackRoutedLanderTakesTheSessionsWindowAndShare: the Sub-agents seat's lander in a
// session lands routed, over the window the session is bound to — its observed one where nothing
// pins it — and the reserve share the session's bound entry resolves.
func TestTUIFallbackRoutedLanderTakesTheSessionsWindowAndShare(t *testing.T) {
	t.Parallel()
	seat := newTUIFallbackSeat(t)
	primary, err := findServer(seat.live.serverList(), "primary")
	if err != nil {
		t.Fatalf("find primary: %v", err)
	}
	primary.ResponseReserve = 0.3
	seat.live.followEntry(primary)
	seat.live.observe(65536, "")

	lander := seat.mover.fallback.routedLander()

	want := delegationParent{window: 65536, reserve: 0.3}
	if !lander.routed || lander.parent != want {
		t.Errorf("routed lander = routed %v over %+v; want routed over %+v", lander.routed, lander.parent, want)
	}
}

// tuiFallbackSeat is one TUI session seat on primary, falling back to a live backup: the live
// settings bound to primary, the holder dialled at a port nothing answers, and the session's
// resolver over both, with the mover that rearms and follows it.
type tuiFallbackSeat struct {
	backup  *stubllm.Server
	live    *liveSettings
	holder  *upstreamHolder
	stamper *fakeStamper
	caps    *parallelAgentsSpy
	mover   sessionMover
}

func newTUIFallbackSeat(t *testing.T) tuiFallbackSeat {
	t.Helper()
	backup := stubllm.New(t, stubllm.Script{Model: "backup-model", Turns: []stubllm.Turn{{Text: "unused"}}})
	primary := config.ServerEntry{Name: "primary", Endpoint: "http://127.0.0.1:1", Model: "primary-model",
		Fallback: config.NewFallbackList("backup"), FallbackAfter: fallbackTestGrace.String()}
	opts := config.Options{Servers: []config.ServerEntry{
		primary,
		{Name: "backup", Endpoint: backup.URL, ContextWindow: 16384},
		{Name: "plain", Endpoint: "http://127.0.0.1:2"},
	}}
	live := newLiveSettings(opts, config.ProjectOffer{})
	live.followEntry(primary)
	holder := newUpstreamHolder()
	dial := bindingOfEntry(primary, "")
	holder.Bind(dial, dial.Monitor())
	keys := config.NewKeyResolver("")
	stamper, caps := &fakeStamper{}, &parallelAgentsSpy{}
	fallback := newSessionFallback(live, holder, keys, stateRoots{config: t.TempDir(), probe: t.TempDir()})
	mover := sessionMover{agent: &fakeSwitcher{}, holder: holder, host: stamper, live: live, keys: keys,
		caps: newParallelAgentsCap(caps), fallback: fallback}
	return tuiFallbackSeat{backup: backup, live: live, holder: holder, stamper: stamper, caps: caps, mover: mover}
}

// TestTUIFallbackResolverAnswersForTheBoundEntry: the session's one resolver answers a fault on the
// entry the session is bound to — primary's grace runs out and the walk lands on backup — and after
// a human move onto an entry with no `fallback:` it answers a fault there with "none" at once and
// has forgotten the landing on backup.
func TestTUIFallbackResolverAnswersForTheBoundEntry(t *testing.T) {
	t.Parallel()
	seat := newTUIFallbackSeat(t)
	ctx := context.Background()

	answer := seat.mover.fallback.Resolve(ctx, apogee.FallbackReport{Server: "primary", Model: "primary-model"})

	if answer.Verdict != apogee.FallbackSwitch || answer.Server != "backup" || answer.Rebind.Model != "backup-model" {
		t.Fatalf("answer = %+v, want a switch onto backup's model", answer)
	}
	if _, ok := seat.mover.fallback.landing("backup"); !ok {
		t.Fatal("the landing on backup was not kept for the follow")
	}

	plain, err := findServer(seat.live.serverList(), "plain")
	if err != nil {
		t.Fatalf("find plain: %v", err)
	}
	if _, err := seat.mover.move(plain); err != nil {
		t.Fatalf("move onto plain: %v", err)
	}
	answer = seat.mover.fallback.Resolve(ctx, apogee.FallbackReport{Server: "plain"})

	if answer.Verdict != apogee.FallbackNone || answer.Waited != 0 {
		t.Errorf("answer on plain = %+v, want none at once — plain names no fallback", answer)
	}
	if _, ok := seat.mover.fallback.landing("backup"); ok {
		t.Error("the landing on backup survived a human move")
	}
}

// TestTUIFallbackFalloverRunsTheHeartbeatTail: following a committed switch onto backup moves the
// holder's Monitor and binding onto backup with its model bound, stamps that model on the record,
// latches backup as the bound entry, re-follows the fan-out width and records backup's observed
// window — and answers the display the engine's window. A server the seat never landed on is
// refused and moves nothing.
func TestTUIFallbackFalloverRunsTheHeartbeatTail(t *testing.T) {
	t.Parallel()
	seat := newTUIFallbackSeat(t)
	ctx := context.Background()
	if _, err := seat.mover.fallover("backup"); err == nil {
		t.Fatal("a follow before any landing was accepted")
	}
	if got := seat.holder.Binding().Endpoint; got != "http://127.0.0.1:1" {
		t.Fatalf("the refused follow moved the holder to %q", got)
	}
	answer := seat.mover.fallback.Resolve(ctx, apogee.FallbackReport{Server: "primary", Model: "primary-model"})
	if answer.Verdict != apogee.FallbackSwitch {
		t.Fatalf("answer = %+v, want a switch", answer)
	}
	widths := len(seat.caps.widths)

	result, err := seat.mover.fallover("backup")

	if err != nil {
		t.Fatalf("fallover: %v", err)
	}
	if got := seat.holder.Binding(); got.Endpoint != seat.backup.URL || got.Model != "backup-model" {
		t.Errorf("holder binding = %+v, want backup with its model bound", got)
	}
	if got := seat.holder.Beat(ctx); !got.Reachable {
		t.Errorf("the holder's beat = %+v, want it observing the live backup", got)
	}
	if got := seat.stamper.models; len(got) == 0 || got[len(got)-1] != "backup-model" {
		t.Errorf("record models = %v, want backup-model stamped last", got)
	}
	if entry, ok := seat.live.boundEntry(); !ok || entry.Name != "backup" {
		t.Errorf("bound entry = %q (%v), want backup", entry.Name, ok)
	}
	if len(seat.caps.widths) == widths {
		t.Error("the fan-out width was not re-followed onto backup")
	}
	want := tuiFallbackWant(seat.backup.URL)
	if result.Endpoint != want.Endpoint || result.HostAlias != want.HostAlias || result.Model != want.Model ||
		result.ContextWindow != want.ContextWindow {
		t.Errorf("result = %+v, want %+v", result, want)
	}
	if result.ObservedWindow != seat.live.observed() {
		t.Errorf("observed window %d answered, %d recorded; want the one beat's reading in both",
			result.ObservedWindow, seat.live.observed())
	}
}

// TestTUIFallbackLandsWithOptionsEditedAfterTheResolverWasBuilt: a walk lands on the configuration
// as it stands at the walk, not as it stood when the seat's resolver was built — a `servers:` edit
// that re-pins backup's window and a `/settings` edit that moves the root working window, both made
// after the build, are what the answer states and what the follow reports.
func TestTUIFallbackLandsWithOptionsEditedAfterTheResolverWasBuilt(t *testing.T) {
	t.Parallel()
	seat := newTUIFallbackSeat(t)
	ctx := context.Background()
	if seat.mover.fallback.resolver() == nil {
		t.Fatal("no resolver was built for primary")
	}
	servers := slices.Clone(seat.live.serverList())
	for i := range servers {
		if servers[i].Name == "backup" {
			servers[i].ContextWindow = 32768
		}
	}
	seat.live.setServers(servers)
	seat.live.update(func(opts *config.Options) { opts.WorkingWindow = 4096 })

	answer := seat.mover.fallback.Resolve(ctx, apogee.FallbackReport{Server: "primary", Model: "primary-model"})

	if answer.Verdict != apogee.FallbackSwitch || answer.Server != "backup" {
		t.Fatalf("answer = %+v, want a switch onto backup", answer)
	}
	if answer.Upstream.MaxContextTokens != 32768 || answer.Rebind.MaxContextTokens != 32768 {
		t.Errorf("upstream window %d, rebind window %d; want the edited pin 32768 in both",
			answer.Upstream.MaxContextTokens, answer.Rebind.MaxContextTokens)
	}
	if answer.Upstream.WorkingWindow != 4096 {
		t.Errorf("working window = %d, want the edited root 4096", answer.Upstream.WorkingWindow)
	}
	result, err := seat.mover.fallover("backup")
	if err != nil {
		t.Fatalf("fallover: %v", err)
	}
	if result.ContextWindow != 32768 {
		t.Errorf("follow reports window %d, want the edited pin 32768", result.ContextWindow)
	}
}

// tuiFallbackWant is what the display adopts after the follow onto the backup at endpoint.
func tuiFallbackWant(endpoint string) tui.ServerFallbackResult {
	return tui.ServerFallbackResult{Endpoint: endpoint, HostAlias: "backup", Model: "backup-model", ContextWindow: 16384}
}
