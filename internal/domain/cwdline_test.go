package domain

import "testing"

// TestStripCwdLine pins the strip on the shapes the consumers hand it: a cwd line comes off
// whole, a body with none is returned untouched, and a cwd line with nothing after it is empty.
func TestStripCwdLine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"cwd: /ws\nhello\n", "hello\n"},
		{"cwd: /ws\n\n[exit code 1]", "\n[exit code 1]"},
		{"hello\n", "hello\n"},
		{"cwd: /ws", ""},
		{"", ""},
		{"\ncwd: /ws\n", "\ncwd: /ws\n"},
	} {
		if got := StripCwdLine(tc.in); got != tc.want {
			t.Errorf("StripCwdLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
