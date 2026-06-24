package processing

import (
	"strings"
	"testing"
)

// toolCallPattern is the named-group pattern the apogee-code CustomRegexParser vectors use
// (test/unit/custom-regex-parser.test.ts) — written in JavaScript named-group syntax, which
// NewCustomRegexParser rewrites to Go's (?P<name>…).
const toolCallPattern = `<tool_call>(?<name>\w+)\((?<args>\{.*?\})\)</tool_call>`

func TestCustomRegex_PortedOracleVectors(t *testing.T) {
	t.Parallel()

	t.Run("parses tool call matching regex pattern", func(t *testing.T) {
		t.Parallel()
		p := NewCustomRegexParser(CustomRegexConfig{Pattern: toolCallPattern})
		raw := `Some text <tool_call>read_file({"path":"src/main.ts"})</tool_call>`
		call, ok := p.ParseToolCall(raw)
		if !ok {
			t.Fatal("expected a parsed call")
		}
		if call.Tool != "read_file" {
			t.Errorf("Tool = %q, want read_file", call.Tool)
		}
		if got := argString(t, call.Arguments, "path"); got != "src/main.ts" {
			t.Errorf("path = %q, want src/main.ts", got)
		}
	})

	t.Run("returns no call when no match", func(t *testing.T) {
		t.Parallel()
		p := NewCustomRegexParser(CustomRegexConfig{Pattern: toolCallPattern})
		if _, ok := p.ParseToolCall("Just a normal response with no tool calls."); ok {
			t.Error("expected no call")
		}
	})

	t.Run("strips thinking before parsing", func(t *testing.T) {
		t.Parallel()
		// The oracle strips thinking before the regex parse; processing composes the two.
		p := NewCustomRegexParser(CustomRegexConfig{Pattern: toolCallPattern})
		raw := `<think>Let me think...</think>Here: <tool_call>ls({"path":"."})</tool_call>`
		visible := StripThinking(raw, gemmaConfig).Visible
		call, ok := p.ParseToolCall(visible)
		if !ok {
			t.Fatal("expected a parsed call after thinking strip")
		}
		if call.Tool != "ls" {
			t.Errorf("Tool = %q, want ls", call.Tool)
		}
	})

	t.Run("handles non-JSON args gracefully", func(t *testing.T) {
		t.Parallel()
		p := NewCustomRegexParser(CustomRegexConfig{Pattern: `<tool>(?<name>\w+):(?<args>[^<]+)</tool>`})
		raw := `<tool>read_file:src/main.ts</tool>`
		call, ok := p.ParseToolCall(raw)
		if !ok {
			t.Fatal("expected a parsed call")
		}
		if call.Tool != "read_file" {
			t.Errorf("Tool = %q, want read_file", call.Tool)
		}
		if got := argString(t, call.Arguments, "raw"); got != "src/main.ts" {
			t.Errorf("raw arg = %q, want src/main.ts", got)
		}
	})
}

func TestCustomRegex_StripRemovesMatch(t *testing.T) {
	t.Parallel()

	p := NewCustomRegexParser(CustomRegexConfig{Pattern: toolCallPattern})
	raw := `Done. <tool_call>read_file({"path":"a"})</tool_call>`
	got := p.StripToolCall(raw)
	if strings.Contains(got, "tool_call") {
		t.Errorf("strip left markup: %q", got)
	}
	if !strings.Contains(got, "Done.") {
		t.Errorf("strip removed prose: %q", got)
	}
}

func TestCustomRegex_InvalidPatternNeverMatches(t *testing.T) {
	t.Parallel()

	// An invalid regex degrades to a never-match parser (the oracle's warn-and-fallback),
	// never a panic or construction error.
	p := NewCustomRegexParser(CustomRegexConfig{Pattern: `(?<name>\w+`}) // unbalanced
	if _, ok := p.ParseToolCall(`<tool_call>x({})</tool_call>`); ok {
		t.Error("expected no call from an invalid pattern")
	}
	if got := p.StripToolCall("unchanged text"); got != "unchanged text" {
		t.Errorf("strip = %q, want unchanged on invalid pattern", got)
	}
}

func TestCustomRegex_EmptyArgsGroupYieldsEmptyObject(t *testing.T) {
	t.Parallel()

	p := NewCustomRegexParser(CustomRegexConfig{Pattern: `\[(?<name>\w+)\]`})
	call, ok := p.ParseToolCall(`call [refresh] now`)
	if !ok {
		t.Fatal("expected a parsed call")
	}
	if call.Tool != "refresh" {
		t.Errorf("Tool = %q, want refresh", call.Tool)
	}
	if string(call.Arguments) != "{}" {
		t.Errorf("Arguments = %s, want {}", call.Arguments)
	}
}
