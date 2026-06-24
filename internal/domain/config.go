package domain

import "time"

// ----------------------------------------------------------------------------
// Construction surface (ADR 0001)
// ----------------------------------------------------------------------------

// Config is the full construction surface. It carries the Upstream target, the
// autonomy posture, the host-supplied delegates, the extension registries, and the
// injected state roots. A zero Config is not valid; Endpoint, Model, and Events are
// the minimum. A struct (not functional options) because every field is a
// deliberate, reviewable seam and ADR 0001 speaks of state "injected via Config".
type Config struct {
	// Upstream — the local OpenAI-compatible LLM server (CONTEXT: Upstream).
	Endpoint string
	Model    string

	// Autonomy.
	Mode   Mode // Plan / Ask-Before / Allow-Edits / Auto (the privilege ladder)
	Bypass bool // ADR 0006: Mechanisms off, structure on (the hard-constraint floor)

	// ConfineToWorkspace tunes Auto's blast radius (ADR 0012); meaningful only in Auto.
	// true (the default) fences subprocess writes to the workspace under OS confinement
	// (network open, MCP gated); false ("I am the sandbox") runs Auto unconfined, safe
	// only inside a VM. It is loaded from the GLOBAL config only (a project config cannot
	// loosen it — the hostile-repo footgun is closed). The host sets it; the loop reads it
	// in the dispatch disposition.
	ConfineToWorkspace bool

	// ConfineWritablePaths and ConfineNetworkAllow extend the confinement box beyond the
	// workspace root (confinement-execution-contract §7): the toolchain cache/temp dirs a
	// confined `go build`/`pip` needs to write, and the per-project network tightening
	// list. The host probes/configures these and folds them into Config; the loop confines
	// a subprocess to WorkspaceDir ∪ ConfineWritablePaths with ConfineNetworkAllow as the
	// box's NetworkAllow. Empty NetworkAllow leaves the network open (the ADR 0012 default).
	ConfineWritablePaths []string
	ConfineNetworkAllow  []string

	// Host-supplied delegates. The host (TUI / bench / embedder) owns these.
	Approver Approver  // the human-in-the-loop gate; required unless Mode==Plan
	Confiner Confiner  // nil ⇒ no confinement ⇒ Auto is refused (ADR 0004)
	Events   EventSink // where typed Events are pushed; required

	// Extension points. nil ⇒ the built-in defaults.
	Tools      *ToolRegistry      // open extension point (ADR 0002)
	Mechanisms *MechanismRegistry // curated catalogue + bench experimental hooks (ADR 0002/0003)

	// Injected state roots — no implicit ~/.apogee (ADR 0001). The bench points
	// these at ephemeral dirs so sim runs never touch the production Library.
	LibraryDir  string
	SessionsDir string
	ConfigDir   string

	// WorkspaceDir is the sandbox root the built-in file tools are scoped to when
	// Config.Tools is nil. Empty ⇒ no default tools are wired (the host must inject
	// Config.Tools to give the Agent any tools). The bench points it at an ephemeral
	// workspace so a file-edit task never escapes its sandbox (ADR 0001 isolation).
	WorkspaceDir string

	// ExternalEffects is the single injectable boundary for non-forkable effects
	// (network, MCP). nil ⇒ live. The bench injects a deterministic stub for v1;
	// record/replay slots in behind the same interface later (ADR 0008).
	ExternalEffects ExternalEffects

	// Budget / Compaction knobs (context/) are structural and load-bearing — they
	// run even under Bypass. Defaults are sane; overrides are advanced.
	Context ContextConfig
}

// ContextConfig governs the structural context reducers — Budget and Compaction —
// which are NOT Mechanisms and stay on under Bypass (CONTEXT: Budget, Compaction).
type ContextConfig struct {
	MaxContextTokens  int // 0 ⇒ discover from the model
	ResponseReserve   int
	CompactionEnabled bool // generative summarisation; default true
}

// Mode is the autonomy level governing whether tool calls need human approval
// (CONTEXT: Agent mode). It is orthogonal to Config.Bypass.
type Mode string

const (
	// ModePlan is read-only: no writes, no command execution.
	ModePlan Mode = "plan"
	// ModeAskBefore requires an Approval for every write, command, and external reach
	// (a harmless read runs free).
	ModeAskBefore Mode = "ask-before"
	// ModeAllowEdits auto-approves Apogee's own workspace-scoped writes (path-safety-
	// bounded); shell/exec, network, MCP, third-party in-process tools, and any
	// out-of-workspace write still gate. It needs NO Confinement — path-safety bounds
	// the auto-approved writes and the human backstops the unbounded surface — so it is
	// identical on every OS (ADR 0012).
	ModeAllowEdits Mode = "allow-edits"
	// ModeAuto runs unbounded tool calls without per-call approval, tuned by
	// Config.ConfineToWorkspace (ADR 0012). With confinement on (the default), the
	// subprocess surface runs OS-confined to the workspace; an unfenceable tool (MCP) or
	// an out-of-workspace Apogee write still gates through Approval; if fs-confinement is
	// unavailable, the subprocess surface gates ("confine if you can, gate if you can't").
	ModeAuto Mode = "auto"
)

// ----------------------------------------------------------------------------
// Stepping & Turns (ADR 0007)
// ----------------------------------------------------------------------------

// UserInput is one user message into an Exchange: free text plus optional file
// references the context builder resolves. Stays a value (no live handles) so it
// snapshots cleanly.
//
// Phase 1 consumes Text only. FileRefs are carried and snapshotted but not yet resolved
// into context — turning references into budgeted file content is a context-builder
// concern (TDD §8 #8) deferred past Phase 1. Until it lands, supplying FileRefs is
// surfaced as a loop ErrorEvent rather than silently ignored.
type UserInput struct {
	Text     string
	FileRefs []string
}

// StepResult reports the outcome of one Step at the quiescent boundary.
type StepResult struct {
	Status    StepStatus
	TurnIndex int           // 0-based index of the Turn just completed
	Elapsed   time.Duration // wall time for this Turn
}

// StepStatus is the disposition of a completed Step. The set is open (additively
// extensible — treat unknown values defensively).
type StepStatus string

const (
	// StatusTurnComplete: the Turn finished and more Turns are pending (the model
	// requested tools; the loop will continue on the next Step).
	StatusTurnComplete StepStatus = "turn-complete"
	// StatusExchangeComplete: the model produced a final no-tool response; the
	// Agent now awaits the next Submit.
	StatusExchangeComplete StepStatus = "exchange-complete"
	// StatusCancelled: ctx was cancelled; state is serializable, resume is valid.
	StatusCancelled StepStatus = "cancelled"
)
