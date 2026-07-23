package version

import "testing"

// These tests mutate the package-global Version, so they must not run in parallel
// with each other; they restore the prior value with t.Cleanup.

func TestStringReturnsLdflagsValueVerbatim(t *testing.T) {
	prev := Version
	t.Cleanup(func() { Version = prev })

	Version = "v9.9.9-test"
	if got := String(); got != "v9.9.9-test" {
		t.Errorf("String() = %q; want the injected Version %q verbatim", got, "v9.9.9-test")
	}
}

func TestStringFallsBackWhenUnset(t *testing.T) {
	prev := Version
	t.Cleanup(func() { Version = prev })

	Version = ""
	if got := String(); got == "" {
		t.Error(`String() = ""; want a non-empty fallback ("dev" or a VCS revision) when Version is unset`)
	}
}
