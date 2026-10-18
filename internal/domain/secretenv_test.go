package domain

import (
	"slices"
	"testing"
)

// Widen only ever adds: a held name stays, a new one joins after it in the order given, and a name
// already held in another spelling or a blank one is skipped — the scrub's own case-insensitive
// comparison.
func TestSecretEnvWidenOnlyAdds(t *testing.T) {
	t.Parallel()
	holder := NewSecretEnv([]string{"BOX_KEY"})

	holder.Widen([]string{"wiki_token", "box_key", "  ", "WIKI_TOKEN", " BELL_TOKEN "})

	want := []string{"BOX_KEY", "wiki_token", "BELL_TOKEN"}
	if got := holder.Names(); !slices.Equal(got, want) {
		t.Errorf("Names = %q, want %q", got, want)
	}
}

// A nil holder is the empty scrub, and a Config without one reads its fixed list.
func TestConfigSecretEnvNamesFallsBackToTheFixedList(t *testing.T) {
	t.Parallel()
	var holder *SecretEnv
	holder.Widen([]string{"IGNORED"})
	fixed := Config{SecretEnvVars: []string{"FIXED"}}
	live := Config{SecretEnvVars: []string{"FIXED"}, SecretEnv: NewSecretEnv([]string{"LIVE"})}

	if holder.Names() != nil {
		t.Errorf("nil holder Names = %q, want none", holder.Names())
	}
	if got := fixed.SecretEnvNames(); !slices.Equal(got, []string{"FIXED"}) {
		t.Errorf("fixed SecretEnvNames = %q, want the SecretEnvVars list", got)
	}
	if got := live.SecretEnvNames(); !slices.Equal(got, []string{"LIVE"}) {
		t.Errorf("live SecretEnvNames = %q, want the holder's list, which outranks the fixed one", got)
	}
}
