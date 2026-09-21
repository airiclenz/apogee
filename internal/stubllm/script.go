package stubllm

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// defaultChunkRunes is how many runes one streamed delta carries when a Turn leaves
// ChunkRunes unset: small enough that a short reply still arrives in several deltas (so a
// test can observe partial state mid-stream), large enough that a 400-line fixture does not
// become tens of thousands of flushes.
const defaultChunkRunes = 4

// defaultToolArguments is the argument string a tool call streams when a fixture names no
// arguments — a valid empty JSON object, because the loop parses what it receives.
const defaultToolArguments = "{}"

// The two sources a Capture may read. Anything else is a parse error: a misspelled `from:`
// would otherwise capture from an empty string and match nothing, far from its cause.
const (
	captureFromSystem      = "system"
	captureFromLastMessage = "last_message"
)

// placeholderPattern finds the `{{name}}` slots a Turn's captures fill. It deliberately matches
// ANY braced word, not just the known names, so an unknown placeholder is caught at parse time
// rather than reaching the wire as a literal `{{x}}`.
var placeholderPattern = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

// Script is one scripted upstream: the model it advertises and the ordered Turns it answers
// requests with. It is the whole configuration of a [Server]; everything else is transport.
//
// The YAML form is the same shape, so a fixture recorded from a real server and a Script
// built in Go are one format:
//
//	model: stub-model
//	turns:
//	  - text: "hello"
//	    chunk_runes: 2
//	    token_delay: 1ms
type Script struct {
	// Model is the id advertised on GET /v1/models — when Discovery names no models of its
	// own — and echoed on every reply. A test that seeds apogee's config from a Server uses
	// it as the model name.
	Model string `yaml:"model,omitempty"`
	// Discovery is what the server advertises to the two probes apogee makes before its first
	// completion. The zero value is today's server: one model, Model, and no /props.
	Discovery Discovery `yaml:"discovery,omitempty"`
	// Turns are the replies, in order. See [Server] for which Turn answers which request. A
	// Script with a non-zero Discovery block may carry none: a discovery-only fixture drives a
	// probe, never a completion.
	Turns []Turn `yaml:"turns,omitempty"`
}

// Discovery is what a [Server] answers the discovery probes with: the model list GET /v1/models
// advertises and the launch facts llama.cpp's GET /props reports. It is the stub's half of a
// test about what apogee makes of a server's self-description — a window, a slot count, a
// thinking-effort tell, a refused or stalled probe — so those tests script the upstream
// instead of a handler of their own (ADR 0062).
//
// Status, Body and Hang script a FAILED models probe. Status answers GET /v1/models with that
// code and Body instead of the list; Hang holds the probe until the client gives up, and is
// refused beside Status because the two cannot both happen. Both leave GET /props alone.
type Discovery struct {
	// Models is the list GET /v1/models advertises. Empty means the one entry `{id: Model}`
	// the Script's Model names, which is what every fixture written before this block
	// existed advertised.
	Models []DiscoveredModel `yaml:"models,omitempty"`
	// Props is what GET /props reports; nil means the path 404s, as on a server that is not
	// llama.cpp.
	Props *Props `yaml:"props,omitempty"`
	// Status, when non-zero, is the HTTP status GET /v1/models answers with in place of the
	// list. It is an HTTP status, so it is at least 100.
	Status int `yaml:"status,omitempty"`
	// Body is the body a Status reply carries.
	Body string `yaml:"body,omitempty"`
	// Hang holds GET /v1/models until the request's context ends, writing nothing — the
	// server that accepted the connection and never answered. The probe is logged before it
	// blocks, so a test can see it arrive.
	Hang bool `yaml:"hang,omitempty"`
}

// DiscoveredModel is one entry of the advertised model list. The members are exactly what
// internal/provider reads off a real entry: the id, the two spellings of a display name (the
// OpenAI-shaped `name`, the Messages API's `display_name`), the two spellings of a context
// window (OpenRouter's `context_length`, llama.cpp's `meta.n_ctx_train`) and the per-model
// `reasoning` object whose presence is the thinking-effort tell.
type DiscoveredModel struct {
	ID            string          `yaml:"id"`
	Name          string          `yaml:"name,omitempty"`
	DisplayName   string          `yaml:"display_name,omitempty"`
	ContextLength int             `yaml:"context_length,omitempty"`
	NCtxTrain     int             `yaml:"n_ctx_train,omitempty"`
	Reasoning     *ModelReasoning `yaml:"reasoning,omitempty"`
}

// ModelReasoning is the per-model `reasoning` object an OpenRouter-shaped entry carries. Its
// presence alone says the model has a thinking-effort dial, so an empty `reasoning: {}` is a
// meaningful thing to script; the members name the dial's vocabulary, its default and whether
// the model cannot be asked to skip it.
type ModelReasoning struct {
	SupportedEfforts []string `yaml:"supported_efforts,omitempty"`
	DefaultEffort    string   `yaml:"default_effort,omitempty"`
	Mandatory        bool     `yaml:"mandatory,omitempty"`
}

// Props is what llama.cpp's GET /props reports about how the server was launched: the per-slot
// context window (`default_generation_settings.n_ctx`), the number of generation slots
// (`total_slots`) and the Jinja chat template it loaded (`chat_template`, read for the
// thinking-effort tell). Every member is served whether or not it is set — a real /props
// carries all three — so a zero here is a server that reports zero.
type Props struct {
	NCtx         int    `yaml:"n_ctx,omitempty"`
	TotalSlots   int    `yaml:"total_slots,omitempty"`
	ChatTemplate string `yaml:"chat_template,omitempty"`
}

// Turn is one scripted reply. A Turn is exactly ONE kind — a completion, an HTTP reply or a
// hang — with the empty Turn (no text, no tool calls, no http, no hang) meaning the EMPTY-REPLY
// turn a real model produces when it abandons a reply mid-flight. A completion is text, tool
// calls, or BOTH: a model that narrates before it calls a tool streams its content deltas first
// and the tool-call fragments after them, framed head and tail exactly as a call without
// narration is, so one Turn scripts that shape rather than two fakes stitched together.
// Reasoning and Usage accompany a completion; they are refused on an http or hang turn, which
// never reach the completion wire shape at all.
//
// Cut and Error are TERMINATORS, not kinds: each rides a completion or empty turn and replaces
// the stream's ordinary ending — the finish_reason, the usage chunk and [DONE] — with a failure
// a real upstream produces. A turn sets at most one of them, and neither on an http or hang
// turn, which never start a stream to end.
type Turn struct {
	// When, when set, makes this Turn answer only the requests it matches — and makes it
	// beat the ordered turns for those requests. A nil When is an ordered turn.
	When *Match `yaml:"when,omitempty"`
	// Repeat keeps the Turn available forever: it is never consumed, so it answers every
	// request that reaches it. Useful for a "whatever else is asked" fallback.
	Repeat bool `yaml:"repeat,omitempty"`
	// Await withholds this Turn's reply until the test calls [Server.Release] with this
	// label. It is how a fixture states an ORDER when apogee has two requests in flight at
	// once and the test is about what the first answer changed: a delegation's child and the
	// out-of-band call that names it, say, where a name that comes back after the child has
	// finished is dropped by contract (ADR 0068).
	//
	// The gate is deliberately opened by the TEST rather than by another turn, because what
	// such a test waits for is apogee having ACTED on the earlier answer — the name painted,
	// the event folded — and a server can only see when it wrote the bytes, not when the
	// client was done with them. It holds the reply, never the request: every request is
	// matched and logged as it arrives, and only the answer waits.
	//
	// A gate nobody opens fails the held request with a 500 naming the label after
	// [awaitLimit], rather than hanging the suite until the test binary's own timeout.
	Await string `yaml:"await,omitempty"`
	// Captures lift text out of the request this Turn answers, so the reply can echo a path
	// apogee itself announced rather than one the fixture guessed. Every `{{name}}` in Text and
	// in the ToolCalls' Arguments is replaced by the matching capture's value.
	Captures []Capture `yaml:"captures,omitempty"`
	// Text is the assistant content, streamed in ChunkRunes-sized deltas.
	Text string `yaml:"text,omitempty"`
	// TokenDelay is the pause between two streamed deltas. Zero streams as fast as the
	// connection allows; a millisecond or two is enough to observe a partial reply.
	TokenDelay time.Duration `yaml:"token_delay,omitempty"`
	// ChunkRunes is how many runes one delta carries; zero means defaultChunkRunes.
	ChunkRunes int `yaml:"chunk_runes,omitempty"`
	// Chunks places the content's stream boundaries BY HAND: each element is one delta, in
	// order, and the Turn's content is their concatenation. It is for a test about what happens
	// AT a boundary — a `<think>` tag arriving in a delta of its own, a suppressed span split
	// across two — where ChunkRunes' even split lands nowhere useful. It is set INSTEAD of Text
	// and ChunkRunes, never alongside them, and no element is empty: a real server never sends
	// an empty content delta, and a stub that did would prove a decoder against a shape it
	// never meets.
	Chunks []string `yaml:"chunks,omitempty"`
	// ReasoningChunks is Chunks for the reasoning channel: the deltas the thinking streams in,
	// set instead of Reasoning under the same rules.
	ReasoningChunks []string `yaml:"reasoning_chunks,omitempty"`
	// Reasoning is the chain-of-thought channel, streamed BEFORE the content, exactly as the
	// servers that emit it do. ReasoningField names the wire spelling it goes out in.
	Reasoning string `yaml:"reasoning,omitempty"`
	// ReasoningField is the WIRE SPELLING this Turn's Reasoning is written in: either
	// `reasoning_content` — the default, and what an empty value means — or the bare
	// `reasoning` that Ollama and OpenRouter send for the same channel. It exists because a
	// stub that only ever wrote one of the two could not reproduce half the servers apogee
	// meets, and the decoders that read both have to be driven over both to be proven.
	//
	// It is a SPELLING and not a kind: the turn is the same turn either way, and exactly one
	// of the two fields reaches the wire, so a Turn that leaves this unset streams and encodes
	// byte-identically to one written before the key existed.
	ReasoningField string `yaml:"reasoning_field,omitempty"`
	// ToolCalls are the calls this Turn emits. Each is streamed as two fragments — the
	// id-bearing head and an argument tail — the split real servers send.
	ToolCalls []ToolCall `yaml:"tool_calls,omitempty"`
	// Usage is the terminal accounting chunk. Nil means the server reports none, which is
	// what most local servers do.
	Usage *Usage `yaml:"usage,omitempty"`
	// HTTP replaces the completion with a raw HTTP reply — a status, a body, a redirect.
	// Nothing SSE-shaped is written.
	HTTP *HTTPReply `yaml:"http,omitempty"`
	// Hang stalls the request for this long before answering as the empty-reply turn does.
	// A cancelled request context ends the stall at once and writes nothing.
	Hang time.Duration `yaml:"hang,omitempty"`
	// FinishReason overrides the terminal finish_reason. Empty means "stop", or "tool_calls"
	// when the Turn emits any.
	FinishReason string `yaml:"finish_reason,omitempty"`
	// Cut kills the connection mid-stream, after the runes it names, so the client reads an
	// unexpected EOF where the terminator should have been.
	Cut *Cut `yaml:"cut,omitempty"`
	// Error ends the stream with an in-band `{"error": {...}}` object on the 200 response —
	// the way an aggregator reports the failure of the upstream it routed to.
	Error *InBandError `yaml:"error,omitempty"`
}

// Cut is the mid-stream connection loss: the server streams the Turn's reasoning and the first
// AfterRunes runes of its Text, then kills the TCP connection without a terminal chunk, so the
// client's read fails with io.ErrUnexpectedEOF. When AfterRunes covers the whole Text (or the
// Turn has none) every delta is streamed — the tool-call fragments included — and the kill
// lands in place of the terminator; a cut inside the Text drops everything after it. The
// non-streamed path kills the connection after the 200 header, before any body.
//
// The connection is KILLED rather than the handler returned: a returned handler ends the
// chunked body cleanly, which is the EOF the provider client commits as Done(stop) — the
// opposite of the fault this scripts.
type Cut struct {
	AfterRunes int `yaml:"after_runes"`
}

// InBandError is the failure an OpenAI-compatible aggregator delivers on an HTTP 200: an
// `{"error": {"code": N, "message": "..."}}` object, written as an SSE data event after the
// leading deltas on the streamed path and as a member of the JSON body on the whole-reply
// path. Code defaults to 502, the transient class the provider client retries.
type InBandError struct {
	Code    int    `yaml:"code,omitempty"`
	Message string `yaml:"message,omitempty"`
}

// defaultInBandErrorCode is the code an `error` turn carries when the fixture names none: a
// 502, because the fault a test most often scripts is the retryable one.
const defaultInBandErrorCode = 502

// code is the numeric code this error reaches the wire with.
func (e InBandError) code() int {
	if e.Code != 0 {
		return e.Code
	}
	return defaultInBandErrorCode
}

// Match selects the requests a Turn answers. Any combination of its members may be set, and
// every member that is set must match. A Match that sets none is refused by validation — an
// always-true matcher is an ordered turn written the confusing way.
type Match struct {
	// LastMessage is a regexp over the text of the request's LAST message, whatever its role.
	LastMessage string `yaml:"last_message,omitempty"`
	// ToolResult is a tool NAME: the Turn matches when the request's last message is that
	// tool's result. The name is resolved by following the message's tool_call_id back to the
	// assistant turn that issued the call, because the wire shape of a tool result carries the
	// id and not the name.
	ToolResult string `yaml:"tool_result,omitempty"`
	// System is a regexp over the request's system messages, concatenated in wire order the
	// same way a `from: system` capture reads them. It is the discriminator for a request
	// whose distinguishing mark is the directive the engine announced on it rather than
	// anything in the conversation — a tool-less request, say, whose empty tool menu makes the
	// preceding tool result render as a plain user message that `tool_result` cannot see and
	// `last_message` cannot tell apart from the round before.
	System string `yaml:"system,omitempty"`
}

// Capture is one value a Turn lifts out of the request it answers. Name is the placeholder it
// fills (`{{name}}`), From names the request text it reads — `system` for the system messages'
// text concatenated in wire order, `last_message` for the same text `when.last_message` matches
// — and Pattern is a regexp with EXACTLY one capture group, whose group 1 is the value.
//
// A capture is how a fixture scripts "the model uses exactly what it was told": the path in the
// tool call is the one the orientation or a skill header announced on this very request, not a
// path the test guessed and would silently stop testing the day the announcement changed.
type Capture struct {
	Name    string `yaml:"name"`
	From    string `yaml:"from"`
	Pattern string `yaml:"pattern"`
}

// ToolCall is one call a Turn emits. ID may be left unset, in which case the call is numbered
// by position, so a fixture need not invent ids.
type ToolCall struct {
	ID        string `yaml:"id,omitempty"`
	Name      string `yaml:"name"`
	Arguments string `yaml:"arguments,omitempty"`
}

// Usage is the terminal accounting chunk. Cached is the share of Prompt the server answered
// from its prefix cache; it reaches the wire as `prompt_tokens_details.cached_tokens` ONLY
// when it is above zero, because an absent breakdown and a zero one mean different things to
// the provider seam and both shapes must be scriptable.
type Usage struct {
	Prompt     int `yaml:"prompt"`
	Completion int `yaml:"completion"`
	Cached     int `yaml:"cached,omitempty"`
}

// HTTPReply is a raw HTTP answer in place of a completion: an error status, a redirect, a
// proxy's HTML interstitial. Status is required; ContentType defaults to plain text.
type HTTPReply struct {
	Status      int    `yaml:"status"`
	Body        string `yaml:"body,omitempty"`
	Location    string `yaml:"location,omitempty"`
	ContentType string `yaml:"content_type,omitempty"`
}

// Load reads and validates the Script at path.
func Load(path string) (Script, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Script{}, fmt.Errorf("stubllm: read script: %w", err)
	}
	script, err := Parse(data)
	if err != nil {
		return Script{}, fmt.Errorf("%s: %w", path, err)
	}
	return script, nil
}

// Parse decodes and validates a Script from YAML. Unknown keys are refused: a fixture with a
// misspelled `chunk_rune:` would otherwise stream with the default chunking and the test would
// pass for the wrong reason.
func Parse(data []byte) (Script, error) {
	var script Script
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	if err := decoder.Decode(&script); err != nil {
		if errors.Is(err, io.EOF) {
			return Script{}, errors.New("stubllm: empty script")
		}
		return Script{}, fmt.Errorf("stubllm: parse script: %w", err)
	}
	if err := script.Validate(); err != nil {
		return Script{}, err
	}
	return script, nil
}

// Marshal renders a Script as YAML in the form Parse reads back. It is what the recorder
// writes and what a test uses to pin the round trip.
func Marshal(s Script) ([]byte, error) {
	data, err := yaml.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("stubllm: render script: %w", err)
	}
	return data, nil
}

// Validate reports the first thing wrong with a Script. It runs on every construction path —
// Parse, [New] and [Serve] — so an unplayable script fails where it was written rather than
// halfway through a driver test.
func (s Script) Validate() error {
	if err := s.Discovery.validate(); err != nil {
		return fmt.Errorf("stubllm: discovery: %w", err)
	}
	if len(s.Turns) == 0 && s.Discovery.isZero() {
		return errors.New("stubllm: a script needs at least one turn")
	}
	for i := range s.Turns {
		if err := s.Turns[i].validate(); err != nil {
			return fmt.Errorf("stubllm: turn %d: %w", i, err)
		}
	}
	return nil
}

// minHTTPStatus is the smallest code an HTTP status line can carry; a `status:` below it is a
// typo, not a reply any server sends.
const minHTTPStatus = 100

// validate reports the first thing wrong with a Discovery block.
func (d Discovery) validate() error {
	if d.Hang && d.Status != 0 {
		return errors.New("sets both hang and status — a probe is either held or answered")
	}
	if d.Status != 0 && d.Status < minHTTPStatus {
		return fmt.Errorf("status %d is not an HTTP status", d.Status)
	}
	return nil
}

// isZero reports whether the block scripts nothing, so the Script advertises what one without
// the block would. It is the same test yaml's omitempty applies, and the one that decides
// whether a Script may go without Turns.
func (d Discovery) isZero() bool {
	return len(d.Models) == 0 && d.Props == nil && d.Status == 0 && d.Body == "" && !d.Hang
}

// models is the list GET /v1/models advertises: the scripted entries, or the one entry the
// Script's Model names when none are scripted.
func (d Discovery) models(fallback string) []DiscoveredModel {
	if len(d.Models) > 0 {
		return d.Models
	}
	return []DiscoveredModel{{ID: fallback}}
}

// validate reports the first thing wrong with a Turn.
func (t Turn) validate() error {
	if kinds := t.kindCount(); kinds > 1 {
		return errors.New(
			"sets more than one of a completion (text, chunks and/or tool_calls), http and hang — " +
				"a turn is exactly one kind (a turn with none of them is the empty-reply turn)",
		)
	}
	if t.HTTP != nil {
		if t.HTTP.Status == 0 {
			return errors.New("an http turn needs a status")
		}
		if t.hasReasoning() || t.Usage != nil {
			return errors.New("an http turn carries no reasoning or usage")
		}
		if len(t.Captures) > 0 {
			return errors.New("an http turn carries no captures")
		}
	}
	if t.Hang > 0 && (t.hasReasoning() || t.Usage != nil) {
		return errors.New("a hang turn carries no reasoning or usage")
	}
	if t.Hang > 0 && len(t.Captures) > 0 {
		return errors.New("a hang turn carries no captures")
	}
	if t.ChunkRunes < 0 {
		return errors.New("chunk_runes cannot be negative")
	}
	if err := t.validateChunks(); err != nil {
		return err
	}
	for i := range t.ToolCalls {
		if t.ToolCalls[i].Name == "" {
			return fmt.Errorf("tool call %d needs a name", i)
		}
	}
	if t.When != nil {
		if err := t.When.validate(); err != nil {
			return err
		}
	}
	if err := t.validateTerminators(); err != nil {
		return err
	}
	if err := t.validateReasoningField(); err != nil {
		return err
	}
	return t.validateCaptures()
}

// validateChunks reports the first thing wrong with a Turn's hand-placed boundaries. Each list
// stands in for the member it chunks, so the member and the list are refused together, and a
// chunk_runes beside a chunks list would describe a split the list already made. An empty
// element is refused for the wire's sake: no real server sends an empty delta.
func (t Turn) validateChunks() error {
	if len(t.Chunks) > 0 {
		if t.Text != "" {
			return errors.New("sets both text and chunks — chunks IS the text, placed delta by delta")
		}
		if t.ChunkRunes != 0 {
			return errors.New("sets both chunks and chunk_runes — chunks already places every boundary")
		}
	}
	if len(t.ReasoningChunks) > 0 {
		if t.Reasoning != "" {
			return errors.New("sets both reasoning and reasoning_chunks — reasoning_chunks IS the reasoning, placed delta by delta")
		}
		if t.ChunkRunes != 0 {
			return errors.New("sets both reasoning_chunks and chunk_runes — reasoning_chunks already places every boundary")
		}
	}
	for i, chunk := range t.Chunks {
		if chunk == "" {
			return fmt.Errorf("chunks[%d] is empty — no server sends an empty delta", i)
		}
	}
	for i, chunk := range t.ReasoningChunks {
		if chunk == "" {
			return fmt.Errorf("reasoning_chunks[%d] is empty — no server sends an empty delta", i)
		}
	}
	return nil
}

// validateTerminators reports the first thing wrong with a Turn's `cut` or `error`. They are
// not counted by kindCount because they are not kinds: each rides a text, tool-call or empty
// turn and only changes how its stream ENDS, so the one-kind rule and its wording stand and
// the refusals here are the terminators' own — the other terminator, the two kinds that never
// start a stream, and the terminal members a cut or errored stream never reaches.
func (t Turn) validateTerminators() error {
	if t.Cut != nil && t.Error != nil {
		return errors.New("sets both cut and error — a stream ends one way")
	}
	name := t.terminatorName()
	if name == "" {
		return nil
	}
	if t.HTTP != nil {
		return fmt.Errorf("a %s ends a stream, and an http turn never starts one", name)
	}
	if t.Hang > 0 {
		return fmt.Errorf("a %s ends a stream, and a hang turn never starts one", name)
	}
	if t.Usage != nil || t.FinishReason != "" {
		return fmt.Errorf("a %s turn never reaches the terminator, so it carries no usage or finish_reason", name)
	}
	if t.Cut != nil && t.Cut.AfterRunes < 0 {
		return errors.New("cut.after_runes cannot be negative")
	}
	return nil
}

// terminatorName is the key of the terminator this Turn sets, or "" for an ordinary ending.
func (t Turn) terminatorName() string {
	switch {
	case t.Cut != nil:
		return "cut"
	case t.Error != nil:
		return "error"
	}
	return ""
}

// The two wire spellings of the thinking channel a Turn can be scripted in. `reasoning_content`
// is what llama.cpp, vLLM and LM Studio send and what an unset `reasoning_field` means; the bare
// `reasoning` is what Ollama and OpenRouter send for the very same channel.
const (
	reasoningFieldContent = "reasoning_content"
	reasoningFieldBare    = "reasoning"
)

// spellsBareReasoning reports whether this Turn writes its thinking channel as the bare
// `reasoning`. It is the ONE place the default is resolved, so both emitters — the streamed
// deltas and the whole message — agree by construction, and a third spelling would be a branch
// here and nowhere else.
func (t Turn) spellsBareReasoning() bool { return t.ReasoningField == reasoningFieldBare }

// validateReasoningField reports the first thing wrong with a Turn's `reasoning_field`. Every
// message NAMES the key, because the failure a strict parser cannot catch for us is a fixture
// that meant to change the spelling and instead changed nothing.
func (t Turn) validateReasoningField() error {
	if t.ReasoningField == "" {
		return nil
	}
	if t.ReasoningField != reasoningFieldContent && t.ReasoningField != reasoningFieldBare {
		return fmt.Errorf("reasoning_field is %q — a turn spells the thinking channel %s (the default) or %s",
			t.ReasoningField, reasoningFieldContent, reasoningFieldBare)
	}
	if t.HTTP != nil {
		return errors.New("an http turn carries no reasoning, so it carries no reasoning_field")
	}
	if t.Hang > 0 {
		return errors.New("a hang turn carries no reasoning, so it carries no reasoning_field")
	}
	if !t.hasReasoning() {
		return errors.New("reasoning_field spells a turn's reasoning, and this turn has none")
	}
	return nil
}

// validateCaptures reports the first thing wrong with this Turn's captures, including a
// placeholder that names none of them. Both halves have to hold together: a capture nothing
// substitutes is harmless, while a placeholder with no capture would reach the model as the
// literal text `{{x}}` and send the run somewhere nobody scripted.
func (t Turn) validateCaptures() error {
	names := make(map[string]bool, len(t.Captures))
	for i := range t.Captures {
		if err := t.Captures[i].validate(); err != nil {
			return fmt.Errorf("capture %d: %w", i, err)
		}
		if names[t.Captures[i].Name] {
			return fmt.Errorf("capture %d: duplicate name %q", i, t.Captures[i].Name)
		}
		names[t.Captures[i].Name] = true
	}
	for _, text := range t.templated() {
		for _, match := range placeholderPattern.FindAllStringSubmatch(text, -1) {
			if !names[match[1]] {
				return fmt.Errorf("%s names no capture on this turn", match[0])
			}
		}
	}
	return nil
}

// templated is every string of this Turn that captures substitute into: the assistant text —
// whole or chunk by chunk — and each tool call's arguments.
func (t Turn) templated() []string {
	out := make([]string, 0, 1+len(t.Chunks)+len(t.ToolCalls))
	out = append(out, t.Text)
	out = append(out, t.Chunks...)
	for i := range t.ToolCalls {
		out = append(out, t.ToolCalls[i].Arguments)
	}
	return out
}

// validate reports whether a Capture can be evaluated at all.
func (c Capture) validate() error {
	if c.Name == "" {
		return errors.New("needs a name")
	}
	if c.From != captureFromSystem && c.From != captureFromLastMessage {
		return fmt.Errorf("from is %q, want %s or %s", c.From, captureFromSystem, captureFromLastMessage)
	}
	pattern, err := regexp.Compile(c.Pattern)
	if err != nil {
		return fmt.Errorf("pattern is not a regexp: %w", err)
	}
	if pattern.NumSubexp() != 1 {
		return fmt.Errorf("pattern has %d capture groups, want exactly one", pattern.NumSubexp())
	}
	return nil
}

// placeholder is the slot a capture of this name fills.
func placeholder(name string) string {
	return "{{" + name + "}}"
}

// kindCount is how many of the three mutually exclusive reply kinds the Turn sets. Text, chunks
// and tool calls are ONE kind between them — the completion — because a narrating model sends
// all of them in one reply.
func (t Turn) kindCount() int {
	kinds := 0
	for _, set := range []bool{t.isCompletion(), t.HTTP != nil, t.Hang > 0} {
		if set {
			kinds++
		}
	}
	return kinds
}

// isCompletion reports whether the Turn carries any completion content: text, chunks or tool
// calls.
func (t Turn) isCompletion() bool {
	return t.Text != "" || len(t.Chunks) > 0 || len(t.ToolCalls) > 0
}

// hasReasoning reports whether the Turn carries a thinking channel, whole or chunked.
func (t Turn) hasReasoning() bool {
	return t.Reasoning != "" || len(t.ReasoningChunks) > 0
}

// content is the Turn's whole assistant text: Text, or the Chunks joined.
func (t Turn) content() string {
	if len(t.Chunks) > 0 {
		return strings.Join(t.Chunks, "")
	}
	return t.Text
}

// reasoning is the Turn's whole thinking channel: Reasoning, or the ReasoningChunks joined.
func (t Turn) reasoning() string {
	if len(t.ReasoningChunks) > 0 {
		return strings.Join(t.ReasoningChunks, "")
	}
	return t.Reasoning
}

// contentDeltas is the content as it streams: the hand-placed Chunks when the Turn has them,
// the Text split every chunkRunes otherwise.
func (t Turn) contentDeltas() []string {
	if len(t.Chunks) > 0 {
		return t.Chunks
	}
	return splitRunes(t.Text, t.chunkRunes())
}

// reasoningDeltas is the thinking channel as it streams, under the same rule as contentDeltas.
func (t Turn) reasoningDeltas() []string {
	if len(t.ReasoningChunks) > 0 {
		return t.ReasoningChunks
	}
	return splitRunes(t.Reasoning, t.chunkRunes())
}

// chunkRunes is the number of runes one streamed delta of this Turn carries.
func (t Turn) chunkRunes() int {
	if t.ChunkRunes > 0 {
		return t.ChunkRunes
	}
	return defaultChunkRunes
}

// finishReason is the terminal finish_reason this Turn ends on.
func (t Turn) finishReason() string {
	if t.FinishReason != "" {
		return t.FinishReason
	}
	if len(t.ToolCalls) > 0 {
		return "tool_calls"
	}
	return "stop"
}

// validate reports whether a Match can select anything, and compiles every regexp it sets so an
// unplayable pattern fails at the YAML line that wrote it rather than at the turn that matches.
func (m Match) validate() error {
	if m.LastMessage == "" && m.ToolResult == "" && m.System == "" {
		return errors.New("a when block sets last_message, tool_result, system, or any combination")
	}
	if m.LastMessage != "" {
		if _, err := regexp.Compile(m.LastMessage); err != nil {
			return fmt.Errorf("when.last_message is not a regexp: %w", err)
		}
	}
	if m.System != "" {
		if _, err := regexp.Compile(m.System); err != nil {
			return fmt.Errorf("when.system is not a regexp: %w", err)
		}
	}
	return nil
}

// callID is the id this call is streamed under; an unset one is numbered by position.
func (c ToolCall) callID(position int) string {
	if c.ID != "" {
		return c.ID
	}
	return fmt.Sprintf("call_%d", position+1)
}

// arguments is the argument string this call streams, defaulting to an empty JSON object.
func (c ToolCall) arguments() string {
	if c.Arguments != "" {
		return c.Arguments
	}
	return defaultToolArguments
}
