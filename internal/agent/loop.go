package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/processing"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
)

// experimentalMechanismID is the loop's shorthand for the reserved synthetic MechanismID a
// descriptor-less experimental hook fires under (ADR 0002 — no descriptor, no
// self-regulation). The constant itself lives in domain (R5, phase-4-review-fixes item 4)
// so MechanismRegistry.Add can refuse a catalogued Mechanism claiming it.
const experimentalMechanismID = domain.ExperimentalMechanismID

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
// fake. Validation order is deliberate: required fields, then the ordering-cycle and
// incompatibility gates (ADR 0003), then the Auto/Confinement gate (ADR 0012 — FSWrite-only
// AutoEligible).
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
	if err := registry.ValidateIncompatibilities(); err != nil {
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

	return &Agent{
		cfg:        cfg,
		upstream:   up,
		registry:   registry,
		tools:      resolveTools(cfg),
		guards:     security.NewDefaultGuards(),
		mode:       cfg.Mode, // seed the live, swappable mode from the construction config
		textParser: textParser,
		stripper:   stripper,
		tracker:    newSelfRegulator(),
		tokens:     apogeectx.NewTokenEstimator(),
	}, nil
}

// resolveTools picks the Agent's tool set: an explicitly injected Config.Tools wins;
// otherwise, when Config.WorkspaceDir is set, the built-in file tools scoped to it (with the
// network/host tools configured from Config — the url-safety policy, the web-search endpoint,
// and the Asker); else no tools (the host gave neither, so the Agent runs tool-less).
func resolveTools(cfg domain.Config) *domain.ToolRegistry {
	if cfg.Tools != nil {
		return cfg.Tools
	}
	if cfg.WorkspaceDir != "" {
		return tools.NewDefaultRegistryWithHost(cfg.WorkspaceDir, hostTools(cfg))
	}
	return nil
}

// hostTools builds the host-supplied tool configuration (P3.11) from Config: the url-safety
// guard the network tools filter through (the zero URLGuard — its default-on SSRF floor always
// applies in ALL modes, an app-level guard independent of OS confinement), the configured
// web-search endpoint (empty ⇒ web_search's built-in DuckDuckGo default; "off" disables it),
// and the Asker delegate (nil ⇒ ask_user is not registered).
//
// The url-safety policy is deliberately the default floor, NOT seeded from ConfineNetworkAllow:
// that field is the OS confinement box's network allow-list (CIDRs the confined SUBPROCESS may
// reach), a different concept from the in-process tools' host allow/deny — conflating them would
// silently restrict the network tools to the confinement list. A dedicated url-safety config key
// is a thin later addition; the SSRF floor is the security-relevant default and is on regardless.
func hostTools(cfg domain.Config) tools.HostTools {
	return tools.HostTools{
		URLGuard:          security.URLGuard{},
		WebSearchEndpoint: cfg.WebSearchEndpoint,
		Asker:             cfg.Asker,
	}
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
		// exchangeStart marks the boundary this Exchange opens at (before its first user
		// message), so AbortExchange can roll a cancelled Exchange all the way back to a clean,
		// submittable boundary. It is set once per Exchange: pendingInput is non-nil only on the
		// opening Turn (Submit is refused mid-Exchange), so a continuation Turn never resets it.
		a.exchangeStart = a.conv.Len()
		// Order: attached-skill blocks → @file-ref blocks → the user's text. Skills are
		// per-turn instructions, so prepending them scopes them to this one message (the right
		// semantics; it avoids a skill leaking into every later turn as a system-prompt edit).
		skillBlocks := a.resolveSkillRefs(turn, a.pendingInput.SkillIDs)
		refs := a.resolveFileRefs(turn, a.pendingInput.FileRefs)
		a.conv.Append(domain.Message{Role: domain.RoleUser, Content: skillBlocks + refs + a.pendingInput.Text})
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
		// Final no-tool response: commit the assistant message and end the Exchange. An
		// empty final (whitespace-only text, no calls) is a harmful proxy signal for
		// self-regulation's next-Turn judgment (R3); a substantive answer is neutral.
		if strings.TrimSpace(resp.Text()) == "" {
			a.tracker.noteEmptyResponse()
		}
		a.conv.Append(assistantMessage(resp, nil))
		a.cfg.Events.Emit(domain.MessageEvent{EventBase: a.base(turn), Text: resp.Text()})
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
// place for an ActionRetry decision (bounded by maxPostResponseRetries). A retrying
// decision that carries a correction (Inject != "") re-streams a corrected request in the
// same Turn (R1, amending catalogue C5): the superseded assistant message (text + tool
// calls, when non-empty) and then the correction as a role-safe user message are appended
// to the in-flight request — request-scoped, never committed to history — the exchange the
// sim's retry builders carried. Corrections accumulate across attempts (each retry appends
// onto the same request — the sim's escalating re-asks), bounded by the cap; at the cap
// the last response passes through with no further append. It returns the reviewed
// *Response on turnOK, or nil with turnCancelled / turnFailed.
func (a *Agent) respondAndReview(ctx context.Context, turn int, req *domain.Request) (*domain.Response, turnOutcome) {
	for attempt := 0; ; attempt++ {
		reply := a.streamResponse(ctx, turn, req)
		if ctx.Err() != nil {
			return nil, turnCancelled // a cancel masquerades as a stream error; ctx wins
		}
		if reply.failed {
			a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: "loop", Err: reply.errMsg})
			return nil, turnFailed
		}

		nativeCalls, err := parseToolCalls(reply.toolCalls)
		if err != nil {
			// A malformed tool call degrades to a parse-error path, not a panic: surface
			// it and treat the Turn as a final no-tool response.
			a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: "processing", Err: err.Error()})
			nativeCalls = nil
		}

		resp := a.assembleResponse(turn, req.View(), reply, nativeCalls)
		retry, inject, hookErr := a.runPostResponseHooks(ctx, turn, resp)
		if hookErr != nil {
			// A post-response hook panicked (recovered into an ErrorEvent): the model did
			// reply, so proceed with the response as reviewed so far rather than abandon.
			return resp, turnOK
		}
		if retry && attempt < maxPostResponseRetries {
			// The Turn re-streams: tell observers the tokens emitted this attempt are
			// superseded, so a streaming UI discards them before the retry streams afresh.
			a.cfg.Events.Emit(domain.StreamResetEvent{EventBase: a.base(turn)})
			if inject != "" {
				// Carry the corrective exchange onto the retried request (R1): the
				// superseded assistant message, then the correction as a role-safe user
				// message. An Inject-less retry stays a bare re-stream of the request.
				req.AppendSupersededAssistant(resp.Text(), resp.ToolCalls())
				req.InjectContext(inject)
			}
			continue
		}
		return resp, turnOK
	}
}

// assembleResponse applies the model profile at the parse seam (D5/D6). It strips the reply's
// inline thinking/harmony channel out of the visible content and — only when the structured
// native path produced no calls — recovers a text-format tool call from that stripped content,
// removing the call's markup from the committed text and assigning it a deterministic
// Turn-derived ID (so snapshot/resume and tests stay stable, unlike the oracle's wall-clock ID).
// The model's reasoning (the Upstream-split reasoning_content joined with any stripped inline
// channel) rides on the Response so assistantMessage can preserve it in history. For a native,
// no-inline-thinking profile the stripper and text parser are no-ops, so visible == reply.content
// and calls == nativeCalls — byte-identical to the pre-profile path.
func (a *Agent) assembleResponse(turn int, view domain.LoopView, rep reply, nativeCalls []domain.ToolCall) *domain.Response {
	visible, reasoning := a.stripper.Strip(rep.content)

	calls := nativeCalls
	if len(calls) == 0 {
		// The native channel found nothing, so the text parser is the only tool-call source
		// (D5). It yields at most one call; native profiles return the no-op parser, so this is
		// a no-op there.
		if call, found := a.textParser.ParseToolCall(visible); found {
			visible = a.textParser.StripToolCall(visible)
			call.ID = fmt.Sprintf("text_call_%d", turn)
			calls = []domain.ToolCall{call}
		}
	}

	return domain.NewResponse(visible, joinThinking(rep.thinking, reasoning), calls, rep.finish, view)
}

// joinThinking combines the Upstream-split reasoning (reply.thinking, the reasoning_content
// field) with the reasoning the stripper lifted out of the inline content, Upstream first and
// blank-line joined. Either being empty returns the other unchanged, so a native reply with no
// inline channel returns reply.thinking untouched (the byte-identical anchor).
func joinThinking(upstream, inline string) string {
	switch {
	case upstream == "":
		return inline
	case inline == "":
		return upstream
	default:
		return upstream + "\n\n" + inline
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

// streamResponse consumes the provider's Delta stream, emitting a TokenEvent for the newly-
// revealed VISIBLE content as it arrives (the live half of §6 #6) and accumulating text,
// reasoning, and the fully-joined tool calls. While the accumulated content ends inside an
// unclosed inline reasoning span (stripper.IsMidChannel), token emission is HELD so a model that
// inlines thinking/harmony channels never leaks that markup onto a live stream (item 3); the
// channel's visible text is revealed once its span closes. A native / no-inline-thinking profile's
// stripper is never mid-channel and returns the content untouched, so every content delta emits
// verbatim and unbuffered — byte-identical to the pre-profile loop. The SSE body is drained to its
// terminal Delta and closed before this returns — so Approval, consulted afterward in
// dispatchTools, never blocks an open Upstream connection. A cancellation surfaces as a terminal
// DeltaError; the caller distinguishes it from a real fault by checking ctx.Err().
func (a *Agent) streamResponse(ctx context.Context, turn int, req *domain.Request) reply {
	var out reply
	var content, thinking strings.Builder
	emitted := 0 // bytes of stripped visible content already sent as TokenEvents this stream
	for delta := range a.upstream.Stream(ctx, a.toProviderRequest(req)) {
		switch delta.Kind {
		case provider.DeltaContent:
			content.WriteString(delta.Content)
			emitted = a.emitVisibleDelta(turn, content.String(), emitted)
		case provider.DeltaThinking:
			thinking.WriteString(delta.Thinking)
		case provider.DeltaToolCall:
			if delta.ToolCall != nil {
				out.toolCalls = append(out.toolCalls, *delta.ToolCall)
			}
		case provider.DeltaDone:
			out.finish = domain.FinishReason(delta.FinishReason)
			if u := delta.Usage; u != nil {
				// Calibrate the token accounting against the server's own count before surfacing
				// it: the reported prompt tokens are the honest fill, and prompt-tokens vs the
				// characters actually sent recomputes this model's chars→token ratio (bounded and
				// smoothed), so LoopView.Budget() tracks the real tokenizer instead of a fixed
				// guess (TDD §8 #8, plan item 8).
				st := req.State()
				a.tokens.Calibrate(apogeectx.PromptChars(st.Messages, st.Tools), u.PromptTokens)
				// Surface the server's token accounting so a streaming observer can light up
				// the context-usage gauge and time the completion for a tokens/sec readout. A
				// server that omits usage sends no Usage here, so no event fires (events.go).
				a.cfg.Events.Emit(domain.UsageEvent{
					EventBase:        a.base(turn),
					PromptTokens:     u.PromptTokens,
					CompletionTokens: u.CompletionTokens,
					TotalTokens:      u.TotalTokens,
				})
			}
		case provider.DeltaError, provider.DeltaContextOverflow:
			out.failed = true
			out.errMsg = delta.Err
		}
	}
	out.content = content.String()
	out.thinking = thinking.String()
	return out
}

// emitVisibleDelta emits the newly-revealed VISIBLE tail of the accumulated content as a
// TokenEvent and returns the running count of visible bytes emitted so far this stream. While acc
// ends inside an unclosed inline reasoning span (stripper.IsMidChannel) it emits nothing — holding
// the channel's opening markup and in-flight reasoning off the live stream — and once the span
// closes it strips the reasoning channel and emits only the visible bytes past the count already
// sent. The no-op stripper of a native / no-inline-thinking profile never reports mid-channel and
// returns acc untouched, so this emits each content delta verbatim (the provider filters empty
// content chunks, so len(visible) always advances past emitted) — byte-identical to today.
//
// A channel start token split across two deltas (e.g. "<thi" then "nk>") briefly reveals the
// partial prefix live, because IsMidChannel only turns true once the whole token has accumulated;
// this mirrors the oracle's isThinking and is accepted parity — assembleResponse's post-stream
// strip still removes it from the committed message and final MessageEvent, so no suffix buffering
// is added here (item 3's recorded chunk-boundary edge).
func (a *Agent) emitVisibleDelta(turn int, acc string, emitted int) int {
	if a.stripper.IsMidChannel(acc) {
		return emitted
	}
	visible, _ := a.stripper.Strip(acc)
	if len(visible) <= emitted {
		return emitted
	}
	a.cfg.Events.Emit(domain.TokenEvent{EventBase: a.base(turn), Text: visible[emitted:]})
	return len(visible)
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
	// Resolve the completed Turn for self-regulation (R3, next-Turn judgment): this Turn's
	// outcome judges the PREVIOUS Turn's fires — striking, freezing, or clearing — and this
	// Turn's fires shift into the pending set the next Turn's outcome will judge.
	a.tracker.endTurn()
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
	// A faulted Turn (an Upstream fault or a recovered hook panic) produced no usable outcome, so
	// self-regulation discards it WITHOUT judging — an infra fault neither strikes a Mechanism nor
	// advances the Turn Budget, and this Turn's fires do not bleed into the next Turn's judgment.
	// The pending set (the previous Turn's fires) stays in place for the next completed Turn to
	// judge (R3).
	a.tracker.discardTurn()
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
	// The Turn is rolled back and re-attempted on resume, so self-regulation discards it WITHOUT
	// judging — the re-attempt repopulates the fired-this-Turn set and the proxy signals from
	// scratch. The discard also rolls this Turn's novel-read keys back out of seenReads, so the
	// mandated re-attempt regains its novelty credit; the pending set (the previous Turn's fires)
	// stays in place for the re-attempt's outcome to judge (R3).
	a.tracker.discardTurn()
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
	req := domain.NewRequest(a.cfg.Model, a.conv.Messages(), a.toolMenu(), a.budget(), turn, a.tracker.fireCounts)
	deferred, ok := a.conv.TakeDeferred()
	if ok {
		for _, inject := range deferred {
			req.InjectContext(inject)
		}
	}
	return req, deferred
}

// maxRefFileBytes caps a single @file reference, mirroring the read_file tool's ceiling
// (tools.maxFileReadBytes). It is a sanity bound, not a context budget — token-aware
// trimming is the deferred context-builder's job (TDD §8 #8).
const maxRefFileBytes = 10 * 1024 * 1024

// resolveFileRefs reads each @file reference within the workspace fence and returns the
// content blocks to prepend to the user message. It reuses security.SafeReadFile — the
// os.Root-pinned, TOCTOU-safe read the read_file tool uses — so a ref can never escape the
// workspace (a symlink swapped mid-read is refused, not followed). A missing, escaping,
// oversized, directory, or otherwise unreadable ref is surfaced as a loop ErrorEvent and
// skipped: the Turn proceeds with whatever resolved, and a partly-consumed input is never
// mistaken for working. The refs round-trip through a snapshot on UserInput, so a resumed
// session re-resolves them.
func (a *Agent) resolveFileRefs(turn int, refs []string) string {
	if len(refs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, ref := range refs {
		content, err := a.readFileRef(ref)
		if err != nil {
			a.cfg.Events.Emit(domain.ErrorEvent{
				EventBase: a.base(turn),
				Source:    "loop",
				Err:       fmt.Sprintf("@%s could not be resolved and was ignored: %v", ref, err),
			})
			continue
		}
		fmt.Fprintf(&b, "Referenced file `%s`:\n```\n%s\n```\n\n", ref, content)
	}
	return b.String()
}

// readFileRef resolves one workspace-relative reference to its bounded content. An empty
// WorkspaceDir means no file tools are wired, so references cannot be honoured. The size is
// checked by statting within the workspace fence BEFORE the read, so a hostile @ref cannot
// force a huge file fully into memory before being rejected — the read_file tool's
// stat-then-read discipline (the cap used to be checked only after SafeReadFile had already
// materialized the whole file).
func (a *Agent) readFileRef(ref string) (string, error) {
	if a.cfg.WorkspaceDir == "" {
		return "", errors.New("no workspace is configured for file references")
	}
	info, err := security.SafeStat(a.cfg.WorkspaceDir, ref)
	if err != nil {
		return "", err
	}
	if info.Size() > maxRefFileBytes {
		return "", fmt.Errorf("file too large: %d bytes (max %d)", info.Size(), maxRefFileBytes)
	}
	data, err := security.SafeReadFile(a.cfg.WorkspaceDir, ref)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// resolveSkillRefs resolves each attached skill ID through Config.Skills and returns the
// labeled instruction blocks to prepend to the user message — mirroring resolveFileRefs. The
// blocks are emitted in the order the IDs were attached. An unknown ID (or any ID at all when
// no resolver is wired) is surfaced as a loop ErrorEvent and dropped, so an attached skill is
// never silently ignored — the same "report-and-proceed" contract the @file path keeps. The
// IDs round-trip through a snapshot on UserInput, so a resumed session re-resolves them.
func (a *Agent) resolveSkillRefs(turn int, ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	if a.cfg.Skills == nil {
		a.cfg.Events.Emit(domain.ErrorEvent{
			EventBase: a.base(turn),
			Source:    "loop",
			Err: fmt.Sprintf("%d attached skill(s) could not be resolved (no skills are configured) "+
				"and were ignored", len(ids)),
		})
		return ""
	}

	resolved := a.cfg.Skills.ResolveSkills(ids)
	byID := make(map[string]domain.ResolvedSkill, len(resolved))
	for _, s := range resolved {
		byID[s.ID] = s
	}

	var b strings.Builder
	for _, id := range ids {
		s, ok := byID[id]
		if !ok {
			a.cfg.Events.Emit(domain.ErrorEvent{
				EventBase: a.base(turn),
				Source:    "loop",
				Err:       fmt.Sprintf("attached skill %q is not known and was ignored", id),
			})
			continue
		}
		fmt.Fprintf(&b, "<skill: %s>\n%s\n</skill>\n\n", s.DisplayName, s.Body)
	}
	return b.String()
}

// budget reports the model's context Budget: the discovered window (n_ctx), the token accounting
// the estimator has calibrated against server usage (an honest Used fill and chars→token ratio),
// and the window Allocation the context reducers consume (internal/context.Allocate). It is
// structural — read even under Bypass (D5/D6) — and advisory here: no request is reshaped by it
// until the reducers land (plan item 9).
func (a *Agent) budget() domain.Budget {
	window := a.cfg.Context.MaxContextTokens
	alloc := apogeectx.Allocate(window, a.cfg.Context.ResponseReserve)
	return domain.Budget{
		ContextLimit:    window,
		Used:            a.tokens.Used(),
		CharsPerToken:   a.tokens.CharsPerToken(),
		ResponseReserve: alloc.ResponseReserve,
		SystemPrompt:    alloc.SystemPrompt,
		FileContext:     alloc.FileContext,
		History:         alloc.History,
	}
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
		// Plan mode offers only read-only tools — EXCEPT the sub_agent recursion point, which
		// is bounded one level down (a Plan sub-agent inherits Plan, so its children are
		// read-only too). It is not a leaf write, so hiding it would wrongly deny a Plan-mode
		// parent the ability to delegate read/research work (ADR 0013).
		if a.Mode() == domain.ModePlan && !domain.IsReadOnly(t) && t.Name() != tools.SubAgentToolName {
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
	return domain.NewRequest(a.cfg.Model, a.conv.Messages(), a.toolMenu(), a.budget(), turn, a.tracker.fireCounts).View()
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

	tools := toProviderTools(st.Tools)

	// A non-native tool-call format learns its tools from a text menu + emission instructions,
	// not the wire tools array (D2/D3/D4): render the block over THIS request's (mode-filtered)
	// menu, fold it into the wire system channel, and suppress the native array — sending both
	// would double-tell the model in two formats, and a template without tool support can error
	// on the array. The block is wire-only: it never enters domain history, the snapshot, or any
	// event. A native/zero profile renders "" (processing.InstructionsFor), so the request is
	// byte-identical — no injection, no suppression.
	if block := a.toolInstructions(st.Tools); block != "" {
		messages = injectSystemInstructions(messages, block)
		tools = nil
	}

	return provider.Request{
		Model:    st.Model,
		Messages: messages,
		Tools:    tools,
		Sampling: toProviderSampling(st.Sampling),
	}
}

// toolInstructions renders the non-native profile's wire-only tool menu + emission instructions
// for menu (this request's mode-filtered tool menu) — the emit-side mirror of the parse seam's
// ParserFor (processing.InstructionsFor). A native/zero profile or an empty menu renders "". The
// error path is unreachable at runtime: an unknown tool-call format fails construction via
// ParserFor before any request is built, so a defensively-caught error degrades to no injection
// (the request keeps the native array) rather than propagating up the no-error wire seam.
func (a *Agent) toolInstructions(menu []domain.ToolDef) string {
	block, err := processing.InstructionsFor(a.cfg.Profile, menu)
	if err != nil {
		return ""
	}
	return block
}

// injectSystemInstructions folds the rendered tool menu + format instructions into the wire
// request's system channel (D3): it appends block to the FIRST system message when the wire
// projection already carries one (an embedder can seed one via a hook), else prepends a new sole
// system message at position 0. One merged system message is the shape llama.cpp chat templates
// reliably render — the domain.Request.appendOrCreateSystem semantics applied at the wire seam.
// messages is freshly built by the caller, so the in-place edit is local to this request.
func injectSystemInstructions(messages []provider.Message, block string) []provider.Message {
	for i := range messages {
		if messages[i].Role != string(domain.RoleSystem) {
			continue
		}
		if messages[i].Content == "" {
			messages[i].Content = block
		} else {
			messages[i].Content += "\n\n" + block
		}
		return messages
	}
	sys := provider.Message{Role: string(domain.RoleSystem), Content: block}
	return append([]provider.Message{sys}, messages...)
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

// base is the EventBase every Event this Agent emits carries: the given Turn index and the
// Agent's sub-agent nesting Depth (0 for the top-level Agent, parent+1 for a sub-agent — ADR
// 0013), so a sub-agent's events nest into the parent's stream at Depth > 0 with no per-call
// threading. A nested sub-agent re-emits through its OWN Agent (constructed at the deeper
// depth), so the depth is read from the emitting Agent rather than passed around.
func (a *Agent) base(turn int) domain.EventBase { return domain.EventBase{Depth: a.depth, Turn: turn} }
