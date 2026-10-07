package processing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// InstructionsFor renders the emission-side counterpart of ParserFor: the text tool menu plus
// the format-specific tool-call instructions a non-native model needs to learn its tools and the
// exact markup it must emit. It is the request-side seam mirror of the parse seam — the same
// profile knobs and defaults the parser reads drive the text, so what we tell the model and what
// we parse can never drift (ADR 0010; oracle: context-builder.ts formatToolsBlock /
// buildMarkdownFencedInstructions / buildCustomRegexInstructions / pickExampleToolCall).
//
// A native or zero profile returns "" with a nil error (the byte-identical anchor — the native
// template renders the wire tools array, so adding text would only double-tell the model). An
// empty menu also returns "" (there is nothing to describe, matching the oracle's early return).
// An unknown tool-call format is an error so a misconfigured profile fails loudly, though it
// cannot reach here at runtime once ParserFor has accepted the profile at construction.
func InstructionsFor(p domain.ModelProfile, menu []domain.ToolDef) (string, error) {
	format := ToolCallFormat(p.ToolCallFormat)
	switch format {
	case "", FormatNative:
		return "", nil
	case FormatMarkdownFenced, FormatCustomRegex:
		// fall through to render below
	default:
		return "", fmt.Errorf("processing: unknown tool-call format %q", format)
	}

	// No tools ⇒ nothing to describe; the format instructions reference the first tool, so an
	// empty menu returns "" before pickExampleToolCall would index into it (oracle parity).
	if len(menu) == 0 {
		return "", nil
	}

	block := toolMenuBlock(menu)

	var instructions string
	switch format {
	case FormatMarkdownFenced:
		instructions = markdownFencedInstructions(MarkdownFencedConfig{}.withDefaults(), menu)
	case FormatCustomRegex:
		instructions = customRegexInstructions(p, menu)
	}
	if instructions != "" {
		block += "\n\n" + instructions
	}
	return block, nil
}

// toolMenuBlock renders the "## Available Tools" markdown menu: one bullet per tool with its name,
// description, and compact JSON-schema parameters, entries separated by a blank line. It ports the
// oracle's formatToolsBlock menu, without the budget/truncation note (apogee's context budget is a
// separate mechanism — the grilled scope guard).
func toolMenuBlock(menu []domain.ToolDef) string {
	entries := make([]string, 0, len(menu))
	for _, t := range menu {
		entries = append(entries, fmt.Sprintf("- **%s**: %s\n  Parameters: %s", t.Name, t.Description, schemaJSON(t.Schema)))
	}
	return "## Available Tools\n\n" + strings.Join(entries, "\n\n")
}

// schemaJSON renders a tool's argument schema as compact JSON, mirroring the oracle's
// JSON.stringify(parameters). An empty schema becomes "{}" (the oracle's parsed-empty-object form);
// invalid JSON falls back to the raw bytes rather than dropping the parameters entirely.
func schemaJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}

// markdownFencedInstructions renders the "## Tool Call Format" block for the markdown-fenced
// format: the empty template plus a live example built from the first tool. It is a faithful port
// of the oracle's buildMarkdownFencedInstructions, driven by the same fence-language / field-name
// knobs (defaults applied by the caller so the text matches what withDefaults() parses).
func markdownFencedInstructions(cfg MarkdownFencedConfig, menu []domain.ToolDef) string {
	ex := pickExampleToolCall(menu)
	lines := []string{
		"## Tool Call Format",
		"",
		fmt.Sprintf("To call a tool, output a fenced code block with language `%s` using this exact structure:", cfg.FenceLanguage),
		"",
		"````",
		"```" + cfg.FenceLanguage,
		cfg.NameField,
		"<tool_name>",
		cfg.ArgStartField,
		"<argument_name>",
		cfg.ArgEndField,
		"<argument_value>",
		"```",
		"````",
		"",
		fmt.Sprintf("Each argument needs its own %s / %s pair.", cfg.ArgStartField, cfg.ArgEndField),
		"",
		fmt.Sprintf("Example — calling `%s`:", ex.toolName),
		"",
		"````",
		"```" + cfg.FenceLanguage,
		cfg.NameField,
		ex.toolName,
		cfg.ArgStartField,
		ex.argName,
		cfg.ArgEndField,
		ex.argValue,
		"```",
		"````",
		"",
		"IMPORTANT: Use ONLY the format shown above. Do NOT invent other tool call formats.",
	}
	return strings.Join(lines, "\n")
}

// customRegexInstructions renders the "## Tool Call Format" block for the custom-regex format
// around the one example call customRegexExample picks for p: its ToolCallExample verbatim, or a
// call written in the pattern's literal delimiters that the profile's own parser reads back. An
// empty pattern returns "" (oracle parity). A pattern with no name or args group has no call to
// derive, so the block carries no example line — config load refuses such a profile, which leaves
// only an embedder's unvalidated Config.Profile to reach that branch.
func customRegexInstructions(p domain.ModelProfile, menu []domain.ToolDef) string {
	if p.Pattern == "" {
		return ""
	}
	// The error is the load check's verdict on the profile; at runtime the shown call, if any,
	// is all the instructions need — a refused profile never got this far.
	example, _ := customRegexExample(p.Pattern, p.ToolCallExample, menu)
	if example == "" {
		return strings.Join([]string{
			"## Tool Call Format",
			"",
			"To call a tool, output the tool name and a JSON object of arguments.",
			"",
			"Arguments MUST be valid JSON. Do NOT use any other format.",
		}, "\n")
	}

	lines := []string{
		"## Tool Call Format",
		"",
		"To call a tool, output the tool name and a JSON object of arguments in this format:",
		"",
		example,
		"",
		"Arguments MUST be valid JSON. Do NOT use any other format.",
	}
	return strings.Join(lines, "\n")
}

// regexDelimiters are the literal strings surrounding a pattern's name and args groups: the text
// before the first of the two, between them, and after the second. argsFirst records that the
// args group comes first in the pattern.
type regexDelimiters struct {
	prefix    string
	middle    string
	suffix    string
	argsFirst bool
}

// call writes a tool name and its argument text into the delimiters, in the pattern's group order.
func (d regexDelimiters) call(name, args string) string {
	if d.argsFirst {
		return d.prefix + args + d.middle + name + d.suffix
	}
	return d.prefix + name + d.middle + args + d.suffix
}

// extractRegexDelimiters recovers the literal delimiters around the pattern's name and args groups
// (the CustomRegexConfig default names, in the (?<name>…) or (?P<name>…) spelling, in either
// order) so an example call reproduces the pattern's surrounding markup. Each delimiter's regex
// syntax is rewritten to the text it most plausibly matches (literalDelimiter); the rewrite is a
// best guess that the caller's round-trip through the parser judges. ok is false when either
// group is missing or one sits inside the other.
func extractRegexDelimiters(pattern string) (regexDelimiters, bool) {
	groups := CustomRegexConfig{}.withDefaults()
	nameStart, nameEnd, okName := namedGroupSpan(pattern, groups.NameGroup)
	argsStart, argsEnd, okArgs := namedGroupSpan(pattern, groups.ArgsGroup)
	if !okName || !okArgs {
		return regexDelimiters{}, false
	}

	firstStart, firstEnd, secondStart, secondEnd := nameStart, nameEnd, argsStart, argsEnd
	argsFirst := argsStart < nameStart
	if argsFirst {
		firstStart, firstEnd, secondStart, secondEnd = argsStart, argsEnd, nameStart, nameEnd
	}
	if secondStart < firstEnd {
		return regexDelimiters{}, false
	}
	return regexDelimiters{
		prefix:    literalDelimiter(pattern[:firstStart]),
		middle:    literalDelimiter(pattern[firstEnd:secondStart]),
		suffix:    literalDelimiter(pattern[secondEnd:]),
		argsFirst: argsFirst,
	}, true
}

// namedGroupSpan locates the capture group called group — opened as (?<group> or (?P<group> — and
// returns the byte range from its opening paren to just past its balanced closing paren. Escaped
// characters and bracket expressions are skipped, so neither \( nor a paren inside [...] counts.
// ok is false when the pattern has no such group or the group never closes.
func namedGroupSpan(pattern, group string) (start, end int, ok bool) {
	openers := []string{"(?<" + group + ">", "(?P<" + group + ">"}
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			i++
		case '[':
			i = bracketEnd(pattern, i)
		case '(':
			if !strings.HasPrefix(pattern[i:], openers[0]) && !strings.HasPrefix(pattern[i:], openers[1]) {
				continue
			}
			closing, closed := closingParen(pattern, i)
			return i, closing + 1, closed
		}
	}
	return 0, 0, false
}

// closingParen returns the index of the paren that balances the one at open, skipping escaped
// characters and bracket expressions. closed is false when the pattern ends first.
func closingParen(pattern string, open int) (closing int, closed bool) {
	depth := 0
	for i := open; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			i++
		case '[':
			i = bracketEnd(pattern, i)
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// bracketEnd returns the index of the ] closing the bracket expression opened at open: a ] right
// after the [ or [^ is a literal member, an escaped character never closes it, and a [:class:]
// inside is skipped whole. An unclosed expression runs to the pattern's last byte.
func bracketEnd(pattern string, open int) int {
	i := open + 1
	if i < len(pattern) && pattern[i] == '^' {
		i++
	}
	if i < len(pattern) && pattern[i] == ']' {
		i++
	}
	for ; i < len(pattern); i++ {
		switch {
		case pattern[i] == '\\':
			i++
		case pattern[i] == '[' && strings.HasPrefix(pattern[i:], "[:"):
			if end := strings.Index(pattern[i+2:], ":]"); end >= 0 {
				i += end + 3
			}
		case pattern[i] == ']':
			return i
		}
	}
	return len(pattern) - 1
}

// repeatQuantifier matches a counted repetition {n}, {n,} or {n,m} at the start of a string.
var repeatQuantifier = regexp.MustCompile(`^\{\d+(?:,\d*)?\}`)

// literalDelimiter rewrites the regex text around the name and args groups to the literal text it
// most plausibly matches: \s* becomes nothing, \s+ and \s one space, \n a newline, \t a tab, and
// any other escaped character itself; the anchors ^, $, \A and \z and the quantifiers *, +, ?
// (a lazy ? included), {n}, {n,} and {n,m} are dropped; everything else is kept as written.
func literalDelimiter(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s):
			i++
			switch escaped := s[i]; escaped {
			case 's':
				if i+1 < len(s) && s[i+1] == '*' {
					continue // \s* matches nothing; its * is dropped on the next byte
				}
				b.WriteByte(' ')
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'A', 'z':
				// anchors match no text
			default:
				b.WriteByte(escaped)
			}
		case c == '^' || c == '$' || c == '*' || c == '+' || c == '?':
			// anchors and quantifiers match no text of their own
		case c == '{' && repeatQuantifier.MatchString(s[i:]):
			i += len(repeatQuantifier.FindString(s[i:])) - 1
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// exampleToolCall is a representative tool call rendered into the format instructions: a tool name
// with one argument name and a plausible value.
type exampleToolCall struct {
	toolName string
	argName  string
	argValue string
}

// argsJSON is the example's arguments as the instructions write them: a one-member JSON object
// with the value as a string (oracle parity).
func (ex exampleToolCall) argsJSON() string {
	return fmt.Sprintf(`{"%s": "%s"}`, ex.argName, ex.argValue)
}

// pickExampleToolCall builds a representative example from the first tool (exampleForTool). The
// caller guarantees a non-empty menu.
func pickExampleToolCall(menu []domain.ToolDef) exampleToolCall {
	return exampleForTool(menu[0])
}

// exampleForTool builds a representative example call of tool, porting the oracle's
// pickExampleToolCall: a parameter-less tool becomes input/example; otherwise the first schema
// property names the argument and its type picks a plausible value (path/command string hints,
// 1 for numbers, true for booleans, "example" otherwise).
func exampleForTool(tool domain.ToolDef) exampleToolCall {
	name, typ, ok := firstProperty(tool.Schema)
	if !ok {
		return exampleToolCall{toolName: tool.Name, argName: "input", argValue: "example"}
	}

	argValue := "example"
	switch typ {
	case "string":
		lower := strings.ToLower(name)
		switch {
		case strings.Contains(lower, "path"):
			argValue = "src/main.ts"
		case strings.Contains(lower, "command"):
			argValue = "ls -la"
		}
	case "number", "integer":
		argValue = "1"
	case "boolean":
		argValue = "true"
	}
	return exampleToolCall{toolName: tool.Name, argName: name, argValue: argValue}
}

// firstProperty returns the name and declared type of a schema's first "properties" entry, in
// source order (mirroring the oracle's Object.keys(props)[0]). ok is false when the schema has no
// object-shaped properties map or it is empty; a property with no declared type yields an empty typ
// (the caller then uses the default example value), matching the oracle's props[argName]?.type.
func firstProperty(schema json.RawMessage) (name, typ string, ok bool) {
	if len(schema) == 0 {
		return "", "", false
	}
	var wrapper struct {
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schema, &wrapper); err != nil || len(wrapper.Properties) == 0 {
		return "", "", false
	}

	dec := json.NewDecoder(bytes.NewReader(wrapper.Properties))
	open, err := dec.Token()
	if err != nil {
		return "", "", false
	}
	if d, isDelim := open.(json.Delim); !isDelim || d != '{' {
		return "", "", false
	}
	if !dec.More() {
		return "", "", false // properties is an empty object
	}
	keyTok, err := dec.Token()
	if err != nil {
		return "", "", false
	}
	key, isStr := keyTok.(string)
	if !isStr {
		return "", "", false
	}
	var val struct {
		Type string `json:"type"`
	}
	if err := dec.Decode(&val); err != nil {
		return "", "", false
	}
	return key, val.Type, true
}
