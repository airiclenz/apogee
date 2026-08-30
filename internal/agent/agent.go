package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/console"
	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/processing"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/undo"
)

// ----------------------------------------------------------------------------
// Construction & lifecycle (ADR 0001)
// ----------------------------------------------------------------------------

// Agent is a single embeddable Apogee agent instance. It owns the loop,
// conversation state, tool dispatch, and Mechanism application. It holds no
// process-global state: every state root is injected through Config, so many
// Agents can run in one process against isolated directories (the property the
// bench relies on for isolation — ADR 0001). The root apogee package re-exports it
// as an alias (type Agent = agent.Agent); its methods are the public surface.
//
// An Agent is not safe for concurrent use by multiple goroutines; drive one Agent
// from one goroutine (Step/Run), and observe it from another only via its EventSink.
// That is a rule about the CALLER, and a depth-0 sub-agent fan-out does not bend it: the
// parent's loop still runs on one goroutine, and the pool it opens mid-dispatch (ADR 0039)
// is internal to that one dispatch — each worker drives a SEPARATE nested Agent, and every
// touch of the parent's own state happens on the dispatching goroutine before the pool opens
// or after it joins. What the children share of the parent is the EventSink always and the
// Upstream conditionally: this said "the Upstream and the EventSink" flatly until ADR 0045 made a
// spawn with a Delegation target latched ROUTED, dialing a provider client of its own
// (newChildAgent), so the Upstream is shared only while nothing is latched. Both tolerate the
// sharing (the sink through the engine's serializing seam), and a routed child's own client is
// reached by nobody else — routing only narrows what concurrent siblings touch.
// The methods touching loop state fall into three call classes. Idle-only calls (Submit,
// ClearContext, RestoreSession, Compact, Rebind, SwapTools, SetProfile, AbortExchange) need a
// quiescent boundary with no Exchange mid-flight. Between-Steps calls by the goroutine DRIVING the loop
// (Snapshot, Interject) are additionally valid at the boundary between two Steps of an open
// Exchange: that goroutine owns the conversation there, so the boundary itself is the
// synchronization — no lock, and no other goroutine may make the call (ADR 0025). The
// anytime-goroutine-safe class — SetMode, SetConfineToWorkspace, SetBypass,
// SetCompactionEnabled, SetContextFiles, SetParallelAgents and SetDelegationTarget — is the exception: each swaps ONE live field
// behind its own mutex, so the host (the settings surface, Shift+Tab, /confine) may call it
// while a Step runs and the change lands at that field's next consumption boundary.
type Agent struct {
	cfg      domain.Config
	upstream provider.Responder        // provider seam (Decision C): fake in tests, real HTTP via New
	registry *domain.MechanismRegistry // catalogued + experimental hooks driving the loop
	tools    *domain.ToolRegistry      // resolved tool set (Config.Tools, or the default registry composed over the roster ladder)
	guards   security.Guards           // always-on, mode-independent guardrails (dangerous-action + circuit-breaker + audit, D6)

	// ownsUpstream says whether THIS Agent dialled the client in upstream and is therefore the one
	// allowed to tear it down. New, Resume, SwitchUpstream and a ROUTED spawn each build a client of
	// their own and set it; a plain spawn speaks over the PARENT's client (newChildAgent) and leaves
	// it false, as does the fake Responder a test injects through newAgent. Close and SwitchUpstream
	// consult it before closing, so a shared client outlives every child that borrowed it and is
	// closed exactly once — by its owner.
	ownsUpstream bool

	// ownsToolSet says whether the ENGINE composed the tool set in tools — resolveTools' default
	// branch, the build's menu with the roster ladder applied (ADR 0057) — and may therefore
	// RE-compose it when the bound model's roster axis changes at a Rebind. It is false whenever
	// the set belongs to the host instead: an injected Config.Tools, which is the host's authority
	// verbatim (ADR 0001), and a set installed mid-session through SwapTools, which is the host's
	// own assembly with its MCP tools folded in (ADR 0037 binding F). Rebuilding under either would
	// silently drop tools the host put there, so the roster binding stops at this flag.
	//
	// Seeded at construction (composesDefaultRoster) and cleared by SwapTools; never set back.
	ownsToolSet bool

	// textParser and stripper are the parse-seam collaborators selected from cfg.Profile at
	// construction (processing.ParserFor): the text-format tool-call parser recovers a call from
	// the visible content of a non-native model, and stripper lifts the inline thinking/harmony
	// channel out of that content. A native, no-inline-thinking profile (the zero value) yields
	// no-op parsers, so the content path is byte-identical to the pre-profile loop. applyProfile
	// re-selects both from a new profile at an idle boundary, through either door onto it: a
	// `model-profiles:` edit under a stable model (SetProfile) or an observed model change
	// (Rebind, ADR 0044). cfg.Profile moves with them, so the emit half (toolInstructions)
	// follows.
	textParser processing.ToolCallParser
	stripper   processing.ContentStripper

	// modeMu guards mode — one of the two fields shared across goroutines (confineToWorkspace
	// below is the other), the deliberate exception to the single-goroutine contract above. The
	// UI cycles the autonomy mode (Shift+Tab → SetMode) while the worker goroutine reads it
	// during dispatch (Mode() in toolMenu / the Resolution); the RWMutex makes that overlap
	// race-free. cfg.Mode stays the immutable construction seed.
	modeMu sync.RWMutex
	mode   domain.Mode // live autonomy mode; seeded from cfg.Mode at construction, swappable via SetMode

	// confineMu guards confineToWorkspace, the second live field the UI may swap while the
	// worker drives a Step (/confine off|on → SetConfineToWorkspace). It is a SIBLING of modeMu,
	// not the same lock, because the two settings are independent: the Resolution reads them as
	// two separate facts (resolutionInput), never as one pair that must be consistent, and each
	// mutex here stays named for the single field it guards. cfg.ConfineToWorkspace stays the
	// immutable construction seed.
	confineMu          sync.RWMutex
	confineToWorkspace bool // live confine-to-workspace flag; seeded from cfg.ConfineToWorkspace, swappable via SetConfineToWorkspace

	// scratchMu guards scratchDir, the session scratch directory the confinement box carries as
	// an extra writable root (Config.ScratchDir). It is another live field of the modeMu class,
	// with its own lock for the same reason each sibling has one: the host moves it at a SESSION
	// boundary (/clear|/new mints a new session id, /sessions resume adopts another) from its own
	// goroutine while the worker reads it per tool call (confinementBox, dispatch.go).
	// cfg.ScratchDir stays the immutable construction seed.
	scratchMu  sync.RWMutex
	scratchDir string // live session scratch dir; seeded from cfg.ScratchDir, swappable via SetScratchDir

	// bypassMu, compactionMu and contextFilesMu guard the three settings the settings surface may
	// swap mid-session (SetBypass / SetCompactionEnabled / SetContextFiles). They follow the modeMu
	// pattern to the letter — one mutex per field, named for the single field it guards, because the
	// three are independent facts read at three different boundaries and never as one consistent
	// tuple. Their cfg counterparts (cfg.Bypass, cfg.Context.CompactionEnabled, cfg.ContextFiles)
	// stay the immutable construction seeds, so the whole-struct cfg copy a sub-agent spawn takes
	// (newChildAgent) keeps reading fields nothing ever writes.
	bypassMu sync.RWMutex
	bypass   bool // live Bypass flag; seeded from cfg.Bypass, swappable via SetBypass

	compactionMu sync.RWMutex
	compaction   bool // live auto-Compaction gate; seeded from cfg.Context.CompactionEnabled, swappable via SetCompactionEnabled

	contextFilesMu   sync.RWMutex
	contextFileNames []string // live workspace context-file names; seeded from cfg.ContextFiles, swappable via SetContextFiles

	// parallelAgentsMu guards parallelAgents, a fifth field of the same class and for the same
	// reason: the Parallel agents cap is a property of the SERVER this session is bound to (ADR
	// 0039), and a `/server` switch or a heartbeat that observes another slot count moves it from
	// the host's goroutine while a Step may be running. cfg.ParallelAgents stays the immutable
	// construction seed.
	parallelAgentsMu sync.RWMutex
	parallelAgents   int // live depth-0 fan-out width; seeded from cfg.ParallelAgents, swappable via SetParallelAgents

	// delegation is the Delegation-target latch — the Sub-agent server every delegation in this
	// tree routes to, or nil for today's parent-inheriting spawn (ADR 0045 —
	// internal/agent/delegationtarget.go). It is a sixth live field of the anytime-safe class, and
	// the one field held by POINTER rather than by value: newChildAgent hands the child the
	// PARENT's holder, so one latch serves the whole tree and a host pushing to the top-level
	// Agent reaches a depth-2 spawn too. It carries its own lock for the same reason the five
	// above carry theirs, and is never nil on a constructed Agent (newAgent allocates it).
	delegation *delegationLatch

	// effortMu guards effortOverride, this session's Thinking-effort intent (CONTEXT: Thinking
	// effort): the level a user asked THIS session to think at, layered above whatever the bound
	// model's profile carries (ADR 0050). It belongs to the anytime-safe class above — /effort is
	// settable while a Turn runs and the wire projection reads it once per request — and it is the
	// one member with NO cfg seed: an override is intent a user states mid-session, never
	// configuration an Agent is constructed with, so it starts empty and the profile alone governs
	// until someone sets it. The zero value is the ABSENCE of an override, not a fifth level.
	effortMu       sync.RWMutex
	effortOverride domain.ThinkingEffort

	// effortDialect is the wire shape THIS server reads a thinking-effort intent in — the one
	// effort fact that crosses into the engine (ADR 0060). It is a property of the SERVER, not of
	// the model, so it moves the way the other server-scoped bounds do: SEEDED at construction
	// from Config.EffortDialect (the mode/confine/scratch pattern), then overwritten by every
	// RebindSpec the composition root computes whole and Rebind commits. Unlike the override above
	// it carries NO lock, because it moves only at construction and at the idle-only Rebind
	// boundary that a.cfg itself moves at (the boundary IS the synchronization — see Rebind),
	// while the wire projection reads it once per request on the worker goroutine.
	//
	// The seed is what gives a Driver that never rebinds the same wire a session gets (ADR 0031):
	// an unattended Firing, a bench arm and any embedder over run.Once state the detected or
	// configured dialect on the Config and are served it from the first request. The zero value
	// (provider.EffortDialectNone) stays the wire anchor for a caller that names none — it
	// reproduces the request bytes that predate the dialect seam, so nothing that leaves the field
	// alone speaks differently than it always did.
	effortDialect provider.EffortDialect

	// liveMode, when non-nil, is a sub-agent's read-only view of its PARENT's live mode: the
	// parent's effectiveMode accessor, captured at spawn (ADR 0013). The per-call
	// Resolution takes the TIGHTER of this and the child's own spawn mode, so a parent that
	// tightens mid-delegation (Shift+Tab down from Auto to Plan) gates/refuses the still-running
	// child's next call, while a parent loosening can never loosen it. Because the captured
	// accessor is the parent's EFFECTIVE mode, the view COMPOSES transitively: at depth 2 it
	// already folds in the top-level agent's live mode, so a tightening reaches every descendant
	// and no child's privileges can exceed its parent's. It is a closure over the
	// accessor — NOT the shared mode field/mutex — so the child observes the parent's mode
	// race-free but cannot mutate it. nil for a top-level Agent, which then behaves exactly as
	// before (its own mode governs).
	liveMode func() domain.Mode

	// tracker is the per-Session self-regulation state (effectiveness tracking, Adaptive
	// Suppression, the Turn Budget — internal/agent/selfreg.go). It is NOT serialized: Resume
	// rebuilds it fresh via newAgent, the accepted v1 reset-on-resume posture (plan item 3).
	tracker *selfRegulator

	// tokens is the structural token accounting behind the Budget view: a chars→token estimator
	// the loop calibrates against each Turn's server-reported usage (internal/context). Like the
	// tracker it is per-Session and NOT serialized — a resumed Agent recalibrates from its first
	// UsageEvent, reporting the default ratio and a zero Used until then. It is structural, not a
	// Mechanism, so it stays live under Bypass (D5/D6).
	tokens *apogeectx.TokenEstimator

	// usage is THIS Agent's cumulative token accounting — the running sum every UsageEvent it
	// emits is stamped with, so a Driver reads session totals off the latest event per agent
	// instead of summing a stream (domain.UsageEvent). It counts this Agent's own calls only:
	// newChildAgent builds a child through newAgent, which gives it a fresh zero tally, so a
	// sub-agent's events carry CHILD-LOCAL totals and per-agent grouping stays with the observer,
	// which already has the Depth and CallID stamps to group by. Like tokens and tracker it is
	// per-Session and NOT serialized — a resumed Agent counts from zero.
	usage usageTally

	// prompts is the ONE prompt surface this Agent's Steps designate on their context
	// (domain.PromptSlot): the slot every human gate reached under a Step queues on, whatever KIND
	// of gate it is — an Approval the loop raises itself, or an ask_user question a tool raises one
	// interface boundary away. A Driver draws one prompt, so serializing each kind against itself
	// would still let an approval and a question collide once a fan-out is running (ADR 0039).
	// Every Agent constructs one, but only the OUTERMOST one is designated: WithPromptSlot keeps
	// whichever the context already carries, and a sub-agent runs under a context derived from its
	// parent's, so a whole tree — however deep, however wide — queues on the top-level Agent's and
	// a nested Agent's own stays inert.
	prompts *domain.PromptSlot

	// now is the request-render clock the configured system prompt's {{datetime}} placeholder
	// reads (ADR 0023). It is a field rather than a direct time.Now call so a test can pin the
	// rendered date — the injectable-clock shape cmd/apogee's sessionHost.now already uses.
	// newAgent seeds it with time.Now; it is never nil on a constructed Agent.
	now func() time.Time

	// contextFiles is the session's cache of discovered workspace context files
	// (Config.ContextFiles — internal/agent/contextfiles.go). It is filled at construction and
	// REFILLED only at a session boundary (ClearContext, a successful RestoreSession), so the
	// standing content a request carries is byte-stable for the life of a session — a
	// mid-session edit to a repo's AGENTS.md lands on the next session, not under a running
	// one. A sub-agent is handed the parent's slice verbatim (newChildAgent), so one session's
	// nested agents all speak from the same bytes. It is not serialized: the resolved-live
	// posture of ADR 0023 §6 means a resumed session re-reads the CURRENT files.
	contextFiles []contextFile

	// journal is the per-Exchange pre-image record behind the human's `/undo` (ADR 0051):
	// what every funnel write replaced, grouped by the Exchange that caused it. It is LIVE
	// HOST STATE, not session state (ADR 0022 §8) — in memory, for this process only, never
	// serialized — so a resumed session starts with an empty one and cannot revert an earlier
	// process's writes. newAgent always supplies it; a nil journal is the honest encoding of an
	// engine that records nothing, and every reader treats it as such rather than as an error.
	// A delegated child is handed the PARENT's instance rather than its own (newChildAgent), so
	// the whole tree's writes land in one Exchange's undo step; only the depth-0 Agent opens a
	// group on it (loop.go).
	journal *undo.Journal

	// consoles is the set of interactive processes the model drives across Turns — the Console
	// family's live state (ADR 0059). Like the journal it is LIVE HOST STATE, not session state
	// (ADR 0022 §8) — in-memory, for this process only, never serialized — because what it holds
	// are running processes: a resumed session inherits no shells from the process that ended,
	// and an id from an earlier run is simply an id this registry does not have. newAgent always
	// supplies it; the nil receiver its close paths tolerate is the honest "no consoles" engine.
	// A delegated child is handed the PARENT's instance (newChildAgent), so one engine has one
	// set of Consoles however deep the delegation nests, and ownership is carried by the Console's
	// Owner field rather than by which Agent holds the registry: a delegation's end closes the
	// Consoles that delegation opened (Close) and nothing else.
	consoles *console.Registry

	// library is the Library store the build derived for this session's catalogue, or nil when no
	// armed row asked for one (mechanisms.DepNeeds.Library). It is the per-process, per-directory
	// instance library.Open shares, so the session's own registry, its every Rebind and a routed
	// sub-agent's catalogue all hold this same pointer rather than three writers on one file. What
	// the Agent owns is not the store but the FLUSH: Close is the only place that knows the run has
	// ended, so it is where the observations recorded since the last debounce reach disk. A child at
	// depth ≥ 1 never flushes it — the parent's Close does (closeLibrary).
	library *library.Store

	// tree is the tracked-file mutation floor around subprocess tool calls
	// (treesnapshot.go): git-status snapshots taken before and after each subprocess
	// run so the result names the workspace files the command changed. A structural
	// floor in the ADR 0006 class — always on, every mode including Bypass — active
	// only when the workspace root is a git repository (probed once per Agent,
	// cached). newAgent always supplies it; nil is an inactive floor, never an error.
	tree *treeSnapshotter

	conv          domain.Conversation // serializable conversation state (ADR 0001)
	pendingInput  *domain.UserInput   // queued by Submit, consumed by the next Step
	turns         *turnLifecycle      // owns the Turn/Exchange lifecycle state (index, inExchange, exchangeStart) and, from item 2 on, the exits — internal/agent/turn.go
	compacting    bool                // guards the automatic Compaction trigger against re-entry (item 9)
	compactSat    bool                // saturation latch: a prior auto-fold could not bring history under its allocation, so further automatic folds stand down until the estimate drops back under it (S2)
	compactFailed bool                // stand-down latch: an automatic fold FAULTED, so the estimate-driven trigger stands down for the rest of THIS Exchange rather than re-running the identical failing summary call at every Turn boundary (turnLifecycle.openExchange clears it; the emergency fold and the on-demand /compact ignore it)
	depth         int                 // sub-agent nesting level: 0 = top-level; a sub-agent runs at parent+1 (ADR 0013)
	callID        string              // this Agent's run identity: the id of the sub_agent call that spawned it, stamped on every Event it emits (domain.EventBase.CallID); empty at depth 0
	consoleOwner  string              // this Agent's Console PRIVILEGE identity: the engine-minted key (console.Registry.MintOwner) its Consoles are stamped with and its end reaps by; empty at depth 0. Deliberately not callID — that id is the model's to choose, and two siblings of one Turn can collide on it (ADR 0059 §6)
	task          string              // the task this Agent was delegated, from the spawning sub_agent call's arguments — what an Approval prompt names it by (domain.ApprovalRequest.SubAgentTask); empty at depth 0
	name          string              // this Agent's display identity in words: the optional short name the spawning sub_agent call supplied, normalised to a trimmed first line; empty = unnamed, and every display falls back to task. Display only, never privilege (ADR 0005)

	// stepCap bounds the Turns this Agent may take in ONE Exchange before Run ends it; 0 =
	// unbounded. It is a DELEGATE bound: only newChildAgent seeds it (from
	// Config.Delegation.MaxSteps, the `delegate-max-steps` key), and a sub_agent call's optional
	// max_steps may lower it further for that one delegation — a top-level Agent is left at 0,
	// because the main loop is the human's to stop and a delegate's is nobody's. Structural
	// (ADR 0006), not a Mechanism: it stays on under Bypass. Run is the ONE enforcement site.
	stepCap int

	// midExchangeCompaction lifts shouldAutoCompact's Exchange-boundary-only gate (S2) for this
	// Agent, so the estimate-driven fold may also run at a quiescent TURN boundary — the top of
	// step(), where the previous Turn's tool results are already committed. It is a DELEGATE
	// contract, set by newChildAgent alone: a child's whole life is ONE Exchange, so a
	// boundary-only trigger never fires for it however far its history outgrows the Budget, while
	// the main loop keeps folding at Exchange boundaries only so bench arms stay comparable.
	// Structural (ADR 0006), not a Mechanism: there is no config key and it stays on under Bypass.
	midExchangeCompaction bool

	// lastFault is the text of the most recent loop-level fault this Agent surfaced as an
	// ErrorEvent — the very sentence the human already read (emitLoopFault). It exists for the
	// PARENT of a delegation: runSubAgent turns a faulted child Exchange into an error tool
	// result, and without this the result could only point at "the preceding error", which the
	// parent MODEL never sees. A fault ends the Exchange, so the last one recorded is always the
	// one that abandoned it; an Exchange abandoned with no ErrorEvent at all (a recovered hook
	// panic) leaves this empty and the caller falls back to wording that names no cause. Written
	// on this Agent's own loop goroutine, read by the parent only after Run has returned.
	lastFault string
}

// stepCapErrFormat is the ErrorEvent text a delegate's Exchange ends on when it reaches its step
// cap — the human-facing half of the bound, and the only thing that says the delegation was
// STOPPED rather than finished. It is a package constant, pinned by test, because the line names
// the key that raises the bound and a watcher acts on it. %d is the cap actually applied.
const stepCapErrFormat = "delegate stopped at its step cap (%d steps) — returning what it has; " +
	"narrow the task or raise delegate-max-steps"

// usageTally is one Agent's running token accounting: the sums and the call count behind the
// cumulative fields of every domain.UsageEvent that Agent emits. It is deliberately a plain
// value struct on the Agent — the loop touches it only from the goroutine driving that Agent
// (the single-goroutine contract above; a fan-out's children are separate Agents with separate
// tallies), so it needs no lock, and holding no pointer keeps it copy-safe.
type usageTally struct {
	prompt     int
	completion int
	total      int
	cached     int
	calls      int
}

// record folds one completed upstream call's server-reported usage into the running totals and
// returns the UsageEvent to emit for it: the call's OWN counts in the fill fields, the UPDATED
// totals in the cumulative ones. TotalTokens is folded as the server reported it rather than
// recomputed from the two parts, so the totals stay consistent with the server's own arithmetic
// (a server may count cached or reasoning tokens the split does not show). cached — the share of
// the prompt the server answered from its prefix cache, 0 where it reports none — folds in the
// same way but is INFORMATIONAL throughout: no Budget, reducer or guard reads it, because a cached
// prompt token is still context the model reads; only the bill differs. A caller accounting
// for something other than a Turn's completion — Compaction — sets Maintenance on the returned
// event before emitting it.
//
// model and window are the emitting Agent's bound model and context window, stamped on the reading
// they produced. They are passed in rather than read off a member because the tally holds only its
// own arithmetic, and they are passed at all because a routed sub-agent runs on a model — and in a
// window — of its own (ADR 0045): without the stamp a Driver painting the child's fill has no way
// to say which model filled it, nor what it was full OF.
func (t *usageTally) record(base domain.EventBase, model string, window, prompt, completion, total, cached int) domain.UsageEvent {
	t.prompt += prompt
	t.completion += completion
	t.total += total
	t.cached += cached
	t.calls++
	return domain.UsageEvent{
		EventBase:                    base,
		PromptTokens:                 prompt,
		CompletionTokens:             completion,
		TotalTokens:                  total,
		CachedPromptTokens:           cached,
		Model:                        model,
		ContextWindow:                window,
		CumulativePromptTokens:       t.prompt,
		CumulativeCompletionTokens:   t.completion,
		CumulativeTotalTokens:        t.total,
		CumulativeCachedPromptTokens: t.cached,
		CumulativeCalls:              t.calls,
	}
}

// New constructs an Agent from cfg. It validates the configuration — including the
// Auto-mode/Confinement gate (ADR 0004) and the Mechanism ordering graph (ADR 0003,
// a constraint cycle is a startup error) — and returns an error rather than
// silently degrading a misconfigured surface. The root facade forwards apogee.New
// here, binding the real OpenAI-compatible provider client at cfg.Endpoint (P1.1)
// carrying cfg.APIKey — unconditionally, since an empty key sends no auth header — and,
// only when cfg.Inspector asks for it, the Inspector's wire observer (wireTap).
func New(cfg domain.Config) (*Agent, error) {
	opts, tap := armWireCapture(cfg)
	a, err := newAgent(cfg, provider.NewClient(cfg.Endpoint, cfg.Model,
		append(opts, provider.WithAPIKey(cfg.APIKey))...))
	if err != nil {
		return nil, err
	}
	a.ownsUpstream = true // this Agent dialled that client, so Close is the one that tears it down
	tap.bind(a)
	return a, nil
}

// Resume reconstructs an Agent from a prior Session snapshot. Config supplies the
// live delegates (Approver, Confiner, EventSink) and state roots again — only the
// serializable conversation state comes from snap. External connections (MCP,
// network) reconnect fresh; no server-side state is restored (ADR 0008).
func Resume(cfg domain.Config, snap domain.Session) (*Agent, error) {
	opts, tap := armWireCapture(cfg)
	a, err := resumeAgent(cfg, snap, provider.NewClient(cfg.Endpoint, cfg.Model,
		append(opts, provider.WithAPIKey(cfg.APIKey))...))
	if err != nil {
		return nil, err
	}
	a.ownsUpstream = true // as in New: a resumed session dials its own client and owns it
	tap.bind(a)
	return a, nil
}

// Close releases the Agent's resources. Because tools are stateless across Turns (ADR 0008),
// there is no live tool state to flush; what Close tears down is the provider client — but only
// the one this Agent OWNS, meaning the client New, Resume, SwitchUpstream or a routed spawn
// dialled for it (ownsUpstream). A sub-agent speaking over its PARENT's client closes nothing, so
// the session's connection survives every delegation that borrowed it, and an in-process
// Responder injected through the internal seam has no connection to close at all.
//
// The Consoles this Agent's run opened are the one live resource it DOES tear down (ADR 0059 §6),
// because Close is the only place that knows the run has ended: a delegated child closes the
// Consoles its own call id owns and leaves its parent's alone, and the top-level Agent closes
// every Console the engine holds. That makes this both the delegation-end site — subagent.go's
// deferred sub.Close() covers the normal, error, cancelled and faulted exits alike — and the
// engine-exit site (cmd/apogee's lateEngine.Close, internal/run).
//
// It is idempotent, and it does not end the Agent: closing a client only returns its idle
// sockets, so a later request dials again. The other live resources of a running session belong
// to the host that wired them rather than to this call: cmd/apogee closes the MCP connections
// alongside it, and the log sink is torn down by the TUI that opened it (internal/tui).
// The Library store this session's catalogue opened is flushed here too, between the two: the
// observations describe the run the Consoles were part of, and the flush must land before the
// socket goes back. It is a bounded flush (library.Store.Close), so a hung filesystem cannot hold
// up a shutdown.
func (a *Agent) Close() error {
	a.closeConsoles()
	return errors.Join(a.closeLibrary(), a.closeOwnedUpstream(a.upstream))
}

// closeLibrary flushes the Library store this Agent's build opened, so the observations recorded
// since the writer's last debounce window reach disk before the process ends. Nothing else in the
// engine touches the store's lifetime: recording is asynchronous by design (internal/library), and
// Close is the one call that knows the run is over.
//
// Ownership is the depth, not the pointer: a child at depth ≥ 1 holds the SAME shared instance its
// parent does, and its Close comes at the end of one delegation rather than at the end of the
// session. Under the store's flush-and-park Close an early flush would be harmless, but "the
// top-level Agent flushes; a delegate does not" is the rule that is simple to state and to test.
// A nil store — no armed row needed one — is the honest "nothing to flush" engine, not an error.
func (a *Agent) closeLibrary() error {
	if a.depth > 0 || a.library == nil {
		return nil
	}
	return a.library.Close()
}

// closeConsoles ends the Consoles this Agent's run is responsible for. Ownership is the Console's
// own Owner field, not the registry (one registry serves the whole tree): a child at depth ≥ 1
// closes the Consoles stamped with its own minted owner key, and the top-level Agent — whose key
// is empty, and which is the last thing standing — closes them all.
//
// Best-effort by contract, like every close on the way out: a process that resists teardown must
// not stop the Agent from closing its client, and there is no caller left to report it to.
func (a *Agent) closeConsoles() {
	if a.depth > 0 {
		a.consoles.CloseOwnedBy(a.consoleOwner)
		return
	}
	a.consoles.CloseAll()
}

// closeOwnedUpstream tears down up when this Agent owns it and it actually holds a connection —
// the io.Closer seam provider.Client satisfies and no in-process fake does. It is the single
// teardown path: Close applies it to the current Upstream, SwitchUpstream to the one it retires.
func (a *Agent) closeOwnedUpstream(up provider.Responder) error {
	if !a.ownsUpstream {
		return nil
	}
	closer, ok := up.(io.Closer)
	if !ok {
		return nil
	}
	return closer.Close()
}

// ----------------------------------------------------------------------------
// Stepping & Turns (ADR 0007)
// ----------------------------------------------------------------------------

// Submit enqueues user input to begin (or continue) an Exchange. It does not run
// the loop; the next Step/Run consumes it. Submitting mid-Exchange is an error.
//
// Submitting with no model bound is also an error (errNoModelBound): construction no longer
// requires Config.Model, so a host may run ahead of its Upstream discovery and bind the model
// later through Rebind (ADR 0024) — this is the gate that keeps a model-less request off the
// wire until it does.
func (a *Agent) Submit(in domain.UserInput) error {
	if a.cfg.Model == "" {
		return errNoModelBound
	}
	if a.pendingInput != nil || a.turns.inExchange {
		return domain.ErrInputPending
	}
	a.pendingInput = &in
	return nil
}

// Step advances the loop exactly one Turn and returns at a quiescent boundary — no
// in-flight stream, no in-flight tool call, conversation state fully serializable
// (ADR 0007). Streaming tokens and Approval prompts happen *inside* a Step (via the
// EventSink and Approver). Snapshot and Resume are valid only at the boundary Step
// returns at.
//
// Cancellation: cancelling ctx abandons the in-flight Upstream call or tool and
// returns at the next quiescent boundary with StepResult.Status == StatusCancelled
// and conversation state left serializable — never half-streamed (ADR 0007).
//
// Recovery: a panic in a tool or Mechanism is caught at that extension boundary,
// converted to an ErrorEvent, and the loop degrades to the quiescent boundary
// rather than unwinding into the host (ADR 0007 / ADR 0002). Step returns a non-nil
// error only for loop-level faults the Agent itself cannot localise.
func (a *Agent) Step(ctx context.Context) (domain.StepResult, error) { return a.step(ctx) }

// Run steps the loop until the Exchange completes (a final no-tool response),
// cancellation, or a loop-level error — a convenience wrapper over Step for hosts
// that do not need Turn-level control. It returns the StepResult of the Step that ended
// the loop (StatusExchangeComplete or StatusCancelled). Each intermediate Turn still
// returns at its own quiescent boundary, so a cancel delivered through ctx is honoured at
// the next boundary exactly as it is under Step. The bench drives Step directly.
//
// Run owns the between-Steps boundaries it loops over, so there is no seam to interject at:
// an embedder that wants to commit a mid-Exchange user message (Interject) drives Step
// itself and calls it between the Steps it makes.
//
// It is also the one place the DELEGATE STEP CAP is enforced (Agent.stepCap): a child agent that
// is still asking for tools after its capped number of Turns has its Exchange ended here rather
// than looping on, and the boundary returned carries StepCapped. A Step-driving host is not
// capped — it decides when to stop stepping itself — and neither is a top-level Agent, whose cap
// is always 0.
func (a *Agent) Run(ctx context.Context) (domain.StepResult, error) {
	for {
		res, err := a.step(ctx)
		if err != nil || res.Status != domain.StatusTurnComplete {
			return res, err
		}
		// The delegate step cap, counted and enforced HERE and nowhere else (stepCap): step() is
		// the Turn, and a Turn cannot know how many came before it. The count is taken AFTER the
		// Turn completed, which is after its tool calls were dispatched and their results
		// appended — so the history the parent snapshots ends on a complete tool round and stays
		// alternation-clean. A top-level Agent has no cap and never leaves this loop early.
		a.turns.exchangeTurns++
		if a.stepCap > 0 && a.turns.exchangeTurns >= a.stepCap {
			return a.endAtStepCap(res), nil
		}
	}
}

// endAtStepCap ends the open Exchange at the delegate step cap: it surfaces the bound to the human
// as one ErrorEvent (the child's own stream, at its Depth) and closes the Exchange through the
// exit table's endStepCapped row. The Exchange is NOT faulted — the Turns up to the cap did real
// work and the parent receives what they produced (runSubAgent) — so the returned boundary carries
// StepCapped rather than Faulted.
//
// last is the StepResult of the Turn that just completed, and it is what the returned boundary
// reports: no new Turn ran here, so the index is that Turn's and its wall time is reconstructed
// from the elapsed step() already measured for it rather than invented as ~0.
func (a *Agent) endAtStepCap(last domain.StepResult) domain.StepResult {
	a.cfg.Events.Emit(domain.ErrorEvent{
		EventBase: a.base(last.TurnIndex),
		Source:    "loop",
		Err:       fmt.Sprintf(stepCapErrFormat, a.stepCap),
	})
	return a.turns.end(&turnRun{turn: last.TurnIndex, start: time.Now().Add(-last.Elapsed)}, endStepCapped)
}

// AbortExchange discards an interrupted Exchange and returns the Agent to a clean quiescent
// boundary that accepts the next Submit. It rolls the conversation back to the boundary the
// Exchange began at — dropping the un-answered user message and any tool Turns committed so
// far — and clears inExchange. It is a no-op when no Exchange is open.
//
// It is the interactive host's counterpart to the Step-driven resume path. After a cancel,
// Step leaves the Exchange OPEN on purpose so a Step-driven host (the bench) re-Steps to
// re-attempt the Turn (see end()'s endCancelled row, turn.go). A host with no resume affordance — the TUI, where Esc
// means "stop, scrap it" — calls this instead, so the next /clear or message is accepted
// rather than rejected with ErrInputPending. Like Snapshot, it is valid only at a quiescent
// boundary: no worker may be driving the Agent when it is called (the host calls it only after
// the worker has returned its cancellation), preserving the single-goroutine contract.
func (a *Agent) AbortExchange() {
	if !a.turns.inExchange {
		return
	}
	a.conv.DropRange(a.exchangeBoundary(), a.conv.Len())
	// The Exchange is scrapped: closeExchange expires any deferred Response Action with it (F6) — a
	// mid-fan-out abort must not leave a stale remaining-items directive queued for the next
	// Exchange's request.
	a.turns.closeExchange()
	a.pendingInput = nil
}

// exchangeBoundary returns the conversation index the open Exchange began at — the rollback
// target AbortExchange drops from and the boundary the snapshot round-trips. It is the ONE
// reader seam over the cached a.turns.exchangeStart (ADR 0017 §2's recorded fallback): the boundary
// canNOT be re-derived as "the index of the last user message" (domain.CurrentExchange),
// because a mid-Exchange truncate_history fold drops the open Exchange's opening user message
// whenever the Exchange already holds keepLastTurns or more assistant messages — the inserted
// user-role gap note then anchors the derivation, and an abort would wrongly drop the note
// (pinned by TestExchangeStartRepairedAfterMidExchangeTruncation). So the cache stays —
// written at step()'s Exchange opening, re-anchored by the S2 repair after a history rewrite
// (loop.go), and restored by restoreState — and every consumer reads it here, so a future
// swap to the derivation has exactly one seam.
func (a *Agent) exchangeBoundary() int { return a.turns.exchangeStart }

// Mode reports the Agent's current autonomy mode. It reads the live mode under the lock, so a
// concurrent SetMode (Shift+Tab from the UI) is observed safely from the worker goroutine.
func (a *Agent) Mode() domain.Mode {
	a.modeMu.RLock()
	defer a.modeMu.RUnlock()
	return a.mode
}

// SetMode changes the autonomy mode for subsequent tool calls. It is safe to call from another
// goroutine while a Step runs: the tool menu (Plan filter) and the per-call Resolution both
// read the mode through Mode() under the same lock, so the change lands on the next read with no
// registry rebuild. A switch to Auto is safe even where fs-confinement is unavailable — the
// subprocess surface gates through Approval ("confine if you can, gate if you can't", ADR 0012),
// so no eligibility precheck is needed here.
func (a *Agent) SetMode(m domain.Mode) {
	a.modeMu.Lock()
	a.mode = m
	a.modeMu.Unlock()
}

// ConfineToWorkspace reports whether Auto's blast radius is currently fenced to the workspace
// (ADR 0012). It reads the live flag under the lock, so a concurrent SetConfineToWorkspace
// (/confine from the UI) is observed safely from the worker goroutine.
func (a *Agent) ConfineToWorkspace() bool {
	a.confineMu.RLock()
	defer a.confineMu.RUnlock()
	return a.confineToWorkspace
}

// SetConfineToWorkspace changes Auto's blast radius for subsequent tool calls: true (the
// default) fences confinable subprocess writes to the workspace and gates what cannot be
// fenced; false is the user's explicit "I am the sandbox" — Auto then runs every call
// unconfined with the user's full privileges, which is safe ONLY on a disposable machine
// (ADR 0012, as amended 2026-07-21). It is the engine half of /confine off|on; the Agent never
// flips it on its own initiative, and the ladder itself is untouched either way.
//
// It is safe to call from another goroutine (the UI) while a Step runs: the per-call Resolution
// reads the flag through ConfineToWorkspace() under the same lock, so the change lands on the
// NEXT tool call with no rebuild — exactly like SetMode. It changes only THIS Session; nothing
// is written to disk (persisting the host acknowledgement is the host's job, not the engine's).
//
// Sub-agents: a child spawned AFTER a toggle inherits the new value (newChildAgent reads the
// live flag at spawn, as it does for the mode); one already mid-flight keeps the value it was
// spawned with, so the toggle can neither loosen nor tighten a running delegation.
func (a *Agent) SetConfineToWorkspace(confine bool) {
	a.confineMu.Lock()
	a.confineToWorkspace = confine
	a.confineMu.Unlock()
}

// ScratchDir reports the session scratch directory the confinement box currently carries as an
// extra writable root ("" ⇒ none). It reads the live value under the lock, so a concurrent
// SetScratchDir (a session boundary in the host) is observed safely from the worker goroutine.
func (a *Agent) ScratchDir() string {
	a.scratchMu.RLock()
	defer a.scratchMu.RUnlock()
	return a.scratchDir
}

// guardExemptions returns the paths whose spellings the dangerous-action guard must not see
// for THIS agent's calls: today exactly the session's own scratch dir, read live so it follows
// a SetScratchDir move like every other per-call read. The confinement box already declares
// that dir writable (ADR 0056), so the `~/.apogee` forced look (ADR 0049 §4) would answer
// nothing there while prompting on every scratch-routed command. nil when there is no scratch
// dir — the guard as it was. Each agent, root or sub-agent, passes its OWN: the guard itself is
// shared read-only, so the exemption travels as a per-call argument and never as guard state.
func (a *Agent) guardExemptions() []string {
	dir := a.ScratchDir()
	if dir == "" {
		return nil
	}
	return []string{dir}
}

// SetScratchDir moves the session scratch directory for subsequent tool calls — the extra
// writable root ConfinementBox folds into WritablePaths (Config.ScratchDir documents the field).
// The host calls it at a SESSION boundary so the box handed to each call follows the ACTIVE
// session: /clear|/new mints a fresh session id and with it a fresh scratch dir, a resume adopts
// the restored session's. "" removes the root. The engine never mints, creates, or deletes the
// directory — lifecycle is the host's (ADR 0031: wire-silent, and a Driver that never calls this
// simply keeps its construction seed, so there is no new Driver obligation).
//
// It is safe to call from another goroutine while a Step runs: the per-call Resolution reads the
// value through ScratchDir() under the same lock, so the change lands on the NEXT tool call with
// no rebuild — exactly like SetMode and SetConfineToWorkspace. A sub-agent spawned AFTER the move
// inherits the new value (newChildAgent reads the live value at spawn); one already mid-flight
// keeps the dir it was spawned with.
func (a *Agent) SetScratchDir(dir string) {
	a.scratchMu.Lock()
	a.scratchDir = dir
	a.scratchMu.Unlock()
}

// UndoPreview describes what the human's next undo would put back: the top un-undone Exchange
// group, classified against the files as they are NOW (restore / delete / skip, with resolved
// paths — the disclosure the human authorises the revert from), together with the journal
// generation [Agent.UndoRevert] must quote back. It reports false when nothing is left to undo,
// which is also what an engine built without a journal answers.
//
// It is the engine half of `/undo` (ADR 0051). Like Snapshot it is valid at a QUIESCENT boundary
// only — between Steps, with no worker driving the loop — because a Step in flight is writing
// into the very group this describes; the Driver's idle-only command gate is the enforcement.
// It changes nothing on disk and does not move the journal, so previewing twice is free.
func (a *Agent) UndoPreview() (undo.Step, bool) {
	if a.journal == nil {
		return undo.Step{}, false
	}
	return a.journal.Preview()
}

// UndoRevert executes the top un-undone Exchange group — the one [Agent.UndoPreview] just
// described — and reports what it restored, removed, and skipped. generation is the stamp that
// preview carried: it refuses with [undo.ErrStaleGeneration], touching nothing, when the journal
// has moved since (ADR 0051, ratified call 7), so a human always confirms the step they were
// shown rather than whatever the journal happens to hold by the time they answer. A journal with
// nothing left to undo answers [undo.ErrNothingToUndo].
//
// It carries UndoPreview's boundary rule for the same reason, and more sharply: the generation is
// read and the revert runs as two calls, so only a quiescent engine makes the pair atomic. Paths
// whose current content no longer matches what the agent wrote are SKIPPED with a reason rather
// than overwritten — the human's own edit outranks the undo — and the group is popped either way,
// so a repeated undo walks further back instead of retrying a skip.
func (a *Agent) UndoRevert(generation uint64) (undo.Report, error) {
	if a.journal == nil {
		return undo.Report{}, undo.ErrNothingToUndo
	}
	if live := a.journal.Generation(); live != generation {
		return undo.Report{}, fmt.Errorf("%w: previewed at generation %d, journal is at %d",
			undo.ErrStaleGeneration, generation, live)
	}
	return a.journal.Revert()
}

// SetBypass switches Bypass — Mechanisms off, structure on (ADR 0006) — on or off for the rest
// of the session. It takes effect at the NEXT hook fire: the gate is consulted per catalogued
// Mechanism per hook point (skipUnderBypass, via skipMechanism), so nothing is rebuilt and a
// Turn already mid-flight starts honouring the new value at its next hook point. Off-ramp
// Mechanisms and the structural machinery (Budget, Compaction, the guardrails) are unaffected
// either way — Bypass has never governed them.
//
// It is safe to call from another goroutine (the settings surface) while a Step runs, like
// SetMode. A sub-agent spawned AFTER the switch inherits the new value (newChildAgent reads the
// live flag at spawn); one already mid-flight keeps what it was spawned with.
func (a *Agent) SetBypass(enabled bool) {
	a.bypassMu.Lock()
	a.bypass = enabled
	a.bypassMu.Unlock()
}

// bypassEnabled reports the live Bypass flag under the lock, so the worker goroutine's per-hook
// read is race-free against a concurrent SetBypass. It is the ONE read seam for the flag: cfg.Bypass
// is only the construction seed.
func (a *Agent) bypassEnabled() bool {
	a.bypassMu.RLock()
	defer a.bypassMu.RUnlock()
	return a.bypass
}

// SetCompactionEnabled switches the automatic, budget-driven Compaction trigger (the `auto-compact`
// key) on or off for the rest of the session. It takes effect at the next Exchange boundary: the
// gate is consulted per fold decision (shouldAutoCompact) and by the overflow rescue
// (emergencyFold), so switching it off stops the next automatic fold and switching it on arms it
// again with no rebuild. The on-demand /compact is unaffected — it has never consulted this gate.
//
// It is safe to call from another goroutine (the settings surface) while a Step runs, like SetMode.
// A sub-agent spawned AFTER the switch inherits the new value at spawn.
func (a *Agent) SetCompactionEnabled(enabled bool) {
	a.compactionMu.Lock()
	a.compaction = enabled
	a.compactionMu.Unlock()
}

// compactionEnabled reports the live auto-Compaction gate under the lock, so the fold decision is
// race-free against a concurrent SetCompactionEnabled. cfg.Context.CompactionEnabled is only the
// construction seed.
func (a *Agent) compactionEnabled() bool {
	a.compactionMu.RLock()
	defer a.compactionMu.RUnlock()
	return a.compaction
}

// SetContextFiles replaces the workspace context-file names folded into the standing system
// content. enable false — or an empty list, the second spelling of "off" — installs no names at
// all, exactly as the composition root's resolution collapses the two spellings into one value.
//
// It deliberately does NOT re-read the workspace: the cache moves only at a session boundary
// (construction, ClearContext, RestoreSession — reloadContextFiles), so this session keeps seeding
// byte-identical content and the server's prefix KV cache survives it. The new names are picked up
// by the NEXT /clear or restored session, which is the boundary the host tells the user about.
//
// It is safe to call from another goroutine (the settings surface) while a Step runs, like SetMode:
// the names are copied in and the slice is never mutated in place, so the reload reads a stable
// list. A name that escapes the workspace is refused by the read fence (readContextFile) and
// reported as unreadable, never folded in — the construction-time name gate is the host's to apply
// before calling this.
func (a *Agent) SetContextFiles(enable bool, names []string) {
	var live []string
	if enable && len(names) > 0 {
		live = slices.Clone(names)
	}
	a.contextFilesMu.Lock()
	a.contextFileNames = live
	a.contextFilesMu.Unlock()
}

// SetParallelAgents moves the Parallel agents cap — the width of a depth-0 sub-agent fan-out (ADR
// 0039) — for the rest of the session, or until the session moves to another server. Anything < 2
// means serial, which is the floor this loop has always run at, so a host that resolves nothing
// leaves the engine exactly where it was.
//
// It is the cap the SESSION server advertises, which is the width of every fan-out that actually
// runs there: while a Delegation target is latched the children run somewhere else and the width
// comes from that server instead (ADR 0045 §5, delegationCap). Hosts push both regardless — the
// two monitors observe two servers — and the one that governs is decided per dispatch.
//
// It is the SERVER's number rather than the session's: the host resolves it from the bound entry's
// `parallel-agents:` pin, else that server's advertised slot count, and re-states it whenever the
// session moves or a heartbeat observes a different one. That is why it belongs to the anytime-safe
// class alongside SetMode: the beat that observes a restarted server lands on its own goroutine,
// and the cap is read once per dispatch rather than held across one, so a change lands on the next
// fan-out and never mid-flight.
func (a *Agent) SetParallelAgents(width int) {
	a.parallelAgentsMu.Lock()
	a.parallelAgents = width
	a.parallelAgentsMu.Unlock()
}

// parallelAgentsCap reports the live fan-out width under the lock, so the dispatch read is race-free
// against a concurrent SetParallelAgents. It is the ONE read seam for the SESSION server's cap:
// cfg.ParallelAgents is only the construction seed. Which cap a dispatch then GOVERNS by — this one
// or a latched Delegation target's — is delegationCap's single choice (dispatch.go, ADR 0045 §5).
func (a *Agent) parallelAgentsCap() int {
	a.parallelAgentsMu.RLock()
	defer a.parallelAgentsMu.RUnlock()
	return a.parallelAgents
}

// contextFileList reports the live context-file names under the lock. The returned slice is never
// mutated in place — SetContextFiles installs a fresh one — so the caller may read it after the
// lock is dropped. cfg.ContextFiles is only the construction seed.
func (a *Agent) contextFileList() []string {
	a.contextFilesMu.RLock()
	defer a.contextFilesMu.RUnlock()
	return a.contextFileNames
}

// SetEffortOverride states this SESSION's Thinking effort — the level layered ABOVE the bound
// model's profile (`model-profiles:` → `thinking.effort:`), and the engine half of /effort. The
// eight levels (domain.EffortOff … domain.EffortMax) each stand until another call moves them;
// the ZERO value clears the override and hands the resolution back to the profile. Nothing is
// persisted: an override is what a user asked for NOW, so the next session starts from the
// profile again (ADR 0050).
//
// It is configuration rather than a Mechanism, so it holds under Bypass — Bypass turns the
// Mechanisms off, while how hard the model thinks is a dial ON the request, the same class as the
// reply ceiling (ADR 0046). And it is an engine door rather than TUI-local state so that any
// Driver reaches it — a bench sweeping the levels, a daemon taking it off an API (ADR 0031).
//
// It is safe to call from another goroutine while a Step runs, like SetMode: the wire projection
// reads it once per request under the same lock, so a change lands on the NEXT request and never
// mid-flight. It is PRIMARY-loop state: a delegated child is a separate Agent built from the
// parent's Config, which carries no override, so a sub-agent thinks at ITS OWN profile's effort
// and the session override never leaks into a delegation.
func (a *Agent) SetEffortOverride(e domain.ThinkingEffort) {
	a.effortMu.Lock()
	a.effortOverride = e
	a.effortMu.Unlock()
}

// ThinkingEffort reports the two layers behind the effort a request carries: this session's
// override (SetEffortOverride) and the bound model profile's own setting, each "" when unset. A
// Driver showing the resolution — bare /effort — needs BOTH, because a level means something
// different when the user asked for it than when the profile did; the EFFECTIVE value is the
// override when non-empty, else the profile's, the one order the projection resolves in.
//
// The profile half is read without a lock, which is safe for the same reason every other cfg read
// is: cfg.Profile moves only through the idle-only doors (SetProfile, Rebind) the host itself
// drives, so it is never written while a Step is running (ADR 0011's idle-only engine-call class).
func (a *Agent) ThinkingEffort() (override, profile domain.ThinkingEffort) {
	return a.effortOverrideValue(), a.cfg.Profile.Thinking.Effort
}

// effortOverrideValue reports the live session override under the lock, so the per-request read is
// race-free against a concurrent SetEffortOverride. It is the ONE read seam for the field — there
// is no cfg counterpart to seed it from.
func (a *Agent) effortOverrideValue() domain.ThinkingEffort {
	a.effortMu.RLock()
	defer a.effortMu.RUnlock()
	return a.effortOverride
}

// ----------------------------------------------------------------------------
// Sessions (ADR 0001 — snapshot/resume is the user feature; the bench composes fork)
// ----------------------------------------------------------------------------

// Snapshot captures the Agent's conversation state at the current quiescent
// boundary as a copyable, serializable value (ADR 0001/0007). It is valid only at a
// boundary (between Steps). Apogee exposes snapshot/resume; it exposes no fork — the
// bench composes forking by deep-copying a Session and the sandbox directory.
//
// Domain owns the Session envelope and its version; the engine owns the opaque State
// payload, so Snapshot serializes the engine's loop state (conversation + turnIndex +
// inExchange + pending input — internal/agent/state.go) into it (ADR 0010).
func (a *Agent) Snapshot() (domain.Session, error) {
	state, err := a.encodeState()
	if err != nil {
		return domain.Session{}, err
	}
	return domain.Session{Version: domain.SessionVersion, State: state}, nil
}

// ----------------------------------------------------------------------------
// Context controls (/clear, /compact — the chat mini-language seams)
// ----------------------------------------------------------------------------

// ClearContext drops the model-facing conversation history while preserving the rest of
// the loop state — the Turn counter keeps advancing, allow-for-session approvals and the
// autonomy mode survive, and the visible TUI transcript (a separate structure the host
// owns) is untouched. It is the engine half of the /clear command: the model forgets
// prior turns; the human keeps their scrollback. Valid only at a quiescent boundary;
// calling it mid-Exchange is refused (ErrInputPending) so a half-streamed Turn is never
// orphaned. The Agent stays snapshot-safe after it returns.
//
// It is also a session boundary, so the workspace context files are re-read here: the new
// session speaks from whatever the repo's AGENTS.md says NOW. A refused call changes nothing.
//
// It is a boundary for the Consoles too, and that is the ONE place their lifetime diverges from
// the undo journal's: the journal survives /clear so `/undo` can still reach the writes of the
// conversation just forgotten, while every Console is closed here (ADR 0059 §1). A Console is a
// live process the model steers by id, and the ids live in the history this call drops — leaving
// four shells running that nothing in the new session can name is exactly the forgotten-process
// leak the cap exists to prevent.
func (a *Agent) ClearContext() error {
	if a.turns.inExchange {
		return domain.ErrInputPending
	}
	a.consoles.CloseAll()
	a.reloadContextFiles()
	a.conv = *domain.NewConversation(nil)
	return nil
}

// RestoreSession swaps a prior Session snapshot into this LIVE Agent, replacing its conversation
// and loop counters (turn index, in-Exchange flag, Exchange boundary, pending input) without a
// rebuild — so the resolved tools, Mechanisms, and MCP wiring stand. It is the in-TUI resume
// primitive: the live-restore counterpart to construction-time Resume, letting the host switch
// sessions without relaunching (ADR 0001's snapshot/resume feature, live variant).
//
// Like ClearContext it is valid only at a quiescent boundary with no worker driving the Agent
// (the TUI calls it at idle); calling it mid-Exchange is refused (ErrInputPending) so a
// half-streamed Turn is never orphaned. A snapshot newer than this build understands is refused
// (ErrSessionVersion) and a malformed payload returns a decode error; in EITHER failure the live
// conversation is left untouched — restoreSnapshot decodes into a temporary and swaps only on
// full success. It does NOT touch the allow-for-session approval cache, the autonomy mode, or the
// confinement flag: those are live host state re-confirmed per ADR 0008, not part of the Session.
//
// It DOES reset two things the outgoing conversation owned, exactly as ClearContext does at the
// /new boundary: every Console is closed (ADR 0059 §1 — a Console is live host state the model
// steers by id, and the ids live in the history the swap drops, so leaving the shells running
// would be the same unnameable-process leak the cap exists to prevent), and the cumulative usage
// tally is zeroed (the sums belong to the conversation that just left; a Driver restoring a
// record seeds its own accounting from that record's stored totals). Both sit AFTER the swap:
// a REFUSED restore leaves the Consoles running and the tally standing.
//
// A successful restore starts a new session, so the workspace context files are re-read: the
// resolved-live posture of ADR 0023 §6 — the standing content is not serialized, so a resumed
// session speaks from the CURRENT files, not the ones its snapshot was taken under. A REFUSED
// restore (mid-Exchange, or a corrupt/future-version snapshot) leaves the cache untouched
// along with the conversation.
func (a *Agent) RestoreSession(snap domain.Session) error {
	if a.turns.inExchange {
		return domain.ErrInputPending
	}
	if err := a.restoreSnapshot(snap); err != nil {
		return err
	}
	a.consoles.CloseAll()
	a.usage = usageTally{}
	a.reloadContextFiles()
	return nil
}

// InExchange reports whether a multi-Turn Exchange is currently open (mid-flight). It is a
// boundary-only read: the host calls it after a RestoreSession (or at startup after Resume) to
// detect a Session snapshotted mid-task, so it can drive the step-only continue path rather than
// waiting on a Submit the open Exchange would reject. It reads a.turns.inExchange directly and,
// like Snapshot, is meant for use only at a quiescent boundary with no worker driving the Agent.
func (a *Agent) InExchange() bool { return a.turns.inExchange }

// LastFault reports the text of the most recent loop-level fault this Agent surfaced as an
// ErrorEvent — the very sentence the human already read (emitLoopFault) — and "" when no
// loop-level fault has been surfaced at all. A fault ends the Exchange, so after Run returns
// with domain.StepResult.Faulted set this is the reason that Turn was ABANDONED; an Exchange
// abandoned with no ErrorEvent behind it (a recovered hook panic) leaves it empty, and a caller
// must render whatever it says without one rather than assert a cause.
//
// It exists so a Driver learns the reason as DATA rather than by tapping the event stream (ADR
// 0031): internal/run copies it onto run.Result so an unattended caller — `apogee headless`, the
// daemon — can say WHY its answer is not an answer. Like InExchange it is a boundary-only read,
// meant for use after Run has returned with no worker driving the Agent.
func (a *Agent) LastFault() string { return a.lastFault }

// Compact (the /compact command's engine half) lives in compact.go alongside its provider
// adapter and the generative reducer it drives (internal/context.Compact).
