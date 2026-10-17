package main

import (
	"strings"
	"testing"
)

func TestParseSelector(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		spelled string
		want    EntrySelector
		wantErr string
	}{
		{name: "kind alone", spelled: "kind=interjected", want: EntrySelector{Kind: "interjected"}},
		{name: "text", spelled: "kind=assistant,text=Fixed", want: EntrySelector{Kind: "assistant", Text: "Fixed"}},
		{name: "tool and target", spelled: "kind=toolCall,tool=Replace,target=task.go",
			want: EntrySelector{Kind: "toolCall", Tool: "Replace", Target: "task.go"}},
		{name: "nth number", spelled: "kind=toolCall,tool=Tests,nth=2", want: EntrySelector{Kind: "toolCall", Tool: "Tests", Nth: 2}},
		{name: "nth last", spelled: "kind=toolCall,tool=Tests,nth=last", want: EntrySelector{Kind: "toolCall", Tool: "Tests", Nth: NthLast}},
		{name: "keys in any order", spelled: "tool=Sub-Agent,kind=toolCall", want: EntrySelector{Kind: "toolCall", Tool: "Sub-Agent"}},
		{name: "whitespace around pairs", spelled: " kind=toolCall , target=CHANGELOG.md ",
			want: EntrySelector{Kind: "toolCall", Target: "CHANGELOG.md"}},
		{name: "workflow kinds", spelled: "kind=workflow_item", want: EntrySelector{Kind: "workflow_item"}},
		{name: "missing kind", spelled: "tool=Replace", wantErr: "want kind=<entry kind>"},
		{name: "empty", spelled: "", wantErr: "want key=value"},
		{name: "unknown kind", spelled: "kind=toolcall", wantErr: `kind: unknown entry kind "toolcall"`},
		{name: "unknown key", spelled: "kind=toolCall,contains=PASS", wantErr: `unknown key "contains"`},
		{name: "pair without equals", spelled: "kind=toolCall,Replace", wantErr: `"Replace": want key=value`},
		{name: "empty value", spelled: "kind=toolCall,tool=", wantErr: "tool: want a value"},
		{name: "repeated key", spelled: "kind=toolCall,tool=Tests,tool=Replace", wantErr: "tool: given twice"},
		{name: "nth zero", spelled: "kind=toolCall,nth=0", wantErr: `nth: want a positive integer or last, got "0"`},
		{name: "nth word", spelled: "kind=toolCall,nth=first", wantErr: `nth: want a positive integer or last, got "first"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseSelector(tc.spelled)

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v (selector %+v)", tc.wantErr, err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSelector(%q): %v", tc.spelled, err)
			}
			if got != tc.want {
				t.Errorf("want %+v, got %+v", tc.want, got)
			}
		})
	}
}

func TestEntrySelectorString(t *testing.T) {
	t.Parallel()
	cases := map[string]EntrySelector{
		"{kind: toolCall, tool: Replace, target: task.go}": {Kind: "toolCall", Tool: "Replace", Target: "task.go"},
		"{kind: toolCall, tool: Tests, nth: last}":         {Kind: "toolCall", Tool: "Tests", Nth: NthLast},
		"{kind: user, nth: 2}":                             {Kind: "user", Nth: 2},
		"{kind: assistant, text: Fixed}":                   {Kind: "assistant", Text: "Fixed"},
	}
	for want, selector := range cases {
		if got := selector.String(); got != want {
			t.Errorf("want %s, got %s", want, got)
		}
	}
}
