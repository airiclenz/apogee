package processing

import (
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// ToolCallFormat identifies how a model emits tool calls, so the factory can select the
// matching parser. It mirrors apogee-code's ToolCallingConfig.format.
type ToolCallFormat string

const (
	// FormatNative is the structured tool_calls path: the server delivers calls out-of-band and
	// ParseNativeToolCalls handles them, so the text parser finds nothing in the visible content.
	FormatNative ToolCallFormat = "native"
	// FormatMarkdownFenced is the markdown-fenced code-block format (MarkdownFencedParser).
	FormatMarkdownFenced ToolCallFormat = "markdown-fenced"
	// FormatCustomRegex is the user-supplied named-group regex format (CustomRegexParser).
	FormatCustomRegex ToolCallFormat = "custom-regex"
)

// ToolCallingConfig selects and configures a text-format tool-call parser. Only the config
// matching Format is consulted; the others are ignored. A zero Format defaults to native.
type ToolCallingConfig struct {
	// Format selects the parser; "" is treated as native.
	Format ToolCallFormat
	// MarkdownFenced configures the markdown-fenced parser (used when Format is markdown-fenced).
	MarkdownFenced MarkdownFencedConfig
	// CustomRegex configures the custom-regex parser (used when Format is custom-regex).
	CustomRegex CustomRegexConfig
}

// NewToolCallParser selects the text-format parser for cfg.Format. It is the parser half of
// apogee-code's ProcessorFactory: native returns the no-op text parser (the structured path
// owns native calls), markdown-fenced and custom-regex return their respective parsers. An
// unrecognised format is an error so a misconfigured model fails loudly rather than silently
// parsing nothing.
func NewToolCallParser(cfg ToolCallingConfig) (ToolCallParser, error) {
	switch cfg.Format {
	case "", FormatNative:
		return nativeTextParser{}, nil
	case FormatMarkdownFenced:
		return NewMarkdownFencedParser(cfg.MarkdownFenced), nil
	case FormatCustomRegex:
		return NewCustomRegexParser(cfg.CustomRegex), nil
	default:
		return nil, fmt.Errorf("processing: unknown tool-call format %q", cfg.Format)
	}
}

// nativeTextParser is the no-op text parser for the native format: native calls arrive as a
// structured tool_calls array (ParseNativeToolCalls), never in the visible text, so there is
// nothing to extract or strip here. It mirrors apogee-code's NativeToolParser.
type nativeTextParser struct{}

// ParseToolCall always reports no call — native calls do not appear in text.
func (nativeTextParser) ParseToolCall(string) (domain.ToolCall, bool) {
	return domain.ToolCall{}, false
}

// StripToolCall returns raw unchanged — there is no inline markup to remove for native calls.
func (nativeTextParser) StripToolCall(raw string) string { return raw }
