package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/present"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/provider"
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

// discoverUpstreamModel probes the OpenAI-compatible server at endpoint for its active
// model id — the first advertised model when none is configured (provider.Discover). It is
// the production modelDiscoverer the root wires into resolveModel so a single-model server
// runs with no --model set; tests inject a fake. ctx bounds the probe (Discover also
// self-imposes a short timeout), and a model-less client is fine — Discover treats the
// configured model only as a hint.
func discoverUpstreamModel(ctx context.Context, endpoint string) (discoveredUpstream, error) {
	info, err := provider.NewClient(endpoint, "").Discover(ctx)
	if err != nil {
		return discoveredUpstream{}, err
	}
	return discoveredUpstream{model: info.ActiveModel, contextWindow: info.ContextWindow}, nil
}

// contextWindowNotice returns the one-line startup notice to print when the context window is
// unknown (0) while automatic Compaction is enabled — the Budget and the automatic fold both
// bind against the window, so with none known they are inactive (item 3 / S3). It returns "" (no
// notice) when the window is known or Compaction is off. Pure so the startup message is
// table-testable without capturing os.Stderr.
func contextWindowNotice(maxContextTokens int, compactionEnabled bool) string {
	if maxContextTokens != 0 || !compactionEnabled {
		return ""
	}
	return "apogee: context window unknown — automatic compaction and the Budget are inactive; " +
		"set context-window: in config.yaml or let discovery run"
}

// The confinement backend label and the Auto-degradation notice used below live in
// internal/probe: `apogee probe` reports the same verdict off-session and the TUI's
// /confine status renders it in-session, so the wording is extracted rather than copied —
// three surfaces, one sentence (Phase-5 item 3 / ADR 0021).

// shouldPrewarmLabelWalk reports whether startup should eagerly run the confinement backend's
// one-time label walk (ADR 0020 §2) rather than let the first confined command pay it mid-session.
// It is the MIRROR of probe.DegradedNotice's gate — the same three inputs, FSWrite inverted: the
// degradation notice fires when Auto asks for confinement a host CANNOT enforce (FSWrite false),
// this fires when it CAN (FSWrite true), which on the Windows token backend is the disk-label pass
// worth pre-warming behind a progress notice. It returns true on the Linux and macOS backends too
// (they also report FSWrite under Auto+confine), where PrewarmLabelWalk is a genuine no-op — only
// the Windows-tagged labelBox pays a walk — so the Windows-vs-not distinction lives in that seam,
// not here. Pure so the decision is table-testable off Windows (the contextWindowNotice /
// DegradedNotice seam pattern).
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
	// these same dirs on demand: the /skill picker refreshes it each time it opens, so a skill
	// added or edited after launch is picked up without restarting. The initial load error is
	// soft (a missing dir is skipped, a malformed skill is skipped), so the catalog is always
	// usable. The SAME *skills.Provider feeds both the loop (Config.Skills resolves attached IDs
	// into the turn) and the TUI's /skill picker (Options.Skills lists/labels them), so a
	// refreshed skill both shows in the picker AND resolves when attached.
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
	rungs := presentationRungs(opts.present, runtime.GOOS, os.Getenv)
	bridge.SetPresentation(rungs)
	if rungs.Docs != nil {
		// The doc server's listener is owned by the app: it binds lazily on the first served
		// presentation and closes with the session, like the MCP connections and the Agent below.
		defer rungs.Docs.Close()
	}

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

	cfg := apogee.Config{
		Endpoint:     opts.endpoint,
		Model:        opts.model,
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
		Skills:  skillProvider,
		// The discovered runtime context window (0 when the server did not report one). It is the
		// budget /compact and the automatic Compaction trigger bound their summary request against
		// so compaction survives high fill (the summary call would otherwise overflow near n_ctx);
		// the same value drives the TUI's footer/gauge below. CompactionEnabled carries the
		// `auto-compact` key (default on) — the budget-driven automatic trigger (item 9); the
		// on-demand /compact runs regardless of it.
		Context: apogee.ContextConfig{MaxContextTokens: opts.contextWindow, CompactionEnabled: opts.autoCompact},
	}

	// A per-session startup warning whenever Auto runs unconfined (ADR 0012): confine=false
	// is safe only inside a VM, and it is the only blanket loosen in the system.
	if mode == modeAuto && !opts.confineToWorkspace {
		fmt.Fprintln(os.Stderr, "apogee: WARNING — auto mode is running UNCONFINED "+
			"(confine-to-workspace: false). This is safe only inside a VM/container; "+
			"the dangerous-action guard is a footgun-net, not a security boundary.")
	}

	// The mirror branch: Auto WITH confinement asked for, on a host whose backend cannot
	// enforce it. The ladder gates every terminal command instead — correct, but silent until
	// now, which is what made Auto look broken (ISSUES.md, 2026-07-21). Say it once, name the
	// backend, and point at /confine.
	if notice := probe.DegradedNotice(probe.BackendName(confiner), confiner.Capabilities(), mode, opts.confineToWorkspace); notice != "" {
		fmt.Fprintln(os.Stderr, notice)
	}

	// When the context window is still unknown (discovery found none and no context-window: key
	// was set) but automatic Compaction is on, the Budget and the fold have nothing to bind
	// against — both silently do nothing. Say so once, and name the fix (item 3 / S3).
	if notice := contextWindowNotice(cfg.Context.MaxContextTokens, cfg.Context.CompactionEnabled); notice != "" {
		fmt.Fprintln(os.Stderr, notice)
	}

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
	defer mcpClient.Close()
	if mcpTools := mcpClient.Tools(); len(mcpTools) > 0 {
		cfg.Tools = registryWithMCP(roots.workspace, cfg, mcpTools)
	}

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
	cfg.EnableMechanisms, err = mechanismIDs(opts.mechanisms, mechanisms.KnownIDs())
	if err != nil {
		return err
	}

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

	agent, err := buildAgent(cfg, opts.resume)
	if err != nil {
		return friendlyConstructErr(err)
	}
	defer agent.Close()

	// The saver persists a snapshot to SessionsDir when the UI quits cleanly. It owns the
	// path and on-disk format (internal/session); the TUI sees only the func(Session) error
	// seam, keeping file I/O out of the renderer (phase-2 detail plan §3 C5). It records the
	// last path written so a resume hint can be shown once the alternate screen is torn down.
	saver := &sessionSaver{store: session.NewStore(roots.sessions)}

	err = launch(ctx, agent, bridge, tui.Options{
		Model:         opts.model,
		Endpoint:      opts.endpoint,
		Mode:          mode,
		Bypass:        opts.bypass,
		Workspace:     roots.workspace,
		ContextWindow: opts.contextWindow,
		HostAlias:     opts.hostAlias,
		// The single source of truth (the embedded top-level VERSION file, via apogee.Version),
		// so the footer, the /version command, and the start-up box all display one value.
		Version: apogee.Version(),
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
		Skills: skillProvider,
		// Re-scan the skill source dirs when the /skill picker opens, swapping in a fresh catalog
		// on the shared Provider — the same one Config.Skills resolves against — so a skill added
		// mid-session both shows and attaches. The error is soft (Provider.Reload never signals
		// unusable), so it is dropped.
		ReloadSkills: func() { _ = skillProvider.Reload() },
		Save:         saver.save,
	})
	if path := saver.saved(); path != "" {
		fmt.Fprintf(os.Stdout, "Session saved · resume with: apogee --resume %s\n", path)
	}
	return err
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
//     where the user reaches this box from cannot change mid-session.
//
// Rung 0 — the transcript line carrying the path — is deliberately absent: it needs no mechanism,
// it is never skipped, and nothing in the config can turn it off.
func presentationRungs(p presentSettings, goos string, env func(string) string) tui.Presentation {
	rungs := tui.Presentation{Local: present.Locality(env) == present.Local}
	if rungs.Local && p.autoOpen {
		rungs.Opener = &present.Opener{GOOS: goos, Env: env, CommandOverride: p.command}
	}
	if !rungs.Local {
		rungs.Docs = &present.DocServer{Host: present.AdvertiseHost(env, p.host), Port: p.port}
	}
	return rungs
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

// sessionSaver adapts a session.Store to the TUI's func(Session) error saver seam and
// records the last path written. save runs on the program goroutine (a clean quit);
// saved is read after launch returns — the mutex makes that hand-off race-free regardless
// of how the program loop synchronises its shutdown.
type sessionSaver struct {
	store *session.Store

	mu   sync.Mutex
	path string
}

// save persists the snapshot and records its path on success.
func (s *sessionSaver) save(sess apogee.Session) error {
	path, err := s.store.Save(sess)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.path = path
	s.mu.Unlock()
	return nil
}

// saved reports the last path written, or "" if nothing was saved.
func (s *sessionSaver) saved() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.path
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
		probe:     library.ProbeDir(absHome),
		workspace: absWorkspace,
	}, nil
}

// ----------------------------------------------------------------------------
// Agent construction (dogfooding the public surface — C5)
// ----------------------------------------------------------------------------

// buildAgent constructs a fresh Agent, or resumes one from a saved session file when
// --resume is set. Both go through the public apogee surface. The richer session UX
// (snapshot-on-quit, a config file, flag/env/file precedence) lands in P2.5.
func buildAgent(cfg apogee.Config, resumePath string) (*apogee.Agent, error) {
	if resumePath == "" {
		return apogee.New(cfg)
	}
	data, err := os.ReadFile(resumePath)
	if err != nil {
		return nil, fmt.Errorf("apogee: read session %q: %w", resumePath, err)
	}
	session, err := apogee.DecodeSession(data)
	if err != nil {
		return nil, err // ErrSessionVersion already carries a clear message
	}
	return apogee.Resume(cfg, session)
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
