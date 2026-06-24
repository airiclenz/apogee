package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/tui"
)

// Compile-time proof that the public Agent satisfies the TUI's narrow engine seam
// (phase-2 detail plan §3 C5): cmd dogfoods apogee.New, and *apogee.Agent (= *agent.Agent)
// is exactly what internal/tui drives — without internal/tui ever importing the root path.
var _ tui.Engine = (*apogee.Agent)(nil)

// The known autonomy modes, named locally so the flag default and parser reference a
// symbol rather than a bare string.
const (
	modePlan      = apogee.ModePlan
	modeAskBefore = apogee.ModeAskBefore
	modeAuto      = apogee.ModeAuto
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
		ConfigDir:    roots.config,
		LibraryDir:   roots.library,
		SessionsDir:  roots.sessions,
		WorkspaceDir: roots.workspace,
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
		Save:          saver.save,
	})
	if path := saver.saved(); path != "" {
		fmt.Fprintf(os.Stdout, "Session saved · resume with: apogee --resume %s\n", path)
	}
	return err
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

// parseMode validates the --mode flag against the known autonomy modes. Auto parses
// successfully here but is refused at construction (ADR 0004); friendlyConstructErr
// surfaces that as a Phase-3 message (phase-2 detail plan §3 C8).
func parseMode(s string) (apogee.Mode, error) {
	switch apogee.Mode(s) {
	case modePlan, modeAskBefore, modeAuto:
		return apogee.Mode(s), nil
	default:
		return "", fmt.Errorf("apogee: invalid --mode %q (want plan, ask-before, or auto)", s)
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

// errAutoPhase3 is the friendly Phase-2 framing of ErrAutoUnavailable: Auto mode needs
// Confinement, which is a Phase-3 deliverable (phase-2 detail plan §3 C8).
var errAutoPhase3 = errors.New(
	"apogee: auto mode requires Confinement, which lands in Phase 3 — " +
		"use --mode plan or --mode ask-before")

// friendlyConstructErr maps construction errors to actionable CLI messages. The headline
// case is Auto mode: New returns ErrAutoUnavailable when Mode==Auto without a Confiner.
func friendlyConstructErr(err error) error {
	if errors.Is(err, apogee.ErrAutoUnavailable) {
		return errAutoPhase3
	}
	return err
}
