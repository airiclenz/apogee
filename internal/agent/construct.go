package agent

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/console"
	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/processing"
	"github.com/airiclenz/apogee/internal/prompt"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tasklist"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/undo"
)

var (
	errMissingEvents   = errors.New("apogee: Config.Events is required")
	errMissingEndpoint = errors.New("apogee: Config.Endpoint is required")
)

// newAgent validates cfg and constructs a ready-to-Step TOP-LEVEL Agent bound to up. The public
// New delegates here with the real provider client; white-box tests inject a deterministic
// fake. A delegate is built by newDelegateAgent beside it; both are the one construction path
// (buildAgent) told which of the two it is building.
func newAgent(cfg domain.Config, up provider.Responder) (*Agent, error) {
	return buildAgent(cfg, up, nil)
}

// newDelegateAgent constructs a DELEGATE — a sub-agent one nesting level below the parent that
// composed d (newChildAgentOn, subagent.go) — bound to up, which is the parent's own Upstream or
// the client a routed spawn dialled. cfg is the parent's Config with the spawn's posture and dial
// facts already applied; d is everything else the child is: its identity, its bounds and the
// handles it shares with the parent. The constructor copies each of those facts into the Agent
// once, so a child leaves here complete and nothing is written to it afterwards.
func newDelegateAgent(cfg domain.Config, up provider.Responder, d *delegation) (*Agent, error) {
	return buildAgent(cfg, up, d)
}

// buildAgent is the one construction path behind newAgent (d == nil) and newDelegateAgent (d set).
// Validation order is deliberate: required fields first, then the Auto/Confinement gate
// (ADR 0012 — FSWrite-only AutoEligible), and finally the armed Reactions, which are validated
// against the engine's own builtins once those exist (ADR 0076 D1). The two kinds of Agent differ
// in exactly two places, both marked below: the fields a top-level Agent owns afresh and a delegate
// takes from d (seedTopLevel / delegation.seed), and the context-file cache, which only a session
// boundary fills from disk — a delegate is handed its parent's.
func buildAgent(cfg domain.Config, up provider.Responder, d *delegation) (*Agent, error) {
	if err := validateConfig(cfg); err != nil {
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
	// across both kinds and not merely within each (the question can only be the top-level agent's
	// since 2026-09-15 — plan 2026-09-14 - 03, item 5 — no child holds ask_user).
	cfg.Approver = queuedApprovals(cfg.Approver)

	a := &Agent{
		cfg:                cfg,
		upstream:           up,
		tools:              resolveTools(cfg),
		ownsToolSet:        composesDefaultRoster(cfg),                                                                        // …and whether the engine may RE-compose it when the model's roster axis changes (ADR 0057)
		mode:               cfg.Mode,                                                                                          // seed the live, swappable mode from the construction config
		confineToWorkspace: cfg.ConfineToWorkspace,                                                                            // likewise the live, swappable blast-radius flag (/confine)
		scratchDir:         cfg.ScratchDir,                                                                                    // and the live, session-following scratch root (SetScratchDir)
		gen:                domain.Generation{Bypass: cfg.Bypass, Floor: cfg.Floor, ContextFillNotice: cfg.ContextFillNotice}, // and the live Generation the settings surface swaps whole: Bypass and the notice switch beside the Floor enable set (SetReactions, ADR 0076 A8, ADR 0077) …
		compaction:         cfg.Context.CompactionEnabled,
		prune:              cfg.Context.PruneToolResults,
		contextFileNames:   cfg.ContextFiles,
		parallelAgents:     cfg.ParallelAgents, // and the fan-out width the host resolved per bound server
		textParser:         textParser,
		stripper:           stripper,
		tokens:             apogeectx.NewTokenEstimator(),        // fresh for a delegate too: a routed child starts uncalibrated by construction, so it never needs the reset SwitchUpstream and Rebind perform
		prompts:            domain.NewPromptSlot(),               // the one prompt surface this Agent tree queues on
		tasks:              tasklist.New(),                       // the model's checklist, empty and ENGINE-held: the tool may not hold it, because SwapTools rebuilds tool instances mid-session (ADR 0072, ADR 0008). A delegate's is fresh too — the delegation value carries no task-list handle by design (ADR 0072)
		tree:               newTreeSnapshotter(cfg.WorkspaceDir), // the tracked-file mutation floor around subprocess calls (treesnapshot.go)
	}
	// The fields the two kinds of Agent hold differently: a top-level Agent OWNS each afresh — its
	// guards, its undo journal, its Console registry, its Delegation-target latch, its clock and the
	// dialect its own server reads — where a delegate takes the parent's by handle, plus the identity
	// and bounds only a spawn can know. Seeded rather than written in the literal so neither kind
	// builds an instance the other would throw away.
	if d == nil {
		a.seedTopLevel(cfg)
	} else {
		d.seed(a)
	}
	// The engine's own Reactions — the Floor guards cfg.Floor leaves ON, and the context-fill
	// notice when cfg.ContextFillNotice switches it on — are
	// built HERE, after the literal, because each handler closes over this Agent. The ladder is the
	// ENABLE SET (ADR 0076 A8): a guard whose opt-out is set is absent from it, and SetReactions
	// rebuilds it whenever the live Floor or a notice switch moves. The host's own Reactions are
	// validated against ALL SEVEN guard keys whatever the enable set holds (armReactions): an
	// ill-formed entry, or one reusing a guard's or a sibling's ID, fails construction rather than
	// firing under a name something else already answers to.
	a.builtins = a.buildBuiltins(cfg.Floor, cfg.ContextFillNotice)
	armed, err := armReactions(cfg.Reactions)
	if err != nil {
		return nil, err
	}
	a.armed = armed

	// Fill the context-file cache for this session's first boundary: construction. Every later
	// refill goes through the same seam at a session boundary (contextfiles.go). A delegate is NOT
	// a session boundary: it speaks from its parent's bytes (seeded above from d.contextFiles), so an
	// AGENTS.md edited or deleted mid-delegation never reaches it — and the workspace is not read.
	if d == nil {
		a.reloadContextFiles()
	}
	// Wire the Turn lifecycle owner AFTER the literal so conv points at the Agent's field: a later
	// restoreState value-assigns a.conv, and the pointer keeps that write visible through a.turns.
	// The Agent rides along as the lifecycle's exchangeObserver for the same reason: the lifecycle
	// owns the moment an Exchange ends and the moment a cancelled Turn is rolled back, the Agent
	// owns what each costs — the undo journal's closing capture (ADR 0074) and the two notices
	// that rode the tool results the rollback drops, the context-fill ladder (ADR 0077 D4) and the
	// step-budget notice's latch (stepnotice.go).
	a.turns = &turnLifecycle{
		conv:     &a.conv,
		observer: a,
	}
	return a, nil
}

// exchangeClosed is the Agent's half of the exchangeObserver contract for an Exchange END
// (turnLifecycle.closeExchange): the undo journal's closing capture (closeUndoGroup, agent.go).
func (a *Agent) exchangeClosed() { a.closeUndoGroup() }

// turnRolledBack is the Agent's half of the exchangeObserver contract for a cancelled Turn's
// ROLLBACK (turnLifecycle.end's endCancelled row): the two notices that rode the dropped tool
// results are re-armed (rearmNotices, stepnotice.go).
func (a *Agent) turnRolledBack() { a.rearmNotices() }

// seedTopLevel gives a top-level Agent the fields it owns afresh — the ones a delegate takes from
// its parent instead (delegation.seed). Empty and per-process, every one of them, because this
// Agent IS the session's root: nothing above it holds an instance to share.
func (a *Agent) seedTopLevel(cfg domain.Config) {
	a.guards = security.NewDefaultGuards()
	a.dial = dialProvider                                  // the real provider client, until the constructor that took WithDialer says otherwise (New, Resume)
	a.effortDialect = toProviderDialect(cfg.EffortDialect) // the wire shape this server reads an effort intent in, so a Driver that never rebinds still speaks it (ADR 0060, ADR 0031)
	a.delegation = &delegationLatch{}                      // an empty Delegation-target latch: no routing until the host pushes one (ADR 0045)
	a.journal = undo.New()                                 // the per-Exchange undo record (ADR 0051)
	a.consoles = console.New()                             // the engine's live Consoles (ADR 0059)
	a.now = time.Now                                       // the request-render clock for the system prompt's {{datetime}}
}

// seed copies the delegation into the Agent under construction — every fact once, before anything
// can observe the child. The order matters in one place: the routed spawn's capture seam is bound
// LAST, after the identity it stamps on WireEvents (depth, spawning call id) is in place, so a
// routed child's events never carry the zero values a top-level Agent would.
func (d *delegation) seed(a *Agent) {
	a.depth = d.depth
	a.callID = d.spawnCallID
	a.task = d.task
	a.name = d.name // written bare: the child is unpublished until it is returned, so no reader can race the lock setName takes later
	a.consoleOwner = d.consoleOwner
	a.seatFallback = d.seatFallback
	a.stepCap = d.stepCap
	a.tokenCap = d.tokenCap
	a.timeCap = d.timeCap
	a.now = d.now
	a.effortDialect = d.effortDialect
	a.dial = d.dial                  // the parent's seam, so a grandchild's routed dial crosses the same one the host injected
	a.ownsUpstream = d.upstreamOwned // a routed child closes the client it dialled; an unrouted one must never close the session's out from under the parent still speaking over it (Agent.Close)
	a.guards = d.guards
	a.contextFiles = d.contextFiles
	a.liveMode = d.parentLiveMode
	a.delegation = d.latch
	a.journal = d.journal
	a.consoles = d.consoles
	// The child's other structural bound on runaway context: it folds under budget pressure at
	// quiescent TURN boundaries, not only at Exchange boundaries (shouldAutoCompact's S2 guard). A
	// delegation is ONE Exchange from its first Turn to its report, so the boundary the main loop's
	// trigger waits for never arrives for a child — without this its history simply grows until the
	// window is blown. Set on EVERY child, routed or not: it is the child's contract, not a Reaction
	// and not a per-server posture, so there is no key to disagree about.
	a.midExchangeCompaction = true
	d.tap.bind(a)
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
// question raised through the tool seam in internal/tools — by a sibling child until 2026-09-15,
// by the top-level agent alone since (plan 2026-09-14 - 03, item 5 withholds ask_user from every
// child). Queueing approvals only
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
// (domain.WithPromptSlot) and therefore also held for the ask_user questions the children raised —
// no longer reachable since 2026-09-15 (plan 2026-09-14 - 03, item 5): the tool is withheld from
// every sub-agent, so the slot now serialises children's Approvals against the top-level agent's
// own question.
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
//
// The Config → HostTools translation is tools.HostToolsOf — the one composer the composition root's
// MCP-aware assembly shares, so no host policy can apply on one path and not the other. The engine
// passes seatChoice false: `sub-agents-choice:` shapes the sub_agent schema a Driver publishes, and
// Config carries no field for it because the engine reads no config of its own (ADR 0031).
func defaultRoster(cfg domain.Config) *domain.ToolRegistry {
	return tools.NewDefaultRegistryWithHost(cfg.WorkspaceDir, tools.HostToolsOf(cfg, false))
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

// dialOptions composes the provider Options every dial in the engine crosses with: the wire the
// Config names for the server about to be dialled (provider.WithWire over provider.WireFor, so an
// unnamed wire folds to openai and the Client never speaks a protocol it does not have — ADR 0078),
// and, only when cfg.Inspector asks for it, the Inspector's wire observer (armWireCapture). It is
// called at every dial site — New, Resume, SwitchUpstream and the routed spawn — with the Config of
// the Agent that will speak over the connection, which is what keeps the wire a per-server fact:
// a switch dials the arrived-at server's wire, a routed child its target's, and neither inherits
// the departed or parent server's protocol.
func dialOptions(cfg domain.Config) ([]provider.Option, *wireTap) {
	capture, tap := armWireCapture(cfg)
	return append([]provider.Option{provider.WithWire(provider.WireFor(cfg.Wire))}, capture...), tap
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
