package main

// The composition proper: runRoot, the four phases it walks, the wiring those phases fill in, the
// state-root resolution behind them and the narrow surfaces its seam files share. Everything else
// the wiring owns lives in a wire_<seam>.go beside it (ADR 0043: a composition root splits by seam
// once it outgrows one file).
//
// The phases, in the order runRoot walks them:
//
//   - wire_boot.go — the host facilities one run owns before a session exists (skills, Bridge,
//     presentation ladder, Confiner), the base [apogee.Config] built from them, and what this host's
//     confinement posture says for itself on stderr.
//   - wire_live.go — the live-session assembly: the MCP connections, the tool registry, the
//     Mechanism list, the session store, the engine and Upstream holders and the bind that fills
//     them, the live-settings holder, the config watcher, and the out-of-band work.
//   - wire_verbs.go — the composition root's own verbs: the rebind, the beat wrapper, and the three
//     ways a session arrives on or records an Upstream.
//   - wire_options.go — the projection of all of it onto [tui.Options], the renderer's whole view.
//
// The seams those phases build, each split off by concern:
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
// wire_test.go covers this file and all eleven.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/tui"
)

// Compile-time proof that the public Agent satisfies the TUI's narrow engine seam
// (phase-2 detail plan §3 C5): cmd dogfoods apogee.New, and *apogee.Agent (= *agent.Agent)
// is exactly what internal/tui drives — without internal/tui ever importing the root path.
var _ tui.Engine = (*apogee.Agent)(nil)

// ----------------------------------------------------------------------------
// Root command body
// ----------------------------------------------------------------------------

// runRoot is the root command's body: parse the mode, resolve the state roots, build a
// Config, construct (or resume) the Agent through the public surface, and launch the UI.
// Each of those is a named phase over one wiring, and this is the whole sequence.
func runRoot(ctx context.Context, opts config.Options, launch launcher) error {
	mode, err := domain.ParseMode(opts.Mode)
	if err != nil {
		return err
	}

	roots, err := resolveRoots(opts.ConfigDir, opts.Workspace)
	if err != nil {
		return err
	}

	// The facilities this run owns for its whole life, and — in one defer, before the first
	// fallible step — the teardown for everything the phases below reach (wire_boot.go).
	w := newRootWiring(opts, mode, roots)
	defer w.close()

	// The base Config the session is constructed from, and the sentences this host's confinement
	// posture owes the user while stderr is still a safe place to write (wire_boot.go).
	if err := w.resolveConfig(); err != nil {
		return err
	}
	w.announceConfinement()

	// What this run does about a plaintext `api-key:` in the config file (keymigrate.go, ADR 0047):
	// raise the migration offer where the machine has a store that can complete the move, or say on
	// stderr what can be done by hand where it has not. It goes here, beside the confinement
	// sentences, because it is the same kind of thing — a start-up fact about the human's own
	// machine, told while stderr is still a safe place to write.
	w.prepareKeyMigration(probeKeyStore, os.Stderr)

	// The running session: connections, registry, holders, hosts, and the one bind that fills
	// them (wire_live.go).
	if err := w.wireSession(ctx); err != nil {
		return err
	}

	// And the launch: the engine seam, the Bridge the program late-binds to, and the projection of
	// everything above onto the renderer's Options (wire_options.go).
	err = launch(ctx, w.engine, w.bridge, w.options())
	// Once the alternate screen is torn down, point the user at how to pick this session back up.
	// ActiveID is non-empty exactly when there is a resumable session — a resumed one, or a fresh
	// one that reached at least one Turn (an empty conversation is never written).
	if w.host.ActiveID() != "" {
		fmt.Fprintln(os.Stdout, "Session saved · resume with: apogee --continue   (or /sessions inside apogee)")
	}
	return err
}

// rootWiring is what runRoot assembles: one value carrying everything the phases wire, so a phase
// can be a named function rather than another two hundred lines of one. It is deliberately a
// STRUCT and not a set of closures over locals, because the teardown is the delicate part — the
// holders below used to be closed by six inline defers, and close() ends exactly the same things in
// exactly the same order, skipping whatever a failed startup never reached.
//
// Nothing outside this file's own phases touches it: the renderer is handed seams (wire_options.go),
// never the wiring.
type rootWiring struct {
	// The launch snapshot and what runRoot resolved from it before anything was built.
	opts  config.Options
	mode  apogee.Mode
	roots stateRoots

	// The boot phase (wire_boot.go): the facilities one run owns, and the Config built from them.
	// keys is this run's ONE key resolver: every seam that needs a server entry's API key asks it,
	// so an entry whose key comes from a command or a variable pays for that source once per
	// session however many seams read it (config.KeyResolver).
	keys          *config.KeyResolver
	skillProvider *skills.Provider
	// The start-up key migration (keymigrate.go): the machine's secret store, when it has one this
	// run can complete a move into, and the offer the renderer raises over it. Both stay zero on a
	// config that names no plaintext key — the store is not even probed then — and the nil store is
	// what leaves the two migration seams unwired.
	secrets      secretStore
	keyOffer     tui.KeyMigrationOffer
	bridge       *tui.Bridge
	presentation *livePresentation
	confiner     domain.Confiner
	cfg          apogee.Config

	// The live session (wire_live.go). The tool registry and the Mechanism list are folded onto
	// cfg above rather than held here — the engine reads them off the Config it is built from.
	mcpSet        *liveMCP
	toolSet       *liveTools
	store         *session.Store
	resumed       *session.Record
	engine        *lateEngine
	host          *sessionHost
	holder        *upstreamHolder
	caps          *parallelAgentsCap
	hints         hintObserver
	delegation    *delegationWiring
	binder        serverBinder
	live          *liveSettings
	externalEdits *externalEdit
	configWatch   *config.Watcher
	mover         sessionMover
	launcherPath  *launcherPath
	launcherSeams launcherWiring
	titles        titleWiring
	gate          *idleGate
	schedules     *schedule.Scheduler

	// The resolved `ui.color-scheme:` palette and whatever the resolve complained about.
	colorScheme         scheme.Scheme
	colorSchemeWarnings []string
}

// close ends the run: every facility this wiring opened, in the reverse of the order it was opened,
// and only the ones that were actually reached. It is the single deferred call runRoot registers
// before its first fallible step, which is what makes a failure half way down the assembly tear
// down precisely what the six inline defers it replaces used to tear down at that same line.
func (w *rootWiring) close() {
	// A TUI-hosted Schedule dies with the TUI — the honest v1 promise (ADR 0033): Close takes every
	// Schedule off the clock, cancels the context its Firings run under and joins their goroutines,
	// so nothing this session started outlives the alternate screen. It goes first, BEFORE the
	// Agent's own Close: the firings let go of the process while everything they were composed from
	// still stands.
	if w.schedules != nil {
		w.schedules.Close()
	}

	// The config watcher ends next, for the schedules' reason: the poll stops while everything it
	// reported into is still standing. Stop waits for the poll goroutine and closes the channel
	// behind it, so the wait the renderer parks on returns rather than leaking, and nothing the
	// assembly let go of outlives runRoot.
	if w.configWatch != nil {
		w.configWatch.Stop()
	}

	if w.engine != nil {
		_ = w.engine.Close()
	}

	// Whatever set of MCP connections the holder is on NOW — which after a mid-session reconnect is
	// not the set startup dialled.
	if w.mcpSet != nil {
		_ = w.mcpSet.close()
	}

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
	if closer, ok := w.confiner.(interface{ Close() error }); ok {
		if notice := platform.ConfinementTeardownNotice(closer.Close()); notice != "" {
			fmt.Fprintln(os.Stderr, notice)
		}
	}

	// The doc server's listener is owned by the app: it binds lazily on the first served
	// presentation and closes with the session, like the MCP connections and the Agent above.
	// Closing through the holder closes whichever server is current, which after a `present.port`
	// edit is not the one this session started with.
	if w.presentation != nil {
		w.presentation.close()
	}
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
// the rebind verb rather than from here, because a rebind is a per-MODEL resolution the heartbeat
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
// switch seam ([tui.SchemeHost.Resolve]), so a scheme picked at start-up and the same scheme
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
