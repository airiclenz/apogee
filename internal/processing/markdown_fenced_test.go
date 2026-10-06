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
		if got := argString(t, call.Arguments, "content"); got != "Hello world" {
			t.Errorf("content = %q, want exactly Hello world", got)
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

func TestMarkdownFenced_ValuesStayVerbatim(t *testing.T) {
	t.Parallel()

	readme := "# Demo\n\nBuild it:\n\n```bash\nmake build\n```\n\nThen run it."
	python := "    def area(self, r):\n        if r < 0:\n            raise ValueError(r)\n        return 3.14 * r * r"
	cases := []struct {
		name string
		raw  string
		key  string
		want string
	}{
		{
			name: "a README holding its own bash fence keeps the whole value",
			raw:  "Writing it.\n\n```tool\nTOOL_NAME\nwrite_file\nBEGIN_ARG\npath\nEND_ARG\nREADME.md\nBEGIN_ARG\ncontent\nEND_ARG\n" + readme + "\n```",
			key:  "content",
			want: readme,
		},
		{
			name: "prose after the block holding a bash fence leaves the value unchanged",
			raw:  "```tool\nTOOL_NAME\nwrite_file\nBEGIN_ARG\npath\nEND_ARG\nnotes.txt\nBEGIN_ARG\ncontent\nEND_ARG\nplain notes\n```\n\nAfterwards run:\n```bash\nls\n```",
			key:  "content",
			want: "plain notes",
		},
		{
			name: "a list-item block loses the opener's indentation",
			raw:  "Steps:\n\n1. Edit it.\n   ```tool\n   TOOL_NAME\n   edit_file\n   BEGIN_ARG\n   path\n   END_ARG\n   main.py\n   BEGIN_ARG\n   new_string\n   END_ARG\n   if ok:\n       run()\n   ```",
			key:  "new_string",
			want: "if ok:\n    run()",
		},
		{
			name: "a close glued to the last line still closes the block",
			raw:  "```tool\nTOOL_NAME\nread_file\nBEGIN_ARG\npath\nEND_ARG\nsrc/main.ts```",
			key:  "path",
			want: "src/main.ts",
		},
		{
			name: "an indented block with a glued close yields the unindented value",
			raw:  "- Read it:\n  ```tool\n  TOOL_NAME\n  read_file\n  BEGIN_ARG\n  path\n  END_ARG\n  src/main.ts```",
			key:  "path",
			want: "src/main.ts",
		},
		{
			// The documented limit: a bare ``` opener cannot be told from the block's close.
			name: "a nested fence opened bare closes the block",
			raw:  "```tool\nTOOL_NAME\nwrite_file\nBEGIN_ARG\npath\nEND_ARG\nout.txt\nBEGIN_ARG\ncontent\nEND_ARG\nintro\n```\nx\n```\n```",
			key:  "content",
			want: "intro",
		},
		{
			name: "a last argument keeps its trailing text less one line break",
			raw:  "```tool\nTOOL_NAME\nwrite_file\nBEGIN_ARG\npath\nEND_ARG\nout.txt\nBEGIN_ARG\ncontent\nEND_ARG\n  first\nsecond  \n\n```",
			key:  "content",
			want: "  first\nsecond  \n",
		},
		{
			name: "a Python new_string keeps its indentation",
			raw:  "```tool\nTOOL_NAME\nedit_file\nBEGIN_ARG\npath\nEND_ARG\ngeo.py\nBEGIN_ARG\nnew_string\nEND_ARG\n" + python + "\n```",
			key:  "new_string",
			want: python,
		},
		{
			name: "a value between two arguments drops one line break each side",
			raw:  "```tool\nTOOL_NAME\nedit_file\nBEGIN_ARG\nold_string\nEND_ARG\n\n    x = 1\n\nBEGIN_ARG\npath\nEND_ARG\na.py\n```",
			key:  "old_string",
			want: "    x = 1",
		},
	}
	p := defaultFencedParser()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			call, ok := p.ParseToolCall(tc.raw)

			if !ok {
				t.Fatal("expected a parsed call")
			}
			if got := argString(t, call.Arguments, tc.key); got != tc.want {
				t.Errorf("%s = %q, want %q", tc.key, got, tc.want)
			}
		})
	}
}

func TestMarkdownFenced_StripKeepsTrailingProseFence(t *testing.T) {
	t.Parallel()
	raw := "Saving.\n\n```tool\nTOOL_NAME\nwrite_file\nBEGIN_ARG\npath\nEND_ARG\nnotes.txt\nBEGIN_ARG\ncontent\nEND_ARG\nplain notes\n```\n\nAfterwards run:\n```bash\nls\n```"

	got := defaultFencedParser().StripToolCall(raw)

	want := "Saving.\n\n\n\nAfterwards run:\n```bash\nls\n```"
	if got != want {
		t.Errorf("strip = %q, want %q", got, want)
	}
}

func TestMarkdownFenced_JSONValueStaysVerbatim(t *testing.T) {
	t.Parallel()
	packageJSON := "{\n  \"name\": \"demo\",\n  \"private\": true\n}"
	raw := "```tool\nTOOL_NAME\nwrite_file\nBEGIN_ARG\npath\nEND_ARG\npackage.json\nBEGIN_ARG\ncontent\nEND_ARG\n" + packageJSON + "\n```"

	call, ok := defaultFencedParser().ParseToolCall(raw)

	if !ok {
		t.Fatal("expected a parsed call")
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		t.Fatalf("arguments are not a JSON object: %v (%s)", err, call.Arguments)
	}
	var content string
	if err := json.Unmarshal(args["content"], &content); err != nil {
		t.Fatalf("content is not a JSON string: %v (%s)", err, args["content"])
	}
	if content != packageJSON {
		t.Errorf("content = %q, want the verbatim %q", content, packageJSON)
	}
}

func TestMarkdownFenced_TypedParamsStayVerbatim(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "read_file start_line stays the string 42",
			raw:  "```tool\nTOOL_NAME\nread_file\nBEGIN_ARG\npath\nEND_ARG\nmain.go\nBEGIN_ARG\nstart_line\nEND_ARG\n42\n```",
			want: `{"path":"main.go","start_line":"42"}`,
		},
		{
			name: "ask_user choices stays the array's text",
			raw:  "```tool\nTOOL_NAME\nask_user\nBEGIN_ARG\nquestion\nEND_ARG\nWhich one?\nBEGIN_ARG\nchoices\nEND_ARG\n[\"red\", \"blue\"]\n```",
			want: `{"choices":"[\"red\", \"blue\"]","question":"Which one?"}`,
		},
	}
	p := defaultFencedParser()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			call, ok := p.ParseToolCall(tc.raw)

			if !ok {
				t.Fatal("expected a parsed call")
			}
			if string(call.Arguments) != tc.want {
				t.Errorf("Arguments = %s, want %s", call.Arguments, tc.want)
			}
		})
	}
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
