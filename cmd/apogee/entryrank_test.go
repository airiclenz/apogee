package main

// Characterisation pins of how each engine door resolves a server entry: the session's startup
// bind (A), its beat-driven rebind (B) and its `/server` move (B'), an unattended Firing (C) and
// one raised in-session (C'), and the context probe (G). ADR 0083 part 4 keeps one hand-ranked
// resolution per door rather than a shared entry→engine function, so these tables are what makes a
// door's drift fail a test: every door runs over the same scenario matrix, and each door's want
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
