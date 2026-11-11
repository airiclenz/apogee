package gitexec

import "testing"

// TestSubcommand_SkipsLeadingGlobalOptions pins how a failure message finds the subcommand: `-c`
// and `-C` are skipped with their value, any other leading option alone, and a list of options
// only falls back to its first argument.
func TestSubcommand_SkipsLeadingGlobalOptions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "config pair", args: []string{"-c", "k=v", "add", "-A"}, want: "add"},
		{name: "empty config value", args: []string{"-c", "core.excludesFile=", "add", "-A", "--ignore-errors"}, want: "add"},
		{name: "flag without value", args: []string{"--literal-pathspecs", "rm", "--cached"}, want: "rm"},
		{name: "directory", args: []string{"-C", "dir", "status"}, want: "status"},
		{name: "bare subcommand", args: []string{"diff"}, want: "diff"},
		{name: "options only", args: []string{"--version"}, want: "--version"},
		{name: "config pair only", args: []string{"-c", "k=v"}, want: "-c"},
	}
	for _, tc := range cases {
		name, args, want := tc.name, tc.args, tc.want
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := subcommand(args)

			if got != want {
				t.Errorf("subcommand(%q) = %q, want %q", args, got, want)
			}
		})
	}
}
