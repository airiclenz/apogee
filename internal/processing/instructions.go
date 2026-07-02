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
		instructions = customRegexInstructions(p.Pattern, menu)
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

// customRegexInstructions renders the "## Tool Call Format" block for the custom-regex format: a
// single example call in the model's pattern, built by extracting the literal delimiters around the
// pattern's named groups (falling back to the oracle's <tool_call>…</tool_call> shape). It ports
// buildCustomRegexInstructions's pattern branch; an empty pattern returns "" (oracle parity — the
// exampleOutput branch has no profile knob and is intentionally not ported).
func customRegexInstructions(pattern string, menu []domain.ToolDef) string {
	if pattern == "" {
		return ""
	}
	ex := pickExampleToolCall(menu)

	var exampleCall string
	if d, ok := extractRegexDelimiters(pattern); ok {
		exampleCall = fmt.Sprintf(`%s%s%s{"%s": "%s"}%s`, d.prefix, ex.toolName, d.middle, ex.argName, ex.argValue, d.suffix)
	} else {
		exampleCall = fmt.Sprintf(`<tool_call>%s({"%s": "%s"})</tool_call>`, ex.toolName, ex.argName, ex.argValue)
	}

	lines := []string{
		"## Tool Call Format",
		"",
		"To call a tool, output the tool name and a JSON object of arguments in this format:",
		"",
		exampleCall,
		"",
		"Arguments MUST be valid JSON. Do NOT use any other format.",
	}
	return strings.Join(lines, "\n")
}

// regexNamedGroup matches a JavaScript-style named capture group (?<name>…) up to the first
// closing paren — the same shape the oracle's extractRegexDelimiters scans for (the profile's
// patterns are written in the JS form the parser translates).
var regexNamedGroup = regexp.MustCompile(`\(\?<\w+>[^)]*\)`)

// regexDelimiters are the literal strings surrounding a pattern's first two named groups: the text
// before the name group, between the name and args groups, and after the args group.
type regexDelimiters struct {
	prefix string
	middle string
	suffix string
}

// extractRegexDelimiters recovers the literal delimiters around the first two named groups so the
// example call reproduces the pattern's surrounding markup. ok is false when the pattern has fewer
// than two named groups (the caller then uses the oracle's <tool_call>…</tool_call> fallback).
// Backslashes are stripped from each delimiter, mirroring the oracle's backslash-removal step.
func extractRegexDelimiters(pattern string) (regexDelimiters, bool) {
	locs := regexNamedGroup.FindAllStringIndex(pattern, -1)
	if len(locs) < 2 {
		return regexDelimiters{}, false
	}
	return regexDelimiters{
		prefix: stripBackslashes(pattern[:locs[0][0]]),
		middle: stripBackslashes(pattern[locs[0][1]:locs[1][0]]),
		suffix: stripBackslashes(pattern[locs[1][1]:]),
	}, true
}

// stripBackslashes removes every backslash from s (the oracle's backslash-removal step).
func stripBackslashes(s string) string { return strings.ReplaceAll(s, `\`, "") }

// exampleToolCall is a representative tool call rendered into the format instructions: a tool name
// with one argument name and a plausible value.
type exampleToolCall struct {
	toolName string
	argName  string
	argValue string
}

// pickExampleToolCall builds a representative example from the first tool, porting the oracle's
// pickExampleToolCall: a parameter-less tool becomes input/example; otherwise the first schema
// property names the argument and its type picks a plausible value (path/command string hints,
// 1 for numbers, true for booleans, "example" otherwise). The caller guarantees a non-empty menu.
func pickExampleToolCall(menu []domain.ToolDef) exampleToolCall {
	tool := menu[0]
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
