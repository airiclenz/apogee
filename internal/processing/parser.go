package processing

import "github.com/airiclenz/apogee/internal/domain"

// ToolCallParser extracts a model's tool call from the assistant's raw response text. It is
// the text-format counterpart to ParseNativeToolCalls: where the native path receives a
// structured tool_calls array from the server, a text-format parser recovers the call from
// the visible content a server emits inline (markdown-fenced or a custom regex). It mirrors
// the apogee-code ToolCallParser oracle (markdown-fenced + custom-regex), the riskiest port.
//
// Implementations parse at most one call per response (the oracle's single-call contract)
// and degrade silently to the no-call path rather than erroring: a response without a
// recognisable call returns found == false, so a plain assistant turn is never a failure.
// Malformed structure never panics — it is reported as no call (the P1.3 parse-error
// contract). The returned domain.ToolCall carries an empty ID (text formats name no call ID;
// the loop assigns one downstream).
type ToolCallParser interface {
	// ParseToolCall extracts a tool call from raw; found is false when none is present.
	ParseToolCall(raw string) (call domain.ToolCall, found bool)
	// StripToolCall returns raw with the parsed call's markup removed and trimmed — the
	// visible content that survives into conversation history when a call is present.
	StripToolCall(raw string) string
}
