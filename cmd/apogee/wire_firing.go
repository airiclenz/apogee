package main

import (
	"context"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/provider"
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
	// manualIDs are the validated `mechanisms:` keys this host spelled out. Validation stays the
	// Driver's for the same reason: it reports a typo'd key at the surface the human typed it on.
	manualIDs []apogee.MechanismID
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
	// beat is this run's ONE observation of the server it is bound to; nil takes discoverBeat, the
	// one-shot beat standing in for the heartbeat an unattended run has none of. It is one seam
	// rather than the two probes it replaced — a width and a dialect asked separately — because
	// both answers come off the SAME observation of the SAME endpoint, model and key: two probes
	// were two round trips that could straddle a restart and report a server that never existed in
	// one state. It also carries the liveness the unattended Drivers refuse a Firing on, which is
	// why it is taken even when the entry pins both values away: the round trip IS the gate.
	//
	// A session passes its own beat instead — the width it already resolves and the dialect its
	// heartbeat already observed, with an empty Failure — because it is holding the answer, and a
	// Firing must not spend a round trip re-asking the server the session is talking to (design
	// call 4).
	beat func(ctx context.Context, endpoint, model, apiKey string) heartbeat.Beat
	// recordID is the id this run's record is filed under, minted by the Driver because the Driver
	// is what hands it to the runner. The run's scratch dir is created under it, so a saved run and
	// the working files its model left behind are one thing to find and one thing to sweep.
	recordID string
}

// firingConfig composes the construction surface EVERY unattended run is driven from: one prompt,
// nobody watching, no delegate that assumes a human. It exists because that surface was previously
// spelled out three times — once per Driver — and three copies of a twenty-field literal is three
// chances for one configuration to mean two different runs depending on which Driver read it, which
// is the one thing ADR 0031's benchable-all-the-way-up shape cannot afford.
//
// It composes; it does not decide. The mode gate, the roots, the `mechanisms:` validation, the
// scratch sweep and every notice a Driver prints in its own voice stay with the Driver — what comes
// back is a Config, this run's routing (firingRouting) and the per-model rebind notices, which
// headless prints on stderr, the daemon logs, and the TUI's `/schedule` Driver drops (its narration
// is the session record it leaves behind).
//
// Events, Approver, Asker and Presenter are deliberately left nil: run.Once pins its own, and
// handing it any of them is how a run acquires a human it does not have. Tools is left nil too and
// the engine builds its own registry — EXCEPT under `sub-agents-choice: model`, where the gate
// shapes the sub_agent SCHEMA rather than anything on the Config the engine reads (ADR 0031), so a
// Firing that must publish `run_on` has to hand over a roster assembled here. Either way a Firing
// still reaches no external MCP server (ADR 0034): the assembled registry carries no MCP tools.
func firingConfig(ctx context.Context, in firingInputs) (apogee.Config, firingRouting, []string, error) {
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
	keys := in.keys
	if keys == nil {
		keys = config.NewKeyResolver(in.roots.workspace)
	}
	apiKey := in.apiKey
	if apiKey == "" {
		resolved, err := keys.Resolve(in.entry)
		if err != nil {
			return apogee.Config{}, firingRouting{}, nil, err
		}
		apiKey = resolved
	}

	// The per-model half of the Config, resolved exactly as a rebind resolves it — the system prompt
	// keys on the model (ADR 0023) and so does the validated Mechanism set (ADR 0016), so a Firing
	// must land in the state a session started on this model and this server would be in.
	//
	// The overlay onto the copy is rebindInputs' own (wire_settings.go), spelled here because two of
	// the three Drivers have no live settings holder to spell it: the endpoint a run RESOLVES
	// against and the endpoint it DIALS must be one value, or every input keyed on the endpoint —
	// the probe record behind the identity ladder, and so the Validated-set decision above it —
	// would be resolved against the startup server while the run talked to another one. The
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
	spec, notices, err := rebindSpecFor(specOpts, in.roots, in.manualIDs, model, 0, pinnedWindow, in.entry.MaxOutputTokens)
	if err != nil {
		return apogee.Config{}, firingRouting{}, nil, err
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
		observe = discoverBeat
	}
	beat := observe(ctx, in.entry.Endpoint, spec.Model, apiKey)

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
	if notice := hintNotice(spec.Model, beat.Resolution, beat.ContextWindow, spec.MaxContextTokens); notice != "" {
		notices = append(notices, notice)
	}

	// How wide this run may fan its delegations out — the same cap a session resolves, so every
	// Driver reaches the same engine behaviour (ADR 0031; the resolution itself is ADR 0039 decision
	// 2). The pin is the BOUND entry's own `parallel-agents:`, and ResolveParallelAgents never lets
	// what the beat saw overrule it.
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

	cfg := apogee.Config{
		Endpoint: in.entry.Endpoint,
		Model:    spec.Model,
		APIKey:   apiKey,
		// The bound entry in the HUMAN's own words, for the orientation block to name the SESSION
		// seat by when the model is offered a seat to choose (ADR 0069, wire_server.go's shape). An
		// unattended run needs them for the same reason a session does: the bullet that names the
		// far seat is unreadable beside a near one the model can only call "this server".
		ServerName:        in.entry.Name,
		ServerDescription: in.entry.Description,
		Mode:              in.mode,
		Bypass:            in.opts.Bypass,
		ConfigDir:         in.roots.config,
		WorkspaceDir:      in.roots.workspace,
		// This run's own scratch dir, named after the record it will be saved under (wire.go): the
		// model is offered writable scratch INSIDE the box rather than putting its working files
		// wherever else it can reach, and the dir is reclaimed on the same 14-day schedule a
		// session's own dir is. Per run rather than per Driver, so two Firings on the same minute
		// stay out of each other's files.
		ScratchDir: ensureScratchDir(in.roots.scratch, in.recordID),
		// Confiner and posture as the session's CONFIGURED one, so an Auto run here is fenced by the
		// same box an Auto session on this configuration would be. The posture is the boot value and
		// not a `/confine` toggled since: that command moves the blast radius on the live engine and
		// nothing mirrors it onto the settings holder, so it never reaches `in.opts`. That is the
		// second named exception to "a Firing sees exactly what the session sees" (ADR 0037, note of
		// 2026-08-25) — a `/confine off` is a per-session act a watching human takes on their own
		// turn, while `/confine off --save`, which writes the host acknowledgement, is what loosens
		// the Firings a LATER session raises.
		Confiner:           in.confiner,
		ConfineToWorkspace: in.opts.ConfineToWorkspace,
		// The dialect resolved above, spelled in the domain's mirror of the provider vocabulary —
		// the same five words on this side of the boundary (internal/agent's toProviderDialect
		// converts them back at the wire seam, where the provider package holds no domain import).
		EffortDialect:     domain.EffortDialect(effortDialect),
		WebSearchEndpoint: in.opts.WebSearchEndpoint,
		// Every file-only key from here down is honoured for one reason: it is one configuration,
		// and an unattended run must offer the model the same tools, obey the same host allow/deny
		// lists, scrub the same variables out of a subprocess it chose the contents of, mount the
		// same context files and read responses in the same shape a session on this host would.
		// `ui.inspector:` too — this run has no /inspect pane to show the capture in, but its sink
		// sees the WireEvents like any other, which is the benchable-all-the-way-up shape (ADR 0031).
		DisabledTools: in.opts.ToolsDisabled,
		EnabledTools:  in.opts.ToolsEnabled,
		URLAllowHosts: in.opts.URLAllowHosts,
		URLDenyHosts:  in.opts.URLDenyHosts,
		Inspector:     in.opts.UI.Inspector,
		SecretEnvVars: config.APIKeyEnvNames(in.opts),
		// The Model profile the resolution above matched for THIS model (ADR 0044) — off the spec
		// rather than off opts, so the run reads responses in the same shape a session on the same
		// model would, and a built-in match has already narrated itself through the notices.
		Profile:      spec.Profile,
		SystemPrompt: spec.SystemPrompt,
		ContextFiles: in.opts.ContextFiles,
		Skills:       skillProvider,
		// And the same provider behind the model-facing door (ADR 0065 §6): an unattended run gets
		// load_skill exactly as a session does, so a Firing's model can reach a written procedure
		// without a human there to attach one (ADR 0031's Driver parity).
		SkillLookup: skillProvider,
		// The same read-only mounts a session gets: the model can read the bundled files of a skill
		// it was given exactly as an interactive one can. Sub-agents inherit them through the tool
		// instances a Subset carries, so no per-child wiring exists.
		// ReadRoots, like the session's own mount, is the resolved-path view of the same sources —
		// a workspace anchor that is a symlink out of the workspace is dropped rather than mounted
		// (audit 2026-08-25 F-13), and it stays a method value so the mount follows SetSources.
		ExtraReadRoots: skillProvider.ReadRoots,
		// And the mount a shipped skill's bundled files are served through, so an unattended run
		// reads the `shipped:<id>` address its own injected block announces exactly as a session
		// does (ADR 0031's Driver parity).
		VirtualReadRoots: skillProvider.VirtualReadRoots,
		EnableMechanisms: spec.EnableMechanisms,
		ParallelAgents:   config.ResolveParallelAgents(in.entry.ParallelAgents, slots),
		Context: apogee.ContextConfig{
			MaxContextTokens: spec.MaxContextTokens,
			// The room inside it this run works in: the bound entry's own `working-window:` over the
			// top-level key (config.ResolveWorkingWindow, the ranks the window pin above spells).
			// Unbounded at both scopes it stays 0 and the run works in the whole advertised window.
			WorkingWindow: config.ResolveWorkingWindow(in.entry.WorkingWindow, in.opts.WorkingWindow),
			// The `response-reserve:` share the bound entry resolves to, read back off the spec
			// above. Unstated at both scopes it stays 0 and the Budget holds its own built-in fifth
			// back.
			ResponseReserveFraction: reserve,
			// The bound entry's `max-output-tokens:` pin (ADR 0046). Unpinned it stays 0 and the
			// engine derives the cap from its own reply budget — never "no cap", which for an
			// unattended run is precisely the thing a runaway reply must not be able to become.
			MaxOutputTokens:   in.entry.MaxOutputTokens,
			CompactionEnabled: in.opts.AutoCompact,
			// And the `prune-tool-results:` toggle beside it, so a scheduled Firing prunes stale
			// tool results the way the session it was raised from does.
			PruneToolResults: in.opts.PruneToolResults,
		},
		// The `delegate-max-steps` bound on a sub-agent's Exchange (default 80; 0 ⇒ unbounded).
		// A Firing runs while nobody watches, which is exactly the case a runaway delegation
		// must not be able to become.
		Delegation: apogee.DelegationConfig{MaxSteps: in.opts.DelegateMaxSteps},
		// The six Floor-guard gates the session resolved, negated at the one seam that negates them
		// (floorFromOptions). A Firing is composed out of the session's LIVE options, so a guard
		// switched off in `/settings` is off for the run this session raises as well.
		Floor: floorFromOptions(in.opts),
	}

	// Where this run's delegations go, resolved off the same `sub-agents-server:` key a session
	// resolves and handed back for the Driver to latch through run.Spec. It is resolved AFTER the
	// Config above because the named entry's Mechanism catalogue is built out of it (subAgentCatalogue),
	// and every way it can fail leaves the run unrouted with a notice — never an error.
	routing, routingNotice := resolveFiringRouting(ctx, in, keys, cfg)
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
// resolveFiringRouting observes separately through discoverDelegationBeat, because the two are
// different boxes with different keys.
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
// does not carry, an entry whose `mechanisms:` map this build refuses, a key source that would not
// answer, a server that is unreachable or has no model bound — leaves the target nil, the run
// delegating to its own Upstream, and ONE notice saying so (delegationStateNotice). That is the same
// visible degrade a session takes (ADR 0042), and the reason is stronger here: a Firing runs while
// nobody is watching, so refusing to start over a grunt box that is merely down would turn a
// scheduled run into a silent gap in the record.
//
// base is the run's own composed Config, read for exactly what building the named entry's Mechanism
// catalogue needs — the state roots, with that entry's endpoint and `model:` swapped on so the
// identity a Library observation is filed under is the SUB-AGENT server's (subAgentCatalogue).
func resolveFiringRouting(
	ctx context.Context,
	in firingInputs,
	keys *config.KeyResolver,
	base apogee.Config,
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

	// The same build a session's startup and its config reloads go through, so a routed Firing arms
	// the entry's own `mechanisms:` map rather than inheriting the parent's by accident — and refuses
	// the same defective maps, which here is a notice rather than the session's refusal to load.
	server, err := newSubAgentServer(entry, base)
	if err != nil {
		return firingRouting{}, delegationStateNotice(name, nil, "", err)
	}
	// The far seat, installed on the ENTRY rather than on the observation below: the words are the
	// human's and they do not move when the box does (ADR 0069, delegationSeatOf).
	seat := delegationSeatOf(server)

	// That entry's OWN key source — never the run's, which authenticates against another server.
	apiKey, err := keys.Resolve(entry)
	if err != nil {
		return firingRouting{seat: seat}, delegationStateNotice(name, nil, "", err)
	}

	// One beat, no retry: the composition happens once and there is no later beat to widen on, which
	// is the contract discoverBeat already set for an unattended run's own server.
	observed := discoverDelegationBeat(ctx, entry.Endpoint, entry.Model, apiKey)
	// And the one resolution the session's own beat lands, reused whole rather than re-derived: the
	// pin-else-observe ladder is ADR 0045 decision 4, and a second copy of it is how one Driver ends
	// up routing to a window the other would not.
	target := resolveDelegationTarget(entry, apiKey, observed, in.opts.ModelProfiles, server.catalogue)
	return firingRouting{target: target, seat: seat},
		delegationStateNotice(name, target, "", nil)
}
