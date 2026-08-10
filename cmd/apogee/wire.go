package main

// The composition proper: runRoot, which assembles roots, config, tools, MCP, sessions, recall and
// engine into a running TUI, plus the state-root resolution and the narrow surfaces its seam files
// share. Everything else the wiring owns lives in a wire_<seam>.go beside it (ADR 0043: a
// composition root splits by seam once it outgrows one file).
//
//   - wire_settings.go — the live `settings` holder and the dispatcher a committed /settings key is
//     applied through, plus the per-model re-resolution a heartbeat rebind drives.
//   - wire_tools.go — the live tool registry, the builders that assemble one (built-ins plus MCP),
//     and the validation of the `mechanisms:` block against the catalogue.
//   - wire_mcp.go — the connected MCP sessions and the validate-then-commit reconnect that moves a
//     session onto another set of servers.
//   - wire_present.go — the presentation ladder this host can walk, and the holder that rebuilds it
//     and re-installs it on the presenter.
//   - wire_session.go — the session-persistence and prompt-recall hosts, and the resume resolution a
//     --resume/--continue start goes through.
//   - wire_engine.go — Agent construction through the public surface, and the late-bound
//     [tui.Engine] that stands in until a server is picked.
//   - wire_server.go — the entry a startup selection collapses to, the one step that binds any entry
//     to a session, and the config-change wait the reload chain parks on.
//
// wire_test.go covers this file and all seven.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/tui"
)

// Compile-time proof that the public Agent satisfies the TUI's narrow engine seam
// (phase-2 detail plan §3 C5): cmd dogfoods apogee.New, and *apogee.Agent (= *agent.Agent)
// is exactly what internal/tui drives — without internal/tui ever importing the root path.
var _ tui.Engine = (*apogee.Agent)(nil)

// The confinement backend label and the Auto-degradation notice used below live in
// internal/probe: `apogee probe` reports the same verdict off-session and the TUI's
// /confine status renders it in-session, so the wording is extracted rather than copied —
// three surfaces, one sentence (Phase-5 item 3 / ADR 0021).

// unconfinedAutoWarning is what Auto says for itself when confinement is switched off
// (`confine-to-workspace: false`) — the only blanket loosen in the system (ADR 0012), so it is
// stated every time it is used rather than assumed remembered. It is a const because two surfaces
// now print it — the interactive launch below and an `apogee headless --mode auto` run — and a
// user who has read it at one of them must not meet a softer wording at the other.
const unconfinedAutoWarning = "apogee: WARNING — auto mode is running UNCONFINED " +
	"(confine-to-workspace: false). This is safe only inside a VM/container; " +
	"the dangerous-action guard is a footgun-net, not a security boundary."

// shouldPrewarmLabelWalk reports whether startup should eagerly run the confinement backend's
// one-time label walk (ADR 0020 §2) rather than let the first confined command pay it mid-session.
// It is the MIRROR of probe.DegradedNotice's gate — the same three inputs, FSWrite inverted: the
// degradation notice fires when Auto asks for confinement a host CANNOT enforce (FSWrite false),
// this fires when it CAN (FSWrite true), which on the Windows token backend is the disk-label pass
// worth pre-warming behind a progress notice. It returns true on the Linux and macOS backends too
// (they also report FSWrite under Auto+confine), where PrewarmLabelWalk is a genuine no-op — only
// the Windows-tagged labelBox pays a walk — so the Windows-vs-not distinction lives in that seam,
// not here. Pure so the decision is table-testable off Windows (the DegradedNotice seam pattern).
func shouldPrewarmLabelWalk(mode apogee.Mode, confineToWorkspace, fsWrite bool) bool {
	return mode == domain.ModeAuto && confineToWorkspace && fsWrite
}

// ----------------------------------------------------------------------------
// Root command body
// ----------------------------------------------------------------------------

// runRoot is the root command's body: parse the mode, resolve the state roots, build a
// Config, construct (or resume) the Agent through the public surface, and launch the UI.
func runRoot(ctx context.Context, opts config.Options, launch launcher) error {
	mode, err := domain.ParseMode(opts.Mode)
	if err != nil {
		return err
	}

	roots, err := resolveRoots(opts.ConfigDir, opts.Workspace)
	if err != nil {
		return err
	}

	// Discover the user's skills from the layered source dirs: the global library
	// (~/.apogee/skills), the project's .apogee/skills, and — when use-project-skills is on —
	// the project's bare skills/. The Provider holds the current catalog and can Reload it from
	// these same dirs on demand: the merged "/" menu refreshes it each time it opens, so a skill
	// added or edited after launch is picked up without restarting. The initial load error is
	// soft (a missing dir is skipped, a malformed skill is skipped), so the catalog is always
	// usable. The SAME *skills.Provider feeds both the loop (Config.Skills resolves attached IDs
	// into the turn) and the TUI's merged "/" menu (Options.Skills lists/labels them), so a
	// refreshed skill both shows in the menu AND resolves when attached.
	skillProvider := skills.NewProvider(skills.Sources{
		Home:             roots.config,
		Workspace:        roots.workspace,
		UseProjectSkills: opts.UseProjectSkills,
	})

	// The Bridge late-binds the event sink and approval gate to the Bubble Tea program
	// the launcher starts. Its Sink/Approver are installed in Config before construction
	// (apogee.New requires Events; Ask-Before needs the Approver), then bound once the
	// program exists (phase-2 detail plan §3 C2/C3).
	bridge := tui.NewBridge()

	// The presentation ladder's host-side mechanisms (ADR 0019), resolved from the `present:`
	// block and THIS session's environment and installed on the Bridge. Installing them is also
	// what makes bridge.Presenter() non-nil, and with it registers present_document — so the tool
	// exists exactly where a presentation can be carried out, which in the TUI is always (rung 0,
	// the transcript line, needs no mechanism at all).
	// It is a HOLDER rather than a value because the four `present.` keys are editable in the
	// `/settings` pane and apply to the running session (ADR 0037): the holder rebuilds the ladder
	// from the new block and re-installs it on the presenter the engine already captured.
	presentation := newLivePresentation(
		opts.Present, roots.workspace, runtime.GOOS, os.Getenv, bridge.SetPresentation)
	// The doc server's listener is owned by the app: it binds lazily on the first served
	// presentation and closes with the session, like the MCP connections and the Agent below.
	// Closing through the holder closes whichever server is current, which after a `present.port`
	// edit is not the one this session started with.
	defer presentation.close()

	// The host's real Confiner backend, hoisted into a local so its Capabilities() can be read
	// here for the degradation notice below — the backend probes once at construction, so this
	// is the same value the engine's dispatch disposition will consult.
	confiner := platform.NewConfiner()
	// A backend that mutated the machine to build its box has to put it back. Only the
	// Windows token backend does: it expresses the box's writable half as a mandatory label
	// on the disk and reverts it here (ADR 0020 §2). domain.Confiner deliberately does NOT
	// grow a teardown method for one OS — it is a public interface (ADR 0010) — so the hook
	// is an optional-interface assertion at the composition root, beside the other Closes.
	// A teardown that could not put the disk back is the ONE confinement failure with no other
	// surface: the session is ending, the TUI is gone, and the labels are still there. Discarding
	// it would leave the user with a silently mutated disk, so it goes to stderr naming the
	// journal that survived the failure and ADR 0020 §2's manual remedy (the wording lives in
	// internal/platform beside the host report's, so both read the same).
	if closer, ok := confiner.(interface{ Close() error }); ok {
		defer func() {
			if notice := platform.ConfinementTeardownNotice(closer.Close()); notice != "" {
				fmt.Fprintln(os.Stderr, notice)
			}
		}()
	}

	// The system prompt this session STARTS with (ADR 0023), selected for the model as configured
	// — which on a cold start is no model at all, so this selects the global template and the
	// per-model entry lands seconds later, on the first beat's rebind (rebindSpecFor re-runs
	// exactly this call with the observed model). A selected file that cannot be read, or a
	// template carrying an unknown placeholder, fails startup naming the config key — the prompt is
	// structural configuration, not something to degrade quietly around.
	sysPrompt, err := config.ResolveSystemPrompt(opts.SystemPrompt, opts.Model, roots.config, os.ReadFile)
	if err != nil {
		return err
	}

	cfg := apogee.Config{
		Endpoint: opts.Endpoint,
		Model:    opts.Model,
		// The upstream bearer token, resolved from the startup `servers:` entry's own `api-key`,
		// which APOGEE_API_KEY overlays. Empty — the keyless local default — sends no
		// Authorization header at all.
		APIKey:       opts.APIKey,
		Mode:         mode,
		Bypass:       opts.Bypass,
		Events:       bridge.Sink(),
		Approver:     bridge.Approver(),
		Asker:        bridge.Asker(),
		Presenter:    bridge.Presenter(),
		ConfigDir:    roots.config,
		LibraryDir:   roots.library,
		SessionsDir:  roots.sessions,
		WorkspaceDir: roots.workspace,
		// The host's real Confiner backend for this OS (landlock on Linux, seatbelt on macOS,
		// denyConfiner elsewhere — confinement-execution-contract §2.6). It is no longer
		// denyConfiner, so --mode auto WORKS where fs-confinement exists and gates the
		// subprocess surface where it does not (rather than refusing Auto).
		Confiner:           confiner,
		ConfineToWorkspace: opts.ConfineToWorkspace,
		WebSearchEndpoint:  opts.WebSearchEndpoint,
		// The `tools.disabled:` roster switch: the built-in tools this config takes off the menu.
		// Empty ⇒ the whole roster, exactly the set built before the key existed. It is carried on
		// Config rather than passed to the assembly alone so every Driver — this session, a headless
		// run, an embedder — prunes the same roster from the same value.
		DisabledTools: opts.ToolsDisabled,
		// The model profile (CONTEXT: Model profile) — tool-call format + thinking channel —
		// resolved from config.yaml (file-only). A zero profile is native tool calls with no
		// inline thinking, so an unconfigured model behaves exactly as today.
		Profile: opts.Profile,
		// The configured system-prompt TEMPLATE (ADR 0023), which the loop renders fresh per
		// request and seeds as the first system message. Empty ⇒ no prompt: the request opens with
		// the user's own message, exactly as it did before this key existed.
		SystemPrompt: sysPrompt,
		// The workspace context files (`context-files:`, file-only): the names the engine looks for
		// in the workspace root at every session boundary, whose content rides the same first system
		// message as the prompt above — verbatim, never as a template. Nil ⇒ the feature is off, and
		// the request is exactly what it was before the key existed.
		ContextFiles: opts.ContextFiles,
		Skills:       skillProvider,
		// The `context-window:` PIN (0 when unpinned — nothing probes at startup any more). It is
		// the budget /compact and the automatic Compaction trigger bound their summary request
		// against so compaction survives high fill (the summary call would otherwise overflow near
		// n_ctx); the same value drives the TUI's footer/gauge below. Unpinned it stays 0 until the
		// first heartbeat rebind binds the observed window. CompactionEnabled carries the
		// `auto-compact` key (default on) — the budget-driven automatic trigger (item 9); the
		// on-demand /compact runs regardless of it.
		Context: apogee.ContextConfig{MaxContextTokens: opts.ContextWindow, CompactionEnabled: opts.AutoCompact},
	}

	// A per-session startup warning whenever Auto runs unconfined (ADR 0012): confine=false
	// is safe only inside a VM, and it is the only blanket loosen in the system.
	if mode == domain.ModeAuto && !opts.ConfineToWorkspace {
		fmt.Fprintln(os.Stderr, unconfinedAutoWarning)
	}

	// The mirror branch: Auto WITH confinement asked for, on a host whose backend cannot
	// enforce it. The ladder gates every terminal command instead — correct, but silent until
	// now, which is what made Auto look broken (ISSUES.md, 2026-07-21). Say it once, name the
	// backend, and point at /confine.
	if notice := probe.DegradedNotice(probe.BackendName(confiner), confiner.Capabilities(), mode, opts.ConfineToWorkspace); notice != "" {
		fmt.Fprintln(os.Stderr, notice)
	}

	// The unknown-window honesty line used to print here, before the alt-screen. It has moved into
	// the TUI's rebind fold (ADR 0024): at this point in startup NOTHING has asked the server yet,
	// so a launch-time notice would fire on every cold start and be wrong ten seconds later. The
	// first beat that binds a window without one is where the sentence is actually true.

	// Eager pre-warm of the confinement label walk (ADR 0020 §2, the plan's approach A). On the
	// Windows token backend the box is a mandatory Low label on the workspace tree, and labelling a
	// large .git or node_modules costs ~1 ms/object; a FIRST confined command that silently blocks
	// for seconds mid-session is the click-through-frustration trap Auto was built to avoid. Under
	// Auto+confine a confined command is effectively certain, so the walk is hoisted to startup —
	// pre-alt-screen, behind WindowsLabelProgressNotice — where a raw stderr write is safe and the
	// first in-session Confine then hits the memo and no-ops. This moves only the TIMING of the
	// already-ratified label pass, never WHAT is labelled (Close still reverts at shutdown),
	// consistent with the owner's "keep semantics". It is a genuine no-op on every other host:
	// PrewarmLabelWalk is empty off Windows, and the Windows backend refuses when FSWrite is false —
	// the same host probe.DegradedNotice above speaks for.
	if shouldPrewarmLabelWalk(mode, opts.ConfineToWorkspace, confiner.Capabilities().FSWrite) {
		platform.PrewarmLabelWalk(confiner, roots.workspace, os.Stderr)
	}

	// Connect the configured external MCP servers (P3.15) and surface their tools into the
	// Agent's registry. With no servers configured this is dormant (a no-op Client, nil tools).
	// On resume the connection is established FRESH here — no server-side state is restored
	// (ADR 0008). An MCP connect failure is fatal: a configured server that cannot be reached is
	// a misconfiguration the user should see, not a silently-dropped capability.
	mcpClient, err := mcp.Connect(ctx, opts.MCPServers, security.URLGuard{})
	if err != nil {
		return fmt.Errorf("apogee: connect MCP servers: %w", err)
	}
	// The connections are held rather than captured, because `mcp-servers:` is editable mid-session
	// (ADR 0037 decision 6): a reconnect dials a new set, swaps it in and tears the old one down, so
	// what has to be closed at the end of the run is whatever the holder is on NOW — closing the
	// client this line connected would leave the live sessions orphaned and tear down a set that was
	// already closed hours ago.
	mcpSet := newLiveMCP(mcpClient, func(servers []mcp.ServerConfig) (mcpSession, error) {
		return mcp.Connect(ctx, servers, security.URLGuard{})
	})
	defer mcpSet.close()
	// The registry is assembled HERE unconditionally rather than left to the engine's own
	// resolveTools — which would build the identical set from this same Config — because the
	// composition root has to keep the pointer. The settings surface re-points a live tool in place
	// when only its configuration moved (the web_search endpoint, ADR 0037), and rebuilds the whole
	// set through Agent.SwapTools when the SET has to change. Neither is reachable through a
	// registry the engine built privately, and with no MCP server configured the two builds are the
	// same tools in the same order, so this changes what the root HOLDS, never what the Agent runs.
	cfg.Tools = registryWithMCP(roots.workspace, cfg, mcpSet.tools())
	toolSet := newLiveTools(cfg.Tools, cfg.WebSearchEndpoint, opts.ToolsDisabled,
		func(endpoint string, disabled []string) *apogee.ToolRegistry {
			// The set as this session would have built it with another search endpoint and another
			// roster: the MCP tools are re-folded from the holder rather than remembered, so a
			// rebuild always carries the connections that are live NOW.
			host := cfg
			host.WebSearchEndpoint = endpoint
			host.DisabledTools = disabled
			return registryWithMCP(roots.workspace, host, mcpSet.tools())
		})

	// Resolve the catalogued Mechanisms enabled in config.yaml to the sorted ID list the engine arms
	// (ADR 0015 §1: wire.go collapses to a YAML→ID-list producer). runRoot validates EVERY
	// `mechanisms:` key here — enabled AND disabled — and hands only the enabled IDs to
	// Config.EnableMechanisms; apogee.New/Resume then build them, derive their Deps (the Library store
	// under LibraryDir, the resolved model fingerprint, the inert grammar seam), merge them into
	// Config.Mechanisms, and run the ordering / incompatibility / requirements gates. The disabled-key
	// validation must stay here because the engine only ever sees the enabled IDs, so a typo'd DISABLED
	// key — never constructed — must still fail loudly at this startup boundary. With nothing enabled
	// the list is empty and the engine arms nothing, so a config without a mechanisms block behaves
	// exactly as before.
	//
	// The list is hoisted into a local because it outlives this assignment: it is the MANUAL
	// choice, model-independent by construction, and the rebind closure below re-runs the
	// "an explicit mechanisms: block suppresses a validated set" rule against it for every new
	// model — so it must survive the validated-set overwrite two blocks down.
	manualIDs, err := mechanismIDs(opts.Mechanisms, mechanisms.KnownIDs())
	if err != nil {
		return err
	}
	cfg.EnableMechanisms = manualIDs

	// The Validated-set runtime surface (ADR 0016): match the resolved model fingerprint
	// against the shipped + user-local entries and fold an applying set into
	// EnableMechanisms — HERE at wire time, never in the engine, so ADR 0015's single
	// enable path stands and bench arms cannot be contaminated. When a set applies,
	// opts.mechanisms was empty (manual control suppresses the apply), so the assignment
	// replaces an empty list, never a user's choice. The notices are the ADR's visible
	// per-session notice, on stderr pre-TUI like the unconfined-Auto warning above.
	vset, vnotices, err := resolveValidatedSet(opts, roots.validated, roots.probe)
	if err != nil {
		return err // a dangling validated-sets alias — the user's own config, loud by design
	}
	for _, n := range vnotices {
		fmt.Fprintln(os.Stderr, n)
	}
	if len(vset) > 0 {
		cfg.EnableMechanisms = vset
	}

	// The id-addressed session store under this run's SessionsDir, and the record a --resume or
	// --continue start restores from (nil for a fresh start). Resolving it here lets the host begin
	// ACTIVE on that record — continuing its file in place rather than forking a new session — and
	// the Agent resume off rec.Session below.
	store := session.NewStore(roots.sessions)
	resumed, err := resolveResume(store, opts.Resume, opts.ContinueSession, roots.workspace)
	if err != nil {
		return err
	}

	// The engine seam the renderer drives. It is a HOLDER rather than the Agent itself, because
	// construction is no longer something startup always gets to do: with no startup server
	// determined the TUI opens pre-bound and the engine arrives with the human's first pick (ADR
	// 0036 decision 3). Everything below this line wires against the holder and never learns which
	// of the two happened — the seams are identical, and the engine is behind them either way.
	engine := newLateEngine(mode, opts.ConfineToWorkspace)
	defer engine.Close()

	// The store-backed session host: it persists the active session (per-Turn, at idle, and on
	// quit) and backs the /sessions browser. It owns id minting and the metadata policy — the
	// facts only the binary knows (workspace root, resolved model) — so the renderer stays free of
	// file I/O (phase-2 detail plan §3 C5). Seeded active on a resumed record, it updates that
	// session's file rather than starting a new one.
	host := newSessionHost(store, roots.workspace, opts.Model, resumed)

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
	// composition-root move: Options.Heartbeat below is wired to holder.Beat, one signature for the
	// life of the session, and the renderer never learns which Monitor answered. It starts empty
	// for the same reason the engine holder above does — the bind step is what fills both.
	holder := newUpstreamHolder()

	// The bind step: the one place a ServerEntry becomes a running session (the Agent, the Monitor,
	// and the binding the out-of-band calls read). A determined startup binds HERE, before the TUI
	// starts, which is what keeps the ordinary path exactly what it was; an undetermined one leaves
	// both holders empty and binds through the seam handed to the renderer below.
	// The Parallel agents cap for whichever server this session is on (ADR 0039 decision 2). It is
	// declared beside the two holders above and for their reason: the width belongs to the SERVER, so
	// every point the session arrives on one — this bind, a first pick, a `/server` switch — re-states
	// it, and the beat that discovers a server's slot count re-states it again. Empty until something
	// is bound, which resolves to 1: the serial floor.
	caps := newParallelAgentsCap(engine)

	binder := serverBinder{cfg: cfg, resumed: resumed, engine: engine, holder: holder, caps: caps}
	if opts.Prebound.Reason == "" {
		if err := binder.bind(startupEntry(opts)); err != nil {
			return err
		}
	}

	// The startup snapshot's MUTABLE half (ADR 0037): the `context-window:` pin, the `servers:` list,
	// the manual Mechanism ids and the `validated-sets:`/`system-prompt-*` inputs — every value below
	// that a committed `/settings` edit can now move mid-session. The closures that used to capture
	// each of them by value read this holder instead, so the next thing that re-resolves — a rebind, a
	// server switch, a scheduled Firing — sees what the human changed rather than what the process
	// launched with. Seeded from opts, so a session nobody edits behaves exactly as it did.
	//
	// The servers this session can be moved to are derived from it the same way they always were: the
	// `servers:` entries plus a synthesized row for the startup endpoint only when that endpoint came
	// from a raw override and is therefore in no entry (upstreamChoices), so the way back is always
	// offered. The closures below resolve a name against THAT list (they need the key and the hint);
	// the TUI is handed the display-and-identity projection of the same list, in the same order.
	live := newLiveSettings(opts, manualIDs)

	// The `$EDITOR` round trip's own half of the same story (ADR 0037 decision 5): the keys holding a
	// structure no row can express are edited in the file, and this is what opens it at the right line
	// and works out what came back different. It is built here rather than at the seam block below
	// because its baseline is the file as it stands NOW, and now is before anything has been edited.
	externalEdits := newExternalEdit(opts, os.Getenv)

	// The other trigger for that same round trip (ADR 0041 decision 3): the file itself. An editor's
	// EXIT can only speak for the editors apogee waits on, and a desktop opener returns before the
	// human has typed a character — so `config.yaml` is polled for the whole session and every save
	// applies, whoever made it (decision 5). Started here, beside the baseline it will be diffed
	// against, and stopped below with the run's other closers.
	//
	// The path is the one this session resolved, which is the same file every seam in the block below
	// writes; the watcher reads no YAML and holds no projection of its own (internal/config/configwatch.go).
	configWatch := config.NewWatcher(config.FilePath(opts.ConfigDir))
	configWatch.Start()
	// Registered after the Agent's own Close, so it runs BEFORE it (the schedules' posture, and for
	// the schedules' reason): the poll ends while everything it reported into is still standing. Stop
	// waits for the poll goroutine and closes the channel behind it, so the wait the renderer parks on
	// returns rather than leaking, and nothing this line let go of outlives runRoot.
	defer configWatch.Stop()

	// The rebind closure: the composition root's half of an observed model change. The TUI decides
	// WHEN (at idle, or at the exchange-terminal boundary), this decides WHAT — because every input
	// to the decision is config the binary owns (the per-model system prompt, ADR 0023; the
	// validated set, ADR 0016; the manual mechanisms list; the window pin) and the engine mutators
	// are the binary's to drive. It runs on the Update goroutine, at a quiescent boundary Agent.
	// Rebind demands, so nothing here needs a lock of its own. A resolution error returns WITHOUT
	// touching the engine, and Agent.Rebind is itself validate-then-commit, so a refused rebind
	// leaves the session bound exactly where it was.
	rebind := func(model string, window int) (tui.RebindResult, error) {
		// What the beat could name about the server's own window, remembered before it is resolved
		// against the pin: a later pin EDIT re-drives this closure with no beat of its own, and a
		// cleared pin has to bind the discovered window rather than unbind it (ADR 0024).
		live.observe(window)
		base, manualIDs, pinnedWindow := live.rebindInputs(opts, holder.Binding())
		spec, notices, err := rebindSpecFor(base, roots, manualIDs, model, window, pinnedWindow)
		if err != nil {
			return tui.RebindResult{}, err
		}
		if err := engine.Rebind(spec); err != nil {
			return tui.RebindResult{}, err
		}
		// Session metadata follows the wire: a session that switched models mid-conversation is
		// listed under the model it ends on, which is the one its last Turns actually ran against.
		host.SetModel(spec.Model)
		// And so does discovery: the hint is "which of the served models do I mean", a property of
		// the binding rather than of the launch. Re-stating it on every commit keeps the next beat
		// measuring the model the session now runs as "nothing new" — without it a server serving
		// both the old and the new id would resolve the stale config hint and this very closure
		// would bind straight back to it on the next beat. Through the holder, so the hint lands on
		// whichever server the session is currently on.
		holder.SetModel(spec.Model)
		return tui.RebindResult{
			Model:         spec.Model,
			ContextWindow: spec.MaxContextTokens,
			Notices:       notices,
		}, nil
	}

	// The discovery half of the Parallel agents cap (ADR 0039 decision 2), wrapped around the beat
	// the TUI already runs rather than probing for itself: `total_slots` rides the observation the
	// heartbeat lands every Interval (ADR 0024 — no new beat machinery), and this is the composition
	// root reading it on the way past. An unpinned server therefore widens from serial to its own
	// slot count within one beat of being bound, and narrows again if the operator restarts it
	// smaller; a pinned one is unmoved, because the pin outranks discovery.
	//
	// It runs on the TUI's beat goroutine, which is safe on both sides: the holder has its own mutex
	// and the engine seam behind it is the anytime-safe class (Agent.SetParallelAgents). The Beat is
	// returned untouched — the renderer sees exactly what the server said.
	beat := func(ctx context.Context) heartbeat.Beat {
		observed := holder.Beat(ctx)
		caps.observe(observed.TotalSlots)
		return observed
	}

	// The one fold that re-points a session at another Upstream, shared by `/server`'s switch and
	// a profile load's follow-the-profile: engine switch, Monitor swap, stored model cleared, in order
	// (see sessionMover.move, which carries the reasoning).
	mover := sessionMover{agent: engine, holder: holder, host: host, live: live}

	// Where "is the llama-launcher integration on, and against which config" lives for this session
	// (ADR 0029 D4, per-entry since 2026-08-07). It is declared HERE, above the two closures that
	// install into it, because enablement follows the session's server entry: the startup entry's own
	// key is what the session begins with, and a `/server` switch or a first bind replaces it. A
	// pre-bound start therefore begins empty — the verbs answer tui.ErrNoLauncher until a bind
	// installs a value — and so does a run on the ephemeral `--endpoint` entry, which names no key.
	startPath, _ := entryLauncherPath(opts.StartupLauncher)
	launcher := newLauncherPath(startPath)

	// The server-switch closure: the composition root's half of `/server`. The TUI decides WHEN
	// (at idle, on an explicit act by the human), this decides everything the move touches —
	// because a server is an endpoint, a key, and a discovery hint, and all three are config the
	// binary owns. It runs on the Update goroutine at the quiescent boundary Agent.SwitchUpstream
	// demands, and opens no connection of its own: the first BEAT is what discovers the new server.
	//
	// All this closure adds to the shared fold is RESOLUTION, and it comes first: a name that
	// resolves to nothing never reaches the engine, so the session is left exactly where it was.
	switchServer := func(name string) (tui.ServerSwitchResult, error) {
		entry, err := findServer(live.choices(opts), name)
		if err != nil {
			return tui.ServerSwitchResult{}, err
		}
		result, err := mover.move(entry.Endpoint, entry.Name, entry.Model, entry.APIKey)
		if err != nil {
			return tui.ServerSwitchResult{}, err
		}
		// The launcher follows the entry the session just moved onto — off when the entry names no
		// config, on against its own when it does. It is installed HERE, at the switch, and not
		// inside the shared move: a profile load reaches that move directly and must leave the
		// integration exactly as it found it (launcherPath.follow carries the reasoning). And it is
		// installed only after the move SUCCEEDED, so a refused switch leaves the session's launcher
		// where the session still is.
		launcher.follow(entry)
		// And so does the fan-out width, for the same reason and on the same terms (ADR 0039): how
		// many sub-agents may run at once is the new server's answer, not the old one's, and the
		// entry's pin is in hand right here. A move is the one moment the previously observed slot
		// count must be forgotten — parallelAgentsCap.follow does that — so an unpinned server starts
		// serial and widens the moment its own first beat reports its slots.
		caps.follow(entry)
		return result, nil
	}

	// The recording closure: the composition root's half of ADR 0036 decision 2 — the `server:` key
	// this session's last deliberate choice becomes, so the NEXT session starts where this one ended
	// without asking. It is spliced into the same config.yaml every other write goes through (the
	// ADR 0035 writer, extended by 0036 to this key), which is why it lives beside the two closures
	// above rather than in the renderer: the file, the path, and the registry that says the key may
	// be written are all the binary's.
	//
	// The CONFIGURED check is the whole of the decision, and it is a name lookup in the file's own
	// list rather than in the switchable choices: the one row those two lists differ by is the
	// synthesized ephemeral startup (upstreamChoices), which names no entry and must never be
	// written back as one. A move onto it — like a launcher profile's server, which never reaches
	// this seam at all — is skipped SILENTLY: false with no error, which the renderer states by
	// saying nothing about a recording.
	recordServerChoice := func(name string) (bool, error) {
		if !configuredServer(live.serverList(), name) {
			return false, nil
		}
		if err := config.SaveConfigSetting(filepath.Join(roots.config, "config.yaml"), "server", name); err != nil {
			return false, err
		}
		return true, nil
	}

	// The bind closure: the same resolution, ending in a CONSTRUCTION rather than a move. It is the
	// only way out of the pre-bound state, and it answers with what a switch answers so the display
	// adopts a first binding exactly as it adopts a move — the endpoint now on the wire, the entry's
	// name as the alias, and the global window pin, which was never per-server. The binder refuses a
	// second construction, so an already-bound session is told to use `/server` instead of quietly
	// growing a second engine.
	bindServer := func(name string) (tui.ServerSwitchResult, error) {
		entry, err := findServer(live.choices(opts), name)
		if err != nil {
			return tui.ServerSwitchResult{}, err
		}
		if err := binder.bind(entry); err != nil {
			return tui.ServerSwitchResult{}, err
		}
		// The same install as the switch above, for the same reason: this is the other way a session
		// arrives ON an entry, so a pre-bound start that binds the launcher-fronted server has the
		// integration from that moment. A refused bind installs nothing.
		launcher.follow(entry)
		return tui.ServerSwitchResult{
			Endpoint:      entry.Endpoint,
			HostAlias:     entry.Name,
			ContextWindow: live.pin(),
		}, nil
	}

	// The llama-launcher seams (ADR 0029 D1): four closures over the bridge in launcher.go, which is
	// the only file that names the library. An entry's `llama-launcher:` value resolves HERE, at the
	// layer that knows the launcher — into the path holder above, which is where "is the integration
	// on" lives. The four members are wired UNCONDITIONALLY for that reason and answer
	// tui.ErrNoLauncher while the holder is empty, which the renderer reads as the host having no
	// launcher — the same degrade the nil seams used to express, now able to change its mind when
	// the session moves to another server.
	launcherSeams := launcherWiring{sessionMover: mover, ops: realLauncher{}, path: launcher}

	// The session-naming seam (ADR 0022 addendum): one out-of-band completion, built per call from
	// whatever server and model the session is bound to at that moment — so a `/server` switch or a
	// rebind carries the naming call with it, and neither needs a seam of its own. It is wired
	// unconditionally: `auto-title:` gates only the AUTOMATIC firing (below), while a bare `/rename`
	// regenerates on demand even with the toggle off (Ratified design 7).
	titles := newTitleWiring(holder.Binding, roots.workspace)

	// The scheduler this session's Schedules live in (ADR 0033), built beside the naming call for
	// the same reason: both are out-of-band work against the single-slot server the session is bound
	// to, and both read that binding at CALL time rather than capturing it. Three seams make it
	// live — a runner that composes one headless Firing from the current binding (schedule.go), a
	// Gate that holds a due Firing until this session is quiescent, and a Notify that carries the
	// scheduler's narration into the running program through the Bridge the Sink already uses.
	//
	// New's only refusal is a Config with no runner, which this one has; the error is returned
	// rather than ignored because a scheduler that failed to build must not be handed on as a
	// working seam.
	firings := scheduleWiring{
		base:    cfg,
		opts:    opts,
		roots:   roots,
		live:    live,
		binding: holder.Binding,
		width:   caps.current,
		store:   store,
	}
	gate := newIdleGate()
	schedules, err := schedule.New(schedule.Config{
		Fire:   firings.fire,
		Gate:   gate.wait,
		Notify: bridge.NotifySchedule,
	})
	if err != nil {
		return fmt.Errorf("apogee: build the scheduler: %w", err)
	}
	// A TUI-hosted Schedule dies with the TUI — the honest v1 promise (ADR 0033): Close takes every
	// Schedule off the clock, cancels the context its Firings run under and joins their goroutines,
	// so nothing this session started outlives the alternate screen. Registered after the Agent's
	// own Close, so it runs BEFORE it: the firings let go of the process while everything they were
	// composed from still stands.
	defer schedules.Close()

	// The prompt caret's shape. ApplyConfig already refused a name this build does not know, so the
	// error here cannot fire; ignoring it keeps the parse a single expression, and ParseCursorShape
	// answers an unknown name with the default anyway — a caret is drawn either way.
	cursorShape, _ := tui.ParseCursorShape(opts.CursorShape)

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
	colorScheme, colorSchemeWarnings := resolveColorScheme(opts.UI.ColorScheme, roots.schemes)

	err = launch(ctx, engine, bridge, tui.Options{
		// Both upstream facts are now honestly launch-time-only: Model is the configured pin ("" on
		// a cold start, where the footer says "connecting…" until the first beat binds one), and
		// ContextWindow is the `context-window:` pin (0 when unpinned). Neither is a discovery
		// result any more — the heartbeat and the rebind closure below own everything after launch.
		Model:     opts.Model,
		Endpoint:  opts.Endpoint,
		Mode:      mode,
		Bypass:    opts.Bypass,
		Workspace: roots.workspace,
		// The apogee home THIS run resolved (--config / APOGEE_CONFIG included), so a report that
		// names a path — /skills telling an empty catalog where discovery looked — names the folder
		// the run actually walks rather than the ~/.apogee default it may not be using.
		ConfigHome:    roots.config,
		ContextWindow: opts.ContextWindow,
		HostAlias:     opts.HostAlias,
		// The two upstream seams (ADR 0024): the monitor observes on the TUI's cadence, and the
		// closure applies what the observation implies. Wiring both is what makes the display live.
		// Heartbeat goes through the holder, so the observation follows the session onto another
		// server without the seam — or the renderer — changing shape; the wrapper above reads the
		// slot count off the same observation on its way past.
		Heartbeat: beat,
		Rebind:    rebind,
		// The `/server` half: the servers this session can move to (display and identity only —
		// the keys and hints stay here) and the one verb that moves it. Both are always wired;
		// the list is the `servers:` block verbatim, plus the one row an ephemeral
		// `--endpoint`/`APOGEE_ENDPOINT` start synthesizes for itself (upstreamChoices). It can
		// therefore be EMPTY — a pre-bound start on a config that lists nothing — which is
		// exactly "nothing to switch to" without a special case.
		//
		// Projected from the HOLDER on every ask rather than snapshotted at launch, so a `servers:`
		// block the human edits mid-session (ADR 0037) is offered by the picker and by the settings
		// pane's server row the moment the edit lands — the same list the two closures above resolve
		// a name against, in the same order.
		Servers:      func() []tui.ServerChoice { return serverChoices(live.choices(opts)) },
		SwitchServer: switchServer,
		// The pre-bound half of the same list (ADR 0036 decisions 3, 4 and 7): why this session has
		// no upstream yet — first boot, a `server:` naming an entry that is gone, or nothing
		// configured at all — and the seam that ends it. Both are always wired; on the ordinary start Prebound
		// is the zero value, which says the engine was constructed before the program began and
		// leaves every flow below exactly as it was.
		Prebound:   opts.Prebound,
		BindServer: bindServer,
		// The persistence half of both verbs (ADR 0036 decision 2): every deliberate move onto a
		// configured entry — the first-boot choice included — records that entry as the one the next
		// session starts on, so the question is asked once. Moves onto anything the file does not
		// list are skipped silently, which is what keeps an override or a profile load from becoming
		// config nobody wrote.
		RecordServerChoice: recordServerChoice,
		// The `/model`-over-profiles, `/unload-model`, `/stop-server` half (ADR 0029): browse the
		// launcher's profiles, activate one — following it onto another server when it lives
		// there — and free or stop the server this session is on. All four are wired for the life of
		// the session and report tui.ErrNoLauncher while the integration is off, because `off` is now
		// a value the human can change from inside the program (ADR 0037).
		LaunchProfiles: launcherSeams.profiles,
		LoadProfile:    launcherSeams.load,
		UnloadServer:   launcherSeams.unload,
		StopServer:     launcherSeams.stop,
		// And the cheap question the two actuation verbs ask before they latch: is the integration on
		// right now? It is one atomic load rather than a verb, so the refusal a switched-off session
		// gets is synchronous — no "unloading…" frame for a verb that never runs.
		LauncherEnabled: launcherSeams.on,
		// The resolved `ui:` block: which animation paints the status-line spinner, whether its
		// colour loop runs, and whether the transcript's scroll bar is painted at all. Independent
		// values, resolved and validated by ApplyConfig, so the renderer selects rather than parses.
		// The scroll bar is the one key whose polarity flips here — the config says show, the
		// renderer's option says hide, so its zero value is the shown default (see tui.Options).
		Spinner:       opts.UI.Spinner,
		SpinnerColor:  opts.UI.SpinnerColor,
		HideScrollbar: !opts.UI.ShowScrollbar,
		// The `ui.color-scheme:` key, already resolved to the palette itself (above): the name so the
		// renderer can say which scheme is in force, and the warnings the resolve produced so it can
		// tell the human why the screen is not the one they asked for.
		ColorScheme:         colorScheme,
		ColorSchemeName:     opts.UI.ColorScheme,
		ColorSchemeWarnings: colorSchemeWarnings,
		// And the two that keep the scheme switchable from inside the program: what the picker
		// offers, and the resolve behind an answer to it. Both read the folder on every ask, so a
		// scheme file written or edited mid-session is offered and loaded without a restart.
		ListSchemes: func() []string { return scheme.Discover(roots.schemes) },
		ResolveScheme: func(name string) (scheme.Scheme, []string) {
			return resolveColorScheme(name, roots.schemes)
		},
		// And the one seam that CREATES a scheme file: `/color-scheme export` copies a built-in into
		// the same folder, which is what makes an embedded palette editable at all.
		ExportScheme: func(name string) (string, error) { return scheme.Export(name, roots.schemes) },
		// The `cursor-shape:` key: the shape the REAL terminal cursor takes at the prompt caret
		// (steady always — there is no blink key). Selected here, like the two above, so the
		// renderer never parses a config name.
		CursorShape: cursorShape,
		// The two hidden rendering-diagnostic seams (--tui-trace / --tui-diag), passed through as
		// the paths they are: empty on every ordinary run, and the renderer decides what a named
		// one means and when it goes live.
		TracePath: opts.TUITrace,
		DiagPath:  opts.TUIDiag,
		// The single source of truth (the embedded top-level VERSION file). Version carries the
		// full string (provenance included) that /version prints and --version mirrors; BaseVersion
		// is the release version alone (no provenance), the clean value the start-up box displays.
		Version:     apogee.Version(),
		BaseVersion: apogee.BaseVersion(),
		// The same backend, capabilities, and host id the degradation notice above was built
		// from, so /confine status inside the TUI reports the host's real situation rather than
		// re-deriving it. internal/platform is the binary's dependency, not the renderer's.
		Confinement: tui.ConfinementInfo{
			Backend: probe.BackendName(confiner),
			Caps:    confiner.Capabilities(),
			HostID:  platform.HostID(),
		},
		// The `--save` half of `/confine off --save`: record THIS host in the same config.yaml
		// ApplyConfig read at startup, so the next run resolves unconfined here without the claim
		// following the file onto any other machine. The renderer learns only the path written —
		// the on-disk format is the binary's business, like the session Save seam below.
		SaveHostAcknowledgement: config.HostAcknowledgementSaver(
			filepath.Join(roots.config, "config.yaml"), platform.HostID()),
		// The `/settings` pane's rows: every key the registry describes, with the value THIS run
		// resolved and the marker for a key an environment variable or a flag overrode
		// (settingsRows.go). A provider rather than a slice because the pane derives its rows on
		// every paint — the picker's convention — and it closes over the resolved opts because the
		// pane reports the resolution THIS run made: a key persisted mid-session is applied by the
		// dispatcher below and shown from the pane's own journal, marked ` *` (ADR 0037 decision 8).
		SettingsRows: func() []tui.SettingRow { return settingsRows(opts) },
		// The pane's write half: one key per deliberate edit, spliced into the same config.yaml the
		// acknowledgement above records a host in (ADR 0035). The registry decides what may be
		// written and the splice writer owns the file (internal/config/configwrite.go) — the renderer hands over a
		// path and the value as the file spells it, and learns only whether it landed.
		//
		// Every landed write re-takes the external edit's baseline (ADR 0041 decision 8). The pane
		// applies the key it just persisted in the same keypress, and the watcher below is looking at
		// the very file this wrote: without the refresh, apogee's own write comes back a second later
		// as somebody's edit and applies twice — which for `mcp-servers:` is a second dial of every
		// server. A write that FAILED changed no file and refreshes nothing.
		WriteSetting: func(key, value string) error {
			if err := config.SaveConfigSetting(filepath.Join(roots.config, "config.yaml"), key, value); err != nil {
				return err
			}
			externalEdits.refresh()
			return nil
		},
		// Reset is the same write in reverse: the key's active line is REMOVED, so the value goes
		// back to the binary's default rather than being pinned to today's spelling of it. It refreshes
		// the same baseline for the same reason — a removed line is a change to the file like any other.
		ResetSetting: func(key string) error {
			if err := config.ResetConfigSetting(filepath.Join(roots.config, "config.yaml"), key); err != nil {
				return err
			}
			externalEdits.refresh()
			return nil
		},
		// And the apply half of the same keypress (ADR 0037): what the file now says, the session
		// now runs. The dispatcher owns the resolution from a registry path and a file-spelled value
		// onto a live engine seam — the renderer holds neither schema nor engine mutator.
		ApplySetting: applySettingFor(settingsApplier{
			engine:     engine,
			live:       live,
			binding:    holder.Binding,
			rebind:     rebind,
			configPath: filepath.Join(roots.config, "config.yaml"),
			skills:     skillProvider,
			tools:      toolSet,
			mcp:        mcpSet,
			roots:      roots,
			present:    presentation,
			caps:       caps,
		}),
		// The `$EDITOR` round trip for the keys no row can hold (ADR 0037 decision 5): out through a
		// command line this binary resolves — the file, the key's own line, the editor this environment
		// names — and back through a re-read that says which keys changed. The pane applies them
		// through the same two homes an in-pane commit uses; nothing here applies anything, so the
		// file's authority and the apply's single path both stay where they were (settingsedit.go).
		ExternalEditSpec: externalEdits.spec,
		ReloadConfig:     externalEdits.changed,
		// And the trigger that does not need an editor at all (ADR 0041 decision 3): one wait on the
		// watcher started above, answered when the file changes. What the renderer does with the news
		// is exactly what it does when an editor exits — re-read through ReloadConfig, apply through
		// the two homes above — so a saved file applies whoever saved it (decision 5).
		AwaitConfigChange: awaitConfigChangeOn(configWatch),
		Skills:            skillProvider,
		// Re-scan the skill source dirs when the merged "/" menu opens, swapping in a fresh catalog
		// on the shared Provider — the same one Config.Skills resolves against — so a skill added
		// mid-session both shows and attaches. The error is soft (Provider.Reload never signals
		// unusable), so it is dropped.
		ReloadSkills: func() { _ = skillProvider.Reload() },
		// The store-backed session host drives all persistence (per-Turn saves, /sessions, quit
		// flush); the renderer sees only the SessionHost seam. Resumed carries the startup-replay
		// payload for a --resume/--continue start (nil on a fresh start), so newModel repaints the
		// stored scrollback beneath the start-up box and relights the gauge.
		Sessions: host,
		// Prompt recall: this workspace's own list of sent inputs, which the box walks with ↑/↓.
		// The workspace is bound HERE — the renderer resolves no paths — and the store is always
		// wired, because an empty recall file and an unwired seam look identical to the human
		// until they have sent something.
		Recall: newRecallHost(roots.prompts, roots.workspace),
		// The naming half of the same records: the seam that turns a first prompt into a title, and
		// the `auto-title:` key that says whether a new session names itself without being asked.
		// The seam is wired either way — the key is a preference about automatism, not a ban on the
		// call — so `/rename` regenerates on demand regardless.
		GenerateTitle: titles.generate,
		AutoTitle:     opts.AutoTitle,
		// The scheduler surface (ADR 0033): the seam /schedule and /schedule-stop drive, the reason
		// auto is unavailable on this host (empty ⇒ it is available, and the picker offers it), and
		// the activity report the Gate above releases a due Firing on. All three are wired together
		// or not at all — the renderer's nil check on the seam speaks for the set.
		Schedules: schedules,
		ScheduleAutoBlocked: scheduleAutoBlocked(
			probe.BackendName(confiner), confiner.Capabilities(), opts.ConfineToWorkspace),
		ReportActivity: gate.report,
		// engine.InExchange() reads the resumed Agent's open-Exchange state (false on a fresh start,
		// or a cleanly-closed resume; true only when the stored snapshot died mid-task), so newModel
		// appends the interrupted note and /continue picks the work back up. A pre-bound start has
		// no Agent to ask, and answers false: nothing is open until something is bound.
		Resumed: resumedSession(resumed, engine.InExchange()),
	})
	// Once the alternate screen is torn down, point the user at how to pick this session back up.
	// ActiveID is non-empty exactly when there is a resumable session — a resumed one, or a fresh
	// one that reached at least one Turn (an empty conversation is never written).
	if host.ActiveID() != "" {
		fmt.Fprintln(os.Stdout, "Session saved · resume with: apogee --continue   (or /sessions inside apogee)")
	}
	return err
}

// ----------------------------------------------------------------------------
// The live-apply seams (the composition root's half of ADR 0037)
// ----------------------------------------------------------------------------
//
// The narrow surfaces a committed `/settings` key is applied through, and the two boundary sentences
// the rows carry back, held here because more than one seam file reads them. The dispatcher itself
// and the holders it moves live in wire_settings.go, wire_tools.go, wire_mcp.go and wire_present.go
// (ADR 0043: a composition root splits by seam).

// settingsEngine is the engine surface the live-apply dispatcher drives: the anytime-safe mutator
// class of ADR 0037 decision 2 — one mutex, one field, consumed at a boundary the loop crosses
// constantly, so a call is safe while a Step runs — plus the two idle-only mutators a settings edit
// reaches directly: SwapTools, the single door for a tool-set change (binding F), and SetProfile, the
// single door for a dialect change. Nothing else: the third idle-only door, Rebind, is driven through
// the rebind closure rather than from here, because a rebind is a per-MODEL resolution the heartbeat
// also drives. It is an interface rather than the holder itself so the dispatcher can be
// exercised against a spy with no Agent behind it, the narrowing every seam this file hands the
// renderer already follows.
type settingsEngine interface {
	SetMode(apogee.Mode)
	SetBypass(bool)
	SetCompactionEnabled(bool)
	SetContextFiles(enable bool, names []string)
	SwapTools(*apogee.ToolRegistry) error
	SetProfile(apogee.ModelProfile) error
}

// settingsSkills is the skill-catalogue surface the dispatcher drives, narrowed off *skills.Provider
// for settingsEngine's reason. The two calls are one act — re-point, then re-scan — because the
// Provider deliberately keeps serving the catalogue it has until someone asks for a fresh one.
type settingsSkills interface {
	SetSources(skills.Sources)
	Reload() error
}

// mcpSession is the connected-MCP surface a reconnect drives: the tools the servers advertise, and
// the teardown that ends the connections. *mcp.Client is the only implementation that ships — it is
// an interface for settingsEngine's reason, so the reconnect's ORDER (dial, swap, tear down) is
// exercisable against a fake without a server process behind it.
type mcpSession interface {
	Tools() []apogee.Tool
	Close() error
}

// contextFileNote is the boundary sentence the `context-files:` keys carry back to the row: the name
// list moves now, but the CONTENT is folded into the standing system prompt only at a session
// boundary (ADR 0026's KV-prefix stability, restated by ADR 0037 decision 3). It is the only
// deferral wording this dispatcher has — nothing here ever says "next launch".
const contextFileNote = "applies at next clear"

// toolRosterNote is the boundary sentence `tools.disabled` carries back: the set is swapped the
// moment the key is committed, and the model learns of it at the next request, whose tool list is
// built from the set the session now holds. It is a boundary, not a deferral — nothing waits for a
// session boundary and nothing waits for a launch.
const toolRosterNote = "applies to the next request"

// ----------------------------------------------------------------------------
// State-root resolution (phase-2 detail plan §3 C7)
// ----------------------------------------------------------------------------

// stateRoots are the resolved, absolute directories injected into Config.
type stateRoots struct {
	config    string
	library   string
	sessions  string
	validated string
	probe     string
	prompts   string
	schemes   string
	workspace string
}

// resolveColorScheme loads one colour scheme by name and renders whatever the load complained about
// to plain lines. It is the single spelling of that pair for both the boot resolution and the live
// switch seam ([tui.Options.ResolveScheme]), so a scheme picked at start-up and the same scheme
// picked from the settings pane cannot answer differently — the same shadowing rule, the same
// forgiving fallbacks, the same sentences.
//
// It never fails: an unknown name, an unreadable file and a defective key each cost a warning and
// keep the built-in default (ADR 0040 design call 8), so what comes back is always a usable palette.
// Rendering the warnings here rather than passing [scheme.Warning] values on is what keeps the
// renderer out of the scheme package: it is handed sentences it prints, not a type it formats.
func resolveColorScheme(name, schemesDir string) (scheme.Scheme, []string) {
	s, warnings := scheme.Resolve(name, schemesDir)
	lines := make([]string, 0, len(warnings))
	for _, w := range warnings {
		lines = append(lines, w.String())
	}
	return s, lines
}

// resolveRoots computes the state roots the library refuses to assume (ADR 0001): the
// apogee home (configDir override, else ~/.apogee) holds config/library/sessions, and the
// workspace (workspace override, else the current directory) scopes the file tools. It
// computes paths only — directory creation is deferred to the writer that needs them (P2.5).
func resolveRoots(configDir, workspace string) (stateRoots, error) {
	absHome, err := config.ApogeeHome(configDir)
	if err != nil {
		return stateRoots{}, err
	}

	ws := workspace
	if ws == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return stateRoots{}, fmt.Errorf("apogee: resolve working directory: %w", err)
		}
		ws = cwd
	}
	absWorkspace, err := filepath.Abs(ws)
	if err != nil {
		return stateRoots{}, fmt.Errorf("apogee: resolve workspace directory: %w", err)
	}

	return stateRoots{
		config:    absHome,
		library:   filepath.Join(absHome, "library"),
		sessions:  filepath.Join(absHome, "sessions"),
		validated: filepath.Join(absHome, "validated"),
		// The behavioral-probe records `apogee probe model` writes and the fingerprint
		// resolver reads back (ADR 0021 §3). Named by internal/library rather than joined
		// here, because the resolver has to find the same directory from the apogee home
		// alone when it is reached from the engine's construction path.
		probe: library.ProbeDir(absHome),
		// Prompt recall: one JSONL file per workspace, keyed by a digest of its path
		// (internal/recall). It lives under the apogee home rather than in the project tree —
		// what the human typed is theirs, not the repository's — and, like every root here, it is
		// a path only: internal/recall creates the directory on the first prompt it records, so a
		// run that sends nothing leaves no trace.
		prompts: filepath.Join(absHome, "prompts"),
		// The user's own colour schemes (ADR 0040): one `<name>.yaml` per scheme, shadowing a
		// built-in of the same name. Like every root here it is a path only — nothing creates the
		// folder until `/color-scheme export` writes into it, and a run whose scheme is a built-in
		// never looks inside it beyond listing what is there.
		schemes:   filepath.Join(absHome, "schemes"),
		workspace: absWorkspace,
	}, nil
}
