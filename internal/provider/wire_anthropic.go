package provider

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// This file is the Anthropic Messages codec — the wireCodec a server entry with
// `wire: anthropic` speaks (ADR 0078). It owns the request projection onto the Messages body
// (system fold, tool_result / tool_use blocks, output_config.effort), the whole-reply decode
// over anthropicResponse, and the in-band error body; the event-typed SSE parser is its other
// half, in wire_anthropic_stream.go. The JSON shapes live at the foot of this file; nothing
// outside the two branches on the Messages dialect.
//
// Thinking follows the resolved effort: a named effort at or above low requests
// `thinking: {"type":"adaptive"}` beside `output_config.effort`, and every other request —
// off, none, minimal, or no effort at all — sends the no-effort shape its model accepts
// (anthropicNoEffortShapes): `thinking: {"type":"disabled"}` explicitly by default, because the
// current models run adaptive thinking when the key is omitted; no `thinking` key and
// `output_config.effort: "low"` for the models that refuse disabled and have no lower rung; and
// `thinking: {"type":"between_tools"}` for the one that refuses disabled but takes that. Thinking
// blocks are signed and must be replayed verbatim: a reply's `thinking` and `redacted_thinking`
// blocks surface opaque and verbatim (RawResponse.ThinkingBlocks, DeltaThinkingBlock), each with
// the place it held among the reply's text and tool_use blocks — plus the reply's whole layout
// when its text sat in more than one block or after a tool call — and an assistant Message's
// ThinkingBlocks are written back at those places, its text split back into the reply's own text
// blocks — the API takes an assistant turn back only in the order it sent it
// (anthropicReasoningEntry, anthropicReplyLayout). On a preserved-thinking model a reply's entries
// also carry the digest of the prefix the reply was produced on, and they are written back only
// while the prefix before the message still digests the same; otherwise its reasoning blocks are
// left off and the rest of the message goes as it was (anthropicReplayGuard, ADR 0092). Unless
// thinking is disabled the body drops the
// profile's sampling knobs (temperature, top_p, top_k), which the API constrains under thinking
// (ADR 0078 amendments 2026-10-02).

const (
	// anthropicMessagesPath is the Messages endpoint every anthropic request is posted to.
	anthropicMessagesPath = "/v1/messages"
	// anthropicVersion is the API version header value the Messages API requires.
	anthropicVersion = "2023-06-01"
	// anthropicDefaultMaxTokens is the `max_tokens` written when the request pins none —
	// the field is mandatory on this wire, where chat-completions leaves it to the server.
	anthropicDefaultMaxTokens = 4096
	// anthropicThinkingDisabled is the thinking mode a request with no effort above minimal asks
	// for on every model anthropicNoEffortShapes does not list.
	anthropicThinkingDisabled = "disabled"
	// anthropicThinkingAdaptive is the thinking mode a request with a named effort (low..max)
	// asks for: the model decides how much to think, steered by output_config.effort.
	anthropicThinkingAdaptive = "adaptive"
	// anthropicThinkingBetweenTools is the thinking mode a no-effort request asks for on a model
	// that refuses disabled but accepts thinking confined to the gaps between tool calls.
	anthropicThinkingBetweenTools = "between_tools"
)

// anthropicNoEffortShape is the thinking shape a request whose effort resolves below low sends.
type anthropicNoEffortShape int

const (
	// anthropicNoEffortDisabled sends `thinking: {"type":"disabled"}` and no effort — the default.
	anthropicNoEffortDisabled anthropicNoEffortShape = iota
	// anthropicNoEffortLowest sends no `thinking` key and `output_config.effort: "low"`: the model
	// refuses disabled, so the least it can be asked for is adaptive thinking at the lowest rung.
	anthropicNoEffortLowest
	// anthropicNoEffortBetweenTools sends `thinking: {"type":"between_tools"}` and no effort.
	anthropicNoEffortBetweenTools
)

// anthropicModelRow is one anthropicNoEffortShapes row: what the codec must know of the models
// whose ids start with prefix.
type anthropicModelRow struct {
	prefix string
	shape  anthropicNoEffortShape
	// preservesThinking marks the preserved-thinking set: models that check a replayed thinking
	// block's signature against the prefix it was produced on, so their replay is guarded
	// (anthropicReplayGuard, ADR 0092).
	preservesThinking bool
}

// anthropicNoEffortShapes lists the model-id prefixes the codec treats apart from the default: the
// ones whose no-effort request must not carry `thinking: {"type":"disabled"}`, because those
// models answer it with a 400, and the preserved-thinking set. Matching is a case-insensitive
// prefix on the request's model and the longest matching prefix wins; an id no row matches keeps
// the disabled default and replays unguarded, so a new model that also refuses disabled or
// preserves thinking needs a row here (ratified 2026-10-02, ADR 0078, ADR 0092).
var anthropicNoEffortShapes = []anthropicModelRow{
	{prefix: "claude-opus-5-5", shape: anthropicNoEffortLowest, preservesThinking: true},
	{prefix: "claude-fable-5", shape: anthropicNoEffortLowest},
	{prefix: "claude-fable-5-1", shape: anthropicNoEffortLowest, preservesThinking: true},
	{prefix: "claude-mythos-5", shape: anthropicNoEffortLowest},
	{prefix: "claude-mythos-5-1", shape: anthropicNoEffortLowest, preservesThinking: true},
	{prefix: "claude-sonnet-5-5", shape: anthropicNoEffortBetweenTools, preservesThinking: true},
}

// anthropicModelRowFor is the row of the longest anthropicNoEffortShapes prefix model starts
// with, ignoring case, or the zero row — the disabled default, unguarded — when none matches.
func anthropicModelRowFor(model string) anthropicModelRow {
	id := strings.ToLower(model)
	found := anthropicModelRow{shape: anthropicNoEffortDisabled}
	for _, row := range anthropicNoEffortShapes {
		if len(row.prefix) > len(found.prefix) && strings.HasPrefix(id, row.prefix) {
			found = row
		}
	}
	return found
}

// anthropicNoEffortShapeFor is the no-effort shape for model (anthropicModelRowFor).
func anthropicNoEffortShapeFor(model string) anthropicNoEffortShape {
	return anthropicModelRowFor(model).shape
}

// anthropicPreservesThinking reports whether model is in the preserved-thinking set
// (anthropicModelRowFor), the models whose replayed thinking the prefix digest guards.
func anthropicPreservesThinking(model string) bool {
	return anthropicModelRowFor(model).preservesThinking
}

// anthropicCodec speaks the Anthropic Messages protocol on behalf of one Client. The Messages
// path is fixed and the version header is a constant of the wire; the one thing it holds is
// that Client, for the codec-neutral service the stream parser needs mid-decode — the
// in-band error renderer, inBandErrorDelta (wire_anthropic_stream.go).
type anthropicCodec struct {
	client *Client
}

// path is the Messages endpoint; the chat path option is a chat-completions fact and is not read.
func (a *anthropicCodec) path() string { return anthropicMessagesPath }

// headers carries the API key as `x-api-key` and always the `anthropic-version` the Messages API
// requires — so a keyless request (a local proxy) still speaks a version. The JSON content type
// is the Client's, set on every wire.
func (a *anthropicCodec) headers(apiKey string) map[string]string {
	h := map[string]string{"anthropic-version": anthropicVersion}
	if apiKey != "" {
		h["x-api-key"] = apiKey
	}
	return h
}

// encode marshals the Messages body for req and reports whether the request carried an effort —
// the gate on thinkingEffortHint, as chatRequest.carriesEffort is on the openai wire — and, on a
// preserved-thinking model, the digest of the whole prefix the reply will be produced on, for the
// decoders to stamp into its reasoning entries (anthropicReplayGuard). The effort report is the
// requested effort (anthropicEffort), never the body's output_config: the no-effort shape of some
// models writes `output_config.effort: "low"` on its own, and a fault on a request that named no
// effort must not be blamed on one.
func (a *anthropicCodec) encode(req Request) ([]byte, sentRequest, error) {
	wire, digest, err := a.buildBody(req)
	if err != nil {
		return nil, sentRequest{}, err
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, sentRequest{}, err
	}
	_, carriesEffort := anthropicEffort(req.ThinkingEffort)
	return body, sentRequest{carriesEffort: carriesEffort, prefixDigest: digest}, nil
}

// decodeWhole decodes one non-streamed reply. An error body — `{type:"error",error:{…}}`, which
// the API can also frame on an HTTP 200 behind an aggregator — comes back as the wireError with
// a zero RawResponse, never as an empty reply; a body that fails to decode returns the bare
// decode error for the Client to wrap. The reply's reasoning entries carry sent's prefix digest.
func (a *anthropicCodec) decodeWhole(body io.Reader, sent sentRequest) (RawResponse, *wireError, error) {
	var decoded anthropicResponse
	if err := json.NewDecoder(body).Decode(&decoded); err != nil {
		return RawResponse{}, nil, err
	}
	if decoded.Error != nil {
		return RawResponse{}, decoded.Error.wireError(), nil
	}
	return decoded.toRawResponse(sent.prefixDigest), nil, nil
}

// buildBody projects a Request onto the Messages body: every system message folds into the
// top-level `system` (blank-line joined, in order), the rest become content-block messages
// (see anthropicMessages), tools become `tools[]` with their schema under `input_schema`,
// `max_tokens` is always present (anthropicDefaultMaxTokens when the request pins none),
// and a named effort at or above low lands in `output_config.effort` and requests
// `thinking: {"type":"adaptive"}`; off/none/minimal and an absent effort send the model's
// no-effort shape (anthropicNoEffortShapeFor — the Messages API has no rung below low). The
// sampling knobs the wire knows are written only when set, and only while thinking is disabled —
// there is no repeat penalty on this wire, so Sampling.RepeatPenalty is always dropped. On a
// preserved-thinking model it also returns the digest of the body's whole prefix — system, tools
// and every message — and guards each assistant message's replay (anthropicReplayGuard); on any
// other model the digest is "".
func (a *anthropicCodec) buildBody(req Request) (anthropicRequest, string, error) {
	body := anthropicRequest{
		Model:     req.Model,
		Stream:    req.Stream,
		MaxTokens: anthropicDefaultMaxTokens,
	}
	if effort, requestsThinking := anthropicEffort(req.ThinkingEffort); requestsThinking {
		body.Thinking = &anthropicThinking{Type: anthropicThinkingAdaptive}
		body.OutputConfig = &anthropicOutputConfig{Effort: effort}
	} else {
		switch anthropicNoEffortShapeFor(req.Model) {
		case anthropicNoEffortLowest:
			body.OutputConfig = &anthropicOutputConfig{Effort: string(EffortLow)}
		case anthropicNoEffortBetweenTools:
			body.Thinking = &anthropicThinking{Type: anthropicThinkingBetweenTools}
		default:
			body.Thinking = &anthropicThinking{Type: anthropicThinkingDisabled}
		}
	}

	if len(req.Tools) > 0 {
		body.Tools = make([]anthropicTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			body.Tools = append(body.Tools, anthropicTool{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.Parameters,
			})
		}
	}
	body.System = anthropicSystem(req.Messages)

	// The guard digests the system text and tools above, so both are final before any message.
	var guard *anthropicReplayGuard
	if anthropicPreservesThinking(req.Model) {
		var err error
		if guard, err = newAnthropicReplayGuard(body.System, body.Tools); err != nil {
			return anthropicRequest{}, "", err
		}
	}
	messages, err := anthropicMessages(req.Messages, len(req.Tools) > 0, guard)
	if err != nil {
		return anthropicRequest{}, "", err
	}
	body.Messages = messages
	whole, err := guard.prefix(messages)
	if err != nil {
		return anthropicRequest{}, "", err
	}

	s := req.Sampling
	if s.MaxTokens != nil {
		body.MaxTokens = *s.MaxTokens
	}
	// The API refuses most sampling values while thinking is on, so any shape but disabled — a
	// requested thinking pass, or a no-effort shape that still lets the model think — wins over the
	// profile's knobs (owner call, 2026-10-02) rather than failing the request.
	if body.Thinking != nil && body.Thinking.Type == anthropicThinkingDisabled {
		body.Temperature = s.Temperature
		body.TopP = s.TopP
		body.TopK = s.TopK
	}
	return body, whole.digest, nil
}

// anthropicEffort maps a seam Effort onto the `output_config.effort` vocabulary: the five
// levels low..max pass through by name; off, none and minimal — rungs the Messages API does
// not have — and an absent or unrecognised effort write nothing.
func anthropicEffort(e Effort) (string, bool) {
	switch e {
	case EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return string(e), true
	default:
		return "", false
	}
}

// anthropicEffortSupport is the thinking-effort dial the Messages wire IMPLIES: the Messages API
// advertises no tell — its model list names no vocabulary and it serves no /props — so on this
// wire the dial is a property of the protocol rather than a detected fact, and discovery reports
// it for every model (discoverAnthropic). The vocabulary is exactly the set anthropicEffort passes
// through, so the picker can never offer a level the encoder would drop. Dialect stays the zero
// value: the codec reads no EffortDialect — the wire is the dialect — and no default is stated
// because the API names none. A fresh slice per call, so no two ModelInfo entries share one.
func anthropicEffortSupport() EffortSupport {
	return EffortSupport{
		Supported: true,
		Efforts: []string{
			string(EffortLow), string(EffortMedium), string(EffortHigh), string(EffortXHigh), string(EffortMax),
		},
	}
}

// anthropicSystem is the top-level system text: every system message, blank-line joined, in order.
func anthropicSystem(msgs []Message) string {
	var systems []string
	for _, m := range msgs {
		if m.Role == "system" {
			systems = append(systems, m.Content)
		}
	}
	return strings.Join(systems, "\n\n")
}

// anthropicMessages renders the seam messages onto the wire: system messages are skipped — they
// fold into the top-level system text (anthropicSystem); a run of consecutive tool-result messages
// folds into ONE user message of tool_result blocks (the API wants every result of a parallel call
// in a single turn); an assistant message carries a text block for its content and one tool_use
// block per call, with the thinking blocks it carries verbatim at their places
// (Message.ThinkingBlocks, assistantBlocks) when guard admits them over the messages rendered
// before it — a nil guard admits every one. A message that ends up with no block at all — an
// assistant turn with neither text, calls nor thinking — is dropped: the API refuses an empty
// content array.
//
// Without tools the wire refuses tool_use and tool_result blocks outright (a 400 naming the
// missing `tools[]`), so a tool history sent with none — the compaction summariser does exactly
// that — is folded to prose: a tool result becomes plain user text and an assistant call is
// rendered as text (anthropicToolCallText), never as a block.
func anthropicMessages(msgs []Message, hasTools bool, guard *anthropicReplayGuard) ([]anthropicMessage, error) {
	out := make([]anthropicMessage, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "system":
			continue
		case "tool":
			if !hasTools {
				out = appendMessage(out, "user", textBlocks(m.Content))
				continue
			}
			block := anthropicBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content}
			if n := len(out); n > 0 && out[n-1].isToolResults {
				out[n-1].Content = append(out[n-1].Content, block)
				continue
			}
			out = append(out, anthropicMessage{Role: "user", Content: []anthropicBlock{block}, isToolResults: true})
		case "assistant":
			prefix, err := guard.prefix(out)
			if err != nil {
				return nil, err
			}
			blocks, err := assistantBlocks(m, hasTools, prefix)
			if err != nil {
				return nil, err
			}
			out = appendMessage(out, "assistant", blocks)
		default:
			out = appendMessage(out, "user", textBlocks(m.Content))
		}
	}
	return out, nil
}

// appendMessage appends one message of blocks to out, dropping a message that has none.
func appendMessage(out []anthropicMessage, role string, blocks []anthropicBlock) []anthropicMessage {
	if len(blocks) == 0 {
		return out
	}
	return append(out, anthropicMessage{Role: role, Content: blocks})
}

// textBlocks is one text block for content, or none for empty content.
func textBlocks(content string) []anthropicBlock {
	if content == "" {
		return nil
	}
	return []anthropicBlock{{Type: "text", Text: content}}
}

// assistantBlocks renders an assistant message: its text, then one tool_use block per call whose
// `input` is the call's argument string re-marshalled as an object — an argument string that is
// not valid JSON is an encode error naming the call, since the wire cannot carry it — with its
// thinking blocks put back among them where the reply had them. When the carried entries hold the
// reply's layout and it still fits the message, the text is split back into the reply's own text
// blocks and every block goes back in its original place (anthropicReplyLayout.blocks); otherwise
// the text is one block ahead of the calls and each thinking block goes to its slot
// (placeReasoning). Without tools the calls are appended to the text instead (see
// anthropicMessages), so a thinking block that followed a call follows the text. When prefix does
// not admit the entries, the message goes without its reasoning blocks, the rest of it as it was.
func assistantBlocks(m Message, hasTools bool, prefix anthropicReplayPrefix) ([]anthropicBlock, error) {
	entries, layout, err := decodeReasoningEntries(m.ThinkingBlocks)
	if err != nil {
		return nil, err
	}
	if !prefix.admits(entries) {
		entries, layout = nil, layout.withoutReasoning()
	}
	if !hasTools && len(m.ToolCalls) > 0 {
		text := m.Content
		for _, tc := range m.ToolCalls {
			if text != "" {
				text += "\n"
			}
			text += anthropicToolCallText(tc)
		}
		return placeReasoning(entries, textBlocks(text)), nil
	}

	calls := make([]anthropicBlock, 0, len(m.ToolCalls))
	for _, tc := range m.ToolCalls {
		input, err := anthropicToolInput(tc)
		if err != nil {
			return nil, err
		}
		calls = append(calls, anthropicBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}
	if blocks, ok := layout.blocks(m.Content, calls, entries); ok {
		return blocks, nil
	}
	return placeReasoning(entries, append(textBlocks(m.Content), calls...)), nil
}

// placeReasoning interleaves the carried reasoning entries into rest — the message's text block,
// if any, then its tool_use blocks — each at the slot its place names (anthropicReasoningEntry),
// in carried order; a slot past the end of rest is the end.
func placeReasoning(entries []anthropicReasoningEntry, rest []anthropicBlock) []anthropicBlock {
	leadingText := len(rest) > 0 && rest[0].Type == "text"
	blocks := make([]anthropicBlock, 0, len(entries)+len(rest))
	next := 0 // rest[:next] is written
	for _, entry := range entries {
		if slot := min(entry.slot(leadingText), len(rest)); slot > next {
			blocks = append(blocks, rest[next:slot]...)
			next = slot
		}
		blocks = append(blocks, anthropicBlock{raw: entry.Block})
	}
	return append(blocks, rest[next:]...)
}

// anthropicReasoningEntry is how one reasoning block rides the seam (RawResponse.ThinkingBlocks,
// DeltaThinkingBlock, Message.ThinkingBlocks) when something came before it in its reply or its
// reply was produced on a preserved-thinking model: the wire block verbatim under Block, its
// place — whether a text block with text preceded it and how many tool_use blocks did — and
// under Digest the digest of the prefix the reply was produced on (anthropicReplayGuard). A block
// that led its reply on any other model rides as the bare wire block, so the common case there is
// the block itself, and so is every entry a session saved before places or digests were recorded;
// a bare block reads as the zero place with no digest.
//
// The place exists because the API takes an assistant turn back only as it sent it: "every block
// type, in the order received", and a serializer that "reorders blocks edits the prefix for every
// later turn" (platform.claude.com/docs/en/build-with-claude/preserved-thinking, "Send assistant
// turns back exactly as returned"); the latest turn's consecutive thinking blocks "must match what
// the model generated" or the request is a 400 (…/build-with-claude/thinking, "Preserving thinking
// blocks"). A reply can put a thinking block after text or between tool_use blocks — a progress
// update sits immediately before the tool call it introduces — so leading every block is not the
// order received.
//
// A place cannot say where text went once the reply had text in more than one block, or text
// after a tool call: Content is every text block joined. Such a reply carries one more entry after
// its reasoning blocks, holding no block but the reply's whole layout under Layout
// (anthropicReplyLayout), which the encoder prefers while it still fits the message.
type anthropicReasoningEntry struct {
	AfterText  bool            `json:"after_text,omitempty"`
	AfterCalls int             `json:"after_calls,omitempty"`
	Digest     string          `json:"prefix_digest,omitempty"`
	Block      json.RawMessage `json:"block"`
	Layout     []int           `json:"reply_layout,omitempty"`
}

// reasoningEntry is the seam entry for a reasoning block at a place, from a reply produced on a
// prefix with digest: the block itself when nothing preceded it and there is no digest, else the
// block wrapped with its place and digest. block is one JSON object — a decoded block's bytes or a
// streamed block's encoding — and is spliced in as it is, never re-encoded; digest is hex, so it
// needs no escaping.
func reasoningEntry(block json.RawMessage, afterText bool, afterCalls int, digest string) json.RawMessage {
	if !afterText && afterCalls == 0 && digest == "" {
		return block
	}
	var b bytes.Buffer
	b.WriteByte('{')
	if afterText {
		b.WriteString(`"after_text":true,`)
	}
	if afterCalls > 0 {
		fmt.Fprintf(&b, `"after_calls":%d,`, afterCalls)
	}
	if digest != "" {
		fmt.Fprintf(&b, `"prefix_digest":%q,`, digest)
	}
	b.WriteString(`"block":`)
	b.Write(block)
	b.WriteByte('}')
	return b.Bytes()
}

// decodeReasoningEntry reads one seam entry back: a wrapped block with its place and digest, a
// layout entry (Layout set, no Block), or a bare wire block — neither a `block` nor a
// `reply_layout` member — at the zero place with no digest.
func decodeReasoningEntry(raw json.RawMessage) (anthropicReasoningEntry, error) {
	var entry anthropicReasoningEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return anthropicReasoningEntry{}, fmt.Errorf("apogee: carried thinking block: %w", err)
	}
	if entry.Block == nil && entry.Layout == nil {
		return anthropicReasoningEntry{Block: raw}, nil
	}
	return entry, nil
}

// decodeReasoningEntries reads a message's carried entries back: the reasoning blocks in carried
// order, and the reply's layout when an entry recorded one (the last one wins). An empty entry
// carries no block and is skipped; an entry that is not JSON is an encode error, so it never
// reaches the wire.
func decodeReasoningEntries(raws []json.RawMessage) ([]anthropicReasoningEntry, anthropicReplyLayout, error) {
	var layout anthropicReplyLayout
	entries := make([]anthropicReasoningEntry, 0, len(raws))
	for _, raw := range raws {
		if len(raw) == 0 {
			continue
		}
		entry, err := decodeReasoningEntry(raw)
		if err != nil {
			return nil, anthropicReplyLayout{}, err
		}
		if entry.Block == nil {
			layout = anthropicReplyLayout{members: entry.Layout}
			continue
		}
		entries = append(entries, entry)
	}
	return entries, layout, nil
}

// slot is the number of the message's rendered text and tool_use blocks the entry goes after.
// The encoder renders the text first, so a block that followed any of the reply's text or calls
// goes after that text block — when the message has one — and after its AfterCalls calls.
func (e anthropicReasoningEntry) slot(leadingText bool) int {
	if leadingText && (e.AfterText || e.AfterCalls > 0) {
		return e.AfterCalls + 1
	}
	return e.AfterCalls
}

// The members of a reply layout that are not text: every other member is a text block's length.
const (
	// anthropicLayoutReasoning stands for a thinking or redacted_thinking block.
	anthropicLayoutReasoning = -1
	// anthropicLayoutToolUse stands for a tool_use block.
	anthropicLayoutToolUse = -2
)

// anthropicReplyLayout is a reply's blocks in the order received, as the `reply_layout` entry
// carries them: a positive member is a text block of that many bytes of Content, and
// anthropicLayoutReasoning and anthropicLayoutToolUse stand for the next reasoning block and the
// next tool call. Both decoders build one (text, block) and carry it only when the slot places
// cannot rebuild the reply (entry); the encoder rebuilds the reply's blocks from it (blocks).
type anthropicReplyLayout struct {
	members []int
	// textOpen reports that the last member is a text block still taking bytes: the one at
	// block index textIndex.
	textOpen  bool
	textIndex int
}

// text records n bytes of text from the text block at block index index: more of the text block
// the last member counts, or a new one. Empty text records nothing, as an empty block is dropped.
func (l *anthropicReplyLayout) text(index, n int) {
	if n == 0 {
		return
	}
	if l.textOpen && l.textIndex == index {
		l.members[len(l.members)-1] += n
		return
	}
	l.members = append(l.members, n)
	l.textOpen, l.textIndex = true, index
}

// block records a reasoning or tool_use block (anthropicLayoutReasoning, anthropicLayoutToolUse).
func (l *anthropicReplyLayout) block(member int) {
	l.members = append(l.members, member)
	l.textOpen = false
}

// withoutReasoning is the layout of the reply with its reasoning blocks left off — what the
// encoder rebuilds when the replay guard drops them: the text blocks and calls, each in its place.
func (l anthropicReplyLayout) withoutReasoning() anthropicReplyLayout {
	if len(l.members) == 0 {
		return anthropicReplyLayout{}
	}
	members := make([]int, 0, len(l.members))
	for _, member := range l.members {
		if member != anthropicLayoutReasoning {
			members = append(members, member)
		}
	}
	return anthropicReplyLayout{members: members}
}

// entry is the layout entry the reply carries after its reasoning blocks, or nil when it needs
// none: a reply with no reasoning block carries no entry at all, and one whose text is a single
// block ahead of every tool call is rebuilt exactly by the slot places, so it stays as it was.
func (l *anthropicReplyLayout) entry() json.RawMessage {
	hasReasoning, needed, texts, calls := false, false, 0, 0
	for _, member := range l.members {
		switch member {
		case anthropicLayoutReasoning:
			hasReasoning = true
		case anthropicLayoutToolUse:
			calls++
		default:
			texts++
			needed = needed || texts > 1 || calls > 0
		}
	}
	if !hasReasoning || !needed {
		return nil
	}
	raw, err := json.Marshal(struct {
		Layout []int `json:"reply_layout"`
	}{l.members})
	if err != nil {
		return nil
	}
	return raw
}

// blocks rebuilds a message's blocks in the layout's order: text blocks cut from content, the
// calls' tool_use blocks and the reasoning entries' blocks each in turn. It reports false — and
// the caller falls back to the slot places — when there is no layout or it no longer fits the
// message: Content can be edited after decode (a salvaged call, a stripped span), so a cut that
// overruns content, lands inside a rune or leaves bytes over, a member of no known kind, or a
// count of calls or reasoning blocks that differs from the message's, is no layout at all.
func (l anthropicReplyLayout) blocks(
	content string,
	calls []anthropicBlock,
	entries []anthropicReasoningEntry,
) ([]anthropicBlock, bool) {
	if len(l.members) == 0 {
		return nil, false
	}
	blocks := make([]anthropicBlock, 0, len(l.members))
	offset, call, reasoning := 0, 0, 0
	for _, member := range l.members {
		switch {
		case member == anthropicLayoutReasoning && reasoning < len(entries):
			blocks = append(blocks, anthropicBlock{raw: entries[reasoning].Block})
			reasoning++
		case member == anthropicLayoutToolUse && call < len(calls):
			blocks = append(blocks, calls[call])
			call++
		case member > 0 && member <= len(content)-offset && runeBoundary(content, offset+member):
			blocks = append(blocks, anthropicBlock{Type: "text", Text: content[offset : offset+member]})
			offset += member
		default:
			return nil, false
		}
	}
	if offset != len(content) || call != len(calls) || reasoning != len(entries) {
		return nil, false
	}
	return blocks, true
}

// anthropicReplayGuard digests, for one request on a preserved-thinking model, the prefix before
// each assistant message as the codec encodes it: the top-level system text, the tools, and every
// wire message before that one — the set a thinking block's signature binds (ADR 0092). A
// message's reasoning blocks replay only when the digest its entries carry, stamped from the
// request that produced the reply, equals the digest of the prefix encoded so far. Earlier
// assistant messages are part of that prefix as they were actually encoded, so once one reply's
// blocks drop, every later reply's prefix differs and those drop too — the server would refuse
// them anyway. A guard lives for one buildBody and is never shared; a nil guard (any other model)
// admits every entry and digests nothing.
type anthropicReplayGuard struct {
	// head is the sum of the system text and the tools.
	head [sha256.Size]byte
	// settled holds the sums of the leading messages nothing can change any more: every message
	// but the newest, which a following tool result may still join (anthropicMessages).
	settled [][sha256.Size]byte
}

// newAnthropicReplayGuard is the guard of a request whose system text and tools are these.
func newAnthropicReplayGuard(system string, tools []anthropicTool) (*anthropicReplayGuard, error) {
	head, err := json.Marshal(struct {
		System string          `json:"system"`
		Tools  []anthropicTool `json:"tools"`
	}{system, tools})
	if err != nil {
		return nil, fmt.Errorf("apogee: digest request prefix: %w", err)
	}
	return &anthropicReplayGuard{head: sha256.Sum256(head)}, nil
}

// prefix is the replay prefix before a message that follows out: the digest of the system text,
// the tools and out, as hex. A nil guard returns the unguarded prefix.
func (g *anthropicReplayGuard) prefix(out []anthropicMessage) (anthropicReplayPrefix, error) {
	if g == nil {
		return anthropicReplayPrefix{}, nil
	}
	h := sha256.New()
	h.Write(g.head[:])
	for i := range out {
		if i < len(g.settled) {
			h.Write(g.settled[i][:])
			continue
		}
		encoded, err := json.Marshal(out[i])
		if err != nil {
			return anthropicReplayPrefix{}, fmt.Errorf("apogee: digest request prefix: %w", err)
		}
		sum := sha256.Sum256(encoded)
		if i < len(out)-1 {
			g.settled = append(g.settled, sum)
		}
		h.Write(sum[:])
	}
	return anthropicReplayPrefix{guarded: true, digest: hex.EncodeToString(h.Sum(nil))}, nil
}

// anthropicReplayPrefix is what an assistant message's reasoning entries must match to replay:
// nothing when unguarded, else the digest of the prefix encoded before the message.
type anthropicReplayPrefix struct {
	guarded bool
	digest  string
}

// admits reports whether a message's reasoning entries replay over this prefix: always when
// unguarded, else only when every one carries this digest — an entry saved without one cannot show
// its prefix still holds, and the entries of one reply go back together or not at all.
func (p anthropicReplayPrefix) admits(entries []anthropicReasoningEntry) bool {
	if !p.guarded {
		return true
	}
	for _, entry := range entries {
		if entry.Digest == "" || entry.Digest != p.digest {
			return false
		}
	}
	return true
}

// runeBoundary reports whether cutting s at byte offset at splits no rune.
func runeBoundary(s string, at int) bool {
	return at == len(s) || utf8.RuneStart(s[at])
}

// anthropicToolInput re-marshals a call's raw argument string as the object `input` carries:
// the wire wants a JSON object, and an empty argument string — what some models emit for a
// no-argument call — is the empty object.
func anthropicToolInput(tc ToolCall) (json.RawMessage, error) {
	args := strings.TrimSpace(tc.Function.Arguments)
	if args == "" {
		return json.RawMessage(`{}`), nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(args), &object); err != nil {
		return nil, fmt.Errorf("apogee: tool call %s: arguments are not a JSON object: %w", tc.ID, err)
	}
	compact := &bytes.Buffer{}
	if err := json.Compact(compact, []byte(args)); err != nil {
		return nil, fmt.Errorf("apogee: tool call %s: arguments are not a JSON object: %w", tc.ID, err)
	}
	return json.RawMessage(compact.Bytes()), nil
}

// anthropicToolCallText is the prose form of a tool call for a request that offers no tools —
// the name applied to its raw arguments, the way the model would have written it.
func anthropicToolCallText(tc ToolCall) string {
	return fmt.Sprintf("%s(%s)", tc.Function.Name, tc.Function.Arguments)
}

// anthropicRequest is the Messages request body. Sampling pointers, tools, system, thinking and
// output_config are omitted when unset; model, messages, max_tokens and stream are always
// present, and thinking is absent only in the anthropicNoEffortLowest shape.
type anthropicRequest struct {
	Model        string                 `json:"model,omitempty"`
	System       string                 `json:"system,omitempty"`
	Messages     []anthropicMessage     `json:"messages"`
	MaxTokens    int                    `json:"max_tokens"`
	Stream       bool                   `json:"stream"`
	Temperature  *float64               `json:"temperature,omitempty"`
	TopP         *float64               `json:"top_p,omitempty"`
	TopK         *int                   `json:"top_k,omitempty"`
	Tools        []anthropicTool        `json:"tools,omitempty"`
	Thinking     *anthropicThinking     `json:"thinking,omitempty"`
	OutputConfig *anthropicOutputConfig `json:"output_config,omitempty"`
}

// anthropicThinking is the `thinking` object: {"type":"adaptive"} when an effort resolves,
// otherwise the model's no-effort type — {"type":"disabled"} by default, {"type":"between_tools"}
// where anthropicNoEffortShapes says so.
type anthropicThinking struct {
	Type string `json:"type"`
}

// anthropicOutputConfig is the `output_config` object; effort is the one member written.
type anthropicOutputConfig struct {
	Effort string `json:"effort,omitempty"`
}

// anthropicTool is one `tools[]` entry: the seam ToolSpec with its schema under input_schema.
type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// anthropicMessage is one wire message: a role and its content blocks. isToolResults marks a
// user message this codec built from tool results so the next result folds into it; it never
// reaches the wire.
type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`

	isToolResults bool
}

// The content block types that carry reasoning — the ones kept verbatim in both directions.
const (
	// anthropicBlockThinking is a reasoning block: its text and the `signature` that seals it.
	anthropicBlockThinking = "thinking"
	// anthropicBlockRedactedThinking is a reasoning block the server encrypted: only `data`.
	anthropicBlockRedactedThinking = "redacted_thinking"
)

// anthropicBlock is one content block in either direction: text, tool_use (id/name/input),
// tool_result (tool_use_id/content/is_error) or thinking. The union is flat because every
// member is omitted when zero, so each block type serialises to exactly its own keys. A
// reasoning block is the exception: raw holds it as its JSON object — captured on decode
// (UnmarshalJSON), or a carried Message.ThinkingBlocks entry on encode — and MarshalJSON writes
// those bytes instead of the flat members, so a signature, a `data` payload or an empty
// `thinking` member (display omitted) reaches the wire exactly as the server sent it.
type anthropicBlock struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result — IsError is a pointer so it is absent, not false, when the seam carries
	// no outcome (it does not today).
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   *bool  `json:"is_error,omitempty"`
	// thinking
	Thinking string `json:"thinking,omitempty"`

	// raw is a reasoning block's own JSON object; nil for every other block.
	raw json.RawMessage
}

// MarshalJSON writes a reasoning block's raw bytes as they are and every other block by its
// flat members.
func (b anthropicBlock) MarshalJSON() ([]byte, error) {
	if len(b.raw) > 0 {
		return b.raw, nil
	}
	type plain anthropicBlock // no methods: marshals by the struct tags
	return json.Marshal(plain(b))
}

// UnmarshalJSON decodes the flat members and, for a thinking or redacted_thinking block, keeps
// a copy of the block's bytes in raw — the decoder's buffer is reused, so it is never aliased.
func (b *anthropicBlock) UnmarshalJSON(data []byte) error {
	type plain anthropicBlock // no methods: decodes by the struct tags
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*b = anthropicBlock(p)
	if b.Type == anthropicBlockThinking || b.Type == anthropicBlockRedactedThinking {
		b.raw = append(json.RawMessage(nil), data...)
	}
	return nil
}

// anthropicResponse is the whole-reply body, or the error body the API frames as
// `{"type":"error","error":{…}}` — the Error member decides which.
type anthropicResponse struct {
	Type       string           `json:"type"`
	Model      string           `json:"model"`
	Content    []anthropicBlock `json:"content"`
	StopReason string           `json:"stop_reason"`
	Usage      *anthropicUsage  `json:"usage"`
	Error      *anthropicError  `json:"error"`
}

// anthropicError is the `error` member: a class slug and a message.
type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// wireError maps the Messages error onto the seam's in-band error: the slug rides ErrorType,
// where classify reads a class when there is no numeric code (this wire sends none).
func (e anthropicError) wireError() *wireError {
	return &wireError{Message: e.Message, ErrorType: e.Type}
}

// anthropicUsage is the token accounting of a reply. cache_read_input_tokens are the prompt
// tokens served from the prefix cache and are NOT included in input_tokens on this wire.
type anthropicUsage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	CacheReadTokens int `json:"cache_read_input_tokens"`
}

// usage maps the wire accounting onto the seam Usage: the prompt is input plus cache reads
// (the model read both), the cache reads are reported on their own as well, and the total
// is the sum.
func (u anthropicUsage) usage() Usage {
	prompt := u.InputTokens + u.CacheReadTokens
	return Usage{
		PromptTokens:       prompt,
		CompletionTokens:   u.OutputTokens,
		TotalTokens:        prompt + u.OutputTokens,
		CachedPromptTokens: u.CacheReadTokens,
	}
}

// toRawResponse assembles the seam RawResponse: text blocks concatenate into Content, thinking
// blocks into Thinking, every thinking and redacted_thinking block is kept verbatim on
// ThinkingBlocks in reply order with its place among the text and tool_use blocks and the
// digest of the prefix the reply was produced on, when there is one (anthropicReasoningEntry) —
// followed by the reply's layout entry when the places alone cannot
// rebuild it (anthropicReplyLayout) — each tool_use block is one ToolCall with its input
// re-stringified as the argument string, and the stop reason is mapped onto the chat-completions
// vocabulary the loop reads (anthropicFinishReason).
func (r anthropicResponse) toRawResponse(digest string) RawResponse {
	out := RawResponse{Model: r.Model, FinishReason: anthropicFinishReason(r.StopReason)}
	var layout anthropicReplyLayout
	for i, b := range r.Content {
		switch b.Type {
		case "text":
			out.Content += b.Text
			layout.text(i, len(b.Text))
		case anthropicBlockThinking, anthropicBlockRedactedThinking:
			out.Thinking += b.Thinking
			out.ThinkingBlocks = append(out.ThinkingBlocks,
				reasoningEntry(b.raw, out.Content != "", len(out.ToolCalls), digest))
			layout.block(anthropicLayoutReasoning)
		case anthropicBlockToolUse:
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:       b.ID,
				Type:     "function",
				Function: FunctionCall{Name: b.Name, Arguments: anthropicToolArguments(b.Input)},
			})
			layout.block(anthropicLayoutToolUse)
		}
	}
	if entry := layout.entry(); entry != nil {
		out.ThinkingBlocks = append(out.ThinkingBlocks, entry)
	}
	if r.Usage != nil {
		out.Usage = r.Usage.usage()
	}
	return out
}

// anthropicToolArguments is the argument string of a tool_use block: its input object as
// compact JSON, or the empty object when the block carried none. The decoder already proved
// the input well-formed, so a compaction that still fails keeps the bytes as they came.
func anthropicToolArguments(input json.RawMessage) string {
	if len(input) == 0 {
		return "{}"
	}
	compact := &bytes.Buffer{}
	if err := json.Compact(compact, input); err != nil {
		return string(input)
	}
	return compact.String()
}

// anthropicFinishReason maps a Messages stop_reason onto the finish vocabulary the loop reads:
// end_turn and stop_sequence are "stop", max_tokens is "length", tool_use is "tool_calls", and
// anything else (refusal, pause_turn, a future value) passes through unchanged.
func anthropicFinishReason(stop string) string {
	switch stop {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return stop
	}
}
