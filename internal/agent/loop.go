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

// defaultCharsPerToken is the Phase-1 trivial chars→tokens estimate the Budget view
// reports until real token accounting and the Budget allocator land (TDD §8 #8). No
// Phase-1 hook reads the budget meaningfully; it is here so the value is usable rather
// than a zero that a future Mechanism might divide by.
const defaultCharsPerToken = 4.0

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

	// Build the pre-request working value from conversation state, run the pre-request
	// hooks against that single shared Request so their mutations compose, then drain
	// it onto the provider wire shape — closing the P0.6 gap where hooks fired but
	// their mutations were dropped (TDD §6.2 / P1.5).
	req := a.buildRequest(turn)
	if err := a.runPreRequestHooks(ctx, turn, req); err != nil {
		// A hook panicked: the ErrorEvent is already emitted; degrade to a clean
		// boundary with the conversation still serializable (no assistant message).
		return a.completeTurn(turn, start), nil
	}

	response, err := a.upstream.Respond(ctx, a.toProviderRequest(req))
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

// runPreRequestHooks fires the registered experimental pre-request hooks against the
// shared req — their mutations compose in registration order — emitting a
// MechanismFiredEvent per successful fire (P0.6d). A panic in any hook is caught,
// reported as an ErrorEvent, and signalled back via errHookPanicked so step can
// degrade — the recover-at-extension-boundary guarantee (ADR 0007 / ADR 0002).
func (a *Agent) runPreRequestHooks(ctx context.Context, turn int, req *domain.Request) error {
	for _, raw := range a.registry.Experimental(domain.HookPreRequest) {
		hook, ok := raw.(domain.PreRequestHook)
		if !ok {
			continue
		}
		if err := a.firePreRequest(ctx, hook, turn, req); err != nil {
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
// shapes the shared req in place (AppendToSystem / InjectContext / SetTools / …); those
// mutations flow into the Upstream request when step drains req with toProviderRequest.
func (a *Agent) firePreRequest(ctx context.Context, hook domain.PreRequestHook, turn int, req *domain.Request) (err error) {
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
	return hook.PreRequest(ctx, req)
}

// buildRequest projects the conversation onto the hook-facing domain.Request the
// pre-request hooks shape. It carries the current tool menu and a trivial Budget so a
// hook can read them through req.View(); the Phase-0 conversation holds only
// role+content messages (P1.6 adds tool calls / IDs / preserved Extra fields).
func (a *Agent) buildRequest(turn int) *domain.Request {
	messages := make([]domain.Message, 0, len(a.conv.Messages))
	for _, m := range a.conv.Messages {
		messages = append(messages, domain.Message{Role: m.Role, Content: m.Content})
	}
	budget := domain.Budget{
		ContextLimit:  a.cfg.Context.MaxContextTokens,
		CharsPerToken: defaultCharsPerToken,
	}
	return domain.NewRequest(a.cfg.Model, messages, a.toolMenu(), budget, turn)
}

// toolMenu builds the model's tool menu from the injected registry (nil ⇒ no tools).
// P1.2 constructs the default registry and dispatches calls; here the menu only feeds
// req.View().Tools() and a tool-filter hook's SetTools.
func (a *Agent) toolMenu() []domain.ToolDef {
	if a.cfg.Tools == nil {
		return nil
	}
	tools := a.cfg.Tools.All()
	menu := make([]domain.ToolDef, 0, len(tools))
	for _, t := range tools {
		menu = append(menu, domain.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Schema:      t.Schema(),
		})
	}
	return menu
}

// toProviderRequest drains the post-hook req onto the provider seam's wire shape — the
// translation boundary between the loop's domain state and the domain-free
// provider.Request the real HTTP provider plugs into (ADR 0010). It carries the
// messages, tool menu, and sampling a pre-request hook shaped; the provider wire has
// no carrier for SetExtra fields yet (response_format is a Phase-4 grammar concern).
func (a *Agent) toProviderRequest(req *domain.Request) provider.Request {
	st := req.State()
	messages := make([]provider.Message, 0, len(st.Messages))
	for _, m := range st.Messages {
		messages = append(messages, provider.Message{Role: string(m.Role), Content: m.Content})
	}
	return provider.Request{
		Model:    st.Model,
		Messages: messages,
		Tools:    toProviderTools(st.Tools),
		Sampling: toProviderSampling(st.Sampling),
	}
}

// toProviderTools maps the domain tool menu onto provider tool specs (nil ⇒ nil).
func toProviderTools(defs []domain.ToolDef) []provider.ToolSpec {
	if len(defs) == 0 {
		return nil
	}
	specs := make([]provider.ToolSpec, 0, len(defs))
	for _, d := range defs {
		specs = append(specs, provider.ToolSpec{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  d.Schema,
		})
	}
	return specs
}

// toProviderSampling maps the two sampling knobs a hook may set onto the provider
// shape; the provider's other knobs (TopP/TopK/RepeatPenalty) stay unset (server default).
func toProviderSampling(p domain.SamplingParams) provider.Sampling {
	return provider.Sampling{Temperature: p.Temperature, MaxTokens: p.MaxTokens}
}
