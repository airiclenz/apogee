package stubllm

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
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
	// Model is the id advertised on GET /v1/models and echoed on every reply. A test that
	// seeds apogee's config from a Server uses it as the model name.
	Model string `yaml:"model,omitempty"`
	// Turns are the replies, in order. See [Server] for which Turn answers which request.
	Turns []Turn `yaml:"turns"`
}

// Turn is one scripted reply. A Turn is exactly ONE kind — text, tool calls, an HTTP reply or
// a hang — with the empty Turn (no text, no tool calls, no http, no hang) meaning the
// EMPTY-REPLY turn a real model produces when it abandons a reply mid-flight. Reasoning and
// Usage accompany a text or tool-call turn; they are refused on an http or hang turn, which
// never reach the completion wire shape at all.
type Turn struct {
	// When, when set, makes this Turn answer only the requests it matches — and makes it
	// beat the ordered turns for those requests. A nil When is an ordered turn.
	When *Match `yaml:"when,omitempty"`
	// Repeat keeps the Turn available forever: it is never consumed, so it answers every
	// request that reaches it. Useful for a "whatever else is asked" fallback.
	Repeat bool `yaml:"repeat,omitempty"`
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
	// Reasoning is the chain-of-thought channel (`reasoning_content`), streamed BEFORE the
	// content, exactly as the servers that emit it do.
	Reasoning string `yaml:"reasoning,omitempty"`
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
}

// Match selects the requests a Turn answers. Both members may be set, in which case both must
// match. A Match that sets neither is refused by validation — an always-true matcher is an
// ordered turn written the confusing way.
type Match struct {
	// LastMessage is a regexp over the text of the request's LAST message, whatever its role.
	LastMessage string `yaml:"last_message,omitempty"`
	// ToolResult is a tool NAME: the Turn matches when the request's last message is that
	// tool's result. The name is resolved by following the message's tool_call_id back to the
	// assistant turn that issued the call, because the wire shape of a tool result carries the
	// id and not the name.
	ToolResult string `yaml:"tool_result,omitempty"`
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
	if len(s.Turns) == 0 {
		return errors.New("stubllm: a script needs at least one turn")
	}
	for i := range s.Turns {
		if err := s.Turns[i].validate(); err != nil {
			return fmt.Errorf("stubllm: turn %d: %w", i, err)
		}
	}
	return nil
}

// validate reports the first thing wrong with a Turn.
func (t Turn) validate() error {
	if kinds := t.kindCount(); kinds > 1 {
		return errors.New(
			"sets more than one of text, tool_calls, http and hang — a turn is exactly one kind " +
				"(a turn with none of them is the empty-reply turn)",
		)
	}
	if t.HTTP != nil {
		if t.HTTP.Status == 0 {
			return errors.New("an http turn needs a status")
		}
		if t.Reasoning != "" || t.Usage != nil {
			return errors.New("an http turn carries no reasoning or usage")
		}
		if len(t.Captures) > 0 {
			return errors.New("an http turn carries no captures")
		}
	}
	if t.Hang > 0 && (t.Reasoning != "" || t.Usage != nil) {
		return errors.New("a hang turn carries no reasoning or usage")
	}
	if t.Hang > 0 && len(t.Captures) > 0 {
		return errors.New("a hang turn carries no captures")
	}
	if t.ChunkRunes < 0 {
		return errors.New("chunk_runes cannot be negative")
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
	return t.validateCaptures()
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

// templated is every string of this Turn that captures substitute into: the assistant text and
// each tool call's arguments.
func (t Turn) templated() []string {
	out := make([]string, 0, 1+len(t.ToolCalls))
	out = append(out, t.Text)
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

// kindCount is how many of the four mutually exclusive reply kinds the Turn sets.
func (t Turn) kindCount() int {
	kinds := 0
	for _, set := range []bool{t.Text != "", len(t.ToolCalls) > 0, t.HTTP != nil, t.Hang > 0} {
		if set {
			kinds++
		}
	}
	return kinds
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

// validate reports whether a Match can select anything.
func (m Match) validate() error {
	if m.LastMessage == "" && m.ToolResult == "" {
		return errors.New("a when block sets last_message, tool_result, or both")
	}
	if m.LastMessage == "" {
		return nil
	}
	if _, err := regexp.Compile(m.LastMessage); err != nil {
		return fmt.Errorf("when.last_message is not a regexp: %w", err)
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
