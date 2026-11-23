package provider

import (
	"encoding/base64"
	"encoding/json"
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
// thinking effort in any dialect (see chatRequest.carriesEffort). This wire replays no signed
// thinking, so the report carries no prefix digest.
func (o *openaiCodec) encode(req Request) ([]byte, sentRequest, error) {
	wire := o.buildBody(req)
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, sentRequest{}, err
	}
	return body, sentRequest{carriesEffort: wire.carriesEffort()}, nil
}

// decodeWhole decodes one non-streamed reply. A body whose error member is set comes back
// as that member with a zero RawResponse — an aggregator can answer HTTP 200 and put the
// provider's failure in the body, and it must not fall through to toRawResponse, which maps
// such a reply's zero choices to a silent zero RawResponse. A body that fails to decode
// returns the bare decode error for the Client to wrap. Nothing of the sent request shapes the
// decode on this wire.
func (o *openaiCodec) decodeWhole(body io.Reader, _ sentRequest) (RawResponse, *wireError, error) {
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
// not told to produce) and an assistant message's tool calls are rendered as text after its
// content (toolCallsAsText) — a request offering no tools, the compaction summariser's, must
// neither drop them nor send a content-less assistant turn a validating server refuses. With
// native tools the tool linkage is preserved, and content is null when an assistant message
// carries only tool calls (OpenAI's convention). content is an array of parts when the
// message carries images (chatParts).
func formatMessage(m Message, hasTools bool) chatMessage {
	if !hasTools && m.Role == "tool" {
		return chatMessage{Role: "user", Content: chatText(m.Content)}
	}
	if !hasTools && len(m.ToolCalls) > 0 {
		m.Content = toolCallsAsText(m.Content, m.ToolCalls)
	}

	out := chatMessage{Role: m.Role}
	switch {
	case len(m.Images) > 0:
		out.Content = chatParts(m.Content, m.Images)
	case len(m.ToolCalls) > 0 && m.Content == "":
		// The zero chatContent: null for a tool-call-only assistant turn.
	default:
		out.Content = chatText(m.Content)
	}
	if hasTools {
		out.ToolCallID = m.ToolCallID
		out.ToolCalls = m.ToolCalls
	}
	return out
}

// toolCallsAsText is content with each call appended on a line of its own in the prose form the
// anthropic codec gives a tool-less request (anthropicToolCallText), so both wires tell a
// tool-less model about a past call in the same words.
func toolCallsAsText(content string, calls []ToolCall) string {
	lines := make([]string, 0, len(calls)+1)
	if content != "" {
		lines = append(lines, content)
	}
	for _, tc := range calls {
		lines = append(lines, anthropicToolCallText(tc))
	}
	return strings.Join(lines, "\n")
}

// chatText is the plain-string content member.
func chatText(text string) chatContent {
	return chatContent{text: &text}
}

// chatParts is the multimodal content member: one text part for text (none when it is empty),
// then one image_url part per image, in order, each carrying the image as a base64 data URL.
func chatParts(text string, images []Image) chatContent {
	parts := make([]chatContentPart, 0, len(images)+1)
	if text != "" {
		parts = append(parts, chatContentPart{Type: "text", Text: text})
	}
	for _, img := range images {
		url := "data:" + img.MediaType + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
		parts = append(parts, chatContentPart{Type: "image_url", ImageURL: &chatImageURL{URL: url}})
	}
	return chatContent{parts: parts}
}

// parseSSE decodes one chat-completions SSE body into what a reply accumulator is told: it
// keeps the dialect and nothing of how a stream ends. The scan loop, the text and tool-call
// caps, the count of skipped events, the read fault and every terminal Delta are the
// accumulator's (replyAccumulator); this codec decodes each `data:` payload and names what it
// carries — the [DONE] terminator, an in-band error, the served model, a usage chunk, the
// chosen reasoning text and the content, tool-call fragments (addressed by wire index, id, or
// last-addressed, and held until the stream ends), and the raw finish_reason, which is already
// in the loop's vocabulary. It has no epilogue.
// sent.carriesEffort is carried through from the request Stream built — the in-band error
// delta needs it, and this is the only seam between that request and the error it explains.
// toolFragment, when non-nil, runs on every tool-call fragment as it arrives (openToolCalls).
// Wire capture is not this parser's business: Client.Stream tees the body before it
// arrives here, so the parser reads exactly what it would read unobserved.
func (o *openaiCodec) parseSSE(body io.Reader, sent sentRequest, toolFragment func(), yield func(Delta) bool) {
	acc := newReplyAccumulator(toolFragment, yield, nil)
	// The id the server put on the reply's chunks: the first one that names a model settles it
	// (every chunk of a reply repeats the same id), and it rides the terminal Delta.
	modelSettled := false

	acc.run(body, func(data string) (ended bool) {
		if data == sseDone {
			acc.finish()
			return true
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			acc.skipMalformed() // skip the event, as the oracle did — but on the record
			return false
		}
		if chunk.Error != nil {
			// An aggregator can answer HTTP 200 and put the provider's failure in-band. It is
			// terminal, and it must not fall through to the choice-less return below — that
			// path ends at the stream's close and commits a silent empty reply.
			return acc.fail(o.client.inBandErrorDelta(*chunk.Error, data, sent.carriesEffort))
		}
		if !modelSettled && chunk.Model != "" {
			acc.setModel(chunk.Model)
			modelSettled = true
		}
		if chunk.Usage != nil {
			acc.setUsage(chunk.Usage.usage())
		}
		if len(chunk.Choices) == 0 {
			return false
		}

		choice := chunk.Choices[0]
		// The cap counts the CHOSEN reasoning field, never both: a proxy that duplicates the
		// channel into both spellings would otherwise be capped at half the real limit. A
		// chunk's content and thinking are charged together, before either is yielded, so a
		// chunk that crosses the cap yields nothing.
		thinking := choice.Delta.thinking()
		if acc.charge(len(choice.Delta.Content) + len(thinking)) {
			return true
		}
		if thinking != "" && acc.emit(Delta{Kind: DeltaThinking, Thinking: thinking}) {
			return true
		}
		if choice.Delta.Content != "" && acc.emit(Delta{Kind: DeltaContent, Content: choice.Delta.Content}) {
			return true
		}
		for _, frag := range choice.Delta.ToolCalls {
			if acc.foldCall(frag) {
				return true
			}
		}
		if choice.FinishReason != "" {
			acc.setFinish(choice.FinishReason)
		}
		return false
	})
}
