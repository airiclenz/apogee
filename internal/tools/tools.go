package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strings"

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
	maxDiffTableCells   = 25_000_000       // view_diff refuses an LCS table larger than this (~200 MiB)
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

// okSummary builds a success ToolResult carrying both halves of the outcome: the prose
// content the model reads and the structured summary a host renders (domain.ToolSummary).
// A tool with nothing structured to report uses okResult and the host reads the prose.
// There is deliberately no error-carrying twin: a failed call has no outcome to describe
// beyond IsError, which the host renders itself.
func okSummary(callID, content string, summary domain.ToolSummary) domain.ToolResult {
	return domain.ToolResult{CallID: callID, Content: content, Summary: summary}
}

// errorResult builds a tool-level failure ToolResult — surfaced to the model rather
// than returned as a Go error, which is reserved for ctx cancellation (ADR 0007).
func errorResult(callID, message string) domain.ToolResult {
	return domain.ToolResult{CallID: callID, Content: message, IsError: true}
}

// rowBreakEscaper spells the two line-break characters as backslash-letter pairs. It is a
// package-level value because strings.NewReplacer builds a matcher once and is safe for
// concurrent use by every tool call.
var rowBreakEscaper = strings.NewReplacer("\r", `\r`, "\n", `\n`)

// escapeRowBreaks rewrites the line breaks in s as the two-character spellings `\r` and `\n`,
// and changes nothing else. Several tool results use a grammar of one row per line, where a
// path is DATA inside a row: a filename may legally carry a line break on POSIX, and pasted
// verbatim it would forge extra rows — a header, a match, a truncation note — that the model
// reads as the tool's own words. Escaping rather than folding or dropping the break keeps the
// name recoverable: the reader still sees exactly which bytes the filename holds.
func escapeRowBreaks(s string) string {
	return rowBreakEscaper.Replace(s)
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

// CanonicalArgs re-encodes a tool call's raw arguments in the one spelling every reader of
// those bytes can agree on: object keys sorted, a duplicated key collapsed to the occurrence
// that WINS (the last — decodeArgs above is stdlib JSON, and so is every guard reading the same
// call), insignificant whitespace dropped, and empty arguments canonicalised to the empty
// object exactly as decodeArgs decodes them.
//
// It exists so a decision made ABOUT a call — today the allow-for-session key a Gate carries
// (internal/agent) — can be keyed on what the executor will actually RUN rather than on the byte
// spelling the model happened to emit: two spellings of one executed call produce identical
// bytes, and two calls the executor would run differently never do. Scalars keep their wire
// bytes for the sake of that second half; re-marshalling decoded values instead would round a
// large integer and replace invalid UTF-8, quietly mapping two different executed calls onto one
// canonical form.
//
// Arguments the executor itself would reject are reported as an error rather than canonicalised,
// so a caller keying on the result can refuse to remember anything about a call that will not
// decode.
func CanonicalArgs(raw json.RawMessage) ([]byte, error) {
	// Validate through the executor's own decode path, so exactly the blobs a tool would run
	// are the blobs that get a canonical form.
	var probe any
	if err := decodeArgs(raw, &probe); err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return []byte("{}"), nil
	}
	return canonicalJSON(trimmed)
}

// canonicalJSON canonicalises one already-validated JSON value: an object is re-emitted with its
// keys sorted, an array keeps its wire order (order is meaning there), and any scalar is
// compacted, which loses only insignificant whitespace.
func canonicalJSON(raw json.RawMessage) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("empty JSON value")
	}
	switch trimmed[0] {
	case '{':
		return canonicalObject(trimmed)
	case '[':
		return canonicalArray(trimmed)
	default:
		var out bytes.Buffer
		if err := json.Compact(&out, trimmed); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}
}

// canonicalObject re-emits a JSON object with its keys sorted. Decoding into a map is what
// collapses a duplicated key to its LAST occurrence — the same value stdlib JSON hands the
// executor — so the canonical form describes the call that will actually run.
func canonicalObject(raw json.RawMessage) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var out bytes.Buffer
	out.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			out.WriteByte(',')
		}
		encoded, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		out.Write(encoded)
		out.WriteByte(':')
		value, err := canonicalJSON(fields[key])
		if err != nil {
			return nil, err
		}
		out.Write(value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

// canonicalArray re-emits a JSON array in wire order, canonicalising each element.
func canonicalArray(raw json.RawMessage) ([]byte, error) {
	var elements []json.RawMessage
	if err := json.Unmarshal(raw, &elements); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	out.WriteByte('[')
	for i, element := range elements {
		if i > 0 {
			out.WriteByte(',')
		}
		value, err := canonicalJSON(element)
		if err != nil {
			return nil, err
		}
		out.Write(value)
	}
	out.WriteByte(']')
	return out.Bytes(), nil
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
