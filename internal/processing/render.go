package processing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// emptyArgsObject is the argument text a call whose arguments are not a JSON object is rendered
// with: such a call has no argument pairs to write, and an empty object is the one argument value
// every format parses back.
const emptyArgsObject = "{}"

// RenderToolCall writes call as the text a model on profile p emits for it — the inverse of the
// format's parser, so a past call can ride in an assistant message's content when the request
// carries no native tool calls. It reads the same profile knobs and defaults ParserFor and
// InstructionsFor read:
//
//   - markdown-fenced: one fenced tool block in the default markers. A string value is written
//     verbatim followed by one line break, the break the parser drops before the next marker or
//     the close; a value that starts with a line break gets one more in front, the leading break
//     the parser drops; every other value is its compact JSON text, which the parser decodes back
//     against the tool menu's schema (DecodeSchemaTypedArgs). Argument pairs are separated by a
//     blank line. A value holding a line that is exactly BEGIN_ARG or ``` cannot be written in
//     this format and reads back cut at that line — the format's own limit.
//   - custom-regex: the call in the pattern's literal delimiters (extractRegexDelimiters), the
//     name and the compact JSON arguments in the order the pattern's groups take, or
//     <tool_call>name(args)</tool_call> when the pattern lacks a name or an args group.
//
// Arguments that are not a JSON object render as no arguments at all. A native or zero profile
// returns "" with a nil error — its calls travel as native tool calls. An unknown tool-call
// format is an error, as it is for InstructionsFor.
func RenderToolCall(p domain.ModelProfile, call domain.ToolCall) (string, error) {
	switch format := ToolCallFormat(p.ToolCallFormat); format {
	case "", FormatNative:
		return "", nil
	case FormatMarkdownFenced:
		return renderFencedCall(MarkdownFencedConfig{}.withDefaults(), call), nil
	case FormatCustomRegex:
		return renderRegexCall(p.Pattern, call), nil
	default:
		return "", fmt.Errorf("processing: unknown tool-call format %q", format)
	}
}

// renderFencedCall writes call as one fenced tool block in cfg's markers (see RenderToolCall).
func renderFencedCall(cfg MarkdownFencedConfig, call domain.ToolCall) string {
	var b strings.Builder
	b.WriteString(codeFence + cfg.FenceLanguage + "\n")
	b.WriteString(cfg.NameField + "\n" + call.Tool + "\n")

	members, _ := objectMembers(call.Arguments)
	for i, m := range members {
		if i > 0 {
			// The parser keeps a mid-call value up to the next marker line and drops one trailing
			// break; the blank line is that break, so a value that itself ends in one keeps it.
			b.WriteString("\n")
		}
		b.WriteString(cfg.ArgStartField + "\n" + m.key + "\n" + cfg.ArgEndField + "\n")
		b.WriteString(fencedValue(m.value) + "\n")
	}

	b.WriteString(codeFence)
	return b.String()
}

// fencedValue is the text a fenced argument value is written as before its trailing line break:
// a string verbatim, with one extra leading break when it starts with one, and anything else as
// its compact JSON text.
func fencedValue(value json.RawMessage) string {
	var text string
	if json.Unmarshal(value, &text) != nil {
		return compactJSON(value)
	}
	if strings.HasPrefix(text, "\n") || strings.HasPrefix(text, "\r\n") {
		return "\n" + text
	}
	return text
}

// renderRegexCall writes call in pattern's literal delimiters and group order — the same
// regexDelimiters.call the instructions' example is written with — or the <tool_call>name(args)
// </tool_call> fallback when the pattern lacks a name or an args group (see RenderToolCall).
func renderRegexCall(pattern string, call domain.ToolCall) string {
	args := emptyArgsObject
	if _, isObject := objectMembers(call.Arguments); isObject {
		args = compactJSON(call.Arguments)
	}
	if d, ok := extractRegexDelimiters(pattern); ok {
		return d.call(call.Tool, args)
	}
	return fmt.Sprintf("<tool_call>%s(%s)</tool_call>", call.Tool, args)
}

// compactJSON is raw with its insignificant whitespace removed; raw is already valid JSON here,
// so the compaction cannot fail, and raw itself stands in should it ever.
func compactJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}
