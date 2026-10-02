package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// This file is the streaming half of the Anthropic Messages codec (wire_anthropic.go): the
// parser for the event-typed SSE the Messages API streams. Every `data:` payload is keyed on
// its JSON `type` — the `event:` framing lines repeat it and are not read, and there is no
// `[DONE]` terminator on this wire; `message_stop` ends the reply. The Client's caps and
// verdicts are shared with the openai parser (maxReplyTextBytes, openToolCalls and its
// maxToolCallBytes and maxOpenToolCalls, the malformed-chunk count, isTransientReadError,
// inBandErrorDelta);
// what differs is only the event vocabulary, which lives here and nowhere else.

// The Messages stream event types, as each `data:` payload's `type` names them.
const (
	// anthropicEventMessageStart opens the reply: the served model and the input usage.
	anthropicEventMessageStart = "message_start"
	// anthropicEventBlockStart opens one content block at an index — a text, thinking or
	// tool_use block; a tool_use block carries its id and name here and its input later.
	anthropicEventBlockStart = "content_block_start"
	// anthropicEventBlockDelta carries one fragment of the block at an index.
	anthropicEventBlockDelta = "content_block_delta"
	// anthropicEventBlockStop closes a block; a reasoning block open at its index is yielded.
	anthropicEventBlockStop = "content_block_stop"
	// anthropicEventMessageDelta closes the reply's metadata: stop reason and output usage.
	anthropicEventMessageDelta = "message_delta"
	// anthropicEventMessageStop is the terminator.
	anthropicEventMessageStop = "message_stop"
	// anthropicEventPing is the keep-alive; ignored.
	anthropicEventPing = "ping"
	// anthropicEventError is the in-band failure, framed inside the HTTP 200 stream.
	anthropicEventError = "error"
)

// The content_block_delta fragment types.
const (
	// anthropicDeltaText is a chunk of a text block.
	anthropicDeltaText = "text_delta"
	// anthropicDeltaInputJSON is a chunk of a tool_use block's argument JSON.
	anthropicDeltaInputJSON = "input_json_delta"
	// anthropicDeltaThinking is a chunk of a thinking block.
	anthropicDeltaThinking = "thinking_delta"
	// anthropicDeltaSignature is a chunk of the signature that seals a thinking block.
	anthropicDeltaSignature = "signature_delta"
)

// anthropicBlockToolUse is the content_block type that opens a tool call.
const anthropicBlockToolUse = "tool_use"

// sseDataPrefix is the SSE line prefix carrying a payload; every other line is framing.
const sseDataPrefix = "data: "

// parseSSE reads the Messages event stream line by line and yields Deltas: text_delta as
// content, thinking_delta as thinking, every thinking and redacted_thinking block — its text,
// signature or data — as one verbatim DeltaThinkingBlock when the block closes, carrying the
// place it opened at among the reply's text and tool_use blocks (anthropicReasoningEntry), and
// after them the reply's layout entry when its text sat in more than one block or after a tool
// call (anthropicReplyLayout), tool_use blocks accumulated in openToolCalls by block index across their input_json_delta
// fragments and emitted together immediately before the terminal Done, the stop reason mapped
// onto the loop's finish vocabulary (anthropicFinishReason), and usage assembled from
// message_start's input side and message_delta's output side. A payload that fails to decode is skipped and counted, exactly
// as on the openai wire, and an unknown event or delta type is ignored, so a new event the API
// adds cannot fault a stream. carriedEffort is the request's, for the in-band error delta;
// toolFragment, when non-nil, runs on every tool_use start and input_json_delta as it arrives
// (openToolCalls). Returning false from yield (consumer broke) stops cleanly.
func (a *anthropicCodec) parseSSE(body io.Reader, carriedEffort bool, toolFragment func(), yield func(Delta) bool) {
	s := &anthropicStream{codec: a, carriedEffort: carriedEffort, yield: yield}
	s.open.onFragment = toolFragment
	scanner := newSSEScanner(body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, sseDataPrefix) {
			continue
		}
		if s.event(strings.TrimPrefix(line, sseDataPrefix)) {
			return
		}
	}

	if err := scanner.Err(); err != nil {
		// A read that failed before message_stop: retryable when the failure is the
		// connection's — a mid-stream EOF or a network timeout — as on the openai wire.
		yield(Delta{
			Kind:            DeltaError,
			Err:             fmt.Sprintf("apogee: read stream: %v", err) + malformedChunksNote(s.malformed),
			Retryable:       isTransientReadError(err),
			MalformedChunks: s.malformed,
		})
		return
	}

	// The server closed the connection without message_stop: end the reply on what it sent —
	// the stop reason and usage a message_delta may already have carried are kept.
	s.finish()
}

// anthropicStream is the state one streamed Messages reply accumulates between its events.
type anthropicStream struct {
	codec         *anthropicCodec
	carriedEffort bool
	yield         func(Delta) bool
	// open holds every tool_use block under accumulation, addressed by its block index.
	open openToolCalls
	// reasoning holds every thinking or redacted_thinking block under accumulation, addressed by
	// its block index; nil until the first one opens. A block still open when the stream ends
	// never closed — its signature may be cut short — and is dropped with the map.
	reasoning map[int]*anthropicReasoningBlock
	// textSeen and callsSeen are what of the reply came before the block now opening: whether a
	// text fragment with text has been yielded, and how many tool_use blocks have opened — the
	// place a reasoning block is carried with (openReasoning).
	textSeen  bool
	callsSeen int
	// layout is the reply's blocks in the order they opened, yielded as the reply's layout entry
	// at its end when the places alone cannot rebuild it (anthropicReplyLayout).
	layout anthropicReplyLayout
	// usage is the accounting so far: input tokens from message_start, output tokens from
	// message_delta; nil until either arrives.
	usage *anthropicUsage
	// servedModel is the id message_start named; it rides the terminal Done.
	servedModel string
	// stopReason is message_delta's stop reason, already mapped; "" until it arrives.
	stopReason string
	// textBytes is the running total of content plus thinking bytes yielded, and of the
	// signature and redacted data bytes the reasoning blocks accumulate, against
	// maxReplyTextBytes; tool-call bytes are openToolCalls' own count.
	textBytes int
	// malformed counts the payloads that failed to decode; it rides the terminal Delta.
	malformed int
}

// event folds one decoded payload into the stream and reports whether the stream has ended —
// by its terminator, a fault, or a consumer that broke — so parseSSE stops reading.
func (s *anthropicStream) event(data string) (ended bool) {
	var ev anthropicEvent
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		s.malformed++ // skip the payload, but on the record
		return false
	}
	switch ev.Type {
	case anthropicEventMessageStart:
		s.startMessage(ev)
	case anthropicEventBlockStart:
		return s.startBlock(ev)
	case anthropicEventBlockDelta:
		return s.deltaBlock(ev)
	case anthropicEventMessageDelta:
		s.closeMessage(ev)
	case anthropicEventMessageStop:
		s.finish()
		return true
	case anthropicEventError:
		return s.fail(ev, data)
	case anthropicEventBlockStop:
		return s.stopBlock(ev)
	case anthropicEventPing:
		// Named so the vocabulary stays complete: the keep-alive is just that.
	}
	return false
}

// startMessage records what message_start carries: the served model and the input usage.
func (s *anthropicStream) startMessage(ev anthropicEvent) {
	if ev.Message == nil {
		return
	}
	s.servedModel = ev.Message.Model
	if ev.Message.Usage != nil {
		s.mergeUsage(*ev.Message.Usage)
	}
}

// startBlock opens a tool call for a tool_use block — its id and name arrive here, keyed by
// the block index that every input_json_delta of the block repeats — and a reasoning block for
// a thinking or redacted_thinking one (openReasoning). A text block opens nothing: its
// fragments are yielded as they come.
func (s *anthropicStream) startBlock(ev anthropicEvent) (ended bool) {
	if ev.ContentBlock == nil {
		return false
	}
	switch ev.ContentBlock.Type {
	case anthropicBlockToolUse:
		s.callsSeen++
		s.layout.block(anthropicLayoutToolUse)
		frag := sseToolCall{ID: ev.ContentBlock.ID, Index: ev.Index}
		frag.Function.Name = ev.ContentBlock.Name
		return s.foldCall(frag)
	case anthropicBlockThinking, anthropicBlockRedactedThinking:
		return s.openReasoning(ev)
	}
	return false
}

// openReasoning starts accumulating the reasoning block at the event's index. A thinking block
// opens empty and is filled by its thinking_delta and signature_delta fragments; a
// redacted_thinking block arrives whole — its `data` is on the start event — so its bytes are
// kept as they came and charged to the text cap here. The block records its place — the text
// and calls the reply opened before it. A block with no index cannot be closed and is not opened.
func (s *anthropicStream) openReasoning(ev anthropicEvent) (ended bool) {
	if ev.Index == nil {
		return false
	}
	block := &anthropicReasoningBlock{afterText: s.textSeen, afterCalls: s.callsSeen}
	s.layout.block(anthropicLayoutReasoning)
	if ev.ContentBlock.Type == anthropicBlockRedactedThinking {
		block.redacted = ev.ContentBlock.raw
		if s.charge(len(block.redacted)) {
			return true
		}
	}
	if s.reasoning == nil {
		s.reasoning = map[int]*anthropicReasoningBlock{}
	}
	s.reasoning[*ev.Index] = block
	return false
}

// deltaBlock yields one fragment: text as content, thinking as thinking — both under the
// text cap — and input JSON onto the call open at the fragment's index. Thinking text and
// signature fragments also accumulate on the reasoning block open at their index; a signature
// is charged to the text cap but yields nothing until the block closes. Any other fragment
// type is ignored.
func (s *anthropicStream) deltaBlock(ev anthropicEvent) (ended bool) {
	if ev.Delta == nil {
		return false
	}
	switch ev.Delta.Type {
	case anthropicDeltaText:
		if ev.Delta.Text != "" {
			s.textSeen = true
		}
		s.layout.text(blockIndex(ev.Index), len(ev.Delta.Text))
		return s.text(Delta{Kind: DeltaContent, Content: ev.Delta.Text}, len(ev.Delta.Text))
	case anthropicDeltaThinking:
		if block := s.reasoningAt(ev.Index); block != nil {
			block.thinking.WriteString(ev.Delta.Thinking)
		}
		return s.text(Delta{Kind: DeltaThinking, Thinking: ev.Delta.Thinking}, len(ev.Delta.Thinking))
	case anthropicDeltaSignature:
		if block := s.reasoningAt(ev.Index); block != nil {
			block.signature.WriteString(ev.Delta.Signature)
			return s.charge(len(ev.Delta.Signature))
		}
	case anthropicDeltaInputJSON:
		frag := sseToolCall{Index: ev.Index}
		frag.Function.Arguments = ev.Delta.PartialJSON
		return s.foldCall(frag)
	}
	return false
}

// blockIndex is the block index an event names, or -1 when it names none.
func blockIndex(index *int) int {
	if index == nil {
		return -1
	}
	return *index
}

// reasoningAt is the reasoning block open at index, or nil when none is.
func (s *anthropicStream) reasoningAt(index *int) *anthropicReasoningBlock {
	if index == nil {
		return nil
	}
	return s.reasoning[*index]
}

// stopBlock closes the block at the event's index: a reasoning block open there is yielded as
// one DeltaThinkingBlock, with its place, and forgotten; any other block's close carries nothing the
// accumulator needs.
func (s *anthropicStream) stopBlock(ev anthropicEvent) (ended bool) {
	block := s.reasoningAt(ev.Index)
	if block == nil {
		return false
	}
	delete(s.reasoning, *ev.Index)
	raw, err := block.encode()
	if err != nil {
		s.yield(Delta{Kind: DeltaError, Err: fmt.Sprintf("apogee: encode thinking block: %v", err)})
		return true
	}
	entry := reasoningEntry(raw, block.afterText, block.afterCalls)
	return !s.yield(Delta{Kind: DeltaThinkingBlock, ThinkingBlock: entry})
}

// yieldLayout yields the reply's layout entry, as one more DeltaThinkingBlock after its reasoning
// blocks, when the reply needs one (anthropicReplyLayout.entry), and reports whether the consumer
// still listens. A reasoning block still open at the end is dropped, so the layout that counts it
// would describe a reply the consumer never received: it is not yielded, and the places stand.
func (s *anthropicStream) yieldLayout() bool {
	if len(s.reasoning) > 0 {
		return true
	}
	entry := s.layout.entry()
	if entry == nil {
		return true
	}
	return s.yield(Delta{Kind: DeltaThinkingBlock, ThinkingBlock: entry})
}

// text yields one content or thinking fragment of n bytes under maxReplyTextBytes (charge).
func (s *anthropicStream) text(d Delta, n int) (ended bool) {
	if n == 0 {
		return false
	}
	if s.charge(n) {
		return true
	}
	return !s.yield(d)
}

// charge counts n more reply bytes against maxReplyTextBytes. Crossing the cap is terminal and
// NOT retryable — the same request re-streamed would overflow again — and the bytes that
// crossed it are never yielded, so what the consumer received stays under it.
func (s *anthropicStream) charge(n int) (ended bool) {
	s.textBytes += n
	if s.textBytes > maxReplyTextBytes {
		s.yield(Delta{
			Kind: DeltaError,
			Err: fmt.Sprintf(
				"apogee: streamed reply exceeded the %d MiB text limit",
				maxReplyTextBytes>>20,
			),
		})
		return true
	}
	return false
}

// foldCall folds one tool-call fragment into the open set; crossing maxToolCallBytes or
// opening a call past maxOpenToolCalls is terminal, as on the openai wire, and the fault text
// is the set's own (openToolCalls.tripped) so both wires render one message.
func (s *anthropicStream) foldCall(frag sseToolCall) (ended bool) {
	if s.open.fold(frag) {
		s.yield(Delta{Kind: DeltaError, Err: s.open.tripped})
		return true
	}
	return false
}

// closeMessage records what message_delta carries: the stop reason, mapped onto the loop's
// vocabulary, and the output usage.
func (s *anthropicStream) closeMessage(ev anthropicEvent) {
	if ev.Delta != nil && ev.Delta.StopReason != "" {
		s.stopReason = anthropicFinishReason(ev.Delta.StopReason)
	}
	if ev.Usage != nil {
		s.mergeUsage(*ev.Usage)
	}
}

// mergeUsage overlays the fields an event carried onto the running accounting. message_start
// reports the input side with a placeholder output count and message_delta the cumulative
// output side, sometimes repeating the input — each is written only when the event sent it,
// so neither event zeroes what the other reported.
func (s *anthropicStream) mergeUsage(u anthropicUsage) {
	if s.usage == nil {
		s.usage = &anthropicUsage{}
	}
	if u.InputTokens != 0 {
		s.usage.InputTokens = u.InputTokens
	}
	if u.CacheReadTokens != 0 {
		s.usage.CacheReadTokens = u.CacheReadTokens
	}
	if u.OutputTokens != 0 {
		s.usage.OutputTokens = u.OutputTokens
	}
}

// fail renders the in-band error event as the terminal fault, through the Client's shared
// renderer so the Messages slugs reach classify. Every tool call accumulated so far is dropped
// with it — none has been emitted — and an error event with no error member is counted as
// malformed rather than faulting the stream on nothing.
func (s *anthropicStream) fail(ev anthropicEvent, data string) (ended bool) {
	if ev.Error == nil {
		s.malformed++
		return false
	}
	fault := s.codec.client.inBandErrorDelta(*ev.Error.wireError(), data, s.carriedEffort)
	fault.MalformedChunks = s.malformed
	s.yield(fault)
	return true
}

// finish ends the stream on its success path — message_stop, or the server closing without
// one: the reply's layout entry when it needs one (yieldLayout), every accumulated tool call,
// then the terminal Done. A stream that yielded no text, no
// tool call, and skipped at least one chunk carried nothing the consumer could commit and is
// faulted on the count instead, as on the openai wire. A tool_use block that streamed no
// input at all is the empty object, as anthropicToolArguments makes it on the whole-reply
// path, so a no-argument call reads the same from either surface.
func (s *anthropicStream) finish() {
	if s.textBytes == 0 && len(s.open.entries) == 0 && s.malformed > 0 {
		s.yield(Delta{
			Kind:            DeltaError,
			Err:             fmt.Sprintf(malformedOnlyErrFmt, s.malformed),
			MalformedChunks: s.malformed,
		})
		return
	}
	if !s.yieldLayout() {
		return
	}
	for _, e := range s.open.entries {
		if e.call.Function.Arguments == "" {
			e.call.Function.Arguments = anthropicToolArguments(nil)
		}
	}
	if !s.open.flush(s.yield) {
		return
	}
	reason := s.stopReason
	if reason == "" {
		reason = "stop"
	}
	var usage *Usage
	if s.usage != nil {
		u := s.usage.usage()
		usage = &u
	}
	s.yield(Delta{
		Kind:            DeltaDone,
		FinishReason:    reason,
		Usage:           usage,
		Model:           s.servedModel,
		MalformedChunks: s.malformed,
	})
}

// anthropicEvent is one decoded stream payload. Every event type populates a different
// subset: Message on message_start, Index and ContentBlock on content_block_start, Index and
// Delta on content_block_delta, Delta and Usage on message_delta, Error on error. Index is a
// POINTER because block 0 is legal and an absent index must not read as one.
type anthropicEvent struct {
	Type         string             `json:"type"`
	Index        *int               `json:"index"`
	Message      *anthropicResponse `json:"message"`
	ContentBlock *anthropicBlock    `json:"content_block"`
	Delta        *anthropicDelta    `json:"delta"`
	Usage        *anthropicUsage    `json:"usage"`
	Error        *anthropicError    `json:"error"`
}

// anthropicDelta is the `delta` member of a content_block_delta (Type names the fragment kind,
// one of Text / PartialJSON / Thinking / Signature carries it) or of a message_delta
// (StopReason).
type anthropicDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	PartialJSON string `json:"partial_json"`
	Thinking    string `json:"thinking"`
	Signature   string `json:"signature"`
	StopReason  string `json:"stop_reason"`
}

// anthropicReasoningBlock is one reasoning block under accumulation: a thinking block's text
// and signature as their fragments arrive, or a redacted_thinking block's bytes as its start
// event carried them, and the place it opened at (anthropicReasoningEntry).
type anthropicReasoningBlock struct {
	thinking   strings.Builder
	signature  strings.Builder
	redacted   json.RawMessage
	afterText  bool
	afterCalls int
}

// encode is the finished block as the one JSON object the Messages API takes back: a redacted
// block's own bytes, or a thinking block with both members always present — an empty
// `thinking` (display omitted) is still a member the signature covers.
func (b *anthropicReasoningBlock) encode() (json.RawMessage, error) {
	if b.redacted != nil {
		return b.redacted, nil
	}
	return json.Marshal(anthropicThinkingBlock{
		Type:      anthropicBlockThinking,
		Thinking:  b.thinking.String(),
		Signature: b.signature.String(),
	})
}

// anthropicThinkingBlock is the wire shape of a thinking block rebuilt from its stream
// fragments; no member is omitted when empty.
type anthropicThinkingBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}
