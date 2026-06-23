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
