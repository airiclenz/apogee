package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/present"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/recall"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/tui"
)

// Compile-time proof that the public Agent satisfies the TUI's narrow engine seam
// (phase-2 detail plan §3 C5): cmd dogfoods apogee.New, and *apogee.Agent (= *agent.Agent)
// is exactly what internal/tui drives — without internal/tui ever importing the root path.
var _ tui.Engine = (*apogee.Agent)(nil)

// The known autonomy modes, named locally so the flag default and parser reference a
// symbol rather than a bare string. The order is the privilege ladder:
// Plan → Ask-Before → Allow-Edits → Auto.
const (
	modePlan       = apogee.ModePlan
	modeAskBefore  = apogee.ModeAskBefore
	modeAllowEdits = apogee.ModeAllowEdits
	modeAuto       = apogee.ModeAuto
)

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
	return mode == modeAuto && confineToWorkspace && fsWrite
}

// ----------------------------------------------------------------------------
// Root command body
// ----------------------------------------------------------------------------

// runRoot is the root command's body: parse the mode, resolve the state roots, build a
// Config, construct (or resume) the Agent through the public surface, and launch the UI.
func runRoot(ctx context.Context, opts options, launch launcher) error {
	mode, err := parseMode(opts.mode)
	if err != nil {
		return err
	}

	roots, err := resolveRoots(opts.configDir, opts.workspace)
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
		UseProjectSkills: opts.useProjectSkills,
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
		opts.present, roots.workspace, runtime.GOOS, os.Getenv, bridge.SetPresentation)
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
	sysPrompt, err := resolveSystemPrompt(opts.systemPrompt, opts.model, roots.config, os.ReadFile)
	if err != nil {
		return err
	}

	cfg := apogee.Config{
		Endpoint: opts.endpoint,
		Model:    opts.model,
		// The upstream bearer token, resolved from the startup `servers:` entry's own `api-key`,
		// which APOGEE_API_KEY overlays. Empty — the keyless local default — sends no
		// Authorization header at all.
		APIKey:       opts.apiKey,
		Mode:         mode,
		Bypass:       opts.bypass,
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
		ConfineToWorkspace: opts.confineToWorkspace,
		WebSearchEndpoint:  opts.webSearchEndpoint,
		// The model profile (CONTEXT: Model profile) — tool-call format + thinking channel —
		// resolved from config.yaml (file-only). A zero profile is native tool calls with no
		// inline thinking, so an unconfigured model behaves exactly as today.
		Profile: opts.profile,
		// The configured system-prompt TEMPLATE (ADR 0023), which the loop renders fresh per
		// request and seeds as the first system message. Empty ⇒ no prompt: the request opens with
		// the user's own message, exactly as it did before this key existed.
		SystemPrompt: sysPrompt,
		// The workspace context files (`context-files:`, file-only): the names the engine looks for
		// in the workspace root at every session boundary, whose content rides the same first system
		// message as the prompt above — verbatim, never as a template. Nil ⇒ the feature is off, and
		// the request is exactly what it was before the key existed.
		ContextFiles: opts.contextFiles,
		Skills:       skillProvider,
		// The `context-window:` PIN (0 when unpinned — nothing probes at startup any more). It is
		// the budget /compact and the automatic Compaction trigger bound their summary request
		// against so compaction survives high fill (the summary call would otherwise overflow near
		// n_ctx); the same value drives the TUI's footer/gauge below. Unpinned it stays 0 until the
		// first heartbeat rebind binds the observed window. CompactionEnabled carries the
		// `auto-compact` key (default on) — the budget-driven automatic trigger (item 9); the
		// on-demand /compact runs regardless of it.
		Context: apogee.ContextConfig{MaxContextTokens: opts.contextWindow, CompactionEnabled: opts.autoCompact},
	}

	// A per-session startup warning whenever Auto runs unconfined (ADR 0012): confine=false
	// is safe only inside a VM, and it is the only blanket loosen in the system.
	if mode == modeAuto && !opts.confineToWorkspace {
		fmt.Fprintln(os.Stderr, unconfinedAutoWarning)
	}

	// The mirror branch: Auto WITH confinement asked for, on a host whose backend cannot
	// enforce it. The ladder gates every terminal command instead — correct, but silent until
	// now, which is what made Auto look broken (ISSUES.md, 2026-07-21). Say it once, name the
	// backend, and point at /confine.
	if notice := probe.DegradedNotice(probe.BackendName(confiner), confiner.Capabilities(), mode, opts.confineToWorkspace); notice != "" {
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
	if shouldPrewarmLabelWalk(mode, opts.confineToWorkspace, confiner.Capabilities().FSWrite) {
		platform.PrewarmLabelWalk(confiner, roots.workspace, os.Stderr)
	}

	// Connect the configured external MCP servers (P3.15) and surface their tools into the
	// Agent's registry. With no servers configured this is dormant (a no-op Client, nil tools).
	// On resume the connection is established FRESH here — no server-side state is restored
	// (ADR 0008). An MCP connect failure is fatal: a configured server that cannot be reached is
	// a misconfiguration the user should see, not a silently-dropped capability.
	mcpClient, err := mcp.Connect(ctx, opts.mcpServers, security.URLGuard{})
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
	toolSet := newLiveTools(cfg.Tools, cfg.WebSearchEndpoint, func(endpoint string) *apogee.ToolRegistry {
		// The set as this session would have built it with another search endpoint: the MCP tools
		// are re-folded from the holder rather than remembered, so a rebuild always carries the
		// connections that are live NOW.
		host := cfg
		host.WebSearchEndpoint = endpoint
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
	manualIDs, err := mechanismIDs(opts.mechanisms, mechanisms.KnownIDs())
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
	resumed, err := resolveResume(store, opts.resume, opts.continueSession, roots.workspace)
	if err != nil {
		return err
	}

	// The engine seam the renderer drives. It is a HOLDER rather than the Agent itself, because
	// construction is no longer something startup always gets to do: with no startup server
	// determined the TUI opens pre-bound and the engine arrives with the human's first pick (ADR
	// 0036 decision 3). Everything below this line wires against the holder and never learns which
	// of the two happened — the seams are identical, and the engine is behind them either way.
	engine := newLateEngine(mode, opts.confineToWorkspace)
	defer engine.Close()

	// The store-backed session host: it persists the active session (per-Turn, at idle, and on
	// quit) and backs the /sessions browser. It owns id minting and the metadata policy — the
	// facts only the binary knows (workspace root, resolved model) — so the renderer stays free of
	// file I/O (phase-2 detail plan §3 C5). Seeded active on a resumed record, it updates that
	// session's file rather than starting a new one.
	host := newSessionHost(store, roots.workspace, opts.model, resumed)

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

	// The bind step: the one place a serverEntry becomes a running session (the Agent, the Monitor,
	// and the binding the out-of-band calls read). A determined startup binds HERE, before the TUI
	// starts, which is what keeps the ordinary path exactly what it was; an undetermined one leaves
	// both holders empty and binds through the seam handed to the renderer below.
	binder := serverBinder{cfg: cfg, resumed: resumed, engine: engine, holder: holder}
	if opts.prebound.Reason == "" {
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
		base, manualIDs, pinnedWindow := live.rebindInputs(opts)
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

	// The one fold that re-points a session at another Upstream, shared by `/server`'s switch and
	// a profile load's follow-the-profile: engine switch, Monitor swap, stored model cleared, in order
	// (see sessionMover.move, which carries the reasoning).
	mover := sessionMover{agent: engine, holder: holder, host: host, live: live}

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
		return mover.move(entry.Endpoint, entry.Name, entry.Model, entry.APIKey)
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
		if err := saveConfigSetting(filepath.Join(roots.config, "config.yaml"), "server", name); err != nil {
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
		return tui.ServerSwitchResult{
			Endpoint:      entry.Endpoint,
			HostAlias:     entry.Name,
			ContextWindow: live.pin(),
		}, nil
	}

	// The llama-launcher seams (ADR 0029 D1): four closures over the bridge in launcher.go, which is
	// the only file that names the library. The `llama-launcher:` key's three values resolve HERE,
	// at the layer that knows the launcher — into the live path holder, which is where "is the
	// integration on" now lives (ADR 0037: the key is editable, so an off session can be switched on
	// without a relaunch). The four members are wired UNCONDITIONALLY for that reason and answer
	// tui.ErrNoLauncher while the holder is empty, which the renderer reads as the host having no
	// launcher — the same degrade the nil seams used to express, now able to change its mind.
	startPath, _ := launcherConfigPath(opts.llamaLauncher)
	launcher := newLauncherPath(startPath)
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

	// The prompt caret's shape. applyConfig already refused a name this build does not know, so the
	// error here cannot fire; ignoring it keeps the parse a single expression, and ParseCursorShape
	// answers an unknown name with the default anyway — a caret is drawn either way.
	cursorShape, _ := tui.ParseCursorShape(opts.cursorShape)

	err = launch(ctx, engine, bridge, tui.Options{
		// Both upstream facts are now honestly launch-time-only: Model is the configured pin ("" on
		// a cold start, where the footer says "connecting…" until the first beat binds one), and
		// ContextWindow is the `context-window:` pin (0 when unpinned). Neither is a discovery
		// result any more — the heartbeat and the rebind closure below own everything after launch.
		Model:     opts.model,
		Endpoint:  opts.endpoint,
		Mode:      mode,
		Bypass:    opts.bypass,
		Workspace: roots.workspace,
		// The apogee home THIS run resolved (--config / APOGEE_CONFIG included), so a report that
		// names a path — /skills telling an empty catalog where discovery looked — names the folder
		// the run actually walks rather than the ~/.apogee default it may not be using.
		ConfigHome:    roots.config,
		ContextWindow: opts.contextWindow,
		HostAlias:     opts.hostAlias,
		// The two upstream seams (ADR 0024): the monitor observes on the TUI's cadence, and the
		// closure applies what the observation implies. Wiring both is what makes the display live.
		// Heartbeat goes through the holder, so the observation follows the session onto another
		// server without the seam — or the renderer — changing shape.
		Heartbeat: holder.Beat,
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
		Prebound:   opts.prebound,
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
		// values, resolved and validated by applyConfig, so the renderer selects rather than parses.
		// The scroll bar is the one key whose polarity flips here — the config says show, the
		// renderer's option says hide, so its zero value is the shown default (see tui.Options).
		Spinner:       opts.ui.spinner,
		SpinnerColor:  opts.ui.spinnerColor,
		HideScrollbar: !opts.ui.showScrollbar,
		// The `cursor-shape:` key: the shape the REAL terminal cursor takes at the prompt caret
		// (steady always — there is no blink key). Selected here, like the two above, so the
		// renderer never parses a config name.
		CursorShape: cursorShape,
		// The two hidden rendering-diagnostic seams (--tui-trace / --tui-diag), passed through as
		// the paths they are: empty on every ordinary run, and the renderer decides what a named
		// one means and when it goes live.
		TracePath: opts.tuiTrace,
		DiagPath:  opts.tuiDiag,
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
		// applyConfig read at startup, so the next run resolves unconfined here without the claim
		// following the file onto any other machine. The renderer learns only the path written —
		// the on-disk format is the binary's business, like the session Save seam below.
		SaveHostAcknowledgement: hostAcknowledgementSaver(
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
		// written and the splice writer owns the file (configwrite.go) — the renderer hands over a
		// path and the value as the file spells it, and learns only whether it landed.
		WriteSetting: func(key, value string) error {
			return saveConfigSetting(filepath.Join(roots.config, "config.yaml"), key, value)
		},
		// Reset is the same write in reverse: the key's active line is REMOVED, so the value goes
		// back to the binary's default rather than being pinned to today's spelling of it.
		ResetSetting: func(key string) error {
			return resetConfigSetting(filepath.Join(roots.config, "config.yaml"), key)
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
			launcher:   launcher,
			present:    presentation,
		}),
		// The `$EDITOR` round trip for the keys no row can hold (ADR 0037 decision 5): out through a
		// command line this binary resolves — the file, the key's own line, the editor this environment
		// names — and back through a re-read that says which keys changed. The pane applies them
		// through the same two homes an in-pane commit uses; nothing here applies anything, so the
		// file's authority and the apply's single path both stay where they were (settingsedit.go).
		ExternalEditSpec: externalEdits.spec,
		ReloadConfig:     externalEdits.changed,
		Skills:           skillProvider,
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
		AutoTitle:     opts.autoTitle,
		// The scheduler surface (ADR 0033): the seam /schedule and /schedule-stop drive, the reason
		// auto is unavailable on this host (empty ⇒ it is available, and the picker offers it), and
		// the activity report the Gate above releases a due Firing on. All three are wired together
		// or not at all — the renderer's nil check on the seam speaks for the set.
		Schedules: schedules,
		ScheduleAutoBlocked: scheduleAutoBlocked(
			probe.BackendName(confiner), confiner.Capabilities(), opts.confineToWorkspace),
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
// The live settings holder (the mutable half of the startup snapshot — ADR 0037)
// ----------------------------------------------------------------------------

// liveSettings owns the resolved values that used to be captured BY VALUE in runRoot's closures and
// were therefore frozen for the life of the process. ADR 0037 decision 1 ends that freeze: a key
// committed in the `/settings` pane applies to the RUNNING session, and the keys that are not engine
// mutators of their own reach the engine by being re-resolved — at the next rebind, the next server
// switch, the next scheduled Firing. Something has to hold what those re-resolutions read, and this
// is it: one place, one mutex, seeded from opts so a session nobody edits behaves exactly as before.
//
// What lives here is the exception list, not a second configuration path. Everything else about a run
// stays in the immutable `options` snapshot the closures still carry, and a value only earns a field
// here once a seam puts it into effect (ADR 0031: no value the human can move without the engine
// hearing about it).
//
// The mutex is real work rather than ceremony. The writes come from the Update goroutine — the pane's
// keypress, through the live-apply dispatcher below — while a scheduled Firing reads them from the
// Scheduler's own goroutine (scheduleWiring.fire), so the fields are genuinely shared. It is an
// RWMutex because reads are the common case (every rebind takes one) and they never nest.
type liveSettings struct {
	mu sync.RWMutex

	// pinnedWindow is the `context-window:` key in tokens: > 0 is the user's pin, which outranks
	// whatever the server reports (ADR 0024 decision 9), and 0 means "discover it, live".
	pinnedWindow int
	// observedWindow is the window the last beat could name — remembered because a pin EDIT re-drives
	// the rebind closure with no beat of its own, and a pin CLEARED to 0 must then bind the discovered
	// window rather than unbind it. Nothing outside a beat knows this number.
	observedWindow int

	// servers is the `servers:` list: the single upstream definition (ADR 0036), which the switch
	// list, the `server:` recording check and the pane's picker all resolve names against.
	servers []serverEntry

	// manualIDs and mechanisms are the two halves of the `mechanisms:` block: the validated enabled
	// ids the engine arms, and the block itself, whose mere non-emptiness is what suppresses a matched
	// Validated set (whole-set-or-nothing, ADR 0016). They move together or the suppression rule and
	// the enable list would describe different configs.
	manualIDs  []apogee.MechanismID
	mechanisms map[string]bool

	// validatedEnable and validatedAlias are the `validated-sets:` block's own two keys — the surface's
	// off-switch and its carry-over map — the other inputs resolveValidatedSet keys a match on.
	validatedEnable bool
	validatedAlias  map[string]string

	// systemPrompt is the `system-prompt-text` / `system-prompt-file` / `system-prompt-models` trio
	// (ADR 0023). It is held whole rather than per key because selection is whole-entry replacement:
	// the three keys are one prompt, and resolveSystemPrompt collapses them per model at every rebind.
	systemPrompt systemPromptSettings

	// contextFilesEnable and contextFileNames are the `context-files:` block's two keys as the session
	// holds them NOW. They live here because each key's edit has to carry the OTHER half — the engine
	// takes the pair (Agent.SetContextFiles) — so switching the block back on installs the names as
	// they stand rather than the ones this run launched with.
	contextFilesEnable bool
	contextFileNames   []string
}

// newLiveSettings seeds the holder with what THIS run resolved. manualIDs is passed in rather than
// re-derived because runRoot has already validated the block against the catalogue and holds the
// answer — deriving it twice is how the two spellings of the same list start to drift.
//
// The context-file PAIR is seeded from the resolved name list, which is the very read the pane's own
// two rows are formatted from (settingsrows.go): the two spellings of "off" collapse into an empty
// list at startup, so an enable read back off that list and a row showing `false` say the same thing.
func newLiveSettings(opts options, manualIDs []apogee.MechanismID) *liveSettings {
	return &liveSettings{
		pinnedWindow:       opts.contextWindow,
		servers:            opts.servers,
		manualIDs:          manualIDs,
		mechanisms:         opts.mechanisms,
		validatedEnable:    opts.validatedSetsEnable,
		validatedAlias:     opts.validatedSetsAlias,
		systemPrompt:       opts.systemPrompt,
		contextFilesEnable: len(opts.contextFiles) > 0,
		contextFileNames:   opts.contextFiles,
	}
}

// pin reports the context-window pin in force right now — what a first binding and a server move
// adopt as the session's window, since the pin is global and survives both.
func (s *liveSettings) pin() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pinnedWindow
}

// setPin moves the pin. 0 restores discover-live, which the next rebind binds from the observed
// window below rather than from nothing.
func (s *liveSettings) setPin(tokens int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pinnedWindow = tokens
}

// observe records the context window a landed beat reported. A beat that could not name one (0) is
// not evidence the window changed — only that this beat could not say — so it is dropped rather than
// written, exactly as the TUI's own observation treats it.
func (s *liveSettings) observe(window int) {
	if window <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observedWindow = window
}

// observed reports the last window a beat could name (0 until one has). It is what a rebind driven by
// something OTHER than a beat — a pin edit — passes as the observation, so an unpinned session lands
// on the server's own window instead of on "unknown".
func (s *liveSettings) observed() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.observedWindow
}

// serverList reports the `servers:` entries as they stand now — the question the `server:` recording
// seam asks, which is about the FILE's list and not about the switchable rows below.
func (s *liveSettings) serverList() []serverEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.servers
}

// choices assembles the servers this session can be MOVED to from the list as it stands now: the same
// upstreamChoices derivation startup made, re-run against the live list rather than the launch one, so
// the one row it may synthesize — the ephemeral `--endpoint` startup, a fact about the invocation that
// no edit can change — is still exactly where it was.
func (s *liveSettings) choices(base options) []serverEntry {
	base.servers = s.serverList()
	return upstreamChoices(base)
}

// setSystemPrompt installs a re-read `system-prompt-*` block. The caller validates first: this is the
// commit half of a validate-then-commit, so a block the file cannot express never displaces a working
// prompt.
func (s *liveSettings) setSystemPrompt(sp systemPromptSettings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.systemPrompt = sp
}

// setContextFilesEnable flips the `context-files:` off-switch and reports the names to install with
// it. The pair is read and written under ONE lock because the engine takes it as a pair: an enable
// that read the names outside the lock could install a half of one edit beside a half of another.
func (s *liveSettings) setContextFilesEnable(on bool) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.contextFilesEnable = on
	return s.contextFileNames
}

// setContextFileNames replaces the `context-files.names:` list and reports the switch to install it
// under — setContextFilesEnable's mirror, and the other half of the same pair.
func (s *liveSettings) setContextFileNames(names []string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.contextFileNames = names
	return s.contextFilesEnable
}

// setServers installs a re-read `servers:` list. Nothing in the engine holds one — the picker, the
// switch and the `server:` recording all resolve names against this field (ADR 0036: one upstream
// definition) — so this store IS the whole apply for that key. The caller validates first, the
// setSystemPrompt posture: a list with a nameless or duplicated entry never displaces a working one.
func (s *liveSettings) setServers(servers []serverEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.servers = servers
}

// setMechanisms installs a re-read `mechanisms:` block: the validated enabled ids and the block
// itself, together, because the block's mere non-emptiness is what suppresses a matched Validated set
// (whole-set-or-nothing, ADR 0016). Written apart they would describe two different configs for as
// long as one rebind takes.
func (s *liveSettings) setMechanisms(ids []apogee.MechanismID, block map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.manualIDs, s.mechanisms = ids, block
}

// setValidatedSets installs a re-read `validated-sets:` block — the surface's off-switch and its
// carry-over map, the two inputs resolveValidatedSet keys a match on, moved together for
// setMechanisms' reason.
func (s *liveSettings) setValidatedSets(enable bool, alias map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.validatedEnable, s.validatedAlias = enable, alias
}

// rebindInputs projects the live values onto a COPY of the startup snapshot and hands back the three
// arguments rebindSpecFor takes them as. It is the one place the overlay is spelled out, so a caller
// cannot re-resolve half from the holder and half from the launch: every re-resolution — the rebind
// closure, a scheduled Firing — opens with this call.
func (s *liveSettings) rebindInputs(base options) (options, []apogee.MechanismID, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	base.contextWindow = s.pinnedWindow
	base.servers = s.servers
	base.mechanisms = s.mechanisms
	base.validatedSetsEnable = s.validatedEnable
	base.validatedSetsAlias = s.validatedAlias
	base.systemPrompt = s.systemPrompt
	return base, s.manualIDs, s.pinnedWindow
}

// ----------------------------------------------------------------------------
// The live-apply dispatcher (the composition root's half of ADR 0037)
// ----------------------------------------------------------------------------

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

// liveTools is the tool set the session is running on. It exists because two different kinds of
// change can reach the tools mid-session and only one of them needs the engine: re-pointing a tool
// that is already registered is a write on the tool itself (the registry holds the pointer, so the
// loop resolves the same object), while adding or removing a tool is a whole-registry replacement
// that has to go through Agent.SwapTools — the single door of ADR 0037 binding F.
//
// Holding the CURRENT registry is what keeps the first kind honest after the second has happened: a
// swap installs a new registry on the Agent, and a root that kept looking up tools in the old one
// would be re-pointing objects nothing dispatches to any more.
//
// The mutex guards the pointer for the same reason lateEngine's does: the writes come from the
// Update goroutine (the pane's keypress) and the tools themselves are read on the loop's worker
// goroutine, which reaches them through the Agent rather than through this holder.
type liveTools struct {
	mu      sync.Mutex
	current *apogee.ToolRegistry
	// endpoint is the web-search endpoint the live set was BUILT from. It is remembered because a
	// rebuild driven by something else entirely — a reconnect to another set of MCP servers — must
	// carry the endpoint this session is actually on, not the one it launched with: rebuilding from
	// the startup value would quietly revert a search-endpoint edit made an hour earlier.
	endpoint string

	// build assembles the whole set as this session would have assembled it at startup, for a given
	// web-search endpoint — host tools plus the MCP tools that are connected now.
	build func(webSearchEndpoint string) *apogee.ToolRegistry
}

// newLiveTools holds the registry the session was constructed with, the endpoint it was built from,
// and the recipe for another.
func newLiveTools(current *apogee.ToolRegistry, endpoint string, build func(webSearchEndpoint string) *apogee.ToolRegistry) *liveTools {
	return &liveTools{current: current, endpoint: endpoint, build: build}
}

// setSearchEndpoint moves web_search to endpoint. While the tool is registered — the ordinary case,
// since the built-in set always carries it (an "off" endpoint registers a tool that declines
// gracefully rather than no tool at all) — the move is a write on the tool the registry already
// holds: nothing is rebuilt, nothing is swapped, and the change is in force for the next call even
// mid-run, which is the whole point of the tool owning a setter.
//
// The swap door is the fallback for a session whose registry has NO web_search to re-point — a set
// narrowed by an earlier swap, or a registry an embedder assembled by hand. Then the endpoint is
// part of the set's identity, so the set is rebuilt and handed to the engine; being idle-only, that
// path can be refused mid-run, and the refusal is what the row reports over a value it has already
// persisted (binding A).
func (t *liveTools) setSearchEndpoint(endpoint string, engine settingsEngine) error {
	if ws := t.webSearch(); ws != nil {
		ws.SetEndpoint(endpoint)
		t.mu.Lock()
		t.endpoint = endpoint
		t.mu.Unlock()
		return nil
	}
	return t.rebuildWith(endpoint, engine)
}

// rebuild reassembles the whole set as this session would assemble it NOW and hands it to the engine
// — the door a change to the tool SET itself goes through when the change is not about any one tool's
// configuration (ADR 0037 binding F). Today that is one caller: a reconnect to another set of MCP
// servers, whose tools are part of the set's identity rather than a field on a tool already in it.
func (t *liveTools) rebuild(engine settingsEngine) error {
	t.mu.Lock()
	endpoint := t.endpoint
	t.mu.Unlock()
	return t.rebuildWith(endpoint, engine)
}

// rebuildWith is the swap both doors share: build, hand to the engine, and become the live set only
// once the engine has taken it. Being idle-only, SwapTools can refuse mid-run — and then nothing has
// moved, which is what makes re-committing the edit a retry rather than a second half-application.
func (t *liveTools) rebuildWith(endpoint string, engine settingsEngine) error {
	next := t.build(endpoint)
	if err := engine.SwapTools(next); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.current, t.endpoint = next, endpoint
	return nil
}

// webSearch resolves the live web_search tool, or nil when the current set has none (or has
// something else under that name — an embedder's own search tool is not one this may re-point).
func (t *liveTools) webSearch() *tools.WebSearch {
	t.mu.Lock()
	registry := t.current
	t.mu.Unlock()
	if registry == nil {
		return nil
	}
	found, ok := registry.Lookup("web_search")
	if !ok {
		return nil
	}
	ws, _ := found.(*tools.WebSearch)
	return ws
}

// mcpSession is the connected-MCP surface a reconnect drives: the tools the servers advertise, and
// the teardown that ends the connections. *mcp.Client is the only implementation that ships — it is
// an interface for settingsEngine's reason, so the reconnect's ORDER (dial, swap, tear down) is
// exercisable against a fake without a server process behind it.
type mcpSession interface {
	Tools() []apogee.Tool
	Close() error
}

// liveMCP owns the MCP connections the session is running on and the recipe for another set of them.
// It exists because `mcp-servers:` is editable mid-session (ADR 0037 decision 6) and a connection is
// not a value: applying the key means dialling a new set, moving the tool registry onto it and tearing
// the old set down, in that order, so that a set that cannot be reached leaves the session on the
// connections it already has.
//
// The holder is what makes the tool registry's rebuild honest afterwards: the build recipe folds in
// whatever this holder currently carries, so a rebuild driven by any reason at all — a search endpoint
// edit, a later reconnect — carries the connections that are live now rather than the ones the
// process started with.
//
// The mutex guards the pointer for liveTools' reason: the writes come from the Update goroutine and
// the surfaced tools are called on the loop's worker goroutine, which reaches them through the
// registry rather than through this holder.
type liveMCP struct {
	mu      sync.Mutex
	current mcpSession

	// connect dials a whole set, all-or-nothing, exactly as startup dialled the first one.
	connect func(servers []mcp.ServerConfig) (mcpSession, error)
}

// newLiveMCP holds the sessions this run connected at startup, and the recipe for another set.
func newLiveMCP(current mcpSession, connect func(servers []mcp.ServerConfig) (mcpSession, error)) *liveMCP {
	return &liveMCP{current: current, connect: connect}
}

// tools surfaces what the connected servers advertise, for folding into a registry build.
func (m *liveMCP) tools() []apogee.Tool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil {
		return nil
	}
	return m.current.Tools()
}

// close tears down whatever set the session ended on — the run's own defer, which is why it reads the
// holder rather than closing the client the process started with.
func (m *liveMCP) close() error {
	m.mu.Lock()
	current := m.current
	m.mu.Unlock()
	return closeSession(current)
}

// swap installs a set and hands back the one it displaced, so a caller that has to put the old one
// back — a reconnect whose registry swap was refused — holds it without a second lock.
func (m *liveMCP) swap(next mcpSession) mcpSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	previous := m.current
	m.current = next
	return previous
}

// reconnect moves the session onto servers: validate-then-commit, where "validate" is the connection
// itself (ADR 0037 decision 6). The new set is dialled FIRST — all-or-nothing, as at startup, but
// reported rather than fatal, because a session that is already running is not a session to kill over
// a server that has gone away — and only once the rebuilt registry has been taken by the engine are
// the old sessions torn down. Every failure leaves the session on the connections and the tool set it
// already had, which is what makes re-committing the edit a retry.
//
// A busy engine is one of those failures: SwapTools is idle-only, so a reconnect committed mid-run is
// refused and reported on the row over a value the file already carries (ADR 0037 binding A).
func (m *liveMCP) reconnect(servers []mcp.ServerConfig, set *liveTools, engine settingsEngine) error {
	next, err := m.connect(servers)
	if err != nil {
		return mcpReconnectFailed(err)
	}
	// The rebuild below folds the holder's tools, so the new set has to be installed before it — and
	// put back by hand if the engine will not take the registry built from it.
	previous := m.swap(next)
	if err := set.rebuild(engine); err != nil {
		m.swap(previous)
		// No orphan: the sessions just opened are torn down before the failure is reported, exactly as
		// mcp.Connect rolls back a half-connected set. The close error is dropped because it describes
		// connections nobody is going to use either way, and the failure worth reporting is the one
		// that already happened.
		_ = closeSession(next)
		return mcpReconnectFailed(err)
	}
	// Nothing dispatches to the old sessions any more, so they go. A close that fails leaves the human
	// nothing to do about it and does not turn a reconnect that worked into an apply that failed.
	_ = closeSession(previous)
	return nil
}

// closeSession tears a set down, tolerating the holder having none: an embedder that wired no MCP at
// all is a dormant holder, and closing nothing is not an error.
func closeSession(s mcpSession) error {
	if s == nil {
		return nil
	}
	return s.Close()
}

// mcpReconnectFailed is what the row says when a reconnect could not land. It names the outcome the
// human most needs to know — that the servers they were talking to a moment ago are still connected —
// because the alternative reading of a failed reconnect is that the session now has no MCP tools.
func mcpReconnectFailed(err error) error {
	return fmt.Errorf("reconnect failed: %w — previous connections kept", err)
}

// contextFileNote is the boundary sentence the `context-files:` keys carry back to the row: the name
// list moves now, but the CONTENT is folded into the standing system prompt only at a session
// boundary (ADR 0026's KV-prefix stability, restated by ADR 0037 decision 3). It is the only
// deferral wording this dispatcher has — nothing here ever says "next launch".
const contextFileNote = "applies at next clear"

// settingsApplier is everything a committed key can have to reach, in one value rather than in a
// constructor that grows an argument per key class. It is composed at the composition root, where all
// six members already exist, and it is what makes the dispatcher exercisable without a session: every
// member is either a narrow interface, a closure, or a plain value a test can supply.
type settingsApplier struct {
	// engine is the anytime-safe mutator class: a key here is in force the moment it returns.
	engine settingsEngine
	// live is the startup snapshot's mutable half — where a key that is re-RESOLVED rather than
	// pushed (the window pin, the system prompt) is written before the re-resolution is driven.
	live *liveSettings
	// binding reads the Upstream binding as it stands now; wired to upstreamHolder.Binding. Its Model
	// is what a rebind must be driven FOR — an empty one means nothing is bound yet.
	binding func() upstreamBinding
	// rebind is [tui.Options.Rebind]'s own closure: the per-model re-resolution, which reads live
	// through rebindInputs and commits through the engine's idle-only Rebind.
	rebind func(model string, window int) (tui.RebindResult, error)
	// configPath is the config.yaml this session resolved — re-read whole for the keys whose value is
	// a structure no single string can spell.
	configPath string
	// skills is the shared skill catalogue: the SAME Provider the loop resolves attached ids against
	// and the "/" menu lists, so re-pointing it at another source layering moves both at once.
	skills settingsSkills
	// tools is the session's live tool set — where a tool is re-pointed in place, and the door a
	// set-level change goes through.
	tools *liveTools
	// mcp is the session's live MCP connections: the one key whose apply is a reconnect rather than a
	// write, and the source of half the tool set the door above swaps.
	mcp *liveMCP
	// launcher is the config path the local-server verbs read, and with it whether they work at all.
	launcher *launcherPath
	// present is the presentation ladder, which rebuilds from a changed `present:` block and
	// re-installs itself on the presenter the engine holds.
	present *livePresentation
	// roots are this session's resolved state dirs, which is what the skill source layering is spelled
	// out of (the same three fields runRoot builds the startup Sources from).
	roots stateRoots
}

// applySettingFor builds the [tui.Options.ApplySetting] dispatcher: the one place a key the pane has
// just persisted becomes a call on a live seam (ADR 0037 decision 1's apply step). It is keyed by
// REGISTRY PATH because that is the only name the renderer knows a setting by — the pane hands back
// the same path and the same file-spelled value it handed [tui.Options.WriteSetting], and the
// resolution from that string into whatever the seam takes happens here, where the schema lives
// (ADR 0031: the engine is handed values, never config text).
//
// It returns the row's boundary note and the apply's refusal. A key this build cannot apply is an
// ERROR naming the key rather than a silent success: the write has already landed, so the honest
// report is that the file changed and the session did not.
//
// Two classes of key and no third. One is PUSHED at an engine mutator and is in force on return. The
// other is re-RESOLVED: the new value lands in the holder and the per-model resolution is re-driven
// over it (rideTheRebind), which is how the window pin and the system prompt reach an engine that has
// no setter for either — the same path a heartbeat-observed model change already takes.
func applySettingFor(a settingsApplier) func(key, value string) (string, error) {
	return func(key, value string) (string, error) {
		// A member this Driver did not compose is a legitimate configuration rather than a bug (ADR
		// 0031: the engine stays sufficient for any Driver), so the key it would have been reached
		// through is refused in the dispatcher's own words — never dereferenced. Asked first, so a
		// key that cannot land does no work on its way to saying so.
		if err := a.unreachable(key); err != nil {
			return "", err
		}
		switch key {
		case "mode":
			mode, err := parseMode(value)
			if err != nil {
				return "", err
			}
			a.engine.SetMode(mode)
		case "bypass":
			on, err := settingBool(key, value)
			if err != nil {
				return "", err
			}
			a.engine.SetBypass(on)
		case "auto-compact":
			on, err := settingBool(key, value)
			if err != nil {
				return "", err
			}
			a.engine.SetCompactionEnabled(on)
		case "context-files.enable":
			on, err := settingBool(key, value)
			if err != nil {
				return "", err
			}
			// The names the session holds NOW: this run's resolution until a names edit replaced them.
			// A block that STARTED off resolved to no names at all (the two spellings of "off" collapse
			// at startup), so what switching it back on installs is whatever the names row has since
			// been given — which is why the two keys share one holder rather than one closure each.
			a.engine.SetContextFiles(on, a.live.setContextFilesEnable(on))
			return contextFileNote, nil
		case "context-files.names":
			// The value arrives as the FILE spells it — the one-line flow sequence the writer just
			// rendered — and is read back by the same parse, so the engine is handed the list a reader
			// takes out of the file rather than a second reading of the keystrokes. The switch travels
			// with it: names alone are not a state the engine can be put into.
			names := parseSettingList(value)
			a.engine.SetContextFiles(a.live.setContextFileNames(names), names)
			return contextFileNote, nil
		case "use-project-skills":
			on, err := settingBool(key, value)
			if err != nil {
				return "", err
			}
			// The same layering startup spells, with the flag as it now stands. Re-pointing alone would
			// change nothing anybody sees — the Provider serves the catalogue it has until asked for a
			// fresh one — so the re-scan is part of the same act.
			a.skills.SetSources(skills.Sources{
				Home:             a.roots.config,
				Workspace:        a.roots.workspace,
				UseProjectSkills: on,
			})
			// The scan's error is soft and is dropped here for the reason the "/" menu's reload drops
			// it: Load never signals an unusable catalogue — a malformed skill is skipped and shown in
			// the /skills report — so a partial scan is not a failed apply.
			_ = a.skills.Reload()
		case "web-search-endpoint":
			return "", a.tools.setSearchEndpoint(value, a.engine)
		case "llama-launcher":
			// The startup ladder run a second time (ADR 0029 D4's three shapes), so `off` and a
			// missing auto-detect both resolve to the empty path the verbs report themselves off
			// from, and a named config is on from the next verb. Nothing is connected to rebuild —
			// every verb re-reads the file — so the whole apply is this store.
			if err := validateLlamaLauncher(value); err != nil {
				return "", err
			}
			path, _ := launcherConfigPath(value)
			a.launcher.set(path)
		case "present.auto-open", "present.command", "present.port", "present.host":
			return "", a.present.apply(key, value)
		case "context-window":
			tokens, err := settingInt(key, value)
			if err != nil {
				return "", err
			}
			// 0 keeps meaning discover-live (ADR 0024): the re-drive below binds the window the last
			// beat reported, so clearing a pin hands the session back to the server rather than to
			// "unknown". No note — the pin is what the Budget and Compaction measure against from the
			// moment the rebind commits.
			a.live.setPin(tokens)
			return "", a.rideTheRebind()
		case "system-prompt-text", "system-prompt-file", "system-prompt-models":
			// The value the pane persisted is deliberately not read here: these three keys are ONE
			// prompt (ADR 0023, whole-entry selection) and `system-prompt-models:` is a map no single
			// string spells, so the block is re-read from the file the pane just wrote and re-resolved
			// per model by the rebind — exactly the resolution startup made.
			if err := a.reloadSystemPrompt(); err != nil {
				return "", err
			}
			return "", a.rideTheRebind()
		case "servers":
			// The `servers:` list reaches no engine seam at all: it is the single upstream definition
			// (ADR 0036) that the picker, the `/server` switch and the choice recording resolve names
			// against, and all three read the holder — so installing the re-read list IS the apply. The
			// value the pane persisted is not read, for the system-prompt keys' reason: a list of blocks
			// is a shape no single string spells.
			return "", a.reloadServers()
		case "mechanisms":
			// Neither block is a value the engine holds either: they are INPUTS to the per-model
			// resolution — the enable list and the whole-set-or-nothing suppression rule (ADR 0016) —
			// so they land in the holder and are committed by the rebind, the one door a model change
			// and a config change share (rideTheRebind).
			if err := a.reloadMechanisms(); err != nil {
				return "", err
			}
			return "", a.rideTheRebind()
		case "validated-sets":
			if err := a.reloadValidatedSets(); err != nil {
				return "", err
			}
			return "", a.rideTheRebind()
		case "mcp-servers":
			// The one key whose value is a set of live CONNECTIONS. It is not pushed and not
			// re-resolved into a holder either: it is dialled, and the session moves onto the servers
			// that answered (ADR 0037 decision 6). The value the pane persisted is not read, for the
			// `servers:` reason — a list of blocks is a shape no single string spells.
			return "", a.reconnectMCP()
		case "model-profile":
			// The dialect the model speaks (CONTEXT: Model profile) is a GLOBAL setting with its own
			// engine door, deliberately not the per-model rebind: a model change is not a dialect
			// change, so Rebind leaves the profile alone and SetProfile is where it moves.
			return "", a.reloadProfile()
		default:
			return "", cannotApply(key)
		}
		return "", nil
	}
}

// cannotApply is the dispatcher's one refusal for a key that will not reach the session at all —
// because this build knows no seam for it, or because this Driver composed the dispatcher without
// the member that seam lives behind. It names the key, since the row it lands on is that key's, and
// it is deliberately the SAME sentence for both: to the human they are one fact, that the file
// changed and the session did not.
func cannotApply(key string) error {
	return fmt.Errorf("apogee: %s cannot be applied to the running session", key)
}

// unreachable reports, for one key, that this applier was composed without something that key's
// apply has to reach. Every member is optional by design — a Driver builds the dispatcher out of
// what it HAS, and a bench or a daemon has no presenter, no launcher and no skill catalogue (ADR
// 0031) — so a nil member has to degrade to the refusal above rather than panic on the Update
// goroutine, halfway through an edit that has already been written to the file.
//
// It is a second switch over the same keys, which is a drift risk taken deliberately and closed by a
// test: TestApplySettingRefusesEveryKeyItCannotReach drives EVERY registry key through a zero
// applier, so a key added to the dispatcher without a line here fails as the panic it would be.
func (a settingsApplier) unreachable(key string) error {
	// The rebind-riding keys share one triple: the value lands in the holder and the per-model
	// resolution is re-driven over it, so all three members are what makes the apply an apply.
	rides := a.live != nil && a.binding != nil && a.rebind != nil
	reaches := true
	switch key {
	case "mode", "bypass", "auto-compact", "model-profile":
		reaches = a.engine != nil
	case "context-files.enable", "context-files.names":
		reaches = a.engine != nil && a.live != nil
	case "use-project-skills":
		reaches = a.skills != nil
	case "web-search-endpoint":
		// The engine as well as the tool set: a registry with no web_search to re-point is rebuilt and
		// handed through SwapTools, which is the swap door and not this holder's to skip.
		reaches = a.tools != nil && a.engine != nil
	case "llama-launcher":
		reaches = a.launcher != nil
	case "present.auto-open", "present.command", "present.port", "present.host":
		reaches = a.present != nil
	case "context-window", "system-prompt-text", "system-prompt-file", "system-prompt-models",
		"mechanisms", "validated-sets":
		reaches = rides
	case "servers":
		// The one re-read key with no rebind behind it: the holder IS the whole apply.
		reaches = a.live != nil
	case "mcp-servers":
		reaches = a.mcp != nil && a.tools != nil && a.engine != nil
	}
	if reaches {
		return nil
	}
	return cannotApply(key)
}

// rideTheRebind re-drives the per-model resolution for the model the session is bound to right now.
// It is how a key with no engine setter of its own is applied: the value is already in the holder,
// rebindSpecFor reads it there, and Agent.Rebind commits the whole per-model binding atomically —
// one door for a model change and a config change alike, rather than a second, subtly different way
// to move the same four fields.
//
// With no model bound — a cold start before the first beat, or the gap a `/server` switch opens —
// there is nothing to rebind and nothing to report: the holder carries the change, and the first beat
// that binds a model resolves it in. A refusal from the engine (Rebind is idle-only, so an open
// Exchange is one) is returned, which the pane renders as the row's apply failure over the value it
// has already persisted; re-committing the edit retries it at a quieter moment.
func (a settingsApplier) rideTheRebind() error {
	model := a.binding().Model
	if model == "" {
		return nil
	}
	_, err := a.rebind(model, a.live.observed())
	return err
}

// reloadSystemPrompt re-reads the `system-prompt-*` block from the config file and installs it on the
// holder, validate-then-commit: a block the file cannot express — both spellings of one prompt at
// once, an entry with neither — is refused before it displaces a prompt that works.
//
// Only the FILE layer carries these keys (there is no flag or environment variable for a prompt), so
// re-reading that one layer resolves them exactly as startup resolved them. The migration notice is
// dropped rather than surfaced: a file still in the retired schema was already migrated and announced
// at launch, and this read happens after the pane has just written to it.
func (a settingsApplier) reloadSystemPrompt() error {
	l, err := loadFileConfig(a.configPath, os.ReadFile, func(string) {})
	if err != nil {
		return err
	}
	var sp systemPromptSettings
	if l.systemPrompt != nil {
		sp = *l.systemPrompt
	}
	if err := sp.validate(); err != nil {
		return err
	}
	a.live.setSystemPrompt(sp)
	return nil
}

// reloadServers re-reads the `servers:` block and installs it on the holder, validate-then-commit:
// an entry that could never be switched to — no name, no endpoint, or a name an earlier entry took —
// is refused by the SAME check startup runs, before it can displace a list that works.
//
// Only the FILE layer carries the list (no flag, no environment variable names an upstream), so
// re-reading that one layer resolves it exactly as startup resolved it — reloadSystemPrompt's own
// reasoning, and the reason the migration notice is dropped here too.
func (a settingsApplier) reloadServers() error {
	l, err := loadFileConfig(a.configPath, os.ReadFile, func(string) {})
	if err != nil {
		return err
	}
	if err := validateServers(l.servers); err != nil {
		return err
	}
	a.live.setServers(l.servers)
	return nil
}

// reloadMechanisms re-reads the `mechanisms:` block and installs both halves of it. The ids are
// derived through the startup producer (mechanismIDs), which validates EVERY key of the block —
// enabled and disabled alike — so a Mechanism id this build does not know is refused here rather than
// silently arming nothing, exactly as it is at launch (ADR 0015 §1).
func (a settingsApplier) reloadMechanisms() error {
	l, err := loadFileConfig(a.configPath, os.ReadFile, func(string) {})
	if err != nil {
		return err
	}
	ids, err := mechanismIDs(l.mechanisms, mechanisms.KnownIDs())
	if err != nil {
		return err
	}
	a.live.setMechanisms(ids, l.mechanisms)
	return nil
}

// reloadValidatedSets re-reads the `validated-sets:` block. An absent off-switch resolves to ON, the
// default resolveSettings starts from, so a block the human deleted goes back to the surface being
// available rather than to it being off — the difference between "unset" and "false" that the file
// layer's pointer carries.
func (a settingsApplier) reloadValidatedSets() error {
	l, err := loadFileConfig(a.configPath, os.ReadFile, func(string) {})
	if err != nil {
		return err
	}
	enable := true
	if l.validatedSetsEnable != nil {
		enable = *l.validatedSetsEnable
	}
	a.live.setValidatedSets(enable, l.validatedSetsAlias)
	return nil
}

// reconnectMCP re-reads the `mcp-servers:` block and moves the session onto it. The dial and the
// registry swap are liveMCP.reconnect's; what belongs here is the same one thing every structured
// key's apply does — resolve the file layer exactly as startup resolved it — because only the FILE
// carries this key (no flag, no environment variable names an MCP server).
//
// A file that no longer parses is refused before anything is dialled, so a typo in an unrelated key
// cannot cost the session the servers it is talking to.
func (a settingsApplier) reconnectMCP() error {
	l, err := loadFileConfig(a.configPath, os.ReadFile, func(string) {})
	if err != nil {
		return err
	}
	return a.mcp.reconnect(l.mcpServers, a.tools, a.engine)
}

// reloadProfile re-reads the `model-profile:` block and swaps it into the running engine. The
// resolution is the composition root's (the on-disk schema projected through toModelProfile, exactly
// as startup projects it) and the VALIDATION is the engine's: SetProfile builds the profile's two
// collaborators before it commits, so a tool-call format this build cannot parse leaves the session
// reading responses exactly as it did — and says so on the row.
//
// An absent block resolves to the zero profile, which is what startup would have resolved it to: a
// human who deleted the block asked for native tool calls and no inline thinking channel, not for
// whatever the process happened to launch with.
func (a settingsApplier) reloadProfile() error {
	l, err := loadFileConfig(a.configPath, os.ReadFile, func(string) {})
	if err != nil {
		return err
	}
	var profile apogee.ModelProfile
	if l.profile != nil {
		profile = *l.profile
	}
	return a.engine.SetProfile(profile)
}

// settingInt reads a whole count the same way (kindInt's own validators do, validateContextWindow),
// so a value the registry accepted is a value this parses. Negative is refused here too rather than
// trusted from the file: the pane is not the only thing that can write one.
func settingInt(key, value string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("apogee: %s is a count of 0 or more, not %q", key, value)
	}
	return n, nil
}

// settingBool reads a bool exactly as the splice writer renders one (renderSettingValue), so the
// value a key was persisted with is the value it is applied with. The message names the key, because
// it lands on that key's row.
func settingBool(key, value string) (bool, error) {
	on, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("apogee: %s is true or false, not %q", key, value)
	}
	return on, nil
}

// ----------------------------------------------------------------------------
// Per-model re-resolution (the composition root's half of a rebind — ADR 0024)
// ----------------------------------------------------------------------------

// rebindSpecFor re-resolves every per-model binding for a model the heartbeat observed, and returns
// the whole answer as one [apogee.RebindSpec] plus the per-session notices worth telling the human.
// It is the same resolution runRoot does at startup, run again with a different model — deliberately
// so: a session that switches models mid-conversation must land in exactly the state a session
// started on that model would have been in, or "rebind" would be a second, subtly different way of
// configuring the engine.
//
// What it re-resolves:
//   - the system-prompt template, because `system-prompt-models:` keys on the model name (ADR 0023);
//   - the validated Mechanism set, because a set is matched against the model's identity fingerprint
//     (ADR 0016) — the opts copy carries the new id so the fingerprint re-keys on it;
//   - the enable list, applying the same precedence startup applies: an explicit `mechanisms:` block
//     is manual control and suppresses any matched set (whole-set-or-nothing, never a merge), which
//     is why manualIDs is passed in rather than re-derived from the map here;
//   - the context window, applying the pin: pinnedWindow > 0 is the user's `context-window:` key and
//     outranks whatever the server reports (decision 9), else the observed window is bound as-is.
//
// What it deliberately does NOT touch: `model-profile:`, which is a GLOBAL key (one profile per
// installation, not per model) and therefore not a per-model binding at all; the endpoint, the mode,
// the tools, and the conversation, none of which a model change has any claim on.
//
// A resolution failure is returned rather than swallowed — an unreadable per-model prompt file or a
// dangling validated-sets alias is the user's own config being wrong about the new model — and the
// caller then leaves the engine bound to what it had, which is the honest outcome.
func rebindSpecFor(
	opts options,
	roots stateRoots,
	manualIDs []apogee.MechanismID,
	model string,
	window, pinnedWindow int,
) (apogee.RebindSpec, []string, error) {
	next := opts
	next.model = model

	sysPrompt, err := resolveSystemPrompt(next.systemPrompt, model, roots.config, os.ReadFile)
	if err != nil {
		return apogee.RebindSpec{}, nil, err
	}

	vset, notices, err := resolveValidatedSet(next, roots.validated, roots.probe)
	if err != nil {
		return apogee.RebindSpec{}, nil, err
	}
	enable := manualIDs
	if len(vset) > 0 {
		enable = vset
	}

	bound := window
	if pinnedWindow > 0 {
		bound = pinnedWindow
	}

	return apogee.RebindSpec{
		Model:            model,
		SystemPrompt:     sysPrompt,
		MaxContextTokens: bound,
		EnableMechanisms: enable,
	}, notices, nil
}

// presentationRungs builds the host-side presentation ladder (ADR 0019) from the resolved
// `present:` block and this session's environment: the mechanisms that exist on THIS machine, for
// the TUI's presenter to walk. goos and env are injected — every seam in internal/present is, for
// exactly this reason — so the wiring is table-testable off whatever machine the tests run on.
//
// A rung is wired only where this session could walk it, because internal/tui reads a zero field
// as "a rung this host did not wire" rather than as a failure (tui.Presentation):
//
//   - the Opener (rungs 1 and 3) on a LOCAL session with auto-open on. Remote is excluded here
//     rather than inside the Opener because an opener fired on a remote box opens into a display
//     nobody is watching; `auto-open: false` wires none either, which covers the command override
//     too — the key says whether a document is opened, present.command only says by what.
//   - the doc server (rung 2) on a REMOTE session, where the user's browser is on another machine.
//     It binds nothing until the first served presentation, so wiring it costs one struct. Its
//     advertised address is resolved HERE, once: AdvertiseHost may probe the routing table, and
//     where the user reaches this box from cannot change mid-session. It is also handed the
//     workspace root — the same root the file tools are scoped to — because it re-checks every
//     served document against that fence on every request, not once at the grant.
//
// Rung 0 — the transcript line carrying the path — is deliberately absent: it needs no mechanism,
// it is never skipped, and nothing in the config can turn it off.
func presentationRungs(p presentSettings, workspace, goos string, env func(string) string) tui.Presentation {
	rungs := tui.Presentation{Local: present.Locality(env) == present.Local}
	if rungs.Local && p.autoOpen {
		rungs.Opener = &present.Opener{GOOS: goos, Env: env, CommandOverride: p.command}
	}
	if !rungs.Local {
		rungs.Docs = &present.DocServer{
			Host: present.AdvertiseHost(env, p.host),
			Port: p.port,
			Root: workspace,
		}
	}
	return rungs
}

// livePresentation owns the `present:` block as it stands right now and the ladder built from it —
// the presentation half of ADR 0037 decision 1. The four keys are editable in the `/settings` pane,
// and a committed one lands here: the block is updated, the rungs are rebuilt exactly as startup
// built them, and they are re-installed on the presenter (tui.Bridge.SetPresentation, which swaps
// the rungs of the presenter the engine captured rather than making a second one).
//
// It also owns the doc server's LIFETIME, because a rebuild is the only thing that can end one
// mid-session: the app's own teardown closes whatever is current, and an address change closes the
// server it displaced (ADR 0037 binding D — the URLs a moved listener issued die with it, which is
// inherent to changing the port).
//
// The mutex guards the pair — settings and rungs must never disagree — and the writes come from the
// Update goroutine while the presenter reads its own copy of the rungs under its own lock.
type livePresentation struct {
	mu       sync.Mutex
	settings presentSettings
	rungs    tui.Presentation

	// The three inputs presentationRungs takes beside the block, held so a rebuild is the identical
	// call startup made. env is injected for the same reason it is injected there: so the whole
	// holder is testable off whatever machine the tests run on.
	workspace string
	goos      string
	env       func(string) string
	// install hands the rebuilt ladder to the renderer; wired to tui.Bridge.SetPresentation.
	install func(tui.Presentation)
}

// newLivePresentation builds this session's ladder and installs it — the call that also makes
// bridge.Presenter() non-nil, and with it registers present_document.
func newLivePresentation(p presentSettings, workspace, goos string, env func(string) string,
	install func(tui.Presentation)) *livePresentation {
	live := &livePresentation{
		settings:  p,
		workspace: workspace,
		goos:      goos,
		env:       env,
		install:   install,
	}
	live.rungs = presentationRungs(p, workspace, goos, env)
	install(live.rungs)
	return live
}

// apply moves one `present.` key and re-installs the ladder built from the block it leaves behind.
// The value is the one the file now spells; it is parsed here rather than trusted, exactly as the
// other dispatcher entries parse theirs.
func (l *livePresentation) apply(key, value string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	next := l.settings
	switch key {
	case "present.auto-open":
		on, err := settingBool(key, value)
		if err != nil {
			return err
		}
		next.autoOpen = on
	case "present.command":
		next.command = value
	case "present.port":
		port, err := settingInt(key, value)
		if err != nil {
			return err
		}
		next.port = port
	case "present.host":
		next.host = value
	default:
		return fmt.Errorf("apogee: %s is not a presentation setting", key)
	}
	// The startup check on the whole block, so a value that would fail deep inside the first
	// presentation is refused here instead — the same validate() the registry's own validator runs.
	if err := next.validate(); err != nil {
		return err
	}

	rungs := presentationRungs(next, l.workspace, l.goos, l.env)
	// A doc server whose address did not move keeps SERVING: the listener stays bound and the grants
	// it issued stay valid, because nothing about it changed (an auto-open or command edit on a
	// remote session rebuilds an identical rung). One whose address did move is displaced, and the
	// old one is closed below — after the new ladder is installed, so no presentation in between
	// climbs to a server that is going away.
	displaced := l.rungs.Docs
	if displaced != nil && rungs.Docs != nil && sameDocServer(displaced, rungs.Docs) {
		rungs.Docs, displaced = displaced, nil
	}

	l.settings, l.rungs = next, rungs
	l.install(rungs)
	if displaced != nil {
		_ = displaced.Close()
	}
	return nil
}

// sameDocServer reports whether two doc servers would bind and advertise the same thing — the whole
// of what configuration says about one (its three exported fields; everything else is the running
// state a rebuilt value has none of). Equal means the bound listener may simply be carried over.
func sameDocServer(a, b *present.DocServer) bool {
	return a.Host == b.Host && a.Port == b.Port && a.Root == b.Root
}

// close ends the session's presentation: the current doc server's listener, whichever one that is
// after any number of `present.port` edits. Idempotent, like DocServer.Close itself.
func (l *livePresentation) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.rungs.Docs != nil {
		_ = l.rungs.Docs.Close()
	}
}

// registryWithMCP builds the Agent's tool registry: the built-in default tools scoped to the
// workspace (with the same host configuration the Agent would derive from Config — the
// url-safety floor, the web-search endpoint, the Asker, the Presenter) PLUS the dynamically
// discovered MCP tools registered on top. MCP tools are DYNAMIC (discovered from a server at
// runtime), so they are NOT in DefaultTools — they ride the registry as classMCP
// ExternalEffectTools the dispatch disposition gates in Auto. A duplicate name (an MCP server's
// qualified tool colliding with a built-in — unlikely given the alias prefix) is dropped with a
// stderr notice rather than failing startup; the built-in wins.
func registryWithMCP(workspace string, cfg apogee.Config, mcpTools []apogee.Tool) *apogee.ToolRegistry {
	registry := tools.NewDefaultRegistryWithHost(workspace, tools.HostTools{
		URLGuard:          security.URLGuard{},
		WebSearchEndpoint: cfg.WebSearchEndpoint,
		Asker:             cfg.Asker,
		Presenter:         cfg.Presenter,
	})
	for _, t := range mcpTools {
		if err := registry.Register(t); err != nil {
			fmt.Fprintf(os.Stderr, "apogee: skipping MCP tool %q: %v\n", t.Name(), err)
		}
	}
	return registry
}

// mechanismIDs validates every `mechanisms:` config key against the known catalogue and returns the
// enabled IDs in sorted canonical order for Config.EnableMechanisms — the engine (apogee.New/Resume)
// builds them, derives their Deps, and runs the stacking gates (ADR 0015 §1: wire.go collapses to a
// YAML→ID-list producer). EVERY key is validated here, enabled AND disabled: the engine only ever
// sees the enabled IDs, so a typo'd DISABLED key — never constructed — must still fail loudly at this
// startup boundary (phase-4-review-fixes item 5). An unknown key, whether true or false, is a loud
// error naming the known catalogue. Keys are walked in sorted spelling so the returned list (and any
// engine-side build error over it) is deterministic; the dispatch order is the registry's own
// topo-sort (ADR 0003), independent of this order. With nothing enabled it returns nil, so
// Config.EnableMechanisms stays empty and the engine arms nothing (today's behaviour for a config
// without a mechanisms block).
func mechanismIDs(enabled map[string]bool, known []apogee.MechanismID) ([]apogee.MechanismID, error) {
	knownSet := make(map[string]bool, len(known))
	for _, id := range known {
		knownSet[string(id)] = true
	}

	keys := make([]string, 0, len(enabled))
	for id := range enabled {
		keys = append(keys, id)
	}
	sort.Strings(keys)

	ids := make([]apogee.MechanismID, 0, len(keys))
	for _, id := range keys {
		if !knownSet[id] {
			return nil, fmt.Errorf("apogee: unknown mechanism %q; known: %s", id, knownMechanismList(known))
		}
		if enabled[id] {
			ids = append(ids, apogee.MechanismID(id))
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return ids, nil
}

// knownMechanismList renders the known catalogue for the unknown-key error, matching the engine's
// own unknown-ID error tail (an empty catalogue renders "(none)").
func knownMechanismList(known []apogee.MechanismID) string {
	if len(known) == 0 {
		return "(none)"
	}
	parts := make([]string, len(known))
	for i, id := range known {
		parts[i] = string(id)
	}
	return strings.Join(parts, ", ")
}

// sessionHost adapts a session.Store to the TUI's [tui.SessionHost] seam: it owns the active
// session's id and the metadata policy the renderer must not, stamps the wiring facts only the
// binary knows (workspace root, resolved model) onto every record, and delegates listing, loading,
// deletion, and renaming to the store. It is the composition root's single owner of id minting and
// metadata, keeping both out of the renderer (phase-2 detail plan §3 C5).
//
// The mutable fields are mutex-guarded: Save runs on a Bubble Tea Cmd goroutine while ActiveID, the
// browser verbs, and SetModel are driven from the Update loop, so the two can race.
type sessionHost struct {
	store     *session.Store
	workspace string
	now       func() time.Time

	mu sync.Mutex
	// model is the model id stamped on saved metadata. It MOVES: a heartbeat rebind switches the
	// session's model mid-conversation (ADR 0024), and SetModel is how the composition root's
	// rebind closure keeps the record's metadata describing what the conversation actually ran
	// against. Guarded because that closure runs on the Update goroutine while Save runs on a Cmd.
	model  string
	active *activeSession // nil ⇒ no active session; the next Save mints one
}

// activeSession is the identity of the session Saves currently target: the id minted once (or
// seeded by a resume), plus the CreatedAt and Title a later Save must preserve — only Rename
// rewrites the title, and only Rotate/Load changes the id.
type activeSession struct {
	id        string
	title     string
	createdAt time.Time
}

// sessionHost satisfies the persistence seam the TUI drives.
var _ tui.SessionHost = (*sessionHost)(nil)

// newSessionHost builds the host over a store and the run's wiring facts. When resumed is non-nil
// (a --resume/--continue start) the host begins ACTIVE on that record, so subsequent Saves update
// its file in place — its id, CreatedAt, and Title carried over rather than a new session forked.
func newSessionHost(store *session.Store, workspace, model string, resumed *session.Record) *sessionHost {
	h := &sessionHost{store: store, workspace: workspace, model: model, now: time.Now}
	if resumed != nil {
		h.active = &activeSession{
			id:        resumed.Meta.ID,
			title:     resumed.Meta.Title,
			createdAt: resumed.Meta.CreatedAt,
		}
	}
	return h
}

// Save persists the active session, minting its id (and fixing its Title and CreatedAt) on the
// first call and updating that same file thereafter. Title is set at create and never overwritten
// by a later Save — Rename is the only writer that changes it, so a user rename sticks — while
// UpdatedAt, the transcript blob, and the browsable counts refresh every Save. Workspace and Model
// come from the wiring, the facts the renderer cannot know.
func (h *sessionHost) Save(sess apogee.Session, transcript []byte, title string, userMsgs, ctxUsed int) error {
	now := h.now().UTC()
	h.mu.Lock()
	if h.active == nil {
		h.active = &activeSession{id: session.NewID(now), title: title, createdAt: now}
	}
	a := *h.active
	model := h.model
	h.mu.Unlock()

	return h.store.Save(session.Record{
		Meta: session.Meta{
			ID:        a.id,
			Title:     a.title,
			CreatedAt: a.createdAt,
			UpdatedAt: now,
			Workspace: h.workspace,
			Model:     model,
			UserMsgs:  userMsgs,
			CtxUsed:   ctxUsed,
		},
		Transcript: transcript,
		Session:    sess,
	})
}

// SetModel restamps the model recorded on subsequent Saves. The composition root's rebind closure
// calls it once the engine has actually been rebound, so the stored metadata names the model the
// conversation is running against rather than the one it launched with — including on a cold start,
// where the session began with no model bound at all. It does not rewrite already-saved records:
// the next Save updates the same file with the new id, which is the session's current truth.
func (h *sessionHost) SetModel(model string) {
	h.mu.Lock()
	h.model = model
	h.mu.Unlock()
}

// Rotate closes the active session so the next Save mints a fresh id (the /clear|/new boundary).
// It is idempotent on an already-inactive host.
func (h *sessionHost) Rotate() {
	h.mu.Lock()
	h.active = nil
	h.mu.Unlock()
}

// List returns every stored session's browsable metadata, newest first (the store's ordering).
func (h *sessionHost) List() ([]session.Meta, error) { return h.store.List() }

// Load returns a stored record; it does NOT change the active session. Activation is deferred to
// Activate so the /sessions resume flow switches which file Saves target only after the live
// RestoreSession has succeeded — a restore that then fails leaves the current session's file
// untouched (subsequent Saves keep updating it, not the loaded one).
func (h *sessionHost) Load(id string) (session.Record, error) {
	return h.store.Load(id)
}

// Activate makes meta's session the one subsequent Saves update, replacing the current active
// session rather than forking a new file — the /sessions resume flow calls it once RestoreSession
// has confirmed the switch. Its id, Title, and CreatedAt carry over so a later Save preserves them.
func (h *sessionHost) Activate(meta session.Meta) {
	h.mu.Lock()
	h.active = &activeSession{id: meta.ID, title: meta.Title, createdAt: meta.CreatedAt}
	h.mu.Unlock()
}

// Delete removes a stored session's file.
func (h *sessionHost) Delete(id string) error { return h.store.Delete(id) }

// Rename sets a stored session's title. When the renamed session is the active one, the new title
// is mirrored onto the active identity too, so the next Save preserves it rather than reverting to
// the create-time title.
func (h *sessionHost) Rename(id, title string) error {
	if err := h.store.Rename(id, title); err != nil {
		return err
	}
	h.mu.Lock()
	if h.active != nil && h.active.id == id {
		h.active.title = title
	}
	h.mu.Unlock()
	return nil
}

// ActiveID reports the active session's id, or "" before the first Save has minted one (and after
// a Rotate). The composition root reads it to decide whether to print the resume hint.
func (h *sessionHost) ActiveID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active == nil {
		return ""
	}
	return h.active.id
}

// ----------------------------------------------------------------------------
// The prompt-recall host (the composition root's half of the recall seam)
// ----------------------------------------------------------------------------

// recallHost adapts a recall.Store to the TUI's [tui.RecallHost] seam by BINDING the workspace this
// run resolved. That binding is the whole of the adapter's reason to exist: the store is
// workspace-keyed (one JSONL file per project) while the renderer knows only "this box", and
// resolving a workspace path is the composition root's job (ADR 0001), never the renderer's.
//
// Both methods forward whatever the store reports; deciding that a recall failure is survivable is
// the TUI's call, made once at its own seam, so nothing is swallowed on this side.
type recallHost struct {
	store     *recall.Store
	workspace string
}

// recallHost satisfies the prompt-recall seam the TUI drives.
var _ tui.RecallHost = (*recallHost)(nil)

// newRecallHost builds the host over the recall directory dir, bound to the absolute workspace
// path. It touches no disk: recall.New creates the directory on the first recorded prompt.
func newRecallHost(dir, workspace string) *recallHost {
	return &recallHost{store: recall.New(dir), workspace: workspace}
}

// AppendPrompt records text as this workspace's newest sent input.
func (h *recallHost) AppendPrompt(text string) error { return h.store.Append(h.workspace, text) }

// LoadPrompts returns this workspace's recorded inputs, oldest→newest.
func (h *recallHost) LoadPrompts() ([]string, error) { return h.store.Load(h.workspace) }

// resolveResume loads the session a start restores from, or returns nil when neither --resume nor
// --continue is set. --resume tries its value as a store id first (the handle /sessions lists) and
// falls back to a file path (which still reads a pre-plan bare envelope); --continue resumes this
// workspace's most recent session. The two flags are mutually exclusive.
func resolveResume(store *session.Store, resume string, continueSession bool, workspace string) (*session.Record, error) {
	switch {
	case resume != "" && continueSession:
		return nil, errors.New("apogee: --resume and --continue are mutually exclusive; pass one or the other")
	case resume != "":
		rec, err := resolveResumeArg(store, resume)
		if err != nil {
			return nil, err
		}
		return &rec, nil
	case continueSession:
		rec, err := resolveContinue(store, workspace)
		if err != nil {
			return nil, err
		}
		return &rec, nil
	default:
		return nil, nil
	}
}

// resolveResumeArg resolves a --resume value: a store id first (the common case — the id shown in
// /sessions), else a file path (LoadPath, which also wraps a legacy bare envelope). A value that is
// neither a known id nor a readable file is a friendly error naming both interpretations.
//
// A record loaded by PATH keeps its conversation but not its identity: its id is content the file
// declares rather than a name this store minted, so adopting it would point every later autosave at
// whatever record that id names — another session's file, silently overwritten, and (before the
// store's id validation) any path the id spelled out. Re-minting makes the path-resumed
// conversation a NEW session of this store, which is also what makes resuming a file from outside
// the store — a repo-shipped session, a copied record — safe.
func resolveResumeArg(store *session.Store, arg string) (session.Record, error) {
	if rec, err := store.Load(arg); err == nil {
		return rec, nil
	}
	rec, err := store.LoadPath(arg)
	if err != nil {
		return session.Record{}, fmt.Errorf(
			"apogee: --resume %q: not a known session id (see /sessions) nor a readable session file", arg)
	}
	rec.Meta.ID = session.NewID(time.Now())
	return rec, nil
}

// resolveContinue resumes the most recent session recorded for the resolved workspace — the
// --continue convenience that needs no id. List returns metas newest-first, so the first record
// whose Workspace matches is the newest; a workspace with none is a friendly error pointing at the
// alternatives.
func resolveContinue(store *session.Store, workspace string) (session.Record, error) {
	metas, err := store.List()
	if err != nil {
		return session.Record{}, err
	}
	for _, m := range metas {
		if m.Workspace == workspace {
			return store.Load(m.ID)
		}
	}
	return session.Record{}, fmt.Errorf(
		"apogee: no saved sessions for this workspace (%s) — start one, or resume another with "+
			"--resume <id> (see /sessions)", workspace)
}

// resumedSession projects a resolved store record onto the TUI's startup-replay payload, or nil for
// a fresh start. The renderer decodes the opaque transcript blob itself; the binary only carries it
// across with the title, context fill, and message count the resume note and gauge need, plus
// inExchange — the resumed Agent's open-Exchange state (agent.InExchange()) — so newModel can append
// the interrupted note when the session died mid-task.
func resumedSession(rec *session.Record, inExchange bool) *tui.ResumedSession {
	if rec == nil {
		return nil
	}
	return &tui.ResumedSession{
		Transcript: rec.Transcript,
		Title:      rec.Meta.Title,
		CtxUsed:    rec.Meta.CtxUsed,
		UserMsgs:   rec.Meta.UserMsgs,
		InExchange: inExchange,
	}
}

// parseMode validates the --mode flag against the known autonomy modes (the ladder
// Plan → Ask-Before → Allow-Edits → Auto). Auto parses successfully here; whether it can
// run depends on the host's fs-confinement (ADR 0012 — Auto needs landlock ABI ≥1 on
// Linux, or is refused only when no fs-confinement exists). friendlyConstructErr surfaces
// an unavailable-Auto as an actionable message.
func parseMode(s string) (apogee.Mode, error) {
	switch apogee.Mode(s) {
	case modePlan, modeAskBefore, modeAllowEdits, modeAuto:
		return apogee.Mode(s), nil
	default:
		return "", fmt.Errorf("apogee: invalid --mode %q (want plan, ask-before, allow-edits, or auto)", s)
	}
}

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
	workspace string
}

// apogeeHome resolves the absolute apogee home directory: the configDir override when
// set, else ~/.apogee (the single uniform dotdir on every OS — owner decision, not XDG).
// It is shared by resolveRoots (the state roots) and configFilePath (where config.yaml
// lives), so both agree on the home.
func apogeeHome(configDir string) (string, error) {
	home := configDir
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("apogee: resolve home directory: %w", err)
		}
		home = filepath.Join(userHome, ".apogee")
	}
	return filepath.Abs(home)
}

// resolveRoots computes the state roots the library refuses to assume (ADR 0001): the
// apogee home (configDir override, else ~/.apogee) holds config/library/sessions, and the
// workspace (workspace override, else the current directory) scopes the file tools. It
// computes paths only — directory creation is deferred to the writer that needs them (P2.5).
func resolveRoots(configDir, workspace string) (stateRoots, error) {
	absHome, err := apogeeHome(configDir)
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
		prompts:   filepath.Join(absHome, "prompts"),
		workspace: absWorkspace,
	}, nil
}

// ----------------------------------------------------------------------------
// Agent construction (dogfooding the public surface — C5)
// ----------------------------------------------------------------------------

// buildAgent constructs a fresh Agent, or resumes one from an already-loaded session record when
// resumed is non-nil (a --resume/--continue start; resolveResume owns the id-or-path lookup and the
// legacy-file wrapping). Both go through the public apogee surface. A future-version snapshot
// surfaces apogee.ErrSessionVersion from Resume, which carries its own clear message.
func buildAgent(cfg apogee.Config, resumed *session.Record) (*apogee.Agent, error) {
	if resumed == nil {
		return apogee.New(cfg)
	}
	return apogee.Resume(cfg, resumed.Session)
}

// startupEntry re-assembles the server selection resolved (ADR 0036) from the flattened fields it
// left on options: the endpoint, the key, the discovery hint, and the alias — which for a
// configured entry IS its `servers:` name and for the ephemeral override entry is the endpoint's
// host. It exists so the bind step below has ONE input shape, the serverEntry, whether it is
// binding the startup server or the one a human picked out of the list.
func startupEntry(opts options) serverEntry {
	return serverEntry{
		Name:     opts.hostAlias,
		Endpoint: opts.endpoint,
		APIKey:   opts.apiKey,
		Model:    opts.model,
	}
}

// serverBinder is the one step that turns a serverEntry into a running session: the Agent
// constructed against that server, the Monitor that observes it, and the binding the out-of-band
// calls (naming, Firings) read. It is a step rather than inline wiring because it now runs at two
// different times — before the TUI starts when a startup server was determined, and on the human's
// first pick when it was not (ADR 0036 decision 3) — and both must produce the same session.
//
// What it does NOT do is re-resolve the per-model bindings for the entry's `model:` hint. Those
// belong to the rebind path (rebindSpecFor), which the first beat of the Monitor installed here
// runs within seconds, for a late bind exactly as it does for the cold start a launch-time bind
// with no model already is.
type serverBinder struct {
	// cfg is everything about the session the server does not decide. The three fields it does —
	// endpoint, key, model hint — are overwritten from the entry, so nothing that reached this
	// struct can contradict the server being bound.
	cfg     apogee.Config
	resumed *session.Record
	engine  *lateEngine
	holder  *upstreamHolder
}

// bind constructs the engine for entry and points both holders at it. The engine is constructed
// FIRST and the Monitor installed only once that succeeded, so a refused construction (Auto on a
// host without confinement, a future-version snapshot) leaves the session exactly as unbound as it
// was rather than half-wired to a server it cannot talk to.
//
// A second bind is refused before anything is constructed (lateEngine.Bind), because the holder can
// only release one Agent at shutdown: a session that already has an engine moves with
// sessionMover.move, which switches the one it has.
func (b serverBinder) bind(entry serverEntry) error {
	cfg := b.cfg
	cfg.Endpoint = entry.Endpoint
	cfg.Model = entry.Model
	cfg.APIKey = entry.APIKey

	if err := b.engine.Bind(func() (*apogee.Agent, error) {
		agent, err := buildAgent(cfg, b.resumed)
		if err != nil {
			return nil, friendlyConstructErr(err)
		}
		return agent, nil
	}); err != nil {
		return err
	}
	b.holder.Bind(entry.Endpoint, entry.APIKey, entry.Model,
		heartbeat.NewMonitor(entry.Endpoint, entry.Model, entry.APIKey))
	return nil
}

// ----------------------------------------------------------------------------
// The late-bound engine (ADR 0036 decision 3: construction waits for a server)
// ----------------------------------------------------------------------------

// lateEngine is the [tui.Engine] the composition root hands the renderer, and the holder of the
// Agent behind it. It exists because the engine cannot always be built at launch: ADR 0024 keeps
// construction impossible without an endpoint, and ADR 0036 lets a session start before anyone has
// said which endpoint that is — so the seam must exist before the thing behind it does.
//
// Unbound, every call that needs a conversation is refused with errNoServerBound and every read
// answers what "nothing is bound" honestly is: no open Exchange, no context files, no snapshot.
// Nothing panics and nothing silently pretends to have worked, because the renderer reaches this
// type through the same interface either way.
//
// The two settings a human can move before a server exists — the autonomy mode and Auto's blast
// radius — are held here and applied to the Agent the moment it is constructed. Without that, a
// mode cycled while the picker was open would show in the footer and be nowhere in the engine.
//
// The mutex guards the pointer and those two values. Beat-style long calls (Step, Compact) read the
// pointer under the lock and then run OUTSIDE it, like upstreamHolder does with its Monitor, so a
// Step that takes a minute never holds the Update loop's next call behind it.
type lateEngine struct {
	mu      sync.Mutex
	agent   *apogee.Agent
	mode    apogee.Mode
	confine bool

	// The settings surface's anytime-safe mutators, held for the same reason the two above are: an
	// edit committed while the server picker is still up must not fall between the file and the
	// engine. A nil pointer means the key was never moved HERE, so a bind leaves the Agent on the
	// seed its Config carried — which is the difference between "not moved" and "moved to false".
	pendingBypass       *bool
	pendingCompaction   *bool
	pendingContextFiles *contextFileChoice
	// pendingProfile is the same idea for the one IDLE-ONLY mutator that has to be remembered: a
	// dialect swap needs an Agent to build its parsers, but a bind with no memory of the edit would
	// install the profile the process started with (see SetProfile).
	pendingProfile *apogee.ModelProfile
}

// contextFileChoice is one remembered SetContextFiles call. The pair travels together because
// either half alone is a different instruction: names with the switch off install nothing.
type contextFileChoice struct {
	enable bool
	names  []string
}

// The composition root's engine holder satisfies every seam it is handed across: the renderer's
// narrow Engine, the switcher half of the shared move (sessionMover), and the live-apply
// dispatcher's mutator surface.
var (
	_ tui.Engine       = (*lateEngine)(nil)
	_ upstreamSwitcher = (*lateEngine)(nil)
	_ settingsEngine   = (*lateEngine)(nil)
)

// errNoServerBound is what every conversation-touching call answers while the session has no
// upstream. It names the way out, because the one surface it can reach is a note the human reads.
var errNoServerBound = errors.New("apogee: no server is bound yet — choose one with /server")

// errAlreadyBound refuses a SECOND construction on a holder that already has an Agent. It names
// `/server` because that verb does what the caller meant: move the session it already has.
var errAlreadyBound = errors.New("apogee: this session already has a server — use /server to switch")

// newLateEngine builds the unbound holder, seeded with the autonomy mode and blast radius this run
// resolved — the same two values the Config carries, so a bind that happens immediately restates
// them and a bind that happens after the human moved one applies what they moved.
func newLateEngine(mode apogee.Mode, confineToWorkspace bool) *lateEngine {
	return &lateEngine{mode: mode, confine: confineToWorkspace}
}

// Bind constructs the Agent through construct and installs it as this session's engine. The
// construction happens UNDER the lock, which is what makes "exactly one Agent" a property of the
// type rather than of its callers: a second Bind is refused before construct is ever called, so no
// engine is ever built that nothing holds. It costs nothing to hold the lock across it — apogee.New
// reaches no network (startup makes no probe, ADR 0024) — and a failed construction leaves the
// holder unbound, free to try another server.
func (e *lateEngine) Bind(construct func() (*apogee.Agent, error)) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.agent != nil {
		return errAlreadyBound
	}
	agent, err := construct()
	if err != nil {
		return err
	}
	// What the human may have moved while there was nothing to move it on.
	agent.SetMode(e.mode)
	agent.SetConfineToWorkspace(e.confine)
	if e.pendingBypass != nil {
		agent.SetBypass(*e.pendingBypass)
	}
	if e.pendingCompaction != nil {
		agent.SetCompactionEnabled(*e.pendingCompaction)
	}
	if c := e.pendingContextFiles; c != nil {
		agent.SetContextFiles(c.enable, c.names)
	}
	// The one remembered value that can be REFUSED: a dialect this build cannot parse. The Agent is
	// released and the bind fails, which is exactly what a config carrying that profile at launch
	// does — a session is never installed reading its model's replies in a language it does not have.
	if p := e.pendingProfile; p != nil {
		if err := agent.SetProfile(*p); err != nil {
			_ = agent.Close()
			return err
		}
	}
	e.agent = agent
	return nil
}

// bound reports whether an Agent is installed — the pointer read every method below opens with.
func (e *lateEngine) bound() *apogee.Agent {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.agent
}

// Submit enqueues user input, or refuses when there is no engine to enqueue it in.
func (e *lateEngine) Submit(in apogee.UserInput) error {
	agent := e.bound()
	if agent == nil {
		return errNoServerBound
	}
	return agent.Submit(in)
}

// Step advances the loop one Turn. It runs outside the lock: a Turn is a network call.
func (e *lateEngine) Step(ctx context.Context) (apogee.StepResult, error) {
	agent := e.bound()
	if agent == nil {
		return apogee.StepResult{}, errNoServerBound
	}
	return agent.Step(ctx)
}

// Snapshot captures the conversation, or refuses: an unbound session has no conversation to save.
func (e *lateEngine) Snapshot() (apogee.Session, error) {
	agent := e.bound()
	if agent == nil {
		return apogee.Session{}, errNoServerBound
	}
	return agent.Snapshot()
}

// Interject commits a message into the open Exchange; unbound there is none.
func (e *lateEngine) Interject(in apogee.UserInput) error {
	agent := e.bound()
	if agent == nil {
		return errNoServerBound
	}
	return agent.Interject(in)
}

// ClearContext drops the model's history; unbound there is no history to drop.
func (e *lateEngine) ClearContext() error {
	agent := e.bound()
	if agent == nil {
		return errNoServerBound
	}
	return agent.ClearContext()
}

// AbortExchange discards a cancelled Exchange. Unbound it is a no-op rather than a refusal: it
// answers nothing, it is called on a path that is already unwinding, and there is nothing to abort.
func (e *lateEngine) AbortExchange() {
	if agent := e.bound(); agent != nil {
		agent.AbortExchange()
	}
}

// RestoreSession swaps a stored snapshot into the live Agent — which a pre-bound session does not
// have, so the /sessions restore is refused until a server is chosen.
func (e *lateEngine) RestoreSession(sess apogee.Session) error {
	agent := e.bound()
	if agent == nil {
		return errNoServerBound
	}
	return agent.RestoreSession(sess)
}

// InExchange reports whether an Exchange is open — false with nothing bound, which is exactly true.
func (e *lateEngine) InExchange() bool {
	agent := e.bound()
	return agent != nil && agent.InExchange()
}

// ContextFilesReport reports what the workspace context files contributed. Unbound the report is
// empty, which the renderer already reads as "nothing to note" (it notes a report with no files
// exactly as it notes none at all).
func (e *lateEngine) ContextFilesReport() apogee.ContextFilesReport {
	agent := e.bound()
	if agent == nil {
		return apogee.ContextFilesReport{}
	}
	return agent.ContextFilesReport()
}

// Compact folds the conversation through the upstream; unbound there is neither.
func (e *lateEngine) Compact(ctx context.Context) (bool, error) {
	agent := e.bound()
	if agent == nil {
		return false, errNoServerBound
	}
	return agent.Compact(ctx)
}

// SetMode changes the autonomy mode. Unbound it is REMEMBERED rather than dropped, so the mode the
// footer shows is the mode the engine is constructed into.
func (e *lateEngine) SetMode(mode apogee.Mode) {
	e.mu.Lock()
	e.mode = mode
	agent := e.agent
	e.mu.Unlock()
	if agent != nil {
		agent.SetMode(mode)
	}
}

// SetConfineToWorkspace changes Auto's blast radius, remembered while unbound for SetMode's reason.
func (e *lateEngine) SetConfineToWorkspace(confine bool) {
	e.mu.Lock()
	e.confine = confine
	agent := e.agent
	e.mu.Unlock()
	if agent != nil {
		agent.SetConfineToWorkspace(confine)
	}
}

// SetBypass switches Mechanisms off or back on for the rest of the session (the settings surface's
// `bypass` key), remembered while unbound for SetMode's reason: the pane can be opened before a
// server is chosen, and an edit that persisted must not be the only half that happened.
func (e *lateEngine) SetBypass(enabled bool) {
	e.mu.Lock()
	e.pendingBypass = &enabled
	agent := e.agent
	e.mu.Unlock()
	if agent != nil {
		agent.SetBypass(enabled)
	}
}

// SetCompactionEnabled arms or disarms the automatic Compaction trigger (`auto-compact`), on the
// same terms as SetBypass above.
func (e *lateEngine) SetCompactionEnabled(enabled bool) {
	e.mu.Lock()
	e.pendingCompaction = &enabled
	agent := e.agent
	e.mu.Unlock()
	if agent != nil {
		agent.SetCompactionEnabled(enabled)
	}
}

// SetContextFiles replaces the workspace context-file names folded in at the next session boundary
// (`context-files:`), on the same terms as the two above. The pair is remembered together because
// the switch and the names are one instruction.
func (e *lateEngine) SetContextFiles(enable bool, names []string) {
	e.mu.Lock()
	e.pendingContextFiles = &contextFileChoice{enable: enable, names: names}
	agent := e.agent
	e.mu.Unlock()
	if agent != nil {
		agent.SetContextFiles(enable, names)
	}
}

// SwapTools replaces the session's tool set outright (ADR 0037 binding F — the one door for a
// tool-set change). Unlike the four setters above it is NOT remembered while unbound, and does not
// need to be: the tools an unbound session will start with are Config.Tools, the registry the
// composition root holds and rebuilds directly, so nothing is lost by refusing here — there is
// simply no Agent yet whose set could differ from it.
func (e *lateEngine) SwapTools(registry *apogee.ToolRegistry) error {
	agent := e.bound()
	if agent == nil {
		return errNoServerBound
	}
	return agent.SwapTools(registry)
}

// SetProfile swaps the dialect the session reads responses in (`model-profile`, ADR 0037's other
// idle-only door). Unbound it is REMEMBERED and installed at the bind, like the anytime-safe setters
// above and unlike SwapTools: a tool registry has a carrier across the bind already — Config.Tools,
// which the composition root holds and hands over — while the profile a bind would otherwise install
// is the one the process STARTED with, so a pre-bound edit that was only remembered nowhere would be
// an edit that waits for the next launch, which ADR 0037 exists to abolish.
//
// A remembered profile is validated where a bound one is: at the Agent. It is built before anything
// is submitted, so the only way it can fail there is a dialect this build cannot parse — the same
// failure the file would have produced at construction had it carried that profile at launch, and it
// is reported the same way, as a bind that did not happen.
func (e *lateEngine) SetProfile(profile apogee.ModelProfile) error {
	e.mu.Lock()
	agent := e.agent
	if agent == nil {
		e.pendingProfile = &profile
		e.mu.Unlock()
		return nil
	}
	e.mu.Unlock()
	return agent.SetProfile(profile)
}

// ConfineToWorkspace reports the blast radius the next tool call will read: the Agent's own once
// there is one, and until then the value a bind would install.
func (e *lateEngine) ConfineToWorkspace() bool {
	e.mu.Lock()
	agent, confine := e.agent, e.confine
	e.mu.Unlock()
	if agent != nil {
		return agent.ConfineToWorkspace()
	}
	return confine
}

// Close releases the Agent, or nothing at all when a session ends without ever binding one.
func (e *lateEngine) Close() error {
	agent := e.bound()
	if agent == nil {
		return nil
	}
	return agent.Close()
}

// Rebind swaps in the per-model bindings a heartbeat observation implies (the composition root's
// rebind closure). A beat cannot land before a Monitor exists, so unbound this is a backstop.
func (e *lateEngine) Rebind(spec apogee.RebindSpec) error {
	agent := e.bound()
	if agent == nil {
		return errNoServerBound
	}
	return agent.Rebind(spec)
}

// SwitchUpstream re-points the session at another server (sessionMover.move). Unbound there is
// nothing to re-point: the first server arrives through Bind above, not through a move.
func (e *lateEngine) SwitchUpstream(spec apogee.UpstreamSpec) error {
	agent := e.bound()
	if agent == nil {
		return errNoServerBound
	}
	return agent.SwitchUpstream(spec)
}

// errAutoUnavailable is the friendly framing of ErrAutoUnavailable: Auto needs
// filesystem-write confinement, which this host cannot provide (no landlock on Linux, no
// sandbox-exec on macOS — ADR 0012). The lower rungs of the ladder still work.
var errAutoUnavailable = errors.New(
	"apogee: auto mode requires filesystem-write confinement, which is unavailable on this host " +
		"(Linux needs landlock — kernel ≥5.13; macOS needs sandbox-exec) — " +
		"use --mode plan, --mode ask-before, or --mode allow-edits")

// friendlyConstructErr maps construction errors to actionable CLI messages. The headline
// case is Auto mode: New returns ErrAutoUnavailable when Mode==Auto but the host's Confiner
// cannot enforce filesystem confinement.
func friendlyConstructErr(err error) error {
	if errors.Is(err, apogee.ErrAutoUnavailable) {
		return errAutoUnavailable
	}
	return err
}
