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
	Mode   Mode // Plan / Ask-Before / Auto
	Bypass bool // ADR 0006: Mechanisms off, structure on (the hard-constraint floor)

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
	// ModeAskBefore requires an Approval for every tool call.
	ModeAskBefore Mode = "ask-before"
	// ModeAuto runs tool calls without per-call approval. Requires Confinement
	// (ADR 0004); a tool that cannot be confined still gates through Approval.
	ModeAuto Mode = "auto"
)

// ----------------------------------------------------------------------------
// Stepping & Turns (ADR 0007)
// ----------------------------------------------------------------------------

// UserInput is one user message into an Exchange: free text plus optional file
// references the context builder resolves. Stays a value (no live handles) so it
// snapshots cleanly.
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
