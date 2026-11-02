package main

// Characterisation pins of how each engine door resolves a server entry: the session's startup
// bind (A), its beat-driven rebind (B) and its `/server` move (B'), an unattended Firing (C) and
// one raised in-session (C'), the session seat's Server fallback landing (D), a routed spawn's
// Delegation target (E), the Sub-agents seat's Server fallback landing (F), and the context probe
// (G). ADR 0083 part 4 keeps one hand-ranked resolution per door rather than a shared entry→engine
// function, so these tables are what makes a door's drift fail a test: every door runs over the
// same scenario matrix, and each door's want
// spells its ranks out by hand — never through the config.Resolve* helpers the doors call — so a
// door that changes which scope wins changes a row here. Where two doors differ on purpose, the
// want says why in one line.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/session"
)

// The scenario values, distinct per scope so a resolved number says which scope it came from.
const (
	rankName         = "rack"
	rankDescription  = "the rack box in the hall"
	rankEndpoint     = "http://127.0.0.1:1111"
	rankKey          = "sk-rank"
	rankEntryModel   = "entry-model"
	rankBeatModel    = "beat-model"
	rankEntryWindow  = 40000
	rankRootWindow   = 20000
	rankBeatWindow   = 80000
	rankEntryWorking = 30000
	rankRootWorking  = 15000
	rankEntryReserve = 0.25
	rankRootReserve  = 0.4
	rankEntryOutput  = 4096
	rankEntryIdle    = 90 * time.Second
	rankRootIdle     = 45 * time.Second
	rankEntryWidth   = 3
	rankBeatSlots    = 6
	rankKeyedFloor   = 4
	rankUnkeyedFloor = 1
	rankForced       = domain.EffortDialectReasoning
	rankObserved     = domain.EffortDialectOpenAI
	rankRecordID     = "2026-11-02T10-00-00-rank"
)

// rankScenario is one row of the matrix every door is run over.
type rankScenario struct {
	// pinned: the entry pins every rank it can — model, window, working window, reserve share,
	// reply cap, stream-idle window and fan-out width.
	pinned bool
	// root: the top-level keys (window, working window, reserve share, stream-idle window) are set.
	root bool
	// beat: the server's observation names a window, a slot count, a dialect and a model.
	beat bool
	// forced: the entry forces an `effort-dialect:`.
	forced bool
	// keyed: the entry carries a key.
	keyed bool
}

func (s rankScenario) String() string {
	return fmt.Sprintf("entry=%s,root=%s,beat=%s,dialect=%s,key=%s",
		choose(s.pinned, "pinned", "bare"), choose(s.root, "set", "unset"),
		choose(s.beat, "present", "absent"), choose(s.forced, "forced", "free"),
		choose(s.keyed, "keyed", "unkeyed"))
}

func choose(on bool, yes, no string) string {
	if on {
		return yes
	}
	return no
}

// rankScenarios is the whole matrix: every combination of the five axes.
func rankScenarios() []rankScenario {
	var rows []rankScenario
	for bits := 0; bits < 1<<5; bits++ {
		rows = append(rows, rankScenario{
			pinned: bits&1 != 0,
			root:   bits&2 != 0,
			beat:   bits&4 != 0,
			forced: bits&8 != 0,
			keyed:  bits&16 != 0,
		})
	}
	return rows
}

// entry is the `servers:` entry the scenario binds. It is always ephemeral and always described,
// so a door that drops either shows it.
func (s rankScenario) entry() config.ServerEntry {
	e := config.ServerEntry{
		Name:        rankName,
		Description: rankDescription,
		Endpoint:    rankEndpoint,
		Ephemeral:   true,
	}
	if s.pinned {
		e.Model = rankEntryModel
		e.ContextWindow = rankEntryWindow
		e.WorkingWindow = rankEntryWorking
		e.ResponseReserve = rankEntryReserve
		e.MaxOutputTokens = rankEntryOutput
		e.StreamIdleTimeout = rankEntryIdle.String()
		e.ParallelAgents = rankEntryWidth
	}
	if s.forced {
		e.EffortDialect = string(rankForced)
	}
	if s.keyed {
		e.APIKey = rankKey
	}
	return e
}

// key is the token the scenario's entry resolves to.
func (s rankScenario) key() string {
	return choose(s.keyed, rankKey, "")
}

// options is the run's configuration: the top-level keys when the scenario sets them, and the
// entry as the one the run starts on.
func (s rankScenario) options() config.Options {
	opts := config.Options{
		Endpoint:     rankEndpoint,
		HostAlias:    rankName,
		StartupEntry: s.entry(),
		Servers:      []config.ServerEntry{s.entry()},
	}
	if s.root {
		opts.ContextWindow = rankRootWindow
		opts.WorkingWindow = rankRootWorking
		opts.ResponseReserve = rankRootReserve
		opts.StreamIdleTimeout = rankRootIdle
	}
	return opts
}

// observation is the server's beat: answered either way, naming its facts only when present.
func (s rankScenario) observation() heartbeat.Beat {
	beat := heartbeat.Beat{Reachable: true, Answered: true}
	if s.beat {
		beat.ActiveModel = rankBeatModel
		beat.ContextWindow = rankBeatWindow
		beat.TotalSlots = rankBeatSlots
		beat.EffortSupport = domain.EffortSupport{Dialect: rankObserved}
	}
	return beat
}

// The ranks spelled by hand: the entry's pin, else the top-level key, else the honest zero.

func (s rankScenario) pinnedWindow() int {
	switch {
	case s.pinned:
		return rankEntryWindow
	case s.root:
		return rankRootWindow
	}
	return 0
}

// observedWindow is the pin, else the window the beat saw.
func (s rankScenario) observedWindow() int {
	if w := s.pinnedWindow(); w > 0 || !s.beat {
		return w
	}
	return rankBeatWindow
}

func (s rankScenario) working() int {
	switch {
	case s.pinned:
		return rankEntryWorking
	case s.root:
		return rankRootWorking
	}
	return 0
}

func (s rankScenario) reserve() float64 {
	switch {
	case s.pinned:
		return rankEntryReserve
	case s.root:
		return rankRootReserve
	}
	return 0
}

func (s rankScenario) maxOutput() int {
	if s.pinned {
		return rankEntryOutput
	}
	return 0
}

func (s rankScenario) streamIdle() time.Duration {
	switch {
	case s.pinned:
		return rankEntryIdle
	case s.root:
		return rankRootIdle
	}
	return 0
}

// width is the entry's pin, else the slots a beat saw when the door reads one, else the floor a
// keyed entry and an unkeyed one each take.
func (s rankScenario) width(readsBeat bool) int {
	switch {
	case s.pinned:
		return rankEntryWidth
	case readsBeat && s.beat:
		return rankBeatSlots
	case s.keyed:
		return rankKeyedFloor
	}
	return rankUnkeyedFloor
}

func (s rankScenario) forcedDialect() domain.EffortDialect {
	if s.forced {
		return rankForced
	}
	return domain.EffortDialectNone
}

func (s rankScenario) entryModel() string {
	return choose(s.pinned, rankEntryModel, "")
}

// entryRanks is what one door resolved out of one scenario: every rank a server entry decides, and
// the entry's identity. A field a door does not state stays zero, so "not stated" is pinned as
// precisely as a value; refused says the door would not compose the run at all.
type entryRanks struct {
	refused     bool
	window      int
	working     int
	reserve     float64
	maxOutput   int
	streamIdle  time.Duration
	width       int
	dialect     domain.EffortDialect
	model       string
	name        string
	description string
	ephemeral   bool
	// namedOn is the endpoint the run's namer names an unrouted child on; only the fallback doors
	// state it, as only a fallover can leave it on a server the run no longer talks to.
	namedOn string
}

// ranksOfConfig reads the ranks off the Config a door constructs the engine from.
func ranksOfConfig(cfg apogee.Config) entryRanks {
	return entryRanks{
		window:      cfg.Context.MaxContextTokens,
		working:     cfg.Context.WorkingWindow,
		reserve:     cfg.Context.ResponseReserveFraction,
		maxOutput:   cfg.Context.MaxOutputTokens,
		streamIdle:  cfg.StreamIdleTimeout,
		width:       cfg.ParallelAgents,
		dialect:     cfg.EffortDialect,
		model:       cfg.Model,
		name:        cfg.ServerName,
		description: cfg.ServerDescription,
		ephemeral:   cfg.ServerEphemeral,
	}
}

// rankDoor is one engine door: how it is driven over a scenario, and what it must resolve.
type rankDoor struct {
	name    string
	resolve func(*testing.T, rankScenario) entryRanks
	want    func(rankScenario) entryRanks
}

func runRankDoors(t *testing.T, doors []rankDoor) {
	t.Helper()
	for _, door := range doors {
		t.Run(door.name, func(t *testing.T) {
			t.Parallel()
			for _, s := range rankScenarios() {
				t.Run(s.String(), func(t *testing.T) {
					t.Parallel()

					got := door.resolve(t, s)

					if want := door.want(s); got != want {
						t.Errorf("door %s resolved the entry as\n got %+v\nwant %+v", door.name, got, want)
					}
				})
			}
		})
	}
}

// wantBoundEntry is a door that resolves the entry alone, before any beat: every rank the entry
// pins over the top-level key, the width at the entry's pin or its floor, the dialect the entry
// forces, the entry's model, and the entry's identity whole.
func wantBoundEntry(s rankScenario) entryRanks {
	return entryRanks{
		window:      s.pinnedWindow(),
		working:     s.working(),
		reserve:     s.reserve(),
		maxOutput:   s.maxOutput(),
		streamIdle:  s.streamIdle(),
		width:       s.width(false),
		dialect:     s.forcedDialect(),
		model:       s.entryModel(),
		name:        rankName,
		description: rankDescription,
		ephemeral:   true,
	}
}

// TestEntryRankSessionDoors pins the three doors a TUI session arrives on a server through.
func TestEntryRankSessionDoors(t *testing.T) {
	t.Parallel()

	runRankDoors(t, []rankDoor{
		{name: "A startup bind", resolve: resolveStartupBind, want: wantBoundEntry},
		{name: "B rebind", resolve: resolveRebind, want: wantRebind},
		{name: "B' server move", resolve: resolveServerMove, want: wantServerMove},
	})
}

// resolveStartupBind drives serverBinder.bind and records the Config it constructs the Agent from.
// The dialect is read off the Monitor's binding: a session states the forced key there, and the
// engine takes the dialect from the first beat's rebind.
func resolveStartupBind(t *testing.T, s rankScenario) entryRanks {
	t.Helper()
	engine := newLateEngine(apogee.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })
	opts := s.options()
	cfg := validCfg(t)
	cfg.Context.MaxContextTokens = opts.ContextWindow
	cfg.Context.WorkingWindow = opts.WorkingWindow
	cfg.Context.ResponseReserveFraction = opts.ResponseReserve
	cfg.StreamIdleTimeout = opts.StreamIdleTimeout
	holder := newUpstreamHolder()
	var handed apogee.Config
	binder := serverBinder{
		cfg:    cfg,
		engine: engine,
		holder: holder,
		caps:   newParallelAgentsCap(engine),
		keys:   config.NewKeyResolver(""),
		build: func(cfg apogee.Config, resumed *session.Record, opts ...apogee.Option) (*apogee.Agent, error) {
			handed = cfg
			return buildAgent(cfg, resumed, opts...)
		},
	}

	if err := binder.bind(s.entry()); err != nil {
		t.Fatalf("bind: %v", err)
	}

	ranks := ranksOfConfig(handed)
	ranks.dialect = domain.EffortDialect(holder.Binding().EffortDialect)
	return ranks
}

// resolveRebind drives the two halves of a beat's rebind — the live inputs read against the bound
// entry, and the per-model spec resolved from them — for the model and window the beat saw.
func resolveRebind(t *testing.T, s rankScenario) entryRanks {
	t.Helper()
	live := newLiveSettings(s.options(), config.ProjectOffer{})
	beat := s.observation()

	base, pinned, outputCap := live.rebindInputs(bindingOfEntry(s.entry(), s.key()))
	spec, _, err := rebindSpecFor(base, stateRoots{config: t.TempDir()}, beat.ActiveModel, beat.ContextWindow,
		pinned, outputCap)
	if err != nil {
		t.Fatalf("rebindSpecFor: %v", err)
	}

	ranks := entryRanks{
		window:  spec.MaxContextTokens,
		dialect: domain.EffortDialect(spec.EffortDialect),
		model:   spec.Model,
	}
	if spec.ResponseReserveFraction != nil {
		ranks.reserve = *spec.ResponseReserveFraction
	}
	if spec.MaxOutputTokens != nil {
		ranks.maxOutput = *spec.MaxOutputTokens
	}
	return ranks
}

// wantRebind: the window pin over the beat's window, the reserve and the reply cap re-stated, and
// the beat's model.
func wantRebind(s rankScenario) entryRanks {
	return entryRanks{
		window:    s.observedWindow(),
		reserve:   s.reserve(),
		maxOutput: s.maxOutput(),
		// The beat's model is the one a rebind binds: it is driven by what the server serves.
		model: choose(s.beat, rankBeatModel, ""),
		// Not stated here, on purpose:
		// - working window: a rebind re-resolves one model, and the room is the entry's from its
		//   arrival (bind, move) — an edit to it lands at the next start;
		// - stream idle, width, identity: a rebind moves no server, so the dial, the cap and the
		//   name stay what the arrival stated;
		// - dialect: rootWiring.rebind lays the beat's own over this spec, and that beat comes
		//   off a Monitor the entry's forced key already shaped.
	}
}

// resolveServerMove drives the `/server` move's resolution: entryUpstream over the live top-level
// pins, and the fan-out cap that follows the entry on every arrival.
func resolveServerMove(t *testing.T, s rankScenario) entryRanks {
	t.Helper()
	live := newLiveSettings(s.options(), config.ProjectOffer{})
	entry := s.entry()

	spec, dial := entryUpstream(entry, s.key(), rootPins{
		contextWindow:   live.pin(),
		workingWindow:   live.workingPin(),
		responseReserve: live.reservePin(),
		streamIdle:      live.streamIdlePin(),
	})
	width := newParallelAgentsCap(&parallelAgentsSpy{}).follow(entry)

	ranks := entryRanks{
		window:      spec.MaxContextTokens,
		working:     spec.WorkingWindow,
		reserve:     spec.ResponseReserveFraction,
		maxOutput:   spec.MaxOutputTokens,
		width:       width,
		dialect:     domain.EffortDialect(dial.EffortDialect),
		name:        spec.ServerName,
		description: spec.ServerDescription,
		ephemeral:   spec.ServerEphemeral,
	}
	if spec.StreamIdleTimeout != nil {
		ranks.streamIdle = *spec.StreamIdleTimeout
	}
	return ranks
}

// wantServerMove: the startup bind's ranks, from the same entry.
func wantServerMove(s rankScenario) entryRanks {
	ranks := wantBoundEntry(s)
	// A move unbinds the model until the new server's first beat binds the one it serves.
	ranks.model = ""
	return ranks
}

// TestEntryRankFiringDoors pins the two doors a Firing composes its run through: the unattended
// one (headless, the daemon) and the one a session's Scheduler raises.
func TestEntryRankFiringDoors(t *testing.T) {
	t.Parallel()

	runRankDoors(t, []rankDoor{
		{name: "C unattended Firing", resolve: resolveUnattendedFiring, want: wantUnattendedFiring},
		{name: "C' in-session Firing", resolve: resolveSessionFiring, want: wantSessionFiring},
	})
}

// composeFiring runs firingConfig over in, answering refused for the one refusal the matrix
// reaches: an answered beat that names no model for an entry that names none.
func composeFiring(t *testing.T, in firingInputs) entryRanks {
	t.Helper()
	in.roots = firingRoots(t)
	in.confiner = platform.NewDenyConfiner()
	in.mode = domain.ModePlan
	in.recordID = rankRecordID

	cfg, _, _, err := firingConfig(context.Background(), in)
	if errors.Is(err, errFiringNoActiveModel) {
		return entryRanks{refused: true}
	}
	if err != nil {
		t.Fatalf("firingConfig: %v", err)
	}
	return ranksOfConfig(cfg)
}

// resolveUnattendedFiring composes a headless or daemon Firing on the entry, its one beat stubbed.
func resolveUnattendedFiring(t *testing.T, s rankScenario) entryRanks {
	t.Helper()
	beat := s.observation()
	return composeFiring(t, firingInputs{
		opts:  s.options(),
		entry: s.entry(),
		beat: func(context.Context, string, string, string, provider.Wire) heartbeat.Beat {
			return beat
		},
	})
}

// wantUnattendedFiring: the entry's ranks over the top-level keys, with the beat filling what
// neither pins — the window, the width, the dialect and the model.
func wantUnattendedFiring(s rankScenario) entryRanks {
	if !s.pinned && !s.beat {
		return entryRanks{refused: true}
	}
	ranks := wantBoundEntry(s)
	ranks.window = s.observedWindow()
	ranks.width = s.width(true)
	if !s.forced && s.beat {
		ranks.dialect = rankObserved
	}
	if !s.pinned {
		ranks.model = rankBeatModel
	}
	return ranks
}

// resolveSessionFiring composes a Firing the way scheduleWiring.fire does: the bound entry off the
// live settings, and a beat built from what the session already holds — the model it is bound to,
// the window and dialect its last beat recorded, and its own fan-out width. A present beat is the
// session's last one: recorded on the live settings and the cap, and the model its rebind bound.
func resolveSessionFiring(t *testing.T, s rankScenario) entryRanks {
	t.Helper()
	entry := s.entry()
	live := newLiveSettings(s.options(), config.ProjectOffer{})
	caps := newParallelAgentsCap(&parallelAgentsSpy{})
	caps.follow(entry)
	binding := bindingOfEntry(entry, s.key())
	if s.beat {
		observed := s.observation()
		live.observe(observed.ContextWindow, providerEffortDialect(observed.EffortSupport.Dialect))
		caps.observe(observed.TotalSlots)
		binding.Model = observed.ActiveModel
	}

	opts, bound := live.firingBinding(binding)
	return composeFiring(t, firingInputs{
		opts:   opts,
		entry:  bound,
		apiKey: binding.APIKey,
		beat: func(context.Context, string, string, string, provider.Wire) heartbeat.Beat {
			return heartbeat.Beat{
				Reachable:     true,
				Answered:      true,
				ActiveModel:   binding.Model,
				ContextWindow: live.observed(),
				TotalSlots:    caps.current(),
				EffortSupport: domain.EffortSupport{Dialect: domainEffortDialect(live.observedDialect())},
			}
		},
	})
}

// wantSessionFiring: the unattended Firing's ranks, with the differences an in-session Firing
// keeps on purpose.
func wantSessionFiring(s rankScenario) entryRanks {
	ranks := wantUnattendedFiring(s)
	if ranks.refused {
		return ranks
	}
	// The width is the session's own (ADR 0039 D2): the same ranks, read off the session's cap.
	ranks.width = s.width(true)
	// The description and the forced dialect are dropped (issue firing-description-scrub-parity):
	// the session's observed dialect stands in, which its Monitor already forced.
	ranks.description = ""
	ranks.dialect = domain.EffortDialectNone
	if s.beat {
		ranks.dialect = rankObserved
		// The model is the one the session is bound to, which its rebind took off the beat.
		ranks.model = rankBeatModel
	}
	return ranks
}

// TestEntryRankProbeDoor pins the context probe's door: the unattended composer's server-bound
// half over the startup entry, with no beat taken.
func TestEntryRankProbeDoor(t *testing.T) {
	t.Parallel()

	runRankDoors(t, []rankDoor{
		{name: "G probe", resolve: resolveProbe, want: wantProbe},
	})
}

// resolveProbe composes the probe's Config on the scenario's startup entry.
func resolveProbe(t *testing.T, s rankScenario) entryRanks {
	t.Helper()

	cfg, err := probeContextConfig(s.options(), firingRoots(t), domain.ModePlan, probeContextOverlay{})
	if err != nil {
		t.Fatalf("probeContextConfig: %v", err)
	}
	return ranksOfConfig(cfg)
}

// wantProbe: the entry alone — the probe observes nothing, so a beat moves no rank.
func wantProbe(s rankScenario) entryRanks {
	return wantBoundEntry(s)
}

// The fallback doors' own scenario values: the primary a seat falls over FROM, onto the scenario's
// entry. It pins everything it states, so the window and reserve share a session or a Firing holds
// on it — what a routed child keeps from its parent — are the same in every row and on both
// Drivers.
const (
	rankPrimaryName     = "primary"
	rankPrimaryEndpoint = "http://127.0.0.1:2222"
	rankPrimaryModel    = "primary-model"
	rankPrimaryWidth    = 7
	rankParentWindow    = 100000
	rankParentReserve   = 0.3
)

// rankPrimary is the entry the seat is bound to before the fallover: it names the scenario's entry
// as its one `fallback:`.
func rankPrimary() config.ServerEntry {
	return config.ServerEntry{
		Name:            rankPrimaryName,
		Endpoint:        rankPrimaryEndpoint,
		Model:           rankPrimaryModel,
		ContextWindow:   rankParentWindow,
		ResponseReserve: rankParentReserve,
		ParallelAgents:  rankPrimaryWidth,
		Fallback:        config.NewFallbackList(rankName),
	}
}

// fallbackOptions is the scenario's configuration with the run started on the primary, and the
// scenario's entry listed beside it as the fallback the seat lands on.
func (s rankScenario) fallbackOptions() config.Options {
	opts := s.options()
	opts.Endpoint = rankPrimaryEndpoint
	opts.HostAlias = rankPrimaryName
	opts.StartupEntry = rankPrimary()
	opts.Servers = []config.ServerEntry{rankPrimary(), s.entry()}
	return opts
}

// monitored is the beat as the entry's own Monitor reports it: the observation, with the entry's
// forced `effort-dialect:` ranked over the tell (provider.forceEffortDialect), as every fallback
// beat and a Sub-agent server's beat are taken through bindingOfEntry's Monitor.
func (s rankScenario) monitored() heartbeat.Beat {
	beat := s.observation()
	if s.forced {
		beat.EffortSupport = domain.EffortSupport{Dialect: rankForced}
	}
	return beat
}

// monitoredDialect is the entry's forced dialect, else the one the beat saw.
func (s rankScenario) monitoredDialect() domain.EffortDialect {
	switch {
	case s.forced:
		return rankForced
	case s.beat:
		return rankObserved
	}
	return domain.EffortDialectNone
}

// landedModel is the entry's model pin, else the model the beat saw.
func (s rankScenario) landedModel() string {
	if s.pinned {
		return rankEntryModel
	}
	return choose(s.beat, rankBeatModel, "")
}

// landable says the seat can land on the entry at all: a server that answers names no model and
// the entry pins none, so there is nothing to bind and the walk moves on.
func (s rankScenario) landable() bool {
	return s.pinned || s.beat
}

// landOn drives one Driver's lander onto the scenario's entry, its key and its one beat stubbed —
// each Driver's own beat is the entry's Monitor (monitored) — and reads off what the seat runs on
// once the engine has applied the answer (ranksOfLanding). refused says it did not land.
func landOn(lander fallbackLander, s rankScenario) (fallbackLanding, entryRanks) {
	lander.key = func(config.ServerEntry) (string, error) { return s.key(), nil }
	beat := s.monitored()
	lander.beat = func(context.Context, upstreamBinding) heartbeat.Beat { return beat }

	landing, landed := lander.arrive(context.Background(), rankName)
	if !landed {
		return landing, entryRanks{refused: true}
	}
	return landing, ranksOfLanding(landing.answer)
}

// ranksOfLanding reads the binding a FallbackSwitch leaves the seat on, applied as the engine
// applies it: the switch first, then the rebind over it. The rebind always states the window, the
// reserve share and the reply cap (rebindSpecFor), so those are its; the working window, the
// stream-idle window and the seat's words are the switch's alone. No width rides an answer.
func ranksOfLanding(answer apogee.FallbackAnswer) entryRanks {
	up, rebind := answer.Upstream, answer.Rebind
	ranks := entryRanks{
		window:      rebind.MaxContextTokens,
		working:     up.WorkingWindow,
		reserve:     up.ResponseReserveFraction,
		maxOutput:   up.MaxOutputTokens,
		dialect:     domain.EffortDialect(rebind.EffortDialect),
		model:       rebind.Model,
		name:        up.ServerName,
		description: up.ServerDescription,
		ephemeral:   up.ServerEphemeral,
	}
	if up.StreamIdleTimeout != nil {
		ranks.streamIdle = *up.StreamIdleTimeout
	}
	if rebind.ResponseReserveFraction != nil {
		ranks.reserve = *rebind.ResponseReserveFraction
	}
	if rebind.MaxOutputTokens != nil {
		ranks.maxOutput = *rebind.MaxOutputTokens
	}
	return ranks
}

// sessionFallbackOf is the TUI session's seat fallback over the scenario's configuration, the
// session started on the primary.
func sessionFallbackOf(t *testing.T, s rankScenario) *sessionFallback {
	t.Helper()
	live := newLiveSettings(s.fallbackOptions(), config.ProjectOffer{})
	return newSessionFallback(live, newUpstreamHolder(), config.NewKeyResolver(""), stateRoots{config: t.TempDir()})
}

// firingOnPrimary composes the unattended Firing the scenario's fallover happens in: bound to the
// primary, with the roots its landers resolve against.
func firingOnPrimary(t *testing.T, s rankScenario) (apogee.Config, firingInputs, firingRouting) {
	t.Helper()
	in := firingInputs{
		opts:     s.fallbackOptions(),
		entry:    rankPrimary(),
		roots:    firingRoots(t),
		confiner: platform.NewDenyConfiner(),
		mode:     domain.ModePlan,
		recordID: rankRecordID,
		beat: func(context.Context, string, string, string, provider.Wire) heartbeat.Beat {
			return heartbeat.Beat{Reachable: true, Answered: true, ActiveModel: rankPrimaryModel}
		},
	}
	cfg, routing, _, err := firingConfig(context.Background(), in)
	if err != nil {
		t.Fatalf("firingConfig on the primary: %v", err)
	}
	return cfg, in, routing
}

// landSeatOn lands the run's own seat resolver on the scenario's entry, its key and its one beat
// stubbed as landOn stubs a bare lander's, so whatever follows that resolver's latest landing reads
// the move the engine would have been answered with.
func landSeatOn(t *testing.T, seat *seatResolver, s rankScenario) {
	t.Helper()
	if seat == nil {
		t.Fatal("the run composed no seat resolver; the primary no longer names a `fallback:`")
	}
	lander := seat.src.lander
	seat.src.lander = func() fallbackLander {
		stubbed := lander()
		stubbed.key = func(config.ServerEntry) (string, error) { return s.key(), nil }
		beat := s.monitored()
		stubbed.beat = func(context.Context, upstreamBinding) heartbeat.Beat { return beat }
		return stubbed
	}
	if _, landed := seat.land(context.Background(), rankName); !landed {
		t.Fatal("the run's seat resolver refused an entry its bare lander landed on")
	}
}

// TestEntryRankFallbackDoors pins the door a session seat's Server fallback lands through (D), on
// both Drivers: the TUI's resolver (newSessionFallback) and an unattended Firing's
// (firingSessionSource).
func TestEntryRankFallbackDoors(t *testing.T) {
	t.Parallel()

	runRankDoors(t, []rankDoor{
		{name: "D TUI session-seat landing", resolve: resolveSessionSeatLanding, want: wantSessionSeatLanding},
		{name: "D unattended session-seat landing", resolve: resolveFiringSeatLanding, want: wantFiringSeatLanding},
	})
}

// resolveSessionSeatLanding lands the TUI session seat's lander on the entry, and reads the width
// the session's follow of the switch fans out at (sessionMover.arrive: the cap follows the entry).
func resolveSessionSeatLanding(t *testing.T, s rankScenario) entryRanks {
	t.Helper()
	landing, ranks := landOn(sessionFallbackOf(t, s).lander(), s)
	if ranks.refused {
		return ranks
	}
	ranks.width = newParallelAgentsCap(&parallelAgentsSpy{}).follow(landing.entry)
	return ranks
}

// resolveFiringSeatLanding lands a Firing's session-seat lander on the entry — built as
// firingSessionSource builds it, over the Firing's own Options and roots — and reads, off the run
// composed on the primary, the width it fans out at, which the landing does not move. It then lands
// the run's own seat resolver on the entry as well and reads the server the run's namer names an
// unrouted child on once it has.
func resolveFiringSeatLanding(t *testing.T, s rankScenario) entryRanks {
	t.Helper()
	cfg, in, routing := firingOnPrimary(t, s)
	_, ranks := landOn(fallbackLander{opts: in.opts, roots: in.roots}, s)
	if ranks.refused {
		return ranks
	}
	ranks.width = cfg.ParallelAgents
	landSeatOn(t, routing.fallback, s)
	namer, ok := cfg.Namer.(*delegationNamer)
	if !ok {
		t.Fatalf("the Firing's namer is %T, want *delegationNamer", cfg.Namer)
	}
	unrouted, _ := namer.upstream(false)
	ranks.namedOn = unrouted.Endpoint
	return ranks
}

// wantSessionLanding: the `/server` move's ranks onto the same entry — every pin over the root key
// (entryUpstream) — with the rebind's window, the entry's pin over the root's else the window the
// beat saw, the entry's forced dialect over the beat's, and the entry's model else the beat's. The
// width is each Driver's own.
func wantSessionLanding(s rankScenario) entryRanks {
	if !s.landable() {
		return entryRanks{refused: true}
	}
	return entryRanks{
		window:      s.observedWindow(),
		working:     s.working(),
		reserve:     s.reserve(),
		maxOutput:   s.maxOutput(),
		streamIdle:  s.streamIdle(),
		dialect:     s.monitoredDialect(),
		model:       s.landedModel(),
		name:        rankName,
		description: rankDescription,
		ephemeral:   true,
	}
}

// wantSessionSeatLanding: the session's width follows the entry it arrived on, as a `/server` move's
// does — the pin, else the entry's floor until the new server's first beat names its slots.
func wantSessionSeatLanding(s rankScenario) entryRanks {
	ranks := wantSessionLanding(s)
	if !ranks.refused {
		ranks.width = s.width(false)
	}
	return ranks
}

// wantFiringSeatLanding: the session landing's ranks, with what a Firing does not follow.
func wantFiringSeatLanding(s rankScenario) entryRanks {
	ranks := wantSessionLanding(s)
	if ranks.refused {
		return ranks
	}
	// WRONG at base: issue firing-fallover-width-stale, fixed by plan item 9. The answer carries no
	// width and nothing in a Firing follows the entry, so the run keeps fanning out at the dead
	// primary's width rather than the fallback entry's.
	ranks.width = rankPrimaryWidth
	// The namer's session reader follows the seat's latest landing, so an unrouted child spawned
	// after the fallover is named on the fallback the run now talks to.
	ranks.namedOn = rankEndpoint
	return ranks
}

// TestEntryRankRoutedDoors pins the doors a routed child binds a server entry through: a spawn's
// Delegation target (E), and the Sub-agents seat's Server fallback landing on the entry (F) on both
// Drivers. Every row states ADR 0045 decision 4: no root `context-window:` or `working-window:`
// reaches a routed binding — they describe the session's own server — and an entry stating no
// window or reserve share leaves the child on its parent's.
func TestEntryRankRoutedDoors(t *testing.T) {
	t.Parallel()

	runRankDoors(t, []rankDoor{
		{name: "E routed spawn", resolve: resolveRoutedSpawn, want: wantRoutedSpawn},
		{name: "F TUI routed landing", resolve: resolveSessionRoutedLanding, want: wantRoutedLanding},
		{name: "F unattended routed landing", resolve: resolveFiringRoutedLanding, want: wantRoutedLanding},
	})
}

// resolveRoutedSpawn resolves the Delegation target both Drivers latch off the Sub-agent server's
// beat (the TUI's subAgentBeat, a Firing's resolveFiringRouting) — the target as stated, before the
// engine lays it over the parent's Config.
func resolveRoutedSpawn(t *testing.T, s rankScenario) entryRanks {
	t.Helper()
	target := resolveDelegationTarget(s.entry(), s.key(), s.monitored(), nil, s.options().StreamIdleTimeout)
	if target == nil {
		return entryRanks{refused: true}
	}
	ranks := entryRanks{
		window:    target.ContextWindow,
		working:   target.WorkingWindow,
		reserve:   target.ResponseReserveFraction,
		maxOutput: target.MaxOutputTokens,
		width:     target.ParallelAgents,
		dialect:   domain.EffortDialect(target.EffortDialect),
		model:     target.Model,
		name:      target.ServerName,
	}
	if target.StreamIdleTimeout != nil {
		ranks.streamIdle = *target.StreamIdleTimeout
	}
	return ranks
}

// wantRoutedSpawn: the routed landing's ranks, as a target states them for the engine to lay over
// the parent.
func wantRoutedSpawn(s rankScenario) entryRanks {
	ranks := wantRoutedLanding(s)
	if ranks.refused {
		return ranks
	}
	// A reserve share the entry does not state is left 0, which the engine reads as "the parent's
	// stands" (DelegationTarget.binding) — where the landing states the parent's.
	if !s.pinned {
		ranks.reserve = 0
	}
	// The spawn's width ranks the beat's slots between the pin and the floor (ADR 0039: one width).
	ranks.width = s.width(true)
	// A target states no description, so the child keeps its parent's words; and it is always a
	// configured entry, never the one-run `--endpoint` row (DelegationTarget.binding).
	ranks.description = ""
	ranks.ephemeral = false
	return ranks
}

// resolveSessionRoutedLanding lands the TUI's Sub-agents seat lander (sessionFallback.routedLander)
// on the entry, over the window and reserve share the session holds on the primary.
func resolveSessionRoutedLanding(t *testing.T, s rankScenario) entryRanks {
	t.Helper()
	_, ranks := landOn(sessionFallbackOf(t, s).routedLander(), s)
	return ranks
}

// resolveFiringRoutedLanding lands a Firing's Sub-agents seat lander on the entry — built as
// firingSubAgentsSource builds it, over the parent firingConfig hands resolveFiringRouting: the window
// and reserve share the run bound on the primary.
func resolveFiringRoutedLanding(t *testing.T, s rankScenario) entryRanks {
	t.Helper()
	cfg, in, _ := firingOnPrimary(t, s)
	_, ranks := landOn(fallbackLander{
		opts:   in.opts,
		roots:  in.roots,
		routed: true,
		parent: delegationParent{
			window:  cfg.Context.MaxContextTokens,
			reserve: cfg.Context.ResponseReserveFraction,
		},
	}, s)
	return ranks
}

// choosePin is the pin when the entry states one, else the value observed.
func choosePin(pinned bool, pin, observed int) int {
	if pinned {
		return pin
	}
	return observed
}

// wantRoutedLanding: the entry's pins with no root window rank under them — the window pinned, else
// the beat's, else the parent's; the working window pinned or none; the reserve share pinned, else
// the parent's — the root stream-idle window under the entry's (the run's default silence bound, no
// statement about a server), and the session landing's dialect, model and words.
func wantRoutedLanding(s rankScenario) entryRanks {
	ranks := wantSessionLanding(s)
	if ranks.refused {
		return ranks
	}
	// The parent's window is the last rank, under the beat's; no landable row reaches it, as a beat
	// that names no model names no window either.
	ranks.window = choosePin(s.pinned, rankEntryWindow, rankBeatWindow)
	ranks.working, ranks.reserve = 0, rankParentReserve
	if s.pinned {
		ranks.working, ranks.reserve = rankEntryWorking, rankEntryReserve
	}
	// Width: an answer carries none; a routed child spawned after the landing takes the re-latched
	// target's (E).
	return ranks
}
