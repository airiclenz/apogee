package format

import "testing"

// TestBytes pins the byte ladder: binary units, one decimal above the bare rung — a size below a
// kibibyte stays in bytes, a kibibyte-scale size reads in KiB, a mebibyte-scale one in MiB.
func TestBytes(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{900, "900 B"},
		{3174, "3.1 KiB"},
		{2 * 1024 * 1024, "2.0 MiB"},
	}

	for _, tt := range tests {
		if got := Bytes(tt.n); got != tt.want {
			t.Errorf("Bytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
