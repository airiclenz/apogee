package agent

import (
	"context"
	"sync"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/processing"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/security"
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
type Agent struct {
	cfg      domain.Config
	upstream provider.Responder        // provider seam (Decision C): fake in tests, real HTTP via New
	registry *domain.MechanismRegistry // catalogued + experimental hooks driving the loop
	tools    *domain.ToolRegistry      // resolved tool set (Config.Tools, or the default registry)
	guards   security.Guards           // always-on, mode-independent guardrails (dangerous-action + circuit-breaker + audit, D6)

	// textParser and stripper are the parse-seam collaborators selected from cfg.Profile at
	// construction (processing.ParserFor): the text-format tool-call parser recovers a call from
	// the visible content of a non-native model, and stripper lifts the inline thinking/harmony
	// channel out of that content. A native, no-inline-thinking profile (the zero value) yields
	// no-op parsers, so the content path is byte-identical to the pre-profile loop.
	textParser processing.ToolCallParser
	stripper   processing.ContentStripper

	// modeMu guards mode — the ONE field shared across goroutines, the deliberate exception to
	// the single-goroutine contract above. The UI cycles the autonomy mode (Shift+Tab → SetMode)
	// while the worker goroutine reads it during dispatch (Mode() in toolMenu / the Resolution);
	// the RWMutex makes that overlap race-free. cfg.Mode stays the immutable construction seed.
	modeMu sync.RWMutex
	mode   domain.Mode // live autonomy mode; seeded from cfg.Mode at construction, swappable via SetMode

	// liveMode, when non-nil, is a sub-agent's read-only view of its PARENT's live mode: the
	// parent's modeMu-guarded Mode accessor, captured at spawn (ADR 0013). The per-call
	// Resolution takes the TIGHTER of this and the child's own spawn mode, so a parent that
	// tightens mid-delegation (Shift+Tab down from Auto to Plan) gates/refuses the still-running
	// child's next call, while a parent loosening can never loosen it. It is a closure over the
	// accessor — NOT the shared mode field/mutex — so the child observes the parent's mode
	// race-free but cannot mutate it. nil for a top-level Agent, which then behaves exactly as
	// before (its own mode governs).
	liveMode func() domain.Mode

	conv          domain.Conversation // serializable conversation state (ADR 0001)
	pendingInput  *domain.UserInput   // queued by Submit, consumed by the next Step
	inExchange    bool                // true between Submit and the Step that completes the Exchange
	exchangeStart int                 // conv length before this Exchange's first user message — the boundary AbortExchange rolls back to
	turnIndex     int                 // 0-based index of the next Turn
	approved      map[string]bool     // tools the human allowed for the rest of this Session
	depth         int                 // sub-agent nesting level: 0 = top-level; a sub-agent runs at parent+1 (ADR 0013)
}

// New constructs an Agent from cfg. It validates the configuration — including the
// Auto-mode/Confinement gate (ADR 0004) and the Mechanism ordering graph (ADR 0003,
// a constraint cycle is a startup error) — and returns an error rather than
// silently degrading a misconfigured surface. The root facade forwards apogee.New
// here, binding the real OpenAI-compatible provider client at cfg.Endpoint (P1.1).
func New(cfg domain.Config) (*Agent, error) {
	return newAgent(cfg, provider.NewClient(cfg.Endpoint, cfg.Model))
}

// Resume reconstructs an Agent from a prior Session snapshot. Config supplies the
// live delegates (Approver, Confiner, EventSink) and state roots again — only the
// serializable conversation state comes from snap. External connections (MCP,
// network) reconnect fresh; no server-side state is restored (ADR 0008).
func Resume(cfg domain.Config, snap domain.Session) (*Agent, error) {
	return resumeAgent(cfg, snap, provider.NewClient(cfg.Endpoint, cfg.Model))
}

// Close releases the Agent's resources. Because tools are stateless across Turns
// (ADR 0008), there is no live tool state to flush — Close tears down the provider
// client, any MCP connections, and the log sink. The Phase-0 slice holds no such live
// resources (the responder is in-process and hermetic), so Close is a no-op today; it
// exists now so embedders write the correct lifecycle before Phase 1 adds real teardown.
func (a *Agent) Close() error { return nil }

// ----------------------------------------------------------------------------
// Stepping & Turns (ADR 0007)
// ----------------------------------------------------------------------------

// Submit enqueues user input to begin (or continue) an Exchange. It does not run
// the loop; the next Step/Run consumes it. Submitting mid-Exchange is an error.
func (a *Agent) Submit(in domain.UserInput) error {
	if a.pendingInput != nil || a.inExchange {
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
func (a *Agent) Run(ctx context.Context) (domain.StepResult, error) {
	for {
		res, err := a.step(ctx)
		if err != nil || res.Status != domain.StatusTurnComplete {
			return res, err
		}
	}
}

// AbortExchange discards an interrupted Exchange and returns the Agent to a clean quiescent
// boundary that accepts the next Submit. It rolls the conversation back to the boundary the
// Exchange began at — dropping the un-answered user message and any tool Turns committed so
// far — and clears inExchange. It is a no-op when no Exchange is open.
//
// It is the interactive host's counterpart to the Step-driven resume path. After a cancel,
// Step leaves the Exchange OPEN on purpose so a Step-driven host (the bench) re-Steps to
// re-attempt the Turn (see cancelTurn). A host with no resume affordance — the TUI, where Esc
// means "stop, scrap it" — calls this instead, so the next /clear or message is accepted
// rather than rejected with ErrInputPending. Like Snapshot, it is valid only at a quiescent
// boundary: no worker may be driving the Agent when it is called (the host calls it only after
// the worker has returned its cancellation), preserving the single-goroutine contract.
func (a *Agent) AbortExchange() {
	if !a.inExchange {
		return
	}
	a.conv.DropRange(a.exchangeStart, a.conv.Len())
	a.inExchange = false
	a.pendingInput = nil
}

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
func (a *Agent) ClearContext() error {
	if a.inExchange {
		return domain.ErrInputPending
	}
	a.conv = *domain.NewConversation(nil)
	return nil
}

// Compact (the /compact command's engine half) lives in compact.go alongside its provider
// adapter and the generative reducer it drives (internal/context.Compact).
