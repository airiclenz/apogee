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
// to marker-based detection when no clean fence is present. It is a faithful port of
// apogee-code's MarkdownFencedParser oracle; the one deliberate divergence is the fence-close
// search, which Go's RE2 (no negative lookahead) expresses as an explicit scan instead of the
// TS `(?!fence)` lookahead — the matched behaviour is identical (a closing ``` that does not
// reopen the same fence language). The parser is stateless and safe for concurrent use.
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
	_, blockStart, ok := p.lastFenceBounds(text)
	if !ok {
		return domain.ToolCall{}, false
	}
	closeIdx, ok := p.fenceClose(text, blockStart)
	if !ok {
		return domain.ToolCall{}, false
	}
	block := strings.TrimSpace(text[blockStart:closeIdx])
	return p.parseBlock(block)
}

// strictStrip removes the last fenced tool block; ok is false when no opener is present.
func (p *MarkdownFencedParser) strictStrip(text string) (string, bool) {
	openStart, blockStart, ok := p.lastFenceBounds(text)
	if !ok {
		return "", false
	}
	endIdx := len(text)
	if closeIdx, found := p.fenceClose(text, blockStart); found {
		// Advance past the three closing backticks the close scan points at.
		endIdx = closeIdx + len("```")
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

// fenceClose finds the first closing ``` at or after blockStart that does not reopen the
// fence language — the RE2-safe equivalent of the oracle's ```(?!<lang>) lookahead.
func (p *MarkdownFencedParser) fenceClose(text string, blockStart int) (int, bool) {
	rest := text[blockStart:]
	from := 0
	for {
		i := strings.Index(rest[from:], "```")
		if i == -1 {
			return 0, false
		}
		at := from + i
		after := rest[at+len("```"):]
		if !strings.HasPrefix(after, p.cfg.FenceLanguage) {
			return blockStart + at, true
		}
		from = at + len("```")
	}
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
	block := toolName + "\n" + strings.TrimSpace(text[firstArgStart:])
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
				args[argName] = tryParseValue(strings.Join(valueParts, "\n"))
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

var (
	// backtickNoise matches 1–4 backticks plus an optional fence info word (e.g. ```tool).
	backtickNoise = regexp.MustCompile("`{1,4}\\w*")
	// angleNoise matches an angle-bracket tag, optionally pipe-wrapped (e.g. <|channel|>).
	angleNoise = regexp.MustCompile(`<\|?[^>]*\|?>`)
)
