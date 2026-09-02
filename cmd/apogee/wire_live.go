package main

// The live-session assembly of the composition root, lifted out of wire.go by concern (ADR 0043).
//
// One function: every holder, host and seam the running session is built from, in the order the
// startup has always built them — the MCP connections and the tool registry folded onto the base
// Config, the Mechanism list the engine arms, the session store and the record a --resume restores,
// the engine and Upstream holders and the one bind that fills them, the live-settings holder the
// `/settings` edits move, the config watcher, and the out-of-band work (the launcher, the naming
// call, the scheduler) that reads the binding rather than capturing it.
//
// Nothing here is torn down here: everything closable lands on the wiring, and runRoot's single
// deferred close ends it in reverse — which is what lets any step below return an error and still
// leave exactly the teardown the old inline defers performed.

import (
	"context"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/filewatch"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/session"
)

// configWatchTiming is the seam onto how fast the session's `config.yaml` watcher runs (ADR 0041
// decision 3). Its zero value — the production one — leaves internal/filewatch's own constants in
// place: a poll every second, a quarter of a second of quiet before a save is reported, which is
// what a feature that ends with a human saving a document should cost. A driver test replaces it so
// a watcher step costs a tenth of a second instead of a second and a half of a test suite's budget.
// Production never reassigns it.
//
// It is a pair of durations rather than a filewatch value because the watcher's own knobs are
// exported fields settable before Start, and the seam's job is only to carry the two numbers to
// them: a zero field means "leave the watcher's default alone", so a test may move one and not the
// other.
var configWatchTiming watchTiming

// watchTiming is the poll cadence and settle window [configWatchTiming] carries.
type watchTiming struct {
	Interval time.Duration
	Settle   time.Duration
}

// applyTo moves whichever of the two durations is set onto a watcher that has not been started yet.
func (t watchTiming) applyTo(w *filewatch.Watcher) {
	if w == nil {
		return
	}
	if t.Interval > 0 {
		w.Interval = t.Interval
	}
	if t.Settle > 0 {
		w.Settle = t.Settle
	}
}

// wireSession assembles the running session on top of the boot phase's Config. Every step that can
// fail returns the error untouched, and every step that opens something records it on the wiring
// first — so a failure two thirds of the way down tears down exactly what a failure at that line
// always tore down.
func (w *rootWiring) wireSession(ctx context.Context) error {
	// Connect the configured external MCP servers (P3.15) and surface their tools into the
	// Agent's registry. With no servers configured this is dormant (a no-op Client, nil tools).
	// On resume the connection is established FRESH here — no server-side state is restored
	// (ADR 0008). An MCP connect failure is fatal: a configured server that cannot be reached is
	// a misconfiguration the user should see, not a silently-dropped capability.
	//
	// The endpoint of an `sse`/`streamable-http` server is checked against the operator's OWN
	// url-safety host lists — scheme/host allow-deny, with the resolved-IP floor disabled for it
	// and the connection pinned to its own addresses instead (internal/mcp/transport.go, ADR 0012
	// amendment 2026-07-26). Before, both call sites here handed the transport a ZERO guard, so a
	// configured `deny-hosts` entry applied to every network tool and to no MCP endpoint (audit
	// 2026-08-25 F-40); a denied host is now refused at startup with the url-safety message.
	mcpClient, err := mcp.Connect(ctx, w.opts.MCPServers,
		mcpGuard(w.cfg.URLAllowHosts, w.cfg.URLDenyHosts), w.roots.workspace)
	if err != nil {
		return fmt.Errorf("apogee: connect MCP servers: %w", err)
	}
	// The connections are held rather than captured, because `mcp-servers:` is editable mid-session
	// (ADR 0037 decision 6): a reconnect dials a new set, swaps it in and tears the old one down, so
	// what has to be closed at the end of the run is whatever the holder is on NOW — closing the
	// client this line connected would leave the live sessions orphaned and tear down a set that was
	// already closed hours ago.
	//
	// The reconnect runs LATER, when an `mcp-servers:` edit lands (liveMCP.reconnect), by which time
	// the host lists may have moved through `/settings` — so its guard is built from the spec the
	// live tool set is currently on rather than from the snapshot this line closed over. That is the
	// same read the network tools' own rebuild makes.
	//
	// Reading the live spec is only HALF of what keeps the MCP endpoint check and the network tools
	// agreeing about which hosts are closed, though, and on its own it would leave them disagreeing
	// until the next `mcp-servers:` edit — which may never come. The other half is that a host-list
	// edit RECONNECTS on its own account when the new lists change which servers are admitted
	// (applyURLSafetyHosts), so a server the operator has just closed is dropped there and then
	// rather than kept until something else happens to dial.
	w.mcpSet = newLiveMCP(mcpClient, func(servers []mcp.ServerConfig) (mcpSession, error) {
		spec := w.toolSet.built()
		return mcp.Connect(ctx, servers, mcpGuard(spec.allowHosts, spec.denyHosts), w.roots.workspace)
	})
	// The registry is assembled HERE unconditionally rather than left to the engine's own
	// resolveTools — which would build the identical set from this same Config — because the
	// composition root has to keep the pointer. The settings surface re-points a live tool in place
	// when only its configuration moved (the web_search endpoint, ADR 0037), and rebuilds the whole
	// set through Agent.SwapTools when the SET has to change. Neither is reachable through a
	// registry the engine built privately, and with no MCP server configured the two builds are the
	// same tools in the same order, so this changes what the root HOLDS, never what the Agent runs.
	// Whether the delegation tool offers the model a seat to name (ADR 0069). It is resolved once,
	// here, because the set the session STARTS on and the spec a later rebuild carries forward must
	// be the same reading of the key — the gate is a `sub-agents-choice:` word and the tool takes a
	// bool, and two places translating it is two places one of them can be wrong.
	seatChoice := w.opts.SubAgentsChoice == config.SubAgentsChoiceModel
	w.cfg.Tools = registryWithMCP(w.roots.workspace, w.cfg, seatChoice, w.mcpSet.tools())
	// What the set the session runs was BUILT from — the values a later rebuild has to carry rather
	// than take from this snapshot again. The url-safety host lists ride it beside the endpoint and
	// the roster because the guard is built WITH the set (registryWithMCP hands one URLGuard to every
	// network tool), so an edit of either list is a rebuild rather than a write on a tool. The bound
	// model's roster axis rides it for the same reason: a model switch re-composes the set under the
	// profile it lands on (ADR 0057 decision 7), and the rebuild that does it must carry the axis
	// forward, not the one the process launched under. And the `sub-agents-choice:` gate rides it for
	// that same reason once more: which schema sub_agent publishes is settled at construction, so a
	// rebuild driven by anything else must carry the gate the session is on rather than the launch's.
	built := toolSetSpec{
		endpoint:   w.cfg.WebSearchEndpoint,
		disabled:   w.opts.ToolsDisabled,
		roster:     w.cfg.Profile.Tools,
		allowHosts: w.cfg.URLAllowHosts,
		denyHosts:  w.cfg.URLDenyHosts,
		seatChoice: seatChoice,
	}
	w.toolSet = newLiveTools(w.cfg.Tools, built, func(spec toolSetSpec) *apogee.ToolRegistry {
		// The set as this session would have built it with another search endpoint, another roster,
		// another model's profile axis, another pair of url-safety host lists and the other
		// `sub-agents-choice:` gate: the MCP tools are
		// re-folded from the holder rather than remembered, so a rebuild always carries the
		// connections that are live NOW.
		host := w.cfg
		host.WebSearchEndpoint = spec.endpoint
		host.DisabledTools = spec.disabled
		host.Profile.Tools = spec.roster
		host.URLAllowHosts = spec.allowHosts
		host.URLDenyHosts = spec.denyHosts
		return registryWithMCP(w.roots.workspace, host, spec.seatChoice, w.mcpSet.tools())
	})

	// Resolve the catalogued Mechanisms enabled in config.yaml to the sorted ID list the engine arms
	// (ADR 0015 §1: wire.go collapses to a YAML→ID-list producer). Startup validates EVERY
	// `mechanisms:` key here — enabled AND disabled — and hands only the enabled IDs to
	// Config.EnableMechanisms; apogee.New/Resume then build them, derive their Deps (the Library store
	// under LibraryDir, the resolved model fingerprint), merge them into
	// Config.Mechanisms, and run the ordering / incompatibility / requirements gates. The disabled-key
	// validation must stay here because the engine only ever sees the enabled IDs, so a typo'd DISABLED
	// key — never constructed — must still fail loudly at this startup boundary. With nothing enabled
	// the resolver still returns the OFF-RAMP FLOOR (ADR 0070), so a config without a mechanisms
	// block arms the two recovery guarantees and nothing else; every other Capability stays off
	// until it is named.
	//
	// The list is hoisted into a local because it outlives this assignment: it is the MANUAL
	// choice, model-independent by construction, and the rebind seam re-runs the
	// "an explicit mechanisms: block suppresses a validated set" rule against it for every new
	// model — so it must survive the validated-set overwrite two blocks down.
	manualIDs, retiredNotices, err := mechanisms.ResolveEnabled(w.opts.Mechanisms, mechanisms.KnownIDs())
	if err != nil {
		return err
	}
	w.cfg.EnableMechanisms = manualIDs

	// A `mechanisms:` key naming a RETIRED Mechanism is tolerated rather than refused (the id was
	// valid at the release before the removal), and this is the one caller that says so: startup runs
	// before the alt screen, so a stderr line here reaches the human, where the same resolver running
	// under the live `/settings` apply or per delegate would paint over the TUI. It sits beside the
	// validated-set notices below for the same reason they do.
	for _, n := range retiredNotices {
		fmt.Fprintln(os.Stderr, n)
	}

	// The Validated-set runtime surface (ADR 0016): match the resolved model fingerprint
	// against the shipped + user-local entries and fold an applying set into
	// EnableMechanisms — HERE at wire time, never in the engine, so ADR 0015's single
	// enable path stands and bench arms cannot be contaminated. When a set applies,
	// opts.mechanisms was empty (manual control suppresses the apply), so the assignment
	// replaces an empty list, never a user's choice. The notices are the ADR's visible
	// per-session notice, on stderr pre-TUI like the unconfined-Auto warning above.
	vset, vnotices, err := resolveValidatedSet(w.opts, w.roots.validated, w.roots.probe)
	if err != nil {
		return err // a dangling validated-sets alias — the user's own config, loud by design
	}
	for _, n := range vnotices {
		fmt.Fprintln(os.Stderr, n)
	}
	if len(vset) > 0 {
		w.cfg.EnableMechanisms = withOffRampFloor(vset, w.opts.Mechanisms)
	}

	// The id-addressed session store under this run's sessions root, and the record a --resume or
	// --continue start restores from (nil for a fresh start). Resolving it here lets the host begin
	// ACTIVE on that record — continuing its file in place rather than forking a new session — and
	// the Agent resume off rec.Session below.
	w.store = session.NewStore(w.roots.sessions)
	w.resumed, err = resolveResume(w.store, w.opts.Resume, w.opts.ContinueSession, w.roots.workspace)
	if err != nil {
		return err
	}

	// The engine seam the renderer drives. It is a HOLDER rather than the Agent itself, because
	// construction is no longer something startup always gets to do: with no startup server
	// determined the TUI opens pre-bound and the engine arrives with the human's first pick (ADR
	// 0036 decision 3). Everything below this line wires against the holder and never learns which
	// of the two happened — the seams are identical, and the engine is behind them either way.
	w.engine = newLateEngine(w.mode, w.opts.ConfineToWorkspace)

	// The store-backed session host: it persists the active session (per-Turn, at idle, and on
	// quit) and backs the /sessions browser. It owns id minting and the metadata policy — the
	// facts only the binary knows (workspace root, resolved model) — so the renderer stays free of
	// file I/O (phase-2 detail plan §3 C5). Seeded active on a resumed record, it updates that
	// session's file rather than starting a new one.
	// The scratch seam rides the host because the host owns session identity: it creates the
	// active session's `scratch/<id>/` dir at each identity boundary and pushes the move into the
	// engine holder, so the confinement box follows /clear|/new and /sessions resume. The seed
	// below puts the boot session's dir on the Config the binder captures, which is what makes it
	// writable from the engine's very first tool call (workspace-clobber hardening, 2026-08-22).
	w.host = newSessionHost(w.store, w.roots.workspace, w.opts.Model, w.resumed,
		w.roots.scratch, w.engine.SetScratchDir)
	w.cfg.ScratchDir = w.host.SessionScratchDir()

	// The upstream monitor: one beat every heartbeat.Interval, from inside the running TUI. The
	// configured model id travels with it as the discovery HINT (decision 10) — while the server
	// still serves that id, discovery resolves ITS window rather than the first advertised model's,
	// which is the whole of the pinned-multi-model-server bug; once the pin vanishes from
	// /v1/models the beat reports what is actually loaded and the rebind below follows it.
	// The resolved api key rides with it: the monitor talks to the same keyed server the
	// session does, and a beat that could not authenticate would paint a permanently
	// unreachable Upstream under a session that is working.
	//
	// It is held rather than passed directly, because a Monitor is per-SERVER (endpoint and key
	// alike) and a `/server` switch replaces the whole thing. The holder is what keeps that a
	// composition-root move: the renderer's Beat act is wired to holder.Beat, one signature for the
	// life of the session, and the renderer never learns which Monitor answered. It starts empty
	// for the same reason the engine holder above does — the bind step is what fills both.
	w.holder = newUpstreamHolder()

	// The bind step: the one place a ServerEntry becomes a running session (the Agent, the Monitor,
	// and the binding the out-of-band calls read). A determined startup binds HERE, before the TUI
	// starts, which is what keeps the ordinary path exactly what it was; an undetermined one leaves
	// both holders empty and binds through the seam handed to the renderer below.
	// The Parallel agents cap for whichever server this session is on (ADR 0039 decision 2). It is
	// declared beside the two holders above and for their reason: the width belongs to the SERVER, so
	// every point the session arrives on one — this bind, a first pick, a `/server` switch — re-states
	// it, and the beat that discovers a server's slot count re-states it again. Empty until something
	// is bound, which resolves to 1: the serial floor.
	w.caps = newParallelAgentsCap(w.engine)

	w.binder = serverBinder{cfg: w.cfg, resumed: w.resumed, engine: w.engine, holder: w.holder,
		caps: w.caps, keys: w.keys}
	if w.opts.Prebound.Reason == "" {
		if err := w.binder.bind(startupEntry(w.opts)); err != nil {
			return err
		}
	}

	// The startup snapshot's MUTABLE half (ADR 0037): the `context-window:` pin, the `servers:` list,
	// the manual Mechanism ids and the `validated-sets:`/`system-prompt-*` inputs — every value below
	// that a committed `/settings` edit can now move mid-session. The seams that used to capture
	// each of them by value read this holder instead, so the next thing that re-resolves — a rebind, a
	// server switch, a scheduled Firing — sees what the human changed rather than what the process
	// launched with. Seeded from opts, so a session nobody edits behaves exactly as it did.
	//
	// The servers this session can be moved to are derived from it the same way they always were: the
	// `servers:` entries plus a synthesized row for the startup endpoint only when that endpoint came
	// from a raw override and is therefore in no entry (upstreamChoices), so the way back is always
	// offered. The verbs in wire_verbs.go resolve a name against THAT list (they need the key and the
	// hint); the TUI is handed the display-and-identity projection of the same list, in the same order.
	w.live = newLiveSettings(w.opts, manualIDs)

	// The Sub-agent server (ADR 0045): the `servers:` entry the root `sub-agents-server:` key names,
	// the second heartbeat that discovers what it is serving, and the Delegation target every beat
	// resolves for the engine to route spawns against. It is built from the holder's list rather than
	// from the launch snapshot, like every other read of `servers:` since ADR 0037 — the holder's
	// READER goes in rather than one call's answer, since a mid-session retarget resolves its name
	// against the list as it stands then — and the NAME is handed in from the resolved options,
	// which is the one place routing consults the key.
	//
	// It is wired AFTER the bind and reads the engine holder rather than the Agent, for the reason
	// every seam in this block does: a pre-bound session has no Agent yet, and the named server is
	// observable regardless — routing is a fact about the OTHER server, so nothing here waits for
	// this session to have picked its own. With no entry named — the key absent, the default — it
	// holds no server: no monitor, no beat, nothing latched, and delegations stay on the session's
	// Upstream (delegation.go).
	//
	// The notice seam is the Bridge's, like the scheduler's narration below and for its reason: the
	// routing state changes on the second heartbeat's own goroutine, which needs a send that is safe
	// before the program exists and safe from anywhere after it does (tui.Bridge.NotifyRouting).
	w.delegation, err = newDelegationWiring(w.opts.SubAgentsServer,
		w.live.serverList, w.cfg, w.engine, w.live.modelProfileEntries, w.bridge.NotifyRouting, w.keys)
	if err != nil {
		return err
	}

	// The `$EDITOR` round trip's own half of the same story (ADR 0037 decision 5): the keys holding a
	// structure no row can express are edited in the file, and this is what opens it at the right line
	// and works out what came back different. It is built here rather than at the seam block below
	// because its baseline is the file as it stands NOW, and now is before anything has been edited.
	w.externalEdits = newExternalEdit(w.opts, w.roots.workspace, os.Getenv)

	// The other trigger for that same round trip (ADR 0041 decision 3): the file itself. An editor's
	// EXIT can only speak for the editors apogee waits on, and a desktop opener returns before the
	// human has typed a character — so `config.yaml` is polled for the whole session and every save
	// applies, whoever made it (decision 5). Started here, beside the baseline it will be diffed
	// against, and stopped with the run's other closers.
	//
	// The path is the one this session resolved, which is the same file every seam in the block below
	// writes; the watcher reads no YAML and holds no projection of its own (internal/filewatch).
	w.configWatch = filewatch.New(config.FilePath(w.opts.ConfigDir))
	configWatchTiming.applyTo(w.configWatch)
	w.configWatch.Start()

	// The one fold that re-points a session at another Upstream, shared by `/server`'s switch and
	// a profile load's follow-the-profile: engine switch, Monitor swap, stored model cleared, in order
	// (see sessionMover.move, which carries the reasoning).
	w.mover = sessionMover{agent: w.engine, holder: w.holder, host: w.host, live: w.live, keys: w.keys,
		caps: w.caps}

	// Where "is the llama-launcher integration on, and against which config" lives for this session
	// (ADR 0029 D4, per-entry since 2026-08-07). It is declared HERE, above the two verbs that
	// install into it, because enablement follows the session's server entry: the startup entry's own
	// key is what the session begins with, and a `/server` switch or a first bind replaces it. A
	// pre-bound start therefore begins empty — the verbs answer tui.ErrNoLauncher until a bind
	// installs a value — and so does a run on the ephemeral `--endpoint` entry, which names no key.
	// The entry NAME travels with the path from the first moment, because it is what a committed
	// profile load records its `launch-profile:` pointer onto (remember-model). HostAlias is that
	// name: ApplyConfig writes the SELECTED entry's own into it, and the one start that carries a
	// host-derived label instead — the ephemeral `--endpoint` override — carries no launcher key
	// either, so the two are empty together exactly as launcherPath.follow keeps them.
	startPath, startOn := entryLauncherPath(w.opts.StartupLauncher)
	startEntry := ""
	if startOn {
		startEntry = w.opts.HostAlias
	}
	w.launcherPath = newLauncherPath(startPath, startEntry)

	// The llama-launcher seams (ADR 0029 D1): the bridge in launcher.go, which is the only file that
	// names the library, and which the projection hands the renderer as one tui.LauncherHost
	// (launcherHost, ADR 0054). An entry's `llama-launcher:` value resolves HERE, at the
	// layer that knows the launcher — into the path holder above, which is where "is the integration
	// on" lives. The members are wired UNCONDITIONALLY for that reason and answer
	// tui.ErrNoLauncher while the holder is empty, which the renderer reads as the host having no
	// launcher — the same degrade a nil host expresses, now able to change its mind when
	// the session moves to another server.
	//
	// The toggle rides in as a closure over the LIVE holder, not as the bool the launch snapshot froze:
	// `remember-model:` is live-editable — the `/settings` pane, and the watcher over the same file —
	// and the boot restore is the one seam of the three that is answered off the Update loop. Reading
	// through the closure is what keeps its question ("is remembering on?") the same question the two
	// record seams ask, asked of the same holder the pane's flip writes to.
	w.launcherSeams = launcherWiring{sessionMover: w.mover, ops: liveLauncherOps, path: w.launcherPath,
		remember: func() bool { return w.live.remember() }}

	// The session-naming seam (ADR 0022 addendum): one out-of-band completion, built per call from
	// whatever server and model the session is bound to at that moment — so a `/server` switch or a
	// rebind carries the naming call with it, and neither needs a seam of its own. It is wired
	// unconditionally: `auto-title:` gates only the AUTOMATIC firing (below), while a bare `/rename`
	// regenerates on demand even with the toggle off (Ratified design 7).
	//
	// Its "answer without thinking" ask needs the server's effort wire DIALECT to land in a shape that
	// server reads (ADR 0060), and that fact is read off the same live latch the rebind path writes on
	// every beat — so the naming call and the conversational path can never disagree about the dial of
	// the server the session is on.
	w.titles = newTitleWiring(w.holder.Binding, w.live.observedDialect, w.roots.workspace)

	// The scheduler this session's Schedules live in (ADR 0033), built beside the naming call for
	// the same reason: both are out-of-band work against the single-slot server the session is bound
	// to, and both read that binding at CALL time rather than capturing it. Three seams make it
	// live — a runner that composes one unattended Firing from the session as it stands at that
	// moment, settings and binding alike (schedule.go), a Gate that holds a due Firing until this
	// session is quiescent, and a Notify that carries the scheduler's narration into the running
	// program through the Bridge the Sink already uses.
	//
	// New's only refusal is a Config with no runner, which this one has; the error is returned
	// rather than ignored because a scheduler that failed to build must not be handed on as a
	// working seam.
	firings := scheduleWiring{
		live:     w.live,
		roots:    w.roots,
		binding:  w.holder.Binding,
		width:    w.caps.current,
		keys:     w.keys,
		skills:   w.skillProvider,
		confiner: w.confiner,
		store:    w.store,
	}
	w.gate = newIdleGate()
	w.schedules, err = schedule.New(schedule.Config{
		Fire:   firings.fire,
		Gate:   w.gate.wait,
		Notify: w.bridge.NotifySchedule,
		Clock:  tuiScheduleClock,
	})
	if err != nil {
		return fmt.Errorf("apogee: build the scheduler: %w", err)
	}

	// The colour scheme, resolved HERE rather than in the renderer: reading a file is the
	// composition root's job, so the TUI is handed a palette and never a path (ADR 0031's
	// wire-silent engine has the same shape — the driver resolves, the component renders). Nothing
	// it answers is fatal: an unknown name, an unreadable file and a defective key each cost a
	// warning and keep the default palette (ADR 0040 design call 8), and those warnings travel with
	// the palette so the transcript can say what went wrong.
	// The live half of the same seam is a pair of closures over the SAME folder (ADR 0037's
	// validate → persist → apply, with the apply landing entirely inside the renderer): the settings
	// picker asks what schemes exist when the human opens it, and the switch resolves the chosen one
	// again from disk — which is what makes an edited scheme file land on the next switch without a
	// restart, and why neither is a value captured here.
	w.colorScheme, w.colorSchemeWarnings = resolveColorScheme(w.opts.UI.ColorScheme, w.roots.schemes)

	return nil
}

// withOffRampFloor returns set with the catalogued off-ramp floor (mechanisms.OffRampFloor, ADR
// 0070) folded in — a DEDUPLICATED union, in canonical order, so a set that already names an
// off-ramp still names it exactly once.
//
// It is applied ONLY where a VALIDATED SET replaces the enable list (here at startup and at the
// per-model rebind): a set is a whole-arm record whose author never had to think about a floor, so a
// set that omits an off-ramp must not be read as turning that off-ramp off. The manual `mechanisms:`
// path needs no call — mechanisms.ResolveEnabled already floors what it resolves — and an explicit
// `<off-ramp>: false` in the block suppresses the set entirely (ADR 0016's whole-set-or-nothing), so
// the block is still passed here rather than assumed empty.
//
// The deduplication is load-bearing rather than tidiness: the shipped set already names both
// off-ramps (internal/validated/shipped.json), and an appending union would hand the engine an ID
// twice, whose registry Add refuses as "already registered" — turning every matching model's startup
// into a failure.
func withOffRampFloor(set []apogee.MechanismID, block map[string]bool) []apogee.MechanismID {
	out := slices.Clone(set)
	for _, id := range mechanisms.OffRampFloor(block) {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// mcpGuard builds the url-safety guard an MCP connect is made under, from the host lists whichever
// call site holds — the startup snapshot at wireSession, the live tool set's spec at a reconnect,
// and the spec a `url-safety:` edit has just installed at the re-admission that edit drives
// (applyURLSafetyHosts). It exists so those cannot drift: the guard is built through the same
// constructor, off the same two fields, that registryWithMCP hands every network tool
// (wire_tools.go), so a host the operator closed is closed on every path or on none.
//
// It takes no receiver precisely so the settings dispatcher can reach it: the applier is composed
// from the root's members rather than from the root (wire_options.go), and a guard the apply built
// through some other constructor would be the very drift this function exists to prevent.
func mcpGuard(allow, deny []string) security.URLGuard {
	return security.NewURLGuard(allow, deny)
}
