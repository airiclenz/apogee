package processing

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// MarkdownFencedConfig configures the markdown-fenced tool-call format: a fenced code block
// (```<fenceLanguage>) whose body names a tool and lists named arguments delimited by marker
// lines. Zero-valued fields fall back to the apogee-code defaults (tool / TOOL_NAME /
// BEGIN_ARG / END_ARG), matching the oracle so a partially-specified config still parses.
type MarkdownFencedConfig struct {
	// FenceLanguage is the code-fence info string that opens a tool block (default "tool").
	FenceLanguage string
	// NameField is the marker line preceding the tool name (default "TOOL_NAME").
	NameField string
	// ArgStartField opens an argument; the next line is the argument name (default "BEGIN_ARG").
	ArgStartField string
	// ArgEndField closes the argument name; the lines until the next ArgStartField are its
	// value (default "END_ARG").
	ArgEndField string
}

// withDefaults returns cfg with empty fields replaced by the apogee-code oracle defaults.
func (c MarkdownFencedConfig) withDefaults() MarkdownFencedConfig {
	if c.FenceLanguage == "" {
		c.FenceLanguage = "tool"
	}
	if c.NameField == "" {
		c.NameField = "TOOL_NAME"
	}
	if c.ArgStartField == "" {
		c.ArgStartField = "BEGIN_ARG"
	}
	if c.ArgEndField == "" {
		c.ArgEndField = "END_ARG"
	}
	return c
}

// MarkdownFencedParser extracts a tool call from a markdown-fenced code block, falling back
// to marker-based detection when no clean fence is present. It started as a port of
// apogee-code's MarkdownFencedParser oracle and now diverges from it where the oracle loses
// argument text: the block closes on a line that is exactly ``` (nested fences opened with an
// info string, such as ```bash, are tracked so they do not close it), the block's lines lose
// the opener line's indentation, and a value is kept verbatim (see verbatimValue) rather than
// trimmed. The parser is stateless and safe for concurrent use.
type MarkdownFencedParser struct {
	cfg           MarkdownFencedConfig
	fenceStart    *regexp.Regexp
	toolNameToken *regexp.Regexp
}

// NewMarkdownFencedParser builds a parser for the markdown-fenced format from cfg (empty
// fields take the oracle defaults). The compiled fence-opener regex is built once and reused.
func NewMarkdownFencedParser(cfg MarkdownFencedConfig) *MarkdownFencedParser {
	cfg = cfg.withDefaults()
	return &MarkdownFencedParser{
		cfg: cfg,
		// The fence opener: ```<lang> then optional trailing spaces then a newline.
		fenceStart: regexp.MustCompile("```" + regexp.QuoteMeta(cfg.FenceLanguage) + `[ \t]*\n`),
		// A bare identifier token, used to recover a tool name from fallback noise.
		toolNameToken: regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`),
	}
}

// ParseToolCall extracts a tool call from raw, trying the strict fence form first and the
// marker-based fallback second. found is false when neither yields a name.
func (p *MarkdownFencedParser) ParseToolCall(raw string) (domain.ToolCall, bool) {
	if call, ok := p.strictParse(raw); ok {
		return call, true
	}
	return p.fallbackParse(raw)
}

// StripToolCall returns raw with the recognised tool-call markup removed and trimmed. When no
// call is present it returns raw unchanged (untrimmed, mirroring the oracle's text passthrough).
func (p *MarkdownFencedParser) StripToolCall(raw string) string {
	if stripped, ok := p.strictStrip(raw); ok {
		return stripped
	}

	bounds, ok := p.findFallbackBounds(raw)
	if !ok {
		return raw
	}
	return strings.TrimSpace(raw[:bounds.start] + raw[bounds.end:])
}

// ─── strict (fence-based) ───────────────────────────────────────────────────

// strictParse parses the last fenced tool block, if any.
func (p *MarkdownFencedParser) strictParse(text string) (domain.ToolCall, bool) {
	openStart, blockStart, ok := p.lastFenceBounds(text)
	if !ok {
		return domain.ToolCall{}, false
	}
	blockEnd, _, ok := p.fenceClose(text, blockStart)
	if !ok {
		return domain.ToolCall{}, false
	}
	block := dedentLines(text[blockStart:blockEnd], openerIndent(text, openStart))
	return p.parseBlock(block)
}

// strictStrip removes the last fenced tool block; ok is false when no opener is present.
func (p *MarkdownFencedParser) strictStrip(text string) (string, bool) {
	openStart, blockStart, ok := p.lastFenceBounds(text)
	if !ok {
		return "", false
	}
	endIdx := len(text)
	if _, closeIdx, found := p.fenceClose(text, blockStart); found {
		// Advance past the three closing backticks the close scan points at.
		endIdx = closeIdx + len(codeFence)
	}
	return strings.TrimSpace(text[:openStart] + text[endIdx:]), true
}

// lastFenceBounds finds the last fence opener: openStart is the index of the opening ```,
// blockStart is the index just past the opener line (where the block body begins).
func (p *MarkdownFencedParser) lastFenceBounds(text string) (openStart, blockStart int, ok bool) {
	locs := p.fenceStart.FindAllStringIndex(text, -1)
	if len(locs) == 0 {
		return 0, 0, false
	}
	last := locs[len(locs)-1]
	return last[0], last[1], true
}

// fenceClose finds where the tool block that begins at blockStart ends. blockEnd is the end of
// the block body (exclusive) and closeIdx the index of the closing ```.
//
// Walking the lines from blockStart, a line whose trimmed form is ``` followed by an info
// string (```bash) opens a nested fence, and a line that is exactly ``` closes the innermost
// open nested fence, or the tool block when none is open; blockEnd is then the start of that
// close line, so the block keeps the line break before it. Outside any nested fence, a line
// that ends in ``` after other text closes the block there too — the close glued to a value's
// last line (src/main.ts```) — and it wins over any later ``` line, where blockEnd and
// closeIdx coincide. Only when no line closes the block does the scan fall back to the first
// ``` anywhere that does not reopen the fence language.
//
// A nested fence opened bare (``` with no info string) cannot be told from the tool block's
// own close, so its opener closes the block: the documented limit of the format.
func (p *MarkdownFencedParser) fenceClose(text string, blockStart int) (blockEnd, closeIdx int, ok bool) {
	if end, at, found := closeLine(text, blockStart); found {
		return end, at, true
	}
	return p.gluedClose(text, blockStart)
}

// closeLine walks the lines from blockStart for the line-level close fenceClose documents.
func closeLine(text string, blockStart int) (blockEnd, closeIdx int, ok bool) {
	depth := 0
	for lineStart := blockStart; lineStart < len(text); {
		lineEnd := indexFrom(text, "\n", lineStart)
		if lineEnd == -1 {
			lineEnd = len(text)
		}
		trimmed := strings.TrimSpace(text[lineStart:lineEnd])
		switch {
		case trimmed == codeFence && depth == 0:
			return lineStart, lineStart + strings.Index(text[lineStart:lineEnd], codeFence), true
		case trimmed == codeFence:
			depth--
		case opensNestedFence(trimmed):
			depth++
		case depth == 0 && endsInGluedClose(trimmed):
			at := lineStart + len(strings.TrimRight(text[lineStart:lineEnd], " \t\r")) - len(codeFence)
			return at, at, true
		}
		lineStart = lineEnd + 1
	}
	return 0, 0, false
}

// endsInGluedClose reports whether a trimmed line ends in a ``` glued to other text
// (src/main.ts```). A line of four or more backticks is a longer fence, not a glued close.
func endsInGluedClose(trimmed string) bool {
	body, ok := strings.CutSuffix(trimmed, codeFence)
	return ok && body != "" && !strings.HasSuffix(body, "`")
}

// opensNestedFence reports whether a trimmed line opens a fence with an info string (```bash).
// A run of four or more backticks carries a backtick in its "info string" and is not counted.
func opensNestedFence(trimmed string) bool {
	info, ok := strings.CutPrefix(trimmed, codeFence)
	return ok && info != "" && !strings.Contains(info, "`")
}

// gluedClose is fenceClose's fallback: the first ``` at or after blockStart that does not
// reopen the fence language — the RE2-safe equivalent of the oracle's ```(?!<lang>) lookahead.
func (p *MarkdownFencedParser) gluedClose(text string, blockStart int) (blockEnd, closeIdx int, ok bool) {
	rest := text[blockStart:]
	from := 0
	for {
		i := strings.Index(rest[from:], codeFence)
		if i == -1 {
			return 0, 0, false
		}
		at := from + i
		after := rest[at+len(codeFence):]
		if !strings.HasPrefix(after, p.cfg.FenceLanguage) {
			return blockStart + at, blockStart + at, true
		}
		from = at + len(codeFence)
	}
}

// openerIndent returns the indentation ahead of the fence opener at openStart — the run of
// spaces and tabs between the line start and the opener — or "" when other text precedes the
// opener on its line (prose glued to the fence is not indentation).
func openerIndent(text string, openStart int) string {
	lineStart := strings.LastIndexByte(text[:openStart], '\n') + 1
	prefix := text[lineStart:openStart]
	if strings.TrimLeft(prefix, " \t") != "" {
		return ""
	}
	return prefix
}

// dedentLines removes up to len(indent) leading spaces and tabs from every line of block, the
// CommonMark rule for a fenced block opened at that indentation (a list item's block, say):
// deeper indentation inside a value survives, shallower lines lose what they have.
func dedentLines(block, indent string) string {
	if indent == "" {
		return block
	}
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		cut := 0
		for cut < len(indent) && cut < len(line) && (line[cut] == ' ' || line[cut] == '\t') {
			cut++
		}
		lines[i] = line[cut:]
	}
	return strings.Join(lines, "\n")
}

// ─── fallback (marker-based) ────────────────────────────────────────────────

// fallbackParse recovers a call from the argument markers when no clean fence is present.
func (p *MarkdownFencedParser) fallbackParse(text string) (domain.ToolCall, bool) {
	firstArgStart := strings.Index(text, p.cfg.ArgStartField)
	if firstArgStart == -1 {
		return domain.ToolCall{}, false
	}
	if indexFrom(text, p.cfg.ArgEndField, firstArgStart) == -1 {
		return domain.ToolCall{}, false
	}
	toolName, ok := p.extractToolNameBeforeMarker(text, firstArgStart)
	if !ok {
		return domain.ToolCall{}, false
	}
	block := toolName + "\n" + text[firstArgStart:]
	return p.parseBlock(block)
}

// extractToolNameBeforeMarker recovers a tool name from the text preceding the first argument
// marker, stripping fence/tag noise and choosing the last identifier-shaped token.
func (p *MarkdownFencedParser) extractToolNameBeforeMarker(text string, markerIndex int) (string, bool) {
	before := text[:markerIndex]
	before = backtickNoise.ReplaceAllString(before, "")
	before = angleNoise.ReplaceAllString(before, "")
	before = strings.ReplaceAll(before, p.cfg.NameField, "")
	before = strings.TrimSpace(before)

	tokens := strings.Fields(before)
	for i := len(tokens) - 1; i >= 0; i-- {
		if p.toolNameToken.MatchString(tokens[i]) {
			return tokens[i], true
		}
	}
	return "", false
}

// fallbackBounds delimits the marker-based call region for stripping.
type fallbackBounds struct {
	start int
	end   int
}

// findFallbackBounds locates the text region the marker-based call occupies, extending the
// start backwards over preceding fence/tag noise lines (the oracle's strip heuristic).
func (p *MarkdownFencedParser) findFallbackBounds(text string) (fallbackBounds, bool) {
	firstArgStart := strings.Index(text, p.cfg.ArgStartField)
	if firstArgStart == -1 {
		return fallbackBounds{}, false
	}
	if indexFrom(text, p.cfg.ArgEndField, firstArgStart) == -1 {
		return fallbackBounds{}, false
	}

	scanBack := firstArgStart
	for scanBack > 0 && isSpace(text[scanBack-1]) {
		scanBack--
	}
	lineStart := strings.LastIndexByte(text[:scanBack], '\n')
	if lineStart == -1 {
		lineStart = 0
	} else {
		lineStart++
	}

	prevLineEnd := lineStart - 1
	for prevLineEnd > 0 {
		prevLineStart := strings.LastIndexByte(text[:prevLineEnd], '\n')
		if prevLineStart == -1 {
			prevLineStart = 0
		} else {
			prevLineStart++
		}
		prevLine := strings.TrimSpace(text[prevLineStart : prevLineEnd+1])
		if strings.Contains(prevLine, "`") || strings.Contains(prevLine, "<") || strings.Contains(prevLine, p.cfg.FenceLanguage) {
			lineStart = prevLineStart
			prevLineEnd = prevLineStart - 1
		} else {
			break
		}
	}

	return fallbackBounds{start: lineStart, end: len(text)}, true
}

// ─── shared block parsing ───────────────────────────────────────────────────

// parseBlock runs the line-by-line state machine over a tool block, recovering the name and
// each named argument value. ok is false when no name was found.
func (p *MarkdownFencedParser) parseBlock(block string) (domain.ToolCall, bool) {
	lines := strings.Split(block, "\n")
	var toolName string
	haveName := false
	args := map[string]json.RawMessage{}
	markers := map[string]struct{}{
		p.cfg.NameField:     {},
		p.cfg.ArgStartField: {},
		p.cfg.ArgEndField:   {},
	}

	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		switch {
		case line == p.cfg.NameField:
			i++
			if i < len(lines) {
				toolName = strings.TrimSpace(lines[i])
				haveName = true
			}
		case !haveName && line != "" && !hasKey(markers, line):
			toolName = line
			haveName = true
		case line == p.cfg.ArgStartField:
			i++
			if i >= len(lines) {
				i = len(lines)
				continue
			}
			argName := strings.TrimSpace(lines[i])
			i++
			if i < len(lines) && strings.TrimSpace(lines[i]) == p.cfg.ArgEndField {
				i++
				var valueParts []string
				for i < len(lines) && strings.TrimSpace(lines[i]) != p.cfg.ArgStartField {
					valueParts = append(valueParts, lines[i])
					i++
				}
				args[argName] = verbatimValue(strings.Join(valueParts, "\n"))
				continue
			}
		}
		i++
	}

	if !haveName {
		return domain.ToolCall{}, false
	}
	return domain.ToolCall{Tool: toolName, Arguments: marshalArgs(args)}, true
}

// codeFence is the three-backtick run that opens and closes a markdown code fence.
const codeFence = "```"

var (
	// backtickNoise matches 1–4 backticks plus an optional fence info word (e.g. ```tool).
	backtickNoise = regexp.MustCompile("`{1,4}\\w*")
	// angleNoise matches an angle-bracket tag, optionally pipe-wrapped (e.g. <|channel|>).
	angleNoise = regexp.MustCompile(`<\|?[^>]*\|?>`)
)
