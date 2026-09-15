package tools

import "testing"

// TestClosestToolName pins the two rungs of the unknown-tool near-match — a prefix first, then
// an edit distance of at most three — and that a name unlike every registered one answers "".
func TestClosestToolName(t *testing.T) {
	t.Parallel()

	names := []string{"read_file", "write_file", "list_dir", "terminal", "grep", "find_files"}
	cases := []struct {
		name string
		want string
		got  string
	}{
		{name: "a truncated name is its prefix's owner", want: "read_fil", got: "read_file"},
		{name: "a dropped letter is within distance", want: "wrte_file", got: "write_file"},
		{name: "a transposition is within distance", want: "gerp", got: "grep"},
		{name: "case is ignored", want: "LIST_DIR", got: "list_dir"},
		{name: "a different word matches nothing", want: "bash", got: ""},
		{name: "an empty name matches nothing", want: "", got: ""},
		{name: "a prefix outranks a closer edit", want: "read", got: "read_file"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := ClosestToolName(names, tc.want); got != tc.got {
				t.Errorf("ClosestToolName(%q) = %q, want %q", tc.want, got, tc.got)
			}
		})
	}
}

// TestClosestToolName_TiesAreStable proves two names at the same distance always answer the
// lexicographically smaller one, whatever order the registry handed them in.
func TestClosestToolName_TiesAreStable(t *testing.T) {
	t.Parallel()

	if got := ClosestToolName([]string{"tool_b", "tool_a"}, "tool_"); got != "tool_a" {
		t.Errorf("ClosestToolName = %q, want the smaller of two equal prefix matches", got)
	}
	if got := ClosestToolName([]string{"grip", "grap"}, "grep"); got != "grap" {
		t.Errorf("ClosestToolName = %q, want the smaller of two equal-distance matches", got)
	}
}
