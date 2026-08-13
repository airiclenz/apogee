package main

// The Sub-agent server seam of the composition root (ADR 0045).
//
// One `servers:` entry may be flagged `sub-agents: true`, and every delegation this session makes
// then runs on THAT server — a smart model orchestrating while a cheaper one, possibly on another
// box, does the grunt work. What that server is serving, in which window and across how many slots,
// is not something a config file can know, so it is DISCOVERED: a second heartbeat Monitor observes
// the flagged entry on the session monitor's own cadence, and each beat is resolved into the one
// value the engine latches — the Delegation target.
//
// The split is ADR 0031's: the resolution happens HERE, at the layer that knows the file, the pins,
// the profile tiers and the Mechanism catalogue, and the engine is handed a finished spec it applies
// without reading a line of config. And it is ADR 0042's degrade: a server that cannot be reached,
// or that binds no model, resolves to nil — which the engine reads as "not routing", so delegations
// run on the session's own Upstream exactly as they did before any of this existed.
//
// Two things make that degrade VISIBLE and keep it current. The routing STATE — are delegations
// going to the flagged server or to this session's own — is tracked beside the beat, and each time
// it changes the transcript says so once (ADR 0045 §4: per state change, never per spawn). And the
// flagged entry is re-read whenever `servers:` is edited mid-session (ADR 0037/0041), so adding the
// flag starts observing, removing it stops and unlatches, and re-pointing it moves the whole thing
// to the other server — none of which waits for a relaunch.

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/profiles"
)

// delegationSetter is the engine mutation a resolved target is pushed through, named as the ONE
// method it needs so the wiring below is exercisable without constructing an Agent — the
// upstreamSwitcher / parallelAgentsSetter posture. Satisfied by *lateEngine, and through it by
// *apogee.Agent.
type delegationSetter interface {
	SetDelegationTarget(*apogee.DelegationTarget)
}

// subAgentServer is one flagged `servers:` entry as the wiring holds it: the entry itself — the dial
// facts a target is built from, the pins that outrank discovery, and the posture keys that ride the
// flag — beside the two things a beat on it needs, both derived from that entry and therefore
// replaced WITH it whenever the file re-points the flag somewhere else.
type subAgentServer struct {
	entry config.ServerEntry
	// beat observes the Sub-agent server with the key that server's entry resolved to. It is a func
	// rather than the *heartbeat.Monitor itself for the reason every seam this root hands across is:
	// the resolution is then exercisable against an observation a test writes down, with no server
	// behind it.
	//
	// The key is an ARGUMENT rather than something baked in at build time because a Monitor is per
	// key as much as per endpoint, and the key of an entry whose source is a command is not known
	// until that command has run — which is a thing to do on the beat goroutine, once, and to RETRY
	// on the next beat when it fails, rather than at the startup that has no business waiting on a
	// keychain for a server this session may never delegate to.
	beat func(ctx context.Context, apiKey string) heartbeat.Beat
	// catalogue builds the Mechanism registry ONE routed child runs with, and is nil when the entry
	// carries no `mechanisms:` map — which is the engine's signal to inherit the parent's catalogue
	// as it always has (ADR 0045 §2). It is a factory because siblings in a fan-out run at once and
	// each needs a registry of its own.
	catalogue func() *apogee.MechanismRegistry
}

// delegationWiring is this session's Sub-agent server: which entry is flagged right now, the beat
// that observes it, the routing state the transcript is told about, and the two things the root
// resolves for every beat — where the model-profile match reads its user tier, and where a resolved
// target is latched.
//
// With NO entry flagged it holds no server: no beat, no second Monitor, nothing ever latched, which
// is behaviour identical to before routing existed (ADR 0045 §4's floor). The wiring itself is
// always present even then, because the flag can arrive later: `servers:` is editable mid-session
// (ADR 0037), so "there is no Sub-agent server" is a state this holder moves out of rather than a
// reason not to exist.
//
// It is a pointer with a mutex for that same reason. The beat goroutine reads the server on every
// interval while the Update goroutine replaces it from a config reload, so the fields are genuinely
// shared — the liveSettings posture, one layer up from the latch's own lock inside the engine
// (agent.SetDelegationTarget).
type delegationWiring struct {
	mu sync.Mutex
	// server is the flagged entry and its beat, or nil when the list flags none.
	server *subAgentServer
	// generation counts REPLACEMENTS of the server above. A beat resolves off a snapshot, and an
	// edit that lands while it is in flight must not be overwritten by what the previous server's
	// observation resolved to — so a landing whose generation has moved on is dropped.
	generation int
	// routed is the routing state the notices are transitions OF: true while delegations go to the
	// Sub-agent server, false while they fall back to this session's own Upstream.
	routed bool
	// stated records that the state above has been reported at least once, which is what makes the
	// FIRST resolved state news whichever way it goes — a flagged server that was never reachable
	// degrades as visibly as one that stopped being.
	stated bool
	// base is the session's own Config, kept because a re-read `servers:` list has to build the new
	// entry's Mechanism catalogue out of exactly what startup built the old one from.
	base apogee.Config
	// userProfiles reads the `model-profiles:` user tier as it stands NOW, so a profile committed
	// mid-session reaches the next beat's resolution rather than the next launch.
	userProfiles func() []profiles.Entry
	// notify puts one routing notice in front of the human, and is nil for a Driver that shows
	// nothing (a bench, a headless run) — the degrade every seam this root hands across takes.
	notify func(string)
	// keys resolves the flagged entry's key source. It is asked on every beat rather than once at
	// wiring time, and that is the whole reason a resolver caches: the command runs on the first
	// beat and the ten thousand after it read the answer — while a beat whose resolution FAILED
	// asks again ten seconds later, which is what lets a keychain the human unlocks mid-session
	// bring the Sub-agent server back without a relaunch.
	keys *config.KeyResolver
	// engine is where a resolved target is latched.
	engine delegationSetter
}

// newDelegationWiring finds the Sub-agent server in entries and builds everything a beat on it will
// need. With no entry flagged it answers a wiring holding no server — no monitor is constructed and
// no target is ever pushed, so a session whose config says nothing about delegation routing behaves
// exactly as it did before (ADR 0045 §4's floor) until an edit says otherwise.
//
// base is the session's own Config, and it is read for exactly what building the flagged entry's
// Mechanism catalogue needs: the state roots. The entry's own endpoint and `model:` pin replace the
// session's on the copy that build sees, so the identity a Library observation is filed under is the
// SUB-AGENT server's model rather than the orchestrator's.
//
// It fails the run rather than degrading when the flagged entry's `mechanisms:` map is defective —
// an unknown key, or a set the stacking gates refuse. That is the same posture the session's own
// block takes at this same boundary (mechanismIDs, wireSession): a typo in a file outlives the day
// it was written, and a posture that silently armed nothing would be invisible for months.
func newDelegationWiring(
	entries []config.ServerEntry,
	base apogee.Config,
	engine delegationSetter,
	userProfiles func() []profiles.Entry,
	notify func(string),
	keys *config.KeyResolver,
) (*delegationWiring, error) {
	wiring := &delegationWiring{
		base:         base,
		userProfiles: userProfiles,
		notify:       notify,
		keys:         keys,
		engine:       engine,
	}
	entry, ok := config.SubAgentServer(entries)
	if !ok {
		return wiring, nil
	}
	server, err := newSubAgentServer(entry, base)
	if err != nil {
		return nil, err
	}
	wiring.server = server
	return wiring, nil
}

// newSubAgentServer builds the beat and the Mechanism catalogue for one flagged entry. It is the
// step a startup and a config reload share, so a Sub-agent server that arrives hours into a session
// is assembled exactly as one named at launch — including the refusal of a defective `mechanisms:`
// map, which a reload returns to the settings row rather than to the terminal.
func newSubAgentServer(entry config.ServerEntry, base apogee.Config) (*subAgentServer, error) {
	catalogue, err := subAgentCatalogue(entry, base)
	if err != nil {
		return nil, err
	}
	return &subAgentServer{
		entry:     entry,
		beat:      subAgentBeat(entry),
		catalogue: catalogue,
	}, nil
}

// subAgentBeat builds the beat for one flagged entry: the Monitor that observes it, constructed on
// the first beat that has a key in hand and reused by every beat after it.
//
// The discovery hint it is built with is the entry's own `model:` pin, empty when it pins none — the
// session Monitor's contract verbatim (heartbeat.NewMonitor): while the server still serves the
// pinned id, discovery resolves ITS window rather than the first advertised model's, and once the
// pin vanishes the beat reports what is actually loaded.
//
// What is deliberately NOT settled at build time is the key. A Monitor holds the key it was built
// with for its whole life, and the key of an entry whose source is a command is not known until that
// command has run — so building the Monitor here would mean running a keychain command inside a
// `servers:` re-read, on the Update goroutine, for a server the session may never delegate to. The
// key therefore arrives per beat and the Monitor is rebuilt whenever it CHANGES, which is the same
// per-server-per-key rule a `/server` switch obeys one layer up (upstreamHolder.Swap).
func subAgentBeat(entry config.ServerEntry) func(context.Context, string) heartbeat.Beat {
	var (
		mu      sync.Mutex
		monitor *heartbeat.Monitor
		built   string
	)
	return func(ctx context.Context, apiKey string) heartbeat.Beat {
		mu.Lock()
		if monitor == nil || built != apiKey {
			monitor, built = heartbeat.NewMonitor(entry.Endpoint, entry.Model, apiKey), apiKey
		}
		current := monitor
		mu.Unlock()
		return current.Beat(ctx)
	}
}

// observe starts one beat on the Sub-agent server and hands back the join for it. With no Sub-agent
// server there is nothing to observe and the join is a no-op — no goroutine, no push, no latch.
//
// It is split into start-and-join because the session's beat runs in the same window: two five-second
// discoveries in SERIES would be exactly heartbeat.Interval, and that package's no-overlap property
// rests on a beat staying strictly shorter than the interval. Run side by side they cost the longer
// of the two, so the second server is observed on the same cadence without slowing the first.
//
// What it resolves against is a SNAPSHOT taken before the goroutine starts: the entry, its beat and
// its catalogue travel together, so a `servers:` edit landing mid-beat cannot pair one server's
// observation with another's posture. The landing checks the generation that snapshot was taken on
// (see land), which is what keeps the edit's own push the last word.
func (d *delegationWiring) observe(ctx context.Context) func() {
	d.mu.Lock()
	server, generation := d.server, d.generation
	d.mu.Unlock()
	if server == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		// The key that server takes, resolved on THIS goroutine and before anything is observed: it
		// is what the beat authenticates with, so a Monitor built without it would report a keyed
		// grunt box permanently unreachable while the box was perfectly fine. A source that refuses
		// resolves no target — nothing is beaten, nothing is latched, and the refusal is what the
		// human is told (land below), because a delegation sent to a server this session cannot
		// authenticate against would fail in the child, far from the entry that caused it.
		apiKey, err := d.keys.Resolve(server.entry)
		if err != nil {
			d.land(generation, server.entry.Name, nil, err)
			return
		}
		d.land(generation, server.entry.Name,
			resolveDelegationTarget(server.entry, apiKey, server.beat(ctx, apiKey), d.userProfiles(),
				server.catalogue), nil)
	}()
	return func() { <-done }
}

// land installs what one beat resolved and says so when the routing state moved.
//
// The push is unconditional — a resolved target on a usable beat, nil on an unusable one — because
// "the Sub-agent server is not answering" is exactly as much a fact about routing as "it is". The
// latch it lands in is anytime-safe (ADR 0045: deliberately never idle-gated), so a beat landing
// mid-Exchange re-points the delegations SPAWNED after it and leaves every running child alone.
//
// A landing whose generation has been superseded is DROPPED whole — neither latched nor narrated.
// It describes a server the file no longer flags, and the edit that replaced it has already pushed
// what it wanted; letting this land would restore the old server's target for a whole interval,
// which is the one thing a live edit must never leave behind.
//
// The push happens UNDER the lock, which is what ties it to the generation it was just admitted on. A
// landing that released the lock first could be overtaken in that window by a relist that unflags the
// server: the edit pushes its nil, this superseded target lands after it and wins, and — the flag
// being gone — no further beat ever comes to correct it, so routing would stay engaged against a
// server the file no longer flags. Holding the mutex across the push costs nothing, because the seam
// behind it takes one short lock of its own and calls nothing back into this holder.
//
// The notice, by contrast, goes out AFTER the push and OUTSIDE the lock, and both matter: a notice
// that preceded the latch would announce routing that is not in force yet, and the notice seam blocks
// until the renderer's Update loop takes it — so holding the mutex across it would park the beat
// goroutine on the very loop a config reload calls relist from.
// keyErr is the refusal the entry's key source earned, or nil — it lands exactly like an unusable
// beat (no target, delegations on the session's own server) and differs only in what the human is
// told: an unreachable server is a fact about the network, a refused key source is a fact about
// their config, and only the second one can be fixed by editing something.
func (d *delegationWiring) land(generation int, name string, target *apogee.DelegationTarget, keyErr error) {
	d.mu.Lock()
	if generation != d.generation {
		d.mu.Unlock()
		return
	}
	note := d.stateChange(name, target, keyErr)
	d.engine.SetDelegationTarget(target)
	d.mu.Unlock()

	if note != "" && d.notify != nil {
		d.notify(note)
	}
}

// stateChange folds one resolved target into the routing state and answers the notice that change is
// worth, or "" when nothing changed. Called with the mutex held.
//
// The FIRST resolution is always worth one, whichever way it goes: a human who flagged a server
// wants to know their delegations are going there, and — ADR 0042's visible degrade — wants to know
// just as much when they are not, which a state machine that only reported LOSSES would say nothing
// about for a server that was never up.
//
// Everything after that is transitions only. The model is deliberately not part of the state: a
// grunt box reloaded with another model keeps taking the delegations it was taking, and re-stating
// the routing on every model swap would edge back towards the per-spawn narration ADR 0045 §4 rules
// out.
func (d *delegationWiring) stateChange(name string, target *apogee.DelegationTarget, keyErr error) string {
	routed := target != nil
	if d.stated && routed == d.routed {
		return ""
	}
	d.routed, d.stated = routed, true
	switch {
	case routed:
		return "sub-agents: routing to " + name + " (" + target.Model + ")"
	case keyErr != nil:
		// The resolver's own sentence, which already names the entry and quotes what the command
		// said. It is carried through whole rather than summarized: it is the only place the human
		// is told why a server they flagged is taking no delegations, and "unavailable" would send
		// them looking at a network that is working.
		return "sub-agents: delegations run on the session server — " + keyErr.Error()
	default:
		return "sub-agents: " + name + " unavailable — delegations run on the session server"
	}
}

// relist re-points the wiring at the `servers:` list as it now stands — the Sub-agent server's half
// of ADR 0037's live apply, reached from the same reloadServers every other `servers:` re-read goes
// through (wire_settings.go), whether the edit came from the `/settings` pane or from the watcher
// noticing the file changed under it (ADR 0041).
//
// Four things a list can do to routing, and each is answered where the human would expect:
//
//   - ADDS the flag ⇒ the entry is assembled and observed from the next beat on. Nothing is latched
//     here, because nothing has been observed yet; the first beat says whether it is usable.
//   - REMOVES it ⇒ the beat stops and the latch is cleared NOW rather than at the next beat. Routing
//     ends when the file says it ends, and a target left latched for another interval would keep
//     sending delegations to a server that is no longer the Sub-agent server.
//   - RE-POINTS it at another entry ⇒ both of the above, in that order, for that same reason.
//   - EDITS the flagged entry in place ⇒ the new pins and posture are installed and the NEXT beat
//     resolves against them (never later than that, which is the freshness the plan requires).
//     Routing is not interrupted and the state is not re-stated: it is the same server, still
//     taking the same delegations, and nothing about that changed for the human to be told.
//
// It is validate-then-commit, like every other live re-read: a flagged entry whose `mechanisms:` map
// this build refuses returns the error with NOTHING installed, so the session keeps routing exactly
// as it did while the human fixes the file. A list whose Sub-agent server is untouched — the common
// case, since most `servers:` edits are about some other entry — is a comparison and no work at all.
func (d *delegationWiring) relist(entries []config.ServerEntry) error {
	entry, flagged := config.SubAgentServer(entries)

	d.mu.Lock()
	current := d.server
	d.mu.Unlock()

	if !flagged && current == nil {
		return nil // a list that named no Sub-agent server still names none
	}
	if flagged && current != nil && reflect.DeepEqual(current.entry, entry) {
		return nil // the edit was somewhere else in the list
	}

	var next *subAgentServer
	if flagged {
		built, err := newSubAgentServer(entry, d.base)
		if err != nil {
			return err
		}
		next = built
	}
	// Whether what is LATCHED still describes the flagged server. An entry edited in place still
	// does — same name, same endpoint, so the delegations in flight are going to the right box and
	// only the pins and posture the next beat reads have moved.
	stale := current != nil && (next == nil || next.entry.Name != current.entry.Name ||
		next.entry.Endpoint != current.entry.Endpoint)

	d.mu.Lock()
	d.server = next
	d.generation++
	if stale {
		// The state is forgotten rather than reported: a server the file stopped flagging is not a
		// server that became unavailable, and the human reading the notice is the one who just
		// edited it. Forgetting is what lets the NEXT server announce itself on its first beat.
		d.routed, d.stated = false, false
	}
	d.mu.Unlock()

	if stale {
		d.engine.SetDelegationTarget(nil)
	}
	return nil
}

// resolveDelegationTarget turns one observation of the Sub-agent server into the spec a routed spawn
// is built from, and nil when that server cannot take a delegation right now.
//
// Every field is PIN-else-OBSERVE, which is the whole of ADR 0045 decision 4 and the same rank order
// the session's own bindings already follow: the file's pin is a statement about the server that
// discovery may not overrule, the beat answers whatever the file left open, and the last rank is the
// honest floor. So `model:` outranks the model the beat found bound, `context-window:` outranks the
// window /props reported, and `parallel-agents:` outranks the slot count — that last one through
// config.ResolveParallelAgents itself, so the Sub-agent server's width is resolved by the very
// function the session's own cap is resolved by (ADR 0039: one width everywhere).
//
// Two facts make a target UNUSABLE, and both mean the same thing to the caller: a beat that could not
// read the server at all, and a beat on a server with no model bound and no pin to name one. Neither
// is an error — an unreachable server is a finding (heartbeat.Beat) — so the answer is nil, the
// fallback, and the delegations run on the session's own Upstream with the session's posture.
//
// The POSTURE is copied across untranslated on purpose: `bypass:` is the entry's own pointer, whose
// nil-ness IS the inherit-versus-replace instruction, and the catalogue factory is whatever the
// entry's `mechanisms:` map built (nil when it has none). Neither is a per-beat resolution, because
// neither is something a server can be observed to have.
func resolveDelegationTarget(
	entry config.ServerEntry,
	apiKey string,
	observed heartbeat.Beat,
	userProfiles []profiles.Entry,
	catalogue func() *apogee.MechanismRegistry,
) *apogee.DelegationTarget {
	if !observed.Reachable {
		return nil
	}
	model := entry.Model
	if model == "" {
		model = observed.ActiveModel
	}
	if model == "" {
		return nil
	}
	window := entry.ContextWindow
	if window <= 0 {
		window = observed.ContextWindow
	}
	// The shape the grunt model speaks the wire in, matched on the model that just resolved (ADR
	// 0044). The notice half of the match is deliberately dropped: it explains a built-in match of
	// the model the HUMAN is looking at, and this resolution re-runs every ten seconds on a model
	// they are not — a beat is no place to repeat a sentence.
	profile, _ := resolveModelProfile(model, userProfiles)
	return &apogee.DelegationTarget{
		Endpoint: entry.Endpoint,
		// The key the beat above just authenticated with, resolved from the entry's key source by
		// the caller: the child talks to the same server, with the same credential, and cannot be
		// handed a source it would have to run again for itself.
		APIKey:        apiKey,
		Model:         model,
		ContextWindow: window,
		// The entry's `max-output-tokens:` pin, carried as written (ADR 0046). There is no observed
		// half to fall back to — a server advertises no reply ceiling — so an absent key stays 0 and
		// the child derives its cap from the window resolved above.
		MaxOutputTokens: entry.MaxOutputTokens,
		ParallelAgents:  config.ResolveParallelAgents(entry.ParallelAgents, observed.TotalSlots),
		Profile:         profile,
		Bypass:          entry.Bypass,
		Mechanisms:      catalogue,
	}
}

// subAgentCatalogue builds the factory a routed child's Mechanism catalogue comes out of, and nil
// when the flagged entry carries no `mechanisms:` map at all — the absent key that leaves a child
// inheriting the parent's catalogue as it always has (ADR 0045 §2).
//
// A PRESENT map is the child's entire catalogue, so a map whose every key is false is not the same
// as no map: it builds an EMPTY registry, and a delegation to that server runs with no Mechanism at
// all. That is the replace-whole rule doing exactly what it says, and it is why the emptiness test
// below is on the map rather than on the ids it validates to.
//
// The build goes through the engine's own BuildMechanisms rather than assembling a registry here,
// because the Deps a catalogue row needs — the Library store, the identity ladder keyed on the
// model — are the engine's to derive (ADR 0015 §2). What this layer owns is the two things the
// engine cannot know: that the map's keys are catalogued at all (mechanismIDs, the same validation
// the session's own block gets, typo'd DISABLED keys included), and that the identity is the
// SUB-AGENT server's — hence the endpoint and model swapped onto the config copy the build reads.
//
// The result is built ONCE, at startup, and the factory hands each child a copy through ForSubAgent:
// the registry is a per-run instance surface, not a per-model resolution, so re-deriving it on every
// beat would buy nothing and would put a build error somewhere no one can see it.
func subAgentCatalogue(entry config.ServerEntry, base apogee.Config) (func() *apogee.MechanismRegistry, error) {
	if len(entry.Mechanisms) == 0 {
		return nil, nil
	}
	ids, err := mechanismIDs(entry.Mechanisms, mechanisms.KnownIDs())
	if err != nil {
		// mechanismIDs already carries the house "apogee: " prefix, so the entry that asked for it is
		// appended rather than prefixed (buildEnabledMechanisms' rule): the message would otherwise
		// print the prefix twice, and a user with two servers listed needs to know which one is meant.
		return nil, fmt.Errorf("%w — in the `sub-agents:` server %q", err, entry.Name)
	}
	cfg := base
	cfg.Endpoint = entry.Endpoint
	cfg.Model = entry.Model
	built, err := apogee.BuildMechanisms(cfg, ids)
	if err != nil {
		return nil, fmt.Errorf("%w — in the `sub-agents:` server %q", err, entry.Name)
	}
	return built.ForSubAgent, nil
}
