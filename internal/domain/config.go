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
	Asker    Asker     // free-text Q&A delegate for the ask_user tool; nil ⇒ ask_user is not registered (P3.11)
	Confiner Confiner  // nil ⇒ no confinement ⇒ Auto is refused (ADR 0004)
	Events   EventSink // where typed Events are pushed; required

	// Extension points. nil ⇒ the built-in defaults.
	Tools      *ToolRegistry      // open extension point (ADR 0002)
	Mechanisms *MechanismRegistry // curated catalogue + bench experimental hooks (ADR 0002/0003)

	// Skills resolves the user's attached skill IDs (UserInput.SkillIDs) to their injectable
	// bodies; nil ⇒ no skills are wired and any attached ID is reported and dropped. It is an
	// interface defined here (not the concrete internal/skills catalog) so the loop fulfils the
	// SkillIDs seam without domain importing skills — the dependency flows toward domain (ADR
	// 0010). The host (cmd/apogee) loads the catalog and injects it.
	Skills SkillResolver

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

	// WebSearchEndpoint is the search backend the web_search tool sends a query to
	// (P3.11). DEFAULT-ON: empty ⇒ the tool falls back to its built-in DuckDuckGo
	// provider (no API key needed); the sentinel "off" disables it (a graceful "web
	// search is disabled", never a crash). The host folds a configured endpoint in from
	// config.yaml.
	WebSearchEndpoint string

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

// modeLadder is the autonomy privilege ladder in cycle order (least to most autonomous);
// the cycle wraps Auto → Plan. It is the single source of truth for Shift+Tab mode cycling.
var modeLadder = []Mode{ModePlan, ModeAskBefore, ModeAllowEdits, ModeAuto}

// NextMode returns the mode one rung up the privilege ladder, wrapping Auto back to Plan.
// An unknown or empty mode starts the cycle at Plan (the safest rung), so a caller can never
// get stuck off-ladder.
func NextMode(cur Mode) Mode {
	for i, m := range modeLadder {
		if m == cur {
			return modeLadder[(i+1)%len(modeLadder)]
		}
	}
	return ModePlan
}

// ----------------------------------------------------------------------------
// Stepping & Turns (ADR 0007)
// ----------------------------------------------------------------------------

// UserInput is one user message into an Exchange: free text plus optional file
// references the loop resolves into context, plus reserved skill references. Stays a value
// (no live handles) so it snapshots cleanly.
//
// FileRefs (@file tokens parsed from the chat input) are resolved at Step time — the loop
// reads each within the workspace fence and prepends its content to the user message.
// SkillIDs are the skills the user attached in chat (the /skill command); the loop resolves
// each through Config.Skills and prepends its body to the user message for that one turn. The
// refs round-trip through a snapshot, so a resumed session re-resolves them.
type UserInput struct {
	Text     string
	FileRefs []string
	SkillIDs []string `json:",omitempty"`
}

// SkillResolver maps attached skill IDs to their injectable form. It is implemented by the
// skills catalog (internal/skills) and injected via Config.Skills; the interface lives in
// domain so the loop can fulfil the UserInput.SkillIDs seam without importing the skills
// package (ADR 0010 — the dependency flows toward domain).
type SkillResolver interface {
	// ResolveSkills returns the resolved skills for ids, in the given order, skipping any
	// unknown ID. The caller compares the result against what it requested to report a miss,
	// so a typo in an attached ID is never silently swallowed.
	ResolveSkills(ids []string) []ResolvedSkill
}

// ResolvedSkill is one attached skill reduced to the fields the loop injects: the ID and
// DisplayName label the prepended block, and Body is the skill's instruction text scoped to
// the turn it was attached to.
type ResolvedSkill struct {
	ID          string
	DisplayName string
	Body        string
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
