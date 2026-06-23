package processing

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// ErrMalformedToolCall is the sentinel a tool-call parse failure wraps — a missing tool
// name, or arguments that are not a JSON object. The loop matches it with errors.Is to
// degrade a bad call to a tool-error path (ADR 0007) rather than failing the Turn.
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

// ParseNativeToolCalls normalises native structured tool calls into domain.ToolCall. Each
// call must name a tool and carry a well-formed JSON object for arguments; an empty
// Arguments string is normalised to the empty object "{}". Parsing is atomic: a single
// malformed call returns an ErrMalformedToolCall-wrapped error and no results, so a
// partially-parsed batch never reaches dispatch. A malformed call is reported through err,
// never a panic (the parse-error path the acceptance gate requires).
func ParseNativeToolCalls(calls []NativeToolCall) ([]domain.ToolCall, error) {
	parsed := make([]domain.ToolCall, 0, len(calls))
	for i, call := range calls {
		one, err := parseNativeToolCall(call)
		if err != nil {
			return nil, fmt.Errorf("processing: tool call %d: %w", i, err)
		}
		parsed = append(parsed, one)
	}
	return parsed, nil
}

// parseNativeToolCall normalises a single native call, validating its name and arguments.
func parseNativeToolCall(call NativeToolCall) (domain.ToolCall, error) {
	name := strings.TrimSpace(call.Name)
	if name == "" {
		return domain.ToolCall{}, fmt.Errorf("%w: missing tool name", ErrMalformedToolCall)
	}

	args, err := normalizeArguments(call.Arguments)
	if err != nil {
		return domain.ToolCall{}, fmt.Errorf("%w: tool %q: %v", ErrMalformedToolCall, name, err)
	}

	return domain.ToolCall{ID: call.ID, Tool: name, Arguments: args}, nil
}

// normalizeArguments validates the model-emitted argument string and returns it as a
// JSON object. Empty/whitespace becomes "{}"; anything else must be syntactically valid
// JSON and an object (tool arguments are always an object on the OpenAI wire).
func normalizeArguments(raw string) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return json.RawMessage("{}"), nil
	}
	if !json.Valid([]byte(trimmed)) {
		return nil, errors.New("arguments are not valid JSON")
	}
	if trimmed[0] != '{' {
		return nil, errors.New("arguments are not a JSON object")
	}
	return json.RawMessage(trimmed), nil
}
