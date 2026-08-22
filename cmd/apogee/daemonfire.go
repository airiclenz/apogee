package main

// The daemon's Firing composition (ADR 0034, ADR 0055) — the third Driver over the embeddable
// engine (ADR 0031), beside the TUI's scheduleWiring (schedule.go) and runHeadless (headless.go).
//
// A Firing raised here is the same unattended run those two compose, resolved from a different set
// of facts: not the session's live binding and not one command's flags, but one validated entry of
// `~/.apogee/daemon/schedules.yaml` — which server it names, which model, which workspace, which
// mode — read against a `config.yaml` this process loaded once at startup.

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/daemon"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/skills"
)

// daemonWiring turns one Firing into one unattended run. It is the value wired into
// [schedule.Config.Fire], and its shape follows the daemon's two reload surfaces rather than the
// TUI's live session:
//
//   - The HOST half — the resolved config, the key sources, the confinement backend, the sessions
//     store, the validated `mechanisms:` list — is resolved ONCE, by newDaemonWiring, because
//     `config.yaml` is read once at startup and rebinding servers is a daemon restart (ADR 0055).
//   - The ENTRY half — which server, which model, which workspace, which mode — is resolved per
//     Firing off the adopted set, because `schedules.yaml` IS live-reloaded (ADR 0034) and the
//     entry a name resolves to is whatever the last accepted edit said it was.
//
// That split is why the adopted set lives here rather than beside the daemon's loop: fire runs on
// the Scheduler's own goroutines while a reload swaps the set on the watcher's, so the map and its
// lock belong with the reader that has to survive the swap.
//
// The delegates that assume a human are never composed at all. run.Once pins its own fail-safe
// denier and leaves ask_user and present_document unregistered (ADR 0033, decision 2), and Tools
// stays nil because a Firing reaches no external MCP server (ADR 0034) — the engine builds its own
// registry instead.
type daemonWiring struct {
	// opts is the host's resolved configuration: the startup server selection flattened onto it,
	// the `servers:` list a schedule binds into by name, and every file-only key an unattended run
	// must honour for the one-configuration reason headless honours them (ADR 0031).
	opts config.Options
	// manualIDs is the enable list an explicit `mechanisms:` block spells, validated once at
	// startup — enabled keys AND disabled ones, exactly as a session validates them, because the
	// engine only ever sees the enabled IDs and a typo'd disabled key would otherwise never be
	// reported (ADR 0015 §1).
	manualIDs []apogee.MechanismID
	// keys resolves an entry's key SOURCE into the token its Firings send. One resolver for the
	// daemon's lifetime, so a `api-key-cmd:` runs once per entry rather than once per Firing — it
	// is goroutine-safe, which is what lets Firings on different Schedules share it.
	keys *config.KeyResolver
	// confiner is this host's confinement backend, built once through the newConfiner seam. A
	// Firing takes it whatever its mode: an Auto entry is fenced by it, and a Plan entry's terminal
	// commands are confined by the same posture a session's are. Whether this host may run Auto
	// unattended at all was ruled on at VALIDATION (internal/daemon's Host.AutoEligible), where the
	// refusal could name the entry, so nothing here re-asks.
	confiner apogee.Confiner
	// store is the shared sessions store every Firing's record lands in, so a schedule's runs are
	// browsable in /sessions beside the conversations and headless runs on this host (ADR 0034).
	store *session.Store

	// mu guards adopted, which the daemon's reload replaces wholesale while Firings read it.
	mu sync.RWMutex
	// adopted is the daemon's name→Entry map: the daemon-only half of `run:` — workspace, server,
	// model — which deliberately does not travel through [schedule.Spec] because the scheduler
	// library is runner-agnostic (ADR 0033). A Firing carries the name; this is what turns it back
	// into the instruction the file states.
	adopted map[string]daemon.Entry
}

// newDaemonWiring resolves everything about the HOST that every Firing of this daemon shares, and
// fails before a clock is started when any of it is wrong: a `mechanisms:` key naming no Mechanism
// is a defect in the config the daemon must report at startup rather than at 3am in a saved record.
//
// The adopted set starts empty. A daemon adopts its first file immediately after this, through the
// same [daemonWiring.adopt] call every later reload makes.
func newDaemonWiring(opts config.Options) (*daemonWiring, error) {
	roots, err := resolveRoots(opts.ConfigDir, "")
	if err != nil {
		return nil, err
	}
	manualIDs, err := mechanismIDs(opts.Mechanisms, mechanisms.KnownIDs())
	if err != nil {
		return nil, err
	}
	return &daemonWiring{
		opts:      opts,
		manualIDs: manualIDs,
		keys:      config.NewKeyResolver(),
		confiner:  newConfiner(),
		store:     session.NewStore(roots.sessions),
		adopted:   make(map[string]daemon.Entry),
	}, nil
}

// adopt replaces the set a Firing resolves its name against. It is called once with the file the
// daemon started on and once per accepted reload, always with the WHOLE validated set: an edit is
// all-or-nothing (ADR 0034), so there is no partial swap to express.
//
// A Firing already in flight is unaffected — it resolved its entry when it started, which is the
// instruction it was raised under.
func (w *daemonWiring) adopt(entries []daemon.Entry) {
	adopted := make(map[string]daemon.Entry, len(entries))
	for _, entry := range entries {
		adopted[entry.Name] = entry
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.adopted = adopted
}

// entryFor answers for one schedule name with the entry the daemon last adopted under it.
func (w *daemonWiring) entryFor(name string) (daemon.Entry, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	entry, adopted := w.adopted[name]
	return entry, adopted
}

// fire performs one Firing and reports the record it left behind. It is [schedule.Config.Fire] for
// the daemon's single Scheduler, and it runs on that Scheduler's goroutine — never the reload's —
// which is why everything it touches is its own copy or explicitly goroutine-safe (the adopted map,
// the key resolver, the store, the skills Provider, the Confiner).
func (w *daemonWiring) fire(ctx context.Context, f schedule.Firing) (schedule.Outcome, error) {
	entry, adopted := w.entryFor(f.ScheduleName)
	if !adopted {
		return schedule.Outcome{}, fmt.Errorf("apogee: daemon: the %q schedule fired but no entry of that name is "+
			"adopted — the schedule was taken off the clock while this tick was due", f.ScheduleName)
	}

	cfg, err := w.configFor(ctx, entry)
	if err != nil {
		return schedule.Outcome{}, err
	}
	cfg.Mode = f.Mode

	// Through the package's runner seam (headless.go) rather than run.Once directly: production
	// never reassigns it, so this is the same call, and it is what lets a test read the Config a
	// Firing composed without a live model.
	res, err := runOnce(ctx, run.Spec{
		Config:       cfg,
		Prompt:       f.Prompt,
		ScheduleID:   f.ScheduleID,
		ScheduleName: f.ScheduleName,
		Store:        w.store,
	})
	// Everything the run learned about itself, mapped onto the scheduler's report in one place so
	// both ends of this function tell the daemon's log the same story. The library reads none of it
	// — it is runner-agnostic (ADR 0033) — and the Notify line renders the Firing from these fields
	// alone: the answer without decoding a record, the counts without a second seam onto the run.
	out := schedule.Outcome{
		RecordID:  res.SessionID,
		Title:     res.Title,
		FinalText: res.FinalText,
		Turns:     res.Turns,
		Denied:    res.Denied,
	}
	if err != nil {
		// A failed Firing still reports what it salvaged: run.Once saves whatever completed before
		// it stopped, and naming that record is what lets a human open the interrupted run rather
		// than guess at it (the wording scheduleWiring.fire and runHeadless both use).
		if res.SessionID != "" {
			return out, fmt.Errorf("%w (partial run saved as %s)", err, res.SessionID)
		}
		return out, err
	}
	return out, nil
}

// configFor composes the construction surface one entry's Firings run against. It mirrors
// runHeadless field for field — an unattended run is an unattended run, whichever Driver raised it
// (ADR 0031) — and differs only where the entry, rather than a flag, is the thing that decides:
// the server it names, the model it overlays, and the workspace it runs in.
//
// The mode is deliberately NOT set here: it is the Firing's, taken from the Schedule the library
// fired, exactly as scheduleWiring.fire takes it.
func (w *daemonWiring) configFor(ctx context.Context, entry daemon.Entry) (apogee.Config, error) {
	server, err := w.serverFor(entry)
	if err != nil {
		return apogee.Config{}, err
	}
	// The `model:` overlay, legal here because validation already refused it where a model name
	// would be a request to ACTUATE a load rather than a per-request selection (ADR 0055 decision
	// 2). Absent, the Firing runs whatever the bound entry's own `model:` names — and on a
	// launcher-fronted server that is whatever is serving, because the daemon never actuates the
	// launcher (decision 3): nothing serving means this Firing fails visibly in its record and the
	// next tick behaves normally.
	model := server.Model
	if entry.Run.Model != "" {
		model = entry.Run.Model
	}

	// The entry's own workspace over the daemon's working directory — the one root a schedule
	// decides. Everything else these roots name is home-derived and shared by every Firing.
	roots, err := resolveRoots(w.opts.ConfigDir, entry.Run.Workspace)
	if err != nil {
		return apogee.Config{}, err
	}

	// The bearer token this Firing sends, resolved from the bound entry's own key SOURCE — the
	// literal, the command's output, or the named variable — exactly as a session resolves it, so
	// one configuration means one credential whichever Driver reads it. A source that refuses fails
	// the Firing before a single token is spent: an unattended run that degraded to sending no key
	// would put the prompt on the wire unauthenticated and report a 401 as the model's answer.
	apiKey, err := w.keys.Resolve(server)
	if err != nil {
		return apogee.Config{}, err
	}

	// The per-model half of the Config, resolved exactly as a rebind resolves it — the system
	// prompt keys on the model (ADR 0023) and so does the validated Mechanism set (ADR 0016), so a
	// Firing must land in the state a session started on this model and this server would be in.
	// The overlay onto the options copy is rebindInputs' own (wire_settings.go), spelled here
	// because a daemon has no live settings holder to spell it: the endpoint a Firing resolves
	// against and the endpoint it DIALS must be one value, or every input keyed on the endpoint —
	// the probe record behind the identity ladder, and so the Validated-set decision above it —
	// would be resolved against the startup server while the Firing talked to another one.
	//
	// The observed window is passed as unknown because nothing beats here to observe one; a
	// `context-window:` pin still binds the Budget and an unpinned Firing leaves it inactive, which
	// for one bounded prompt is the honest degrade rather than a guess. The per-session notices are
	// dropped: they are a launch's narration, and a Firing's narration is its own session record.
	specOpts := w.opts
	specOpts.Endpoint = server.Endpoint
	specOpts.APIKey = apiKey
	pinnedWindow := config.ResolveContextWindow(server.ContextWindow, w.opts.ContextWindow)
	specOpts.ContextWindow = pinnedWindow
	specOpts.ResponseReserve = config.ResolveResponseReserve(server.ResponseReserve, w.opts.ResponseReserve)
	spec, _, err := rebindSpecFor(specOpts, roots, w.manualIDs, model, 0, pinnedWindow, server.MaxOutputTokens)
	if err != nil {
		return apogee.Config{}, fmt.Errorf("apogee: daemon: resolve the %q schedule's bindings: %w", entry.Name, err)
	}

	// The share the Firing actually divides its window by, read back OFF the spec rather than
	// resolved a second time, so the spec and the Config cannot state two different splits.
	reserve := 0.0
	if spec.ResponseReserveFraction != nil {
		reserve = *spec.ResponseReserveFraction
	}

	// The skill catalog for this Firing, held in a variable rather than built inline so the SAME
	// provider serves both halves of the skills contract: it resolves an attached ID into the
	// prompt (Config.Skills) AND names the dirs whose files the model may then read
	// (Config.ExtraReadRoots below). It is per Firing because the project half of it is per
	// workspace, and the workspace is the entry's.
	skillProvider := skills.NewProvider(skills.Sources{
		Home:             roots.config,
		Workspace:        roots.workspace,
		UseProjectSkills: w.opts.UseProjectSkills,
	})

	cfg := apogee.Config{
		Endpoint:     server.Endpoint,
		Model:        spec.Model,
		APIKey:       apiKey,
		Bypass:       w.opts.Bypass,
		ConfigDir:    roots.config,
		LibraryDir:   roots.library,
		WorkspaceDir: roots.workspace,
		// Confiner and posture as a session's, so an Auto Firing is fenced by the same box an Auto
		// session would be. Whether this host may run Auto unattended at all is the eligibility
		// gate, which internal/daemon's validation already applied at the surface that offered the
		// mode — the schedules file (ADR 0033, decision 3).
		Confiner:           w.confiner,
		ConfineToWorkspace: w.opts.ConfineToWorkspace,
		WebSearchEndpoint:  w.opts.WebSearchEndpoint,
		// Every file-only key below is honoured for the one reason runHeadless honours it: it is one
		// configuration, and a Firing must offer the model the same tools, obey the same host
		// allow/deny lists, scrub the same variables out of a subprocess and read responses in the
		// same shape a session on this host would.
		DisabledTools: w.opts.ToolsDisabled,
		URLAllowHosts: w.opts.URLAllowHosts,
		URLDenyHosts:  w.opts.URLDenyHosts,
		Inspector:     w.opts.UI.Inspector,
		SecretEnvVars: config.APIKeyEnvNames(w.opts),
		Profile:       spec.Profile,
		SystemPrompt:  spec.SystemPrompt,
		ContextFiles:  w.opts.ContextFiles,
		Skills:        skillProvider,
		// The same read-only mounts a session gets: a Firing's model can read the bundled files of
		// a skill its prompt named exactly as an interactive one can. Sub-agents inherit them
		// through the tool instances a Subset carries, so no per-child wiring exists.
		ExtraReadRoots:   skillProvider.SourceDirs,
		EnableMechanisms: spec.EnableMechanisms,
		Context: apogee.ContextConfig{
			MaxContextTokens:        spec.MaxContextTokens,
			ResponseReserveFraction: reserve,
			// The bound entry's `max-output-tokens:` pin (ADR 0046). Unpinned it stays 0 and the
			// engine derives the cap from its own reply budget — never "no cap", which for an
			// unattended run is precisely the thing a runaway reply must not be able to become.
			MaxOutputTokens:   server.MaxOutputTokens,
			CompactionEnabled: w.opts.AutoCompact,
		},
		// Tools, Events, Approver, Asker and Presenter all stay nil: run.Once pins its own, and
		// handing it any of them is how a run acquires a human it does not have.
	}

	// How wide this Firing may fan its delegations out — the same cap a session and a headless run
	// resolve, so every Driver reaches the same engine behaviour (ADR 0031; the resolution itself is
	// ADR 0039 decision 2). The pin is the BOUND entry's own `parallel-agents:`; the discovery half
	// is the one-shot probe standing in for the beat an unattended run has no heartbeat to take. A
	// pin skips the probe outright — ResolveParallelAgents never lets discovery overrule a pin, so
	// the round trip could only spend a Firing's latency on a question already settled.
	slots := 0
	if server.ParallelAgents < 1 {
		slots = discoverSlots(ctx, server.Endpoint, spec.Model, apiKey)
	}
	cfg.ParallelAgents = config.ResolveParallelAgents(server.ParallelAgents, slots)

	return cfg, nil
}

// serverFor resolves the `servers:` entry one schedule's Firings bind to: the entry it names, or
// the startup default this host would give a fresh session when it names none (ADR 0055 decision
// 1). A name no entry answers to is already refused when the schedules file is validated, so
// reaching that branch means `config.yaml` and the adopted set have drifted apart — which the
// Firing reports rather than silently firing at the wrong server.
func (w *daemonWiring) serverFor(entry daemon.Entry) (config.ServerEntry, error) {
	named := strings.TrimSpace(entry.Run.Server)
	if named == "" {
		return startupEntry(w.opts), nil
	}
	for _, server := range w.opts.Servers {
		if server.Name == named {
			return server, nil
		}
	}
	return config.ServerEntry{}, fmt.Errorf("apogee: daemon: the %q schedule binds to server %q, which no servers: "+
		"entry in config.yaml answers to — the daemon reads config.yaml once at startup, so restart it after "+
		"editing that list", entry.Name, named)
}
