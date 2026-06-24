package processing

import (
	"fmt"
	"testing"
)

func TestNewToolCallParser_SelectsByFormat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		cfg    ToolCallingConfig
		typeOf string
	}{
		{"default is native no-op", ToolCallingConfig{}, "processing.nativeTextParser"},
		{"explicit native is no-op", ToolCallingConfig{Format: FormatNative}, "processing.nativeTextParser"},
		{"markdown-fenced", ToolCallingConfig{Format: FormatMarkdownFenced}, "*processing.MarkdownFencedParser"},
		{"custom-regex", ToolCallingConfig{Format: FormatCustomRegex, CustomRegex: CustomRegexConfig{Pattern: toolCallPattern}}, "*processing.CustomRegexParser"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, err := NewToolCallParser(tc.cfg)
			if err != nil {
				t.Fatalf("NewToolCallParser returned error: %v", err)
			}
			if got := typeName(p); got != tc.typeOf {
				t.Errorf("parser type = %s, want %s", got, tc.typeOf)
			}
		})
	}
}

func TestNewToolCallParser_UnknownFormatErrors(t *testing.T) {
	t.Parallel()

	if _, err := NewToolCallParser(ToolCallingConfig{Format: "yaml-block"}); err == nil {
		t.Fatal("expected an error for an unknown format")
	}
}

func TestNewToolCallParser_NativeFindsNothingInText(t *testing.T) {
	t.Parallel()

	// The native parser is the structured path's text no-op: a fenced block in the text is not
	// its concern, so it reports no call and strips nothing.
	p, err := NewToolCallParser(ToolCallingConfig{Format: FormatNative})
	if err != nil {
		t.Fatalf("NewToolCallParser: %v", err)
	}
	raw := "```tool\nTOOL_NAME\nread_file\nBEGIN_ARG\npath\nEND_ARG\nx\n```"
	if _, ok := p.ParseToolCall(raw); ok {
		t.Error("native parser should find no call in text")
	}
	if got := p.StripToolCall(raw); got != raw {
		t.Errorf("native StripToolCall = %q, want unchanged", got)
	}
}

func TestNewToolCallParser_RoundTripsAFencedCall(t *testing.T) {
	t.Parallel()

	p, err := NewToolCallParser(ToolCallingConfig{Format: FormatMarkdownFenced})
	if err != nil {
		t.Fatalf("NewToolCallParser: %v", err)
	}
	raw := "```tool\nTOOL_NAME\nlist_dir\nBEGIN_ARG\npath\nEND_ARG\n.\n```"
	call, ok := p.ParseToolCall(raw)
	if !ok {
		t.Fatal("expected the factory-selected parser to parse the call")
	}
	if call.Tool != "list_dir" {
		t.Errorf("Tool = %q, want list_dir", call.Tool)
	}
}

// typeName returns the dynamic type name of v as Go prints it with %T — used to assert which
// concrete parser the factory selected.
func typeName(v any) string {
	return fmt.Sprintf("%T", v)
}
