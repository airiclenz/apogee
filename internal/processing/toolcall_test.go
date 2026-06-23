package processing

import (
	"errors"
	"testing"
)

func TestParseNativeToolCalls_ValidCalls_NormaliseToDomain(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		in       NativeToolCall
		wantID   string
		wantTool string
		wantArgs string
	}{
		{
			name:     "object arguments are preserved verbatim",
			in:       NativeToolCall{ID: "call_1", Name: "read_file", Arguments: `{"path":"src/main.go"}`},
			wantID:   "call_1",
			wantTool: "read_file",
			wantArgs: `{"path":"src/main.go"}`,
		},
		{
			name:     "empty arguments normalise to the empty object",
			in:       NativeToolCall{ID: "call_2", Name: "list_dir", Arguments: ""},
			wantID:   "call_2",
			wantTool: "list_dir",
			wantArgs: "{}",
		},
		{
			name:     "whitespace-only arguments normalise to the empty object",
			in:       NativeToolCall{ID: "call_3", Name: "list_dir", Arguments: "  \n "},
			wantID:   "call_3",
			wantTool: "list_dir",
			wantArgs: "{}",
		},
		{
			name:     "surrounding whitespace is trimmed from valid arguments",
			in:       NativeToolCall{Name: "grep", Arguments: "  {\"q\":\"x\"}  "},
			wantTool: "grep",
			wantArgs: `{"q":"x"}`,
		},
		{
			name:     "name is trimmed",
			in:       NativeToolCall{Name: "  write_file  ", Arguments: "{}"},
			wantTool: "write_file",
			wantArgs: "{}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseNativeToolCalls([]NativeToolCall{tc.in})
			if err != nil {
				t.Fatalf("ParseNativeToolCalls returned error: %v", err)
			}

			if len(got) != 1 {
				t.Fatalf("len(got) = %d, want 1", len(got))
			}
			if got[0].ID != tc.wantID {
				t.Errorf("ID = %q, want %q", got[0].ID, tc.wantID)
			}
			if got[0].Tool != tc.wantTool {
				t.Errorf("Tool = %q, want %q", got[0].Tool, tc.wantTool)
			}
			if string(got[0].Arguments) != tc.wantArgs {
				t.Errorf("Arguments = %q, want %q", string(got[0].Arguments), tc.wantArgs)
			}
		})
	}
}

func TestParseNativeToolCalls_MultipleCalls_PreserveOrder(t *testing.T) {
	t.Parallel()

	got, err := ParseNativeToolCalls([]NativeToolCall{
		{ID: "a", Name: "read_file", Arguments: `{"path":"a"}`},
		{ID: "b", Name: "read_file", Arguments: `{"path":"b"}`},
	})
	if err != nil {
		t.Fatalf("ParseNativeToolCalls returned error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].ID != "a" || got[1].ID != "b" {
		t.Errorf("order not preserved: got IDs %q, %q", got[0].ID, got[1].ID)
	}
}

func TestParseNativeToolCalls_EmptyInput_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	got, err := ParseNativeToolCalls(nil)
	if err != nil {
		t.Fatalf("ParseNativeToolCalls returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}

func TestParseNativeToolCalls_MalformedCall_DegradesToParseError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   NativeToolCall
	}{
		{"missing name", NativeToolCall{ID: "x", Arguments: "{}"}},
		{"blank name", NativeToolCall{Name: "   ", Arguments: "{}"}},
		{"invalid json arguments", NativeToolCall{Name: "read_file", Arguments: `{"path":`}},
		{"non-object arguments", NativeToolCall{Name: "read_file", Arguments: `["a","b"]`}},
		{"json scalar arguments", NativeToolCall{Name: "read_file", Arguments: `42`}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseNativeToolCalls([]NativeToolCall{tc.in})

			if !errors.Is(err, ErrMalformedToolCall) {
				t.Fatalf("err = %v, want wrapped ErrMalformedToolCall", err)
			}
			if got != nil {
				t.Errorf("got = %v, want nil results on a malformed call", got)
			}
		})
	}
}

func TestParseNativeToolCalls_OneMalformedInBatch_FailsAtomically(t *testing.T) {
	t.Parallel()

	got, err := ParseNativeToolCalls([]NativeToolCall{
		{ID: "good", Name: "read_file", Arguments: "{}"},
		{ID: "bad", Name: "", Arguments: "{}"},
	})

	if !errors.Is(err, ErrMalformedToolCall) {
		t.Fatalf("err = %v, want wrapped ErrMalformedToolCall", err)
	}
	if got != nil {
		t.Errorf("got = %v, want no partial batch to reach dispatch", got)
	}
}
