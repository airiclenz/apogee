package processing

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// ErrMalformedToolCall is the sentinel a malformed tool call's marker error wraps — arguments
// that are not a JSON object (domain.MalformedArguments.Err). The rule is per call: the marked
// call stays in its batch with arguments "{}" and is answered on its own with the parse error,
// while its siblings dispatch (ADR 0007). Match it with errors.Is on the marker's Err.
var ErrMalformedToolCall = errors.New("processing: malformed tool call")

// NativeToolCall is one structured tool call as an OpenAI-compatible server delivers it —
// the "native"/JSON tool-call shape. It is the most common shape and the one the bench
// relies on: a server lacking native support is driven to emit it via grammar-constrained
// decoding. The provider extracts this wire shape but leaves Arguments unparsed (a
// JSON-encoded object string); processing owns the parse into domain.ToolCall. The loop
// adapts provider.ToolCall → this at the seam, so processing carries no dependency on the
// HTTP wire types (ADR 0010 — wire types stay provider-local).
type NativeToolCall struct {
	// ID links a later tool result back to this call; carried through verbatim.
	ID string
	// Name is the tool the model invoked.
	Name string
	// Arguments is the model-emitted JSON object string; "" (or whitespace) means a
	// no-argument call, which servers commonly emit for a parameterless tool.
	Arguments string
}

// ParseNativeToolCalls normalises native structured tool calls into domain.ToolCall, one call at
// a time, and returns every call in emitted order. An empty Arguments string is normalised to the
// empty object "{}". A call whose arguments are not a JSON object comes back with arguments "{}"
// and a Malformed marker carrying the parse error and the raw text (normalizeArguments), so the
// loop answers that call alone and dispatches its siblings. A call with no name comes back with an
// empty Tool, for the loop's dispatch filter (WellFormedToolCall) to drop and report. Nothing here
// fails the batch and nothing panics.
func ParseNativeToolCalls(calls []NativeToolCall) []domain.ToolCall {
	parsed := make([]domain.ToolCall, 0, len(calls))
	for _, call := range calls {
		parsed = append(parsed, parseNativeToolCall(call))
	}
	return parsed
}

// WellFormedToolCall reports whether a native tool call is one a loop could actually
// dispatch: a call needs a function name to route on and an id to key its result on. Servers
// that answer with a placeholder — `tool_calls:[{}]` for a call the model never produced —
// otherwise read as native tool-call evidence and raise a model's tier on nothing at all
// (probe C-18); dispatching such an entry is worse still, because echoing it back sends a
// tool message whose omitempty tool_call_id drops off the wire and leaves the server holding
// a result it cannot match.
//
// The predicate lives here so the probe's evidence filter and the loop's dispatch filter
// cannot drift: what the probe refuses to COUNT as a call is exactly what the loop refuses to
// RUN. It is deliberately not folded into ParseNativeToolCalls — that parse returns every call,
// a name-less one with an empty Tool, and leaves the dropping to the callers here, which drop the
// unusable entries and keep the rest.
//
// It takes the two strings rather than a call type because its callers hold different ones:
// the probe reads the provider's wire shape, which processing must not import (ADR 0010).
func WellFormedToolCall(name, id string) bool {
	return name != "" && id != ""
}

// parseNativeToolCall normalises a single native call: its name trimmed (empty when the server
// sent none) and its arguments normalised, marked when they are not a JSON object.
func parseNativeToolCall(call NativeToolCall) domain.ToolCall {
	args, malformed := normalizeArguments(call.Arguments)
	return domain.ToolCall{
		ID:        call.ID,
		Tool:      strings.TrimSpace(call.Name),
		Arguments: args,
		Malformed: malformed,
	}
}

// notAJSONObject is the parse error a malformed call's marker carries for arguments that are
// valid JSON but not an object — tool arguments are always an object on the OpenAI wire.
const notAJSONObject = "not a JSON object"

// normalizeArguments is the one home of the tool-argument rule: it returns the model-emitted
// argument string as a JSON object, or the empty object "{}" with a Malformed marker when the
// string is not one. Empty/whitespace is a no-argument call and becomes "{}" unmarked. Anything
// else must be syntactically valid JSON — the marker's error is the encoding/json syntax-error
// text otherwise — and an object (notAJSONObject otherwise). The marker keeps the raw text
// exactly as sent; its error wraps ErrMalformedToolCall.
func normalizeArguments(raw string) (json.RawMessage, *domain.MalformedArguments) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return json.RawMessage("{}"), nil
	}

	var probe json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
		return malformedArguments(raw, err.Error())
	}
	if trimmed[0] != '{' {
		return malformedArguments(raw, notAJSONObject)
	}
	return json.RawMessage(trimmed), nil
}

// malformedArguments is normalizeArguments' marked result: the empty object, and the marker
// carrying raw and the parse failure reason.
func malformedArguments(raw, reason string) (json.RawMessage, *domain.MalformedArguments) {
	return json.RawMessage("{}"), &domain.MalformedArguments{Err: malformedArgumentsError{reason: reason}, Raw: raw}
}

// malformedArgumentsError is a Malformed marker's Err: its text is the parse failure alone, the
// words the model is answered with, and it matches ErrMalformedToolCall under errors.Is.
type malformedArgumentsError struct {
	reason string
}

// Error is the parse failure, worded for the model.
func (e malformedArgumentsError) Error() string { return e.reason }

// Unwrap ties the failure to ErrMalformedToolCall.
func (e malformedArgumentsError) Unwrap() error { return ErrMalformedToolCall }
