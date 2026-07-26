package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadAllBounded pins the bound step of the one-handle read in isolation: at most max
// bytes are accepted, the max+1'th byte flips the verdict, and never more than max+1 bytes
// are drained. This table is the deterministic cover for readWorkspaceFileBounded's
// growth-backstop branch (a file grown past the cap between the fstat and the read), which
// cannot be driven through the shared body without interleaving a writer.
func TestReadAllBounded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		input      string
		max        int64
		wantWithin bool
	}{
		{"under the cap", "abc", 4, true},
		{"exactly the cap", "abcd", 4, true},
		{"one past the cap", "abcde", 4, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, within, err := readAllBounded(strings.NewReader(tc.input), tc.max)

			if err != nil {
				t.Fatalf("readAllBounded: %v", err)
			}
			if within != tc.wantWithin {
				t.Errorf("within = %v, want %v", within, tc.wantWithin)
			}
			if within && string(data) != tc.input {
				t.Errorf("data = %q, want %q", data, tc.input)
			}
			if int64(len(data)) > tc.max+1 {
				t.Errorf("drained %d bytes, want at most max+1 = %d", len(data), tc.max+1)
			}
		})
	}
}

// TestReadWorkspaceFileBounded_RefusesOversizeFile drives the shared body's fstat refusal:
// a file statically past maxFileReadBytes is refused with the exact model-facing message
// the twins rendered before, and its bytes are never materialised. The file is sparse (a
// Truncate with nothing written), so the test costs no 10 MiB write.
func TestReadWorkspaceFileBounded_RefusesOversizeFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	f, err := os.Create(filepath.Join(root, "big.bin"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := f.Truncate(maxFileReadBytes + 1); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	data, failMessage := readWorkspaceFileBounded("big.bin", root)

	if data != nil {
		t.Errorf("data = %d bytes, want nil on a refusal", len(data))
	}
	want := fmt.Sprintf("file too large: %d bytes (max %d)", int64(maxFileReadBytes)+1, int64(maxFileReadBytes))
	if failMessage != want {
		t.Errorf("failMessage = %q, want %q", failMessage, want)
	}
}
