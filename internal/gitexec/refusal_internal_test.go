package gitexec

import "testing"

// TestIsIncompleteRefusal_TellsAnUnfinishedProbeFromARefusedRepository pins the predicate the data
// path sorts its refusals by: true for the incomplete-probe sentence whatever its reason, false
// for the command-config refusal and for anything else git might print.
func TestIsIncompleteRefusal_TellsAnUnfinishedProbeFromARefusedRepository(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		CommandConfigIncompleteRefusal("git config --local timed out after 15s"): true,
		CommandConfigIncompleteRefusal(""):                                       true,
		CommandConfigRefusal([]string{"core.sshcommand"}):                        false,
		"fatal: not a git repository (or any of the parent directories): .git":   false,
		"": false,
	}
	for text, want := range cases {
		if got := isIncompleteRefusal(text); got != want {
			t.Errorf("isIncompleteRefusal(%q) = %v, want %v", text, got, want)
		}
	}
}
