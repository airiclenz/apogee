package processing

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// CustomRegexConfig configures the custom-regex tool-call format: a single regular expression
// with named capture groups for the tool name and its arguments. It is the escape hatch for a
// model whose tool-call markup matches neither the native nor the markdown-fenced shape.
type CustomRegexConfig struct {
	// Pattern is the regular expression matched against the visible content. JavaScript-style
	// named groups (?<name>…) are accepted and rewritten to Go's (?P<name>…); the apogee-code
	// vectors are written in the JS form. An invalid pattern degrades to a never-match parser.
	Pattern string
	// Flags are single-letter regex flags (subset of s, i, m); default "s" (dot matches
	// newline) to mirror the oracle. Unsupported letters are ignored.
	Flags string
	// NameGroup is the capture group holding the tool name (default "name").
	NameGroup string
	// ArgsGroup is the capture group holding the JSON arguments (default "args").
	ArgsGroup string
}

// withDefaults returns cfg with empty fields replaced by the apogee-code oracle defaults.
func (c CustomRegexConfig) withDefaults() CustomRegexConfig {
	if c.Flags == "" {
		c.Flags = "s"
	}
	if c.NameGroup == "" {
		c.NameGroup = "name"
	}
	if c.ArgsGroup == "" {
		c.ArgsGroup = "args"
	}
	return c
}

// CustomRegexParser extracts a tool call by matching a user-supplied regex with named groups.
// It ports the apogee-code CustomRegexParser oracle. An invalid pattern is non-fatal: the
// parser compiles to a regex that never matches, so it silently finds no call (the oracle's
// console.warn-and-fallback behaviour). The parser is stateless and safe for concurrent use.
type CustomRegexParser struct {
	cfg     CustomRegexConfig
	pattern *regexp.Regexp
}

// neverMatch is the fallback for an invalid pattern — Go's RE2 has no always-fail literal, so
// an empty negated character class `[^\x00-\x{10FFFF}]` (matching no rune) stands in.
var neverMatch = regexp.MustCompile(`[^\x00-\x{10FFFF}]`)

// NewCustomRegexParser builds a parser for the custom-regex format from cfg (empty fields take
// the oracle defaults). An invalid Pattern compiles to a never-match parser rather than failing.
func NewCustomRegexParser(cfg CustomRegexConfig) *CustomRegexParser {
	cfg = cfg.withDefaults()
	pattern := neverMatch
	if compiled, err := compilePattern(cfg.Pattern, cfg.Flags); err == nil {
		pattern = compiled
	}
	return &CustomRegexParser{cfg: cfg, pattern: pattern}
}

// ParseToolCall extracts a tool call from raw using the configured pattern. found is false
// when the pattern does not match or the name group is empty.
func (p *CustomRegexParser) ParseToolCall(raw string) (domain.ToolCall, bool) {
	match := p.pattern.FindStringSubmatch(raw)
	if match == nil {
		return domain.ToolCall{}, false
	}

	name := p.group(match, p.cfg.NameGroup)
	if name == "" {
		return domain.ToolCall{}, false
	}

	args := p.coerceArgs(p.group(match, p.cfg.ArgsGroup))
	return domain.ToolCall{Tool: name, Arguments: args}, true
}

// StripToolCall returns raw with every match of the pattern removed and trimmed.
func (p *CustomRegexParser) StripToolCall(raw string) string {
	return strings.TrimSpace(p.pattern.ReplaceAllString(raw, ""))
}

// coerceArgs mirrors the oracle: an empty args group yields {}; a valid-JSON-object group is
// kept verbatim; any other non-empty group becomes {"raw": "<group>"} (the graceful non-JSON
// path). The result is always a JSON object so it slots into domain.ToolCall.Arguments.
func (p *CustomRegexParser) coerceArgs(argsStr string) json.RawMessage {
	if argsStr == "" {
		return json.RawMessage("{}")
	}
	trimmed := strings.TrimSpace(argsStr)
	if trimmed != "" && trimmed[0] == '{' && json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	return marshalArgs(map[string]json.RawMessage{"raw": tryParseValue(argsStr)})
}

// group returns the named capture group's value, or "" when the group is absent or unmatched.
func (p *CustomRegexParser) group(match []string, name string) string {
	for i, n := range p.pattern.SubexpNames() {
		if n == name && i < len(match) {
			return match[i]
		}
	}
	return ""
}

// compilePattern rewrites JS-style named groups to Go's syntax, prepends the supported flags,
// and compiles the result. An invalid pattern returns the compile error (caller falls back).
func compilePattern(pattern, flags string) (*regexp.Regexp, error) {
	translated := jsNamedGroup.ReplaceAllString(pattern, "(?P<$1>")
	if prefix := goFlagPrefix(flags); prefix != "" {
		translated = prefix + translated
	}
	return regexp.Compile(translated)
}

// goFlagPrefix maps the supported single-letter flags to a Go inline-flag prefix, e.g. "si" →
// "(?si)". Unsupported letters (notably the JS global flag g) are ignored — Go has no g flag.
func goFlagPrefix(flags string) string {
	var b strings.Builder
	for _, f := range flags {
		switch f {
		case 's', 'i', 'm':
			b.WriteRune(f)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "(?" + b.String() + ")"
}

// jsNamedGroup matches a JavaScript named-capture opener (?<name> so it can be rewritten to
// Go's (?P<name>. A Go-style group already containing ?P is left untouched.
var jsNamedGroup = regexp.MustCompile(`\(\?<([a-zA-Z_][a-zA-Z0-9_]*)>`)
