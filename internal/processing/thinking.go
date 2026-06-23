package processing

import "strings"

// ThinkingConfig describes a model's inline thinking-channel delimiters — the literal
// tokens that bracket reasoning the user should not see. Two shapes are common: gemma's
// `<think>…</think>` and gpt-oss's harmony `<|channel|>analysis<|message|>…<|end|>`. The
// tokens are matched literally (not as regular expressions). A nil *ThinkingConfig means
// the model emits no inline channel — content passes through untouched, which is also the
// right default when the server already split reasoning into the response's separate
// reasoning_content field (provider.RawResponse.Thinking).
type ThinkingConfig struct {
	// StartToken opens a thinking span; EndToken closes it. Both must be non-empty for
	// stripping to run — an empty token degrades to the no-op pass-through.
	StartToken string
	EndToken   string
}

// Stripped separates a raw assistant message into the content the user sees and the
// reasoning removed from it.
type Stripped struct {
	// Visible is the message with every closed thinking span removed, trimmed of
	// surrounding whitespace.
	Visible string
	// Reasoning is the concatenation of the stripped spans, joined by a blank line. It
	// is empty when the message carried no span; HasReasoning distinguishes that absence
	// from a present-but-empty span (`<think></think>`).
	Reasoning string
	// HasReasoning reports whether at least one span was found (even an empty one) — the
	// signal a consumer uses to decide whether to surface a reasoning channel at all.
	HasReasoning bool
}

// StripThinking separates raw into visible content and stripped reasoning per cfg. It
// mirrors the apogee-code ThinkingStripper.strip oracle: multiple spans accumulate, an
// unclosed trailing span (the model is still streaming its reasoning) is captured as
// reasoning with an empty visible tail, and visible content is trimmed. A nil cfg — or
// one with an empty token — returns the whole message as visible with no reasoning.
func StripThinking(raw string, cfg *ThinkingConfig) Stripped {
	if cfg == nil || cfg.StartToken == "" || cfg.EndToken == "" {
		return Stripped{Visible: raw}
	}

	var visible, reasoning []string
	pos := 0
	for pos < len(raw) {
		start := indexFrom(raw, cfg.StartToken, pos)
		if start == -1 {
			visible = append(visible, raw[pos:])
			break
		}
		visible = append(visible, raw[pos:start])

		spanStart := start + len(cfg.StartToken)
		end := indexFrom(raw, cfg.EndToken, spanStart)
		if end == -1 {
			// Unclosed span: everything after the opener is in-flight reasoning.
			reasoning = append(reasoning, raw[spanStart:])
			break
		}
		reasoning = append(reasoning, raw[spanStart:end])
		pos = end + len(cfg.EndToken)
	}

	return Stripped{
		Visible:      strings.TrimSpace(strings.Join(visible, "")),
		Reasoning:    strings.Join(reasoning, "\n\n"),
		HasReasoning: len(reasoning) > 0,
	}
}

// IsThinking reports whether raw ends inside an unclosed thinking span — the streaming
// guard a consumer uses to know the model is mid-reasoning and hold display. It mirrors
// the oracle's isThinking: the last opener has no closer after it. A nil/empty cfg is
// never thinking.
func IsThinking(raw string, cfg *ThinkingConfig) bool {
	if cfg == nil || cfg.StartToken == "" || cfg.EndToken == "" {
		return false
	}
	lastStart := strings.LastIndex(raw, cfg.StartToken)
	if lastStart == -1 {
		return false
	}
	return indexFrom(raw, cfg.EndToken, lastStart+len(cfg.StartToken)) == -1
}

// indexFrom returns the index of sub in s at or after from, or -1 if absent — the
// substring search the oracle expresses as String.indexOf(sub, from).
func indexFrom(s, sub string, from int) int {
	if from > len(s) {
		return -1
	}
	i := strings.Index(s[from:], sub)
	if i == -1 {
		return -1
	}
	return from + i
}
