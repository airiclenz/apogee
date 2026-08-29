package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/console"
	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/processing"
	"github.com/airiclenz/apogee/internal/prompt"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/undo"
)

var (
	errMissingEvents   = errors.New("apogee: Config.Events is required")
	errMissingEndpoint = errors.New("apogee: Config.Endpoint is required")
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
	deps, err := buildEnabledMechanisms(cfg, registry)
	if err != nil {
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

	// A bad system-prompt template fails construction loudly (like a bad profile above),
	// so an embedder typo never silently ships an un-rendered placeholder to the model.
	// For config users the cmd-side check fires first, naming the offending config key;
	// this is the engine's own gate, mirroring the ParserFor one (ADR 0023).
	if err := prompt.Validate(cfg.SystemPrompt); err != nil {
		return nil, err
	}

	// Likewise for the context-file names: an empty, absolute, or workspace-escaping name
	// fails construction naming the offender rather than letting the loader reach outside
	// the workspace. The host's config-side check fires first for config users.
	if err := validateContextFileNames(cfg.ContextFiles); err != nil {
		return nil, err
	}

	// Every Event this Agent (and every sub-agent it spawns) emits goes through one serializing
	// seam, so a depth-0 fan-out's concurrent children still hand the host a LINEAR stream
	// (domain.EventSink). It is installed once, here, rather than at the ~20 emit sites.
	cfg.Events = serializedEvents(cfg.Events)
	// And every Approval it raises goes through one queueing seam, for the same reason one level
	// up: concurrent children may reach an Approval gate at the same instant, and the host is
	// promised one request at a time (domain.Approver). The seam queues on the PROMPT SLOT this
	// Agent designates below — the surface an ask_user question queues on too, so the promise holds
	// across both kinds and not merely within each.
	cfg.Approver = queuedApprovals(cfg.Approver)

	a := &Agent{
		cfg:                cfg,
		upstream:           up,
		registry:           registry,
		tools:              resolveTools(cfg),
		ownsToolSet:        composesDefaultRoster(cfg), // …and whether the engine may RE-compose it when the model's roster axis changes (ADR 0057)
		guards:             security.NewDefaultGuards(),
		mode:               cfg.Mode,               // seed the live, swappable mode from the construction config
		confineToWorkspace: cfg.ConfineToWorkspace, // likewise the live, swappable blast-radius flag (/confine)
		scratchDir:         cfg.ScratchDir,         // and the live, session-following scratch root (SetScratchDir)
		bypass:             cfg.Bypass,             // and the three the settings surface swaps: Bypass …
		compaction:         cfg.Context.CompactionEnabled,
		contextFileNames:   cfg.ContextFiles,
		parallelAgents:     cfg.ParallelAgents,                   // and the fan-out width the host resolved per bound server
		effortDialect:      toProviderDialect(cfg.EffortDialect), // and the wire shape this server reads an effort intent in, so a Driver that never rebinds still speaks it (ADR 0060, ADR 0031)
		delegation:         &delegationLatch{},                   // an empty Delegation-target latch: no routing until the host pushes one (ADR 0045); newChildAgent replaces it with the parent's
		textParser:         textParser,
		stripper:           stripper,
		tracker:            newSelfRegulator(),
		tokens:             apogeectx.NewTokenEstimator(),
		prompts:            domain.NewPromptSlot(),               // the one prompt surface this Agent tree queues on
		journal:            undo.New(),                           // the per-Exchange undo record, empty and per-process (ADR 0051)
		consoles:           console.New(),                        // the engine's live Consoles, empty and per-process (ADR 0059)
		tree:               newTreeSnapshotter(cfg.WorkspaceDir), // the tracked-file mutation floor around subprocess calls (treesnapshot.go)
		now:                time.Now,                             // the request-render clock for the system prompt's {{datetime}}
		library:            deps.Library,                         // the shared Library store this build opened, nil unless an armed row needed one — Close flushes it
	}
	// Fill the context-file cache for this session's first boundary: construction. Every later
	// refill goes through the same seam at a session boundary (contextfiles.go).
	a.reloadContextFiles()
	// Wire the Turn lifecycle owner AFTER the literal so conv points at the Agent's field: a later
	// restoreState value-assigns a.conv, and the pointer keeps that write visible through a.turns.
	a.turns = &turnLifecycle{conv: &a.conv, tracker: a.tracker}
	return a, nil
}

// serialEventSink serializes concurrent Emit calls onto one host EventSink. It is the engine's
// half of the EventSink contract: the loop may emit from several goroutines once a depth-0
// fan-out is running (ADR 0039), and a host that only ever receives a linear stream — the TUI's
// transcript, the bench's tap, a recording sink in a test — must not have to guard itself.
//
// Ordering between concurrent emitters is deliberately unspecified: the mutex makes the stream
// linear and gives each observer a happens-before edge to the previous event, and the events
// themselves carry the identity an observer demultiplexes by (EventBase.CallID).
type serialEventSink struct {
	mu    sync.Mutex
	inner domain.EventSink
}

func (s *serialEventSink) Emit(e domain.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.Emit(e)
}

// serializedEvents wraps sink in the serializing seam, unless it already IS one. The idempotence
// is what keeps a nested sub-agent on the SAME mutex as its parent: newChildAgent copies the
// parent's Config, so the child's construction sees an already-wrapped sink and re-uses it rather
// than stacking a private, uncontended lock in front of it at every depth.
func serializedEvents(sink domain.EventSink) domain.EventSink {
	if _, already := sink.(*serialEventSink); already {
		return sink
	}
	return &serialEventSink{inner: sink}
}

// queuedApprover queues concurrent Approve calls onto one host Approver: it takes the single prompt
// slot — "the prompt on the screen" (domain.PromptSlot) — and admits one caller at a time. It is
// the engine's half of the Approver contract, the exact counterpart of serialEventSink's: once a
// depth-0 fan-out is running (ADR 0039), several children can reach an Approval gate at the same
// instant, and a host that has only ever fielded one request at a time must not have to grow a
// queue of its own.
//
// The slot it takes is the one the RUNNING AGENT designates on the call's context, not this
// wrapper's own: a Driver draws one prompt, and an Approval shares that surface with an ask_user
// question raised by a sibling through the tool seam in internal/tools. Queueing approvals only
// against other approvals would leave exactly that pair colliding — one of the two reply channels
// orphaned, its child blocked until the Turn is cancelled — so the queue is kind-blind. The
// wrapper's own slot is the fallback for a seam used outside a running Agent (a unit test, a Driver
// embedding the engine differently), where it is the only prompt surface there is.
//
// Cancelled-while-queued returns exactly what a cancelled visible prompt returns, (deny, ctx.Err()),
// which the caller reads as dispatchCancelled; the deny is the safe verdict for a request nobody
// will ever see, and the cancellation, not the verdict, is what ends the Turn. Why the wait is
// ctx-aware at all, and why the slot is held for the human's whole deliberation, are properties of
// domain.PromptSlot — documented there, where both kinds of prompt inherit them.
//
// The seam is also where the Session's allow-for-session memory lives (approvalCache), for the same
// reason the queue does: it is the one object the WHOLE tree shares, so a memory kept here is the
// Session's rather than one Agent's. It owns every WRITE to that memory — an inner Approver
// answering ApprovalAllowForSession seeds the key on the way back out — and one read of its own,
// the twin re-check below.
type queuedApprover struct {
	slot  *domain.PromptSlot // this seam's own surface; the context's wins where one is designated
	inner domain.Approver
	cache *approvalCache // the Session's allow-for-session memory, shared by the whole agent tree
}

func (q *queuedApprover) Approve(ctx context.Context, req domain.ApprovalRequest) (domain.ApprovalDecision, error) {
	slot := domain.PromptSlotFor(ctx, q.slot)
	if err := slot.Acquire(ctx); err != nil {
		return domain.ApprovalDeny, err
	}
	defer slot.Release()

	// The TWIN: a request that was queued behind the very prompt whose answer allowed its key.
	// Its caller checked the memory before queueing, when the key was not yet allowed, so without
	// this second check the human would be asked again for something they just allowed — the one
	// duplicate a session-wide memory cannot prevent by reading early alone. Re-checking here, on
	// the far side of the wait, coalesces it away. The caller emits its usual ApprovalEvent for
	// the verdict, which is what leaves a visible trace of the prompt that never appeared.
	if req.CacheKey != "" && q.cache.Allowed(req.CacheKey) {
		return domain.ApprovalAllowForSession, nil
	}

	decision, err := q.inner.Approve(ctx, req)
	// Only an ANSWERED allow-for-session is remembered: an errored call is not a decision (the
	// caller discards the verdict with it), and an empty key is a request whose answer may never be
	// remembered at all — a forced gate, which authorises its own call and nothing later.
	if err == nil && decision == domain.ApprovalAllowForSession && req.CacheKey != "" {
		q.cache.Allow(req.CacheKey)
	}
	return decision, err
}

// queuedApprovals wraps ap in the queueing seam, unless it already IS one. The idempotence keeps a
// nested sub-agent on the SAME wrapper as its parent, exactly as serializedEvents keeps it on the
// same mutex: newChildAgent copies the parent's Config, so a child re-uses the parent's seam instead
// of stacking a private one at every depth. What actually keeps a whole tree in ONE queue is the
// designated prompt slot, which rides the context a child inherits from its parent
// (domain.WithPromptSlot) and therefore also holds for the ask_user questions the children raise.
//
// That same idempotence is what gives the tree ONE allow-for-session memory: the cache is created
// here, with the wrapper, so re-using a parent's wrapper re-uses its memory — a child neither
// inherits a copy nor starts empty, it reads and writes the very map its parent does.
//
// A nil Approver stays nil. "No Approver configured" is a FACT the resolver reads
// (resolutionInput.approverPresent — a Gate with no Approver folds to a Refuse, Resolution D5), so
// wrapping nil into a non-nil forwarder would tell the ladder a human gate exists where none does.
func queuedApprovals(ap domain.Approver) domain.Approver {
	if ap == nil {
		return nil
	}
	if _, already := ap.(*queuedApprover); already {
		return ap
	}
	return &queuedApprover{slot: domain.NewPromptSlot(), inner: ap, cache: &approvalCache{}}
}

// buildEnabledMechanisms builds each Mechanism named on cfg.EnableMechanisms and Adds it into
// registry — the merge target: the caller's Config.Mechanisms, or the fresh registry newAgent made
// when that was nil — so catalogued Mechanisms and any pre-registered experimental hooks coexist in
// one arm (ADR 0015 §2, locked decision 2). This is the single build path from Config to the live
// registry: cmd/apogee/wire.go now only turns config.yaml into the Config.EnableMechanisms ID list
// and leaves construction to here (ADR 0015 §1). IDs are built in sorted canonical order so a
// build/register error is deterministic, and Deps are derived once by deriveDeps from what the
// enabled ROWS declare they need (mechanisms.DepsNeeded) — so the build loop below is uniform for
// every ID and names no Mechanism. An unknown ID (Build wraps domain.ErrUnknownMechanism), an ID
// listed twice or already pre-built into the registry (the already-registered rejection), and a
// hook-less Mechanism all propagate as construction failures.
// An empty list builds nothing (the default-off posture untouched); the ordering, incompatibility,
// and requirements gates then run over the merged registry unchanged.
//
// The derived Deps come BACK so the caller can hold what the build opened: the Library store is the
// one collaborator with a lifetime (a writer goroutine and pending observations), and the Agent that
// derived it is what flushes it at Close. An empty list — and any failure — returns the zero Deps,
// so a caller never holds a half-built collaborator.
func buildEnabledMechanisms(cfg domain.Config, registry *domain.MechanismRegistry) (mechanisms.Deps, error) {
	if len(cfg.EnableMechanisms) == 0 {
		return mechanisms.Deps{}, nil
	}

	ids := slices.Clone(cfg.EnableMechanisms)
	slices.Sort(ids)

	deps := deriveDeps(cfg, mechanisms.DepsNeeded(ids))

	for _, id := range ids {
		m, err := mechanisms.Build(id, deps)
		if err != nil {
			return mechanisms.Deps{}, err
		}
		if err := registry.Add(m); err != nil {
			// Add's rejections already carry the "apogee: " prefix the house convention puts on a
			// returned error, so the enable-path context is appended rather than prefixed — wrapping
			// would print the prefix twice (cmd/apogee/main.go prints a returned error verbatim). Same
			// shape as the ErrUnknownMechanism wrap in internal/mechanisms: the prefixed error leads.
			return mechanisms.Deps{}, fmt.Errorf("%w — while enabling mechanism %q", err, id)
		}
	}
	return deps, nil
}

// BuildMechanisms builds the catalogued Mechanisms named by ids into a registry of their own and
// runs the three stacking gates over it — the SAME path New walks for Config.EnableMechanisms and
// Rebind re-walks per model, exposed for a host that needs the REGISTRY rather than an Agent.
//
// The Delegation target is why one does (ADR 0045): a routed sub-agent's catalogue is resolved by
// the host from the Sub-agent server's own `mechanisms:` posture, and Config.Mechanisms takes a
// BUILT registry rather than an ID list, so the host has to build one. Going through here rather
// than around it is what keeps ADR 0015 §2's split intact — the engine derives Deps (the Library
// store and the identity ladder behind it), the catalogue declares which rows need them — and what
// keeps ADR 0031's benchable-all-the-way-up door open: a bench Driver latching a target of its own
// can compose the posture without a config file or an Agent in sight.
//
// cfg supplies what the build reads and nothing else: LibraryDir and ConfigDir for the store and the
// probe records behind it, Model and Endpoint for the identity the Library keys observations on — so
// a caller building the SUB-AGENT server's catalogue passes that server's model and endpoint, not
// the session's. It is taken by value and its EnableMechanisms is overwritten, so nothing the caller
// holds is touched.
//
// The registry comes back FRESH and owned by the caller. A per-child copy is
// MechanismRegistry.ForSubAgent, the same live-state isolation an inherited catalogue crosses the
// delegation boundary through — never the returned registry itself, which siblings would then share.
//
// An unknown ID, a Mechanism whose construction fails, and a set tripping the ordering,
// incompatibility or requirements gates are all errors here: exactly the errors a Config carrying
// those ids would have failed New with, raised where the host can still name the config that asked
// for them.
func BuildMechanisms(cfg domain.Config, ids []domain.MechanismID) (*domain.MechanismRegistry, error) {
	cfg.EnableMechanisms = ids
	registry := domain.NewMechanismRegistry()
	// The derived Deps are discarded here and nowhere else: this door hands back a REGISTRY, and the
	// Library store behind it is the per-process instance library.Open shares — the session Agent
	// built on the same LibraryDir holds it and flushes it at Close, so a routed child's catalogue
	// needs no lifetime of its own.
	if _, err := buildEnabledMechanisms(cfg, registry); err != nil {
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
	return registry, nil
}

// deriveDeps turns Config into the collaborators the enabled catalogue rows asked for, deriving each
// one only when needs says some row actually reads it. This is the ADR 0015 §2 split in code: the
// ENGINE derives Deps from Config — the store construction, the degrade notice and the identity
// ladder all live here, outside internal/mechanisms — while the CATALOGUE declares which rows need
// what (mechanisms.DepNeeds). A second Deps-bearing Mechanism therefore adds a flag and a row field,
// never a branch in this package naming a Mechanism ID.
//
// A Library store is rooted at Config.LibraryDir and opened only for a needs.Library arm (never an
// ambient ~/.apogee — ADR 0001). It comes from library.Open rather than library.NewStore, so the
// three paths that reach this function on ONE LibraryDir — New, every Rebind, and the routed
// sub-agent catalogue BuildMechanisms builds — share a single store instead of each rewriting the
// whole file from its own memory. Open Loads on its constructing call alone, which is what keeps the
// degrade notice below a once-per-process line without any coordination here. LookPath is left nil
// (the exec.LookPath default): it is not derived from Config, so it has no DepNeeds flag.
func deriveDeps(cfg domain.Config, needs mechanisms.DepNeeds) mechanisms.Deps {
	var deps mechanisms.Deps
	// The exec fence a Mechanism resolving an executable measures against. It is derived for
	// every run, not behind a needs flag: it costs nothing to carry, and a Mechanism that
	// spawns must refuse a model-writable program even on a host that can establish no
	// confinement box at all (internal/security.RefuseExecFromWritablePath).
	// NetworkAllow is cleared deliberately: it names hosts a confined subprocess may REACH,
	// which says nothing about the paths a program may be resolved FROM. Building the full box
	// and clearing the one field keeps that divergence one visible line rather than a silently
	// short literal.
	// The box is the CONSTRUCTION-TIME one deliberately, not a snapshot that went stale: its
	// only reader, autofix, resolves every formatter from PATH exactly once in newAutofix
	// (internal/mechanisms/autofix.go) — before the model has written a byte — and never
	// re-resolves, so the fence is measured at the same instant the paths it guards are. The
	// one box field that moves afterwards is the session scratch dir (Agent.SetScratchDir),
	// and a moved-to scratch dir is a freshly created ~/.apogee/scratch/<id>/ that cannot
	// contain an already-resolved formatter path: a live box would measure the same paths
	// against a fence that cannot include them. That is why the tools' per-call
	// Agent.confinementBox() (internal/agent/dispatch.go) follows the live scratch dir while
	// this one does not — the tools resolve and spawn PER CALL, against a tree the model has
	// been writing to in between; autofix resolves ONCE, ahead of all of it.
	deps.WritableBox = cfg.ConfinementBox()
	deps.WritableBox.NetworkAllow = nil
	// The operator-declared credential names a spawning Mechanism drops from its child's
	// environment (autofix's formatter, through tools.RunHookSubprocess). Same source and same
	// unconditional derivation as the tools' own copy (hostTools below): the two subprocess
	// families must scrub the same variables, and a run's enabled rows do not change which key
	// the operator exported.
	deps.SecretEnvVars = cfg.SecretEnvVars
	if needs.Library {
		store, err := library.Open(cfg.LibraryDir)
		if err != nil {
			// A broken/absent Library never blocks startup: Load leaves the store empty-and-usable on
			// any soft error, so the run degrades to that empty store and proceeds (like the store's
			// own persist path, the degrade is surfaced to stderr). Load's errors already carry the
			// "apogee: " prefix the house convention puts on a returned error, so the consequence is
			// appended rather than prefixed — wrapping would print it twice. Open returns the error on
			// the CONSTRUCTING call only, so a Rebind or a routed catalogue re-deriving the same
			// directory does not print the notice a second time.
			fmt.Fprintf(os.Stderr, "%v — library store degraded to empty\n", err)
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
	return deps
}

// resolveTools picks the Agent's tool set: an explicitly injected Config.Tools wins;
// otherwise, when Config.WorkspaceDir is set, the built-in file tools scoped to it (with the
// network/host tools configured from Config — the url-safety policy, the web-search endpoint,
// and the Asker and Presenter delegates); else no tools (the host gave neither, so the Agent
// runs tool-less).
//
// The roster ladder (ADR 0057) applies to the DEFAULT branch alone. An injected Config.Tools is
// the host's own assembly and is returned VERBATIM — before any roster is composed, so neither
// the global `tools.disabled:`/`tools.enabled:` lists nor the bound model's profile axis can
// subtract from a set the host built itself (ADR 0001). That is also why the engine may
// re-compose only what it composed: see composesDefaultRoster.
func resolveTools(cfg domain.Config) *domain.ToolRegistry {
	if cfg.Tools != nil {
		return cfg.Tools
	}
	if cfg.WorkspaceDir != "" {
		return defaultRoster(cfg)
	}
	return nil
}

// defaultRoster assembles the engine's OWN tool set for cfg: the build's menu scoped to the
// workspace and configured from Config, with the roster ladder applied over it (profile > global >
// build default — tools.DefaultToolsWithHost). It is one function rather than a line inside
// resolveTools because a model switch RE-runs it: the roster is a per-model binding, so the same
// assembly answers "which tools does this Agent start with" and "which tools does it have now that
// another model is bound", exactly as processing.ParserFor answers the parse seam's version of both
// (applyProfile). Startup and switch therefore cannot disagree about what a roster means.
func defaultRoster(cfg domain.Config) *domain.ToolRegistry {
	return tools.NewDefaultRegistryWithHost(cfg.WorkspaceDir, hostTools(cfg))
}

// composesDefaultRoster reports whether the tool set an Agent built from cfg is the engine's OWN
// assembly — the default registry above — rather than a set the host handed over. It is the line
// between the two tool doors, and it is what makes the roster safe to re-compose at a rebind:
//
//   - an injected Config.Tools is the host's authority verbatim (ADR 0001, ADR 0057's stated
//     bound), so the engine never rebuilds under it — a model switch leaves it exactly as it is;
//   - a set installed mid-session through SwapTools is the host's assembly too (ADR 0037 binding
//     F, MCP tools folded in and all), so taking that door clears the flag on the Agent;
//   - and a tool-less Agent (no workspace, no injected set) has no roster to compose at all.
//
// Only the remaining case — the engine composed the set from the build's menu — may be re-composed
// when the bound model's roster axis changes, because only there does the engine hold every fact
// the assembly needs.
func composesDefaultRoster(cfg domain.Config) bool {
	return cfg.Tools == nil && cfg.WorkspaceDir != ""
}

// hostTools builds the host-supplied tool configuration (P3.11) from Config: the url-safety
// guard the network tools filter through (built from the configured host lists — its default-on
// SSRF floor always applies in ALL modes, an app-level guard independent of OS confinement,
// and configuration can only tighten it), the configured
// web-search endpoint (empty ⇒ web_search's built-in DuckDuckGo default; "off" disables it),
// the Asker delegate (nil ⇒ ask_user is not registered), the Presenter delegate (nil ⇒
// present_document is not registered — ADR 0019), the three rungs of the roster ladder — the
// global `tools.disabled:`/`tools.enabled:` lists and the bound model's profile axis, over the
// build's own default-off declarations (ADR 0057; all empty ⇒ the whole built-in set) —, the
// credential variable names the execution tools scrub from a
// subprocess environment (empty ⇒ apogee's own alone), and the extra read-only roots the read
// tools may reach (nil ⇒ workspace-only).
//
// The url-safety policy comes from Config.URLAllowHosts / URLDenyHosts (`url-safety:`) and is
// still deliberately NOT seeded from ConfineNetworkAllow: that field is the OS confinement box's
// network allow-list (CIDRs the confined SUBPROCESS may reach), a different concept from the
// in-process tools' host allow/deny — conflating them would silently restrict the network tools to
// the confinement list. Empty lists ⇒ the zero guard; the SSRF floor is the security-relevant
// default and is on regardless, because the config layer can only tighten (security.NewURLGuard).
func hostTools(cfg domain.Config) tools.HostTools {
	return tools.HostTools{
		// Built through the shared constructor rather than filled here, because HostTools is
		// composed by hand in a second place too (cmd/apogee's MCP-aware assembly) and a
		// hand-copied fill is how one of the two paths silently stops applying the user's policy.
		URLGuard:          security.NewURLGuard(cfg.URLAllowHosts, cfg.URLDenyHosts),
		WebSearchEndpoint: cfg.WebSearchEndpoint,
		Asker:             cfg.Asker,
		Presenter:         cfg.Presenter,
		// The GLOBAL rung of the roster ladder (`tools.disabled:` / `tools.enabled:`, ADR 0057):
		// the disabled names are left out of the set this builds, which is the whole of that key —
		// an Agent cannot offer or dispatch a tool its registry does not hold — and the enabled
		// names put back what the BUILD leaves off the default menu (a tool registered
		// domain.DefaultOffTool). Both are overridden per tool by the profile axis below, and a
		// name in both is a conflict disabled wins; reporting that, and an unknown name, is the
		// host's job (tools.RosterConflicts / tools.KnownToolNames), never a refusal to build.
		Disabled: cfg.DisabledTools,
		Enabled:  cfg.EnabledTools,
		// The MOST SPECIFIC rung: the bound model's own roster axis, the third axis of the Model
		// profile the composition root already resolved for this model (ADR 0057). It travels on
		// Config.Profile rather than in a field of its own precisely so it cannot drift from the
		// other two axes — one resolved profile, three axes, one binding — and it is what makes a
		// tool off for the small class and on for a big model. A zero delta says nothing, which
		// keeps a zero profile the byte-identical anchor here too.
		ProfileRoster: cfg.Profile.Tools,
		// The caller-named half of the execution tools' credential scrub (`api-key-env:`, ADR 0047):
		// the variables the host's configured key sources read, dropped from every subprocess
		// environment beside apogee's own APOGEE_API_KEY. Empty ⇒ apogee's own alone, the scrub as
		// it was before the host could name any.
		SecretEnvVars: cfg.SecretEnvVars,
		// The read-only mounts beside the workspace fence, handed through verbatim: the engine
		// carries the func without evaluating it, so WHICH dirs are mounted stays the host's
		// question and stays live per call (Config.ExtraReadRoots). nil ⇒ workspace-only.
		ExtraReadRoots: cfg.ExtraReadRoots,
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

// wireTap is the Inspector's capture seam: the provider observer that turns the raw bytes of one
// Upstream round-trip into a domain.WireEvent on the emitting Agent's EventSink. It exists as a
// bindable object rather than a plain closure because of an ordering fact — a provider.Client's
// observer is fixed by an Option at construction, while the Agent whose identity stamps the events
// is built FROM that client. So the tap is created first, installed on the client, and pointed at
// the Agent (bind) the moment it exists; the window between the two is closed before any caller
// can Step, and observe answers a nil binding by capturing nothing rather than by panicking.
//
// One tap serves one CLIENT, and a client is shared by an Agent and its unrouted sub-agents — so a
// delegated call made over the parent's connection is stamped with the parent's identity, which is
// the honest fact about a shared connection. A ROUTED spawn (ADR 0045) builds a client of its own
// and therefore gets a tap of its own, stamped at the child's depth and spawning call id.
type wireTap struct {
	// a is the Agent whose base() stamps the events. Written once by bind, on the goroutine that
	// constructs the Agent, before that Agent can take a Step — the same publication the
	// constructed Agent's own fields rely on.
	a *Agent
}

// armWireCapture returns the provider Options that arm the Inspector for an Agent about to be
// constructed from cfg, together with the tap the caller must bind to it. A cfg that does not ask
// for the Inspector arms nothing and returns (nil, nil): no observer reaches the Client, so its
// capture paths stay dead and the session is byte-identical to one built before the key existed.
// bind on the nil tap is a no-op, so a caller needs no branch of its own.
func armWireCapture(cfg domain.Config) ([]provider.Option, *wireTap) {
	if !cfg.Inspector {
		return nil, nil
	}
	t := &wireTap{}
	return []provider.Option{provider.WithWireObserver(t.observe)}, t
}

// bind points the tap at the Agent whose identity stamps its events. It is a no-op on a nil tap —
// the shape armWireCapture returns when the Inspector is disarmed.
func (t *wireTap) bind(a *Agent) {
	if t == nil {
		return
	}
	t.a = a
}

// observe is the provider.WireRecord callback: one record becomes one WireEvent on the bound
// Agent's sink, stamped with that Agent's current Turn, nesting depth and spawning call id — the
// same EventBase every other Event it emits carries. The payload is carried as text; it is the
// Client's own buffer, which the contract on provider.WireRecord permits keeping, and the string
// conversion copies it anyway.
//
// An unbound tap captures nothing: that can only be the construction window described on wireTap,
// where no call has been made yet, so there is nothing to report and no reason to fail.
func (t *wireTap) observe(rec provider.WireRecord) {
	a := t.a
	if a == nil {
		return
	}
	a.cfg.Events.Emit(domain.WireEvent{
		EventBase: a.base(a.turns.index),
		Direction: string(rec.Direction),
		Payload:   string(rec.Payload),
	})
}

// validateConfig enforces the minimum construction surface (Config: Endpoint and Events).
// Events is load-bearing — the loop emits through it; Endpoint is validated here for an honest
// contract even when a test injects a fake responder that ignores it (the real provider dials it).
//
// Model is deliberately NOT required: a host may construct before it knows which model the
// Upstream serves and bind it later through Rebind (ADR 0024) — that is what lets the TUI paint
// instantly against a server that is still starting. The requirement did not vanish, it moved to
// where it is actually load-bearing: Submit refuses with errNoModelBound while nothing is bound,
// so a model-less request can never reach the wire.
func validateConfig(cfg domain.Config) error {
	if cfg.Events == nil {
		return errMissingEvents
	}
	if cfg.Endpoint == "" {
		return errMissingEndpoint
	}
	return nil
}
