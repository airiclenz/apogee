package console

import "testing"

// TestStripEscapesRemovesTerminalControlSequences covers the two families a program running under
// a pseudo-terminal actually emits — CSI and OSC — plus the text that must survive untouched.
func TestStripEscapesRemovesTerminalControlSequences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain text is untouched",
			in:   "hello world\n$ ",
			want: "hello world\n$ ",
		},
		{
			name: "text with no escape at all is returned as is",
			in:   "no [brackets] or ]osc] here",
			want: "no [brackets] or ]osc] here",
		},
		{
			name: "CSI colour",
			in:   "\x1b[31mred\x1b[0m",
			want: "red",
		},
		{
			name: "CSI colour with several parameters",
			in:   "\x1b[1;38;5;208mbright\x1b[m done",
			want: "bright done",
		},
		{
			name: "CSI cursor moves",
			in:   "one\x1b[2Atwo\x1b[10;20Hthree\x1b[2Kfour",
			want: "onetwothreefour",
		},
		{
			name: "CSI private-mode toggle with intermediates",
			in:   "\x1b[?25lhidden\x1b[?25h",
			want: "hidden",
		},
		{
			name: "OSC window title terminated by BEL",
			in:   "\x1b]0;a title\x07prompt> ",
			want: "prompt> ",
		},
		{
			name: "OSC terminated by ST",
			in:   "\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\",
			want: "link",
		},
		{
			name: "a lone ESC costs only itself",
			in:   "before\x1bafter",
			want: "beforeafter",
		},
		{
			name: "an incomplete CSI keeps its text",
			in:   "tail\x1b[3",
			want: "tail[3",
		},
		{
			name: "an unterminated OSC keeps its text",
			in:   "\x1b]0;never ends",
			want: "]0;never ends",
		},
		{
			name: "a trailing ESC is dropped",
			in:   "done\x1b",
			want: "done",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := stripEscapes(test.in); got != test.want {
				t.Errorf("stripEscapes(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}
