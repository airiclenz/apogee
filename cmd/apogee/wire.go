package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/platform"
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
	cfg := apogee.Config{
		Endpoint:     opts.endpoint,
		Model:        opts.model,
		Mode:         mode,
		Bypass:       opts.bypass,
		Events:       bridge.Sink(),
		Approver:     bridge.Approver(),
		Asker:        bridge.Asker(),
		ConfigDir:    roots.config,
		LibraryDir:   roots.library,
		SessionsDir:  roots.sessions,
		WorkspaceDir: roots.workspace,
		// Select the host's real Confiner backend for this OS (landlock on Linux, seatbelt
		// on macOS, denyConfiner elsewhere — confinement-execution-contract §2.6). It is no
		// longer denyConfiner, so --mode auto WORKS where fs-confinement exists and gates the
		// subprocess surface where it does not (rather than refusing Auto).
		Confiner:           platform.NewConfiner(),
		ConfineToWorkspace: opts.confineToWorkspace,
		WebSearchEndpoint:  opts.webSearchEndpoint,
		// The model profile (CONTEXT: Model profile) — tool-call format + thinking channel —
		// resolved from config.yaml (file-only). A zero profile is native tool calls with no
		// inline thinking, so an unconfigured model behaves exactly as today.
		Profile: opts.profile,
		Skills:  skillProvider,
		// The discovered runtime context window (0 when the server did not report one). It is the
		// budget /compact bounds its summary request against so compaction survives high fill
		// (the summary call would otherwise overflow near n_ctx); the same value drives the TUI's
		// footer/gauge below.
		Context: apogee.ContextConfig{MaxContextTokens: opts.contextWindow},
	}

	// A per-session startup warning whenever Auto runs unconfined (ADR 0012): confine=false
	// is safe only inside a VM, and it is the only blanket loosen in the system.
	if mode == modeAuto && !opts.confineToWorkspace {
		fmt.Fprintln(os.Stderr, "apogee: WARNING — auto mode is running UNCONFINED "+
			"(confine-to-workspace: false). This is safe only inside a VM/container; "+
			"the dangerous-action guard is a footgun-net, not a security boundary.")
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

	// Build the catalogued Mechanisms enabled in config.yaml and fold them into Config.Mechanisms
	// (Phase 4). deps is the construction-injected collaborator set (D3): LookPath is the host's
	// real PATH prober — autofix resolves its formatter table through it once at construction —
	// and the Library store is nil until it lands (item 13). mechanisms.Build drives the catalogue's
	// constructor table; an unknown ID or an incompatible pair is a loud startup error surfaced
	// here (the latter via New's ValidateIncompatibilities gate). With nothing enabled this is a
	// no-op (nil registry ⇒ New defaults to an empty one), so a config without a mechanisms block
	// behaves exactly as before.
	registry, err := buildMechanismRegistry(opts.mechanisms, mechanisms.Deps{LookPath: exec.LookPath}, mechanisms.Build)
	if err != nil {
		return err
	}
	cfg.Mechanisms = registry

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
		Skills:        skillProvider,
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

// registryWithMCP builds the Agent's tool registry: the built-in default tools scoped to the
// workspace (with the same host configuration the Agent would derive from Config — the
// url-safety floor, the web-search endpoint, the Asker) PLUS the dynamically discovered MCP
// tools registered on top. MCP tools are DYNAMIC (discovered from a server at runtime), so they
// are NOT in DefaultTools — they ride the registry as classMCP ExternalEffectTools the dispatch
// disposition gates in Auto. A duplicate name (an MCP server's qualified tool colliding with a
// built-in — unlikely given the alias prefix) is dropped with a stderr notice rather than
// failing startup; the built-in wins.
func registryWithMCP(workspace string, cfg apogee.Config, mcpTools []apogee.Tool) *apogee.ToolRegistry {
	registry := tools.NewDefaultRegistryWithHost(workspace, tools.HostTools{
		URLGuard:          security.URLGuard{},
		WebSearchEndpoint: cfg.WebSearchEndpoint,
		Asker:             cfg.Asker,
	})
	for _, t := range mcpTools {
		if err := registry.Register(t); err != nil {
			fmt.Fprintf(os.Stderr, "apogee: skipping MCP tool %q: %v\n", t.Name(), err)
		}
	}
	return registry
}

// mechanismBuilder constructs one catalogued Mechanism by canonical ID from the injected Deps
// (D3). internal/mechanisms.Build fills it in production (driving the catalogue's constructor
// table); a test injects a fake row so buildMechanismRegistry is table-testable while the real
// catalogue is still empty (waves 5–14 fill it). apogee.MechanismID / apogee.Mechanism are the
// public aliases for the domain types Build returns, so the assignment is a no-op.
type mechanismBuilder func(apogee.MechanismID, mechanisms.Deps) (apogee.Mechanism, error)

// buildMechanismRegistry builds a MechanismRegistry from the enabled-mechanism config (Phase 4),
// driving build for each ID whose `mechanisms:` value is true and Add-ing the result. Enabled IDs
// are sorted by canonical spelling so registration (and any error) is deterministic; the dispatch
// order is the registry's own topo-sort (ADR 0003), independent of registration order. An unknown
// ID (build's error) or a Mechanism implementing no hook interface (Add's error) is a loud startup
// failure — a typo'd or half-built Mechanism never silently vanishes. With nothing enabled it
// returns a nil registry so New falls back to its default empty one (today's behaviour unchanged).
func buildMechanismRegistry(enabled map[string]bool, deps mechanisms.Deps, build mechanismBuilder) (*apogee.MechanismRegistry, error) {
	ids := make([]string, 0, len(enabled))
	for id, on := range enabled {
		if on {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	sort.Strings(ids)

	registry := apogee.NewMechanismRegistry()
	for _, id := range ids {
		m, err := build(apogee.MechanismID(id), deps)
		if err != nil {
			return nil, err
		}
		if err := registry.Add(m); err != nil {
			return nil, fmt.Errorf("apogee: register mechanism %q: %w", id, err)
		}
	}
	return registry, nil
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
