package processing

import (
	"encoding/json"
	"strings"
	"testing"
)

// defaultFencedParser builds the parser the apogee-code MarkdownFencedParser vectors use
// (test/unit/markdown-fenced-parser.test.ts): the default tool / TOOL_NAME / BEGIN_ARG /
// END_ARG markers.
func defaultFencedParser() *MarkdownFencedParser {
	return NewMarkdownFencedParser(MarkdownFencedConfig{})
}

// argString pulls a string-valued argument out of a parsed call's JSON arguments, failing the
// test if the key is missing or not a string — the Go analogue of the oracle's
// arguments.<key> assertion.
func argString(t *testing.T, args json.RawMessage, key string) string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(args, &m); err != nil {
		t.Fatalf("arguments are not a JSON object: %v (%s)", err, args)
	}
	raw, ok := m[key]
	if !ok {
		t.Fatalf("argument %q missing in %s", key, args)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("argument %q is not a string: %v (%s)", key, err, raw)
	}
	return s
}

func TestMarkdownFenced_PortedOracleVectors(t *testing.T) {
	t.Parallel()

	t.Run("parses a basic tool call", func(t *testing.T) {
		t.Parallel()
		raw := "I'll read the file for you.\n\n```tool\nTOOL_NAME\nread_file\nBEGIN_ARG\npath\nEND_ARG\nsrc/main.ts\n```"
		call, ok := defaultFencedParser().ParseToolCall(raw)
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

	t.Run("parses tool call with multiple arguments", func(t *testing.T) {
		t.Parallel()
		raw := "```tool\nTOOL_NAME\nsingle_find_and_replace\nBEGIN_ARG\npath\nEND_ARG\nsrc/types.ts\nBEGIN_ARG\noldText\nEND_ARG\nconst x = 1;\nBEGIN_ARG\nnewText\nEND_ARG\nconst x = 2;\n```"
		call, ok := defaultFencedParser().ParseToolCall(raw)
		if !ok {
			t.Fatal("expected a parsed call")
		}
		if call.Tool != "single_find_and_replace" {
			t.Errorf("Tool = %q, want single_find_and_replace", call.Tool)
		}
		if got := argString(t, call.Arguments, "path"); got != "src/types.ts" {
			t.Errorf("path = %q", got)
		}
		if got := argString(t, call.Arguments, "oldText"); got != "const x = 1;" {
			t.Errorf("oldText = %q", got)
		}
		if got := argString(t, call.Arguments, "newText"); got != "const x = 2;" {
			t.Errorf("newText = %q", got)
		}
	})

	t.Run("parses tool call with multi-line argument", func(t *testing.T) {
		t.Parallel()
		raw := "```tool\nTOOL_NAME\ncreate_new_file\nBEGIN_ARG\npath\nEND_ARG\nsrc/hello.ts\nBEGIN_ARG\ncontent\nEND_ARG\nexport function hello() {\n  return \"world\";\n}\n```"
		call, ok := defaultFencedParser().ParseToolCall(raw)
		if !ok {
			t.Fatal("expected a parsed call")
		}
		if call.Tool != "create_new_file" {
			t.Errorf("Tool = %q, want create_new_file", call.Tool)
		}
		content := argString(t, call.Arguments, "content")
		if !strings.Contains(content, "export function hello()") {
			t.Errorf("content missing function: %q", content)
		}
		if !strings.Contains(content, "return \"world\"") {
			t.Errorf("content missing return: %q", content)
		}
	})

	t.Run("returns no call when no tool block present", func(t *testing.T) {
		t.Parallel()
		if _, ok := defaultFencedParser().ParseToolCall("Just a normal response with no tool calls."); ok {
			t.Error("expected no call")
		}
	})

	t.Run("strips thinking before parsing tool call", func(t *testing.T) {
		t.Parallel()
		// The oracle strips thinking before the fenced parse; processing composes the two
		// (StripThinking then ParseToolCall), so the visible content is what the parser sees.
		raw := "<think>I should read the file</think>Let me check.\n\n```tool\nTOOL_NAME\nread_file\nBEGIN_ARG\npath\nEND_ARG\nsrc/main.ts\n```"
		visible := StripThinking(raw, gemmaConfig).Visible
		call, ok := defaultFencedParser().ParseToolCall(visible)
		if !ok {
			t.Fatal("expected a parsed call after thinking strip")
		}
		if call.Tool != "read_file" {
			t.Errorf("Tool = %q, want read_file", call.Tool)
		}
	})

	t.Run("parses tool call with double opening fence", func(t *testing.T) {
		t.Parallel()
		raw := "```tool\n```tool\ncreate_new_file\nBEGIN_ARG\npath\nEND_ARG\nwishes.txt\nBEGIN_ARG\ncontent\nEND_ARG\nHello world\n```\n```"
		call, ok := defaultFencedParser().ParseToolCall(raw)
		if !ok {
			t.Fatal("expected a parsed call")
		}
		if call.Tool != "create_new_file" {
			t.Errorf("Tool = %q, want create_new_file", call.Tool)
		}
		if got := argString(t, call.Arguments, "path"); got != "wishes.txt" {
			t.Errorf("path = %q, want wishes.txt", got)
		}
		if got := argString(t, call.Arguments, "content"); !strings.Contains(got, "Hello world") {
			t.Errorf("content = %q, want to contain Hello world", got)
		}
	})

	t.Run("parses tool call without TOOL_NAME marker", func(t *testing.T) {
		t.Parallel()
		raw := "```tool\nCREATE_NEW_FILE\nBEGIN_ARG\npath\nEND_ARG\nprimes.txt\nBEGIN_ARG\ncontent\nEND_ARG\n2\n3\n5\n7\n11\n```"
		call, ok := defaultFencedParser().ParseToolCall(raw)
		if !ok {
			t.Fatal("expected a parsed call")
		}
		if call.Tool != "CREATE_NEW_FILE" {
			t.Errorf("Tool = %q, want CREATE_NEW_FILE", call.Tool)
		}
		if got := argString(t, call.Arguments, "path"); got != "primes.txt" {
			t.Errorf("path = %q, want primes.txt", got)
		}
		content := argString(t, call.Arguments, "content")
		if !strings.Contains(content, "2") || !strings.Contains(content, "11") {
			t.Errorf("content = %q, want to contain 2 and 11", content)
		}
	})
}

func TestMarkdownFenced_StripRemovesBlock(t *testing.T) {
	t.Parallel()

	raw := "Here you go.\n\n```tool\nTOOL_NAME\nread_file\nBEGIN_ARG\npath\nEND_ARG\nsrc/main.ts\n```"
	got := defaultFencedParser().StripToolCall(raw)
	if strings.Contains(got, "TOOL_NAME") || strings.Contains(got, "```tool") {
		t.Errorf("strip left markup behind: %q", got)
	}
	if !strings.Contains(got, "Here you go.") {
		t.Errorf("strip removed surrounding prose: %q", got)
	}
}

func TestMarkdownFenced_StripNoBlockReturnsRaw(t *testing.T) {
	t.Parallel()

	raw := "Just prose, nothing to strip."
	if got := defaultFencedParser().StripToolCall(raw); got != raw {
		t.Errorf("strip = %q, want unchanged %q", got, raw)
	}
}

func TestMarkdownFenced_MalformedNeverPanics(t *testing.T) {
	t.Parallel()

	// A truncated/garbled block must degrade to no-call, never panic (the P1.3 contract).
	cases := []string{
		"```tool\n",
		"```tool\nTOOL_NAME\n",
		"BEGIN_ARG\n",
		"```tool\nTOOL_NAME\nread_file\nBEGIN_ARG",
		"```tool```tool```",
	}
	p := defaultFencedParser()
	for _, raw := range cases {
		_, _ = p.ParseToolCall(raw)
		_ = p.StripToolCall(raw)
	}
}
