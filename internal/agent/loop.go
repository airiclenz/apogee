package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// experimentalMechanismID is the synthetic MechanismID a descriptor-less experimental
// hook fires under (ADR 0002 — no descriptor, no self-regulation). It exists only so
// MechanismFiredEvent.Mechanism is never empty for bench attribution.
const experimentalMechanismID domain.MechanismID = "experimental"

var (
	errMissingEvents   = errors.New("apogee: Config.Events is required")
	errMissingEndpoint = errors.New("apogee: Config.Endpoint is required")
	errMissingModel    = errors.New("apogee: Config.Model is required")
	// errHookPanicked is an internal signal — never returned to the host — that a
	// panic was recovered at an extension boundary and reported as an ErrorEvent, so
	// Step can degrade to a clean quiescent boundary instead of unwinding.
	errHookPanicked = errors.New("apogee: extension boundary recovered a panic")
)

// newAgent validates cfg and constructs a ready-to-Step Agent bound to up. The public
// New delegates here with the Phase-0 placeholder responder; white-box tests inject a
// deterministic fake. Validation order is deliberate: required fields, then the
// ordering-cycle gate (ADR 0003), then the Auto/Confinement gate (ADR 0004).
func newAgent(cfg domain.Config, up provider.Responder) (*Agent, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	registry := cfg.Mechanisms
	if registry == nil {
		registry = domain.NewMechanismRegistry()
	}
	if err := registry.ValidateOrdering(); err != nil {
		return nil, err
	}

	if cfg.Mode == domain.ModeAuto && !autoEligible(cfg.Confiner) {
		return nil, domain.ErrAutoUnavailable
	}

	return &Agent{cfg: cfg, upstream: up, registry: registry}, nil
}

// resumeAgent rebuilds an Agent from snap, rejecting a snapshot newer than this build
// understands (ErrSessionVersion) before restoring its conversation. cfg supplies the
// live delegates afresh (ADR 0001); only the serializable conversation comes from snap.
func resumeAgent(cfg domain.Config, snap domain.Session, up provider.Responder) (*Agent, error) {
	if snap.Version > domain.SessionVersion {
		return nil, domain.ErrSessionVersion
	}
	a, err := newAgent(cfg, up)
	if err != nil {
		return nil, err
	}
	conv, err := decodeConversation(snap.State)
	if err != nil {
		return nil, err
	}
	a.conv = conv
	return a, nil
}

// validateConfig enforces the minimum construction surface (Config: Endpoint, Model,
// and Events are the minimum). Events is load-bearing now — the loop emits through it;
// Endpoint/Model are validated here for an honest contract even though the Phase-0 fake
// responder ignores them (the real provider dials them in Phase 1).
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

// autoEligible reports whether c can satisfy the Auto gate. A nil Confiner can confine
// nothing, so Auto is refused (ADR 0004 — Auto never runs unconfined).
func autoEligible(c domain.Confiner) bool {
	if c == nil {
		return false
	}
	return c.Capabilities().AutoEligible()
}

// step advances the loop one Turn and returns at a quiescent boundary (ADR 0007). The
// Phase-0 Turn is the throwaway-thin slice: consume queued input, run pre-request
// hooks, ask the Upstream once (non-streaming, no tools), emit the assistant message.
// It honours ctx cancellation and recovers a panic at the extension boundary — the two
// boundary guarantees the capstone exists to prove — without ever unwinding the host.
func (a *Agent) step(ctx context.Context) (domain.StepResult, error) {
	start := time.Now()
	turn := a.turnIndex

	if a.pendingInput != nil {
		a.conv.append(message{Role: domain.RoleUser, Content: a.pendingInput.Text})
		a.pendingInput = nil
		a.inExchange = true
	}

	if err := a.runPreRequestHooks(ctx, turn); err != nil {
		// A hook panicked: the ErrorEvent is already emitted; degrade to a clean
		// boundary with the conversation still serializable (no assistant message).
		return a.completeTurn(turn, start), nil
	}

	response, err := a.upstream.Respond(ctx, a.buildUpstreamRequest())
	if err != nil {
		if ctx.Err() != nil {
			// Cancelled mid-respond: leave state serializable — never half-streamed
			// (ADR 0007) — so the Snapshot taken here resumes and continues. The Turn
			// is abandoned, not completed: return to a clean quiescent boundary without
			// advancing the Turn counter, so resume re-attempts rather than skips it.
			a.inExchange = false
			return domain.StepResult{
				Status:    domain.StatusCancelled,
				TurnIndex: turn,
				Elapsed:   time.Since(start),
			}, nil
		}
		// A non-cancellation Upstream fault is localised to an ErrorEvent; the loop
		// still reaches a clean boundary rather than failing the whole Step.
		a.cfg.Events.Emit(domain.ErrorEvent{
			EventBase: domain.EventBase{Turn: turn},
			Source:    "loop",
			Err:       err.Error(),
		})
		return a.completeTurn(turn, start), nil
	}

	a.conv.append(message{Role: domain.RoleAssistant, Content: response.Content})
	a.cfg.Events.Emit(domain.MessageEvent{EventBase: domain.EventBase{Turn: turn}, Text: response.Content})
	return a.completeTurn(turn, start), nil
}

// completeTurn closes the Exchange at the quiescent boundary and advances the Turn
// counter. The Phase-0 slice is single-Turn and non-streaming, so every Step that is
// not cancelled ends the Exchange (StatusExchangeComplete — awaiting the next Submit).
func (a *Agent) completeTurn(turn int, start time.Time) domain.StepResult {
	a.inExchange = false
	a.turnIndex++
	return domain.StepResult{
		Status:    domain.StatusExchangeComplete,
		TurnIndex: turn,
		Elapsed:   time.Since(start),
	}
}

// runPreRequestHooks fires the registered experimental pre-request hooks, emitting a
// MechanismFiredEvent per successful fire (P0.6d). A panic in any hook is caught,
// reported as an ErrorEvent, and signalled back via errHookPanicked so step can
// degrade — the recover-at-extension-boundary guarantee (ADR 0007 / ADR 0002).
func (a *Agent) runPreRequestHooks(ctx context.Context, turn int) error {
	for _, raw := range a.registry.Experimental(domain.HookPreRequest) {
		hook, ok := raw.(domain.PreRequestHook)
		if !ok {
			continue
		}
		if err := a.firePreRequest(ctx, hook, turn); err != nil {
			return err
		}
		a.cfg.Events.Emit(domain.MechanismFiredEvent{
			EventBase: domain.EventBase{Turn: turn},
			Mechanism: experimentalMechanismID,
			Hook:      domain.HookPreRequest,
			Action:    "fired",
		})
	}
	return nil
}

// firePreRequest invokes one pre-request hook under a recover boundary. The hook
// receives a Request value for shape-parity with the production surface; wiring its
// mutations back into the Upstream request is the Phase-1 hook-mutation API (TDD §6.2)
// and is out of scope for P0.6 — here the hook fires and is observed, nothing more.
func (a *Agent) firePreRequest(ctx context.Context, hook domain.PreRequestHook, turn int) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			a.cfg.Events.Emit(domain.ErrorEvent{
				EventBase: domain.EventBase{Turn: turn},
				Source:    string(experimentalMechanismID),
				Err:       fmt.Sprintf("panic: %v", recovered),
			})
			err = errHookPanicked
		}
	}()
	return hook.PreRequest(ctx, &domain.Request{})
}

// buildUpstreamRequest projects the conversation onto the provider seam's wire shape.
// It is the Phase-0 translation boundary between the loop's conversation state and the
// domain-free provider.Request — the seam the real HTTP provider plugs into (ADR 0010).
func (a *Agent) buildUpstreamRequest() provider.Request {
	messages := make([]provider.Message, 0, len(a.conv.Messages))
	for _, m := range a.conv.Messages {
		messages = append(messages, provider.Message{Role: string(m.Role), Content: m.Content})
	}
	return provider.Request{Model: a.cfg.Model, Messages: messages}
}
