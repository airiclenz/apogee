package tui

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// detailsText joins a view's detail lines for substring assertions.
func detailsText(tv toolView) string {
	parts := make([]string, 0, len(tv.Details))
	for _, d := range tv.Details {
		parts = append(parts, d.Text)
	}
	return strings.Join(parts, "\n")
}

// TestPresentToolCall proves the open registry: each default tool maps to its friendly label
// and a target pulled from the arguments, its fixed result header summarises to one detail
// line, and an unknown or malformed call falls back to the raw name with its arguments shown
// verbatim (the approval surface never hides the model's request).
func TestPresentToolCall(t *testing.T) {
	tests := []struct {
		name        string
		call        domain.ToolCall
		result      domain.ToolResult
		wantLabel   string
		wantTarget  string
		wantBracket bool
		wantDetail  string // a substring expected in the view's detail lines
	}{
		{
			name:       "read_file → Read File + line range",
			call:       domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)},
			result:     domain.ToolResult{CallID: "1", Content: "[File: main.go, 120 lines total, showing lines 1-100]\npackage main"},
			wantLabel:  "Read File",
			wantTarget: "main.go", wantBracket: true, wantDetail: "1 - 100",
		},
		{
			name:       "write_file → Write File + byte count",
			call:       domain.ToolCall{ID: "2", Tool: "write_file", Arguments: []byte(`{"path":"notes.txt","content":"hello"}`)},
			result:     domain.ToolResult{CallID: "2", Content: "wrote 5 bytes to notes.txt"},
			wantLabel:  "Write File",
			wantTarget: "notes.txt", wantBracket: true, wantDetail: "+5 bytes",
		},
		{
			name:       "list_dir → List Dir + entry count",
			call:       domain.ToolCall{ID: "3", Tool: "list_dir", Arguments: []byte(`{"path":"src"}`)},
			result:     domain.ToolResult{CallID: "3", Content: "[12 entries total]\nfoo\nbar"},
			wantLabel:  "List Dir",
			wantTarget: "src", wantBracket: true, wantDetail: "12 entries",
		},
		{
			name:       "grep → Search + match count",
			call:       domain.ToolCall{ID: "4", Tool: "grep", Arguments: []byte(`{"pattern":"TODO"}`)},
			result:     domain.ToolResult{CallID: "4", Content: "[3 total matches, showing 1-3]\na\nb\nc"},
			wantLabel:  "Search",
			wantTarget: "TODO", wantBracket: true, wantDetail: "3 matches",
		},
		{
			name:        "grep with no matches → 0 matches",
			call:        domain.ToolCall{ID: "5", Tool: "grep", Arguments: []byte(`{"pattern":"zzz"}`)},
			result:      domain.ToolResult{CallID: "5", Content: "No matches found"},
			wantLabel:   "Search",
			wantTarget:  "zzz",
			wantBracket: true, wantDetail: "0 matches",
		},
		{
			name:        "unknown tool → raw label, bare, JSON args as detail",
			call:        domain.ToolCall{ID: "6", Tool: "frobnicate", Arguments: []byte(`{"x":1}`)},
			wantLabel:   "frobnicate",
			wantTarget:  "",
			wantBracket: false, wantDetail: `"x": 1`,
		},
		{
			name:        "malformed args → shown verbatim, not dropped",
			call:        domain.ToolCall{ID: "7", Tool: "weird", Arguments: []byte("{not json")},
			wantLabel:   "weird",
			wantTarget:  "",
			wantBracket: false, wantDetail: "{not json",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tv := presentToolCall(tc.call)
			if tv.Label != tc.wantLabel {
				t.Errorf("Label = %q, want %q", tv.Label, tc.wantLabel)
			}
			if tv.Target != tc.wantTarget {
				t.Errorf("Target = %q, want %q", tv.Target, tc.wantTarget)
			}
			if tv.bracket != tc.wantBracket {
				t.Errorf("bracket = %v, want %v", tv.bracket, tc.wantBracket)
			}
			if tc.result.Content != "" {
				tv.enrichWithResult(tc.result)
			}
			if got := detailsText(tv); !strings.Contains(got, tc.wantDetail) {
				t.Errorf("details = %q; want a line containing %q", got, tc.wantDetail)
			}
		})
	}
}

// An error result is summarised as an "error: …" detail rather than the tool's normal
// summary — a normal in-band outcome the model reacts to.
func TestPresentToolCallErrorResult(t *testing.T) {
	tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"missing"}`)})
	tv.enrichWithResult(domain.ToolResult{CallID: "1", Content: "file not found: missing", IsError: true})
	if got := detailsText(tv); !strings.Contains(got, "error: file not found: missing") {
		t.Errorf("error result detail = %q; want the error text", got)
	}
}
