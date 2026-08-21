package domain

import (
	"encoding/json"
	"testing"
)

// The tool-stage edit values are the working-value contract the loop's acted probe rests on:
// every mutator bumps Revision and writes through to the wrapped struct, every accessor reads
// without bumping, and none of it looks at the value being carried — which is what makes an
// UNCOMPARABLE ToolResult.Summary (the ReadSpan every successful read_file returns) a non-event
// here, where the whole-struct compare this replaces panicked.

// locatedSpan is the uncomparable summary a successful read returns: LocatedOn is a slice, so
// two results carrying one cannot be compared with ==.
func locatedSpan() ToolSummary {
	return ReadSpan{Start: 1, End: 3, Total: 3, Locate: "main", LocatedOn: []int{1, 2}}
}

func TestToolResultEditMutatorsBumpAndWriteThrough(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(*ToolResultEdit)
		wantFrom func(ToolResult) bool
	}{
		{
			name:     "SetContent replaces the prose",
			mutate:   func(e *ToolResultEdit) { e.SetContent("enriched") },
			wantFrom: func(r ToolResult) bool { return r.Content == "enriched" },
		},
		{
			name:     "SetIsError moves the authoritative flag",
			mutate:   func(e *ToolResultEdit) { e.SetIsError(true) },
			wantFrom: func(r ToolResult) bool { return r.IsError },
		},
		{
			name:   "SetSummary replaces the structured half",
			mutate: func(e *ToolResultEdit) { e.SetSummary(ReadSpan{Start: 9, End: 9, Total: 9}) },
			wantFrom: func(r ToolResult) bool {
				span, ok := r.Summary.(ReadSpan)
				return ok && span.Start == 9 && span.LocatedOn == nil
			},
		},
		{
			name:     "SetSummary(nil) clears it back to prose-only",
			mutate:   func(e *ToolResultEdit) { e.SetSummary(nil) },
			wantFrom: func(r ToolResult) bool { return r.Summary == nil },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ToolResult{CallID: "c1", Content: "body", Summary: locatedSpan()}
			edit := NewToolResultEdit(&result)

			tt.mutate(edit)

			if edit.Revision() != 1 {
				t.Errorf("Revision after one mutation = %d, want 1", edit.Revision())
			}
			if !tt.wantFrom(result) {
				t.Errorf("the mutation did not reach the wrapped result: %+v", result)
			}
		})
	}
}

// A read is not an act: the accessors an inspect-and-do-nothing hook uses must leave the
// counter where it was, or every invocation would book a fire.
func TestToolResultEditAccessorsDoNotBump(t *testing.T) {
	t.Parallel()
	result := ToolResult{CallID: "c1", Content: "body", IsError: true, Summary: locatedSpan()}
	edit := NewToolResultEdit(&result)

	if got := edit.CallID(); got != "c1" {
		t.Errorf("CallID() = %q, want %q", got, "c1")
	}
	if got := edit.Content(); got != "body" {
		t.Errorf("Content() = %q, want %q", got, "body")
	}
	if !edit.IsError() {
		t.Error("IsError() = false, want true")
	}
	// The comparison is field-wise on purpose: ReadSpan carries a slice, so == on two
	// ToolSummary interfaces holding one panics — the very failure the counter retires.
	if span, ok := edit.Summary().(ReadSpan); !ok || span.Locate != "main" {
		t.Errorf("Summary() did not return the carried summary; got %#v", edit.Summary())
	}

	if edit.Revision() != 0 {
		t.Errorf("Revision after reads only = %d, want 0", edit.Revision())
	}
}

// A mutator is an act even when it writes the value that was already there — the same
// convention Request's mutators follow. The counter states what the hook DID, not whether the
// bytes ended up different, which is exactly why it cannot inspect an uncomparable value.
func TestToolResultEditCountsEveryMutation(t *testing.T) {
	t.Parallel()
	result := ToolResult{CallID: "c1", Content: "body", Summary: locatedSpan()}
	edit := NewToolResultEdit(&result)

	edit.SetContent("body")
	edit.SetContent("body")

	if edit.Revision() != 2 {
		t.Errorf("Revision after two mutator calls = %d, want 2", edit.Revision())
	}
}

func TestToolCallEditMutatorsBumpAndWriteThrough(t *testing.T) {
	t.Parallel()
	call := ToolCall{ID: "c1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)}
	edit := NewToolCallEdit(&call)

	edit.SetArguments(json.RawMessage(`{"path":"a.go","max_lines":40}`))
	edit.SetTool("read_head")

	if edit.Revision() != 2 {
		t.Errorf("Revision after two mutations = %d, want 2", edit.Revision())
	}
	if call.Tool != "read_head" {
		t.Errorf("wrapped call Tool = %q, want %q", call.Tool, "read_head")
	}
	if string(call.Arguments) != `{"path":"a.go","max_lines":40}` {
		t.Errorf("wrapped call Arguments = %s, want the capped object", call.Arguments)
	}
	if call.ID != "c1" {
		t.Errorf("the call id must survive a reshape; got %q", call.ID)
	}
}

// Arguments hands out a copy on the way out and takes one on the way in, so the only route to
// the pending call is a counted mutator — a hook writing through either slice must not be able
// to change the call behind the counter's back.
func TestToolCallEditArgumentsAreCopied(t *testing.T) {
	t.Parallel()
	call := ToolCall{ID: "c1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)}
	edit := NewToolCallEdit(&call)

	read := edit.Arguments()
	read[0] = 'X'

	if string(call.Arguments) != `{"path":"a.go"}` {
		t.Errorf("writing through Arguments() reached the call: %s", call.Arguments)
	}

	set := json.RawMessage(`{"path":"b.go"}`)
	edit.SetArguments(set)
	set[0] = 'X'

	if string(call.Arguments) != `{"path":"b.go"}` {
		t.Errorf("writing through the slice passed to SetArguments reached the call: %s", call.Arguments)
	}
	if edit.Revision() != 1 {
		t.Errorf("Revision after one mutation = %d, want 1", edit.Revision())
	}
}

// Reading the pending call is not an act, for the same reason it is not on the result side.
func TestToolCallEditAccessorsDoNotBump(t *testing.T) {
	t.Parallel()
	call := ToolCall{ID: "c1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)}
	edit := NewToolCallEdit(&call)

	if got := edit.ID(); got != "c1" {
		t.Errorf("ID() = %q, want %q", got, "c1")
	}
	if got := edit.Tool(); got != "read_file" {
		t.Errorf("Tool() = %q, want %q", got, "read_file")
	}
	if got := string(edit.Arguments()); got != `{"path":"a.go"}` {
		t.Errorf("Arguments() = %s, want the model's argument object", got)
	}

	if edit.Revision() != 0 {
		t.Errorf("Revision after reads only = %d, want 0", edit.Revision())
	}
}
