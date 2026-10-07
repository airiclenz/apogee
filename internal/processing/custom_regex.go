package processing

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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
// It ports the apogee-code CustomRegexParser oracle, except for arguments that are not a JSON
// object: the oracle wrapped them as {"raw": …}, a key the model never sent, while this parser
// marks the call malformed (ParseToolCall). An invalid pattern is non-fatal: the parser
// compiles to a regex that never matches, so it silently finds no call (the oracle's
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
// when the pattern does not match or the name group is empty. The args group follows the native
// argument rule (normalizeArguments): an empty or whitespace-only group is the empty object, a
// JSON-object group is kept verbatim, and any other group comes back as "{}" marked Malformed —
// the parse error and the group text — so the loop answers it exactly as a malformed native call.
func (p *CustomRegexParser) ParseToolCall(raw string) (domain.ToolCall, bool) {
	match := p.pattern.FindStringSubmatch(raw)
	if match == nil {
		return domain.ToolCall{}, false
	}

	name := p.group(match, p.cfg.NameGroup)
	if name == "" {
		return domain.ToolCall{}, false
	}

	args, malformed := normalizeArguments(p.group(match, p.cfg.ArgsGroup))
	return domain.ToolCall{Tool: name, Arguments: args, Malformed: malformed}, true
}

// StripToolCall returns raw with every match of the pattern removed and trimmed.
func (p *CustomRegexParser) StripToolCall(raw string) string {
	return strings.TrimSpace(p.pattern.ReplaceAllString(raw, ""))
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

// ErrCustomRegexPattern marks a custom-regex profile whose pattern cannot carry a tool call: it
// does not compile, or it lacks the name or args capture group.
var ErrCustomRegexPattern = errors.New("unusable custom-regex tool-call pattern")

// ErrCustomRegexExample marks a custom-regex profile with no example call its own pattern parses:
// the given example does not parse to a tool and a JSON-object argument, or the call derived from
// the pattern does not parse back to itself.
var ErrCustomRegexExample = errors.New("no parseable custom-regex tool-call example")

// probeExampleCall is the fixed call the load check derives from a pattern when the profile gives
// no example, and the call the instructions show when no menu tool's derived call parses back.
var probeExampleCall = exampleToolCall{toolName: "read_file", argName: "path", argValue: "src/main.ts"}

// ValidateCustomRegexProfile reports whether a custom-regex profile can show its model a call its
// pattern parses: the pattern must compile and carry a name and an args group, and the example —
// or, when example is empty, the probe call read_file {"path": "src/main.ts"} written in the
// pattern's delimiters — must parse back through the pattern to a tool and a JSON-object argument.
// The error quotes the failing example and wraps ErrCustomRegexPattern or ErrCustomRegexExample.
func ValidateCustomRegexProfile(pattern, example string) error {
	_, err := customRegexExample(pattern, example, nil)
	return err
}

// customRegexExample is the one home of the custom-regex example rule, read by both the load check
// and the instructions. It returns the call to show the model and the rule's verdict:
//
//   - a non-empty example is shown verbatim; the error reports a pattern fault, or an example the
//     pattern does not parse to a tool with JSON-object arguments.
//   - otherwise a pattern that does not compile or lacks a name or args group shows nothing and
//     reports the pattern fault.
//   - otherwise the first menu tool (in menu order) whose derived call parses back to the same
//     tool and JSON-equal arguments is shown; when none does, the probe call is shown, and the
//     error reports it should it not parse back either.
func customRegexExample(pattern, example string, menu []domain.ToolDef) (string, error) {
	cfg := CustomRegexConfig{Pattern: pattern}.withDefaults()
	compiled, err := compilePattern(cfg.Pattern, cfg.Flags)
	if err != nil {
		return example, fmt.Errorf("%w: %q does not compile: %v%s", ErrCustomRegexPattern, pattern, err, quotedExample(example))
	}
	delimiters, ok := extractRegexDelimiters(pattern)
	if !ok {
		return example, fmt.Errorf("%w: %q needs a (?<%s>...) group and a separate (?<%s>...) group%s",
			ErrCustomRegexPattern, pattern, cfg.NameGroup, cfg.ArgsGroup, quotedExample(example))
	}

	parser := &CustomRegexParser{cfg: cfg, pattern: compiled}
	if example != "" {
		return example, checkGivenExample(parser, example)
	}
	for _, tool := range menu {
		candidate := exampleForTool(tool)
		if text := delimiters.call(candidate.toolName, candidate.argsJSON()); parsesBack(parser, text, candidate) {
			return text, nil
		}
	}
	probe := delimiters.call(probeExampleCall.toolName, probeExampleCall.argsJSON())
	if !parsesBack(parser, probe, probeExampleCall) {
		return probe, fmt.Errorf("%w: the call derived from the pattern, %q, does not parse back as %s %s",
			ErrCustomRegexExample, probe, probeExampleCall.toolName, probeExampleCall.argsJSON())
	}
	return probe, nil
}

// quotedExample is the error suffix naming a given example, or "" when none was given.
func quotedExample(example string) string {
	if example == "" {
		return ""
	}
	return fmt.Sprintf(" (example %q)", example)
}

// checkGivenExample reports whether parser reads example as a call of a named tool whose arguments
// are a JSON object, the error quoting the example when it does not.
func checkGivenExample(parser *CustomRegexParser, example string) error {
	call, found := parser.ParseToolCall(example)
	if !found {
		return fmt.Errorf("%w: the pattern reads no tool call from %q", ErrCustomRegexExample, example)
	}
	if call.Malformed != nil {
		return fmt.Errorf("%w: the arguments of %q are not a JSON object: %v", ErrCustomRegexExample, example, call.Malformed.Err)
	}
	return nil
}

// parsesBack reports whether parser reads text as exactly the call want: the same tool, well-formed
// arguments, and arguments JSON-equal to want's.
func parsesBack(parser *CustomRegexParser, text string, want exampleToolCall) bool {
	call, found := parser.ParseToolCall(text)
	if !found || call.Malformed != nil || call.Tool != want.toolName {
		return false
	}
	return jsonEqual(call.Arguments, json.RawMessage(want.argsJSON()))
}

// jsonEqual reports whether a and b are valid JSON with the same value, whatever their spacing.
func jsonEqual(a, b json.RawMessage) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}
