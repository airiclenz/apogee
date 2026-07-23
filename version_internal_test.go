package apogee

import (
	"runtime/debug"
	"testing"
)

// TestBuildMetadata covers the pure VCS-provenance composer with synthetic build settings, so
// the "+g<commit>[.dirty]" suffix logic is verified independent of the machine's real build stamp.
func TestBuildMetadata(t *testing.T) {
	t.Parallel()

	const fullRev = "28b6f838e6e1a9a38357381412c3d975995a7007" // > commitShortLen, gets truncated

	tests := []struct {
		name     string
		settings []debug.BuildSetting
		want     string
	}{
		{
			name:     "clean commit truncates to the short length",
			settings: []debug.BuildSetting{{Key: "vcs.revision", Value: fullRev}, {Key: "vcs.modified", Value: "false"}},
			want:     "g28b6f838e6e1",
		},
		{
			name:     "dirty commit appends the marker",
			settings: []debug.BuildSetting{{Key: "vcs.revision", Value: fullRev}, {Key: "vcs.modified", Value: "true"}},
			want:     "g28b6f838e6e1.dirty",
		},
		{
			name:     "a revision shorter than the short length is kept whole",
			settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}},
			want:     "gabc123",
		},
		{
			name:     "no revision yields no suffix",
			settings: []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}},
			want:     "",
		},
		{
			name:     "no settings at all yields no suffix",
			settings: nil,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := buildMetadata(tt.settings); got != tt.want {
				t.Errorf("buildMetadata() = %q; want %q", got, tt.want)
			}
		})
	}
}
