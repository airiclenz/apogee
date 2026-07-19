package tools

import (
	"bytes"
	"encoding/json"

	"github.com/airiclenz/apogee/internal/domain"
)

// Size and result ceilings, ported from the TS oracle's tool-constants. They bound
// the bytes a single tool call can read, write, or surface so one call cannot exhaust
// memory or flood the model's context.
const (
	maxFileReadBytes    = 10 * 1024 * 1024 // read_file refuses a file larger than this
	maxFileContentBytes = 512 * 1024       // write_file refuses content larger than this
	maxDirEntries       = 1000             // list_dir caps the entries it collects
	maxDirDepthLimit    = 10               // hard ceiling on list_dir recursion depth
	defaultDirDepth     = 3                // list_dir recursion depth when unspecified
	defaultGrepResults  = 50               // grep result count when unspecified
	maxGrepFileBytes    = 5 * 1024 * 1024  // grep skips a file larger than this
)

// toolSpec is a built-in tool's model-facing identity — the stable name the model
// calls, the model-facing description, and the raw JSON argument schema (kept as a
// visible, reviewable string; no generation — ADR 0002, plan D7). A tool embeds one
// spec value and gains the three domain.Tool metadata methods from it, instead of
// hand-rolling a schema var and three methods per tool.
type toolSpec struct {
	name        string
	description string
	schema      json.RawMessage
}

// Name returns the stable identifier the model calls.
func (s toolSpec) Name() string { return s.name }

// Description returns the model-facing summary of the tool.
func (s toolSpec) Description() string { return s.description }

// Schema returns the JSON schema of the tool's arguments.
func (s toolSpec) Schema() json.RawMessage { return s.schema }

// okResult builds a success ToolResult for callID.
func okResult(callID, content string) domain.ToolResult {
	return domain.ToolResult{CallID: callID, Content: content}
}

// errorResult builds a tool-level failure ToolResult — surfaced to the model rather
// than returned as a Go error, which is reserved for ctx cancellation (ADR 0007).
func errorResult(callID, message string) domain.ToolResult {
	return domain.ToolResult{CallID: callID, Content: message, IsError: true}
}

// decodeArgs unmarshals a tool call's raw arguments into dst, treating empty or
// whitespace-only arguments as the empty object so a parameterless call decodes to
// the zero value rather than failing.
func decodeArgs(raw json.RawMessage, dst any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.Unmarshal([]byte("{}"), dst)
	}
	return json.Unmarshal(raw, dst)
}

// decodeToolArgs decodes call's raw arguments into an A, folding the decode-and-error
// preamble every Execute repeated: on a decode failure it returns ok=false and the
// standard "invalid arguments" error ToolResult in fail, which the caller returns
// as-is (with a nil Go error — a bad argument is the model's mistake to see and
// correct, never a Go error, ADR 0007).
func decodeToolArgs[A any](call domain.ToolCall) (args A, fail domain.ToolResult, ok bool) {
	if err := decodeArgs(call.Arguments, &args); err != nil {
		return args, errorResult(call.ID, "invalid arguments: "+err.Error()), false
	}
	return args, domain.ToolResult{}, true
}
