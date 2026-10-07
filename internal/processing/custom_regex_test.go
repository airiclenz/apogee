package processing

import (
	"errors"
	"fmt"
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

	t.Run("marks non-JSON args malformed instead of wrapping them as raw", func(t *testing.T) {
		t.Parallel()
		// The oracle wrapped non-JSON args as {"raw": "<group>"}; apogee marks the call
		// malformed so the model is told its arguments were not a JSON object.
		p := NewCustomRegexParser(CustomRegexConfig{Pattern: `<tool>(?<name>\w+):(?<args>[^<]+)</tool>`})
		raw := `<tool>read_file:src/main.ts</tool>`
		call, ok := p.ParseToolCall(raw)
		if !ok {
			t.Fatal("expected a parsed call")
		}
		if call.Tool != "read_file" {
			t.Errorf("Tool = %q, want read_file", call.Tool)
		}
		if string(call.Arguments) != "{}" {
			t.Errorf("Arguments = %s, want {} — no raw key the model never sent", call.Arguments)
		}
		if call.Malformed == nil || call.Malformed.Raw != "src/main.ts" {
			t.Errorf("Malformed = %+v, want the marker carrying the group text src/main.ts", call.Malformed)
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

// TestCustomRegex_NonObjectArgsAreMalformed: an args group that is not a JSON object is marked
// malformed exactly as a native call is — the encoding/json parse error, the group text as raw,
// and the empty object as Arguments — and valid JSON that is not an object is marked too.
func TestCustomRegex_NonObjectArgsAreMalformed(t *testing.T) {
	t.Parallel()

	p := NewCustomRegexParser(CustomRegexConfig{Pattern: `(?<name>\w+)\s+(?<args>.*)`})
	for _, tc := range []struct {
		name      string
		raw       string
		wantError string
		wantRaw   string
	}{
		{name: "shell words", raw: "bash ls -la", wantError: "invalid character 'l' looking for beginning of value", wantRaw: "ls -la"},
		{name: "a JSON array", raw: `bash ["ls"]`, wantError: notAJSONObject, wantRaw: `["ls"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			call, ok := p.ParseToolCall(tc.raw)
			if !ok {
				t.Fatal("expected a parsed call")
			}
			if call.Tool != "bash" {
				t.Errorf("Tool = %q, want bash", call.Tool)
			}
			if string(call.Arguments) != "{}" {
				t.Errorf("Arguments = %s, want {}", call.Arguments)
			}
			if call.Malformed == nil {
				t.Fatal("Malformed = nil, want the marker")
			}
			if got := call.Malformed.Err.Error(); got != tc.wantError {
				t.Errorf("marker error = %q, want %q", got, tc.wantError)
			}
			if !errors.Is(call.Malformed.Err, ErrMalformedToolCall) {
				t.Errorf("marker error %v does not wrap ErrMalformedToolCall", call.Malformed.Err)
			}
			if call.Malformed.Raw != tc.wantRaw {
				t.Errorf("marker raw = %q, want %q", call.Malformed.Raw, tc.wantRaw)
			}
		})
	}
}

// TestCustomRegex_EmptyArgsAreEmptyObject: an empty or whitespace-only args group is a
// no-argument call — the unmarked empty object — and a JSON-object group is kept verbatim.
func TestCustomRegex_EmptyArgsAreEmptyObject(t *testing.T) {
	t.Parallel()

	p := NewCustomRegexParser(CustomRegexConfig{Pattern: `\[(?<name>\w+)(?<args>[^\]]*)\]`})
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty group", raw: "[refresh]", want: "{}"},
		{name: "whitespace-only group", raw: "[refresh   ]", want: "{}"},
		{name: "JSON object group", raw: `[refresh {"all":true}]`, want: `{"all":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			call, ok := p.ParseToolCall(tc.raw)
			if !ok {
				t.Fatal("expected a parsed call")
			}
			if string(call.Arguments) != tc.want {
				t.Errorf("Arguments = %s, want %s", call.Arguments, tc.want)
			}
			if call.Malformed != nil {
				t.Errorf("Malformed = %+v, want nil", call.Malformed)
			}
		})
	}
}

// manualPattern is the custom-regex pattern docs/manual/configuration.md documents.
const manualPattern = `<tool_call>\s*(?<name>[\w.-]+)\s*(?<args>\{.*?\})\s*</tool_call>`

// TestValidateCustomRegexProfile pins the load check: a pattern lacking a group is a pattern
// fault, an example the pattern does not read as a tool with JSON-object arguments — given, or
// derived from the pattern as the probe call — is an example fault, and each error quotes the
// failing example.
func TestValidateCustomRegexProfile(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		pattern   string
		example   string
		wantErr   error
		wantQuote string
	}{
		{name: "the manual pattern derives a call it parses", pattern: manualPattern},
		{name: "a given example the pattern parses", pattern: manualPattern, example: `<tool_call>ls {"path": "."}</tool_call>`},
		{
			name:      "no args group",
			pattern:   `<tool_call>(?<name>\w+)</tool_call>`,
			example:   `<tool_call>ls</tool_call>`,
			wantErr:   ErrCustomRegexPattern,
			wantQuote: `<tool_call>ls</tool_call>`,
		},
		{
			name:      "a pattern that does not compile",
			pattern:   `<tool_call>(?<name>\w+)(?<args>\{.*?\}</tool_call>`,
			example:   `<tool_call>ls{}</tool_call>`,
			wantErr:   ErrCustomRegexPattern,
			wantQuote: `<tool_call>ls{}</tool_call>`,
		},
		{
			name:      "an example the pattern does not parse",
			pattern:   manualPattern,
			example:   `bash {"command": "ls"}`,
			wantErr:   ErrCustomRegexExample,
			wantQuote: `bash {"command": "ls"}`,
		},
		{
			name:      "an example whose arguments are not a JSON object",
			pattern:   `<tool_call>(?<name>\w+)\s+(?<args>.*?)</tool_call>`,
			example:   `<tool_call>bash [1]</tool_call>`,
			wantErr:   ErrCustomRegexExample,
			wantQuote: `<tool_call>bash [1]</tool_call>`,
		},
		{
			name:      "a derived call that does not parse back",
			pattern:   `(?<name>\w+)[:;](?<args>\{.*\})`,
			wantErr:   ErrCustomRegexExample,
			wantQuote: `read_file[:;]{"path": "src/main.ts"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateCustomRegexProfile(tc.pattern, tc.example)

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateCustomRegexProfile error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateCustomRegexProfile error = %v, want one wrapping %v", err, tc.wantErr)
			}
			if quoted := fmt.Sprintf("%q", tc.wantQuote); !strings.Contains(err.Error(), quoted) {
				t.Errorf("ValidateCustomRegexProfile error = %v, want it to quote %s", err, quoted)
			}
		})
	}
}
