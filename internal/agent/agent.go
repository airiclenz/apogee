package agent

import (
	"context"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
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

	conv         domain.Conversation // serializable conversation state (ADR 0001)
	pendingInput *domain.UserInput   // queued by Submit, consumed by the next Step
	inExchange   bool                // true between Submit and the Step that completes the Exchange
	turnIndex    int                 // 0-based index of the next Turn
	approved     map[string]bool     // tools the human allowed for the rest of this Session
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

// Mode reports the Agent's current Agent mode.
func (a *Agent) Mode() domain.Mode { return a.cfg.Mode }

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
