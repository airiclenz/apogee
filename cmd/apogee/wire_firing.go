package main

import (
	"context"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
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
	// roots are the resolved state roots this run lives in — config/library/sessions/scratch plus
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
	// width is the discovery half of the parallel-agents cap; nil takes discoverSlots, the one-shot
	// probe standing in for the beat an unattended run has no heartbeat to take. A session passes
	// its own width source instead, because it already knows what the server advertises and must
	// not spend a Firing's latency re-asking (design call 4).
	width func(ctx context.Context, endpoint, model, apiKey string) int
	// dialect is the discovery half of the effort wire shape (ADR 0060), asked ONLY when the bound
	// entry forces no `effort-dialect:` of its own; nil takes discoverDialect, the one-shot beat
	// standing in for the heartbeat an unattended run has none of. A session passes its own
	// observation for width's reason: it is already holding the answer, and a Firing must not spend
	// a round trip re-asking the server the session is talking to.
	dialect func(ctx context.Context, endpoint, model, apiKey string) provider.EffortDialect
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
// back is a Config and the per-model rebind notices, which headless prints on stderr and the other
// two Drivers drop (their narration is the session record they leave behind).
//
// Tools, Events, Approver, Asker and Presenter are deliberately left nil: run.Once pins its own,
// and handing it any of them is how a run acquires a human it does not have. Tools stays nil too
// because a Firing reaches no external MCP server (ADR 0034), so the engine builds its own registry.
func firingConfig(ctx context.Context, in firingInputs) (apogee.Config, []string, error) {
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
	apiKey := in.apiKey
	if apiKey == "" {
		keys := in.keys
		if keys == nil {
			keys = config.NewKeyResolver()
		}
		resolved, err := keys.Resolve(in.entry)
		if err != nil {
			return apogee.Config{}, nil, err
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
	pinnedWindow := config.ResolveContextWindow(in.entry.ContextWindow, in.opts.ContextWindow)
	specOpts.ContextWindow = pinnedWindow
	specOpts.ResponseReserve = config.ResolveResponseReserve(in.entry.ResponseReserve, in.opts.ResponseReserve)
	spec, notices, err := rebindSpecFor(specOpts, in.roots, in.manualIDs, model, 0, pinnedWindow, in.entry.MaxOutputTokens)
	if err != nil {
		return apogee.Config{}, nil, err
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
		})
	}

	// How wide this run may fan its delegations out — the same cap a session resolves, so every
	// Driver reaches the same engine behaviour (ADR 0031; the resolution itself is ADR 0039 decision
	// 2). The pin is the BOUND entry's own `parallel-agents:`; the discovery half stands in for the
	// beat an unattended run has no heartbeat to take. A pin skips discovery outright —
	// ResolveParallelAgents never lets discovery overrule a pin, so the round trip could only spend
	// the run's latency on a question already settled.
	width := in.width
	if width == nil {
		width = discoverSlots
	}
	slots := 0
	if in.entry.ParallelAgents < 1 {
		slots = width(ctx, in.entry.Endpoint, spec.Model, apiKey)
	}

	// The wire shape this run expresses a thinking-effort intent in (ADR 0060). A session takes it
	// off the beat that lands every Interval and commits it through Rebind; an unattended run never
	// rebinds, so the value has to be STATED on the construction surface or the engine sends the
	// zero dialect — the historical `chat_template_kwargs` shape — whatever the bound server
	// actually reads. That was the Driver-parity break ADR 0031 rules out (2026-08-25 audit C-03).
	//
	// The bound entry's forced `effort-dialect:` ranks first and skips the round trip, exactly as a
	// `parallel-agents:` pin skips discovery above: a forced dialect is already the answer. With
	// nothing forced, one beat of the same discovery a session's heartbeat drives stands in, and a
	// server with no tell answers the zero, which is what an unattended run has always sent.
	effortDialect := provider.EffortDialectFor(in.entry.EffortDialect)
	if effortDialect == provider.EffortDialectNone {
		observe := in.dialect
		if observe == nil {
			observe = discoverDialect
		}
		effortDialect = observe(ctx, in.entry.Endpoint, spec.Model, apiKey)
	}

	return apogee.Config{
		Endpoint:     in.entry.Endpoint,
		Model:        spec.Model,
		APIKey:       apiKey,
		Mode:         in.mode,
		Bypass:       in.opts.Bypass,
		ConfigDir:    in.roots.config,
		LibraryDir:   in.roots.library,
		WorkspaceDir: in.roots.workspace,
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
		// The same read-only mounts a session gets: the model can read the bundled files of a skill
		// it was given exactly as an interactive one can. Sub-agents inherit them through the tool
		// instances a Subset carries, so no per-child wiring exists.
		// ReadRoots, like the session's own mount, is the resolved-path view of the same sources —
		// a workspace anchor that is a symlink out of the workspace is dropped rather than mounted
		// (audit 2026-08-25 F-13), and it stays a method value so the mount follows SetSources.
		ExtraReadRoots:   skillProvider.ReadRoots,
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
		},
		// The `delegate-max-steps` bound on a sub-agent's Exchange (default 80; 0 ⇒ unbounded).
		// A Firing runs while nobody watches, which is exactly the case a runaway delegation
		// must not be able to become.
		Delegation: apogee.DelegationConfig{MaxSteps: in.opts.DelegateMaxSteps},
	}, notices, nil
}
