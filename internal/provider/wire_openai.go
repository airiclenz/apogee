package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// This file is the OpenAI chat-completions codec — the one wireCodec every server entry
// spoke before the `wire:` key existed, and the default WithWire falls back to. It owns
// the request projection (buildBody, formatMessage, applyEffort), the whole-reply decode
// over chatCompletionResponse and the SSE parser; the JSON shapes it reads and writes live
// in wirejson.go and stream.go. Nothing outside this file branches on the OpenAI dialect.

// sseDone is the chat-completions stream terminator, the one `data:` payload that is not JSON.
const sseDone = "[DONE]"

// openaiCodec speaks OpenAI chat-completions on behalf of one Client. It holds that Client
// for the two things that are codec-neutral but needed mid-decode — sanitize and the
// in-band error renderer — and the chat path WithChatPath may have overridden.
type openaiCodec struct {
	client   *Client
	chatPath string
}

// path is the chat-completions endpoint: defaultChatPath unless WithChatPath overrode it.
func (o *openaiCodec) path() string { return o.chatPath }

// headers carries the API key as a bearer token, and nothing when there is no key.
func (o *openaiCodec) headers(apiKey string) map[string]string {
	if apiKey == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + apiKey}
}

// encode marshals the chat-completions body for req and reports whether it expressed a
// thinking effort in any dialect (see chatRequest.carriesEffort).
func (o *openaiCodec) encode(req Request) ([]byte, bool, error) {
	wire := o.buildBody(req)
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, false, err
	}
	return body, wire.carriesEffort(), nil
}

// decodeWhole decodes one non-streamed reply. A body whose error member is set comes back
// as that member with a zero RawResponse — an aggregator can answer HTTP 200 and put the
// provider's failure in the body, and it must not fall through to toRawResponse, which maps
// such a reply's zero choices to a silent zero RawResponse. A body that fails to decode
// returns the bare decode error for the Client to wrap.
func (o *openaiCodec) decodeWhole(body io.Reader) (RawResponse, *wireError, error) {
	var decoded chatCompletionResponse
	if err := json.NewDecoder(body).Decode(&decoded); err != nil {
		return RawResponse{}, nil, err
	}
	if decoded.Error != nil {
		return RawResponse{}, decoded.Error, nil
	}
	return decoded.toRawResponse(), nil, nil
}

// buildBody projects a Request onto the OpenAI chat-completions JSON body, faithfully to
// the TS oracle: the model is the one Client.encode resolved (the configured model wins
// over the request's), sampling knobs are included only when set,
// stream_options.include_usage rides every streamed request, and tools (when present)
// switch message formatting into native-tool mode. The logprobs pair is
// added only when the caller asked for it (`apogee probe model`), and the thinking-effort keys
// only when a caller named an effort (applyEffort picks which one the bound server reads), so
// the loop's bytes are untouched.
func (o *openaiCodec) buildBody(req Request) chatRequest {
	hasTools := len(req.Tools) > 0

	body := chatRequest{Stream: req.Stream}
	body.Messages = make([]chatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, formatMessage(m, hasTools))
	}

	if req.Stream {
		body.StreamOptions = &streamOptions{IncludeUsage: true}
	}

	body.Model = req.Model

	s := req.Sampling
	body.Temperature = s.Temperature
	body.TopP = s.TopP
	body.TopK = s.TopK
	body.RepeatPenalty = s.RepeatPenalty
	body.MaxTokens = s.MaxTokens

	if req.LogProbs {
		// Asked for only when the caller wants the candidate distribution, so an ordinary
		// loop request stays byte-identical on the wire (see chatRequest's pointer fields).
		on, n := true, topLogProbsCount
		body.LogProbs = &on
		body.TopLogProbs = &n
	}

	applyEffort(&body, req.EffortDialect, req.ThinkingEffort)

	if hasTools {
		body.Tools = make([]chatTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			body.Tools = append(body.Tools, chatTool{
				Type:     "function",
				Function: chatToolFunction(t),
			})
		}
	}
	return body
}

// applyEffort expresses a request's thinking-effort intent in the dialect the bound server
// reads, one canonical mapping per sighted dialect (ADR 0050, amended by ADR 0060):
//
//   - kwargs (llama.cpp, and the zero dialect): `chat_template_kwargs` is forwarded into the
//     chat template, where the Qwen-family templates read `enable_thinking` to pre-close the
//     reasoning block and the newer ones read a `reasoning_effort` ENTRY for how much of it to
//     produce. Template-dependent, and a strict OpenAI-compatible server may reject the unknown
//     field, so a caller must not rely on it alone.
//   - reasoning (OpenRouter): a top-level `reasoning` object — a level, or `enabled: false` to
//     switch thinking off, which is what the "off" rung means in that dialect.
//   - openai (OpenAI, Groq): a top-level `reasoning_effort` FIELD — the same word as the kwargs
//     entry above, in a different place on the wire. Those models cannot disable reasoning at
//     all, so the "off" rung maps to their documented floor, "minimal".
//   - off (an entry's `effort-dialect: off`): nothing is emitted in any shape. A server that
//     errors on a kwarg it does not know is told nothing at all, which is the only mapping that
//     cannot make it fail — and the one case where a stated effort is deliberately dropped.
//
// Nothing is emitted for an absent ("") or unrecognised effort: the config loader's enum
// already rejects typos, the Client stays total, and a caller that asks for nothing puts
// byte-identical bytes on the wire. Levels the bound template does not know pass through
// verbatim — the server rejecting one is the enriched turn error, not this function's business.
func applyEffort(body *chatRequest, dialect EffortDialect, effort Effort) {
	if !isNamedEffort(effort) {
		return
	}

	switch dialect {
	case EffortDialectOff:
		return
	case EffortDialectReasoning:
		if effort == EffortOff || effort == EffortNone {
			enabled := false
			body.Reasoning = &reasoningField{Enabled: &enabled}
			return
		}
		body.Reasoning = &reasoningField{Effort: string(effort)}
	case EffortDialectOpenAI:
		level := string(effort)
		if effort == EffortOff || effort == EffortNone {
			level = string(EffortMinimal)
		}
		body.ReasoningEffort = &level
	case EffortDialectNone, EffortDialectKwargs:
		if effort == EffortOff {
			body.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
			return
		}
		body.ChatTemplateKwargs = map[string]any{"reasoning_effort": string(effort)}
	}
}

// isNamedEffort reports whether e is one of the vocabulary's named levels — "" (absence) and
// anything unrecognised are not, and emit nothing on every dialect.
func isNamedEffort(e Effort) bool {
	switch e {
	case EffortOff, EffortNone, EffortMinimal, EffortLow,
		EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return true
	default:
		return false
	}
}

// formatMessage renders one seam Message onto the wire schema. Without native tools a
// tool-result degrades to a user message (the model never sees a bare "tool" role it was
// not told to produce); with native tools the tool linkage is preserved. content is null
// when an assistant message carries only tool calls (OpenAI's convention).
func formatMessage(m Message, hasTools bool) chatMessage {
	if !hasTools && m.Role == "tool" {
		content := m.Content
		return chatMessage{Role: "user", Content: &content}
	}

	out := chatMessage{Role: m.Role}
	if len(m.ToolCalls) > 0 && m.Content == "" {
		out.Content = nil // null: tool-call-only assistant turn
	} else {
		content := m.Content
		out.Content = &content
	}
	if hasTools {
		out.ToolCallID = m.ToolCallID
		out.ToolCalls = m.ToolCalls
	}
	return out
}

// parseSSE reads the SSE body line by line and yields Deltas. It accumulates every tool
// call of the reply across their argument fragments — addressed by wire index, id, or
// last-addressed, and all emitted at the end, never mid-stream — skips a malformed event
// rather than failing the stream (counting it, so the terminal Delta reports how many were
// skipped and a stream that carried nothing else faults on the count), caps accumulated
// tool-call arguments, caps the total content plus reasoning text at maxReplyTextBytes, and
// emits a terminal Done with the last finish reason and any usage chunk — a port of the
// oracle's parseSSEStream. Returning false from yield (consumer broke) stops cleanly.
// carriedEffort is carried through from the request Stream built — the in-band error
// delta needs it, and this is the only seam between that request and the error it explains.
// Wire capture is not this parser's business: Client.Stream tees the body before it
// arrives here, so the parser reads exactly what it would read unobserved.
func (o *openaiCodec) parseSSE(body io.Reader, carriedEffort bool, yield func(Delta) bool) {
	scanner := newSSEScanner(body)

	var open openToolCalls
	var pendingFinish string
	var pendingUsage *Usage
	// The id the server put on the reply's chunks: the first one that names a model settles it
	// (every chunk of a reply repeats the same id), and it rides the terminal Done.
	var servedModel string

	// Running total of the content and reasoning bytes yielded so far. Tool-call bytes are
	// not summed here — openToolCalls carries its own maxToolCallBytes cap, on the sum
	// across every call it holds open.
	textBytes := 0
	// How many data: payloads failed to decode. Each is skipped so one bad chunk cannot kill
	// a stream that is otherwise fine, but never silently: the count rides the terminal Delta,
	// and a stream that yielded nothing else is faulted on it (finish below).
	malformed := 0

	// finish ends the stream on its success path: every accumulated tool call, then the
	// terminal Done. Both ends of a stream — the explicit [DONE] and the server closing the
	// connection — come through here, so the malformed-only fault is judged once: a stream
	// that yielded no text, no tool call, and skipped at least one chunk carried nothing the
	// consumer could commit, and an empty Done would let it pose as a finished empty reply.
	finish := func(reason string, usage *Usage) {
		if textBytes == 0 && len(open.entries) == 0 && malformed > 0 {
			yield(Delta{
				Kind:            DeltaError,
				Err:             fmt.Sprintf(malformedOnlyErrFmt, malformed),
				MalformedChunks: malformed,
			})
			return
		}
		if !open.flush(yield) {
			return
		}
		yield(Delta{Kind: DeltaDone, FinishReason: reason, Usage: usage, Model: servedModel, MalformedChunks: malformed})
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		if data == sseDone {
			reason := pendingFinish
			if reason == "" {
				reason = "stop"
			}
			finish(reason, pendingUsage)
			return
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			malformed++ // skip the event, as the oracle did — but on the record
			continue
		}
		if chunk.Error != nil {
			// An aggregator can answer HTTP 200 and put the provider's failure in-band. It is
			// terminal, and it must not fall through to the choice-less `continue` below — that
			// path ends at the implicit Done and commits a silent empty reply. Every tool call
			// accumulated so far is dropped with it — none has been emitted, because calls are
			// held until the stream ends: the reply is faulted, not partly usable.
			fault := o.client.inBandErrorDelta(*chunk.Error, data, carriedEffort)
			fault.MalformedChunks = malformed
			yield(fault)
			return
		}
		if servedModel == "" {
			servedModel = chunk.Model
		}
		if chunk.Usage != nil {
			usage := chunk.Usage.usage()
			pendingUsage = &usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		// The cap counts the CHOSEN reasoning field, never both: a proxy that duplicates the
		// channel into both spellings would otherwise be capped at half the real limit.
		thinking := choice.Delta.thinking()
		textBytes += len(choice.Delta.Content) + len(thinking)
		if textBytes > maxReplyTextBytes {
			// Terminal and NOT Retryable: the same request re-streamed would overflow again.
			// Returning here runs the deferred body close and wire-capture flush, as on every
			// other terminal path; the crossing chunk is never yielded, so what the consumer
			// received stays at or under the cap.
			yield(Delta{
				Kind: DeltaError,
				Err: fmt.Sprintf(
					"apogee: streamed reply exceeded the %d MiB text limit",
					maxReplyTextBytes>>20,
				),
			})
			return
		}
		if thinking != "" && !yield(Delta{Kind: DeltaThinking, Thinking: thinking}) {
			return
		}
		if choice.Delta.Content != "" && !yield(Delta{Kind: DeltaContent, Content: choice.Delta.Content}) {
			return
		}
		for _, frag := range choice.Delta.ToolCalls {
			if open.fold(frag) {
				yield(Delta{Kind: DeltaError, Err: open.tripped})
				return
			}
		}
		if choice.FinishReason != "" {
			pendingFinish = choice.FinishReason
		}
	}

	if err := scanner.Err(); err != nil {
		// A read that failed before the terminator. Retryable when the failure is the
		// connection's, not the reply's: a mid-stream EOF or a network timeout is the class an
		// HTTP-layer retry would have covered had it struck before the first byte, so the loop
		// gets to re-stream it exactly like an in-band 502 (isTransientReadError).
		yield(Delta{
			Kind:            DeltaError,
			Err:             fmt.Sprintf("apogee: read stream: %v", err) + malformedChunksNote(malformed),
			Retryable:       isTransientReadError(err),
			MalformedChunks: malformed,
		})
		return
	}

	// The stream ended without an explicit [DONE] (server closed the connection): flush
	// every accumulated tool call and emit a terminal Done, as the oracle does. The usage
	// chunk is not carried on this path, as it never was.
	finish("stop", nil)
}
