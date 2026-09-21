package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/notice"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/reactions"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/skills"
)

// firingInputs is everything a Driver decides about ONE unattended run. Every difference between
// the three Drivers that raise one — `apogee headless`, the daemon's schedules, the `/schedule`
// picker inside a live session — is a field here rather than a branch inside firingConfig: which
// Options the run reads, which `servers:` entry it binds to, whose key resolver and skill catalog
// it borrows, and how it learns how wide it may fan out. That is what keeps the composition below
// a single shape instead of three that drift (ADR 0031: every Driver reaches the same engine
// behaviour from the same configuration).
type firingInputs struct {
	// opts is the configuration this Driver answers for. Headless and the daemon hand over the
	// Options their start-up resolved; a session hands over liveSettings.options(), the boot
	// Options with every applied /settings edit overlaid, so a Firing raised mid-session runs the
	// configuration the human is looking at rather than the one they launched with (ADR 0037).
	opts config.Options
	// entry is the bound `servers:` entry — the endpoint dialled, the key SOURCE resolved, and the
	// five per-entry pins (parallel-agents, max-output-tokens, context-window, working-window,
	// response-reserve).
	// It is one input rather than a scatter of fields because a Firing binds to a server exactly as
	// a session does, and every per-entry fact must come off the SAME entry the run talks to.
	entry config.ServerEntry
	// apiKey is the bearer token, when the Driver already holds the one its session resolved.
	// Empty means "resolve it from entry through keys" — the headless and daemon route, where
	// nothing has asked the key source yet.
	apiKey string
	// keys is the resolver the key source is asked through; nil takes a fresh
	// [config.NewKeyResolver]. A Driver that raises many Firings passes its own so a keychain is
	// not asked once per run.
	keys *config.KeyResolver
	// roots are the resolved state roots this run lives in — config/sessions/scratch plus
	// the workspace the file tools are fenced to. Resolving them stays the Driver's, because the
	// workspace is exactly the root a Driver decides (the daemon's is the schedule entry's).
	roots stateRoots
	// confiner is the OS confinement backend the run is fenced by — the session's own, so an Auto
	// Firing sits in the same box an Auto session would. Whether this host may run Auto unattended
	// at ALL is the eligibility gate, which belongs to the surface that offered the mode (ADR 0033
	// decision 3) and is therefore never asked here.
	confiner apogee.Confiner
	// model is the model this run binds to; empty takes the bound entry's own `model:`. It exists
	// because a schedule may overlay a model name onto the entry it names (ADR 0055 decision 2).
	model string
	// mode is the mode the run executes in — plan or auto, the two an unattended run may use.
	mode domain.Mode
	// skills is the catalog the run resolves attached skill IDs through and mounts the read roots
	// of; nil builds a fresh Provider from roots. A session passes its LIVE provider so a
	// `use-project-skills` flip keeps following its Firings (design call 5); headless and the
	// daemon pass nil, each having no longer-lived catalog to share.
	skills *skills.Provider
	// beat is this run's ONE observation of the server it is bound to; nil takes observeServer, the
	// one-shot beat standing in for the heartbeat an unattended run has none of. It is one seam
	// rather than the two probes it replaced — a width and a dialect asked separately — because
	// both answers come off the SAME observation of the SAME endpoint, model and key: two probes
	// were two round trips that could straddle a restart and report a server that never existed in
	// one state. It also carries the liveness the unattended Drivers refuse a Firing on, which is
	// why it is taken even when the entry pins both values away: the round trip IS the gate.
	//
	// A session passes its own beat instead — the width it already resolves, the dialect its
	// heartbeat already observed, and the liveness its footer last published (the upstream latch,
	// schedule.go) — because it is holding the answer, and a Firing must not spend a round trip
	// re-asking the server the session is talking to (design call 4).
	//
	// The wire is the entry's protocol (ADR 0078), passed so the one-shot beat dials the server
	// the way a session's Monitor does; a session's own beat has nothing to dial and ignores it.
	beat func(ctx context.Context, endpoint, model, apiKey string, wire provider.Wire) heartbeat.Beat
	// now is the Firing's clock: the instant the record id is minted from and the clock the runner
	// stamps CreatedAt with (run.Spec.Now), so the id's timestamp prefix and the record's own
	// timestamps come off ONE reading and can never disagree about the order two runs happened in.
	// nil ⇒ time.Now — headless, and every Driver whose Scheduler runs on the wall clock. A Driver
	// with a scheduler clock passes it through clockNow, so a test that pins the daemon's or the
	// session's sense of time pins the id it mints too.
	now func() time.Time
	// recordID is the id this run's record is filed under. The run's scratch dir is created under
	// it, so a saved run and the working files its model left behind are one thing to find and one
	// thing to sweep. A Driver that goes through raise leaves it empty — raise mints it, so the id
	// that names the record and the id that names the scratch dir cannot be two — and a composition
	// test, which runs nothing, states it directly.
	recordID string
	// hooks is the Reaction Runner this ONE Firing fires through (ADR 0073), built by the Driver from
	// firingHooks and closed by it when the Firing ends. It is a field rather than a branch for the
	// file's own rule: the three Drivers differ in where a Reaction's trouble is reported and in
	// whether the run belongs to a Schedule at all, and both of those are decided before the Runner
	// exists.
	//
	// nil is the legitimate absence — a Driver that raises no Reactions, and every composition test —
	// and it leaves Config.Events nil exactly as it was before this key existed.
	hooks *reactions.Runner
	// report is where a SYNC-lane reaction's trouble is said out loud — a `gate:` or `advise:`
	// command that failed, timed out or could not be spawned (domain.Config.Report). It is the SAME
	// function the Driver hands its Runner for the observe lane's failures, so one `reactions:`
	// file's trouble reads the same way whichever lane it came from and wherever this Driver
	// narrates: stderr for headless, the daemon log, the session's notice line for `/schedule`.
	//
	// nil DROPS the line, exactly as domain.Config.Report's own default does — every composition
	// test, and a Driver with nowhere to put it.
	report func(msg string)
	// runner is what the composed Firing is handed to once both gates have passed — run.Once in
	// production, a recording stub in a composition test that captures the run.Spec and runs
	// nothing (ADR 0033 decision 6 names the runner an injected seam, and ~40 tests observe the
	// composition through it). A Driver that holds its runner as a dependency (headless) passes it
	// through here; nil falls back to the package's runOnce var, read at the moment the Firing is
	// raised and never captured earlier, so the daemon, `/schedule` and the boot keep the seam they
	// still swap until they take their runner as a dependency too.
	runner func(context.Context, run.Spec) (run.Result, error)
}

// firingBinding is the server-BOUND half of an unattended run's Config — everything firingConfig
// composes before it takes its one beat of the server: the key resolved from the bound entry's own
// source, the per-model spec (prompt, profile, window budget) and the projection every Driver fills
// identically, overlaid with the entry-derived keys. What it deliberately lacks is everything only a
// live server or a run about to start can supply: the beat-derived fan-out width and effort dialect,
// the run's scratch dir, the Reaction Runner, the routing and the namer. firingConfig adds those; the
// offline `apogee probe context` (probecontext.go) stops here, because it constructs an Agent that
// sends nothing and must neither dial the beat nor create a scratch dir on the way to its estimate.
type firingBinding struct {
	// cfg is the Config with every binding-derived key filled and every beat-derived key at its
	// zero — a Config an Agent constructs from, not yet one a Firing runs on.
	cfg apogee.Config
	// spec is the per-model resolution the Config was overlaid from, kept because the beat's hint
	// notice and the namer read the model and window it bound.
	spec apogee.RebindSpec
	// apiKey is the bearer token resolved from the bound entry's own key source.
	apiKey string
	// keys is the resolver it was resolved through, kept so the Sub-agent server's entry resolves
	// its own key through the same one (resolveFiringRouting) rather than running an
	// `api-key-cmd:` twice.
	keys *config.KeyResolver
	// notices is the per-model rebind's narration so far — a built-in Model profile announcing
	// itself, the roster delta it carries.
	notices []string
}

// bindFiringConfig composes the firingBinding: the model fallback, the key, the spec, the skill
// catalog, the toolchain probe's start, the shared projection and the entry-derived overlay, in
// firingConfig's own order. It observes nothing and writes nothing — the two facts the offline
// probe relies on — and it fails exactly where firingConfig failed before the extraction: a key
// source that refuses, or a per-model resolution that does.
func bindFiringConfig(in firingInputs) (firingBinding, error) {
	// The bound entry's own `model:` unless the Driver overlaid one. On a launcher-fronted server an
	// empty model is legitimate — it means "whatever is serving" — so this is a fallback, not a
	// default that has to hold a name.
	model := in.model
	if model == "" {
		model = in.entry.Model
	}

	// The bearer token this run sends, resolved from the bound entry's own key SOURCE — the literal,
	// the command's output, or the named variable — exactly as a session resolves it, so one
	// configuration means one credential whichever Driver reads it. A source that refuses fails the
	// run before a single token is spent: an unattended run that degraded to sending no key would
	// put the prompt on the wire unauthenticated and report a 401 as the model's answer.
	//
	// The resolver is hoisted rather than built inside the branch because a second thing asks it: the
	// Sub-agent server's entry resolves its OWN key through the same one (resolveFiringRouting), and
	// two resolvers would run one `api-key-cmd:` twice where the two entries name the same source.
	//
	// The exec fence is judged against THIS Firing's workspace, whatever root the resolver was built
	// with: the daemon holds one resolver across every Schedule it fires, each in the workspace its
	// entry names, so the root that fences an `api-key-cmd:` is a fact of the Firing, not of the
	// resolver — and a key the same command answered for another workspace is refused here all the
	// same when this one holds the program.
	keys := in.keys
	if keys == nil {
		keys = config.NewKeyResolver(in.roots.workspace)
	}
	apiKey := in.apiKey
	if apiKey == "" {
		resolved, err := keys.ResolveWithin(in.entry, in.roots.workspace)
		if err != nil {
			return firingBinding{}, err
		}
		apiKey = resolved
	}

	// The per-model half of the Config, resolved exactly as a rebind resolves it — the system prompt
	// keys on the model (ADR 0023) and so does the model profile (ADR 0044), so a Firing
	// must land in the state a session started on this model and this server would be in.
	//
	// The overlay onto the copy is rebindInputs' own (wire_settings.go), spelled here because two of
	// the three Drivers have no live settings holder to spell it: the endpoint a run RESOLVES
	// against and the endpoint it DIALS must be one value — the Config composed below takes its
	// endpoint off this copy, and any input keyed on the endpoint (the probe record was one, until
	// its startup reader went on 2026-09-16) would otherwise be resolved against the startup
	// server while the run talked to another one. The
	// `response-reserve:` half is the same rule for the share: without it the spec would state the
	// TOP-LEVEL share while the Config below divided the window by the entry's, and one
	// configuration would mean two splits of one window.
	//
	// The observed window is passed as unknown because nothing beats here to observe one; a
	// `context-window:` pin still binds the Budget and an unpinned run leaves it inactive, which for
	// one bounded prompt is the honest degrade rather than a guess.
	specOpts := in.opts
	specOpts.Endpoint = in.entry.Endpoint
	specOpts.APIKey = apiKey
	pinnedWindow := config.ResolveContextWindow(int(in.entry.ContextWindow), in.opts.ContextWindow)
	specOpts.ContextWindow = pinnedWindow
	specOpts.ResponseReserve = config.ResolveResponseReserve(in.entry.ResponseReserve, in.opts.ResponseReserve)
	spec, notices, err := rebindSpecFor(specOpts, in.roots, model, 0, pinnedWindow, in.entry.MaxOutputTokens)
	if err != nil {
		return firingBinding{}, err
	}

	// The share the run actually divides its window by, read back OFF the spec rather than resolved
	// a second time, so the spec and the Config cannot state two different splits. rebindSpecFor
	// always states one — the pointer is its "silence is still expressible" contract, and it never
	// has anything to be silent about — and a nil would mean nobody said, which is the 0 that hands
	// the split back to the engine's own built-in fifth.
	reserve := 0.0
	if spec.ResponseReserveFraction != nil {
		reserve = *spec.ResponseReserveFraction
	}

	// The skill catalog for this run, held in a variable rather than built inline so the SAME
	// provider serves both halves of the skills contract: it resolves an attached ID into the prompt
	// (Config.Skills) AND names the dirs whose files the model may then read (Config.ExtraReadRoots
	// below). A fresh one is per run because the project half of it is per workspace.
	skillProvider := in.skills
	if skillProvider == nil {
		skillProvider = skills.NewProvider(skills.Sources{
			Home:             in.roots.config,
			Workspace:        in.roots.workspace,
			UseProjectSkills: in.opts.UseProjectSkills,
			// Both skill gates come off the SAME resolved options a session reads, so an unattended
			// run's catalog is the session's catalog (ADR 0031's Driver parity). Leaving this one out
			// would hand a Firing the zero value — shipped skills off — and a `/debugging` token in a
			// headless prompt would resolve in the TUI and silently stay prose here.
			UseShippedSkills: in.opts.UseShippedSkills,
		})
	}

	// The toolchain roots' probe, started here for the Drivers that reach this composer with no
	// session before it (headless, the daemon); inside a session it is the no-op second start. It
	// runs in the temp root, never this run's home or workspace (toolchain_roots.go) — the
	// workspace is only what its PATH is scoped out of.
	hostToolchain.start(in.roots.workspace)

	// The keys every Driver fills identically come off the one projection (wire_config.go) — the
	// projection is called after hostToolchain.start above so the read-roots func it composes lists
	// the toolchain roots — and what an unattended run adds on top is the entry it bound to and the
	// observation it took of that server. Confiner and posture are the session's CONFIGURED ones,
	// so an Auto run here is fenced by the same box an Auto session on this configuration would be;
	// the posture is the boot value and not a `/confine` toggled since — that command moves the
	// blast radius on the live engine and nothing mirrors it onto the settings holder, so it never
	// reaches `in.opts`. That is the second named exception to "a Firing sees exactly what the
	// session sees" (ADR 0037, note of 2026-08-25) — a `/confine off` is a per-session act a
	// watching human takes on their own turn, while `/confine off --save`, which writes the host
	// acknowledgement, is what loosens the Firings a LATER session raises.
	//
	// Every file-only key the projection carries is honoured for one reason: it is one
	// configuration, and an unattended run must offer the model the same tools, obey the same host
	// allow/deny lists, scrub the same variables out of a subprocess it chose the contents of, mount
	// the same context files and read responses in the same shape a session on this host would.
	cfg := projectConfig(in.opts, in.roots, in.confiner, in.mode, skillProvider)
	cfg.Endpoint = in.entry.Endpoint
	cfg.Model = spec.Model
	cfg.APIKey = apiKey
	// The bound entry in the HUMAN's own words, for the orientation block to name the SESSION
	// seat by when the model is offered a seat to choose (ADR 0069, wire_server.go's shape). An
	// unattended run needs them for the same reason a session does: the bullet that names the
	// far seat is unreadable beside a near one the model can only call "this server".
	cfg.ServerName = in.entry.Name
	cfg.ServerDescription = in.entry.Description
	// And the protocol the bound entry speaks — its `wire:` key as written (ADR 0078), the same
	// value firingConfig's beat is dialled under, so an unattended run opens the connection a
	// session on this entry opens (ADR 0031's Driver parity).
	cfg.Wire = in.entry.Wire
	// The Model profile the resolution above matched for THIS model (ADR 0044) — off the spec
	// rather than off opts, so the run reads responses in the same shape a session on the same
	// model would, and a built-in match has already narrated itself through the notices.
	cfg.Profile = spec.Profile
	cfg.SystemPrompt = spec.SystemPrompt
	cfg.Context.MaxContextTokens = spec.MaxContextTokens
	// The room inside it this run works in: the bound entry's own `working-window:` over the
	// top-level key (config.ResolveWorkingWindow, the ranks the window pin above spells).
	// Unbounded at both scopes it stays 0 and the run works in the whole advertised window.
	cfg.Context.WorkingWindow = config.ResolveWorkingWindow(in.entry.WorkingWindow, in.opts.WorkingWindow)
	// The `response-reserve:` share the bound entry resolves to, read back off the spec
	// above. Unstated at both scopes it stays 0 and the Budget holds its own built-in fifth
	// back.
	cfg.Context.ResponseReserveFraction = reserve
	// The bound entry's `max-output-tokens:` pin (ADR 0046). Unpinned it stays 0 and the
	// engine derives the cap from its own reply budget — never "no cap", which for an
	// unattended run is precisely the thing a runaway reply must not be able to become.
	cfg.Context.MaxOutputTokens = in.entry.MaxOutputTokens

	return firingBinding{cfg: cfg, spec: spec, apiKey: apiKey, keys: keys, notices: notices}, nil
}

// observeServer is the ONE observation an unattended run takes of a server: the whole Beat, because
// everything the composition needs from discovery comes off it — how many generation slots the
// server reports it was launched with (ADR 0039 decision 2), which wire shape it reads a
// thinking-effort intent in (ADR 0060), and whether it answered at all. It is ONE beat of the very
// Monitor the TUI's heartbeat drives, so an unattended run and a session read the same numbers out
// of the same probes rather than growing a second, subtly different discovery. One beat and no retry
// is the whole contract: a headless run composes once and has no later beat to widen on, so it asks
// once and takes what comes.
//
// It never reports an error, and it replaced two probes that each asked the same server the same
// question at the same moment: a server without /props, an unreachable one, a cancelled context all
// answer the zero Beat, whose slot count ResolveParallelAgents turns into the entry's own default
// width — one for an unkeyed server, four for a keyed one (config.DefaultParallelAgents) — and whose
// dialect is the historical `chat_template_kwargs` shape every unattended run spoke before it
// existed. What the failure MEANS is on the Beat itself (Failure, Answered, Throttled) for the
// Driver that gates on it.
//
// It serves two boxes and is never shared between them: firingConfig beats the run's OWN server
// through it when no Driver hands over a beat (firingInputs.beat), and resolveFiringRouting beats
// the Sub-agent server — its own endpoint, model and key — because a target resolved against the
// primary's observation would route delegations to a box nobody observed. It is a plain function
// rather than a seam: a test that wants a different answer scripts the server it dials
// (internal/stubllm), or hands firingConfig a beat of its own.
//
// wire is the entry's protocol (ADR 0078), carried because the beat IS a discovery and discovery
// differs per wire: the Monitor is dialled with it so an anthropic entry is asked under its own
// headers and never for a /props it does not serve.
func observeServer(ctx context.Context, endpoint, model, apiKey string, wire provider.Wire) heartbeat.Beat {
	return heartbeat.NewMonitor(endpoint, model, apiKey, provider.WithWire(wire)).Beat(ctx)
}

// firingConfig composes the construction surface EVERY unattended run is driven from: one prompt,
// nobody watching, no delegate that assumes a human. It exists because that surface was previously
// spelled out three times — once per Driver — and three copies of a twenty-field literal is three
// chances for one configuration to mean two different runs depending on which Driver read it, which
// is the one thing ADR 0031's benchable-all-the-way-up shape cannot afford.
//
// It composes; it does not decide. The mode gate, the roots, the scratch sweep and every notice a
// Driver prints in its own voice stay with the Driver — what comes
// back is a Config, this run's routing (firingRouting) and the per-model rebind notices, which
// headless prints on stderr, the daemon logs, and the TUI's `/schedule` Driver drops (its narration
// is the session record it leaves behind). The server-bound half — the key, the spec, the shared
// projection and the entry-derived overlay — is bindFiringConfig's, so the offline probe can compose
// the same Config without the beat, the scratch dir and the routing this function adds on top.
//
// Approver, Asker and Presenter are deliberately left nil: run.Once pins its own, and handing it
// any of them is how a run acquires a human it does not have. Events is the ONE exception, and only
// where the Driver built a Reaction Runner (in.hooks): that Runner observes and forwards, so a
// Firing gains no human from it — a Reaction can read what the run did and nothing a Reaction does
// reaches the model, the conversation or the Session record (ADR 0073 §2). With no Runner it stays
// nil, exactly as it was before the key existed. Tools is left nil too and the engine builds its
// own registry — EXCEPT under `sub-agents-choice: model`, where the gate shapes the sub_agent
// SCHEMA rather than anything on the Config the engine reads (ADR 0031), so a Firing that must
// publish `run_on` has to hand over a roster assembled here. Either way a Firing still reaches no
// external MCP server (ADR 0034): the assembled registry carries no MCP tools.
func firingConfig(ctx context.Context, in firingInputs) (apogee.Config, firingRouting, []string, error) {
	bound, err := bindFiringConfig(in)
	if err != nil {
		return apogee.Config{}, firingRouting{}, nil, err
	}
	cfg, spec, apiKey, keys, notices := bound.cfg, bound.spec, bound.apiKey, bound.keys, bound.notices

	// The ONE observation this run takes of the server it is bound to, standing in for the heartbeat
	// an unattended run has none of. Everything discovery can tell this composition comes off it:
	// how wide the run may fan out (below), which wire shape a thinking-effort intent travels in
	// (further below), and whether anything answered at all — which rides out on the routing for the
	// Drivers that refuse a Firing before spending a prompt on a server that is not there.
	//
	// It is unconditional, and that is the change from the two probes it replaced: those were each
	// skipped when the bound entry pinned their answer away, which left a fully pinned entry taking
	// no round trip and therefore observing nothing. The pins still win over the values below — a
	// beat never overrules one — but the call happens, because the call IS the liveness gate.
	observe := in.beat
	if observe == nil {
		observe = observeServer
	}
	beat := observe(ctx, in.entry.Endpoint, spec.Model, apiKey, provider.WireFor(in.entry.Wire))

	// The one thing only that observation can say about the binding above: whether the server this run
	// is about to prompt advertises the model it just bound. A session says it at its rebind seam out of
	// the same two values (wire_verbs.go — the grade discovery reached the id by, and the window it saw),
	// and this is the unattended half of the same sentence. It joins the notices this composition already
	// returns rather than printing itself: headless puts them on stderr and the daemon logs them, so both
	// Drivers gain the hint by reading a channel they already read (ADR 0031's Driver parity).
	//
	// The bound window handed over is the spec's, which on this path is the `context-window:` pin or
	// nothing — rebindSpecFor is passed an observed window of 0 above, deliberately, so an unpinned
	// Firing leaves the Budget inactive rather than binding a per-slot number. An unpinned run therefore
	// says the window is unknown and a pinned one names the pin, while a session's own clause can credit
	// the base entry for a window it actually bound. The two sentences differ because the two Drivers
	// bind differently; aligning them would mean changing what a Firing binds.
	//
	// The else is the whole no-double-say rule. hintNotice's own default branch already carries an
	// unknown-window clause, and it is reached exactly when the bound window is 0 — so an
	// unadvertised unpinned Firing says it once, inside the hint, and an ADVERTISED unpinned one,
	// which composes no hint at all, gets the bare sentence instead. It is gated on the beat too:
	// both unattended Drivers emit these notices BEFORE their offline gate, and a beat that never
	// answered carries a zero Resolution, so an unpinned run against a dead endpoint would
	// otherwise announce an unknown window ahead of "cannot send — server offline" and, in the
	// daemon, burn the once-per-process latch on a Firing that never ran.
	if hint := hintNotice(spec.Model, beat.Resolution, beat.ContextWindow, spec.MaxContextTokens); hint != "" {
		notices = append(notices, hint)
	} else if spec.MaxContextTokens == 0 && beat.Answered {
		notices = append(notices, notice.WindowUnknown)
	}

	// How wide this run may fan its delegations out — the same cap a session resolves, so every
	// Driver reaches the same engine behaviour (ADR 0031; the resolution itself is ADR 0039 decision
	// 2). The pin is the BOUND entry's own `parallel-agents:`, and ResolveParallelAgents never lets
	// what the beat saw overrule it; a keyed entry that neither pins nor advertises a width runs
	// four (config.DefaultParallelAgents), an unkeyed one runs one.
	slots := beat.TotalSlots

	// The wire shape this run expresses a thinking-effort intent in (ADR 0060). A session takes it
	// off the beat that lands every Interval and commits it through Rebind; an unattended run never
	// rebinds, so the value has to be STATED on the construction surface or the engine sends the
	// zero dialect — the historical `chat_template_kwargs` shape — whatever the bound server
	// actually reads. That was the Driver-parity break ADR 0031 rules out (2026-08-25 audit C-03).
	//
	// The bound entry's forced `effort-dialect:` ranks first: a forced dialect is already the
	// answer, and the beat above is not consulted for it. With nothing forced, what that one beat
	// saw stands in, and a server with no tell answers the zero, which is what an unattended run
	// has always sent.
	effortDialect := provider.EffortDialectFor(in.entry.EffortDialect)
	if effortDialect == provider.EffortDialectNone {
		effortDialect = beat.EffortSupport.Dialect
	}

	// This run's own scratch dir, named after the record it will be saved under (wire.go): the
	// model is offered writable scratch INSIDE the box rather than putting its working files
	// wherever else it can reach, and the dir is reclaimed on the same 14-day schedule a
	// session's own dir is. Per run rather than per Driver, so two Firings on the same minute
	// stay out of each other's files. Minted once, here, because it is BOTH the construction
	// seed the box fences writable and the read root the read tools reach it back through — a
	// Firing never moves it, so the live func below answers the one dir the run announced.
	scratchDir := ensureScratchDir(in.roots.scratch, in.recordID)
	cfg.ScratchDir = scratchDir
	// And the same dir as the read root the read tools reach it back through: a Firing's model
	// is told the dir is writable exactly as a session's is, and must be able to read what it
	// wrote there (ADR 0031's Driver parity).
	cfg.ScratchReadRoot = func() string { return scratchDir }
	// The dialect resolved above, spelled in the domain's mirror of the provider vocabulary —
	// the same five words on this side of the boundary (internal/agent's toProviderDialect
	// converts them back at the wire seam, where the provider package holds no domain import).
	cfg.EffortDialect = domain.EffortDialect(effortDialect)
	cfg.ParallelAgents = config.ResolveParallelAgents(in.entry.ParallelAgents, slots,
		config.DefaultParallelAgents(in.entry))

	// The Reaction Runner this Driver built for this ONE Firing, installed as the run's Event sink
	// (ADR 0073 §2). It is assigned after the literal rather than inside it because a nil
	// *reactions.Runner boxed into the domain.EventSink interface is a NON-nil interface holding a nil
	// pointer, and run.Once's own tap would then wrap a sink that panics on the first Event. A Driver
	// that built no Runner leaves Events nil, which is what every composition test asserts.
	//
	// It is the INNERMOST sink of the run: run.Once wraps whatever it is handed (eventTap), and the
	// one Driver that renders an Event itself wraps this Runner in turn (headless's prune notice),
	// so the renderer stays outermost and the Reactions stay invisible to it.
	if in.hooks != nil {
		cfg.Events = in.hooks
	}
	// And the in-loop twin of that Runner's own report seam: where the SYNC lane's failures are
	// said out loud. It is assigned beside the sink rather than in the literal so the two stay one
	// decision — a Driver that narrates a Reaction's trouble narrates it for both lanes, in one
	// voice — and a Driver that passed neither leaves both nil, which is what a bare composition is.
	if in.report != nil {
		cfg.Report = in.report
	}

	// Where this run's delegations go, resolved off the same `sub-agents-server:` key a session
	// resolves and handed back for the Driver to latch through run.Spec. Every way it can fail
	// leaves the run unrouted with a notice — never an error.
	routing, routingNotice := resolveFiringRouting(ctx, in, keys)
	if routingNotice != "" {
		notices = append(notices, routingNotice)
	}
	// And this run's own observation of its PRIMARY server, latched onto the routing AFTER it comes
	// back rather than written inside resolveFiringRouting: that function returns a bare
	// firingRouting{} on the default no-`sub-agents-server:` path and on every failure path, so a
	// field set inside it would read false for every ordinary Firing and a Driver gating on it would
	// refuse every run.
	routing.Beat = beat
	routing.Reachable = beat.Failure == ""

	// And the namer an unnamed delegation is named by (ADR 0068), so a Firing's saved record reads
	// the same way a session's transcript does rather than carrying a wall of task first lines
	// (ADR 0031's Driver parity). Both of its Upstreams are constants for the run — the run's own
	// server, and the Sub-agent server resolved just above when one was named — because an unattended
	// run has no live door to move either through; the gate is `auto-title:` as it stood at startup
	// for the same reason.
	cfg.Namer = newFiringNamer(
		upstreamBinding{Endpoint: in.entry.Endpoint, Model: spec.Model, APIKey: apiKey},
		effortDialect, routing.target, in.opts.AutoTitle)

	// And the one thing an unattended run cannot get from the engine: the `run_on` argument on
	// sub_agent. `sub-agents-choice:` shapes the published schema rather than any Config field (ADR
	// 0031 — the engine reads no config), so under `model` the roster is assembled HERE, with the
	// gate on and no MCP tools (a Firing reaches no MCP server, ADR 0034). The gate is read off the
	// Options every Driver already fills rather than off a firingInputs field of its own, so a
	// Driver that filled opts cannot silently leave the seat unpublished.
	//
	// Under `fixed`, and with the key absent, Tools stays nil byte-for-byte and run.Once's own nil
	// Asker and Presenter go on shaping the engine's roster exactly as before. That is the guard,
	// not an optimisation: a registry handed over on the default path would decide the roster from
	// this Config's delegates rather than from the ones the runner pins.
	if in.opts.SubAgentsChoice == config.SubAgentsChoiceModel {
		cfg.Tools = registryWithMCP(in.roots.workspace, cfg, true, nil)
	}

	return cfg, routing, notices, nil
}

// firingRouting is where ONE unattended run's delegations go, what its model is told about that far
// seat, and what the run observed of its OWN server on the way there. The first two travel together
// for delegationSetter's reason (delegation.go): one thing decides both — which `servers:` entry this
// run delegates to — and a caller that carried one without the other could route children to a box
// the orientation block never named, or name a box nothing routes to.
//
// The observation rides along because it is the other thing firingConfig learns that no Driver can
// re-derive without spending a second round trip: the composition takes exactly one beat of the
// primary server, and a Driver that must refuse a Firing rather than send a prompt into a dead
// endpoint reads it off here. It is the PRIMARY server's — never the Sub-agent server's, which
// resolveFiringRouting observes separately (observeServer), because the two are different boxes
// with different keys.
//
// Both zero is the DEFAULT and the floor: no `sub-agents-server:` key, or a key that resolved to
// nothing, leaves the run exactly as every Firing was before it could route at all — children on the
// run's own Upstream, one seat in the orientation block (ADR 0045 §4).
//
// It is a value rather than two returns because a Driver hands it straight on: run.Spec has a field
// for each (internal/run), and the pair is what a Driver forwards rather than something it reads.
type firingRouting struct {
	// target is the Sub-agent server every delegation this run spawns is built against — endpoint,
	// key, model, window, fan-out width, profile and posture, resolved WHOLE here exactly as the
	// TUI's second heartbeat resolves it (resolveDelegationTarget). nil ⇒ nothing is routed.
	target *apogee.DelegationTarget
	// seat is what the orientation block TELLS the model about that far seat — the host's own words
	// for the box (ADR 0069). It is installed whenever the named ENTRY resolved, reachable or not:
	// the seat is display text a human wrote down, while reachability is a fact about right now that
	// a delegation's own result note reports (delegationSeatOf).
	seat *apogee.DelegationSeat
	// Beat is the ONE observation this run took of its own bound server — the whole Beat rather
	// than the two fields the composition read off it, so a Driver acting on it can say WHY
	// (Beat.Failure) and tell a box that is merely rate-limited (Beat.Throttled, Beat.Answered)
	// from one that is not there. The zero value is "nothing observed", which is what a Driver that
	// handed over its own beat with an empty Failure gets back unchanged.
	Beat heartbeat.Beat
	// Reachable is Beat.Failure == "", spelled once here so the Drivers that gate on it cannot
	// each re-derive it from a different field. It says the server handed back a usable model list;
	// Beat.Answered is the weaker, and for a refusal the more honest, question of whether anything
	// answered at all.
	Reachable bool
}

// resolveFiringRouting answers where an unattended run's delegations go, taking ONE beat against the
// named Sub-agent server to find out — the routing a session gets from a second heartbeat, without
// the live machinery an unattended run has nowhere to put (ADR 0045).
//
// It reads the `sub-agents-server:` name and the `servers:` list off in.opts rather than off
// firingInputs fields of their own, deliberately: both are already there, and a Driver that filled
// the Options while omitting a routing field would silently downgrade a routed run to an unrouted one
// with nothing to compile against. The session Driver's projection mirrors the LIVE name onto that
// same key (liveSettings.subAgentsServer), so a `/sub-agents-server` retarget follows the Firings the
// session raises afterwards.
//
// Nothing here is an error. Every way routing can fail to resolve — no key at all, a name the list
// does not carry, a key source that would not answer, a server that is unreachable or has no model bound — leaves the target nil, the run
// delegating to its own Upstream, and ONE notice saying so (delegationStateNotice). That is the same
// visible degrade a session takes (ADR 0042), and the reason is stronger here: a Firing runs while
// nobody is watching, so refusing to start over a grunt box that is merely down would turn a
// scheduled run into a silent gap in the record.
func resolveFiringRouting(
	ctx context.Context,
	in firingInputs,
	keys *config.KeyResolver,
) (firingRouting, string) {
	name := in.opts.SubAgentsServer
	if name == "" {
		// The default: no key, nothing observed, nothing said. No beat is taken either — a run that
		// delegates to its own server has no second box to ask about.
		return firingRouting{}, ""
	}
	entries := in.opts.Servers
	entry, found := config.SubAgentsServerTarget(entries, name)
	if !found {
		return firingRouting{}, missingNameNotice(name, entries)
	}

	// The same build a session's startup and its config reloads go through, so a routed Firing
	// assembles its seat exactly as a session does.
	server := newSubAgentServer(entry)
	// The far seat, installed on the ENTRY rather than on the observation below: the words are the
	// human's and they do not move when the box does (ADR 0069, delegationSeatOf).
	seat := delegationSeatOf(server)

	// That entry's OWN key source — never the run's, which authenticates against another server —
	// fenced against this Firing's workspace the way the run's own is (firingConfig).
	apiKey, err := keys.ResolveWithin(entry, in.roots.workspace)
	if err != nil {
		return firingRouting{seat: seat}, delegationStateNotice(name, nil, "", err)
	}

	// One beat, no retry: the composition happens once and there is no later beat to widen on, which
	// is the contract observeServer already set for an unattended run's own server. It is the SAME
	// one-liner and never the same observation: this beats the Sub-agent server's own endpoint,
	// model and key, so the target below is resolved against the box it will actually dial. It
	// fires ONLY when `sub-agents-server:` names an entry — a run that delegates to its own server
	// asks nothing here, so the default composition path costs no third round trip — and an
	// unreachable server, a cancelled context and a server with nothing bound are all "no target",
	// which leaves the run unrouted: the fallback every Firing took before routing existed (ADR
	// 0045 §4's floor).
	observed := observeServer(ctx, entry.Endpoint, entry.Model, apiKey, provider.WireFor(entry.Wire))
	// And the one resolution the session's own beat lands, reused whole rather than re-derived: the
	// pin-else-observe ladder is ADR 0045 decision 4, and a second copy of it is how one Driver ends
	// up routing to a window the other would not.
	target := resolveDelegationTarget(entry, apiKey, observed, in.opts.ModelProfiles)
	return firingRouting{target: target, seat: seat},
		delegationStateNotice(name, target, "", nil)
}

// firingHooks builds the Reaction Runner ONE unattended run fires through (ADR 0073). What it arms
// is the OBSERVE half of the generation the root resolved (ADR 0076 A8) — for a Firing a live
// session raises, the rows a `reactions:` apply last installed, since a Firing is composed out of
// the session's live options rather than the file it launched with.
//
// Every Firing root composes a Runner of its own — `apogee headless`, a daemon tick, the
// `/schedule` picker inside a live session — because a Runner per Firing is what carries the
// Schedule a run belongs to onto every payload it stamps, with no per-event plumbing to carry it
// there.
//
// It is one constructor rather than three literals for firingConfig's own reason: everything except
// the four arguments is the same at every root, and three copies of it is three chances for one
// `reactions:` list to mean two different things depending on which Driver read it.
//
// The caller closes what comes back — [hookCloseGrace], the same grace the session gives (wire.go)
// — and a returned error fails the Firing: a `reactions:` list this root cannot resolve is
// structural configuration, exactly as an unreadable prompt is.
func firingHooks(observe []domain.Reaction, workspace string, sched *reactions.ScheduleRef, report func(string)) (*reactions.Runner, error) {
	return reactions.New(observe, reactions.Options{
		// Inner stays nil: a Firing's Config carries no sink of its own (firingConfig), so there is
		// nothing underneath this Runner to forward to. The one Driver that renders an Event itself
		// wraps THIS Runner rather than being wrapped by it (headless's prune notice), which keeps the
		// renderer outermost and the Reactions invisible to it.
		Workspace: workspace,
		Schedule:  sched,
		Report:    report,
		Exec:      reactions.DefaultExecutor(workspace),
	})
}

// The two stages at which raise can refuse a Firing before it starts, carried on errNotStarted so
// a Driver can tell WHICH refusal it is reporting without parsing the sentence: the composition
// would not produce a Config, or it did and the bound server answered nothing.
const (
	stageCompose = "compose"
	stageOffline = "offline"
)

// errNotStarted is raise's refusal of a Firing that never began: nothing was sent, nothing was
// saved, and the sentence inside it is the whole of what the Driver has to say. It is typed rather
// than sentinel because the Drivers act on the CLASS and not on the sentence — headless maps every
// errNotStarted to its never-started exit code (exitNotStarted), whatever the Stage; the daemon
// wraps a "compose" refusal under its own schedule-named log line and passes an "offline" one bare
// — and Error is the wrapped sentence verbatim, with no prefix of this type's own, so the exact
// wording a Driver prints (notice.ServerOffline above all) stays the composer's and the tests that
// pin it keep comparing whole lines.
type errNotStarted struct {
	// Stage is stageCompose or stageOffline: which of raise's two gates refused.
	Stage string
	// Err is the refusal itself, as the composer or the gate worded it.
	Err error
}

// Error reports the wrapped sentence verbatim.
func (e errNotStarted) Error() string { return e.Err.Error() }

// Unwrap exposes the wrapped refusal so errors.Is/As see straight through the stage.
func (e errNotStarted) Unwrap() error { return e.Err }

// firingRefusal is what a Driver that lands a refused Firing as a FAILED one reports: the one
// stage map the daemon's fire and a session's fire share, so the two Drivers tell one refusal in
// one shape. A composition refusal is wrapped under the Driver's own prefix — the daemon names the
// schedule that would not compose, a session names the firing — with `%w` keeping the composer's
// error reachable, so a supervisor reading a journal days later sees which entry failed and a
// caller can still errors.Is through it. The gate's refusal passes BARE: its sentence is the one
// the TUI shows (internal/tui/heartbeat.go's upstreamBlockNote) and the one `apogee headless`
// prints, because all three read it from one composer, notice.ServerOffline — so an edit to the
// wording belongs in internal/notice and nowhere else.
//
// prefix is the Driver's clause up to the noun the refusal is about ("apogee: resolve the
// firing's"); the helper supplies the rest so the two Drivers cannot drift apart on it.
func firingRefusal(prefix string, refused errNotStarted) error {
	if refused.Stage == stageCompose {
		return fmt.Errorf("%s reactions/bindings: %w", prefix, refused.Err)
	}
	return refused
}

// partialRunSuffix names the record a FAILED Firing salvaged, in the one wording every Driver's
// failure carries: run.Once saves whatever completed before it stopped, and naming that record
// is what lets a human open the interrupted run rather than guess at it. Callers append it to
// the failure with a space (`fmt.Errorf("%w %s", err, partialRunSuffix(id))`), so the error
// chain stays reachable and the sentence reads identically on the daemon's journal, a session's
// Firing block and the headless stderr.
func partialRunSuffix(id string) string {
	return fmt.Sprintf("(partial run saved as %s)", id)
}

// clockNow adapts a Scheduler's Clock to the func the Firing mints its id from (firingInputs.now).
// nil in, nil out: daemonClock and tuiScheduleClock are nil in production, and taking `.Now` as a
// method value on a nil interface panics at the point of evaluation — on every production Firing.
// A nil result leaves firingInputs.now at its own nil ⇒ time.Now default, which is the same wall
// clock the scheduler falls back to (internal/schedule's unexported systemClock).
func clockNow(c schedule.Clock) func() time.Time {
	if c == nil {
		return nil
	}
	return c.Now
}

// raise is the ONE act every unattended Firing is: it takes what a Driver decided (firingInputs, the
// prompt, the Schedule the run belongs to, the store its record lands in) and does, in this order,
// everything the three Drivers used to spell out for themselves — divides the `reactions:` list
// into its lanes (ADR 0076 A8), builds this Firing's own Reaction Runner and drains it when the
// Firing ends (ADR 0073), mints the record id, composes the Config (firingConfig), refuses a server
// that answered nothing (notice.ServerOffline), lets the Driver decorate the Event sink, and runs
// the Firing once through its runner (in.runner, or the package's runOnce seam when nil). It exists
// because those steps were three copies that had already drifted: the id that named a record and
// the id that named its scratch dir were two mints in two Drivers rather than one by construction,
// and the liveness gate was a sentence each Driver re-derived from the routing.
//
// It stays in cmd/apogee rather than moving into internal/run for ADR 0033 decision 6's reason: the
// runner is runner-agnostic and the caller composes. What raise composes is the host's business —
// Reactions, keys, skills, the beat — and what it never decides is the Driver's: the mode gate, the
// roots, the sweeps and every notice a Driver prints in its own voice all happen before it is called.
//
// The id is minted HERE and nowhere else, which is the by-construction guarantee: firingConfig
// creates the scratch dir under in.recordID and the runner files the record under run.Spec.RecordID,
// and both read the one value raise wrote. It is minted off in.now — the same clock handed to the runner
// as run.Spec.Now — so the id's timestamp prefix and the record's CreatedAt are one instant, not a
// wall-clock reading here and a second one inside run.Once. onID, when non-nil, sees that id immediately — before the
// composition and before the gate — so a Driver that stamps it on a stream (headless's Event lines,
// ADR 0075 decision 5) stamps it on a refusal's closing frame as well.
//
// narrate, when non-nil, is handed the sink the Config carries — the Runner, or nil where the Driver
// built none — and returns the sink the run is driven with; it is called AFTER the gate, so a Driver
// that announces the run from inside it (headless's opening frame) announces one that is committed
// to. The notices are returned on EVERY path, the refusals included: a per-model rebind that had
// something to say said it before the gate ran, and a Driver prints them before it reads the error.
//
// A refusal before the run is an errNotStarted, stageCompose for a Config that would not compose
// (a `reactions:` list this root cannot resolve counts — it is structural configuration, exactly as
// an unreadable prompt is) and stageOffline for the gate. That gate is Beat.Answered and nothing
// else: false ONLY for a transport-level failure — a refused dial, a timeout, a DNS or TLS failure —
// never for a server that answered something this host has no standing to judge; a 401, a 500 and a
// 429 all ANSWER and keep the proceed-and-degrade every Firing has always had (internal/heartbeat).
// The round trip was already taken by the composition, so the question costs nothing of its own.
// Whatever the runner returns passes through untouched: a run that started is the Driver's to report,
// exit code and all.
//
// The Reaction drain is deferred here, so it runs BEFORE this function returns — a Firing refused
// by either gate takes its workers down with it (a daemon runs for weeks, and a Runner leaked per
// refused tick accumulates), and a Driver's own summary of a finished run prints after the last
// Reaction has had its grace (hookCloseGrace). Close's own error is discarded: it says only that a
// Reaction was killed at the deadline, the drop totals it wanted to report have already gone to the
// Driver's report line, and the run is over either way.
func raise(
	ctx context.Context,
	in firingInputs,
	prompt string,
	ref *reactions.ScheduleRef,
	store *session.Store,
	onID func(recordID string),
	narrate func(recordID string, cfg apogee.Config, sink domain.EventSink) domain.EventSink,
) (run.Result, []string, error) {
	observeReactions, syncReactions := domain.SplitLanes(in.opts.Reactions)
	hookRunner, err := firingHooks(observeReactions, in.roots.workspace, ref, in.report)
	if err != nil {
		return run.Result{}, nil, errNotStarted{Stage: stageCompose, Err: err}
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), hookCloseGrace)
		defer cancel()
		_ = hookRunner.Close(closeCtx)
	}()
	in.hooks = hookRunner

	now := in.now
	if now == nil {
		now = time.Now
	}
	in.recordID = session.NewID(now())
	if onID != nil {
		onID(in.recordID)
	}

	cfg, routing, notices, err := firingConfig(ctx, in)
	if err != nil {
		return run.Result{}, notices, errNotStarted{Stage: stageCompose, Err: err}
	}
	if !routing.Beat.Answered {
		return run.Result{}, notices, errNotStarted{
			Stage: stageOffline,
			Err:   errors.New(notice.ServerOffline(in.entry.Endpoint, routing.Beat.Failure)),
		}
	}
	if narrate != nil {
		cfg.Events = narrate(in.recordID, cfg, cfg.Events)
	}

	spec := run.Spec{
		Config:   cfg,
		Prompt:   prompt,
		Store:    store,
		RecordID: in.recordID,
		// The clock the id above was minted from, so CreatedAt and the id prefix agree.
		Now: now,
		// The sync half of the `reactions:` list this Firing resolved, armed on the Agent run.Once
		// builds before its first Step: a `gate:` answers this run's very first tool call, and its
		// trouble reaches the same report line the Runner's does (Config.Report, firingConfig).
		Sync: syncReactions,
		// The routing the composer resolved, latched through run.Spec's own seam (internal/run): a
		// Firing delegates to the `sub-agents-server:` entry exactly as a session does, and both
		// fields are nil when no key named one — the unrouted floor every Firing had before.
		DelegationTarget: routing.target,
		DelegationSeat:   routing.seat,
	}
	if ref != nil {
		spec.ScheduleID, spec.ScheduleName = ref.ID, ref.Name
	}
	// Resolved HERE and not at construction, so a caller that left it nil gets whatever the seam
	// holds when the Firing is raised — the production runner, or the double a test installed.
	runner := in.runner
	if runner == nil {
		runner = runOnce
	}
	res, err := runner(ctx, spec)
	return res, notices, err
}

// firingOutcome is what a raised Firing reports to the scheduler: everything the run learned about
// itself, mapped onto [schedule.Outcome] in ONE place so every Driver's Firing tells the same
// story from the same fields. The library reads none of it — it is runner-agnostic (ADR 0033) and
// never imports the runner's shapes — and a Driver renders the Firing from these fields alone: the
// answer without decoding a record, the counts without a second seam onto the run.
//
// It carries the context-file ANOMALIES and only those — a file present but unreadable, standing
// content past its Budget share — because the plain loaded-files line is a launch's narration and a
// Firing's narration is the record it leaves behind (contextAnomalies, schedule.go). The text
// crosses as plain data: internal/notice composes, the surface that renders it strips at its own
// seam. What the run CHANGED on disk crosses whole (run.Result.Wrote, the list writtenFilesLines
// renders), and beside it the revert those changes can still have — the exact command, under the
// exact gate the printed offer applies (undoCommand, headless.go), so a surface that renders the
// Outcome offers the same verb the daemon's log and the headless stderr do. On a failure that
// still produced a Result the fields are the salvage; on a failure carrying a ZERO run.Result they
// are all empty, which is the shape a refusal-free failure with nothing to report has always had.
func firingOutcome(res run.Result) schedule.Outcome {
	return schedule.Outcome{
		RecordID:         res.SessionID,
		Title:            res.Title,
		FinalText:        res.FinalText,
		Turns:            res.Turns,
		Denied:           res.Denied,
		Faulted:          res.Faulted,
		Fault:            res.Fault,
		ContextAnomalies: contextAnomalies(res.ContextFiles),
		TotalTokens:      firingSpend(res),
		SubAgents:        len(res.SubAgents),
		Wrote:            res.Wrote,
		UndoCommand:      undoCommand(res),
	}
}
