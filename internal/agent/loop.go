package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/processing"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
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

// maxPostResponseRetries caps how many times an ActionRetry post-response decision may
// re-call the Upstream within one Turn, so a response-repair hook that always retries
// cannot spin the loop forever. After the cap the loop proceeds with the last response.
const maxPostResponseRetries = 3

var (
	errMissingEvents   = errors.New("apogee: Config.Events is required")
	errMissingEndpoint = errors.New("apogee: Config.Endpoint is required")
	errMissingModel    = errors.New("apogee: Config.Model is required")
	// errHookPanicked is an internal signal — never returned to the host — that a
	// panic was recovered at an extension boundary and reported as an ErrorEvent, so
	// the loop can degrade to a clean quiescent boundary instead of unwinding.
	errHookPanicked = errors.New("apogee: extension boundary recovered a panic")
)

// newAgent validates cfg and constructs a ready-to-Step Agent bound to up. The public
// New delegates here with the real provider client; white-box tests inject a deterministic
// fake. Validation order is deliberate: required fields, then the ordering-cycle gate
// (ADR 0003), then the Auto/Confinement gate (ADR 0012 — FSWrite-only AutoEligible).
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

	if cfg.Mode == domain.ModeAuto && cfg.Confiner == nil {
		// Auto needs a Confiner to enforce the subprocess surface. A PRESENT-but-incapable
		// Confiner (no fs-confinement on this host) is allowed: Auto is entered and the
		// subprocess surface gates through Approval rather than refusing Auto ("confine if
		// you can, gate if you can't" — ADR 0012). Only a NIL Confiner — no facility injected
		// at all — refuses, so ErrAutoUnavailable is now conditional, not constant.
		return nil, domain.ErrAutoUnavailable
	}

	return &Agent{
		cfg:      cfg,
		upstream: up,
		registry: registry,
		tools:    resolveTools(cfg),
		guards:   security.NewDefaultGuards(),
	}, nil
}

// resolveTools picks the Agent's tool set: an explicitly injected Config.Tools wins;
// otherwise, when Config.WorkspaceDir is set, the built-in file tools scoped to it; else
// no tools (the host gave neither, so the Agent runs tool-less).
func resolveTools(cfg domain.Config) *domain.ToolRegistry {
	if cfg.Tools != nil {
		return cfg.Tools
	}
	if cfg.WorkspaceDir != "" {
		return tools.NewDefaultRegistry(cfg.WorkspaceDir)
	}
	return nil
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
	if err := a.restoreState(snap.State); err != nil {
		return nil, err
	}
	return a, nil
}

// validateConfig enforces the minimum construction surface (Config: Endpoint, Model, and
// Events are the minimum). Events is load-bearing — the loop emits through it; Endpoint and
// Model are validated here for an honest contract even when a test injects a fake responder
// that ignores them (the real provider dials them).
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

// step advances the loop one Turn and returns at a quiescent boundary (ADR 0007). The full
// Turn is: consume queued input → history-rewrite hooks → build request (drain deferred
// corrections + pre-request hooks) → stream the Upstream reply (emitting TokenEvents) →
// parse tool calls → post-response hooks → if the model asked for tools, dispatch each
// through Approval and continue the Exchange (StatusTurnComplete); otherwise commit the
// final message and end it (StatusExchangeComplete).
//
// Every return is at a serializable boundary. A ctx cancellation rolls this Turn's work
// back and returns StatusCancelled with resumable state; a recovered extension panic or
// Upstream fault degrades the Turn to a clean boundary without unwinding the host.
func (a *Agent) step(ctx context.Context) (domain.StepResult, error) {
	start := time.Now()
	turn := a.turnIndex

	if a.pendingInput != nil {
		a.conv.Append(domain.Message{Role: domain.RoleUser, Content: a.pendingInput.Text})
		a.noteUnresolvedFileRefs(turn, a.pendingInput.FileRefs)
		a.pendingInput = nil
		a.inExchange = true
	}

	// History-rewrite hooks edit conversation state before it is projected (truncation,
	// generative compaction). A recovered panic degrades the Turn with no Upstream call.
	if err := a.runHistoryRewriteHooks(ctx, turn); err != nil {
		return a.abandonTurn(turn, start), nil
	}

	// rollback marks the boundary a cancellation restores to: this Turn's assistant
	// message and tool results are dropped and the drained deferred corrections re-queued,
	// so resume re-attempts the Turn from serializable state. The user message above is
	// kept — the input is not lost to a cancel.
	rollback := a.conv.Len()

	req, deferred := a.buildRequest(turn)
	if err := a.runPreRequestHooks(ctx, turn, req); err != nil {
		// The request was never sent: re-queue the drained corrections so they ride the
		// next request, and degrade the Turn with no assistant message.
		a.restoreDeferred(deferred)
		return a.abandonTurn(turn, start), nil
	}

	resp, outcome := a.respondAndReview(ctx, turn, req)
	switch outcome {
	case turnCancelled:
		return a.cancelTurn(turn, rollback, deferred, start), nil
	case turnFailed:
		a.restoreDeferred(deferred)
		return a.abandonTurn(turn, start), nil
	}

	calls := resp.ToolCalls()
	if len(calls) == 0 {
		// Final no-tool response: commit the assistant message and end the Exchange.
		a.conv.Append(assistantMessage(resp, nil))
		a.cfg.Events.Emit(domain.MessageEvent{EventBase: base(turn), Text: resp.Text()})
		return a.completeTurn(turn, start, domain.StatusExchangeComplete), nil
	}

	// The model requested tools: commit the assistant tool-call message, then dispatch
	// each call through Approval. A cancellation mid-tool rolls the whole Turn back.
	a.conv.Append(assistantMessage(resp, calls))
	if a.dispatchTools(ctx, turn, calls) == dispatchCancelled {
		return a.cancelTurn(turn, rollback, deferred, start), nil
	}
	return a.completeTurn(turn, start, domain.StatusTurnComplete), nil
}

// turnOutcome classifies how the stream → parse → post-response phase ended.
type turnOutcome int

const (
	turnOK        turnOutcome = iota // a usable response (a nil-safe *Response is returned)
	turnCancelled                    // ctx was cancelled mid-stream
	turnFailed                       // an Upstream fault (already surfaced as an ErrorEvent)
)

// respondAndReview streams one Upstream reply, parses its tool calls, builds the post-
// response working value, and runs the post-response hooks — re-calling the Upstream in
// place for an ActionRetry decision (bounded by maxPostResponseRetries). It returns the
// reviewed *Response on turnOK, or nil with turnCancelled / turnFailed.
func (a *Agent) respondAndReview(ctx context.Context, turn int, req *domain.Request) (*domain.Response, turnOutcome) {
	for attempt := 0; ; attempt++ {
		reply := a.streamResponse(ctx, turn, req)
		if ctx.Err() != nil {
			return nil, turnCancelled // a cancel masquerades as a stream error; ctx wins
		}
		if reply.failed {
			a.cfg.Events.Emit(domain.ErrorEvent{EventBase: base(turn), Source: "loop", Err: reply.errMsg})
			return nil, turnFailed
		}

		calls, err := parseToolCalls(reply.toolCalls)
		if err != nil {
			// A malformed tool call degrades to a parse-error path, not a panic: surface
			// it and treat the Turn as a final no-tool response.
			a.cfg.Events.Emit(domain.ErrorEvent{EventBase: base(turn), Source: "processing", Err: err.Error()})
			calls = nil
		}

		resp := domain.NewResponse(reply.content, reply.thinking, calls, reply.finish, req.View())
		retry, hookErr := a.runPostResponseHooks(ctx, turn, resp)
		if hookErr != nil {
			// A post-response hook panicked (recovered into an ErrorEvent): the model did
			// reply, so proceed with the response as reviewed so far rather than abandon.
			return resp, turnOK
		}
		if retry && attempt < maxPostResponseRetries {
			// The Turn re-streams: tell observers the tokens emitted this attempt are
			// superseded, so a streaming UI discards them before the retry streams afresh.
			a.cfg.Events.Emit(domain.StreamResetEvent{EventBase: base(turn)})
			continue
		}
		return resp, turnOK
	}
}

// reply is the assembled result of consuming one streamed completion.
type reply struct {
	content   string
	thinking  string
	toolCalls []provider.ToolCall
	finish    domain.FinishReason
	failed    bool   // a terminal DeltaError / DeltaContextOverflow arrived
	errMsg    string // the terminal fault message when failed
}

// streamResponse consumes the provider's Delta stream, emitting a TokenEvent per content
// chunk as it arrives (the live half of §6 #6) and accumulating text, reasoning, and the
// fully-joined tool calls. The SSE body is drained to its terminal Delta and closed before
// this returns — so Approval, consulted afterward in dispatchTools, never blocks an open
// Upstream connection. A cancellation surfaces as a terminal DeltaError; the caller
// distinguishes it from a real fault by checking ctx.Err().
func (a *Agent) streamResponse(ctx context.Context, turn int, req *domain.Request) reply {
	var out reply
	var content, thinking strings.Builder
	for delta := range a.upstream.Stream(ctx, a.toProviderRequest(req)) {
		switch delta.Kind {
		case provider.DeltaContent:
			content.WriteString(delta.Content)
			a.cfg.Events.Emit(domain.TokenEvent{EventBase: base(turn), Text: delta.Content})
		case provider.DeltaThinking:
			thinking.WriteString(delta.Thinking)
		case provider.DeltaToolCall:
			if delta.ToolCall != nil {
				out.toolCalls = append(out.toolCalls, *delta.ToolCall)
			}
		case provider.DeltaDone:
			out.finish = domain.FinishReason(delta.FinishReason)
		case provider.DeltaError, provider.DeltaContextOverflow:
			out.failed = true
			out.errMsg = delta.Err
		}
	}
	out.content = content.String()
	out.thinking = thinking.String()
	return out
}

// parseToolCalls adapts the provider's wire tool calls onto processing's native shape and
// parses them into domain.ToolCalls (wire types stay provider-local — ADR 0010). An empty
// batch is a no-op; a malformed call returns an ErrMalformedToolCall-wrapped error.
func parseToolCalls(raw []provider.ToolCall) ([]domain.ToolCall, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	native := make([]processing.NativeToolCall, len(raw))
	for i, tc := range raw {
		native[i] = processing.NativeToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		}
	}
	return processing.ParseNativeToolCalls(native)
}

// assistantMessage builds the committed assistant message from the reviewed response. It
// preserves the model's reasoning channel as reasoning_content in the message's Extra so it
// survives snapshot/resume — the channel is recorded in history, not re-sent upstream (the
// provider seam drops Extra). calls is nil for a final no-tool message and the parsed tool
// calls otherwise.
func assistantMessage(resp *domain.Response, calls []domain.ToolCall) domain.Message {
	msg := domain.Message{Role: domain.RoleAssistant, Content: resp.Text(), ToolCalls: calls}
	if think, ok := resp.Thinking(); ok {
		if raw, err := json.Marshal(think); err == nil {
			msg = msg.WithExtra("reasoning_content", raw)
		}
	}
	return msg
}

// completeTurn closes a Turn at the quiescent boundary and advances the Turn counter. A
// final no-tool response ends the Exchange (StatusExchangeComplete — awaiting the next
// Submit); a tool-call Turn leaves the Exchange open (StatusTurnComplete — the next Step
// calls the Upstream again with the tool results in context).
func (a *Agent) completeTurn(turn int, start time.Time, status domain.StepStatus) domain.StepResult {
	if status == domain.StatusExchangeComplete {
		a.inExchange = false
	}
	a.turnIndex++
	return domain.StepResult{Status: status, TurnIndex: turn, Elapsed: time.Since(start)}
}

// abandonTurn ends a Turn that produced no usable assistant message — a recovered
// pre-request / history-rewrite panic, or an Upstream fault — at a clean boundary. The
// Exchange ends (there is nothing to continue from) and the counter advances so resume
// does not re-run the failed Turn.
func (a *Agent) abandonTurn(turn int, start time.Time) domain.StepResult {
	a.inExchange = false
	a.turnIndex++
	return domain.StepResult{Status: domain.StatusExchangeComplete, TurnIndex: turn, Elapsed: time.Since(start)}
}

// cancelTurn rolls the conversation back to the boundary the Turn began at (dropping this
// Turn's assistant message and any tool results), re-queues the deferred corrections it
// drained, and returns StatusCancelled WITHOUT advancing the Turn counter — so the snapshot
// taken here resumes and re-attempts the Turn from serializable state (ADR 0007).
//
// inExchange is deliberately left untouched (NOT cleared): a cancelled Turn does not END the
// Exchange — the user input / tool results committed so far are still mid-flight — so the flag
// must keep reflecting an open Exchange. On resume that makes the next Step re-attempt the Turn
// and, crucially, makes Submit reject a new user message that would otherwise interleave into
// the open Exchange (two consecutive user messages, or a user message wedged after a tool
// result — both of which a strict chat template rejects). Clearing it here contradicted the
// un-advanced turnIndex (which says "re-attempt"), opening that exact hole.
func (a *Agent) cancelTurn(turn, rollback int, deferred []string, start time.Time) domain.StepResult {
	a.conv.DropRange(rollback, a.conv.Len())
	a.restoreDeferred(deferred)
	return domain.StepResult{Status: domain.StatusCancelled, TurnIndex: turn, Elapsed: time.Since(start)}
}

// restoreDeferred re-queues deferred corrections drained by buildRequest when the Turn did
// not commit (cancelled or abandoned), so a best-effort correction is consumed only when a
// request is actually sent and processed to a committed boundary — never silently lost.
func (a *Agent) restoreDeferred(deferred []string) {
	for _, inject := range deferred {
		a.conv.Defer(inject)
	}
}

// buildRequest projects the conversation onto the hook-facing domain.Request the pre-
// request hooks shape, draining any deferred corrections (the ActionDefer feed-forward)
// and injecting each role-safely. It returns the drained corrections so a cancellation can
// re-queue them. The request carries the tool menu (Plan-filtered) and a trivial Budget so
// a hook can read them through req.View().
func (a *Agent) buildRequest(turn int) (*domain.Request, []string) {
	req := domain.NewRequest(a.cfg.Model, a.conv.Messages(), a.toolMenu(), a.budget(), turn)
	deferred, ok := a.conv.TakeDeferred()
	if ok {
		for _, inject := range deferred {
			req.InjectContext(inject)
		}
	}
	return req, deferred
}

// noteUnresolvedFileRefs surfaces that a consumed UserInput carried FileRefs the loop does
// not yet resolve into context. Turning file references into budgeted context is a context-
// builder concern (TDD §8 #8) deferred past Phase 1; until it lands the refs are ignored —
// but NOT silently. It emits a loop ErrorEvent (the same Source "loop" notice channel a
// missing Approver uses) so a host learns its input was only partly consumed and never
// mistakes FileRefs for working. The refs round-trip through a snapshot on UserInput, so
// deferring resolution loses no data.
func (a *Agent) noteUnresolvedFileRefs(turn int, refs []string) {
	if len(refs) == 0 {
		return
	}
	a.cfg.Events.Emit(domain.ErrorEvent{
		EventBase: base(turn),
		Source:    "loop",
		Err: fmt.Sprintf("UserInput.FileRefs are not yet resolved into context and were ignored "+
			"(%d ref(s)); reference resolution is deferred to the context builder", len(refs)),
	})
}

// budget reports the trivial Phase-1 context budget (no token accounting yet — TDD §8 #8).
func (a *Agent) budget() domain.Budget {
	return domain.Budget{ContextLimit: a.cfg.Context.MaxContextTokens, CharsPerToken: defaultCharsPerToken}
}

// toolMenu builds the model's tool menu from the resolved registry (nil ⇒ no tools). In
// Plan mode it offers only read-only tools — the model is never shown a write it cannot
// run (ADR: Plan is read-only).
func (a *Agent) toolMenu() []domain.ToolDef {
	if a.tools == nil {
		return nil
	}
	all := a.tools.All()
	menu := make([]domain.ToolDef, 0, len(all))
	for _, t := range all {
		if a.cfg.Mode == domain.ModePlan && !domain.IsReadOnly(t) {
			continue
		}
		menu = append(menu, domain.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Schema:      t.Schema(),
		})
	}
	return menu
}

// loopView builds the read-only window the tool-stage hooks read — the conversation so far
// (including this Turn's committed assistant + tool messages), the tool menu, the budget,
// and the Turn index. It is rebuilt per call from current state so a hook counting prior
// failures across Turns sees up-to-date history.
func (a *Agent) loopView(turn int) domain.LoopView {
	return domain.NewRequest(a.cfg.Model, a.conv.Messages(), a.toolMenu(), a.budget(), turn).View()
}

// toProviderRequest drains the post-hook req onto the provider seam's wire shape — the
// translation boundary between the loop's domain state and the domain-free provider.Request
// (ADR 0010). It carries messages (with tool calls + tool-call IDs, load-bearing for a
// multi-Turn tool exchange), the tool menu, and the sampling a pre-request hook shaped; the
// provider wire has no carrier for SetExtra fields yet (response_format is a Phase-4 concern).
func (a *Agent) toProviderRequest(req *domain.Request) provider.Request {
	st := req.State()
	messages := make([]provider.Message, 0, len(st.Messages))
	for _, m := range st.Messages {
		messages = append(messages, provider.Message{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCalls:  toProviderToolCalls(m.ToolCalls),
			ToolCallID: m.ToolCallID,
		})
	}
	return provider.Request{
		Model:    st.Model,
		Messages: messages,
		Tools:    toProviderTools(st.Tools),
		Sampling: toProviderSampling(st.Sampling),
	}
}

// toProviderToolCalls maps domain tool calls onto the provider's "function" wire shape so
// an assistant message's tool calls survive the round-trip back to the Upstream (nil ⇒ nil).
func toProviderToolCalls(calls []domain.ToolCall) []provider.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]provider.ToolCall, 0, len(calls))
	for _, c := range calls {
		out = append(out, provider.ToolCall{
			ID:       c.ID,
			Type:     "function",
			Function: provider.FunctionCall{Name: c.Tool, Arguments: string(c.Arguments)},
		})
	}
	return out
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

// toProviderSampling maps the two sampling knobs a hook may set onto the provider shape;
// the provider's other knobs (TopP/TopK/RepeatPenalty) stay unset (server default).
func toProviderSampling(p domain.SamplingParams) provider.Sampling {
	return provider.Sampling{Temperature: p.Temperature, MaxTokens: p.MaxTokens}
}

// base is the EventBase every top-level Event carries (Depth 0; the given Turn index).
func base(turn int) domain.EventBase { return domain.EventBase{Turn: turn} }
