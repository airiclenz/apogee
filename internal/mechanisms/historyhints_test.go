package mechanisms

import (
	"encoding/json"
	"testing"
)

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
