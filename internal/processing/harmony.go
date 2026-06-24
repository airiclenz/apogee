package processing

import (
	"regexp"
	"strings"
)

// HarmonyChannel names a gpt-oss harmony message channel. The model wraps each message as
// `<|channel|>NAME<|message|>…<|end|>` (optionally prefixed by `<|start|>ROLE`), and the
// channel name decides where the content belongs: analysis and commentary are the model's
// private reasoning / tool-planning, while final is the user-facing answer.
type HarmonyChannel string

const (
	// HarmonyAnalysis carries chain-of-thought reasoning — stripped from visible content.
	HarmonyAnalysis HarmonyChannel = "analysis"
	// HarmonyCommentary carries tool-call planning — stripped from visible content.
	HarmonyCommentary HarmonyChannel = "commentary"
	// HarmonyFinal carries the user-facing answer — kept as visible content.
	HarmonyFinal HarmonyChannel = "final"
)

// harmonyMessage matches one harmony message: an optional `<|start|>role` prefix, the channel
// name, the message body up to a terminator, and the terminator itself. The body is captured
// lazily so the first terminator closes the message; an unterminated trailing message (the
// model is still streaming) is matched by harmonyOpen below.
//
// Terminators: `<|end|>` (message complete), `<|call|>` (a tool call follows), `<|return|>`
// (the assistant turn is done) — the harmony control tokens that close a message.
var harmonyMessage = regexp.MustCompile(
	`(?s)(?:<\|start\|>[^<]*)?<\|channel\|>\s*([a-zA-Z_][a-zA-Z0-9_]*)[^<]*<\|message\|>(.*?)(<\|end\|>|<\|call\|>|<\|return\|>)`,
)

// harmonyOpen matches an unterminated trailing harmony message — a channel opened with content
// but no closing control token yet (the streaming tail). The body runs to end of input.
var harmonyOpen = regexp.MustCompile(
	`(?s)(?:<\|start\|>[^<]*)?<\|channel\|>\s*([a-zA-Z_][a-zA-Z0-9_]*)[^<]*<\|message\|>(.*)$`,
)

// HarmonyStripped separates a harmony-formatted response into its three streams.
type HarmonyStripped struct {
	// Visible is the concatenation of the final-channel messages, trimmed — what the user sees.
	Visible string
	// Reasoning is the concatenation of the analysis-channel messages, joined by blank lines.
	Reasoning string
	// Commentary is the concatenation of the commentary-channel (tool-planning) messages,
	// joined by blank lines.
	Commentary string
	// HasReasoning reports whether at least one analysis or commentary message was found —
	// the signal to surface a reasoning channel.
	HasReasoning bool
}

// StripHarmony separates a full harmony-formatted message into its analysis, commentary, and
// final channels. It is the Phase-3 generalisation of StripThinking's single analysis-channel
// strip: where StripThinking treats `<|channel|>analysis<|message|>…<|end|>` as one
// thinking-token pair, StripHarmony parses every channel by name and routes each to the right
// stream, so commentary (tool planning) is also removed from visible content and a `<|call|>`
// or `<|return|>` terminator closes a message as cleanly as `<|end|>`.
//
// Any text outside a harmony message (a model that emits plain content between channels, or no
// channels at all) is treated as visible — a non-harmony response passes through untouched. An
// unterminated trailing message is captured by its channel as in-flight content, so a streaming
// analysis tail never leaks into Visible.
func StripHarmony(raw string) HarmonyStripped {
	var visible, reasoning, commentary []string
	hasReasoning := false
	pos := 0

	for pos < len(raw) {
		loc := harmonyMessage.FindStringSubmatchIndex(raw[pos:])
		if loc == nil {
			break
		}
		// loc offsets are relative to raw[pos:]; shift to absolute.
		msgStart := pos + loc[0]
		msgEnd := pos + loc[1]
		channel := HarmonyChannel(raw[pos+loc[2] : pos+loc[3]])
		body := raw[pos+loc[4] : pos+loc[5]]

		// Text before this message (outside any channel) is visible content.
		if lead := raw[pos:msgStart]; lead != "" {
			visible = append(visible, lead)
		}
		routeHarmony(channel, body, &visible, &reasoning, &commentary, &hasReasoning)
		pos = msgEnd
	}

	// A trailing unterminated message (streaming) or trailing plain text.
	if tail := raw[pos:]; tail != "" {
		if loc := harmonyOpen.FindStringSubmatch(tail); loc != nil {
			lead := tail[:strings.Index(tail, "<|channel|>")]
			if lead != "" {
				visible = append(visible, lead)
			}
			routeHarmony(HarmonyChannel(loc[1]), loc[2], &visible, &reasoning, &commentary, &hasReasoning)
		} else {
			visible = append(visible, tail)
		}
	}

	return HarmonyStripped{
		Visible:      strings.TrimSpace(strings.Join(visible, "")),
		Reasoning:    strings.Join(reasoning, "\n\n"),
		Commentary:   strings.Join(commentary, "\n\n"),
		HasReasoning: hasReasoning,
	}
}

// routeHarmony appends a message body to the stream its channel selects, marking reasoning
// when the channel is non-final. An unknown channel is treated conservatively as reasoning
// (it is not the user-facing final answer, so it is not leaked into visible content).
func routeHarmony(channel HarmonyChannel, body string, visible, reasoning, commentary *[]string, hasReasoning *bool) {
	switch channel {
	case HarmonyFinal:
		*visible = append(*visible, body)
	case HarmonyCommentary:
		*commentary = append(*commentary, body)
		*hasReasoning = true
	default: // analysis and any unrecognised channel
		*reasoning = append(*reasoning, body)
		*hasReasoning = true
	}
}

// IsHarmonyThinking reports whether raw ends inside an unterminated non-final harmony message —
// the streaming guard for the full channel set, the harmony analogue of IsThinking. A response
// whose last opened channel is final (or that has no open channel) is not thinking.
func IsHarmonyThinking(raw string) bool {
	lastOpen := strings.LastIndex(raw, "<|channel|>")
	if lastOpen == -1 {
		return false
	}
	// A terminator after the last opener means the message closed — not thinking.
	rest := raw[lastOpen:]
	if harmonyMessage.MatchString(rest) {
		return false
	}
	// The last message is open; thinking unless it is the final channel.
	m := harmonyOpen.FindStringSubmatch(rest)
	if m == nil {
		return false
	}
	return HarmonyChannel(m[1]) != HarmonyFinal
}
