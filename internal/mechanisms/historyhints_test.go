package mechanisms

import (
	"encoding/json"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// The post-response cascade resolves to autofix → syntax (repair precedes correction). The rows
// that used to head this cascade — the tool-loop detector and the tool-call validator — were
// promoted to Floor guards (ADR 0071) and run AHEAD of every hook, so no ordering edge expresses
// their priority any more, and the history-aware rows that once sat unconstrained beside these two
// retired outright in v0.20.0 on the same verdict.
func TestPostResponseCascadeOrder(t *testing.T) {
	t.Parallel()
	reg := domain.NewMechanismRegistry()
	for _, id := range []domain.MechanismID{autofixID, syntaxID} {
		if err := reg.Add(mustBuild(t, id)); err != nil {
			t.Fatalf("Add(%q): %v", id, err)
		}
	}
	if err := reg.ValidateOrdering(); err != nil {
		t.Fatalf("ValidateOrdering: %v", err)
	}
	want := []domain.MechanismID{autofixID, syntaxID}
	got := reg.Ordered(domain.HookPostResponse)
	if len(got) != len(want) {
		t.Fatalf("Ordered(post-response) has %d mechanisms, want %d", len(got), len(want))
	}
	for i, m := range got {
		if m.Descriptor.ID != want[i] {
			t.Errorf("cascade[%d] = %q, want %q (full order: %v)", i, m.Descriptor.ID, want[i], want)
		}
	}
}

// toolCallPath reads the file a call targets from the four sim-inherited spellings plus
// destination, the key copy_file and move_file carry instead. The precedence is pinned here:
// destination is read last, so a call carrying both path and destination still reports path.
func TestToolCallPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args string
		want string
	}{
		{name: "path", args: `{"path":"alpha.go"}`, want: "alpha.go"},
		{name: "file_path", args: `{"file_path":"beta.go"}`, want: "beta.go"},
		{name: "filePath", args: `{"filePath":"gamma.go"}`, want: "gamma.go"},
		{name: "filename", args: `{"filename":"delta.go"}`, want: "delta.go"},
		{
			name: "copy_file reports the destination",
			args: `{"source":"origin.go","destination":"copy.go"}`,
			want: "copy.go",
		},
		{
			name: "move_file reports the destination",
			args: `{"source":"origin.go","destination":"moved.go","overwrite":true}`,
			want: "moved.go",
		},
		{
			name: "path keeps precedence over destination",
			args: `{"destination":"copy.go","path":"alpha.go"}`,
			want: "alpha.go",
		},
		{name: "source alone is not a path", args: `{"source":"origin.go"}`, want: ""},
		{name: "arguments are not a JSON object", args: `"alpha.go"`, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := toolCallPath(json.RawMessage(tc.args))

			if got != tc.want {
				t.Errorf("toolCallPath(%s) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}
