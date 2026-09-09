package tools

import (
	"os"
	"testing"
)

// TestRedactSecretsReplacesEveryConfiguredValue pins the output-side half of the credential
// scrub. The rows that matter are the ones a naive implementation gets wrong: a secret that is a
// PREFIX of another must not be replaced first — that would leave the longer value's tail in the
// clear, which is worse than not redacting at all because the text now looks scrubbed — and an
// unset or empty-valued name must contribute nothing, since an empty needle matches at every
// position and would turn the whole text into markers.
func TestRedactSecretsReplacesEveryConfiguredValue(t *testing.T) {
	// Not parallel: t.Setenv.
	t.Setenv("APOGEE_TEST_REDACT_TOKEN", "sk-live-42")
	t.Setenv("APOGEE_TEST_REDACT_TOKEN_LONG", "sk-live-42-extended")
	t.Setenv("APOGEE_TEST_REDACT_BLANK", "")
	// t.Setenv first so the cleanup restores whatever the developer's environment held, then
	// unset: the row needs a name that is reliably absent, not merely unlikely.
	t.Setenv("APOGEE_TEST_REDACT_ABSENT", "placeholder")
	if err := os.Unsetenv("APOGEE_TEST_REDACT_ABSENT"); err != nil {
		t.Fatalf("unset the absent-name fixture: %v", err)
	}

	// A blank and an empty entry ride along in every row: a blank name in somebody's
	// configuration is never permission to redact everything.
	configured := []string{
		"APOGEE_TEST_REDACT_TOKEN",
		"APOGEE_TEST_REDACT_TOKEN_LONG",
		"APOGEE_TEST_REDACT_BLANK",
		"APOGEE_TEST_REDACT_ABSENT",
		"   ",
		"",
	}

	tests := []struct {
		name      string
		secretEnv []string
		in        string
		want      string
	}{
		{
			name:      "the longer value is replaced whole rather than left as a tail",
			secretEnv: configured,
			in:        "Authorization: Bearer sk-live-42-extended",
			want:      "Authorization: Bearer [redacted]",
		},
		{
			name:      "both configured values are replaced where both appear",
			secretEnv: configured,
			in:        "short sk-live-42 then long sk-live-42-extended",
			want:      "short [redacted] then long [redacted]",
		},
		{
			name:      "a name nothing exported redacts nothing",
			secretEnv: []string{"APOGEE_TEST_REDACT_ABSENT"},
			in:        "APOGEE_TEST_REDACT_ABSENT is not exported here",
			want:      "APOGEE_TEST_REDACT_ABSENT is not exported here",
		},
		{
			name:      "a name exported empty never matches",
			secretEnv: []string{"APOGEE_TEST_REDACT_BLANK"},
			in:        "an ordinary line of tool output",
			want:      "an ordinary line of tool output",
		},
		{
			name:      "text carrying no secret is returned unchanged",
			secretEnv: configured,
			in:        "plain output with no credential in it",
			want:      "plain output with no credential in it",
		},
		{
			name:      "no configured names leaves the text alone",
			secretEnv: nil,
			in:        "sk-live-42 is not configured as a secret",
			want:      "sk-live-42 is not configured as a secret",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RedactSecrets(tc.in, tc.secretEnv); got != tc.want {
				t.Errorf("RedactSecrets(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
