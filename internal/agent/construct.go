package agent

import (
	"errors"
	"fmt"
	"os"
	"slices"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/processing"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
)

var (
	errMissingEvents   = errors.New("apogee: Config.Events is required")
	errMissingEndpoint = errors.New("apogee: Config.Endpoint is required")
	errMissingModel    = errors.New("apogee: Config.Model is required")
)

// newAgent validates cfg and constructs a ready-to-Step Agent bound to up. The public
// New delegates here with the real provider client; white-box tests inject a deterministic
// fake. Validation order is deliberate: required fields, then the ordering-cycle,
// incompatibility, and requirements gates (ADR 0003; ADR 0014 §4), then the Auto/Confinement
// gate (ADR 0012 — FSWrite-only AutoEligible).
func newAgent(cfg domain.Config, up provider.Responder) (*Agent, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	registry := cfg.Mechanisms
	if registry == nil {
		registry = domain.NewMechanismRegistry()
	}
	// Arm the catalogued Mechanisms named on Config.EnableMechanisms, merging them into registry
	// BEFORE the ordering/incompatibility/requirements gates run over the whole graph (ADR 0015 §1–2).
	// A build/merge failure (unknown ID, duplicate, hook-less) is a construction failure.
	if err := buildEnabledMechanisms(cfg, registry); err != nil {
		return nil, err
	}
	if err := registry.ValidateOrdering(); err != nil {
		return nil, err
	}
	if err := registry.ValidateIncompatibilities(); err != nil {
		return nil, err
	}
	if err := registry.ValidateRequirements(); err != nil {
		return nil, err
	}

	if cfg.Mode == domain.ModeAuto && cfg.Confiner == nil {
		// Auto needs a Confiner to enforce the subprocess surface. A PRESENT-but-incapable
		// Confiner (no fs-confinement on this host) is allowed: Auto is entered and the
		// subprocess surface gates through Approval rather than refusing Auto ("confine if
		// you can, gate if you can't" — ADR 0012). Only a NIL Confiner — no facility injected
		// at all — refuses, so ErrAutoUnavailable is now conditional, not constant.
		return nil, domain.ErrAutoUnavailable
	}

	// Translate the model profile into the loop's parse-seam collaborators once (D2). A bad
	// profile (unknown tool-call format / thinking style) fails construction here rather than
	// silently falling back to native; a zero profile yields the native no-op parser + no-op
	// stripper, so the content path stays byte-identical.
	textParser, stripper, err := processing.ParserFor(cfg.Profile)
	if err != nil {
		return nil, err
	}

	a := &Agent{
		cfg:                cfg,
		upstream:           up,
		registry:           registry,
		tools:              resolveTools(cfg),
		guards:             security.NewDefaultGuards(),
		mode:               cfg.Mode,               // seed the live, swappable mode from the construction config
		confineToWorkspace: cfg.ConfineToWorkspace, // likewise the live, swappable blast-radius flag (/confine)
		textParser:         textParser,
		stripper:           stripper,
		tracker:            newSelfRegulator(),
		tokens:             apogeectx.NewTokenEstimator(),
	}
	// Wire the Turn lifecycle owner AFTER the literal so conv points at the Agent's field: a later
	// restoreState value-assigns a.conv, and the pointer keeps that write visible through a.turns.
	a.turns = &turnLifecycle{conv: &a.conv, tracker: a.tracker}
	return a, nil
}

// libraryMechanismID is the one catalogued ID whose presence in Config.EnableMechanisms makes the
// engine build and Load a Library store into Deps (only `library` reads Deps.Library; every other
// Mechanism ignores it). The catalogue owns the canonical constant (unexported there); this is the
// loop's copy of the same literal, guarded by the tests asserting a non-`library` arm never wires a
// store.
const libraryMechanismID domain.MechanismID = "library"

// buildEnabledMechanisms builds each Mechanism named on cfg.EnableMechanisms and Adds it into
// registry — the merge target: the caller's Config.Mechanisms, or the fresh registry newAgent made
// when that was nil — so catalogued Mechanisms and any pre-registered experimental hooks coexist in
// one arm (ADR 0015 §2, locked decision 2). This is the single build path from Config to the live
// registry: cmd/apogee/wire.go now only turns config.yaml into the Config.EnableMechanisms ID list
// and leaves construction to here (ADR 0015 §1). IDs are built in sorted canonical order so a
// build/register error is deterministic, and Deps are derived here: a Library store rooted at
// Config.LibraryDir and Loaded ONLY when `library` is enabled (never an ambient ~/.apogee — ADR
// 0001; a corrupt/absent store degrades to empty and never blocks construction, the store-persist
// posture that already surfaces soft store failures to stderr), the model Fingerprint resolved
// once, LookPath defaulted to exec.LookPath (nil), and the GrammarConstraint seam left inert. An
// unknown ID (Build wraps domain.ErrUnknownMechanism), an ID listed twice or already pre-built into
// the registry (the already-registered rejection), and a hook-less Mechanism all propagate as
// construction failures.
// An empty list builds nothing (the default-off posture untouched); the ordering, incompatibility,
// and requirements gates then run over the merged registry unchanged.
func buildEnabledMechanisms(cfg domain.Config, registry *domain.MechanismRegistry) error {
	if len(cfg.EnableMechanisms) == 0 {
		return nil
	}

	ids := slices.Clone(cfg.EnableMechanisms)
	slices.Sort(ids)

	var deps mechanisms.Deps
	if slices.Contains(ids, libraryMechanismID) {
		store := library.NewStore(cfg.LibraryDir)
		if err := store.Load(); err != nil {
			// A broken/absent Library never blocks startup: Load leaves the store empty-and-usable on
			// any soft error, so the run degrades to that empty store and proceeds (like the store's
			// own persist path, the degrade is surfaced to stderr).
			fmt.Fprintf(os.Stderr, "apogee: library store degraded to empty: %v\n", err)
		}
		deps.Library = store
		// The full identity ladder (ADR 0021 §3), keyed IDENTICALLY to the Validated-set
		// match at wire time so the Library's observations and an auto-applied set cannot end
		// up filed under two different identities for one model. The probe records live under
		// the injected ConfigDir — an empty one simply removes the behavioral rung rather
		// than reaching for an ambient ~/.apogee (ADR 0001).
		deps.Fingerprint = library.ResolveFingerprintFrom(library.Sources{
			ModelID:  cfg.Model,
			Endpoint: cfg.Endpoint,
			ProbeDir: library.ProbeDir(cfg.ConfigDir),
		})
	}

	for _, id := range ids {
		m, err := mechanisms.Build(id, deps)
		if err != nil {
			return err
		}
		if err := registry.Add(m); err != nil {
			return fmt.Errorf("apogee: enable mechanism %q: %w", id, err)
		}
	}
	return nil
}

// resolveTools picks the Agent's tool set: an explicitly injected Config.Tools wins;
// otherwise, when Config.WorkspaceDir is set, the built-in file tools scoped to it (with the
// network/host tools configured from Config — the url-safety policy, the web-search endpoint,
// and the Asker and Presenter delegates); else no tools (the host gave neither, so the Agent
// runs tool-less).
func resolveTools(cfg domain.Config) *domain.ToolRegistry {
	if cfg.Tools != nil {
		return cfg.Tools
	}
	if cfg.WorkspaceDir != "" {
		return tools.NewDefaultRegistryWithHost(cfg.WorkspaceDir, hostTools(cfg))
	}
	return nil
}

// hostTools builds the host-supplied tool configuration (P3.11) from Config: the url-safety
// guard the network tools filter through (the zero URLGuard — its default-on SSRF floor always
// applies in ALL modes, an app-level guard independent of OS confinement), the configured
// web-search endpoint (empty ⇒ web_search's built-in DuckDuckGo default; "off" disables it),
// the Asker delegate (nil ⇒ ask_user is not registered), and the Presenter delegate (nil ⇒
// present_document is not registered — ADR 0019).
//
// The url-safety policy is deliberately the default floor, NOT seeded from ConfineNetworkAllow:
// that field is the OS confinement box's network allow-list (CIDRs the confined SUBPROCESS may
// reach), a different concept from the in-process tools' host allow/deny — conflating them would
// silently restrict the network tools to the confinement list. A dedicated url-safety config key
// is a thin later addition; the SSRF floor is the security-relevant default and is on regardless.
func hostTools(cfg domain.Config) tools.HostTools {
	return tools.HostTools{
		URLGuard:          security.URLGuard{},
		WebSearchEndpoint: cfg.WebSearchEndpoint,
		Asker:             cfg.Asker,
		Presenter:         cfg.Presenter,
	}
}

// resumeAgent rebuilds an Agent from snap, then restores its loop state through the shared
// restoreSnapshot path — which rejects a snapshot newer than this build understands
// (ErrSessionVersion) before decoding the conversation. cfg supplies the live delegates afresh
// (ADR 0001); only the serializable conversation comes from snap.
func resumeAgent(cfg domain.Config, snap domain.Session, up provider.Responder) (*Agent, error) {
	a, err := newAgent(cfg, up)
	if err != nil {
		return nil, err
	}
	if err := a.restoreSnapshot(snap); err != nil {
		return nil, err
	}
	return a, nil
}

// validateConfig enforces the minimum construction surface (Config: Endpoint, Model, and
// Events are the minimum). Events is load-bearing — the loop emits through it; Endpoint and
// Model are validated here for an honest contract even when a test injects a fake responder
// that ignores them (the real provider dials them).
func validateConfig(cfg domain.Config) error {
	if cfg.Events == nil {
		return errMissingEvents
	}
	if cfg.Endpoint == "" {
		return errMissingEndpoint
	}
	if cfg.Model == "" {
		return errMissingModel
	}
	return nil
}
